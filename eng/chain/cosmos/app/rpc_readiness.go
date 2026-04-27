package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	coretypes "github.com/cometbft/cometbft/rpc/core/types"

	"cosmossdk.io/collections"
	"cosmossdk.io/log"
	"github.com/gorilla/mux"

	evmtypes "nexus/x/evm/types"
)

// HTTP readiness probe (Kubernetes readinessProbe target) at GET /readyz.
//
// Semantics:
//   - Read committed execution head from application state (x/evm BlockState).
//   - Require local Comet sync state reports catching_up=false. That flag is
//     consensus.Reactor.WaitSync(); we read it via Client.Status (same as JSON /status),
//     which for an in-process node uses local.Client and does not hit HTTP.
//   - Require the execution client (Engine API) to accept that head via
//     engine_forkchoiceUpdatedV3 with nil payload attributes (VALID).
//   - Optional head-lag: only when TRUSTED_PEER_STATUS_URL is non-empty after trimming
//     whitespace, compare that peer's Comet latest_block_height to local BlockState.Height.
//     If the URL is unset or blank, lag validation is skipped (no error, not unhealthy).
//     When enabled, HEAD_LAG_MAX_BLOCKS defaults to 2; unhealthy only if
//     abs(peer_height-local_height) > maxLag (equal is still healthy).
//
// HTTP: responds with plain text "ok" or "unhealthy" only; failure details are logged
// (stages include query_context, committed_state, cosmos, reth, head_lag, trusted_peer_*).
//
// Env (read once when API routes register; restart process to pick up changes):
//   - TRUSTED_PEER_STATUS_URL: optional; if empty, head-lag check is not run
//   - TRUSTED_PEER_STATUS_HEADERS: optional JSON object of HTTP headers for the peer request
//   - HEAD_LAG_MAX_BLOCKS: optional; default 2 when head-lag is enabled (invalid env falls back to 2)

const (
	readinessHTTPPath       = "/readyz"
	readinessRequestTimeout = 12 * time.Second

	readinessBodyOK        = "ok"
	readinessBodyUnhealthy = "unhealthy"
)

// readinessProbeConfig holds env-derived settings for GET /readyz (loaded once at API registration).
type readinessProbeConfig struct {
	TrustedPeerStatusURL   string // TrimSpace applied at load
	TrustedPeerHeadersJSON string
	HeadLagMaxBlocks       uint64
}

func loadReadinessProbeConfig() readinessProbeConfig {
	const defaultMaxLag = uint64(2)
	maxLag := defaultMaxLag
	s := strings.TrimSpace(os.Getenv("HEAD_LAG_MAX_BLOCKS"))
	if s != "" {
		if v, err := strconv.ParseUint(s, 10, 64); err == nil {
			maxLag = v
		}
	}
	return readinessProbeConfig{
		TrustedPeerStatusURL:   strings.TrimSpace(os.Getenv("TRUSTED_PEER_STATUS_URL")),
		TrustedPeerHeadersJSON: os.Getenv("TRUSTED_PEER_STATUS_HEADERS"),
		HeadLagMaxBlocks:       maxLag,
	}
}

func registerReadinessHandlers(r *mux.Router, app *App) {
	r.HandleFunc(readinessHTTPPath, app.readinessHTTPHandler).Methods(http.MethodGet)
}

func (app *App) readinessHTTPHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), readinessRequestTimeout)
	defer cancel()

	logger := app.Logger().With("module", "readiness", "path", readinessHTTPPath)
	writeReadinessResult(w, logger, app.runReadinessChecks(ctx))
}

func writeReadinessResult(w http.ResponseWriter, logger log.Logger, err error) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if err == nil {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, readinessBodyOK)
		return
	}
	logReadinessFailure(logger, err)
	w.WriteHeader(readinessFailureHTTPStatus(err))
	_, _ = io.WriteString(w, readinessBodyUnhealthy)
}

