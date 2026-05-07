package keeper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/common"
	etypes "github.com/ethereum/go-ethereum/core/types"

	"nexus/x/evm/tests/testutil"
	"nexus/x/evm/types"
)

// Use a timeout of 1s to prepare the proposal.
const prepareTimeout = 1 * time.Second

// Wait for `buildDelay` to ensure the payload is available.
const buildDelay = 100 * time.Millisecond

func (k *Keeper) PrepareProposal(ctx sdk.Context, req *abci.RequestPrepareProposal) (
	*abci.ResponsePrepareProposal, error,
) {
	timeoutCtx, cancel := context.WithTimeout(ctx, prepareTimeout)
	defer cancel()
	ctx = ctx.WithContext(timeoutCtx)

	if len(req.Txs) > 0 {
		return nil, errors.New("unexpected transactions in proposal")
	}

	if req.Height == 1 {
		return &abci.ResponsePrepareProposal{}, nil
	}

	appHash := common.BytesToHash(ctx.BlockHeader().AppHash)
	height := toUint64(ctx.BlockHeader().Height)
	sdkCtx := ctx // Capture sdk.Context before retry

	state, err := k.GetBlockState(ctx)
	if err != nil {
		return nil, err
	}
	timestamp := k.CalculateTimestamp(sdkCtx, height, state.Timestamp)

	var payloadID engine.PayloadID
	err = retry(ctx, func(ctx context.Context) (bool, error) {
		response, err := k.buildExecutionPayload(ctx, sdkCtx, appHash, height)
		if err != nil {
			return false, nil
		}

		if response.PayloadStatus.Status != engine.VALID {
			return false, errors.New("forkchoice not updated with status: " + response.PayloadStatus.Status)
		}
		if response.PayloadID == nil {
			return false, errors.New("payload ID is nil")
		}

		payloadID = *response.PayloadID
		return true, nil
	})

	if err != nil {
		return nil, err
	}

	// Wait for `buildDelay` to ensure the payload is available.
	select {
	case <-time.After(buildDelay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	var payloadResponse *engine.ExecutionPayloadEnvelope
	err = retry(ctx, func(ctx context.Context) (bool, error) {
		if k.chainSpec.IsEngineV4PragueEnabled(timestamp) {
			payloadResponse, err = k.engineClient.GetPayloadV4(ctx, payloadID)
		} else {
			payloadResponse, err = k.engineClient.GetPayloadV3(ctx, payloadID)
		}
		if isPayloadUnknown(err) {
			return false, err
		}
		if err != nil {
			return false, nil
		}

		return true, nil
	})

	if err != nil {
		return nil, err
	}

	payloadData, err := json.Marshal(payloadResponse.ExecutionPayload)
	if err != nil {
		return nil, err
	}

	payloadMessage := &types.MsgExecutionPayload{
		Authority:        authtypes.NewModuleAddress(types.ModuleName).String(),
		ExecutionPayload: payloadData,
	}

	builder := k.txConfig.NewTxBuilder()
	if err := builder.SetMsgs(payloadMessage); err != nil {
		return nil, err
	}

	tx, err := k.txConfig.TxEncoder()(builder.GetTx())
	if err != nil {
		return nil, err
	}

	// Limit to 20MB to prevent validators from running out of storage

	if len(tx) > testutil.MaxTxSize {
		return nil, fmt.Errorf("transaction too large: %d bytes exceeds maximum of %d bytes",
			len(tx), testutil.MaxTxSize)
	}

	return &abci.ResponsePrepareProposal{
		Txs: [][]byte{tx},
	}, nil
}

// buildExecutionPayload sends a forkChoiceUpdatedV3 to the execution client, then
// waits for the next proposed payload to be available.
func (k *Keeper) buildExecutionPayload(
	ctx context.Context,
	sdkCtx sdk.Context,
	appHash common.Hash,
	blockHeight uint64,
) (engine.ForkChoiceResponse, error) {
	state, err := k.GetBlockState(ctx)
	if err != nil {
		return engine.ForkChoiceResponse{}, err
	}

	timestamp := k.CalculateTimestamp(sdkCtx, blockHeight, state.Timestamp)

	fcState := engine.ForkchoiceStateV1{
		HeadBlockHash:      state.Hash,
		SafeBlockHash:      state.Hash,
		FinalizedBlockHash: state.Hash,
	}

	attrs := &engine.PayloadAttributes{
		Timestamp:             timestamp,
		Random:                state.Hash,
		SuggestedFeeRecipient: k.SuggestedFeeRecipient,
		Withdrawals:           []*etypes.Withdrawal{},
		BeaconRoot:            &appHash,
	}

	response, err := k.engineClient.ForkchoiceUpdatedV3(ctx, fcState, attrs)
	if err != nil {
		return engine.ForkChoiceResponse{}, err
	}

	return response, nil
}

func isPayloadUnknown(err error) bool {
	if err == nil {
		return false
	}

	if strings.Contains(
		strings.ToLower(err.Error()),
		strings.ToLower(engine.UnknownPayload.Error()),
	) {
		return true
	}

	return false
}
