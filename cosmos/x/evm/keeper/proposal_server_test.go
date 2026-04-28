package keeper_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/runtime"
	sdktestutil "github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	nexus "nexus/app/types"
	"nexus/x/evm/keeper"
	module "nexus/x/evm/module"
	"nexus/x/evm/tests/testutil"
	evmtypes "nexus/x/evm/types"
)

func TestProposalMsgServer_ExecutionPayload(t *testing.T) {
	testutil.SetupJWT(t)

	encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
	addressCodec := addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix())
	storeKey := storetypes.NewKVStoreKey(evmtypes.StoreKey)
	storeService := runtime.NewKVStoreService(storeKey)

	sdkCtx := sdktestutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_test")).Ctx
	header := sdkCtx.BlockHeader()
	header.Height = int64(testutil.DefaultStateTimestamp + 1)
	sdkCtx = sdkCtx.WithBlockHeader(header)
	// Set BlockTime to match the expected timestamp for legacy calculation
	expectedTimestamp := testutil.DefaultStateTimestamp + 1
	sdkCtx = sdkCtx.WithBlockTime(time.Unix(int64(expectedTimestamp), 0))

	authority := authtypes.NewModuleAddress(evmtypes.GovModuleName)

	k := keeper.NewKeeper(
		storeService,
		encCfg.Codec,
		addressCodec,
		authority,
		encCfg.TxConfig,
		nexus.ChainSpec{},
	)

	_ = k.Params.Set(sdkCtx, evmtypes.DefaultParams())
	_ = k.SetBlockState(
		sdkCtx,
		evmtypes.NewBlockState(
			testutil.DefaultStateHash,
			testutil.DefaultStateHeight,
			testutil.DefaultStateTimestamp,
		),
	)

	// Build a router and register proposal handlers
	router := baseapp.NewMsgServiceRouter()
	router.SetInterfaceRegistry(encCfg.InterfaceRegistry)
	k.RegisterProposalHandlers(router)

	handler := router.Handler(&evmtypes.MsgExecutionPayload{})

	t.Run("successful validation", func(t *testing.T) {
		req := &evmtypes.MsgExecutionPayload{
			Authority:        authtypes.NewModuleAddress(evmtypes.ModuleName).String(),
			ExecutionPayload: []byte(testutil.BuildPayloadString()),
		}

		resp, err := handler(sdkCtx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)
	})

	t.Run("validation errors", func(t *testing.T) {
		testCases := []struct {
			name          string
			req           *evmtypes.MsgExecutionPayload
			expectedError string
		}{
			{
				name: "empty payload",
				req: &evmtypes.MsgExecutionPayload{
					Authority:        authtypes.NewModuleAddress(evmtypes.ModuleName).String(),
					ExecutionPayload: []byte{},
				},
				expectedError: "unexpected end of JSON input",
			},
			{
				name: "invalid authority",
				req: &evmtypes.MsgExecutionPayload{
					Authority:        "invalid-authority",
					ExecutionPayload: []byte(testutil.BuildPayloadString()),
				},
				expectedError: "invalid authority",
			},
			{
				name: "invalid JSON",
				req: &evmtypes.MsgExecutionPayload{
					Authority:        authtypes.NewModuleAddress(evmtypes.ModuleName).String(),
					ExecutionPayload: []byte(`invalid json`),
				},
				expectedError: "invalid execution payload",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				_, err := handler(sdkCtx, tc.req)
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.expectedError)
			})
		}
	})
}

func TestProposalMsgServer_UpdateParams(t *testing.T) {
	k := keeper.Keeper{}
	// Register proposal handlers on a router
	encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
	router := baseapp.NewMsgServiceRouter()
	router.SetInterfaceRegistry(encCfg.InterfaceRegistry)
	k.RegisterProposalHandlers(router)

	handler := router.Handler(&evmtypes.MsgUpdateParams{})
	sdkCtx := sdk.Context{}

	req := &evmtypes.MsgUpdateParams{
		Authority: authtypes.NewModuleAddress(evmtypes.GovModuleName).String(),
		Params:    evmtypes.DefaultParams(),
	}

	_, err := handler(sdkCtx, req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "UpdateParams should not be called during ProcessProposal")
}
