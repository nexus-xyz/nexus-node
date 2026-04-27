package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"cosmossdk.io/log"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/common"

	"nexus/lib"
	evmmodulekeeper "nexus/x/evm/keeper"
	evmtypes "nexus/x/evm/types"
)

// readinessEngineClientStub implements lib.EngineClient for app readiness tests (forkchoice path only).
type readinessEngineClientStub struct {
	fcResp engine.ForkChoiceResponse
	fcErr  error
}

func (s *readinessEngineClientStub) NewPayloadV3(
	context.Context, engine.ExecutableData, []common.Hash, *common.Hash,
) (engine.PayloadStatusV1, error) {
	return engine.PayloadStatusV1{}, errors.New("not used in readiness test")
}

func (s *readinessEngineClientStub) NewPayloadV4(
	context.Context, engine.ExecutableData, []common.Hash, *common.Hash, *evmtypes.ConsensusRequests,
) (engine.PayloadStatusV1, error) {
	return engine.PayloadStatusV1{}, errors.New("not used in readiness test")
}

func (s *readinessEngineClientStub) NewPayloadV5(
	context.Context, engine.ExecutableData, []common.Hash, *common.Hash, *evmtypes.ConsensusRequests,
) (engine.PayloadStatusV1, error) {
	return engine.PayloadStatusV1{}, errors.New("not used in readiness test")
}

func (s *readinessEngineClientStub) ForkchoiceUpdatedV3(
	_ context.Context,
	_ engine.ForkchoiceStateV1,
	_ *engine.PayloadAttributes,
) (engine.ForkChoiceResponse, error) {
	if s.fcErr != nil {
		return engine.ForkChoiceResponse{}, s.fcErr
	}
	return s.fcResp, nil
}

func (s *readinessEngineClientStub) GetPayloadV3(
	context.Context, engine.PayloadID,
) (*engine.ExecutionPayloadEnvelope, error) {
	return nil, errors.New("not used in readiness test")
}

func (s *readinessEngineClientStub) GetPayloadV4(
	context.Context, engine.PayloadID,
) (*engine.ExecutionPayloadEnvelope, error) {
	return nil, errors.New("not used in readiness test")
}

func (s *readinessEngineClientStub) GetPayloadV5(
	context.Context, engine.PayloadID,
) (*engine.ExecutionPayloadEnvelope, error) {
	return nil, errors.New("not used in readiness test")
}

func testReadinessApp(
	ec lib.EngineClient,
	comet func(context.Context) (*coretypes.ResultStatus, error),
) *App {
	return &App{
		EvmKeeper:            evmmodulekeeper.NewKeeperForReadinessTests(ec),
		readinessCometStatus: comet,
	}
}

func cometStatusNotCatchingUp() func(context.Context) (*coretypes.ResultStatus, error) {
	return func(context.Context) (*coretypes.ResultStatus, error) {
		return &coretypes.ResultStatus{SyncInfo: coretypes.SyncInfo{CatchingUp: false}}, nil
	}
}

func cometStatusCatchingUp() func(context.Context) (*coretypes.ResultStatus, error) {
	return func(context.Context) (*coretypes.ResultStatus, error) {
		return &coretypes.ResultStatus{SyncInfo: coretypes.SyncInfo{CatchingUp: true}}, nil
	}
}

