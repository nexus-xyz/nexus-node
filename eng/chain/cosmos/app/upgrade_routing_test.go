package app

import (
	"errors"
	"os"
	"testing"
	"time"

	"cosmossdk.io/core/header"
	"cosmossdk.io/log"
	upgradekeeper "cosmossdk.io/x/upgrade/keeper"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/stretchr/testify/require"

	"nexus/x/evm/tests/testutil"
)

// setupUpgradeRoutingApp builds an App suitable for exercising the x/upgrade
// MsgServer against the governance authority configured in app_config.go.
func setupUpgradeRoutingApp(t *testing.T) (*App, sdk.Context) {
	t.Helper()
	testutil.SetupJWT(t)

	app := New(
		log.NewLogger(os.Stdout),
		dbm.NewMemDB(),
		nil,
		true,
		EmptyAppOptions{},
		baseapp.SetChainID("test-chain"),
	)

	const height int64 = 10
	blockTime := time.Unix(1_700_000_000, 0).UTC()
	ctx := app.BaseApp.NewUncachedContext(false, tmproto.Header{
		Height:  height,
		ChainID: "test-chain",
		Time:    blockTime,
	})
	ctx = ctx.
		WithExecMode(sdk.ExecModeFinalize).
		WithHeaderInfo(header.Info{
			Height:  height,
			Time:    blockTime,
			ChainID: "test-chain",
		})

	return app, ctx
}

// govAuthority is the module address used by x/gov to sign passed proposals.
// x/upgrade is configured with govtypes.ModuleName as its authority, so this
// address is the only one the MsgServer will accept. Must stay a function:
// a package-level var would bech32-encode before config.go's init() seals the
// "nexus" prefix, producing a "cosmos"-prefixed string the bank keeper rejects.
func govAuthority() string {
	return authtypes.NewModuleAddress(govtypes.ModuleName).String()
}

func TestUpgradeRouting_SoftwareUpgradeFromGovSchedulesPlan(t *testing.T) {
	app, ctx := setupUpgradeRoutingApp(t)
	msgSrv := upgradekeeper.NewMsgServerImpl(app.UpgradeKeeper)

	plan := upgradetypes.Plan{
		Name:   "halt/test",
		Height: ctx.HeaderInfo().Height + 100,
		Info:   "coordinated halt",
	}

	_, err := msgSrv.SoftwareUpgrade(ctx, &upgradetypes.MsgSoftwareUpgrade{
		Authority: govAuthority(),
		Plan:      plan,
	})
	require.NoError(t, err)

	stored, err := app.UpgradeKeeper.GetUpgradePlan(ctx)
	require.NoError(t, err)
	require.Equal(t, plan.Name, stored.Name)
	require.Equal(t, plan.Height, stored.Height)
	require.Equal(t, plan.Info, stored.Info)
}

func TestUpgradeRouting_CancelUpgradeFromGovClearsPlan(t *testing.T) {
	app, ctx := setupUpgradeRoutingApp(t)
	msgSrv := upgradekeeper.NewMsgServerImpl(app.UpgradeKeeper)

	plan := upgradetypes.Plan{
		Name:   "halt/test",
		Height: ctx.HeaderInfo().Height + 100,
		Info:   "coordinated halt",
	}
	require.NoError(t, app.UpgradeKeeper.ScheduleUpgrade(ctx, plan))

	_, err := msgSrv.CancelUpgrade(ctx, &upgradetypes.MsgCancelUpgrade{
		Authority: govAuthority(),
	})
	require.NoError(t, err)

	_, err = app.UpgradeKeeper.GetUpgradePlan(ctx)
	require.Error(t, err)
	require.True(t, errors.Is(err, upgradetypes.ErrNoUpgradePlanFound),
		"expected ErrNoUpgradePlanFound, got %v", err)
}

