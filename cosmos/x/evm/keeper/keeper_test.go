package keeper_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"cosmossdk.io/core/address"
	storetypes "cosmossdk.io/store/types"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/runtime"
	sdktestutil "github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/suite"

	nexus "nexus/app/types"
	"nexus/lib"
	"nexus/x/evm/keeper"
	module "nexus/x/evm/module"
	"nexus/x/evm/tests/testutil"
	"nexus/x/evm/types"
)

type KeeperTestSuite struct {
	suite.Suite
}

type fixture struct {
	ctx          context.Context
	keeper       keeper.Keeper
	addressCodec address.Codec
}

func initFixture(t *testing.T) *fixture {
	t.Helper()

	// Create JWT file for test
	testutil.SetupJWT(t)

	encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
	addressCodec := addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix())
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)

	storeService := runtime.NewKVStoreService(storeKey)
	ctx := sdktestutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_test")).Ctx

	authority := authtypes.NewModuleAddress(types.GovModuleName)

	k := keeper.NewKeeper(
		storeService,
		encCfg.Codec,
		addressCodec,
		authority,
		encCfg.TxConfig,
		nexus.ChainSpec{},
	)

	// Initialize params
	if err := k.Params.Set(ctx, types.DefaultParams()); err != nil {
		t.Fatalf("failed to set params: %v", err)
	}

	return &fixture{
		ctx:          ctx,
		keeper:       k,
		addressCodec: addressCodec,
	}
}

func (s *KeeperTestSuite) writeConfigFile(contents string) {
	dir := s.T().TempDir()
	path := filepath.Join(dir, "config.yaml")

	err := os.WriteFile(path, []byte(contents), 0644)
	s.Require().NoError(err)

	s.T().Setenv(lib.NEXUS_CONFIG_PATH, path)
}

func (s *KeeperTestSuite) TestMissingConfig() {
	recipient := common.Address{}.Hex()
	fixture := initFixture(s.T())
	keeper := fixture.keeper

	s.Require().Equal(
		keeper.SuggestedFeeRecipient.Hex(),
		recipient,
		"expected recipient to match input",
	)
}

func (s *KeeperTestSuite) TestConfigInvalidAddress() {
	cases := [...]string{
		"abcd", "0xabcd", "123",
	}

	for _, recipient := range cases {
		contents := fmt.Sprintf(
			"suggested_fee_recipient: %v",
			recipient,
		)

		s.writeConfigFile(contents)

		s.Require().Panics(func() {
			initFixture(s.T())
		}, "should panic on invalid fee recipient")
	}
}

func (s *KeeperTestSuite) TestConfigValidAddress() {
	recipient := "0xe48ac8e78B0Bd6137723c59cDC2Ada6D449266B7"
	contents := fmt.Sprintf(
		"suggested_fee_recipient: %v",
		recipient,
	)
	s.writeConfigFile(contents)

	fixture := initFixture(s.T())
	keeper := fixture.keeper

	s.Require().Equal(
		keeper.SuggestedFeeRecipient.Hex(),
		recipient,
		"expected recipient to match input",
	)
}

func (s *KeeperTestSuite) TestConfigMissingUseBlockTimestamp() {
	keeper := initFixture(s.T()).keeper

	s.Require().Nil(keeper.UseBlockTimestamp)
	s.Require().Equal(uint64(0), keeper.UnixTimestampOffset)
}

func (s *KeeperTestSuite) TestConfigValidUseBlockTimestamp() {
	contents := fmt.Sprintf(
		"use_block_timestamp:\n  start_block_height: %v\n  offset: %v\n"+
			"  stop_block_height: %v\nunix_timestamp_offset: %v",
		100, 1700000000, 200, 500,
	)
	s.writeConfigFile(contents)
	keeper := initFixture(s.T()).keeper

	s.Require().Equal(uint64(100), keeper.UseBlockTimestamp.StartBlockHeight)
	s.Require().Equal(uint64(1700000000), keeper.UseBlockTimestamp.Offset)
	s.Require().Equal(uint64(200), *keeper.UseBlockTimestamp.StopBlockHeight)
	s.Require().Equal(uint64(500), keeper.UnixTimestampOffset)
}

