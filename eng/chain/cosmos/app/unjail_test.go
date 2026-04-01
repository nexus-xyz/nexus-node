package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"nexus/lib"
	"nexus/x/evm/tests/testutil"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/suite"
	yaml "gopkg.in/yaml.v3"
)

type ValidatorTestSuite struct {
	suite.Suite

	// Test validator data
	valPubKey        cryptotypes.PubKey
	valAddr          sdk.ValAddress
	consAddr         sdk.ConsAddress
	validatorAccAddr sdk.AccAddress
}

func TestValidatorTestSuite(t *testing.T) {
	suite.Run(t, new(ValidatorTestSuite))
}

func (s *ValidatorTestSuite) SetupTest() {
	s.valPubKey = ed25519.GenPrivKey().PubKey()
	s.valAddr = sdk.ValAddress(s.valPubKey.Address())
	s.consAddr = sdk.ConsAddress(s.valPubKey.Address())
	s.validatorAccAddr = sdk.AccAddress(s.valAddr)
}

func (s *ValidatorTestSuite) setupAppAndContext() (*App, sdk.Context) {
	testutil.SetupJWT(s.T())

	db := dbm.NewMemDB()
	logger := log.NewNopLogger()

	app := New(
		logger,
		db,
		nil,
		true,
		EmptyAppOptions{},
		baseapp.SetChainID("test-chain"),
	)

	header := tmproto.Header{
		Height:  1,
		ChainID: "test-chain",
		Time:    time.Now(),
	}
	ctx := app.BaseApp.NewUncachedContext(false, header)
	ctx = ctx.WithExecMode(sdk.ExecModeFinalize)

	// Initialize slashing params to avoid nil defaults during liveness logging
	{
		params := slashingtypes.DefaultParams()
		err := app.SlashingKeeper.SetParams(ctx, params)
		s.Require().NoError(err)
	}

	return app, ctx
}

func (s *ValidatorTestSuite) TestCompleteSlashJailUnjailFlow() {
	s.Run("complete slash -> jail -> unjail flow", func() {
		app, ctx := s.setupAppAndContext()
		s.setupTestValidator(app, ctx)

		// Initialize signing info
		signingInfo := slashingtypes.NewValidatorSigningInfo(
			s.consAddr,
			ctx.BlockHeight(),
			ctx.BlockHeight()-1,
			ctx.BlockTime(),
			false,
			0,
		)
		err := app.SlashingKeeper.SetValidatorSigningInfo(ctx, s.consAddr, signingInfo)
		s.Require().NoError(err)

		// Get initial validator state
		validator, err := app.StakingKeeper.GetValidator(ctx, s.valAddr)
		s.Require().NoError(err)
		initialTokens := validator.Tokens

		s.Require().False(validator.Jailed, "Validator should start unjailed")

		// Step 1: Slash the validator
		slashFraction := math.LegacyNewDecWithPrec(1, 4) // 0.01%
		power := validator.ConsensusPower(sdk.DefaultPowerReduction)
		infractionHeight := ctx.BlockHeight()

		err = app.SlashingKeeper.Slash(ctx, s.consAddr, slashFraction, power, infractionHeight)
		s.Require().NoError(err)

		// Verify slashing reduced tokens
		validator, err = app.StakingKeeper.GetValidator(ctx, s.valAddr)
		s.Require().NoError(err)
		slashedTokens := validator.Tokens

		s.Require().True(slashedTokens.LT(initialTokens), "Tokens should be reduced after slashing")

		// Step 2: Jail the validator
		err = app.SlashingKeeper.Jail(ctx, s.consAddr)
		s.Require().NoError(err)

		// Verify validator is jailed
		validator, err = app.StakingKeeper.GetValidator(ctx, s.valAddr)
		s.Require().NoError(err)
		s.Require().True(validator.Jailed, "Validator should be jailed")
		s.Require().Equal(slashedTokens, validator.Tokens, "Tokens should remain the same after jailing")

		// Step 3: Unjail the validator using ExecuteUnjailing
		err = app.ExecuteUnjailing(ctx, s.valAddr)
		s.Require().NoError(err)

		// Verify validator is unjailed
		validator, err = app.StakingKeeper.GetValidator(ctx, s.valAddr)
		s.Require().NoError(err)
		finalTokens := validator.Tokens
		s.Require().False(validator.Jailed, "Validator should be unjailed")
		s.Require().Equal(initialTokens, finalTokens, "Tokens should be restored to initial amount after unjailing")

	})
}

