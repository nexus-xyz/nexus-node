package ante_test

import (
	"testing"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	sdktestutil "github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	protov2 "google.golang.org/protobuf/proto"

	"nexus/x/globalfee/ante"
	"nexus/x/globalfee/keeper"
	"nexus/x/globalfee/types"

	"github.com/cosmos/cosmos-sdk/runtime"
)

// mockFeeTx implements sdk.FeeTx for testing.
type mockFeeTx struct {
	gas  uint64
	fees sdk.Coins
}

func (m mockFeeTx) GetMsgs() []sdk.Msg                    { return nil }
func (m mockFeeTx) GetMsgsV2() ([]protov2.Message, error) { return nil, nil }
func (m mockFeeTx) GetGas() uint64                        { return m.gas }
func (m mockFeeTx) GetFee() sdk.Coins                     { return m.fees }
func (m mockFeeTx) FeePayer() []byte                      { return nil }
func (m mockFeeTx) FeeGranter() []byte                    { return nil }

func setupKeeper(t *testing.T) (keeper.Keeper, sdk.Context) {
	t.Helper()
	storeKey := storetypes.NewKVStoreKey(types.ModuleName)
	ctx := sdktestutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient")).Ctx
	k := keeper.NewKeeper(runtime.NewKVStoreService(storeKey))
	return k, ctx
}

func TestParamsValidate(t *testing.T) {
	t.Run("rejects wrong denom", func(t *testing.T) {
		p := types.Params{MinimumGasPrice: sdk.NewDecCoinFromDec("wrongdenom", math.LegacyMustNewDecFromStr("0.25"))}
		require.ErrorContains(t, p.Validate(), "denom must be")
	})

	t.Run("accepts correct denom", func(t *testing.T) {
		p := types.Params{
			MinimumGasPrice: sdk.NewDecCoinFromDec(types.ChainDenom, math.LegacyMustNewDecFromStr("0.25")),
		}
		require.NoError(t, p.Validate())
	})
}

func TestMinGasPriceDecorator(t *testing.T) {
	k, ctx := setupKeeper(t)

	price := sdk.NewDecCoinFromDec(sdk.DefaultBondDenom, math.LegacyMustNewDecFromStr("0.25"))
	require.NoError(t, k.SetParams(ctx, types.Params{MinimumGasPrice: price}))

	dec := ante.NewMinGasPriceDecorator(k)
	noop := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) { return ctx, nil }

	t.Run("passes when fee covers minimum", func(t *testing.T) {
		// gas=100, price=0.25 => required=ceil(25)=25 atnex
		tx := mockFeeTx{gas: 100, fees: sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 25))}
		_, err := dec.AnteHandle(ctx, tx, false, noop)
		require.NoError(t, err)
	})

	t.Run("passes when fee exceeds minimum", func(t *testing.T) {
		tx := mockFeeTx{gas: 100, fees: sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 100))}
		_, err := dec.AnteHandle(ctx, tx, false, noop)
		require.NoError(t, err)
	})

	t.Run("rejects when fee is insufficient", func(t *testing.T) {
		tx := mockFeeTx{gas: 100, fees: sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 24))}
		_, err := dec.AnteHandle(ctx, tx, false, noop)
		require.ErrorContains(t, err, "insufficient fees")
	})

	t.Run("rejects when fee is zero and price is set", func(t *testing.T) {
		tx := mockFeeTx{gas: 100, fees: sdk.Coins{}}
		_, err := dec.AnteHandle(ctx, tx, false, noop)
		require.ErrorContains(t, err, "insufficient fees")
	})

	t.Run("passes when price is zero (default)", func(t *testing.T) {
		k2, ctx2 := setupKeeper(t)
		dec2 := ante.NewMinGasPriceDecorator(k2)
		tx := mockFeeTx{gas: 100, fees: sdk.Coins{}}
		_, err := dec2.AnteHandle(ctx2, tx, false, noop)
		require.NoError(t, err)
	})

	t.Run("skips check in simulate mode", func(t *testing.T) {
		tx := mockFeeTx{gas: 100, fees: sdk.Coins{}}
		_, err := dec.AnteHandle(ctx, tx, true, noop)
		require.NoError(t, err)
	})
}
