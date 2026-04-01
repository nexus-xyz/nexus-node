package app

import (
	"encoding/json"
	"testing"
	"time"

	"nexus/x/evm/tests/testutil"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	abci "github.com/cometbft/cometbft/abci/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
)

func newGenesisValidationApp(t *testing.T) *App {
	t.Helper()
	testutil.SetupJWT(t)
	return New(
		log.NewNopLogger(),
		dbm.NewMemDB(),
		nil,
		true,
		EmptyAppOptions{},
		baseapp.SetChainID("test-chain"),
	)
}

func defaultGenesisWithBondedValidator(t *testing.T, app *App, tokens math.Int) json.RawMessage {
	t.Helper()

	valPubKey := ed25519.GenPrivKey().PubKey()
	valAddr := sdk.ValAddress(valPubKey.Address())
	consAddr := sdk.ConsAddress(valPubKey.Address())
	delegatorAddr := sdk.AccAddress(valAddr)

	genesis := app.DefaultGenesis()

	// Staking: one bonded validator with the given token amount
	stakingGenesis := stakingtypes.DefaultGenesisState()
	stakingGenesis.Params.BondDenom = sdk.DefaultBondDenom

	validator, err := stakingtypes.NewValidator(valAddr.String(), valPubKey, stakingtypes.Description{Moniker: "test"})
	require.NoError(t, err)
	validator.Status = stakingtypes.Bonded
	validator.Tokens = tokens
	validator.DelegatorShares = math.LegacyNewDecFromInt(tokens)
	validator.MinSelfDelegation = math.OneInt()
	stakingGenesis.Validators = append(stakingGenesis.Validators, validator)
	stakingGenesis.Delegations = append(stakingGenesis.Delegations, stakingtypes.NewDelegation(
		delegatorAddr.String(), valAddr.String(), math.LegacyNewDecFromInt(tokens),
	))
	stakingGenesis.LastTotalPower = math.NewIntFromBigInt(tokens.BigInt())
	stakingGenesis.LastValidatorPowers = []stakingtypes.LastValidatorPower{
		{Address: valAddr.String(), Power: tokens.Int64() / 1_000_000},
	}
	genesis[stakingtypes.ModuleName] = app.AppCodec().MustMarshalJSON(stakingGenesis)

	// Bank: fund the bonded pool and delegator account
	bankGenesis := banktypes.DefaultGenesisState()
	bondedPoolAddr := authtypes.NewModuleAddress(stakingtypes.BondedPoolName)
	bankGenesis.Balances = append(bankGenesis.Balances,
		banktypes.Balance{
			Address: bondedPoolAddr.String(),
			Coins:   sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, tokens)),
		},
		banktypes.Balance{
			Address: delegatorAddr.String(),
			Coins:   sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, tokens)),
		},
	)
	bankGenesis.Supply = bankGenesis.Supply.Add(sdk.NewCoin(sdk.DefaultBondDenom, tokens.MulRaw(2)))
	genesis[banktypes.ModuleName] = app.AppCodec().MustMarshalJSON(bankGenesis)

	// Slashing: signing info for the validator
	slashingGenesis := slashingtypes.DefaultGenesisState()
	slashingGenesis.SigningInfos = append(slashingGenesis.SigningInfos, slashingtypes.SigningInfo{
		Address: consAddr.String(),
		ValidatorSigningInfo: slashingtypes.NewValidatorSigningInfo(
			consAddr, 0, 0, time.Unix(0, 0), false, 0,
		),
	})
	genesis[slashingtypes.ModuleName] = app.AppCodec().MustMarshalJSON(slashingGenesis)

	appStateBytes, err := json.Marshal(genesis)
	require.NoError(t, err)
	return appStateBytes
}

func TestInitChain_GenesisValidatorBalances(t *testing.T) {
	t.Run("succeeds when bonded validator has exactly RequiredValidatorBalance", func(t *testing.T) {
		app := newGenesisValidationApp(t)
		appState := defaultGenesisWithBondedValidator(t, app, math.NewInt(RequiredValidatorBalance))

		_, err := app.InitChain(&abci.RequestInitChain{
			AppStateBytes: appState,
			ChainId:       "test-chain",
		})
		require.NoError(t, err)
	})

	// Validators with fewer tokens than PowerReduction (1_000_000) have 0 voting power
	// and are rejected by the staking module itself before our validation runs.
	// Either way the chain refuses to start, which is the correct behaviour.
	t.Run("fails when bonded validator has fewer tokens than RequiredValidatorBalance", func(t *testing.T) {
		app := newGenesisValidationApp(t)
		appState := defaultGenesisWithBondedValidator(t, app, math.NewInt(10_000))

		_, err := app.InitChain(&abci.RequestInitChain{
			AppStateBytes: appState,
			ChainId:       "test-chain",
		})
		require.Error(t, err)
	})

	// Validators with more tokens than RequiredValidatorBalance have non-zero voting power
	// so the staking module accepts them — only our ensureValidatorBalance catches this case.
	t.Run("fails when bonded validator has more tokens than RequiredValidatorBalance", func(t *testing.T) {
		app := newGenesisValidationApp(t)
		appState := defaultGenesisWithBondedValidator(t, app, math.NewInt(RequiredValidatorBalance*2))

		_, err := app.InitChain(&abci.RequestInitChain{
			AppStateBytes: appState,
			ChainId:       "test-chain",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "bonded tokens")
	})
}
