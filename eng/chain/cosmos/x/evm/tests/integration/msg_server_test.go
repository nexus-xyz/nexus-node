package integration

import (
	"context"
	"encoding/json"
	"math/big"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/core/address"
	storetypes "cosmossdk.io/store/types"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/runtime"
	sdktestutil "github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	nexus "nexus/app/types"
	"nexus/testutil/server"
	"nexus/x/evm/keeper"
	evmmodule "nexus/x/evm/module"
	"nexus/x/evm/tests/mock_engine"
	"nexus/x/evm/tests/testutil"
	evmtypes "nexus/x/evm/types"
)

// buildPayloadRequest creates a MsgExecutionPayload request from ExecutableData
func buildPayloadRequest(payload engine.ExecutableData) *evmtypes.MsgExecutionPayload {
	payloadData, _ := json.Marshal(payload)
	return &evmtypes.MsgExecutionPayload{
		Authority:        authtypes.NewModuleAddress(evmtypes.ModuleName).String(),
		ExecutionPayload: payloadData,
	}
}

func buildTxBytes(tx types.TxData) ([]byte, error) {
	return types.NewTx(tx).MarshalBinary()
}

func checkVersionedHashes(t *testing.T, suite *ExecutionPayloadTestSuite, expectedHashes []common.Hash) {
	requests := suite.mockEngine.GetRequests()
	require.Equal(t, len(requests), 2)
	require.Equal(t, requests[0].Method, "engine_newPayloadV4")
	require.Equal(t, requests[1].Method, "engine_forkchoiceUpdatedV3")

	versionedHashes := []common.Hash{}
	require.NoError(t, json.Unmarshal(requests[0].Params[1], &versionedHashes))
	require.Equal(t, expectedHashes, versionedHashes)
}

type ExecutionPayloadTestSuite struct {
	keeper       keeper.Keeper
	msgServer    evmtypes.MsgServer
	ctx          sdk.Context
	addressCodec address.Codec
	storeKey     *storetypes.KVStoreKey
	mockEngine   *mock_engine.MockEngine
}

type setupOption int

const (
	useEmptyAppHash setupOption = iota
)

func (s *ExecutionPayloadTestSuite) SetupTest(
	t *testing.T,
	behavior mock_engine.EngineBehavior,
	options ...setupOption,
) {
	t.Helper()
	prague := uint64(0)
	s.SetupTestWithChainSpec(t, behavior, nexus.ChainSpec{
		PragueTimestamp: &prague,
	}, options...)
}

// SetupTestWithChainSpec sets fork thresholds on the test keeper.
func (s *ExecutionPayloadTestSuite) SetupTestWithChainSpec(
	t *testing.T,
	behavior mock_engine.EngineBehavior,
	chainSpec nexus.ChainSpec,
	options ...setupOption,
) {
	t.Helper()
	require.NoError(t, chainSpec.Validate())

	// Set up JWT secret for the engine client
	testutil.SetupJWT(t)

	engineURL, err := server.GetTestEngineUrl()
	require.NoError(t, err)
	t.Setenv("EVM_ENGINE_URL", engineURL)

	parsedURL, err := url.Parse(engineURL)
	require.NoError(t, err)
	engineAddr := parsedURL.Host
	require.NotEmpty(t, engineAddr)

	// Start mock engine with the specified behavior
	s.mockEngine = mock_engine.NewMockEngine(engineAddr, behavior)
	err = s.mockEngine.Start()
	require.NoErrorf(t, err, "Failed to start mock engine on %s. Port may already be in use.", engineAddr)
	s.mockEngine.WaitUntilReady()

	s.addressCodec = addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix())
	s.storeKey = storetypes.NewKVStoreKey(evmtypes.StoreKey)
	s.ctx = sdktestutil.DefaultContextWithDB(t, s.storeKey, storetypes.NewTransientStoreKey("transient_test")).Ctx

	// Set context to finalize mode
	s.ctx = s.ctx.WithExecMode(sdk.ExecModeFinalize)

	// Set a mock app hash
	header := s.ctx.BlockHeader()
	if !slices.Contains(options, useEmptyAppHash) {
		header.AppHash = common.HexToHash(testutil.RandomHex(32)).Bytes()
	}
	header.Height = int64(testutil.DefaultStateTimestamp + 1)
	s.ctx = s.ctx.WithBlockHeader(header)
	// Set BlockTime to match the expected timestamp for legacy calculation
	expectedTimestamp := testutil.DefaultStateTimestamp + 1
	s.ctx = s.ctx.WithBlockTime(time.Unix(int64(expectedTimestamp), 0))

	// Set up keeper
	encCfg := moduletestutil.MakeTestEncodingConfig(evmmodule.AppModule{})
	storeService := runtime.NewKVStoreService(s.storeKey)
	authority := authtypes.NewModuleAddress(evmtypes.GovModuleName)

	s.keeper = keeper.NewKeeper(
		storeService,
		encCfg.Codec,
		s.addressCodec,
		authority,
		encCfg.TxConfig,
		chainSpec,
	)

	s.msgServer = keeper.NewMsgServerImpl(s.keeper)

	// Ensure a default block state exists for contextual validation checks
	_ = s.keeper.Params.Set(s.ctx, evmtypes.DefaultParams())
	_ = s.keeper.SetBlockState(
		s.ctx,
		evmtypes.NewBlockState(
			testutil.DefaultStateHash,
			testutil.DefaultStateHeight,
			testutil.DefaultStateTimestamp,
		),
	)
}

