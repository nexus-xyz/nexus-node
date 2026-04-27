package ante

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"nexus/x/globalfee/keeper"
)

// MinGasPriceDecorator enforces a minimum gas price stored on-chain.
// Unlike the SDK's MinGasPricesDecorator, this runs in both CheckTx and
// FinalizeBlock, making it a consensus rule.
type MinGasPriceDecorator struct {
	keeper keeper.Keeper
}

func NewMinGasPriceDecorator(k keeper.Keeper) MinGasPriceDecorator {
	return MinGasPriceDecorator{keeper: k}
}

func (d MinGasPriceDecorator) AnteHandle(
	ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler,
) (sdk.Context, error) {
	if simulate {
		return next(ctx, tx, simulate)
	}

	minPrice := d.keeper.GetParams(ctx).MinimumGasPrice
	if minPrice.IsZero() {
		return next(ctx, tx, simulate)
	}

	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return ctx, fmt.Errorf("tx must implement sdk.FeeTx")
	}

	gas := feeTx.GetGas()
	required := minPrice.Amount.MulInt64(int64(gas)).Ceil().TruncateInt()
	paid := feeTx.GetFee().AmountOf(minPrice.Denom)

	if paid.LT(required) {
		return ctx, fmt.Errorf("insufficient fees: got %s%s, required %s%s (gas %d × price %s)",
			paid, minPrice.Denom, required, minPrice.Denom, gas, minPrice.Amount)
	}

	return next(ctx, tx, simulate)
}