func TestUpgradeRouting_RejectsUnauthorizedSender(t *testing.T) {
	app, ctx := setupUpgradeRoutingApp(t)
	msgSrv := upgradekeeper.NewMsgServerImpl(app.UpgradeKeeper)

	// authtypes module address is a valid bech32 address but is not the
	// upgrade module's configured authority (x/gov is).
	unauthorized := authtypes.NewModuleAddress(authtypes.ModuleName).String()

	_, err := msgSrv.SoftwareUpgrade(ctx, &upgradetypes.MsgSoftwareUpgrade{
		Authority: unauthorized,
		Plan: upgradetypes.Plan{
			Name:   "halt/unauthorized",
			Height: ctx.HeaderInfo().Height + 100,
			Info:   "should be rejected",
		},
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, upgradetypes.ErrInvalidSigner),
		"expected ErrInvalidSigner, got %v", err)

	_, err = app.UpgradeKeeper.GetUpgradePlan(ctx)
	require.True(t, errors.Is(err, upgradetypes.ErrNoUpgradePlanFound),
		"unauthorized call must not store a plan; got err=%v", err)
}

func TestUpgradeRouting_ValidateMsgAllowed(t *testing.T) {
	app, _ := setupUpgradeRoutingApp(t)

	buildTx := func(msgs ...sdk.Msg) sdk.Tx {
		builder := app.TxConfig().NewTxBuilder()
		require.NoError(t, builder.SetMsgs(msgs...))
		return builder.GetTx()
	}

	upgradeTx := buildTx(&upgradetypes.MsgSoftwareUpgrade{
		Plan: upgradetypes.Plan{Name: "halt/test", Height: 100, Info: "test halt"},
	})
	require.NoError(t, validateMsgAllowed(upgradeTx),
		"MsgSoftwareUpgrade must be permitted by validateMsgAllowed")

	cancelTx := buildTx(&upgradetypes.MsgCancelUpgrade{})
	require.NoError(t, validateMsgAllowed(cancelTx),
		"MsgCancelUpgrade must be permitted by validateMsgAllowed")

	// MsgMultiSend is not in the allowlist (only MsgSend is). Pins the contract
	// closed: if the allowlist in proposal.go is accidentally broadened, this fails.
	disallowedTx := buildTx(&banktypes.MsgMultiSend{})
	require.Error(t, validateMsgAllowed(disallowedTx),
		"MsgMultiSend must NOT be permitted by validateMsgAllowed")

	// Multi-message tx must be rejected even when each message is individually
	// allowed. Pins the "exactly one message per tx" invariant in proposal.go.
	multiTx := buildTx(
		&upgradetypes.MsgSoftwareUpgrade{Plan: upgradetypes.Plan{Name: "halt/test", Height: 100}},
		&banktypes.MsgSend{},
	)
	require.Error(t, validateMsgAllowed(multiTx),
		"multi-message tx must be rejected even when each message is individually allowed")
}

func TestUpgradeRouting_RejectsPastHeightUpgrade(t *testing.T) {
	app, ctx := setupUpgradeRoutingApp(t)
	msgSrv := upgradekeeper.NewMsgServerImpl(app.UpgradeKeeper)

	// SDK's ScheduleUpgrade rejects plans strictly in the past.
	// Same-height is intentionally allowed for emergency hard-fork recovery.
	_, err := msgSrv.SoftwareUpgrade(ctx, &upgradetypes.MsgSoftwareUpgrade{
		Authority: govAuthority(),
		Plan: upgradetypes.Plan{
			Name:   "halt/past",
			Height: ctx.HeaderInfo().Height - 1,
			Info:   "must be rejected",
		},
	})
	require.Error(t, err, "past-height upgrade must be rejected")

	_, err = app.UpgradeKeeper.GetUpgradePlan(ctx)
	require.True(t, errors.Is(err, upgradetypes.ErrNoUpgradePlanFound),
		"past-height call must not store a plan; got err=%v", err)
}