func TestRunReadinessChecks_healthy_noTrustedPeer(t *testing.T) {
	ctx := t.Context()
	committed := evmtypes.NewBlockState(common.Hash{1}, 100, 0)
	app := testReadinessApp(&readinessEngineClientStub{
		fcResp: engine.ForkChoiceResponse{PayloadStatus: engine.PayloadStatusV1{Status: engine.VALID}},
	}, cometStatusNotCatchingUp())
	err := app.readinessChecksPhases(ctx, committed, readinessProbeConfig{HeadLagMaxBlocks: 2})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRunReadinessChecks_headLag_skippedWhenTrustedPeerURLWhitespaceOnly(t *testing.T) {
	ctx := t.Context()
	committed := evmtypes.NewBlockState(common.Hash{1}, 100, 0)
	app := testReadinessApp(&readinessEngineClientStub{
		fcResp: engine.ForkChoiceResponse{PayloadStatus: engine.PayloadStatusV1{Status: engine.VALID}},
	}, cometStatusNotCatchingUp())
	err := app.readinessChecksPhases(ctx, committed, readinessProbeConfig{
		TrustedPeerStatusURL: " \t ", HeadLagMaxBlocks: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRunReadinessChecks_healthy_trustedPeerWithinLag(t *testing.T) {
	peer := httptest.NewServer(http.HandlerFunc(mockCometStatus(false, "101")))
	defer peer.Close()

	ctx := t.Context()
	committed := evmtypes.NewBlockState(common.Hash{1}, 100, 0)
	app := testReadinessApp(&readinessEngineClientStub{
		fcResp: engine.ForkChoiceResponse{PayloadStatus: engine.PayloadStatusV1{Status: engine.VALID}},
	}, cometStatusNotCatchingUp())
	err := app.readinessChecksPhases(ctx, committed, readinessProbeConfig{
		TrustedPeerStatusURL: peer.URL + "/status",
		HeadLagMaxBlocks:     2,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRunReadinessChecks_headLag_diffEqualsMaxLag_stillHealthy(t *testing.T) {
	peer := httptest.NewServer(http.HandlerFunc(mockCometStatus(false, "102")))
	defer peer.Close()

	ctx := t.Context()
	committed := evmtypes.NewBlockState(common.Hash{1}, 100, 0)
	app := testReadinessApp(&readinessEngineClientStub{
		fcResp: engine.ForkChoiceResponse{PayloadStatus: engine.PayloadStatusV1{Status: engine.VALID}},
	}, cometStatusNotCatchingUp())
	err := app.readinessChecksPhases(ctx, committed, readinessProbeConfig{
		TrustedPeerStatusURL: peer.URL + "/status",
		HeadLagMaxBlocks:     2,
	})
	if err != nil {
		t.Fatalf("abs(102-100)=2 with maxLag=2 must be healthy, got %v", err)
	}
}

func TestRunReadinessChecks_rethMissingCommittedHead(t *testing.T) {
	ctx := t.Context()
	committed := evmtypes.NewBlockState(common.Hash{1}, 100, 0)
	app := testReadinessApp(lib.NewStubEngineClient(), cometStatusNotCatchingUp())
	err := app.readinessChecksPhases(ctx, committed, readinessProbeConfig{HeadLagMaxBlocks: 2})
	if !asProbeError(err, errKindReth) {
		t.Fatalf("want reth probe error, got %v", err)
	}
}

func TestRunReadinessChecks_cosmosCatchingUp(t *testing.T) {
	ctx := t.Context()
	committed := evmtypes.NewBlockState(common.Hash{1}, 100, 0)
	app := testReadinessApp(&readinessEngineClientStub{
		fcResp: engine.ForkChoiceResponse{PayloadStatus: engine.PayloadStatusV1{Status: engine.VALID}},
	}, cometStatusCatchingUp())
	err := app.readinessChecksPhases(ctx, committed, readinessProbeConfig{HeadLagMaxBlocks: 2})
	if !asProbeError(err, errKindCosmos) {
		t.Fatalf("want cosmos probe error, got %v", err)
	}
}

func TestRunReadinessChecks_headLag_unhealthyWhenDiffExceedsMax(t *testing.T) {
	peer := httptest.NewServer(http.HandlerFunc(mockCometStatus(false, "100")))
	defer peer.Close()

	ctx := t.Context()
	committed := evmtypes.NewBlockState(common.Hash{1}, 10, 0)
	app := testReadinessApp(&readinessEngineClientStub{
		fcResp: engine.ForkChoiceResponse{PayloadStatus: engine.PayloadStatusV1{Status: engine.VALID}},
	}, cometStatusNotCatchingUp())
	err := app.readinessChecksPhases(ctx, committed, readinessProbeConfig{
		TrustedPeerStatusURL: peer.URL + "/status",
		HeadLagMaxBlocks:     2,
	})
	if !asProbeError(err, errKindHeadLag) {
		t.Fatalf("want head_lag probe error, got %v", err)
	}
}

func TestLoadReadinessProbeConfig_headLagMaxBlocks(t *testing.T) {
	t.Setenv("HEAD_LAG_MAX_BLOCKS", "")
	t.Setenv("TRUSTED_PEER_STATUS_URL", "")
	t.Setenv("TRUSTED_PEER_STATUS_HEADERS", "")
	cfg := loadReadinessProbeConfig()
	if cfg.HeadLagMaxBlocks != 2 {
		t.Fatalf("empty HEAD_LAG: got %d want 2", cfg.HeadLagMaxBlocks)
	}
	t.Setenv("HEAD_LAG_MAX_BLOCKS", "not-a-number")
	cfg = loadReadinessProbeConfig()
	if cfg.HeadLagMaxBlocks != 2 {
		t.Fatalf("invalid HEAD_LAG: got %d want 2", cfg.HeadLagMaxBlocks)
	}
	t.Setenv("HEAD_LAG_MAX_BLOCKS", "5")
	cfg = loadReadinessProbeConfig()
	if cfg.HeadLagMaxBlocks != 5 {
		t.Fatalf("valid HEAD_LAG: got %d want 5", cfg.HeadLagMaxBlocks)
	}
}

func TestRunReadinessChecks_trustedPeerBadResponse(t *testing.T) {
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `not json`)
	}))
	defer peer.Close()

	ctx := t.Context()
	committed := evmtypes.NewBlockState(common.Hash{1}, 100, 0)
	app := testReadinessApp(&readinessEngineClientStub{
		fcResp: engine.ForkChoiceResponse{PayloadStatus: engine.PayloadStatusV1{Status: engine.VALID}},
	}, cometStatusNotCatchingUp())
	err := app.readinessChecksPhases(ctx, committed, readinessProbeConfig{
		TrustedPeerStatusURL: peer.URL + "/status",
		HeadLagMaxBlocks:     2,
	})
	if !asProbeError(err, errKindTrustedPeerResponse) {
		t.Fatalf("want trusted_peer_response probe error, got %v", err)
	}
}

func TestRunReadinessChecks_trustedPeerUnavailable(t *testing.T) {
	ctx := t.Context()
	committed := evmtypes.NewBlockState(common.Hash{1}, 100, 0)
	app := testReadinessApp(&readinessEngineClientStub{
		fcResp: engine.ForkChoiceResponse{PayloadStatus: engine.PayloadStatusV1{Status: engine.VALID}},
	}, cometStatusNotCatchingUp())
	err := app.readinessChecksPhases(ctx, committed, readinessProbeConfig{
		TrustedPeerStatusURL: "http://127.0.0.1:1/status",
		HeadLagMaxBlocks:     2,
	})
	if !asProbeError(err, errKindTrustedPeerRequest) {
		t.Fatalf("want trusted_peer_request probe error, got %v", err)
	}
}

func TestRunReadinessChecks_invalidTrustedPeerHeadersJSON(t *testing.T) {
	peer := httptest.NewServer(http.HandlerFunc(mockCometStatus(false, "100")))
	defer peer.Close()

	ctx := t.Context()
	committed := evmtypes.NewBlockState(common.Hash{1}, 100, 0)
	app := testReadinessApp(&readinessEngineClientStub{
		fcResp: engine.ForkChoiceResponse{PayloadStatus: engine.PayloadStatusV1{Status: engine.VALID}},
	}, cometStatusNotCatchingUp())
	err := app.readinessChecksPhases(ctx, committed, readinessProbeConfig{
		TrustedPeerStatusURL:   peer.URL + "/status",
		TrustedPeerHeadersJSON: `not-json`,
		HeadLagMaxBlocks:       2,
	})
	if !asProbeError(err, errKindTrustedPeerConfig) {
		t.Fatalf("want trusted_peer_config probe error, got %v", err)
	}
}

func TestRunReadinessChecks_trustedPeerNumericHeightWithoutQuotes(t *testing.T) {
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/status" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintf(w, `{"result":{"sync_info":{"catching_up":false,"latest_block_height":101}}}`)
	}))
	defer peer.Close()

	ctx := t.Context()
	committed := evmtypes.NewBlockState(common.Hash{1}, 100, 0)
	app := testReadinessApp(&readinessEngineClientStub{
		fcResp: engine.ForkChoiceResponse{PayloadStatus: engine.PayloadStatusV1{Status: engine.VALID}},
	}, cometStatusNotCatchingUp())
	err := app.readinessChecksPhases(ctx, committed, readinessProbeConfig{
		TrustedPeerStatusURL: peer.URL + "/status",
		HeadLagMaxBlocks:     2,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRunReadinessChecks_cometStatusRPCError(t *testing.T) {
	ctx := t.Context()
	committed := evmtypes.NewBlockState(common.Hash{1}, 100, 0)
	app := testReadinessApp(&readinessEngineClientStub{
		fcResp: engine.ForkChoiceResponse{PayloadStatus: engine.PayloadStatusV1{Status: engine.VALID}},
	}, func(context.Context) (*coretypes.ResultStatus, error) {
		return nil, fmt.Errorf("rpc down")
	})
	err := app.readinessChecksPhases(ctx, committed, readinessProbeConfig{HeadLagMaxBlocks: 2})
	if !asProbeError(err, errKindCosmos) {
		t.Fatalf("want cosmos probe error, got %v", err)
	}
}

func TestRunReadinessChecks_cometClientNotConfigured(t *testing.T) {
	ctx := t.Context()
	committed := evmtypes.NewBlockState(common.Hash{1}, 100, 0)
	app := testReadinessApp(&readinessEngineClientStub{
		fcResp: engine.ForkChoiceResponse{PayloadStatus: engine.PayloadStatusV1{Status: engine.VALID}},
	}, nil)
	err := app.readinessChecksPhases(ctx, committed, readinessProbeConfig{HeadLagMaxBlocks: 2})
	if !asProbeError(err, errKindCosmos) {
		t.Fatalf("want cosmos probe error, got %v", err)
	}
}

func TestWriteReadinessResult_ok(t *testing.T) {
	rec := httptest.NewRecorder()
	writeReadinessResult(rec, log.NewNopLogger(), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if got := rec.Body.String(); got != readinessBodyOK {
		t.Fatalf("body %q want %q", got, readinessBodyOK)
	}
}

func TestWriteReadinessResult_unhealthy_probeError(t *testing.T) {
	rec := httptest.NewRecorder()
	err := &readinessProbeError{kind: errKindHeadLag, err: fmt.Errorf("lag detail")}
	writeReadinessResult(rec, log.NewNopLogger(), err)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d want 503", rec.Code)
	}
	if got := rec.Body.String(); got != readinessBodyUnhealthy {
		t.Fatalf("body %q want %q", got, readinessBodyUnhealthy)
	}
}

func TestWriteReadinessResult_unhealthy_internal(t *testing.T) {
	rec := httptest.NewRecorder()
	writeReadinessResult(rec, log.NewNopLogger(), fmt.Errorf("internal detail"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d want 500", rec.Code)
	}
	if got := rec.Body.String(); got != readinessBodyUnhealthy {
		t.Fatalf("body %q want %q", got, readinessBodyUnhealthy)
	}
}

func TestWriteReadinessResult_unhealthy_wrappedProbeError(t *testing.T) {
	rec := httptest.NewRecorder()
	inner := &readinessProbeError{kind: errKindCosmos, err: fmt.Errorf("catching up")}
	wrapped := fmt.Errorf("wrap: %w", inner)
	writeReadinessResult(rec, log.NewNopLogger(), wrapped)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d want 503 for errors.As wrapped probe error", rec.Code)
	}
}

func mockCometStatus(catchingUp bool, latestBlockHeight string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/status" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintf(w, `{"result":{"sync_info":{"catching_up":%t,"latest_block_height":%q}}}`,
			catchingUp, latestBlockHeight)
	}
}

func asProbeError(err error, want readinessErrorKind) bool {
	var pe *readinessProbeError
	if !errors.As(err, &pe) {
		return false
	}
	return pe.kind == want
}

func TestAsProbeError_recognizesWrappedError(t *testing.T) {
	inner := &readinessProbeError{kind: errKindReth, err: fmt.Errorf("detail")}
	wrapped := fmt.Errorf("outer: %w", inner)
	if !asProbeError(wrapped, errKindReth) {
		t.Fatal("errors.As should find readinessProbeError through fmt.Errorf %%w")
	}
}