func (s *ExecutionPayloadTestSuite) TearDownTest(t *testing.T) {
	if s.mockEngine != nil {
		err := s.mockEngine.Stop()
		require.NoError(t, err)
	}
}

// CustomBehavior implements mock_engine.EngineBehavior for custom responses
type CustomBehavior struct {
	newPayloadResponse engine.PayloadStatusV1
	newPayloadError    *mock_engine.JsonRPCError
	forkchoiceResponse engine.ForkChoiceResponse
	forkchoiceError    *mock_engine.JsonRPCError
}

func (c *CustomBehavior) HandleNewPayloadV4(
	state *mock_engine.EngineState,
	payload engine.ExecutableData,
	versionedHashes []common.Hash,
	parentBeaconBlockRoot *common.Hash,
	requests *evmtypes.ConsensusRequests,
) (engine.PayloadStatusV1, *mock_engine.JsonRPCError) {
	if c.newPayloadError != nil {
		return engine.PayloadStatusV1{}, c.newPayloadError
	}
	return c.newPayloadResponse, nil
}

func (c *CustomBehavior) HandleForkchoiceUpdatedV3(
	state *mock_engine.EngineState,
	forkchoiceState engine.ForkchoiceStateV1,
	payloadAttributes *engine.PayloadAttributes,
) (engine.ForkChoiceResponse, *mock_engine.JsonRPCError) {
	if c.forkchoiceError != nil {
		return engine.ForkChoiceResponse{}, c.forkchoiceError
	}
	return c.forkchoiceResponse, nil
}

func (c *CustomBehavior) HandleGetPayloadV4(
	state *mock_engine.EngineState,
	payloadID engine.PayloadID,
) (engine.ExecutionPayloadEnvelope, *mock_engine.JsonRPCError) {
	// Not used in ExecutionPayload method
	return engine.ExecutionPayloadEnvelope{}, nil
}

func (c *CustomBehavior) HandleGetPayloadV5(
	state *mock_engine.EngineState,
	payloadID engine.PayloadID,
) (engine.ExecutionPayloadEnvelope, *mock_engine.JsonRPCError) {
	return c.HandleGetPayloadV4(state, payloadID)
}

// RetryBehavior simulates network failures that recover after retry
type RetryBehavior struct {
	callCount int
}

func (r *RetryBehavior) HandleNewPayloadV4(
	state *mock_engine.EngineState,
	payload engine.ExecutableData,
	versionedHashes []common.Hash,
	parentBeaconBlockRoot *common.Hash,
	requests *evmtypes.ConsensusRequests,
) (engine.PayloadStatusV1, *mock_engine.JsonRPCError) {
	r.callCount++

	// Fail the first call (simulating network error), succeed on retry
	if r.callCount == 1 {
		return engine.PayloadStatusV1{}, &mock_engine.JsonRPCError{Code: -1, Message: "network error"}
	}

	return engine.PayloadStatusV1{Status: engine.VALID}, nil
}

