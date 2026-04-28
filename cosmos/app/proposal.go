package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	upgradetypes "cosmossdk.io/x/upgrade/types"
	abci "github.com/cometbft/cometbft/abci/types"
	cmttypes "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"

	evmkeeper "nexus/x/evm/keeper"
	"nexus/x/evm/tests/testutil"
	"nexus/x/evm/types"
)

const timeout = time.Second * 10

// parsedTx holds a decoded and classified transaction.
type parsedTx struct {
	raw   []byte
	tx    sdk.Tx
	isEVM bool
}

// allowedMsgTypes is the set of Cosmos SDK message types permitted in blocks.
var allowedMsgTypes = map[reflect.Type]struct{}{
	reflect.TypeFor[*govv1.MsgSubmitProposal]():         {},
	reflect.TypeFor[*govv1.MsgVote]():                   {},
	reflect.TypeFor[*govv1.MsgVoteWeighted]():           {},
	reflect.TypeFor[*govv1.MsgDeposit]():                {},
	reflect.TypeFor[*banktypes.MsgSend]():               {},
	reflect.TypeFor[*upgradetypes.MsgSoftwareUpgrade](): {},
	reflect.TypeFor[*upgradetypes.MsgCancelUpgrade]():   {},
}

// validateMsgAllowed checks that the tx contains exactly one message of a permitted type.
func validateMsgAllowed(tx sdk.Tx) error {
	msgs := tx.GetMsgs()
	if len(msgs) != 1 {
		return errors.New("expected exactly one message")
	}
	switch msgs[0].(type) {
	case *types.MsgExecutionPayload:
		return nil
	default:
		if _, ok := allowedMsgTypes[reflect.TypeOf(msgs[0])]; !ok {
			return fmt.Errorf("message type %T not permitted", msgs[0])
		}
		return nil
	}
}

// parseTx decodes, validates, and parses a raw transaction type.
// Returns an error if the tx is malformed or its message type is not permitted.
func parseTx(txConfig client.TxConfig, raw []byte) (parsedTx, error) {
	tx, err := txConfig.TxDecoder()(raw)
	if err != nil {
		return parsedTx{}, err
	}

	if err := validateMsgAllowed(tx); err != nil {
		return parsedTx{}, err
	}

	_, isEVM := tx.GetMsgs()[0].(*types.MsgExecutionPayload)
	if isEVM {
		if err := validateTx(tx); err != nil {
			return parsedTx{}, err
		}
	}

	return parsedTx{raw: raw, tx: tx, isEVM: isEVM}, nil
}

// makePrepareProposalHandler returns a PrepareProposalHandler that passes
// permitted Cosmos SDK transactions from the mempool through to the block,
// then appends the EVM execution payload as the final transaction.
func makePrepareProposalHandler(
	evmKeeper *evmkeeper.Keeper, txConfig client.TxConfig, maxCosmosTxs int,
) sdk.PrepareProposalHandler {
	return func(ctx sdk.Context, req *abci.RequestPrepareProposal) (*abci.ResponsePrepareProposal, error) {
		if req.Height == 1 {
			return &abci.ResponsePrepareProposal{}, nil
		}

		var cosmosTxs [][]byte
		for _, rawTx := range req.Txs {
			if len(cosmosTxs) >= maxCosmosTxs {
				break
			}
			parsed, err := parseTx(txConfig, rawTx)
			if err != nil || parsed.isEVM {
				continue
			}
			cosmosTxs = append(cosmosTxs, rawTx)
		}

		evmResp, err := evmKeeper.PrepareProposal(ctx, req)
		if err != nil {
			return nil, err
		}

		// Cosmos txs first, EVM payload last.
		return &abci.ResponsePrepareProposal{
			Txs: append(cosmosTxs, evmResp.Txs...),
		}, nil
	}
}

func makeProcessProposalRouter(app *App) *baseapp.MsgServiceRouter {
	router := baseapp.NewMsgServiceRouter()
	router.SetInterfaceRegistry(app.interfaceRegistry)
	app.EvmKeeper.RegisterProposalHandlers(router)

	return router
}

