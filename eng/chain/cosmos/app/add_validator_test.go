package app

import (
	"encoding/base64"
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
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/suite"
	yaml "gopkg.in/yaml.v3"
)

type AddValidatorTestSuite struct {
	suite.Suite

	valPubKey        cryptotypes.PubKey
	valAddr          sdk.ValAddress
	consAddr         sdk.ConsAddress
	validatorAccAddr sdk.AccAddress
}

func TestAddValidatorTestSuite(t *testing.T) {
	suite.Run(t, new(AddValidatorTestSuite))
}

func (s *AddValidatorTestSuite) SetupTest() {
	s.valPubKey = ed25519.GenPrivKey().PubKey()
	s.valAddr = sdk.ValAddress(s.valPubKey.Address())
	s.consAddr = sdk.ConsAddress(s.valPubKey.Address())
	s.validatorAccAddr = sdk.AccAddress(s.valAddr)
}

func (s *AddValidatorTestSuite) setupAppAndContext() (*App, sdk.Context) {
	testutil.SetupJWT(s.T())

	db := dbm.NewMemDB()
	logger := log.NewLogger(os.Stdout)

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

	stakingParams, err := app.StakingKeeper.GetParams(ctx)
	s.Require().NoError(err)
	stakingParams.BondDenom = sdk.DefaultBondDenom
	err = app.StakingKeeper.SetParams(ctx, stakingParams)
	s.Require().NoError(err)

	return app, ctx
}

func (s *AddValidatorTestSuite) setBlockHookConfig(app *App, config BlockHooksConfig) {
	tempDir := s.T().TempDir()
	configPath := filepath.Join(tempDir, "nexus_config.yaml")

	configData, err := yaml.Marshal(config)
	s.Require().NoError(err)

	err = os.WriteFile(configPath, configData, 0o644)
	s.Require().NoError(err)

	s.T().Setenv("NEXUS_CONFIG_PATH", configPath)

	app.hooks = app.LoadHooks()
}

func (s *AddValidatorTestSuite) buildAddValidatorParams(
	minSelf math.Int,
	bondDenom string,
) map[string]interface{} {
	params := map[string]interface{}{
		"validator": s.valAddr.String(),
		"pub_key": map[string]interface{}{
			"type":  "ed25519",
			"value": base64.StdEncoding.EncodeToString(s.valPubKey.Bytes()),
		},
		"min_self_delegation": minSelf.String(),
		"bond_denom":          bondDenom,
	}

	return params
}

func (s *AddValidatorTestSuite) TestAddValidatorBlockHook_TopsUpToRequiredBalance() {
	app, ctx := s.setupAppAndContext()

	valsBefore, err := app.StakingKeeper.GetAllValidators(ctx)
	s.Require().NoError(err)
	beforeCount := len(valsBefore)

	params := s.buildAddValidatorParams(math.OneInt(), sdk.DefaultBondDenom)
	config := BlockHooksConfig{
		Hooks: []BlockHook{
			{
				Block:  ctx.BlockHeight(),
				Action: hookAddValidator,
				Params: params,
			},
		},
	}

	s.setBlockHookConfig(app, config)

	endBlockResponse, err := app.EndBlocker(ctx)
	s.Require().NoError(err)
	s.Require().NotNil(endBlockResponse)

	valsAfter, err := app.StakingKeeper.GetAllValidators(ctx)
	s.Require().NoError(err)
	s.Require().Equal(beforeCount+1, len(valsAfter))

	validator, err := app.StakingKeeper.GetValidator(ctx, s.valAddr)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(RequiredValidatorBalance), validator.Tokens)
	s.Require().False(validator.Jailed)
}

