package keeper

import (
	"encoding/json"
	"testing"
	"time"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/ethereum/go-ethereum/beacon/engine"

	storetypes "cosmossdk.io/store/types"
	sdktestutil "github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"nexus/x/evm/tests/testutil"
	evmtypes "nexus/x/evm/types"
)

// generatePayload creates a new payload (JSON) with the given `timestamp`
func generatePayload(timestamp uint64) string {
	payload := testutil.BuildPayload()
	payload.Timestamp = timestamp

	return testutil.PayloadToString(payload)
}

// initContext creates a new sdk.Context
func initContext(t *testing.T) sdk.Context {
	storeKey := storetypes.NewKVStoreKey(evmtypes.StoreKey)
	ctx := sdktestutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_test")).Ctx

	header := ctx.BlockHeader()
	header.Height = int64(testutil.DefaultStateTimestamp + 1)
	ctx = ctx.WithBlockHeader(header)
	// Set BlockTime to match the expected timestamp for legacy calculation
	// The payload timestamp is DefaultStateTimestamp + 1, so set BlockTime accordingly
	expectedTimestamp := testutil.DefaultStateTimestamp + 1
	ctx = ctx.WithBlockTime(time.Unix(int64(expectedTimestamp), 0))

	return ctx
}

// initKeeper creates a new Keeper
func initKeeper() Keeper {
	return Keeper{}
}

// FuzzValidatePayload tests execution payload validation with random JSON inputs
func FuzzValidatePayload(f *testing.F) {
	// Seed corpus. The first case is valid. The second is intentionally minimal.
	f.Add(generatePayload(0))
	f.Add("{}")

	f.Fuzz(func(t *testing.T, jsonInput string) {
		// Create a valid MsgExecutionPayload with the authority
		msg := &evmtypes.MsgExecutionPayload{
			Authority:        authtypes.NewModuleAddress(evmtypes.ModuleName).String(),
			ExecutionPayload: []byte(jsonInput),
		}

		// Test validatePayload - should not panic with any JSON input
		// We use a zero state: hash/height/timestamp are 0 to exercise transitions from genesis.
		ctx := initContext(t)
		keeper := initKeeper()

		payload, err := keeper.validatePayload(
			ctx,
			msg,
			&evmtypes.BlockState{
				Hash:      testutil.DefaultStateHash,
				Height:    0,
				Timestamp: 0,
			},
		)

		if err == nil {
			// If validation succeeded, check invariants
			if payload.Withdrawals == nil {
				t.Error("Withdrawals should not be nil for valid payload")
			}
			if payload.BlobGasUsed == nil {
				t.Error("BlobGasUsed should not be nil for valid payload")
			}
			if payload.ExcessBlobGas == nil {
				t.Error("ExcessBlobGas should not be nil for valid payload")
			}
			if len(payload.Withdrawals) > 0 {
				t.Error("Withdrawals should be empty (not supported)")
			}
			if payload.ExecutionWitness != nil {
				t.Error("ExecutionWitness should be nil (not supported)")
			}
		}
		// Most random inputs will be invalid JSON or invalid payloads, which is expected
	})
}

// FuzzValidatePayloadAuthority tests payload validation with random authority strings
func FuzzValidatePayloadAuthority(f *testing.F) {
	validPayload := generatePayload(testutil.DefaultStateTimestamp)

	// Minimal seed - let fuzzer generate random authority strings
	f.Add(authtypes.NewModuleAddress(evmtypes.ModuleName).String()) // One valid example
	f.Add("")                                                       // One edge case

	f.Fuzz(func(t *testing.T, authority string) {
		msg := &evmtypes.MsgExecutionPayload{
			Authority:        authority,
			ExecutionPayload: []byte(validPayload),
		}

		ctx := initContext(t)
		keeper := initKeeper()

		// Test validatePayload - should not panic with any authority string
		_, err := keeper.validatePayload(
			ctx,
			msg,
			&evmtypes.BlockState{
				Hash:      testutil.DefaultStateHash,
				Height:    0,
				Timestamp: 0,
			},
		)

		// Only the correct module address should be valid
		expectedAuthority := authtypes.NewModuleAddress(evmtypes.ModuleName).String()
		if authority == expectedAuthority {
			if err != nil {
				t.Errorf("Expected no error for valid authority %s, got: %v", authority, err)
			}
		} else {
			if err == nil {
				t.Errorf("Expected error for invalid authority %s", authority)
			}
		}
	})
}

// FuzzJSONUnmarshalExecutionPayload tests JSON unmarshaling with completely random data
func FuzzJSONUnmarshalExecutionPayload(f *testing.F) {
	// Minimal seed - let fuzzer generate completely random JSON-like strings
	f.Add(`{"Number": 1}`) // One simple valid case
	f.Add("")              // Empty string

	f.Fuzz(func(t *testing.T, jsonInput string) {
		var payload engine.ExecutableData

		// Test JSON unmarshaling - should not panic with any random input
		err := json.Unmarshal([]byte(jsonInput), &payload)

		// We don't check for specific errors since most random strings
		// will not be valid JSON, which is expected behavior
		_ = err
	})
}