func logReadinessFailure(logger log.Logger, err error) {
	var re *readinessProbeError
	if errors.As(err, &re) {
		logger.Error("readiness probe failed", "stage", string(re.kind), "err", re.err.Error())
		return
	}
	logger.Error("readiness probe failed", "stage", "internal", "err", err.Error())
}

func readinessFailureHTTPStatus(err error) int {
	var re *readinessProbeError
	if errors.As(err, &re) {
		return http.StatusServiceUnavailable
	}
	return http.StatusInternalServerError
}

// readinessProbeError tags which readiness stage failed (for logs and classification).
type readinessProbeError struct {
	kind readinessErrorKind
	err  error
}

func (e *readinessProbeError) Error() string { return string(e.kind) + ": " + e.err.Error() }

func (e *readinessProbeError) Unwrap() error { return e.err }

type readinessErrorKind string

const (
	errKindCommittedState readinessErrorKind = "committed_state"
	errKindQueryContext   readinessErrorKind = "query_context"
	errKindCosmos         readinessErrorKind = "cosmos"
	errKindReth           readinessErrorKind = "reth"
	errKindHeadLag        readinessErrorKind = "head_lag"

	errKindTrustedPeerConfig   readinessErrorKind = "trusted_peer_config"
	errKindTrustedPeerRequest  readinessErrorKind = "trusted_peer_request"
	errKindTrustedPeerResponse readinessErrorKind = "trusted_peer_response"
)

func (app *App) runReadinessChecks(ctx context.Context) error {
	// Latest-height query (height 0) with checkHeader=false: avoids failures when the in-memory
	// check/finalize header is briefly ahead of or out of sync with the committed multistore version
	// (e.g. during ABCI finalize/commit). Same idea as BaseApp.RegisterGRPCServerWithSkipCheckHeader.
	qctx, err := app.BaseApp.CreateQueryContextWithCheckHeader(0, false, false)
	if err != nil {
		return &readinessProbeError{
			kind: errKindQueryContext,
			err:  fmt.Errorf("create query context: %w", err),
		}
	}
	committed, err := app.EvmKeeper.GetBlockState(qctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return &readinessProbeError{
				kind: errKindCommittedState,
				err:  fmt.Errorf("block state not initialized: %w", err),
			}
		}
		return fmt.Errorf("block state: %w", err)
	}
	return app.readinessChecksPhases(ctx, committed, app.readinessProbeConfig)
}

// readinessChecksPhases runs Comet sync, EL forkchoice, and optional peer lag for a known committed head.
// Tests call this with a synthetic committed state when BaseApp block state is not wired.
func (app *App) readinessChecksPhases(
	ctx context.Context, committed evmtypes.BlockState, cfg readinessProbeConfig,
) error {
	if err := checkLocalCometNotCatchingUp(ctx, app.readinessCometStatus); err != nil {
		return &readinessProbeError{kind: errKindCosmos, err: err}
	}
	if err := app.EvmKeeper.ProbeCommittedExecutionHead(ctx, committed); err != nil {
		return &readinessProbeError{kind: errKindReth, err: err}
	}
	if err := checkTrustedPeerLag(
		ctx, committed.Height, cfg.TrustedPeerStatusURL, cfg.TrustedPeerHeadersJSON, cfg.HeadLagMaxBlocks,
	); err != nil {
		return err
	}
	return nil
}

type cometStatusParsed struct {
	catchingUp        *bool
	latestBlockHeight *int64
}

func parseCometStatusJSON(b []byte) (cometStatusParsed, error) {
	var wrap struct {
		Result *struct {
			SyncInfo *struct {
				CatchingUp        *bool           `json:"catching_up"`
				LatestBlockHeight json.RawMessage `json:"latest_block_height"`
			} `json:"sync_info"`
		} `json:"result"`
		Error *json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(b, &wrap); err != nil {
		return cometStatusParsed{}, fmt.Errorf("json decode: %w", err)
	}
	if wrap.Error != nil {
		return cometStatusParsed{}, fmt.Errorf("rpc error: %s", string(*wrap.Error))
	}
	if wrap.Result == nil || wrap.Result.SyncInfo == nil {
		return cometStatusParsed{}, fmt.Errorf("missing sync_info")
	}
	si := wrap.Result.SyncInfo
	out := cometStatusParsed{catchingUp: si.CatchingUp}
	if len(si.LatestBlockHeight) > 0 {
		h, err := parseJSONInt64(si.LatestBlockHeight)
		if err != nil {
			return cometStatusParsed{}, fmt.Errorf("latest_block_height: %w", err)
		}
		out.latestBlockHeight = &h
	}
	return out, nil
}

func parseJSONInt64(raw json.RawMessage) (int64, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return 0, fmt.Errorf("empty")
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return 0, err
		}
		return strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	}
	var n int64
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, err
	}
	return n, nil
}

