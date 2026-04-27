package app

import (
	"context"

	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	globalfeetypes "nexus/x/globalfee/types"
)

func (app *App) registerMigrations() {
	upgradeInfo, err := app.UpgradeKeeper.ReadUpgradeInfoFromDisk()
	if err != nil {
		panic(err)
	}
	app.registerGlobalFeeV1(upgradeInfo)
}

func (app *App) registerGlobalFeeV1(upgradeInfo upgradetypes.Plan) {
	const name = "globalfee-v1"

	app.UpgradeKeeper.SetUpgradeHandler(name,
		func(ctx context.Context, _ upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
			return app.ModuleManager.RunMigrations(ctx, app.Configurator(), fromVM)
		},
	)

	if upgradeInfo.Name == name && !app.UpgradeKeeper.IsSkipHeight(upgradeInfo.Height) {
		app.SetStoreLoader(upgradetypes.UpgradeStoreLoader(upgradeInfo.Height, &storetypes.StoreUpgrades{
			Added: []string{globalfeetypes.ModuleName},
		}))
	}
}
