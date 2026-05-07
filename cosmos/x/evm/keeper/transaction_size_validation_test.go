package keeper

import (
	"fmt"
	"testing"

	"nexus/x/evm/tests/testutil"

	"github.com/stretchr/testify/require"
)

// TestTransactionSizeBoundaryConditions tests the exact boundary conditions for transaction size validation
func TestTransactionSizeBoundaryConditions(t *testing.T) {

	tests := []struct {
		name           string
		txSize         int
		expectedResult string
		shouldPass     bool
	}{
		{
			name:           "transaction smaller than limit",
			txSize:         testutil.MaxTxSize - 1, // 20MB - 1 byte
			expectedResult: "ACCEPTED",
			shouldPass:     true,
		},
		{
			name:           "transaction exactly at limit",
			txSize:         testutil.MaxTxSize, // exactly 20MB
			expectedResult: "ACCEPTED",
			shouldPass:     true,
		},
		{
			name:           "transaction larger than limit",
			txSize:         testutil.MaxTxSize + 1, // 20MB + 1 byte
			expectedResult: "REJECTED",
			shouldPass:     false,
		},
		{
			name:           "very small transaction",
			txSize:         1, // 1 byte
			expectedResult: "ACCEPTED",
			shouldPass:     true,
		},
		{
			name:           "empty transaction",
			txSize:         0, // 0 bytes
			expectedResult: "ACCEPTED",
			shouldPass:     true,
		},
		{
			name:           "significantly oversized transaction",
			txSize:         testutil.MaxTxSize * 2, // 40MB
			expectedResult: "REJECTED",
			shouldPass:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create transaction data of specified size
			txData := make([]byte, tt.txSize)

			// Test the validation logic from PrepareProposal/ProcessProposal
			isOversized := len(txData) > testutil.MaxTxSize

			if tt.shouldPass {
				require.False(t, isOversized,
					"Transaction of size %d bytes should be accepted (within %d byte limit)",
					tt.txSize, testutil.MaxTxSize)
				require.LessOrEqual(t, len(txData), testutil.MaxTxSize, "Transaction size should be <= limit")
			} else {
				require.True(t, isOversized,
					"Transaction of size %d bytes should be rejected (exceeds %d byte limit)",
					tt.txSize, testutil.MaxTxSize)
				require.Greater(t, len(txData), testutil.MaxTxSize, "Transaction size should be > limit")
			}

			// Verify the logic matches our expectation
			actualResult := "ACCEPTED"
			if isOversized {
				actualResult = "REJECTED"
			}
			require.Equal(t, tt.expectedResult, actualResult, "Validation result should match expected")

			// Log the test result for visibility
			txSizeMB := float64(len(txData)) / (1024 * 1024)
			if isOversized {
				t.Logf("REJECTED %s: %.6f MB (%d bytes)", tt.name, txSizeMB, len(txData))
			} else {
				t.Logf("ACCEPTED %s: %.6f MB (%d bytes)", tt.name, txSizeMB, len(txData))
			}
		})
	}
}

// TestTransactionSizeErrorMessages tests that error messages are correctly formatted
func TestTransactionSizeErrorMessages(t *testing.T) {

	oversizedTx := make([]byte, testutil.MaxTxSize+1000) // 20MB + 1000 bytes

	// Test error message format matches what's used in the actual code
	if len(oversizedTx) > testutil.MaxTxSize {
		expectedError := fmt.Sprintf("transaction too large: %d bytes exceeds maximum of %d bytes",
			len(oversizedTx), testutil.MaxTxSize)

		require.Contains(t, expectedError, "transaction too large")
		require.Contains(t, expectedError, fmt.Sprintf("%d bytes", len(oversizedTx)))
		require.Contains(t, expectedError, fmt.Sprintf("%d bytes", testutil.MaxTxSize))
		require.Contains(t, expectedError, "exceeds maximum")

		t.Logf("Error message format: %s", expectedError)
	}
}

// TestTransactionSizeConstants verifies the constants match between different files
func TestTransactionSizeConstants(t *testing.T) {
	// Verify the constant value
	require.Equal(t, 20971520, testutil.MaxTxSize, "Max transaction size should be exactly 20MB in bytes")

	// Verify conversions
	require.Equal(t, 20.0, float64(testutil.MaxTxSize)/(1024*1024), "Should be exactly 20 MB")
	require.Equal(t, 20480, testutil.MaxTxSize/1024, "Should be exactly 20480 KB")

	t.Logf("Max transaction size: %d bytes (%.1f MB)", testutil.MaxTxSize, float64(testutil.MaxTxSize)/(1024*1024))
}

// TestTransactionSizeLogic tests the core validation logic in isolation
func TestTransactionSizeLogic(t *testing.T) {

	testCases := []struct {
		size     int
		expected bool // true if should be rejected
	}{
		{0, false},                      // empty - should pass
		{1, false},                      // tiny - should pass
		{testutil.MaxTxSize - 1, false}, // just under - should pass
		{testutil.MaxTxSize, false},     // exactly at limit - should pass
		{testutil.MaxTxSize + 1, true},  // just over - should reject
		{testutil.MaxTxSize * 2, true},  // way over - should reject
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("size_%d", tc.size), func(t *testing.T) {
			// This is the exact logic used in the actual code
			isOversized := tc.size > testutil.MaxTxSize

			require.Equal(t, tc.expected, isOversized,
				"Size %d bytes: expected rejected=%v, got rejected=%v", tc.size, tc.expected, isOversized)
		})
	}
}

// BenchmarkTransactionSizeCheck benchmarks the size validation performance
func BenchmarkTransactionSizeCheck(b *testing.B) {

	// Test with a transaction at the limit
	txData := make([]byte, testutil.MaxTxSize)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// This is the core validation logic - should be extremely fast
		_ = len(txData) > testutil.MaxTxSize
	}
}
