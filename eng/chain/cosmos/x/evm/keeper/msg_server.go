package keeper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"nexus/x/evm/types"

	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/common"
	evmtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/hashicorp/go-metrics"
)

const (
	// MaxBlobsPerBlock enforces the Cancun (a.k.a. Dencun) hard fork blob cap.
	// MAX_DATA_GAS_PER_BLOCK_DENCUN / DATA_GAS_PER_BLOB = 786432 / 131072 = 6.
	// Update this when migrating to a fork that changes the per-block blob limit.
	MaxBlobsPerBlock = 6
)

type msgServer struct {
	Keeper
}

// NewMsgServerImpl returns an implementation of the MsgServer interface
// for the provided Keeper.
func NewMsgServerImpl(keeper Keeper) types.MsgServer {
	return &msgServer{Keeper: keeper}
}

var _ types.MsgServer = msgServer{}

// ExecutionPayload handles MsgExecutionPayload
func (s msgServer) ExecutionPayload(
	ctx context.Context,
	req *types.MsgExecutionPayload,
) (*types.MsgExecutionPayloadResponse, error) {
	// Make sure we are finalizing the block
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if sdkCtx.ExecMode() != sdk.ExecModeFinalize {
		return nil, errors.New("execution payload can only be submitted in finalize mode")
	}

	// Make sure the execution payload is not empty
	if len(req.ExecutionPayload) == 0 {
		return nil, fmt.Errorf("execution payload cannot be empty")
	}

	// Get the current block state
	state, err := s.GetBlockState(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get block state: %w", err)
	}

	// Validate the payload
	payload, err := s.Keeper.validatePayload(sdkCtx, req, &state)
	if err != nil {
		return nil, err
	}

	// Send the payload to the EVM engine client
	err = retry(ctx, func(ctx context.Context) (bool, error) {
		status, err := s.Keeper.sendPayload(ctx, payload)

		// Retry on network failures
		if err != nil {
			return false, nil
		}

		telemetry.IncrCounterWithLabels(
			[]string{"evm", "payload_status"},
			1,
			[]metrics.Label{telemetry.NewLabel("status", status.Status)},
		)

		// Proceed if our status is valid or syncing
		if status.Status != engine.VALID && status.Status != engine.SYNCING {
			return false, errors.New("payload is not valid with status: " + status.Status)
		}

		return true, nil
	})

	if err != nil {
		return nil, err
	}

	fcState := engine.ForkchoiceStateV1{
		HeadBlockHash:      payload.BlockHash,
		SafeBlockHash:      payload.BlockHash,
		FinalizedBlockHash: payload.BlockHash,
	}

	// Update the forkchoice state
	err = retry(ctx, func(ctx context.Context) (bool, error) {
		response, err := s.engineClient.ForkchoiceUpdatedV3(ctx, fcState, nil)
		if err != nil {
			return false, nil
		}

		telemetry.IncrCounterWithLabels(
			[]string{"evm", "forkchoice_status"},
			1,
			[]metrics.Label{telemetry.NewLabel("status", response.PayloadStatus.Status)},
		)

		if response.PayloadStatus.Status == engine.INVALID {
			return false, errors.New("forkchoice not updated with status: " + response.PayloadStatus.Status)
		}

		return true, nil
	})

	if err != nil {
		return nil, err
	}

	// Set the block state
	blockState := types.BlockState{
		Height:    payload.Number,
		Timestamp: payload.Timestamp,
		Hash:      payload.BlockHash,
	}

	if err := s.SetBlockState(ctx, blockState); err != nil {
		return nil, err
	}

	return &types.MsgExecutionPayloadResponse{}, nil
}

