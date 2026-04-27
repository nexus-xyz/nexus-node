package keeper

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/beacon/engine"

	"nexus/lib"
	"nexus/x/evm/types"
)

// NewKeeperForReadinessTests returns a Keeper with only the Engine client set. It exists so
// app-layer readiness tests can exercise ProbeCommittedExecutionHead without a full module
// wiring; do not use in production.
func NewKeeperForReadinessTests(engineClient lib.EngineClient) Keeper {
	return Keeper{engineClient: engineClient}
}

// ProbeCommittedExecutionHead checks that the execution client already recognizes the
// committed head via engine_forkchoiceUpdatedV3 with nil payload attributes (same
// pattern as finalizing an execution payload, without building a new block).
func (k Keeper) ProbeCommittedExecutionHead(ctx context.Context, committed types.BlockState) error {
	fcState := engine.ForkchoiceStateV1{
		HeadBlockHash:      committed.Hash,
		SafeBlockHash:      committed.Hash,
		FinalizedBlockHash: committed.Hash,
	}
	resp, err := k.engineClient.ForkchoiceUpdatedV3(ctx, fcState, nil)
	if err != nil {
		return err
	}
	if resp.PayloadStatus.Status != engine.VALID {
		return fmt.Errorf("execution client forkchoice status %s (want VALID) head=%s",
			resp.PayloadStatus.Status, committed.Hash.Hex())
	}
	return nil
}
