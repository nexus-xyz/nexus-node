package app

import (
	"errors"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"

	evmtypes "nexus/x/evm/types"
	globalfeeante "nexus/x/globalfee/ante"
	globalfeekeeper "nexus/x/globalfee/keeper"
)

// newAnteHandler returns an AnteHandler that bypasses all checks for
// MsgExecutionPayload (unsigned and fee-free by design) and applies the
// standard SDK ante chain for all other message types.
func newAnteHandler(
	ak authkeeper.AccountKeeper, bk bankkeeper.Keeper, txConfig client.TxConfig, gfk globalfeekeeper.Keeper,
) sdk.AnteHandler {
	sdkAnteHandler, err := ante.NewAnteHandler(ante.HandlerOptions{
		AccountKeeper:   ak,
		BankKeeper:      bk,
		SignModeHandler: txConfig.SignModeHandler(),
		SigGasConsumer:  ante.DefaultSigVerificationGasConsumer,
	})
	if err != nil {
		panic(err)
	}

	minGasPrices := globalfeeante.NewMinGasPriceDecorator(gfk)

	return func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
		msgs := tx.GetMsgs()
		if len(msgs) == 1 {
			if _, ok := msgs[0].(*evmtypes.MsgExecutionPayload); ok {
				// EVM payloads are injected by PrepareProposal and must never
				// arrive via the mempool. Reject at CheckTx to prevent spam.
				if ctx.IsCheckTx() || ctx.IsReCheckTx() {
					return ctx, errors.New("MsgExecutionPayload cannot be submitted directly")
				}
				return ctx, nil
			}
		}
		ctx, err := minGasPrices.AnteHandle(ctx, tx, simulate, sdkAnteHandler)
		return ctx, err
	}
}