func (s *ValidatorTestSuite) setupTestValidator(app *App, ctx sdk.Context) {
	uniquePubKey := ed25519.GenPrivKey().PubKey()
	uniqueValAddr := sdk.ValAddress(uniquePubKey.Address())
	uniqueConsAddr := sdk.ConsAddress(uniquePubKey.Address())
	uniqueValidatorAccAddr := sdk.AccAddress(uniqueValAddr)

	s.valPubKey = uniquePubKey
	s.valAddr = uniqueValAddr
	s.consAddr = uniqueConsAddr
	s.validatorAccAddr = uniqueValidatorAccAddr

	validatorAcc := app.AuthKeeper.NewAccountWithAddress(ctx, s.validatorAccAddr)
	uniqueAccountNumber := uint64(ctx.BlockTime().UnixNano())
	validatorAcc.SetAccountNumber(uniqueAccountNumber)
	app.AuthKeeper.SetAccount(ctx, validatorAcc)

	initialCoins := sdk.NewCoins(sdk.NewCoin(testutil.TestDenom, math.NewInt(2000000)))
	err := app.BankKeeper.MintCoins(ctx, "mint", initialCoins)
	s.Require().NoError(err)
	err = app.BankKeeper.SendCoinsFromModuleToAccount(ctx, "mint", s.validatorAccAddr, initialCoins)
	s.Require().NoError(err)

	validator, err := stakingtypes.NewValidator(
		s.valAddr.String(),
		s.valPubKey,
		stakingtypes.Description{Moniker: "test-validator"},
	)
	s.Require().NoError(err)

	validator.Status = stakingtypes.Bonded
	validator.Tokens = math.NewInt(1000000)
	validator.DelegatorShares = math.LegacyNewDec(1000000)

	err = app.StakingKeeper.SetValidator(ctx, validator)
	s.Require().NoError(err)
	err = app.StakingKeeper.SetValidatorByConsAddr(ctx, validator)
	s.Require().NoError(err)

	app.DistrKeeper.SetValidatorHistoricalRewards(
		ctx, s.valAddr, 0, distrtypes.NewValidatorHistoricalRewards(sdk.DecCoins{}, 1),
	)
	app.DistrKeeper.SetValidatorCurrentRewards(ctx, s.valAddr, distrtypes.NewValidatorCurrentRewards(sdk.DecCoins{}, 1))
	app.DistrKeeper.SetValidatorAccumulatedCommission(
		ctx, s.valAddr, distrtypes.InitialValidatorAccumulatedCommission(),
	)

	stakingParams := stakingtypes.DefaultParams()
	stakingParams.BondDenom = testutil.TestDenom
	err = app.StakingKeeper.SetParams(ctx, stakingParams)
	s.Require().NoError(err)

	bondedPoolCoins := sdk.NewCoins(sdk.NewCoin(testutil.TestDenom, math.NewInt(10000000)))
	err = app.BankKeeper.MintCoins(ctx, "mint", bondedPoolCoins)
	s.Require().NoError(err)
	err = app.BankKeeper.SendCoinsFromModuleToModule(ctx, "mint", stakingtypes.BondedPoolName, bondedPoolCoins)
	s.Require().NoError(err)

	delegation := stakingtypes.NewDelegation(
		s.validatorAccAddr.String(), s.valAddr.String(), math.LegacyNewDec(1000000),
	)
	err = app.StakingKeeper.SetDelegation(ctx, delegation)
	s.Require().NoError(err)

	// Initialize delegator distribution info
	app.DistrKeeper.SetDelegatorStartingInfo(
		ctx, s.valAddr, s.validatorAccAddr, distrtypes.NewDelegatorStartingInfo(2, math.LegacyOneDec(), 0),
	)

	// Initialize distribution fee pool
	feePool := distrtypes.InitialFeePool()
	err = app.DistrKeeper.FeePool.Set(ctx, feePool)
	s.Require().NoError(err)
}