// checkLocalCometNotCatchingUp rejects when Status reports CatchingUp (WaitSync on the reactor).
func checkLocalCometNotCatchingUp(
	ctx context.Context,
	statusFn func(context.Context) (*coretypes.ResultStatus, error),
) error {
	if statusFn == nil {
		return fmt.Errorf("comet rpc client not configured for readiness")
	}
	st, err := statusFn(ctx)
	if err != nil {
		return fmt.Errorf("status: %w", err)
	}
	if st.SyncInfo.CatchingUp {
		return fmt.Errorf("catching_up=true")
	}
	return nil
}

// checkTrustedPeerLag runs only when peerStatusURL is non-empty after TrimSpace; otherwise
// it returns nil and readiness does not depend on lag.
func checkTrustedPeerLag(
	ctx context.Context,
	localCommittedHeight uint64,
	peerStatusURL, headersJSON string,
	maxLag uint64,
) error {
	if strings.TrimSpace(peerStatusURL) == "" {
		return nil
	}
	extra, err := parseOptionalJSONHeaders(headersJSON)
	if err != nil {
		return &readinessProbeError{kind: errKindTrustedPeerConfig, err: err}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, peerStatusURL, nil)
	if err != nil {
		return &readinessProbeError{kind: errKindTrustedPeerRequest, err: err}
	}
	for k, vals := range extra {
		for _, v := range vals {
			req.Header.Add(k, v)
		}
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return &readinessProbeError{kind: errKindTrustedPeerRequest, err: err}
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return &readinessProbeError{kind: errKindTrustedPeerRequest, err: err}
	}
	if resp.StatusCode != http.StatusOK {
		return &readinessProbeError{
			kind: errKindTrustedPeerResponse,
			err:  fmt.Errorf("http %d: %s", resp.StatusCode, truncateBytes(b, 200)),
		}
	}

	parsed, err := parseCometStatusJSON(b)
	if err != nil {
		return &readinessProbeError{
			kind: errKindTrustedPeerResponse,
			err:  fmt.Errorf("status body: %w", err),
		}
	}
	if parsed.latestBlockHeight == nil {
		return &readinessProbeError{
			kind: errKindTrustedPeerResponse,
			err:  fmt.Errorf("missing sync_info.latest_block_height"),
		}
	}
	peerH := *parsed.latestBlockHeight
	if peerH < 0 {
		return &readinessProbeError{
			kind: errKindTrustedPeerResponse,
			err:  fmt.Errorf("latest_block_height negative"),
		}
	}

	var diff uint64
	lh := int64(localCommittedHeight)
	if peerH >= lh {
		diff = uint64(peerH - lh)
	} else {
		diff = uint64(lh - peerH)
	}
	if diff > maxLag {
		return &readinessProbeError{
			kind: errKindHeadLag,
			err: fmt.Errorf("%d blocks apart (local_committed=%d peer=%d max_abs=%d)",
				diff, localCommittedHeight, peerH, maxLag),
		}
	}
	return nil
}

func parseOptionalJSONHeaders(raw string) (http.Header, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, fmt.Errorf("TRUSTED_PEER_STATUS_HEADERS must be JSON object: %w", err)
	}
	h := make(http.Header)
	for k, v := range m {
		h.Set(k, v)
	}
	return h, nil
}

func truncateBytes(b []byte, n int) string {
	s := string(b)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