func (s *KeeperTestSuite) TestConfigUseBlockTimestampOnlyOffset() {
	contents := fmt.Sprintf("use_block_timestamp:\n  offset: %v", 1700000000)
	s.writeConfigFile(contents)
	keeper := initFixture(s.T()).keeper

	s.Require().Equal(uint64(0), keeper.UseBlockTimestamp.StartBlockHeight)
	s.Require().Equal(uint64(1700000000), keeper.UseBlockTimestamp.Offset)
	s.Require().Nil(keeper.UseBlockTimestamp.StopBlockHeight)
	s.Require().Equal(uint64(0), keeper.UnixTimestampOffset)
}

func (s *KeeperTestSuite) TestConfigUseBlockTimestampOnlyStartBlockHeight() {
	contents := fmt.Sprintf("use_block_timestamp:\n  start_block_height: %v", 100)
	s.writeConfigFile(contents)
	keeper := initFixture(s.T()).keeper

	s.Require().Equal(uint64(100), keeper.UseBlockTimestamp.StartBlockHeight)
	s.Require().Equal(uint64(0), keeper.UseBlockTimestamp.Offset)
}

func (s *KeeperTestSuite) TestConfigInvalidUseBlockTimestampOffset() {
	cases := [...]string{
		"use_block_timestamp:\n  offset: abc",
		"use_block_timestamp:\n  offset: -1",
		"use_block_timestamp:\n  offset: 1.5",
	}
	for _, contents := range cases {
		s.writeConfigFile(contents)
		s.Require().Panics(func() { initFixture(s.T()) },
			fmt.Sprintf("should panic on invalid offset: %s", contents))
	}
}

func (s *KeeperTestSuite) TestConfigInvalidUseBlockTimestampStartBlockHeight() {
	cases := [...]string{
		"use_block_timestamp:\n  start_block_height: abc",
		"use_block_timestamp:\n  start_block_height: -1",
		"use_block_timestamp:\n  start_block_height: 1.5",
	}
	for _, contents := range cases {
		s.writeConfigFile(contents)
		s.Require().Panics(func() { initFixture(s.T()) },
			fmt.Sprintf("should panic on invalid start_block_height: %s", contents))
	}
}

func (s *KeeperTestSuite) TestConfigInvalidUseBlockTimestampStopBlockHeight() {
	cases := [...]string{
		"use_block_timestamp:\n  stop_block_height: abc",
		"use_block_timestamp:\n  stop_block_height: -1",
		"use_block_timestamp:\n  stop_block_height: 1.5",
	}
	for _, contents := range cases {
		s.writeConfigFile(contents)
		s.Require().Panics(func() { initFixture(s.T()) },
			fmt.Sprintf("should panic on invalid stop_block_height: %s", contents))
	}
}

func (s *KeeperTestSuite) TestConfigInvalidUnixTimestampOffset() {
	cases := [...]string{
		"unix_timestamp_offset: abc",
		"unix_timestamp_offset: -1",
		"unix_timestamp_offset: 1.5",
	}
	for _, contents := range cases {
		s.writeConfigFile(contents)
		s.Require().Panics(func() { initFixture(s.T()) },
			fmt.Sprintf("should panic on invalid unix_timestamp_offset: %s", contents))
	}
}

func (s *KeeperTestSuite) TestConfigStopBlockHeightBeforeStartBlockHeightPanics() {
	contents := fmt.Sprintf(
		"use_block_timestamp:\n  start_block_height: %v\n  stop_block_height: %v",
		100, 50,
	)
	s.writeConfigFile(contents)
	s.Require().Panics(func() { initFixture(s.T()) })
}

func (s *KeeperTestSuite) TestConfigStopBlockHeightEqualsStartBlockHeightAllowed() {
	contents := fmt.Sprintf(
		"use_block_timestamp:\n  start_block_height: %v\n  stop_block_height: %v",
		100, 100,
	)
	s.writeConfigFile(contents)
	s.Require().NotPanics(func() { initFixture(s.T()) })
}

func (s *KeeperTestSuite) TestConfigStopBlockHeightNotSetAllowedWithStartBlockHeight() {
	// Omitting stop_block_height means block-height mode runs indefinitely.
	contents := fmt.Sprintf(
		"use_block_timestamp:\n  start_block_height: %v",
		100,
	)
	s.writeConfigFile(contents)
	keeper := initFixture(s.T()).keeper
	s.Require().Nil(keeper.UseBlockTimestamp.StopBlockHeight)
}

func TestKeeperSuite(t *testing.T) {
	suite.Run(t, new(KeeperTestSuite))
}
