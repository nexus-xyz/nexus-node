package app

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"nexus/x/evm/tests/testutil"

	"cosmossdk.io/core/header"
	"cosmossdk.io/log"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/stretchr/testify/require"
)

// setupHaltLoggingApp builds an App with a buffered logger so tests can
// capture halt_triggered log output.
func setupHaltLoggingApp(t *testing.T) (*App, sdk.Context, *bytes.Buffer) {
	t.Helper()
	testutil.SetupJWT(t)

	buf := &bytes.Buffer{}
	logger := log.NewLogger(buf)

	app := New(
		logger,
		dbm.NewMemDB(),
		nil,
		true,
		EmptyAppOptions{},
		baseapp.SetChainID("test-chain"),
	)

	const height int64 = 10
	blockTime := time.Unix(1_700_000_000, 0).UTC()
	tmHeader := tmproto.Header{
		Height:  height,
		ChainID: "test-chain",
		Time:    blockTime,
	}
	ctx := app.BaseApp.NewUncachedContext(false, tmHeader)
	ctx = ctx.
		WithExecMode(sdk.ExecModeFinalize).
		WithLogger(logger).
		WithHeaderInfo(header.Info{
			Height:  height,
			Time:    blockTime,
			ChainID: "test-chain",
		})

	return app, ctx, buf
}

func extractHaltLog(t *testing.T, buf *bytes.Buffer) (haltLog, bool) {
	t.Helper()
	for _, line := range strings.Split(buf.String(), "\n") {
		idx := strings.Index(line, `{"event":"halt_triggered"`)
		if idx < 0 {
			continue
		}
		end := strings.Index(line[idx:], "}")
		if end < 0 {
			continue
		}
		var out haltLog
		require.NoError(t, json.Unmarshal([]byte(line[idx:idx+end+1]), &out))
		return out, true
	}
	return haltLog{}, false
}

func TestLogHaltIfTriggered_EmitsOnHalt(t *testing.T) {
	app, ctx, buf := setupHaltLoggingApp(t)

	plan := upgradetypes.Plan{
		Name:   "halt/test",
		Height: ctx.HeaderInfo().Height,
		Info:   "emergency halt: state integrity risk",
	}
	require.NoError(t, app.UpgradeKeeper.ScheduleUpgrade(ctx, plan))

	logHaltIfTriggered(ctx, app.UpgradeKeeper)

	got, ok := extractHaltLog(t, buf)
	require.True(t, ok, "expected halt_triggered log line; got:\n%s", buf.String())
	require.Equal(t, haltEventName, got.Event)
	require.Equal(t, plan.Name, got.PlanName)
	require.Equal(t, ctx.HeaderInfo().Height, got.Height)
	require.Equal(t, plan.Info, got.Info)
	require.Equal(t, ctx.BlockTime().UTC().Format(time.RFC3339Nano), got.Timestamp)
}

func TestLogHaltIfTriggered_SkipsWhenNoPlan(t *testing.T) {
	app, ctx, buf := setupHaltLoggingApp(t)

	logHaltIfTriggered(ctx, app.UpgradeKeeper)

	_, ok := extractHaltLog(t, buf)
	require.False(t, ok, "expected no halt_triggered log without a scheduled plan; got:\n%s", buf.String())
}

func TestLogHaltIfTriggered_SkipsBeforeHaltHeight(t *testing.T) {
	app, ctx, buf := setupHaltLoggingApp(t)

	plan := upgradetypes.Plan{
		Name:   "halt/future",
		Height: ctx.HeaderInfo().Height + 100,
		Info:   "future halt",
	}
	require.NoError(t, app.UpgradeKeeper.ScheduleUpgrade(ctx, plan))

	logHaltIfTriggered(ctx, app.UpgradeKeeper)

	_, ok := extractHaltLog(t, buf)
	require.False(t, ok, "expected no halt_triggered log before plan height; got:\n%s", buf.String())
}

func TestLogHaltIfTriggered_SkipsWhenHandlerRegistered(t *testing.T) {
	app, ctx, buf := setupHaltLoggingApp(t)

	plan := upgradetypes.Plan{
		Name:   "v1.0.1",
		Height: ctx.HeaderInfo().Height,
		Info:   "routine upgrade",
	}
	require.NoError(t, app.UpgradeKeeper.ScheduleUpgrade(ctx, plan))
	app.UpgradeKeeper.SetUpgradeHandler(
		plan.Name,
		func(_ context.Context, _ upgradetypes.Plan, vm module.VersionMap) (module.VersionMap, error) {
			return vm, nil
		},
	)

	logHaltIfTriggered(ctx, app.UpgradeKeeper)

	_, ok := extractHaltLog(t, buf)
	require.False(t, ok, "expected no halt_triggered log when handler is registered; got:\n%s", buf.String())
}
