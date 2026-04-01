package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChainSpec_IsEngineV4PragueEnabled(t *testing.T) {
	tests := []struct {
		name      string
		chainSpec ChainSpec
		timestamp uint64
		expected  bool
	}{
		{
			name:      "Prague disabled - nil EngineV4PragueTimestamp",
			chainSpec: ChainSpec{EngineV4PragueTimestamp: nil},
			timestamp: 0,
			expected:  false,
		},
		{
			name:      "Prague disabled - nil EngineV4PragueTimestamp at any timestamp",
			chainSpec: ChainSpec{EngineV4PragueTimestamp: nil},
			timestamp: 100,
			expected:  false,
		},
		{
			name:      "Prague enabled at timestamp 0 - enabled from genesis",
			chainSpec: ChainSpec{EngineV4PragueTimestamp: uint64Ptr(0)},
			timestamp: 0,
			expected:  true,
		},
		{
			name:      "Prague enabled at timestamp 0 - enabled at timestamp 1",
			chainSpec: ChainSpec{EngineV4PragueTimestamp: uint64Ptr(0)},
			timestamp: 1,
			expected:  true,
		},
		{
			name:      "Prague enabled at timestamp 10 - disabled before activation",
			chainSpec: ChainSpec{EngineV4PragueTimestamp: uint64Ptr(10)},
			timestamp: 9,
			expected:  false,
		},
		{
			name:      "Prague enabled at timestamp 10 - enabled at activation timestamp",
			chainSpec: ChainSpec{EngineV4PragueTimestamp: uint64Ptr(10)},
			timestamp: 10,
			expected:  true,
		},
		{
			name:      "Prague enabled at timestamp 10 - enabled after activation",
			chainSpec: ChainSpec{EngineV4PragueTimestamp: uint64Ptr(10)},
			timestamp: 11,
			expected:  true,
		},
		{
			name:      "Prague enabled at timestamp 10 - enabled at much later timestamp",
			chainSpec: ChainSpec{EngineV4PragueTimestamp: uint64Ptr(10)},
			timestamp: 1000,
			expected:  true,
		},
		{
			name:      "Prague enabled at timestamp 1 - disabled at timestamp 0",
			chainSpec: ChainSpec{EngineV4PragueTimestamp: uint64Ptr(1)},
			timestamp: 0,
			expected:  false,
		},
		{
			name:      "Prague enabled at timestamp 1 - enabled at timestamp 1",
			chainSpec: ChainSpec{EngineV4PragueTimestamp: uint64Ptr(1)},
			timestamp: 1,
			expected:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.chainSpec.IsEngineV4PragueEnabled(tt.timestamp)
			require.Equal(t, tt.expected, result,
				"IsEngineV4PragueEnabled(%d) should return %v", tt.timestamp, tt.expected)
		})
	}
}

func uint64Ptr(v uint64) *uint64 {
	return &v
}
