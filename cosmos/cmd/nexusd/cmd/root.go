package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"cosmossdk.io/client/v2/autocli"
	"cosmossdk.io/depinject"
	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/config"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/x/auth/tx"
	authtxconfig "github.com/cosmos/cosmos-sdk/x/auth/tx/config"
	"github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/spf13/cobra"

	"nexus/app"
	"nexus/genesis"
)

const FlagChain = "chain"

// NewRootCmd creates a new root command for nexusd. It is called once in the main function.
func NewRootCmd() *cobra.Command {
	var (
		autoCliOpts        autocli.AppOptions
		moduleBasicManager module.BasicManager
		clientCtx          client.Context
	)

	chainSpec := app.LoadChainSpec()

	if err := depinject.Inject(
		depinject.Configs(app.AppConfig(),
			depinject.Supply(log.NewNopLogger(), chainSpec),
			depinject.Provide(
				ProvideClientContext,
			),
		),
		&autoCliOpts,
		&moduleBasicManager,
		&clientCtx,
	); err != nil {
		panic(err)
	}

	rootCmd := &cobra.Command{
		Use:           app.Name + "d",
		Short:         "nexus node",
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			// set the default command outputs
			cmd.SetOut(cmd.OutOrStdout())
			cmd.SetErr(cmd.ErrOrStderr())

			clientCtx = clientCtx.WithCmdContext(cmd.Context()).WithViper(app.Name)
			clientCtx, err := client.ReadPersistentCommandFlags(clientCtx, cmd.Flags())
			if err != nil {
				return err
			}

			clientCtx, err = config.ReadFromClientConfig(clientCtx)
			if err != nil {
				return err
			}

			if err := client.SetCmdClientContextHandler(clientCtx, cmd); err != nil {
				return err
			}

			customAppTemplate, customAppConfig := initAppConfig()
			customCMTConfig := initCometBFTConfig()

			if err := server.InterceptConfigsPreRunHandler(cmd,
				customAppTemplate, customAppConfig, customCMTConfig); err != nil {
				return err
			}

			return materializeEmbeddedGenesis(cmd)
		},
	}

	rootCmd.PersistentFlags().String(
		FlagChain, "",
		"Embedded network genesis to use ("+formatNetworkList()+"). When set on the "+
			"start command, the embedded genesis is materialized to <home>/config/genesis.json before launch.",
	)

	// Since the IBC modules don't support dependency injection, we need to
	// manually register the modules on the client side.
	// This needs to be removed after IBC supports App Wiring.
	ibcModules := app.RegisterIBC(clientCtx.Codec)
	for name, mod := range ibcModules {
		moduleBasicManager[name] = module.CoreAppModuleBasicAdaptor(name, mod)
		autoCliOpts.Modules[name] = mod
	}

	initRootCmd(rootCmd, clientCtx.TxConfig, moduleBasicManager)

	if err := autoCliOpts.EnhanceRootCommand(rootCmd); err != nil {
		panic(err)
	}

	return rootCmd
}

// ProvideClientContext creates and provides a fully initialized client.Context,
// allowing it to be used for dependency injection and CLI operations.
func ProvideClientContext(
	appCodec codec.Codec,
	interfaceRegistry codectypes.InterfaceRegistry,
	txConfigOpts tx.ConfigOptions,
	legacyAmino *codec.LegacyAmino,
) client.Context {
	clientCtx := client.Context{}.
		WithCodec(appCodec).
		WithInterfaceRegistry(interfaceRegistry).
		WithLegacyAmino(legacyAmino).
		WithInput(os.Stdin).
		WithAccountRetriever(types.AccountRetriever{}).
		WithHomeDir(app.DefaultNodeHome).
		WithViper(app.Name) // env variable prefix

	// Read the config again to overwrite the default values with the values from the config file
	clientCtx, _ = config.ReadFromClientConfig(clientCtx)

	// textual is enabled by default, we need to re-create the tx config grpc instead of bank keeper.
	txConfigOpts.TextualCoinMetadataQueryFn = authtxconfig.NewGRPCCoinMetadataQueryFn(clientCtx)
	txConfig, err := tx.NewTxConfigWithOptions(clientCtx.Codec, txConfigOpts)
	if err != nil {
		panic(err)
	}
	clientCtx = clientCtx.WithTxConfig(txConfig)

	return clientCtx
}

// materializeEmbeddedGenesis writes the genesis embedded for --chain <network>
// to <home>/config/genesis.json. It is a no-op unless --chain is set, and it
// only writes for the `start` command — other subcommands keep validation but
// skip materialization so embedded files are never touched by client flows.
//
// Always overwrites <home>/config/genesis.json with the embedded bytes —
// passing --chain is an explicit opt-in by the operator.
func materializeEmbeddedGenesis(cmd *cobra.Command) error {
	chain, err := cmd.Flags().GetString(FlagChain)
	if err != nil || chain == "" {
		return nil
	}

	if !genesis.IsEmbedded(chain) {
		return fmt.Errorf("--chain %q is not supported (available: %v)",
			chain, genesis.Names())
	}

	if cmd.Name() != "start" {
		return nil
	}

	home, err := cmd.Flags().GetString(flags.FlagHome)
	if err != nil || home == "" {
		home = app.DefaultNodeHome
	}

	embedded, err := genesis.Genesis(chain)
	if err != nil {
		return err
	}

	target := filepath.Join(home, "config", "genesis.json")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	if err := os.WriteFile(target, embedded, 0o644); err != nil {
		return fmt.Errorf("write embedded genesis: %w", err)
	}
	cmd.PrintErrf("Wrote embedded %q genesis to %s\n", chain, target)
	return nil
}

func formatNetworkList() string {
	names := genesis.Names()
	out := ""
	for i, n := range names {
		if i > 0 {
			out += "|"
		}
		out += n
	}
	return out
}
