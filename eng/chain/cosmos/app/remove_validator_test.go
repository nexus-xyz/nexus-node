package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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

type RemoveValidatorTestSuite struct {
	suite.Suite

	valPubKey        cryptotypes.PubKey
	valAddr          sdk.ValAddress
	consAddr         sdk.ConsAddress
	validatorAccAddr sdk.AccAddress
}

func TestRemoveValidatorTestSuite(t *testing.T) {
	suite.Run(t, new(RemoveValidatorTestSuite))
}

func (s *RemoveValidatorTestSuite) SetupTest() {
	s.valPubKey = ed25519.GenPrivKey().PubKey()
	s.valAddr = sdk.ValAddress(s.valPubKey.Address())
	s.consAddr = sdk.ConsAddress(s.valPubKey.Address())
	s.validatorAccAddr = sdk.AccAddress(s.valAddr)
}

func (s *RemoveValidatorTestSuite) setupAppAndContext() (*App, sdk.Context) {
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

	params := slashingtypes.DefaultParams()
	err := app.SlashingKeeper.SetParams(ctx, params)
	s.Require().NoError(err)

	return app, ctx
}

func (s *RemoveValidatorTestSuite) setupTestValidator(app *App, ctx sdk.Context) {
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
	err = app.StakingKeeper.SetValidatorByPowerIndex(ctx, validator)
	s.Require().NoError(err)

	app.DistrKeeper.SetValidatorHistoricalRewards(
		ctx, s.valAddr, 0, distrtypes.NewValidatorHistoricalRewards(sdk.DecCoins{}, 1),
	)
	app.DistrKeeper.SetValidatorCurrentRewards(
		ctx, s.valAddr, distrtypes.NewValidatorCurrentRewards(sdk.DecCoins{}, 1),
	)
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
	err = app.BankKeeper.SendCoinsFromModuleToModule(
		ctx, "mint", stakingtypes.BondedPoolName, bondedPoolCoins,
	)
	s.Require().NoError(err)

	delegation := stakingtypes.NewDelegation(
		s.validatorAccAddr.String(), s.valAddr.String(), math.LegacyNewDec(1000000),
	)
	err = app.StakingKeeper.SetDelegation(ctx, delegation)
	s.Require().NoError(err)

	app.DistrKeeper.SetDelegatorStartingInfo(
		ctx, s.valAddr, s.validatorAccAddr,
		distrtypes.NewDelegatorStartingInfo(2, math.LegacyOneDec(), 0),
	)

	feePool := distrtypes.InitialFeePool()
	err = app.DistrKeeper.FeePool.Set(ctx, feePool)
	s.Require().NoError(err)

	// Initialize signing info for the validator
	signingInfo := slashingtypes.NewValidatorSigningInfo(
		s.consAddr,
		ctx.BlockHeight(),
		ctx.BlockHeight()-1,
		ctx.BlockTime(),
		false,
		0,
	)
	err = app.SlashingKeeper.SetValidatorSigningInfo(ctx, s.consAddr, signingInfo)
	s.Require().NoError(err)
}

func (s *RemoveValidatorTestSuite) setBlockHookConfig(app *App, config BlockHooksConfig) {
	tempDir := s.T().TempDir()
	configPath := filepath.Join(tempDir, "nexus_config.yaml")

	configData, err := yaml.Marshal(config)
	s.Require().NoError(err)

	err = os.WriteFile(configPath, configData, 0o644)
	s.Require().NoError(err)

	s.T().Setenv("NEXUS_CONFIG_PATH", configPath)

	app.hooks = app.LoadHooks()
}