// validatePayload validates the execution payload.
func (k Keeper) validatePayload(
	ctx sdk.Context,
	msg *types.MsgExecutionPayload,
	state *types.BlockState,
) (engine.ExecutableData, error) {
	// Verify the payload authority
	if msg.Authority != authtypes.NewModuleAddress(types.ModuleName).String() {
		return engine.ExecutableData{}, fmt.Errorf(
			"invalid authority; expected %s, got %s",
			authtypes.NewModuleAddress(types.ModuleName).String(),
			msg.Authority,
		)
	}

	// Parse the payload
	var payload engine.ExecutableData
	if err := json.Unmarshal(msg.ExecutionPayload, &payload); err != nil {
		return engine.ExecutableData{}, fmt.Errorf("invalid execution payload: %w", err)
	}

	// Make sure there are no withdrawals
	if len(payload.Withdrawals) > 0 {
		return engine.ExecutableData{}, errors.New("withdrawals are not supported")
	}

	// Validate the withdrawals, blob gas used, and excess blob gas are not nil
	if payload.Withdrawals == nil || payload.BlobGasUsed == nil || payload.ExcessBlobGas == nil {
		return engine.ExecutableData{}, errors.New("withdrawals, blob gas used, and excess blob gas are required")
	}

	// Make sure there is no execution witness
	if payload.ExecutionWitness != nil {
		return engine.ExecutableData{}, errors.New("execution witness is not supported")
	}

	// The parent hash must equal the current block hash
	if payload.ParentHash != state.Hash {
		return engine.ExecutableData{}, fmt.Errorf(
			"invalid parent hash: expected %s, got %s",
			state.Hash.Hex(),
			payload.ParentHash.Hex(),
		)
	}

	// The payload block number must be exactly current height + 1
	if payload.Number != state.Height+1 {
		return engine.ExecutableData{}, fmt.Errorf(
			"invalid block number: expected %d, got %d",
			state.Height+1,
			payload.Number,
		)
	}

	// The payload timestamp must be strictly greater than current timestamp
	if payload.Timestamp <= state.Timestamp {
		return engine.ExecutableData{}, fmt.Errorf(
			"invalid timestamp: expected > %d, got %d",
			state.Timestamp,
			payload.Timestamp,
		)
	}

	// The payload timestamp must be equal to the expected value
	height := toUint64(ctx.BlockHeader().Height)
	timestamp := k.CalculateTimestamp(ctx, height, state.Timestamp)
	if payload.Timestamp != timestamp {
		return engine.ExecutableData{}, fmt.Errorf(
			"invalid timestamp: expected %d, got %d",
			timestamp,
			payload.Timestamp,
		)
	}

	return payload, nil
}

// sendPayload sends the payload to the EVM engine client.
func (k Keeper) sendPayload(
	ctx context.Context,
	payload engine.ExecutableData,
) (engine.PayloadStatusV1, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	appHash := common.BytesToHash(sdkCtx.BlockHeader().AppHash)
	if appHash == (common.Hash{}) {
		return engine.PayloadStatusV1{}, errors.New("app hash is empty")
	}

	// Build versioned blob hashes from the payload transactions (EIP-4844).
	// Source: https://eips.wiki/eips/protocol/execution/eip-4844/
	// The execution client validates that the provided versioned hashes
	// correspond to the blob transactions contained in the payload.
	versionedHashes := make([]common.Hash, 0)
	for _, txBytes := range payload.Transactions {
		tx := new(evmtypes.Transaction)
		if err := tx.UnmarshalBinary(txBytes); err != nil {
			return engine.PayloadStatusV1{}, fmt.Errorf("decode tx: %w", err)
		}

		// Append any versioned blob hashes for blob-carrying transactions.
		if hashes := tx.BlobHashes(); len(hashes) > 0 {
			versionedHashes = append(versionedHashes, hashes...)
		}
	}

	if len(versionedHashes) > MaxBlobsPerBlock {
		return engine.PayloadStatusV1{}, fmt.Errorf(
			"too many blobs in block: %d > %d",
			len(versionedHashes),
			MaxBlobsPerBlock,
		)
	}

	var (
		status engine.PayloadStatusV1
		err    error
	)

	if k.chainSpec.IsAmsterdamActive(payload.Timestamp) {
		status, err = k.engineClient.NewPayloadV5(ctx, payload, versionedHashes, &appHash, nil)
		if err != nil {
			return engine.PayloadStatusV1{}, err
		}
	} else if k.chainSpec.IsPragueActive(payload.Timestamp) {
		status, err = k.engineClient.NewPayloadV4(ctx, payload, versionedHashes, &appHash, nil)
		if err != nil {
			return engine.PayloadStatusV1{}, err
		}
	} else {
		status, err = k.engineClient.NewPayloadV3(ctx, payload, versionedHashes, &appHash)
		if err != nil {
			return engine.PayloadStatusV1{}, err
		}
	}

	return status, nil
}
