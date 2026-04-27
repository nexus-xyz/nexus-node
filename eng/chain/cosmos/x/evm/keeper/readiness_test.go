package keeper

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/common"

	"nexus/lib"
	"nexus/x/evm/types"
)

// engineClientForkchoiceStub implements lib.EngineClient for ProbeCommittedExecutionHead tests only.
type engineClientForkchoiceStub struct {
	fcResp engine.ForkChoiceResponse
	fcErr  error
}

func (s *engineClientForkchoiceStub) NewPayloadV3(
	context.Context, engine.ExecutableData, []common.Hash, *common.Hash,
) (engine.PayloadStatusV1, error) {
	return engine.PayloadStatusV1{}, errors.New("not used in readiness test")
}

func (s *engineClientForkchoiceStub) NewPayloadV4(
	context.Context, engine.ExecutableData, []common.Hash, *common.Hash, *types.ConsensusRequests,
) (engine.PayloadStatusV1, error) {
	return engine.PayloadStatusV1{}, errors.New("not used in readiness test")
}

func (s *engineClientForkchoiceStub) NewPayloadV5(
	context.Context, engine.ExecutableData, []common.Hash, *common.Hash, *types.ConsensusRequests,
) (engine.PayloadStatusV1, error) {
	return engine.PayloadStatusV1{}, errors.New("not used in readiness test")
}

func (s *engineClientForkchoiceStub) ForkchoiceUpdatedV3(
	_ context.Context,
	_ engine.ForkchoiceStateV1,
	_ *engine.PayloadAttributes,
) (engine.ForkChoiceResponse, error) {
	if s.fcErr != nil {
		return engine.ForkChoiceResponse{}, s.fcErr
	}
	return s.fcResp, nil
}

func (s *engineClientForkchoiceStub) GetPayloadV3(
	context.Context, engine.PayloadID,
) (*engine.ExecutionPayloadEnvelope, error) {
	return nil, errors.New("not used in readiness test")
}

func (s *engineClientForkchoiceStub) GetPayloadV4(
	context.Context, engine.PayloadID,
) (*engine.ExecutionPayloadEnvelope, error) {
	return nil, errors.New("not used in readiness test")
}

func (s *engineClientForkchoiceStub) GetPayloadV5(
	context.Context, engine.PayloadID,
) (*engine.ExecutionPayloadEnvelope, error) {
	return nil, errors.New("not used in readiness test")
}

func TestProbeCommittedExecutionHead_valid(t *testing.T) {
	t.Parallel()
	hash := common.HexToHash("0x01")
	committed := types.NewBlockState(hash, 1, 0)
	k := Keeper{
		engineClient: &engineClientForkchoiceStub{
			fcResp: engine.ForkChoiceResponse{
				PayloadStatus: engine.PayloadStatusV1{Status: engine.VALID},
			},
		},
	}
	if err := k.ProbeCommittedExecutionHead(t.Context(), committed); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}

func TestProbeCommittedExecutionHead_nonValidStatus(t *testing.T) {
	t.Parallel()
	hash := common.HexToHash("0x02")
	committed := types.NewBlockState(hash, 2, 0)
	k := Keeper{
		engineClient: &engineClientForkchoiceStub{
			fcResp: engine.ForkChoiceResponse{
				PayloadStatus: engine.PayloadStatusV1{Status: engine.SYNCING},
			},
		},
	}
	err := k.ProbeCommittedExecutionHead(t.Context(), committed)
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "SYNCING") {
		t.Fatalf("error %q should mention SYNCING", err.Error())
	}
	if !strings.Contains(err.Error(), hash.Hex()) {
		t.Fatalf("error %q should mention head %s", err.Error(), hash.Hex())
	}
}

func TestProbeCommittedExecutionHead_rpcError(t *testing.T) {
	t.Parallel()
	committed := types.NewBlockState(common.Hash{3}, 3, 0)
	rpcErr := fmt.Errorf("connection refused")
	k := Keeper{engineClient: &engineClientForkchoiceStub{fcErr: rpcErr}}
	err := k.ProbeCommittedExecutionHead(t.Context(), committed)
	if !errors.Is(err, rpcErr) {
		t.Fatalf("want rpc error, got %v", err)
	}
}

func TestProbeCommittedExecutionHead_usesStubEngineSyncing(t *testing.T) {
	t.Parallel()
	// lib.NewStubEngineClient returns SYNCING on forkchoice — documents real client contract.
	k := Keeper{engineClient: lib.NewStubEngineClient()}
	committed := types.NewBlockState(common.Hash{4}, 4, 0)
	err := k.ProbeCommittedExecutionHead(t.Context(), committed)
	if err == nil || !strings.Contains(err.Error(), "SYNCING") {
		t.Fatalf("want SYNCING-related error from stub client, got %v", err)
	}
}