func (s *RemoveValidatorTestSuite) TestRemoveValidatorBlockHook_JailsValidator() {
	s.Run("remove validator hook jails the validator", func() {
		app, ctx := s.setupAppAndContext()
		s.setupTestValidator(app, ctx)

		// Verify validator starts unjailed
		validator, err := app.StakingKeeper.GetValidator(ctx, s.valAddr)
		s.Require().NoError(err)
		s.Require().False(validator.Jailed, "Validator should start unjailed")
		s.Require().Equal(stakingtypes.Bonded, validator.Status)

		// Set up the remove validator hook
		config := BlockHooksConfig{
			Hooks: []BlockHook{
				{
					Block:  ctx.BlockHeight(),
					Action: hookRemoveValidator,
					Params: map[string]interface{}{
						"validator": s.valAddr.String(),
					},
				},
			},
		}

		s.setBlockHookConfig(app, config)

		// Execute EndBlocker
		endBlockResponse, err := app.EndBlocker(ctx)
		s.Require().NoError(err)
		s.Require().NotNil(endBlockResponse)

		// Verify validator is now jailed
		validator, err = app.StakingKeeper.GetValidator(ctx, s.valAddr)
		s.Require().NoError(err)
		s.Require().True(validator.Jailed, "Validator should be jailed after remove")

		// Verify validator is tombstoned
		signingInfo, err := app.SlashingKeeper.GetValidatorSigningInfo(ctx, s.consAddr)
		s.Require().NoError(err)
		s.Require().True(signingInfo.Tombstoned, "Validator should be tombstoned")
	})
}

func (s *RemoveValidatorTestSuite) TestRemoveValidatorBlockHook_BurnsTokens() {
	s.Run("remove validator hook burns tokens when requested", func() {
		app, ctx := s.setupAppAndContext()
		s.setupTestValidator(app, ctx)

		// Get initial token count
		validator, err := app.StakingKeeper.GetValidator(ctx, s.valAddr)
		s.Require().NoError(err)
		initialTokens := validator.Tokens
		s.Require().True(initialTokens.IsPositive(), "Validator should have tokens")

		// Set up the remove validator hook with burn_tokens
		config := BlockHooksConfig{
			Hooks: []BlockHook{
				{
					Block:  ctx.BlockHeight(),
					Action: hookRemoveValidator,
					Params: map[string]interface{}{
						"validator":   s.valAddr.String(),
						"burn_tokens": true,
					},
				},
			},
		}

		s.setBlockHookConfig(app, config)

		// Execute EndBlocker
		endBlockResponse, err := app.EndBlocker(ctx)
		s.Require().NoError(err)
		s.Require().NotNil(endBlockResponse)

		// Verify tokens are zeroed
		validator, err = app.StakingKeeper.GetValidator(ctx, s.valAddr)
		s.Require().NoError(err)
		s.Require().True(validator.Tokens.IsZero(), "Validator tokens should be zero after burn")
		s.Require().True(
			validator.DelegatorShares.IsZero(),
			"Validator delegator shares should be zero after burn",
		)
	})
}

func (s *RemoveValidatorTestSuite) TestExecuteRemoveValidator_ValidatorNotFound() {
	s.Run("panics when validator not found", func() {
		app, ctx := s.setupAppAndContext()

		// Try to remove a non-existent validator
		nonExistentValAddr := sdk.ValAddress(ed25519.GenPrivKey().PubKey().Address())

		s.Require().Panics(func() {
			_ = app.ExecuteRemoveValidator(ctx, nonExistentValAddr, nil)
		})
	})
}

