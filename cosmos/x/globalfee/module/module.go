package globalfee

import (
	"encoding/json"

	"cosmossdk.io/core/appmodule"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"google.golang.org/grpc"

	"nexus/x/globalfee/keeper"
	"nexus/x/globalfee/types"
)

var (
	_ module.AppModuleBasic = (*AppModule)(nil)
	_ module.AppModule      = (*AppModule)(nil)
	_ module.HasGenesis     = (*AppModule)(nil)
	_ appmodule.AppModule   = (*AppModule)(nil)
)

type AppModule struct {
	keeper keeper.Keeper
}

func NewAppModule(k keeper.Keeper) AppModule {
	return AppModule{keeper: k}
}

func (AppModule) IsAppModule() {}

func (AppModule) Name() string { return types.ModuleName }

func (AppModule) RegisterLegacyAminoCodec(*codec.LegacyAmino) {}

func (AppModule) RegisterInterfaces(codectypes.InterfaceRegistry) {}

func (AppModule) RegisterGRPCGatewayRoutes(client.Context, *runtime.ServeMux) {}

func (AppModule) RegisterServices(grpc.ServiceRegistrar) error { return nil }

func (AppModule) ConsensusVersion() uint64 { return 1 }

func (AppModule) DefaultGenesis(codec.JSONCodec) json.RawMessage {
	bz, _ := types.MarshalGenesis(types.DefaultGenesis())
	return bz
}

func (AppModule) ValidateGenesis(_ codec.JSONCodec, _ client.TxEncodingConfig, bz json.RawMessage) error {
	gs, err := types.UnmarshalGenesis(bz)
	if err != nil {
		return err
	}
	return gs.Validate()
}

func (am AppModule) InitGenesis(ctx sdk.Context, _ codec.JSONCodec, bz json.RawMessage) {
	gs, err := types.UnmarshalGenesis(bz)
	if err != nil {
		panic(err)
	}
	if err := am.keeper.SetParams(ctx, gs.Params); err != nil {
		panic(err)
	}
}

func (am AppModule) ExportGenesis(ctx sdk.Context, _ codec.JSONCodec) json.RawMessage {
	bz, err := types.MarshalGenesis(types.GenesisState{Params: am.keeper.GetParams(ctx)})
	if err != nil {
		panic(err)
	}
	return bz
}
