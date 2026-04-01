package lib

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"math/big"

	"nexus/lib"
	testserver "nexus/testutil/server"

	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
)

const (
	TEST_JWT_SECRET = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

func TestNewAuthClient(t *testing.T) {
	engineURL, err := testserver.GetTestEngineUrl()
	require.NoError(t, err)
	t.Setenv("EVM_ENGINE_URL", engineURL)

	secretBytes, err := hex.DecodeString(TEST_JWT_SECRET)
	require.NoError(t, err)
	jwtSecret := lib.NewJwtSecret(secretBytes)

	// Test successful client creation
	client, err := lib.NewAuthClient(context.Background(), engineURL, jwtSecret)
	require.NoError(t, err)
	require.NotNil(t, client)
}

func TestEngineClientMethods(t *testing.T) {
	secretBytes, err := hex.DecodeString(TEST_JWT_SECRET)
	require.NoError(t, err)
	jwtSecret := lib.NewJwtSecret(secretBytes)

	// Create a test server that mimics the engine API
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify JWT token is present
		auth := r.Header.Get("Authorization")
		require.Contains(t, auth, "Bearer")

		var req struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
			ID     interface{}       `json:"id"`
		}

		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)

		var response interface{}

		switch req.Method {
		case "engine_newPayloadV4":
			response = engine.PayloadStatusV1{Status: engine.VALID}
		case "engine_forkchoiceUpdatedV3":
			payloadID := engine.PayloadID{1, 2, 3, 4, 5, 6, 7, 8}
			response = engine.ForkChoiceResponse{
				PayloadStatus: engine.PayloadStatusV1{Status: engine.VALID},
				PayloadID:     &payloadID,
			}
		case "engine_getPayloadV4":
			response = &engine.ExecutionPayloadEnvelope{
				ExecutionPayload: &engine.ExecutableData{
					BlockHash:     common.HexToHash("0x123"),
					ParentHash:    common.HexToHash("0x456"),
					Number:        1,
					Timestamp:     uint64(time.Now().Unix()),
					BaseFeePerGas: big.NewInt(1000000000), // 1 Gwei
					GasLimit:      8000000,
					GasUsed:       0,
					Transactions:  [][]byte{},
					Withdrawals:   []*types.Withdrawal{},
				},
				BlockValue: big.NewInt(0),
			}
		}

		resp := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  response,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Create authenticated client
	client, err := lib.NewAuthClient(context.Background(), server.URL, jwtSecret)
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("NewPayloadV4", func(t *testing.T) {
		payload := engine.ExecutableData{
			BlockHash: common.HexToHash("0x123"),
			Number:    1,
			Timestamp: uint64(time.Now().Unix()),
		}

		status, err := client.NewPayloadV4(ctx, payload, []common.Hash{}, &common.Hash{}, nil)
		require.NoError(t, err)
		require.Equal(t, engine.VALID, status.Status)
	})

	t.Run("ForkchoiceUpdatedV3", func(t *testing.T) {
		fcState := engine.ForkchoiceStateV1{
			HeadBlockHash: common.HexToHash("0x123"),
		}
		attrs := &engine.PayloadAttributes{
			Timestamp: uint64(time.Now().Unix()),
		}

		response, err := client.ForkchoiceUpdatedV3(ctx, fcState, attrs)
		require.NoError(t, err)
		require.Equal(t, engine.VALID, response.PayloadStatus.Status)
		require.NotNil(t, response.PayloadID)
	})

	t.Run("GetPayloadV4", func(t *testing.T) {
		payloadID := engine.PayloadID{1, 2, 3, 4, 5, 6, 7, 8}

		envelope, err := client.GetPayloadV4(ctx, payloadID)
		require.NoError(t, err)
		require.NotNil(t, envelope)
		require.NotNil(t, envelope.ExecutionPayload)
	})
}

func TestForkchoiceAcceptedError(t *testing.T) {
	secretBytes, err := hex.DecodeString(TEST_JWT_SECRET)
	require.NoError(t, err)
	jwtSecret := lib.NewJwtSecret(secretBytes)

	// Create a test server that returns ACCEPTED status for forkchoice
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
			ID     interface{}       `json:"id"`
		}

		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)

		var response interface{}

		if req.Method == "engine_forkchoiceUpdatedV3" {
			response = engine.ForkChoiceResponse{
				PayloadStatus: engine.PayloadStatusV1{Status: "ACCEPTED"},
				PayloadID:     nil,
			}
		}

		resp := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  response,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Create authenticated client
	client, err := lib.NewAuthClient(context.Background(), server.URL, jwtSecret)
	require.NoError(t, err)

	ctx := context.Background()

	fcState := engine.ForkchoiceStateV1{
		HeadBlockHash: common.HexToHash("0x123"),
	}
	attrs := &engine.PayloadAttributes{
		Timestamp: uint64(time.Now().Unix()),
	}

	_, err = client.ForkchoiceUpdatedV3(ctx, fcState, attrs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unexpected status accepted")
}