// TestAddValidator_BondedPoolFunded asserts the invariant that after ExecuteAddValidator,
// the bonded pool holds coins equal to the validator's token count.
// Tests both omitting initial_tokens (nil) and supplying RequiredValidatorBalance explicitly.
func (s *AddValidatorTestSuite) TestAddValidator_BondedPoolFunded() {
	for _, initialTokens := range []math.Int{
		{}, // nil — initial_tokens omitted
		math.NewInt(RequiredValidatorBalance),
	} {
		s.SetupTest()
		app, ctx := s.setupAppAndContext()

		bondedPoolAddr := app.AuthKeeper.GetModuleAddress(stakingtypes.BondedPoolName)
		bondedBefore := app.BankKeeper.GetBalance(ctx, bondedPoolAddr, sdk.DefaultBondDenom)

		err := app.ExecuteAddValidator(ctx, s.valAddr, &AddValidatorOptions{
			PubKey:            s.valPubKey,
			Description:       stakingtypes.Description{Moniker: "test"},
			InitialTokens:     initialTokens,
			MinSelfDelegation: math.OneInt(),
		})
		s.Require().NoError(err)

		validator, err := app.StakingKeeper.GetValidator(ctx, s.valAddr)
		s.Require().NoError(err)

		// 1. Validator tokens must equal RequiredValidatorBalance.
		s.Require().Equal(math.NewInt(RequiredValidatorBalance), validator.Tokens)

		// 2. DelegatorShares must match Tokens (no rounding drift).
		s.Require().Equal(math.LegacyNewDecFromInt(validator.Tokens), validator.DelegatorShares)

		// 3. A self-delegation record must exist with matching shares.
		delegatorAddr := sdk.AccAddress(s.valAddr)
		delegation, err := app.StakingKeeper.GetDelegation(ctx, delegatorAddr, s.valAddr)
		s.Require().NoError(err)
		s.Require().Equal(validator.DelegatorShares, delegation.Shares)

		// 4. Bonded pool must hold exactly the validator's tokens — no phantom balances.
		bondedAfter := app.BankKeeper.GetBalance(ctx, bondedPoolAddr, sdk.DefaultBondDenom)
		bondedDeposited := bondedAfter.Amount.Sub(bondedBefore.Amount)
		s.Require().Equal(validator.Tokens, bondedDeposited)
	}
}

func (s *AddValidatorTestSuite) TestExecuteAddValidator_InvalidInitialTokens() {
	app, ctx := s.setupAppAndContext()

	for _, badTokens := range []math.Int{
		math.ZeroInt(),
		math.NewInt(500_000),
		math.NewInt(RequiredValidatorBalance * 2),
	} {
		err := app.ExecuteAddValidator(ctx, s.valAddr, &AddValidatorOptions{
			PubKey:            s.valPubKey,
			InitialTokens:     badTokens,
			MinSelfDelegation: math.OneInt(),
		})
		s.Require().ErrorContains(err, "initial_tokens must equal RequiredValidatorBalance")
	}
}

func (s *AddValidatorTestSuite) TestEnsureValidatorBalance() {
	v, err := stakingtypes.NewValidator(s.valAddr.String(), s.valPubKey, stakingtypes.Description{})
	s.Require().NoError(err)
	v.Status = stakingtypes.Bonded
	v.Tokens = math.NewInt(RequiredValidatorBalance)
	s.Require().NoError(ensureValidatorBalance(v))
}

func (s *AddValidatorTestSuite) TestAddValidatorBlockHook_NoOpWhenSufficient() {
	app, ctx := s.setupAppAndContext()

	valsBefore, err := app.StakingKeeper.GetAllValidators(ctx)
	s.Require().NoError(err)
	beforeCount := len(valsBefore)

	params := s.buildAddValidatorParams(math.OneInt(), sdk.DefaultBondDenom)
	config := BlockHooksConfig{
		Hooks: []BlockHook{
			{
				Block:  ctx.BlockHeight(),
				Action: hookAddValidator,
				Params: params,
			},
		},
	}

	s.setBlockHookConfig(app, config)

	endBlockResponse, err := app.EndBlocker(ctx)
	s.Require().NoError(err)
	s.Require().NotNil(endBlockResponse)

	valsAfter, err := app.StakingKeeper.GetAllValidators(ctx)
	s.Require().NoError(err)
	s.Require().Equal(beforeCount+1, len(valsAfter))

	validator, err := app.StakingKeeper.GetValidator(ctx, s.valAddr)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(RequiredValidatorBalance), validator.Tokens)
}