func (r *RetryBehavior) HandleForkchoiceUpdatedV3(
	state *mock_engine.EngineState,
	forkchoiceState engine.ForkchoiceStateV1,
	payloadAttributes *engine.PayloadAttributes,
) (engine.ForkChoiceResponse, *mock_engine.JsonRPCError) {
	return engine.ForkChoiceResponse{PayloadStatus: engine.PayloadStatusV1{Status: engine.VALID}}, nil
}

func (r *RetryBehavior) HandleGetPayloadV4(
	state *mock_engine.EngineState,
	payloadID engine.PayloadID,
) (engine.ExecutionPayloadEnvelope, *mock_engine.JsonRPCError) {
	return engine.ExecutionPayloadEnvelope{}, nil
}

func (r *RetryBehavior) HandleGetPayloadV5(
	state *mock_engine.EngineState,
	payloadID engine.PayloadID,
) (engine.ExecutionPayloadEnvelope, *mock_engine.JsonRPCError) {
	return r.HandleGetPayloadV4(state, payloadID)
}

// RetryWithBlockUpdateBehavior simulates getting a new block during retry
// This simulates the realistic scenario where retry waits for a new block from consensus
type RetryWithBlockUpdateBehavior struct {
	callCount int
	keeper    *keeper.Keeper // Need access to keeper to update block state
}

func (r *RetryWithBlockUpdateBehavior) HandleNewPayloadV4(
	state *mock_engine.EngineState,
	payload engine.ExecutableData,
	versionedHashes []common.Hash,
	parentBeaconBlockRoot *common.Hash,
	requests *evmtypes.ConsensusRequests,
) (engine.PayloadStatusV1, *mock_engine.JsonRPCError) {
	r.callCount++

	// On the second call, simulate that a new block has arrived from consensus
	// In reality, this would happen when the retry mechanism waits and consensus provides a new block
	if r.callCount == 2 && r.keeper != nil {
		// Simulate setting a new block state (as would happen from consensus)
		newBlockState := evmtypes.BlockState{
			Hash:      common.HexToHash(testutil.RandomHex(32)), // New block hash
			Height:    2,                                        // New height
			Timestamp: 999,                                      // New timestamp
		}
		// In a real scenario, this would be set by the consensus engine
		// Here we simulate it by directly updating the keeper's block state
		ctx := context.Background()
		_ = r.keeper.SetBlockState(ctx, newBlockState)
	}

	return engine.PayloadStatusV1{Status: engine.VALID}, nil
}

func (r *RetryWithBlockUpdateBehavior) HandleForkchoiceUpdatedV3(
	state *mock_engine.EngineState,
	forkchoiceState engine.ForkchoiceStateV1,
	payloadAttributes *engine.PayloadAttributes,
) (engine.ForkChoiceResponse, *mock_engine.JsonRPCError) {
	return engine.ForkChoiceResponse{PayloadStatus: engine.PayloadStatusV1{Status: engine.VALID}}, nil
}

func (r *RetryWithBlockUpdateBehavior) HandleGetPayloadV4(
	state *mock_engine.EngineState,
	payloadID engine.PayloadID,
) (engine.ExecutionPayloadEnvelope, *mock_engine.JsonRPCError) {
	return engine.ExecutionPayloadEnvelope{}, nil
}

func (r *RetryWithBlockUpdateBehavior) HandleGetPayloadV5(
	state *mock_engine.EngineState,
	payloadID engine.PayloadID,
) (engine.ExecutionPayloadEnvelope, *mock_engine.JsonRPCError) {
	return r.HandleGetPayloadV4(state, payloadID)
}

