package integration

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/suite"

	"cosmossdk.io/core/address"
	"cosmossdk.io/store/types"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	nexus "nexus/app/types"
	"nexus/testutil/server"
	"nexus/x/evm/keeper"
	evmmodule "nexus/x/evm/module"
	"nexus/x/evm/tests/mock_engine"
	evmtypes "nexus/x/evm/types"
)

type IntegrationTestSuite struct {
	suite.Suite

	mockEngine   *mock_engine.MockEngine
	keeper       keeper.Keeper
	ctx          sdk.Context
	addressCodec address.Codec
	tempDir      string
	storeKey     *types.KVStoreKey
}

func (s *IntegrationTestSuite) SetupSuite() {
	s.tempDir = s.T().TempDir()
	jwtFile := filepath.Join(s.tempDir, "jwt.hex")
	jwtSecret := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	err := os.WriteFile(jwtFile, []byte(jwtSecret), 0644)
	s.Require().NoError(err)

	s.T().Setenv("EVM_ENGINE_JWT_SECRET_PATH", jwtFile)

	s.addressCodec = addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix())
	s.storeKey = types.NewKVStoreKey(evmtypes.StoreKey)
}

// withPrepareProposalDeadline wraps s.ctx so PrepareProposal honors a shorter deadline than the default 10s.
func (s *IntegrationTestSuite) withPrepareProposalDeadline(d time.Duration) {
	s.T().Helper()
	c, cancel := context.WithTimeout(s.ctx.Context(), d)
	s.T().Cleanup(cancel)
	s.ctx = s.ctx.WithContext(c)
}

// SetupTestWithBehavior initializes a mock engine with a specific behavior and sets up the keeper for a test.
func (s *IntegrationTestSuite) SetupTestWithBehavior(behavior mock_engine.EngineBehavior) {
	s.T().Helper()

	engineURL, err := server.GetTestEngineUrl()
	s.Require().NoError(err)
	s.T().Setenv("EVM_ENGINE_URL", engineURL)

	parsedURL, err := url.Parse(engineURL)
	s.Require().NoError(err)
	engineAddr := parsedURL.Host
	s.Require().NotEmpty(engineAddr, "engine URL must include a host")
	s.mockEngine = mock_engine.NewMockEngine(engineAddr, behavior)
	err = s.mockEngine.Start()
	s.Require().NoError(err)
	s.mockEngine.WaitUntilReady()

	s.ctx = testutil.DefaultContextWithDB(s.T(), s.storeKey, types.NewTransientStoreKey("transient_test")).Ctx

	encCfg := moduletestutil.MakeTestEncodingConfig(evmmodule.AppModule{})
	storeService := runtime.NewKVStoreService(s.storeKey)
	authority := authtypes.NewModuleAddress(evmtypes.GovModuleName)
	timestamp := uint64(0)
	chainSpec := nexus.ChainSpec{
		PragueTimestamp: &timestamp,
	}

	s.keeper = keeper.NewKeeper(
		storeService,
		encCfg.Codec,
		s.addressCodec,
		authority,
		encCfg.TxConfig,
		chainSpec,
	)
	s.Require().NotNil(s.keeper)
}

func (s *IntegrationTestSuite) TearDownTest() {
	if s.mockEngine != nil {
		err := s.mockEngine.Stop()
		s.Require().NoError(err)
	}
}

func (s *IntegrationTestSuite) TearDownSuite() {
	err := os.RemoveAll(s.tempDir)
	s.Require().NoError(err)
}

func (s *IntegrationTestSuite) SetDefaultBlockState() {
	s.T().Helper()
	blockState := evmtypes.BlockState{
		Hash:      common.HexToHash("0x123"),
		Height:    2,
		Timestamp: uint64(time.Now().Unix()),
	}
	err := s.keeper.SetBlockState(s.ctx, blockState)
	s.Require().NoError(err)
}

func (s *IntegrationTestSuite) TestKeeperInitialization() {
	s.SetupTestWithBehavior(&mock_engine.DefaultEngineBehavior{})
	s.Require().NotNil(s.keeper)
	authorityBytes := s.keeper.GetAuthority()
	authorityAddr, err := s.addressCodec.BytesToString(authorityBytes)
	s.Require().NoError(err)
	s.Require().Equal(authtypes.NewModuleAddress(evmtypes.GovModuleName).String(), authorityAddr)
}

func (s *IntegrationTestSuite) TestPrepareProposal() {
	s.SetupTestWithBehavior(&mock_engine.DefaultEngineBehavior{})
	s.SetDefaultBlockState()

	req := &abci.RequestPrepareProposal{
		Height: 2,
		Time:   time.Now(),
	}
	resp, err := s.keeper.PrepareProposal(s.ctx, req)

	s.Require().NoError(err)
	s.Require().NotNil(resp)
	s.Require().Len(resp.Txs, 1)
}

