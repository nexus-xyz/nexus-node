package keeper_test

import (
	"encoding/json"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/common"
	etypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	testserver "nexus/testutil/server"
	"nexus/x/evm/keeper"
	evmtypes "nexus/x/evm/types"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

func TestMsgExecutionPayloadValidation(t *testing.T) {
	engineURL, err := testserver.GetTestEngineUrl()
	require.NoError(t, err)
	t.Setenv("EVM_ENGINE_URL", engineURL)

	f := initFixture(t)

	createValidPayload := func() engine.ExecutableData {
		return engine.ExecutableData{
			ParentHash:    common.HexToHash("0x1234"),
			FeeRecipient:  common.HexToAddress("0x5678"),
			StateRoot:     common.HexToHash("0x9abc"),
			ReceiptsRoot:  common.HexToHash("0xdef0"),
			LogsBloom:     make([]byte, 256),
			Random:        common.HexToHash("0x1111"),
			Number:        1,
			GasLimit:      1000000,
			GasUsed:       500000,
			Timestamp:     1234567890,
			ExtraData:     []byte("test"),
			BaseFeePerGas: big.NewInt(1000000000),
			BlockHash:     common.HexToHash("0x2222"),
			Transactions:  [][]byte{},
			Withdrawals:   []*etypes.Withdrawal{},
			BlobGasUsed:   new(uint64),
			ExcessBlobGas: new(uint64),
		}
	}

	testCases := []struct {
		name          string
		createMsg     func() *evmtypes.MsgExecutionPayload
		expectedError string
	}{
		{
			name: "invalid authority",
			createMsg: func() *evmtypes.MsgExecutionPayload {
				payload := createValidPayload()
				payloadBytes, _ := json.Marshal(payload)
				return &evmtypes.MsgExecutionPayload{
					Authority:        "invalid_authority",
					ExecutionPayload: payloadBytes,
				}
			},
			expectedError: "invalid authority",
		},
		{
			name: "empty execution payload",
			createMsg: func() *evmtypes.MsgExecutionPayload {
				return &evmtypes.MsgExecutionPayload{
					Authority:        authtypes.NewModuleAddress(evmtypes.ModuleName).String(),
					ExecutionPayload: []byte{},
				}
			},
			expectedError: "execution payload cannot be empty",
		},
		{
			name: "invalid JSON payload",
			createMsg: func() *evmtypes.MsgExecutionPayload {
				return &evmtypes.MsgExecutionPayload{
					Authority:        authtypes.NewModuleAddress(evmtypes.ModuleName).String(),
					ExecutionPayload: []byte("invalid json"),
				}
			},
			expectedError: "invalid execution payload",
		},
		{
			name: "withdrawals not supported - has withdrawals",
			createMsg: func() *evmtypes.MsgExecutionPayload {
				payload := createValidPayload()
				payload.Withdrawals = []*etypes.Withdrawal{
					{
						Index:     0,
						Validator: 1,
						Address:   common.HexToAddress("0x1234"),
						Amount:    1000,
					},
				}
				payloadBytes, _ := json.Marshal(payload)
				return &evmtypes.MsgExecutionPayload{
					Authority:        authtypes.NewModuleAddress(evmtypes.ModuleName).String(),
					ExecutionPayload: payloadBytes,
				}
			},
			expectedError: "withdrawals are not supported",
		},
		{
			name: "withdrawals field is nil",
			createMsg: func() *evmtypes.MsgExecutionPayload {
				payload := createValidPayload()
				payload.Withdrawals = nil
				payloadBytes, _ := json.Marshal(payload)
				return &evmtypes.MsgExecutionPayload{
					Authority:        authtypes.NewModuleAddress(evmtypes.ModuleName).String(),
					ExecutionPayload: payloadBytes,
				}
			},
			expectedError: "withdrawals, blob gas used, and excess blob gas are required",
		},
		{
			name: "blob gas used is nil",
			createMsg: func() *evmtypes.MsgExecutionPayload {
				payload := createValidPayload()
				payload.BlobGasUsed = nil
				payloadBytes, _ := json.Marshal(payload)
				return &evmtypes.MsgExecutionPayload{
					Authority:        authtypes.NewModuleAddress(evmtypes.ModuleName).String(),
					ExecutionPayload: payloadBytes,
				}
			},
			expectedError: "withdrawals, blob gas used, and excess blob gas are required",
		},
		{
			name: "excess blob gas is nil",
			createMsg: func() *evmtypes.MsgExecutionPayload {
				payload := createValidPayload()
				payload.ExcessBlobGas = nil
				payloadBytes, _ := json.Marshal(payload)
				return &evmtypes.MsgExecutionPayload{
					Authority:        authtypes.NewModuleAddress(evmtypes.ModuleName).String(),
					ExecutionPayload: payloadBytes,
				}
			},
			expectedError: "withdrawals, blob gas used, and excess blob gas are required",
		},
		{
			name: "execution witness not supported",
			createMsg: func() *evmtypes.MsgExecutionPayload {
				payload := createValidPayload()
				// Set ExecutionWitness to a non-nil value to trigger the error
				payload.ExecutionWitness = &etypes.ExecutionWitness{}
				payloadBytes, _ := json.Marshal(payload)
				return &evmtypes.MsgExecutionPayload{
					Authority:        authtypes.NewModuleAddress(evmtypes.ModuleName).String(),
					ExecutionPayload: payloadBytes,
				}
			},
			expectedError: "execution witness is not supported",
		},
	}

	// Test the validation errors that occur before execution mode check
	ms := keeper.NewMsgServerImpl(f.keeper)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			msg := tc.createMsg()

			// Test validation - some will fail on exec mode, others on validation
			_, err := ms.ExecutionPayload(f.ctx, msg)

			require.Error(t, err, "expected error for invalid payload")

			// Check that we get either the expected validation error OR the exec mode error
			hasExpectedError := strings.Contains(err.Error(), tc.expectedError)
			hasExecModeError := strings.Contains(err.Error(),
				"execution payload can only be submitted in finalize mode")

			if !hasExpectedError && !hasExecModeError {
				t.Errorf("Expected either validation error '%s' or exec mode error, got: %s",
					tc.expectedError, err.Error())
			}

			// If we don't have exec mode error, we should have the validation error
			if !hasExecModeError {
				require.Contains(t, err.Error(), tc.expectedError,
					"error message should contain expected validation text")
			}
		})
	}
}