// TestExecuteRemoveValidator_BurnTokens_PoolBalanceLessThanValidatorTokens tests the low-level case
// where the validator record claims more tokens than the bonded pool actually holds.
func (s *RemoveValidatorTestSuite) TestExecuteRemoveValidator_BurnTokens_PoolBalanceLessThanValidatorTokens() {
	s.Run("does not panic when bonded pool balance is less than validator tokens", func() {
		app, ctx := s.setupAppAndContext()

		stakingParams := stakingtypes.DefaultParams()
		stakingParams.BondDenom = testutil.TestDenom
		err := app.StakingKeeper.SetParams(ctx, stakingParams)
		s.Require().NoError(err)

		validatorAcc := app.AuthKeeper.NewAccountWithAddress(ctx, s.validatorAccAddr)
		app.AuthKeeper.SetAccount(ctx, validatorAcc)

		// Validator record claims 1_000_000 tokens
		validator, err := stakingtypes.NewValidator(
			s.valAddr.String(),
			s.valPubKey,
			stakingtypes.Description{Moniker: "test-validator"},
		)
		s.Require().NoError(err)
		validator.Status = stakingtypes.Bonded
		validator.Tokens = math.NewInt(1_000_000)
		validator.DelegatorShares = math.LegacyNewDec(1_000_000)

		err = app.StakingKeeper.SetValidator(ctx, validator)
		s.Require().NoError(err)
		err = app.StakingKeeper.SetValidatorByConsAddr(ctx, validator)
		s.Require().NoError(err)
		err = app.StakingKeeper.SetValidatorByPowerIndex(ctx, validator)
		s.Require().NoError(err)

		// Bonded pool only holds 10_000 — less than validator.Tokens
		bondedPoolCoins := sdk.NewCoins(sdk.NewCoin(testutil.TestDenom, math.NewInt(10_000)))
		err = app.BankKeeper.MintCoins(ctx, "mint", bondedPoolCoins)
		s.Require().NoError(err)
		err = app.BankKeeper.SendCoinsFromModuleToModule(ctx, "mint", stakingtypes.BondedPoolName, bondedPoolCoins)
		s.Require().NoError(err)

		signingInfo := slashingtypes.NewValidatorSigningInfo(
			s.consAddr, ctx.BlockHeight(), ctx.BlockHeight()-1, ctx.BlockTime(), false, 0,
		)
		err = app.SlashingKeeper.SetValidatorSigningInfo(ctx, s.consAddr, signingInfo)
		s.Require().NoError(err)

		feePool := distrtypes.InitialFeePool()
		err = app.DistrKeeper.FeePool.Set(ctx, feePool)
		s.Require().NoError(err)

		s.Require().NotPanics(func() {
			err = app.ExecuteRemoveValidator(ctx, s.valAddr, &RemoveValidatorOptions{BurnTokens: true})
			s.Require().NoError(err)
		})

		validator, err = app.StakingKeeper.GetValidator(ctx, s.valAddr)
		s.Require().NoError(err)
		s.Require().True(validator.Jailed)

		signingInfo, err = app.SlashingKeeper.GetValidatorSigningInfo(ctx, s.consAddr)
		s.Require().NoError(err)
		s.Require().True(signingInfo.Tombstoned)
	})
}

// TestRemoveValidator_BurnTokens_AfterAddWithFullInitialTokens reproduces the production panic:
// when a validator is added via the add-validator hook with initial_tokens == RequiredValidatorBalance,
// setValidatorBalance skips (tokens are not less than required), so the bonded pool is never funded.
// The validator record claims 1_000_000 tokens but the pool holds nothing for it.
// A subsequent remove with burn_tokens=true then panics with "insufficient funds".
func (s *RemoveValidatorTestSuite) TestRemoveValidator_BurnTokens_AfterAddWithFullInitialTokens() {
	s.Run("does not panic when bonded pool was never funded by add-validator", func() {
		app, ctx := s.setupAppAndContext()

		stakingParams := stakingtypes.DefaultParams()
		stakingParams.BondDenom = testutil.TestDenom
		err := app.StakingKeeper.SetParams(ctx, stakingParams)
		s.Require().NoError(err)

		// Add validator with initial_tokens == RequiredValidatorBalance.
		// setValidatorBalance will short-circuit (not less than required),
		// so no coins are ever deposited into the bonded pool.
		err = app.ExecuteAddValidator(ctx, s.valAddr, &AddValidatorOptions{
			PubKey:            s.valPubKey,
			Description:       stakingtypes.Description{Moniker: "test-validator"},
			InitialTokens:     math.NewInt(RequiredValidatorBalance),
			MinSelfDelegation: math.OneInt(),
		})
		s.Require().NoError(err)

		// Confirm the validator record claims RequiredValidatorBalance tokens.
		validator, err := app.StakingKeeper.GetValidator(ctx, s.valAddr)
		s.Require().NoError(err)
		s.Require().Equal(math.NewInt(RequiredValidatorBalance), validator.Tokens)

		// This panics before the fix: pool is empty but burn tries 1_000_000.
		s.Require().NotPanics(func() {
			err = app.ExecuteRemoveValidator(ctx, s.valAddr, &RemoveValidatorOptions{BurnTokens: true})
			s.Require().NoError(err)
		})

		validator, err = app.StakingKeeper.GetValidator(ctx, s.valAddr)
		s.Require().NoError(err)
		s.Require().True(validator.Jailed)

		signingInfo, err := app.SlashingKeeper.GetValidatorSigningInfo(ctx, s.consAddr)
		s.Require().NoError(err)
		s.Require().True(signingInfo.Tombstoned)
	})
}