func TestExecutionPayload(t *testing.T) {
	suite := &ExecutionPayloadTestSuite{}

	t.Run("successful execution payload", func(t *testing.T) {
		behavior := &CustomBehavior{
			newPayloadResponse: engine.PayloadStatusV1{Status: engine.VALID},
			forkchoiceResponse: engine.ForkChoiceResponse{PayloadStatus: engine.PayloadStatusV1{Status: engine.VALID}},
		}
		suite.SetupTest(t, behavior)
		defer suite.TearDownTest(t)

		// Create valid payload with all required fields
		payload := testutil.BuildPayload()
		req := buildPayloadRequest(payload)

		// Execute
		resp, err := suite.msgServer.ExecutionPayload(suite.ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)
	})

	t.Run("successful execution payload with blob tx", func(t *testing.T) {
		behavior := &CustomBehavior{
			newPayloadResponse: engine.PayloadStatusV1{Status: engine.VALID},
			forkchoiceResponse: engine.ForkChoiceResponse{PayloadStatus: engine.PayloadStatusV1{Status: engine.VALID}},
		}
		suite.SetupTest(t, behavior)
		defer suite.TearDownTest(t)

		// Create valid payload with all required fields
		payload := testutil.BuildPayload()
		hashes := []common.Hash{common.HexToHash("0x01")}
		txBytes, err := buildTxBytes(&types.BlobTx{BlobHashes: hashes})
		require.NoError(t, err)
		payload.Transactions = [][]byte{txBytes}

		req := buildPayloadRequest(payload)

		// Execute
		resp, err := suite.msgServer.ExecutionPayload(suite.ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		checkVersionedHashes(t, suite, hashes)
	})

	t.Run("successful execution payload with blob tx containing multiple hashes", func(t *testing.T) {
		behavior := &CustomBehavior{
			newPayloadResponse: engine.PayloadStatusV1{Status: engine.VALID},
			forkchoiceResponse: engine.ForkChoiceResponse{PayloadStatus: engine.PayloadStatusV1{Status: engine.VALID}},
		}
		suite.SetupTest(t, behavior)
		defer suite.TearDownTest(t)

		// Create valid payload with all required fields
		hashes := []common.Hash{common.HexToHash("0x01"), common.HexToHash("0x02"), common.HexToHash("0x03")}
		payload := testutil.BuildPayload()
		blobTxBytes, err := buildTxBytes(&types.BlobTx{
			BlobHashes: hashes,
		})
		require.NoError(t, err)
		payload.Transactions = [][]byte{blobTxBytes}

		req := buildPayloadRequest(payload)

		// Execute
		resp, err := suite.msgServer.ExecutionPayload(suite.ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		checkVersionedHashes(t, suite, hashes)
	})

	t.Run("successful execution payload with multiple blob and non-blob tx", func(t *testing.T) {
		behavior := &CustomBehavior{
			newPayloadResponse: engine.PayloadStatusV1{Status: engine.VALID},
			forkchoiceResponse: engine.ForkChoiceResponse{PayloadStatus: engine.PayloadStatusV1{Status: engine.VALID}},
		}
		suite.SetupTest(t, behavior)
		defer suite.TearDownTest(t)

		// Create valid payload with mixed transactions
		hashes1 := []common.Hash{common.HexToHash("0x00"), common.HexToHash("0x01")}
		hashes2 := []common.Hash{common.HexToHash("0x02")}
		hashes3 := []common.Hash{common.HexToHash("0x03")}
		hashes4 := []common.Hash{common.HexToHash("0x04")}
		hashes5 := []common.Hash{common.HexToHash("0x05")}
		payload := testutil.BuildPayload()
		blobTxBytes1, err := buildTxBytes(&types.BlobTx{BlobHashes: hashes1})
		require.NoError(t, err)
		blobTxBytes2, err := buildTxBytes(&types.BlobTx{BlobHashes: hashes2})
		require.NoError(t, err)
		blobTxBytes3, err := buildTxBytes(&types.BlobTx{BlobHashes: hashes3})
		require.NoError(t, err)
		blobTxBytes4, err := buildTxBytes(&types.BlobTx{BlobHashes: hashes4})
		require.NoError(t, err)
		blobTxBytes5, err := buildTxBytes(&types.BlobTx{BlobHashes: hashes5})
		require.NoError(t, err)
		nonBlobTxBytes, err := buildTxBytes(&types.DynamicFeeTx{})
		require.NoError(t, err)

		payload.Transactions = [][]byte{
			blobTxBytes1,
			nonBlobTxBytes,
			blobTxBytes2,
			nonBlobTxBytes,
			blobTxBytes3,
			nonBlobTxBytes,
			blobTxBytes4,
			blobTxBytes5,
			nonBlobTxBytes,
		} // multiple txs

		req := buildPayloadRequest(payload)

		// Execute
		resp, err := suite.msgServer.ExecutionPayload(suite.ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		checkVersionedHashes(t, suite, []common.Hash{
			hashes1[0], hashes1[1], hashes2[0], hashes3[0], hashes4[0], hashes5[0],
		})
	})

	t.Run("error when not in finalize mode", func(t *testing.T) {
		behavior := &mock_engine.DefaultEngineBehavior{}
		suite.SetupTest(t, behavior)
		defer suite.TearDownTest(t)

		// Set context to check mode instead of finalize
		suite.ctx = suite.ctx.WithExecMode(sdk.ExecModeCheck)

		req := &evmtypes.MsgExecutionPayload{
			Authority:        authtypes.NewModuleAddress(evmtypes.ModuleName).String(),
			ExecutionPayload: []byte(`{}`), // Simple JSON payload
		}

		_, err := suite.msgServer.ExecutionPayload(suite.ctx, req)
		require.Error(t, err)
		require.Contains(t, err.Error(), "execution payload can only be submitted in finalize mode")
	})

	t.Run("error when payload is empty", func(t *testing.T) {
		behavior := &mock_engine.DefaultEngineBehavior{}
		suite.SetupTest(t, behavior)
		defer suite.TearDownTest(t)

		req := &evmtypes.MsgExecutionPayload{
			Authority:        authtypes.NewModuleAddress(evmtypes.ModuleName).String(),
			ExecutionPayload: []byte{}, // Empty payload bytes
		}

		_, err := suite.msgServer.ExecutionPayload(suite.ctx, req)
		require.Error(t, err)
		require.Contains(t, err.Error(), "execution payload cannot be empty")
	})

	t.Run("error with invalid authority", func(t *testing.T) {
		behavior := &mock_engine.DefaultEngineBehavior{}
		suite.SetupTest(t, behavior)
		defer suite.TearDownTest(t)

		payload := testutil.BuildPayload()

		req := buildPayloadRequest(payload)
		req.Authority = "invalid-authority" // Wrong authority

		_, err := suite.msgServer.ExecutionPayload(suite.ctx, req)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid authority")
	})

	t.Run("error with invalid JSON payload", func(t *testing.T) {
		behavior := &mock_engine.DefaultEngineBehavior{}
		suite.SetupTest(t, behavior)
		defer suite.TearDownTest(t)

		req := buildPayloadRequest(testutil.BuildPayload())
		req.ExecutionPayload = []byte(`invalid json`) // Invalid JSON payload

		_, err := suite.msgServer.ExecutionPayload(suite.ctx, req)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid execution payload")
	})

	t.Run("error with withdrawals not supported", func(t *testing.T) {
		behavior := &mock_engine.DefaultEngineBehavior{}
		suite.SetupTest(t, behavior)
		defer suite.TearDownTest(t)

		payload := testutil.BuildPayload()
		payload.Withdrawals = []*types.Withdrawal{{Amount: 100}} // Non-empty withdrawals

		req := buildPayloadRequest(payload)

		_, err := suite.msgServer.ExecutionPayload(suite.ctx, req)
		require.Error(t, err)
		require.Contains(t, err.Error(), "withdrawals are not supported")
	})

	t.Run("error with nil required fields", func(t *testing.T) {
		behavior := &mock_engine.DefaultEngineBehavior{}
		suite.SetupTest(t, behavior)
		defer suite.TearDownTest(t)

		payload := testutil.BuildPayload()
		payload.Withdrawals = nil      // Should not be nil
		payload.BlobGasUsed = nil      // Should not be nil
		payload.ExcessBlobGas = nil    // Should not be nil
		payload.ExecutionWitness = nil // Should be nil

		req := buildPayloadRequest(payload)

		_, err := suite.msgServer.ExecutionPayload(suite.ctx, req)
		require.Error(t, err)
		require.Contains(t, err.Error(), "withdrawals, blob gas used, and excess blob gas are required")
	})

	t.Run("error with execution witness not supported", func(t *testing.T) {
		behavior := &mock_engine.DefaultEngineBehavior{}
		suite.SetupTest(t, behavior)
		defer suite.TearDownTest(t)

		// Create a non-nil ExecutionWitness
		witness := &types.ExecutionWitness{}
		payload := testutil.BuildPayload()
		payload.ExecutionWitness = witness // Should be nil

		req := buildPayloadRequest(payload)

		_, err := suite.msgServer.ExecutionPayload(suite.ctx, req)
		require.Error(t, err)
		require.Contains(t, err.Error(), "execution witness is not supported")
	})

	t.Run("retry mechanism works correctly", func(t *testing.T) {
		// Test that the retry mechanism works when network calls initially fail
		behavior := &RetryBehavior{}
		suite.SetupTest(t, behavior)
		defer suite.TearDownTest(t)

		payload := testutil.BuildPayload()
		req := buildPayloadRequest(payload)

		// This should succeed after retry
		_, err := suite.msgServer.ExecutionPayload(suite.ctx, req)
		require.NoError(t, err)

		// Verify that retry actually happened (should have been called twice)
		require.Equal(t, 2, behavior.callCount, "Expected retry to occur")
	})

	t.Run("error with empty app hash", func(t *testing.T) {
		// Use standalone setup to create a context with empty app hash
		behavior := &RetryBehavior{}
		suite.SetupTest(t, behavior, useEmptyAppHash)
		defer suite.TearDownTest(t)

		payload := testutil.BuildPayload()
		req := buildPayloadRequest(payload)

		goCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()

		ctx := suite.ctx.WithContext(goCtx)

		_, err := suite.msgServer.ExecutionPayload(ctx, req)
		require.Error(t, err)

		require.True(t,
			strings.Contains(err.Error(), "context deadline exceeded"),
			"Expected timeout error, got: %v", err)
	})

	t.Run("error with invalid payload status", func(t *testing.T) {
		behavior := &CustomBehavior{
			newPayloadResponse: engine.PayloadStatusV1{Status: engine.INVALID},
		}
		suite.SetupTest(t, behavior)
		defer suite.TearDownTest(t)

		payload := testutil.BuildPayload()
		req := buildPayloadRequest(payload)

		_, err := suite.msgServer.ExecutionPayload(suite.ctx, req)
		require.Error(t, err)
		require.Contains(t, err.Error(), "payload is not valid with status: INVALID")
	})

	t.Run("error with forkchoice update failure", func(t *testing.T) {
		behavior := &CustomBehavior{
			newPayloadResponse: engine.PayloadStatusV1{Status: engine.VALID},
			forkchoiceResponse: engine.ForkChoiceResponse{
				PayloadStatus: engine.PayloadStatusV1{Status: engine.INVALID},
			},
		}
		suite.SetupTest(t, behavior)
		defer suite.TearDownTest(t)

		payload := testutil.BuildPayload()
		req := buildPayloadRequest(payload)

		_, err := suite.msgServer.ExecutionPayload(suite.ctx, req)
		require.Error(t, err)
		require.Contains(t, err.Error(), "forkchoice not updated with status: INVALID")
	})

	t.Run("error with mismatched parent hash", func(t *testing.T) {
		behavior := &mock_engine.DefaultEngineBehavior{}
		suite.SetupTest(t, behavior)
		defer suite.TearDownTest(t)

		payload := testutil.BuildPayload()
		// Change the parent hash to something "random"
		payload.ParentHash = common.HexToHash(testutil.RandomHex(32))
		req := buildPayloadRequest(payload)

		_, err := suite.msgServer.ExecutionPayload(suite.ctx, req)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid parent hash")
	})

	t.Run("error with incorrect block number", func(t *testing.T) {
		behavior := &mock_engine.DefaultEngineBehavior{}
		suite.SetupTest(t, behavior)
		defer suite.TearDownTest(t)

		payload := testutil.BuildPayload()
		// Default state height is 0, so expected number is 1. Use 2.
		payload.Number = 2
		req := buildPayloadRequest(payload)

		_, err := suite.msgServer.ExecutionPayload(suite.ctx, req)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid block number")
	})

	t.Run("error with non-increasing timestamp", func(t *testing.T) {
		behavior := &mock_engine.DefaultEngineBehavior{}
		suite.SetupTest(t, behavior)
		defer suite.TearDownTest(t)

		payload := testutil.BuildPayload()
		payload.Timestamp = 20
		req := buildPayloadRequest(payload)

		_, err := suite.msgServer.ExecutionPayload(suite.ctx, req)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid timestamp")
	})

	t.Run("error with too many blob tx", func(t *testing.T) {
		behavior := &CustomBehavior{
			newPayloadResponse: engine.PayloadStatusV1{Status: engine.VALID},
			forkchoiceResponse: engine.ForkChoiceResponse{PayloadStatus: engine.PayloadStatusV1{Status: engine.VALID}},
		}
		suite.SetupTest(t, behavior)
		defer suite.TearDownTest(t)

		payload := testutil.BuildPayload()
		hashes := make([]common.Hash, keeper.MaxBlobsPerBlock+1)
		for i := range hashes {
			hashes[i] = common.BigToHash(big.NewInt(int64(i + 1)))
		}
		txBytes, err := buildTxBytes(&types.BlobTx{BlobHashes: hashes})
		require.NoError(t, err)
		payload.Transactions = [][]byte{txBytes}

		req := buildPayloadRequest(payload)

		// Set context deadline to 1 second to avoid infinite retries.
		goCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		ctx := suite.ctx.WithContext(goCtx)

		_, err = suite.msgServer.ExecutionPayload(ctx, req)
		require.Error(t, err)
	})
}

// Osaka window: newPayloadV4 only (execution-apis: newPayloadV5 after Amsterdam).
func TestExecutionPayload_EngineNewPayloadOsakaUsesV4UntilAmsterdam(t *testing.T) {
	prague := uint64(0)
	osaka := uint64(1000)
	amsterdam := uint64(2000)
	chainSpec := nexus.ChainSpec{
		PragueTimestamp:    &prague,
		OsakaTimestamp:     &osaka,
		AmsterdamTimestamp: &amsterdam,
	}
	behavior := &CustomBehavior{
		newPayloadResponse: engine.PayloadStatusV1{Status: engine.VALID},
		forkchoiceResponse: engine.ForkChoiceResponse{PayloadStatus: engine.PayloadStatusV1{Status: engine.VALID}},
	}
	suite := &ExecutionPayloadTestSuite{}
	suite.SetupTestWithChainSpec(t, behavior, chainSpec)
	defer suite.TearDownTest(t)

	payload := testutil.BuildPayload()
	require.Equal(t, testutil.DefaultStateTimestamp+1, payload.Timestamp,
		"fixture must be in Osaka range before Amsterdam")

	req := buildPayloadRequest(payload)
	resp, err := suite.msgServer.ExecutionPayload(suite.ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	var nV4, nV5 int
	for _, r := range suite.mockEngine.GetRequests() {
		switch r.Method {
		case "engine_newPayloadV4":
			nV4++
		case "engine_newPayloadV5":
			nV5++
		}
	}
	require.Positive(t, nV4, "expect newPayloadV4 before Amsterdam")
	require.Zero(t, nV5, "no newPayloadV5 before Amsterdam")
}

func TestExecutionPayload_EngineNewPayloadAmsterdamUsesV5(t *testing.T) {
	prague := uint64(0)
	osaka := uint64(400)
	amsterdam := uint64(500)
	chainSpec := nexus.ChainSpec{
		PragueTimestamp:    &prague,
		OsakaTimestamp:     &osaka,
		AmsterdamTimestamp: &amsterdam,
	}
	behavior := &CustomBehavior{
		newPayloadResponse: engine.PayloadStatusV1{Status: engine.VALID},
		forkchoiceResponse: engine.ForkChoiceResponse{PayloadStatus: engine.PayloadStatusV1{Status: engine.VALID}},
	}
	suite := &ExecutionPayloadTestSuite{}
	suite.SetupTestWithChainSpec(t, behavior, chainSpec)
	defer suite.TearDownTest(t)

	payload := testutil.BuildPayload()
	require.GreaterOrEqual(t, payload.Timestamp, amsterdam,
		"fixture at or past Amsterdam for newPayloadV5")

	req := buildPayloadRequest(payload)
	resp, err := suite.msgServer.ExecutionPayload(suite.ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	var nV4, nV5 int
	for _, r := range suite.mockEngine.GetRequests() {
		switch r.Method {
		case "engine_newPayloadV4":
			nV4++
		case "engine_newPayloadV5":
			nV5++
		}
	}
	require.Positive(t, nV5, "expect newPayloadV5 at Amsterdam")
	require.Zero(t, nV4, "no newPayloadV4 at Amsterdam")
}