func (s *ValidatorTestSuite) TestBlockHooksUnjail() {
	s.Run("block hooks unjail execution", func() {
		app, ctx := s.setupAppAndContext()
		s.setupTestValidator(app, ctx)

		// Initialize signing info
		signingInfo := slashingtypes.NewValidatorSigningInfo(
			s.consAddr,
			ctx.BlockHeight(),
			ctx.BlockHeight()-1,
			ctx.BlockTime(),
			false,
			0,
		)
		err := app.SlashingKeeper.SetValidatorSigningInfo(ctx, s.consAddr, signingInfo)
		s.Require().NoError(err)

		// Jail the validator first
		err = app.SlashingKeeper.Jail(ctx, s.consAddr)
		s.Require().NoError(err)

		// Verify validator is jailed
		validator, err := app.StakingKeeper.GetValidator(ctx, s.valAddr)
		s.Require().NoError(err)
		s.Require().True(validator.Jailed, "Validator should be jailed")

		// Create a temporary config file for nexus config
		tempDir := s.T().TempDir()
		configPath := filepath.Join(tempDir, "nexus_config.yaml")

		config := BlockHooksConfig{
			Hooks: []BlockHook{
				BlockHook{
					Block:  ctx.BlockHeight(),
					Action: hookUnjail,
					Params: map[string]interface{}{
						"validator": s.valAddr.String(),
					},
				},
			},
		}

		configData, err := yaml.Marshal(config)
		s.Require().NoError(err)
		err = os.WriteFile(configPath, configData, 0644)
		s.Require().NoError(err)

		// Set the config path environment variable
		s.T().Setenv(lib.NEXUS_CONFIG_PATH, configPath)

		// Reload block hooks with the test config
		app.hooks = app.LoadHooks()

		// Verify block hooks were loaded
		s.Require().Len(app.hooks, 1, "Should have loaded 1 block hook")
		s.Require().Contains(app.hooks, ctx.BlockHeight(), "Should contain hook for current block")

		// Execute EndBlocker to trigger the unjail operation
		endBlockResponse, err := app.EndBlocker(ctx)
		s.Require().NoError(err)
		s.Require().NotNil(endBlockResponse)

		// Verify validator is unjailed
		validator, err = app.StakingKeeper.GetValidator(ctx, s.valAddr)
		s.Require().NoError(err)
		s.Require().False(validator.Jailed, "Validator should be unjailed after block hook")
	})
}

func (s *ValidatorTestSuite) TestBlockHooksInvalidConfig() {
	s.Run("block hooks handles invalid config gracefully", func() {
		app, ctx := s.setupAppAndContext()
		s.setupTestValidator(app, ctx)

		// Create a config with invalid action
		tempDir := s.T().TempDir()
		configPath := filepath.Join(tempDir, "invalid_nexus_config.yaml")

		invalidConfig := `
		hooks:
		  - block: 1
		    action: "invalid_action"
		    params:
		      validator: "validator1"
		`

		err := os.WriteFile(configPath, []byte(invalidConfig), 0644)
		s.Require().NoError(err)

		// Set the config path environment variable
		s.T().Setenv(lib.NEXUS_CONFIG_PATH, configPath)

		// Expect panic with invalid config
		s.Require().Panics(func() {
			app.LoadHooks()
		}, "should panic on invalid config")
	})
}

func (s *ValidatorTestSuite) TestBlockHooksMissingValidator() {
	s.Run("block hooks handles missing validator parameter", func() {
		app, ctx := s.setupAppAndContext()
		s.setupTestValidator(app, ctx)

		// Create a config with missing validator parameter
		tempDir := s.T().TempDir()
		configPath := filepath.Join(tempDir, "missing_validator_nexus_config.yaml")

		invalidConfig := `
		hooks:
		  - block: 1
		    action: "unjail"
		    params:
		      other_field: "value"
		`

		err := os.WriteFile(configPath, []byte(invalidConfig), 0644)
		s.Require().NoError(err)

		// Set the config path environment variable
		s.T().Setenv(lib.NEXUS_CONFIG_PATH, configPath)

		// Expect panic with invalid config
		s.Require().Panics(func() {
			app.LoadHooks()
		}, "should panic on invalid config")
	})
}
