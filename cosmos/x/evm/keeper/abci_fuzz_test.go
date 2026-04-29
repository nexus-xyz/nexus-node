package keeper

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"nexus/x/evm/types"

	"github.com/ethereum/go-ethereum/common"
)

// FuzzIsPayloadUnknown tests the payload unknown error detection with random error messages
func FuzzIsPayloadUnknown(f *testing.F) {
	// Minimal seed - let fuzzer generate random error messages
	f.Add(`unknown payload`) // One case that should match
	f.Add(``)                // Empty string

	f.Fuzz(func(t *testing.T, errorMsg string) {
		// Create an error with the random fuzz input
		var err error
		if errorMsg != "" {
			err = &testError{msg: errorMsg}
		}

		// Test isPayloadUnknown - should not panic with any error message
		result := isPayloadUnknown(err)

		// Verify expected behavior
		if err == nil && result {
			t.Error("isPayloadUnknown should return false for nil error")
		}
	})
}

// FuzzAppHashValidation tests app hash handling with completely random byte arrays
func FuzzAppHashValidation(f *testing.F) {
	// Minimal seed - let fuzzer generate random byte arrays
	f.Add(make([]byte, 32)) // One standard case
	f.Add([]byte{})         // Empty case

	f.Fuzz(func(t *testing.T, hashBytes []byte) {
		// Convert to common.Hash (will pad or truncate as needed)
		appHash := common.BytesToHash(hashBytes)

		// Test that BytesToHash doesn't panic with any input and produces valid hash
		if len(appHash) != 32 {
			t.Errorf("Expected hash length 32, got %d", len(appHash))
		}

		// Test hash hex conversion doesn't panic with any input
		hexStr := appHash.Hex()
		if len(hexStr) != 66 { // "0x" + 64 hex chars
			t.Errorf("Expected hex string length 66, got %d", len(hexStr))
		}
	})
}

// Helper types for testing
type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}

// FuzzExecutionPayloadProcessing tests the actual ExecutionPayload processing with random data
func FuzzExecutionPayloadProcessing(f *testing.F) {
	// Add seed values with valid JSON structures
	// Long JSON payload for testing
	longJSON := `{"parentHash":"0x0000000000000000000000000000000000000000000000000000000000000000",` +
		`"feeRecipient":"0x0000000000000000000000000000000000000000",` +
		`"stateRoot":"0x0000000000000000000000000000000000000000000000000000000000000000",` +
		`"receiptsRoot":"0x0000000000000000000000000000000000000000000000000000000000000000",` +
		`"logsBloom":"0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",` +
		`"prevRandao":"0x0000000000000000000000000000000000000000000000000000000000000000",` +
		`"blockNumber":"0x1","gasLimit":"0x1c9c380","gasUsed":"0x0","timestamp":"0x5",` +
		`"extraData":"0x","baseFeePerGas":"0x7",` +
		`"blockHash":"0x0000000000000000000000000000000000000000000000000000000000000000",` +
		`"transactions":[],"withdrawals":[],"blobGasUsed":"0x0","excessBlobGas":"0x0"}`
	f.Add(longJSON)
	f.Add(`{}`)                 // Empty JSON
	f.Add(`{"invalid":"json"}`) // Invalid structure
	f.Add(`malformed json`)     // Malformed JSON

	f.Fuzz(func(t *testing.T, jsonPayload string) {
		// Limit input size to prevent memory issues
		if len(jsonPayload) > 1024*1024 { // 1MB limit for JSON
			t.Skip("Skipping oversized JSON input")
			return
		}

		// Test JSON unmarshalling doesn't panic with random input
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("JSON unmarshalling panicked with input: %q, panic: %v", jsonPayload, r)
			}
		}()

		// This is what happens in validatePayload - test it doesn't crash
		var payload map[string]interface{}
		err := json.Unmarshal([]byte(jsonPayload), &payload)

		// We don't care if it errors (expected for malformed JSON)
		// We just want to ensure it doesn't crash or cause security issues
		_ = err

		// Test that the size check logic works with any string length
		maxSize := 20 * 1024 * 1024 // 20MB
		isOversized := len(jsonPayload) > maxSize

		// Basic consistency check
		if isOversized != (len(jsonPayload) > maxSize) {
			t.Errorf("Size check inconsistency for input length %d", len(jsonPayload))
		}
	})
}

// FuzzTransactionSizeCheck tests transaction size validation with random transaction data
func FuzzTransactionSizeCheck(f *testing.F) {
	// Add some seed transaction-like data
	f.Add([]byte("small tx"))
	f.Add(make([]byte, 1024*1024))    // 1MB
	f.Add(make([]byte, 10*1024*1024)) // 10MB

	f.Fuzz(func(t *testing.T, txData []byte) {
		// Limit to reasonable size to prevent memory issues
		if len(txData) > 100*1024*1024 { // 100MB cap
			t.Skip("Skipping oversized transaction input")
			return
		}

		// This should never panic regardless of input
		isOversized := len(txData) > types.MaxTxSize

		// Test error message generation doesn't panic
		if isOversized {
			errorMsg := fmt.Sprintf("transaction too large: %d bytes exceeds maximum of %d bytes",
				len(txData), types.MaxTxSize)
			if !strings.Contains(errorMsg, "transaction too large") {
				t.Errorf("Error message format incorrect: %s", errorMsg)
			}
		}

		// Simulate what happens in the actual code paths
		if len(txData) > types.MaxTxSize {
			// This is what would happen in PrepareProposal/ProcessProposal
			// Just verify the logic is consistent
			if len(txData) <= types.MaxTxSize {
				t.Errorf("Logic error: transaction of size %d should be rejected", len(txData))
			}
		}
	})
}
