package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	cmttypes "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"nexus/x/evm/tests/testutil"
	"nexus/x/evm/types"
)

const timeout = time.Second * 10

func makeProcessProposalRouter(app *App) *baseapp.MsgServiceRouter {
	router := baseapp.NewMsgServiceRouter()
	router.SetInterfaceRegistry(app.interfaceRegistry)
	app.EvmKeeper.RegisterProposalHandlers(router)

	return router
}

func makeProcessProposalHandler(router *baseapp.MsgServiceRouter, txConfig client.TxConfig) sdk.ProcessProposalHandler {
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

		if len(req.Txs) != 1 {
			return rejectProposal(ctx, fmt.Errorf("expect one transaction in block, got %v", len(req.Txs)))
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

		rawTx := req.Txs[0]

		// Limit to 20MB to prevent validators from running out of storage or memory.
		// This is a conservative upper bound to avoid accidental or malicious proposals
		// that could exhaust disk or RAM on validator nodes. The value is chosen to be
		// much larger than typical block sizes, but small enough to avoid resource exhaustion.
		// This will eventually be replaced with protobuf size limits.

		if len(rawTx) > testutil.MaxTxSize {
			return rejectProposal(ctx, fmt.Errorf("transaction too large: %d bytes exceeds maximum of %d bytes",
				len(rawTx), testutil.MaxTxSize))
		}

		tx, err := txConfig.TxDecoder()(rawTx)
		if err != nil {
			return rejectProposal(ctx, err)
		}

		if err = validateTx(tx); err != nil {
			return rejectProposal(ctx, err)
		}

		msg, ok := tx.GetMsgs()[0].(*types.MsgExecutionPayload)
		if !ok {
			return rejectProposal(ctx, errors.New("expected MsgExecutionPayload"))
		}

		handler := router.Handler(msg)
		if handler == nil {
			return rejectProposal(ctx, errors.New("expected handler"))
		}

		if _, err := handler(ctx, msg); err != nil {
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
