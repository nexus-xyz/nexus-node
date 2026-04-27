package keeper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
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

// lastLoggedRPCVersion remembers the engine_getPayloadVx version we logged last
// so we emit a single Info line when the active version changes across a fork
// boundary (V3→V4 at Prague, V4→V5 at Osaka). Per-process state is exactly
// what we want: each validator emits its own transition marker. Zero value
// means "never logged yet"; we seed it on the first PrepareProposal.
var lastLoggedRPCVersion atomic.Int32

// Upper bound for forkchoice + getPayload during PrepareProposal when the incoming sdk.Context
// has no (or a very long) deadline. In normal consensus, CometBFT’s ABCI deadline is shorter and
// wins (context.WithTimeout uses the earlier of the two). A generous cap avoids failing tests and
// any code path that still uses a background context while the EL catches up after forkchoice.
const prepareTimeout = 10 * time.Second

// Wait for `buildDelay` to ensure the payload is available.
const buildDelay = 100 * time.Millisecond

// Observability and safety around engine_getPayload "unknown payload" retries (benign right after
// forkchoiceUpdated; a long streak of only-unknown often indicates a fork-matrix / RPC mismatch).
const (
	unknownPayloadLogEvery = 25
	// maxUnknownPayloadPoll caps steady unknown-only polling when nothing else cancels the context
	// (rare). Attempt limit is maxUnknownPayloadPoll / retryDelay (~5s of sleeps alone at the default
	// 10ms backoff; each attempt also pays RPC latency).
	maxUnknownPayloadPoll                = 5 * time.Second
	maxConsecutiveUnknownPayloadAttempts = int(maxUnknownPayloadPoll / retryDelay)
)

func (k *Keeper) PrepareProposal(ctx sdk.Context, req *abci.RequestPrepareProposal) (
	*abci.ResponsePrepareProposal, error,
) {
	// sdk.Context is not a context.Context; use the embedded Go context so cancellation/deadlines
	// from the SDK propagate into engine RPCs.
	timeoutCtx, cancel := context.WithTimeout(ctx.Context(), prepareTimeout)
	defer cancel()
	ctx = ctx.WithContext(timeoutCtx)

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

	// Emit a single Info line per validator each time the engine_getPayloadVx
	// version changes (i.e. when a hard fork activates). This makes fork
	// activation visible from the Cosmos side even if Reth is misconfigured
	// for the new version (which is the failure mode we hit on devnet during
	// the Osaka activation).
	rpcVer := int32(k.chainSpec.GetPayloadEngineRPCVersion(timestamp))
	if prev := lastLoggedRPCVersion.Swap(rpcVer); prev != rpcVer {
		sdkCtx.Logger().Info(
			"PrepareProposal: engine getPayload RPC version changed",
			"height", height,
			"timestamp", timestamp,
			"from_get_payload_rpc_version", prev,
			"to_get_payload_rpc_version", rpcVer,
		)
	}

	var payloadID engine.PayloadID
	err = retry(ctx, func(ctx context.Context) (bool, error) {
		response, err := k.buildExecutionPayload(ctx, sdkCtx, appHash, height)
		if err != nil {
			// retry() will swallow this error and try again until the SDK
			// context expires. Without this Warn the Cosmos logs go silent
			// while the EL is failing forkchoiceUpdated, which is exactly
			// what made the Osaka halt opaque.
			sdkCtx.Logger().Warn(
				"PrepareProposal: forkchoiceUpdated failed, will retry",
				"height", height,
				"timestamp", timestamp,
				"get_payload_rpc_version", rpcVer,
				"err", err.Error(),
			)
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
	consecutiveUnknownPayload := 0
	err = retry(ctx, func(ctx context.Context) (bool, error) {
		if k.chainSpec.IsOsakaActive(timestamp) {
			payloadResponse, err = k.engineClient.GetPayloadV5(ctx, payloadID)
		} else if k.chainSpec.IsPragueActive(timestamp) {
			payloadResponse, err = k.engineClient.GetPayloadV4(ctx, payloadID)
		} else {
			payloadResponse, err = k.engineClient.GetPayloadV3(ctx, payloadID)
		}
		// UnknownPayload is common immediately after forkchoiceUpdated; retry() will sleep and
		// repeat until getPayload succeeds or this PrepareProposal context is done (not infinite).
		if isPayloadUnknown(err) {
			consecutiveUnknownPayload++
			rpcVer := k.chainSpec.GetPayloadEngineRPCVersion(timestamp)
			if consecutiveUnknownPayload == 1 || consecutiveUnknownPayload%unknownPayloadLogEvery == 0 {
				sdkCtx.Logger().Warn(
					"PrepareProposal: engine getPayload unknown payload, retrying",
					"attempt", consecutiveUnknownPayload,
					"height", height,
					"get_payload_rpc_version", rpcVer,
					"payload_id", payloadID.String(),
					"err", err.Error(),
				)
			}
			if consecutiveUnknownPayload >= maxConsecutiveUnknownPayloadAttempts {
				minWall := time.Duration(consecutiveUnknownPayload) * retryDelay
				return false, fmt.Errorf(
					"PrepareProposal: engine getPayload still unknown after ~%s of polling "+
						"(%d attempts, %s between attempts; height=%d get_payload_rpc_version=%d payload_id=%s): %w",
					minWall.Round(time.Millisecond),
					consecutiveUnknownPayload,
					retryDelay,
					height,
					rpcVer,
					payloadID.String(),
					err,
				)
			}
			return false, nil
		}
		consecutiveUnknownPayload = 0
		if err != nil {
			// Non-UnknownPayload errors here (e.g. unknown method, version
			// mismatch, transport failures) were previously swallowed by the
			// retry loop, leaving no breadcrumb when the EL rejected the
			// call. Surface them at Warn so a fork-matrix mismatch is
			// immediately visible.
			sdkCtx.Logger().Warn(
				"PrepareProposal: engine getPayload failed (non-unknown), retrying",
				"height", height,
				"timestamp", timestamp,
				"get_payload_rpc_version", rpcVer,
				"payload_id", payloadID.String(),
				"err", err.Error(),
			)
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