func makeProcessProposalHandler(
	router *baseapp.MsgServiceRouter, txConfig client.TxConfig, maxCosmosTxs int,
) sdk.ProcessProposalHandler {
	return func(ctx sdk.Context, req *abci.RequestProcessProposal) (*abci.ResponseProcessProposal, error) {
		// Reject the proposal if it takes longer than 10s
		timeoutCtx, timeoutCancel := context.WithTimeout(ctx.Context(), timeout)
		defer timeoutCancel()
		ctx = ctx.WithContext(timeoutCtx)

		if req.Height == 1 {
			if len(req.Txs) > 0 {
				return rejectProposal(ctx, errors.New("initial block cannot have transactions"))
			}

			return &abci.ResponseProcessProposal{Status: abci.ResponseProcessProposal_ACCEPT}, nil
		}

		if len(req.Txs) == 0 {
			return rejectProposal(ctx, errors.New("block must contain at least one transaction"))
		}

		var totalPower, votedPower int64
		for _, vote := range req.ProposedLastCommit.Votes {
			totalPower += vote.Validator.Power
			// Voted power is the sum of the power of all the validators that committed to the block
			if vote.BlockIdFlag == cmttypes.BlockIDFlagCommit {
				votedPower += vote.Validator.Power
			}
		}

		if votedPower <= totalPower*2/3 {
			return rejectProposal(ctx, errors.New("reject blocks with less than (or equal to) 2/3 of the voting power"))
		}

		// Parse and classify all transactions. The EVM payload must be last and
		// appear exactly once; all other transactions must be in the allowlist
		// and must not exceed maxCosmosTxs.
		var evmMsg *types.MsgExecutionPayload
		cosmosTxCount := 0
		for i, rawTx := range req.Txs {
			// Limit to 20MB to prevent validators from running out of storage or memory.
			if len(rawTx) > testutil.MaxTxSize {
				return rejectProposal(ctx, fmt.Errorf("transaction too large: %d bytes exceeds maximum of %d bytes",
					len(rawTx), testutil.MaxTxSize))
			}

			parsed, err := parseTx(txConfig, rawTx)
			if err != nil {
				return rejectProposal(ctx, err)
			}

			isLast := i == len(req.Txs)-1
			if parsed.isEVM && !isLast {
				return rejectProposal(ctx, errors.New("EVM payload must be the last transaction"))
			}
			if !parsed.isEVM && isLast {
				return rejectProposal(ctx, errors.New("last transaction must be the EVM payload"))
			}
			if !parsed.isEVM {
				cosmosTxCount++
				if cosmosTxCount > maxCosmosTxs {
					return rejectProposal(ctx, fmt.Errorf("too many cosmos transactions: max %d", maxCosmosTxs))
				}
			}
			if parsed.isEVM {
				msg, ok := parsed.tx.GetMsgs()[0].(*types.MsgExecutionPayload)
				if !ok {
					return rejectProposal(ctx, errors.New("expected EVM payload message"))
				}
				evmMsg = msg
			}
		}

		handler := router.Handler(evmMsg)
		if handler == nil {
			return rejectProposal(ctx, errors.New("expected handler"))
		}

		if _, err := handler(ctx, evmMsg); err != nil {
			return rejectProposal(ctx, err)
		}

		return &abci.ResponseProcessProposal{Status: abci.ResponseProcessProposal_ACCEPT}, nil
	}
}

func rejectProposal(ctx sdk.Context, err error) (*abci.ResponseProcessProposal, error) {
	fmt.Println("Rejecting Proposal", err)
	return &abci.ResponseProcessProposal{Status: abci.ResponseProcessProposal_REJECT}, nil
}

func validateTx(tx sdk.Tx) error {
	msgs := tx.GetMsgs()

	if len(msgs) != 1 {
		return errors.New("expected exactly one message")
	}

	standardTx, ok := tx.(signing.Tx)
	if !ok {
		return errors.New("expected standard transaction")
	}

	signatures, err := standardTx.GetSignaturesV2()
	if err != nil {
		return err
	}

	if len(signatures) != 0 {
		return errors.New("expected no signatures")
	}

	if memo := standardTx.GetMemo(); len(memo) != 0 {
		return errors.New("expected no memo")
	}

	if fee := standardTx.GetFee(); fee != nil {
		return errors.New("expected no fee")
	}

	if !bytes.Equal(standardTx.FeePayer(), authtypes.NewModuleAddress(types.ModuleName).Bytes()) {
		return errors.New("expected fee payer to be the evm module")
	}

	if feeGranter := standardTx.FeeGranter(); feeGranter != nil {
		return errors.New("expected no fee granter")
	}

	return nil
}