func (s *IntegrationTestSuite) TestPrepareProposalWithForkchoiceHang() {
	s.SetupTestWithBehavior(&mock_engine.ForkchoiceHangBehavior{})
	s.SetDefaultBlockState()
	s.withPrepareProposalDeadline(500 * time.Millisecond)

	req := &abci.RequestPrepareProposal{
		Height: 2,
		Time:   time.Now(),
	}
	resp, err := s.keeper.PrepareProposal(s.ctx, req)

	s.Require().ErrorContains(err, "context deadline exceeded")
	s.Require().Nil(resp)
}

func (s *IntegrationTestSuite) TestPrepareProposalWithGetPayloadHang() {
	s.SetupTestWithBehavior(&mock_engine.GetPayloadHangBehavior{})
	s.SetDefaultBlockState()
	s.withPrepareProposalDeadline(500 * time.Millisecond)

	req := &abci.RequestPrepareProposal{
		Height: 2,
		Time:   time.Now(),
	}
	resp, err := s.keeper.PrepareProposal(s.ctx, req)

	s.Require().ErrorContains(err, "context deadline exceeded")
	s.Require().Nil(resp)
}

func (s *IntegrationTestSuite) TestPrepareProposalWithInitialFail() {
	s.SetupTestWithBehavior(&mock_engine.InitialFailBehavior{})
	s.SetDefaultBlockState()

	req := &abci.RequestPrepareProposal{
		Height: 2,
		Time:   time.Now(),
	}

	var (
		resp *abci.ResponsePrepareProposal
		err  error
	)
	s.Require().Eventually(func() bool {
		resp, err = s.keeper.PrepareProposal(s.ctx, req)
		return err == nil
	}, 5*time.Second, 1*time.Second, "prepare proposal should eventually succeed")

	s.Require().NoError(err)
	s.Require().NotNil(resp)
	s.Require().Len(resp.Txs, 1)
}

func (s *IntegrationTestSuite) TestPrepareProposalInitialBlockEmptyTx() {
	s.SetupTestWithBehavior(&mock_engine.DefaultEngineBehavior{})
	s.SetDefaultBlockState()

	req := &abci.RequestPrepareProposal{
		Height: 1,
		Time:   time.Now(),
	}

	resp, err := s.keeper.PrepareProposal(s.ctx, req)

	s.Require().NoError(err)
	s.Require().NotNil(resp)
	s.Require().Len(resp.Txs, 0)
}

func (s *IntegrationTestSuite) TestPrepareProposalWithForkchoiceError() {
	s.SetupTestWithBehavior(&mock_engine.ForkchoiceInvalidStatusBehavior{})
	s.SetDefaultBlockState()

	req := &abci.RequestPrepareProposal{
		Height: 2,
		Time:   time.Now(),
	}
	resp, err := s.keeper.PrepareProposal(s.ctx, req)

	s.Require().ErrorContains(err, "forkchoice not updated with status: INVALID")
	s.Require().Nil(resp)
}

func (s *IntegrationTestSuite) TestPrepareProposalWithForkchoiceNilPayload() {
	s.SetupTestWithBehavior(&mock_engine.ForkchoiceNilPayloadBehavior{})
	s.SetDefaultBlockState()

	req := &abci.RequestPrepareProposal{
		Height: 2,
		Time:   time.Now(),
	}
	resp, err := s.keeper.PrepareProposal(s.ctx, req)

	s.Require().ErrorContains(err, "payload ID is nil")
	s.Require().Nil(resp)
}

func (s *IntegrationTestSuite) TestPrepareProposalWithUnknownPayload() {
	s.SetupTestWithBehavior(&mock_engine.UnknownPayloadBehavior{})
	s.SetDefaultBlockState()
	s.withPrepareProposalDeadline(2 * time.Second)

	req := &abci.RequestPrepareProposal{
		Height: 2,
		Time:   time.Now(),
	}
	resp, err := s.keeper.PrepareProposal(s.ctx, req)

	s.Require().ErrorIs(err, context.DeadlineExceeded)
	s.Require().Nil(resp)
	var nGet int
	for _, r := range s.mockEngine.GetRequests() {
		switch r.Method {
		case "engine_getPayloadV3", "engine_getPayloadV4", "engine_getPayloadV5":
			nGet++
		}
	}
	s.Require().GreaterOrEqual(nGet, 2, "expected getPayload retries while EL returns unknown payload")
}

func TestIntegrationTestSuite(t *testing.T) {
	suite.Run(t, new(IntegrationTestSuite))
}