// TestExecuteRemoveValidator_BurnTokens_UnbondedValidator reproduces the wrong-pool bug:
// when a validator is UNBONDED (jailed by the slashing module), the staking module has already
// moved its tokens from the bonded pool to the not-bonded pool. handleValidatorTokens always
// burns from BondedPoolName, so it steals from other validators' legitimate bonded tokens
// and leaves the not-bonded pool with orphaned funds.
func (s *RemoveValidatorTestSuite) TestExecuteRemoveValidator_BurnTokens_UnbondedValidator() {
	s.Run("burns from not-bonded pool when validator is unbonded", func() {
		app, ctx := s.setupAppAndContext()
		s.setupTestValidator(app, ctx)

		// Register the validator in the last consensus set so ApplyAndReturnValidatorSetUpdates
		// knows it needs to be kicked out when jailed.
		err := app.StakingKeeper.SetLastValidatorPower(ctx, s.valAddr, 1000)
		s.Require().NoError(err)

		// Jail the validator, then run ApplyAndReturnValidatorSetUpdates to trigger the
		// full end-of-block transition: status Bonded→Unbonded and token movement
		// from the bonded pool to the not-bonded pool.
		err = app.SlashingKeeper.Jail(ctx, s.consAddr)
		s.Require().NoError(err)
		_, err = app.StakingKeeper.ApplyAndReturnValidatorSetUpdates(ctx)
		s.Require().NoError(err)

		validator, err := app.StakingKeeper.GetValidator(ctx, s.valAddr)
		s.Require().NoError(err)
		s.Require().True(validator.IsJailed())
		// After jailing + ApplyAndReturnValidatorSetUpdates the validator transitions to Unbonding
		// (it reaches Unbonded only after the unbonding period expires, as in production).
		// Either way, its tokens have been moved to the not-bonded pool.
		s.Require().Equal(stakingtypes.Unbonding, validator.Status)

		bondedPoolAddr := app.AuthKeeper.GetModuleAddress(stakingtypes.BondedPoolName)
		notBondedPoolAddr := app.AuthKeeper.GetModuleAddress(stakingtypes.NotBondedPoolName)

		bondedBefore := app.BankKeeper.GetBalance(ctx, bondedPoolAddr, testutil.TestDenom)
		notBondedBefore := app.BankKeeper.GetBalance(ctx, notBondedPoolAddr, testutil.TestDenom)

		// Not-bonded pool should hold the validator's tokens after jailing
		s.Require().True(notBondedBefore.Amount.IsPositive(), "not-bonded pool should hold validator tokens")

		err = app.ExecuteRemoveValidator(ctx, s.valAddr, &RemoveValidatorOptions{BurnTokens: true})
		s.Require().NoError(err)

		bondedAfter := app.BankKeeper.GetBalance(ctx, bondedPoolAddr, testutil.TestDenom)
		notBondedAfter := app.BankKeeper.GetBalance(ctx, notBondedPoolAddr, testutil.TestDenom)

		// Bonded pool should be untouched — tokens were in the not-bonded pool
		s.Require().Equal(
			bondedBefore.Amount, bondedAfter.Amount, "bonded pool should not be touched for an unbonded validator")
		// Not-bonded pool should be drained
		s.Require().True(notBondedAfter.Amount.IsZero(), "not-bonded pool should be drained after burn")
	})
}

func (s *RemoveValidatorTestSuite) TestExecuteRemoveValidator_AlreadyJailed() {
	s.Run("handles already jailed validator", func() {
		app, ctx := s.setupAppAndContext()
		s.setupTestValidator(app, ctx)

		// Jail the validator first
		err := app.SlashingKeeper.Jail(ctx, s.consAddr)
		s.Require().NoError(err)

		// Verify validator is jailed
		validator, err := app.StakingKeeper.GetValidator(ctx, s.valAddr)
		s.Require().NoError(err)
		s.Require().True(validator.Jailed)

		// Execute remove on already jailed validator - should not error
		err = app.ExecuteRemoveValidator(ctx, s.valAddr, nil)
		s.Require().NoError(err)

		// Verify still jailed and tombstoned
		validator, err = app.StakingKeeper.GetValidator(ctx, s.valAddr)
		s.Require().NoError(err)
		s.Require().True(validator.Jailed)

		signingInfo, err := app.SlashingKeeper.GetValidatorSigningInfo(ctx, s.consAddr)
		s.Require().NoError(err)
		s.Require().True(signingInfo.Tombstoned)
	})
}
