package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/sync/errgroup"

	"cosmossdk.io/log"
	confixcmd "cosmossdk.io/tools/confix/cmd"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/debug"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/keys"
	"github.com/cosmos/cosmos-sdk/client/pruning"
	"github.com/cosmos/cosmos-sdk/client/rpc"
	"github.com/cosmos/cosmos-sdk/client/snapshot"
	"github.com/cosmos/cosmos-sdk/server"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	authcmd "github.com/cosmos/cosmos-sdk/x/auth/client/cli"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	genutilcli "github.com/cosmos/cosmos-sdk/x/genutil/client/cli"

	"nexus/app"
	"nexus/lib"
)

func initRootCmd(
	rootCmd *cobra.Command,
	txConfig client.TxConfig,
	basicManager module.BasicManager,
) {
	// Closure to capture the app instance for PostSetup
	var nexusApp *app.App

	// Wrapper to capture the app instance
	newAppWithCapture := func(
		logger log.Logger,
		db dbm.DB,
		traceStore io.Writer,
		appOpts servertypes.AppOptions,
	) servertypes.Application {
		baseappOptions := server.DefaultBaseappOptions(appOpts)
		nexusApp = app.New(logger, db, traceStore, true, appOpts, baseappOptions...)
		return nexusApp
	}

	rootCmd.AddCommand(
		genutilcli.InitCmd(basicManager, app.DefaultNodeHome),
		NewInPlaceTestnetCmd(),
		NewTestnetMultiNodeCmd(basicManager, banktypes.GenesisBalancesIterator{}),
		debug.Cmd(),
		confixcmd.ConfigCommand(),
		pruning.Cmd(newApp, app.DefaultNodeHome),
		snapshot.Cmd(newApp),
	)

	server.AddCommandsWithStartCmdOptions(
		rootCmd, app.DefaultNodeHome, newAppWithCapture, appExport, server.StartCmdOptions{
			AddFlags: addModuleInitFlags,
			PostSetup: func(
				svrCtx *server.Context, clientCtx client.Context, ctx context.Context, g *errgroup.Group,
			) error {
				if os.Getenv(lib.CoreGrpcServerEnabledEnvVar) != "true" {
					return nil // gRPC server disabled
				}

				logger := svrCtx.Logger.With("module", "grpc-server")

				// Start CosmosServer (inbound: Core -> Cosmos)
				cosmosAddr := os.Getenv(lib.CosmosGrpcAddrEnvVar)
				if cosmosAddr == "" {
					cosmosAddr = lib.GRPCDefaultAddr
				}
				// HealthCheck responses report 0; the running App's block
				// height is not plumbed through this PostSetup callback.
				impl := lib.NewCosmosServer(
					"",
					func() int64 { return 0 },
					logger,
				)

				grpcServer, errCh, err := lib.StartGRPCServer(cosmosAddr, impl)
				if err != nil {
					return fmt.Errorf("failed to start grpc server: %w", err)
				}

				// Create CoreClient (outbound: Cosmos -> Core)
				coreAddr := lib.GetCoreAddr()
				coreClient, err := lib.NewCoreClient(ctx, coreAddr, logger)
				if err != nil {
					return fmt.Errorf("failed to create core client: %w", err)
				}

				// Set CoreClient on app for use by other modules
				if nexusApp != nil {
					nexusApp.SetCoreClient(coreClient)
				}

				// Register cleanup for graceful shutdown
				g.Go(func() error {
					select {
					case <-ctx.Done():
						// Stop the inbound gRPC server (Core -> Cosmos)
						grpcServer.GracefulStop()
						// Close the outbound gRPC client connection (Cosmos -> Core)
						if err := coreClient.Close(); err != nil {
							logger.Error("failed to close core client", "error", err)
						}
						return nil
					case err := <-errCh:
						return err
					}
				})

				logger.Info("gRPC components initialized",
					"cosmos_server_addr", cosmosAddr,
					"core_client_addr", coreAddr)
				return nil
			},
		})

	// add keybase, auxiliary RPC, query, genesis, and tx child commands
	rootCmd.AddCommand(
		server.StatusCommand(),
		genutilcli.Commands(txConfig, basicManager, app.DefaultNodeHome),
		queryCommand(),
		txCommand(),
		keys.Commands(),
	)
}

// addModuleInitFlags adds more flags to the start command.
func addModuleInitFlags(startCmd *cobra.Command) {
}

func queryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        "query",
		Aliases:                    []string{"q"},
		Short:                      "Querying subcommands",
		DisableFlagParsing:         false,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(
		rpc.WaitTxCmd(),
		rpc.ValidatorCommand(),
		server.QueryBlockCmd(),
		authcmd.QueryTxsByEventsCmd(),
		server.QueryBlocksCmd(),
		authcmd.QueryTxCmd(),
		server.QueryBlockResultsCmd(),
	)

	return cmd
}

func txCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        "tx",
		Short:                      "Transactions subcommands",
		DisableFlagParsing:         false,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(
		authcmd.GetSignCommand(),
		authcmd.GetSignBatchCommand(),
		authcmd.GetMultiSignCommand(),
		authcmd.GetMultiSignBatchCmd(),
		authcmd.GetValidateSignaturesCommand(),
		flags.LineBreak,
		authcmd.GetBroadcastCommand(),
		authcmd.GetEncodeCommand(),
		authcmd.GetDecodeCommand(),
		authcmd.GetSimulateCmd(),
	)

	return cmd
}

// newApp creates the application
func newApp(
	logger log.Logger,
	db dbm.DB,
	traceStore io.Writer,
	appOpts servertypes.AppOptions,
) servertypes.Application {
	baseappOptions := server.DefaultBaseappOptions(appOpts)

	return app.New(
		logger, db, traceStore, true,
		appOpts,
		baseappOptions...,
	)
}

// appExport creates a new app (optionally at a given height) and exports state.
func appExport(
	logger log.Logger,
	db dbm.DB,
	traceStore io.Writer,
	height int64,
	forZeroHeight bool,
	jailAllowedAddrs []string,
	appOpts servertypes.AppOptions,
	modulesToExport []string,
) (servertypes.ExportedApp, error) {
	var bApp *app.App

	// this check is necessary as we use the flag in x/upgrade.
	// we can exit more gracefully by checking the flag here.
	homePath, ok := appOpts.Get(flags.FlagHome).(string)
	if !ok || homePath == "" {
		return servertypes.ExportedApp{}, errors.New("application home not set")
	}

	viperAppOpts, ok := appOpts.(*viper.Viper)
	if !ok {
		return servertypes.ExportedApp{}, errors.New("appOpts is not viper.Viper")
	}

	appOpts = viperAppOpts
	if height != -1 {
		bApp = app.New(logger, db, traceStore, false, appOpts)
		if err := bApp.LoadHeight(height); err != nil {
			return servertypes.ExportedApp{}, err
		}
	} else {
		bApp = app.New(logger, db, traceStore, true, appOpts)
	}

	return bApp.ExportAppStateAndValidators(forZeroHeight, jailAllowedAddrs, modulesToExport)
}
