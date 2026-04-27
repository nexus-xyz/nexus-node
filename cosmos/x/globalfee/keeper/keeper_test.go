package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	sdktestutil "github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"nexus/x/globalfee/keeper"
	"nexus/x/globalfee/types"
)

func setup(t *testing.T) (keeper.Keeper, sdk.Context) {
	t.Helper()
	storeKey := storetypes.NewKVStoreKey(types.ModuleName)
	ctx := sdktestutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient")).Ctx
	return keeper.NewKeeper(runtime.NewKVStoreService(storeKey)), ctx
}

func TestGetParams_Default(t *testing.T) {
	k, ctx := setup(t)
	p := k.GetParams(ctx)
	require.Equal(t, types.DefaultParams(), p)
}

func TestSetGetParams_Roundtrip(t *testing.T) {
	k, ctx := setup(t)
	want := types.Params{
		MinimumGasPrice: sdk.NewDecCoinFromDec(types.ChainDenom, math.LegacyMustNewDecFromStr("0.25")),
	}
	require.NoError(t, k.SetParams(ctx, want))
	require.Equal(t, want, k.GetParams(ctx))
}
