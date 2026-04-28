package app

import (
	"bytes"
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
	"github.com/stretchr/testify/require"
)

// setupUpgradeLoggingApp builds an App with a buffered logger so tests can
// capture upgrade_scheduled log output.
func setupUpgradeLoggingApp(t *testing.T) (*App, sdk.Context, *bytes.Buffer) {
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

func extractUpgradeLog(t *testing.T, buf *bytes.Buffer) (upgradeLog, bool) {
	t.Helper()
	for _, line := range strings.Split(buf.String(), "\n") {
		idx := strings.Index(line, `{"event":"upgrade_scheduled"`)
		if idx < 0 {
			continue
		}
		end := strings.Index(line[idx:], "}")
		if end < 0 {
			continue
		}
		var out upgradeLog
		require.NoError(t, json.Unmarshal([]byte(line[idx:idx+end+1]), &out))
		return out, true
	}
	return upgradeLog{}, false
}

func TestUpgradeNotifier_LogsNewlyScheduledPlan(t *testing.T) {
	app, ctx, buf := setupUpgradeLoggingApp(t)

	plan := upgradetypes.Plan{
		Name:   "v2-upgrade",
		Height: 999,
		Info:   "New version available!",
	}
	require.NoError(t, app.UpgradeKeeper.ScheduleUpgrade(ctx, plan))

	n := newUpgradeNotifier(app.UpgradeKeeper)
	n.MaybeLogNewPlan(ctx)

	got, ok := extractUpgradeLog(t, buf)
	require.True(t, ok, "expected upgrade_scheduled log; got:\n%s", buf.String())
	require.Equal(t, upgradeEventName, got.Event)
	require.Equal(t, plan.Name, got.Name)
	require.Equal(t, plan.Height, got.Height)
	require.Equal(t, plan.Info, got.Info)
}

func TestUpgradeNotifier_DoesNotRelogSamePlan(t *testing.T) {
	app, ctx, buf := setupUpgradeLoggingApp(t)

	plan := upgradetypes.Plan{
		Name:   "v2-upgrade",
		Height: 999,
		Info:   "New version available!",
	}
	require.NoError(t, app.UpgradeKeeper.ScheduleUpgrade(ctx, plan))

	n := newUpgradeNotifier(app.UpgradeKeeper)
	n.MaybeLogNewPlan(ctx)
	_, ok := extractUpgradeLog(t, buf)
	require.True(t, ok, "expected first emission")

	// Subsequent calls with the same stored plan must not re-emit — this is
	// what prevents the every-block log spam the struct exists to avoid.
	buf.Reset()
	n.MaybeLogNewPlan(ctx)
	_, ok = extractUpgradeLog(t, buf)
	require.False(t, ok, "expected no re-log for unchanged plan; got:\n%s", buf.String())
}

func TestUpgradeNotifier_NoLogWhenNoPlanStored(t *testing.T) {
	app, ctx, buf := setupUpgradeLoggingApp(t)

	n := newUpgradeNotifier(app.UpgradeKeeper)
	n.MaybeLogNewPlan(ctx)

	_, ok := extractUpgradeLog(t, buf)
	require.False(t, ok, "expected no upgrade_scheduled log without a scheduled plan; got:\n%s", buf.String())
}

func TestUpgradeNotifier_LogsAgainAfterCancelAndReschedule(t *testing.T) {
	app, ctx, buf := setupUpgradeLoggingApp(t)

	plan := upgradetypes.Plan{
		Name:   "v2-upgrade",
		Height: 999,
		Info:   "New version available!",
	}
	require.NoError(t, app.UpgradeKeeper.ScheduleUpgrade(ctx, plan))

	n := newUpgradeNotifier(app.UpgradeKeeper)
	n.MaybeLogNewPlan(ctx)
	_, ok := extractUpgradeLog(t, buf)
	require.True(t, ok, "expected first emission")

	require.NoError(t, app.UpgradeKeeper.ClearUpgradePlan(ctx))
	n.MaybeLogNewPlan(ctx)

	// Reschedule at a new height — must log again.
	buf.Reset()
	plan2 := upgradetypes.Plan{
		Name:   "v2-upgrade",
		Height: 1500,
		Info:   "New version available!",
	}
	require.NoError(t, app.UpgradeKeeper.ScheduleUpgrade(ctx, plan2))
	n.MaybeLogNewPlan(ctx)

	got, ok := extractUpgradeLog(t, buf)
	require.True(t, ok, "expected re-log after reschedule; got:\n%s", buf.String())
	require.Equal(t, int64(1500), got.Height)
}

func TestUpgradeNotifier_RelogsWhenInfoChanges(t *testing.T) {
	// Covers the same-block cancel + reschedule edge case: if a cancel and a
	// new MsgSoftwareUpgrade with the same (name, height) but updated info
	// land in the same block, the plan is never absent so forgetLastPlan is
	// not called. Dedup on info ensures the updated payload still re-emits.
	app, ctx, buf := setupUpgradeLoggingApp(t)

	plan := upgradetypes.Plan{
		Name:   "v2-upgrade",
		Height: 999,
		Info:   "https://releases.example.com/v2.0.0",
	}
	require.NoError(t, app.UpgradeKeeper.ScheduleUpgrade(ctx, plan))

	n := newUpgradeNotifier(app.UpgradeKeeper)
	n.MaybeLogNewPlan(ctx)
	_, ok := extractUpgradeLog(t, buf)
	require.True(t, ok, "expected first emission")

	// Replace the stored plan in place with the same (name, height) but a
	// different info field — simulates a same-block cancel + reschedule.
	buf.Reset()
	updated := upgradetypes.Plan{
		Name:   "v2-upgrade",
		Height: 999,
		Info:   "https://releases.example.com/v2.0.1-hotfix",
	}
	require.NoError(t, app.UpgradeKeeper.ScheduleUpgrade(ctx, updated))
	n.MaybeLogNewPlan(ctx)

	got, ok := extractUpgradeLog(t, buf)
	require.True(t, ok, "expected re-log when info changes; got:\n%s", buf.String())
	require.Equal(t, updated.Info, got.Info)
}

func TestUpgradeNotifier_SkipsAtHaltHeight(t *testing.T) {
	// On a restart at exactly the halt height, logHaltIfTriggered will emit
	// halt_triggered in the same PreBlocker. upgrade_scheduled must not fire
	// for the same block — it would produce a misleading or duplicate alert.
	app, ctx, buf := setupUpgradeLoggingApp(t)

	// Schedule the plan at the current context height (height = 10).
	plan := upgradetypes.Plan{
		Name:   "v3-upgrade",
		Height: ctx.HeaderInfo().Height,
		Info:   "halt height reached",
	}
	require.NoError(t, app.UpgradeKeeper.ScheduleUpgrade(ctx, plan))

	n := newUpgradeNotifier(app.UpgradeKeeper)
	n.MaybeLogNewPlan(ctx)

	_, ok := extractUpgradeLog(t, buf)
	require.False(t, ok, "expected no upgrade_scheduled log at halt height; got:\n%s", buf.String())
}

func TestUpgradeNotifier_NilSafe(t *testing.T) {
	// Must tolerate a nil receiver without panicking — this guards the
	// PreBlocker code path against an unset notifier during early init.
	var n *upgradeNotifier
	require.NotPanics(t, func() {
		n.MaybeLogNewPlan(sdk.Context{})
	})
}
