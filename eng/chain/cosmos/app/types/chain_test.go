package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChainSpec_IsPragueActive(t *testing.T) {
	tests := []struct {
		name      string
		chainSpec ChainSpec
		timestamp uint64
		expected  bool
	}{
		{
			name:      "Prague disabled - nil PragueTimestamp",
			chainSpec: ChainSpec{PragueTimestamp: nil},
			timestamp: 0,
			expected:  false,
		},
		{
			name:      "Prague disabled - nil PragueTimestamp at any timestamp",
			chainSpec: ChainSpec{PragueTimestamp: nil},
			timestamp: 100,
			expected:  false,
		},
		{
			name:      "Prague enabled at timestamp 0 - enabled from genesis",
			chainSpec: ChainSpec{PragueTimestamp: uint64Ptr(0)},
			timestamp: 0,
			expected:  true,
		},
		{
			name:      "Prague enabled at timestamp 0 - enabled at timestamp 1",
			chainSpec: ChainSpec{PragueTimestamp: uint64Ptr(0)},
			timestamp: 1,
			expected:  true,
		},
		{
			name:      "Prague enabled at timestamp 10 - disabled before activation",
			chainSpec: ChainSpec{PragueTimestamp: uint64Ptr(10)},
			timestamp: 9,
			expected:  false,
		},
		{
			name:      "Prague enabled at timestamp 10 - enabled at activation timestamp",
			chainSpec: ChainSpec{PragueTimestamp: uint64Ptr(10)},
			timestamp: 10,
			expected:  true,
		},
		{
			name:      "Prague enabled at timestamp 10 - enabled after activation",
			chainSpec: ChainSpec{PragueTimestamp: uint64Ptr(10)},
			timestamp: 11,
			expected:  true,
		},
		{
			name:      "Prague enabled at timestamp 10 - enabled at much later timestamp",
			chainSpec: ChainSpec{PragueTimestamp: uint64Ptr(10)},
			timestamp: 1000,
			expected:  true,
		},
		{
			name:      "Prague enabled at timestamp 1 - disabled at timestamp 0",
			chainSpec: ChainSpec{PragueTimestamp: uint64Ptr(1)},
			timestamp: 0,
			expected:  false,
		},
		{
			name:      "Prague enabled at timestamp 1 - enabled at timestamp 1",
			chainSpec: ChainSpec{PragueTimestamp: uint64Ptr(1)},
			timestamp: 1,
			expected:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.chainSpec.IsPragueActive(tt.timestamp)
			require.Equal(t, tt.expected, result,
				"IsPragueActive(%d) should return %v", tt.timestamp, tt.expected)
		})
	}
}

func TestChainSpec_IsOsakaActive(t *testing.T) {
	tests := []struct {
		name      string
		chainSpec ChainSpec
		ts        uint64
		expected  bool
	}{
		{
			name:      "Osaka disabled - nil OsakaTimestamp",
			chainSpec: ChainSpec{OsakaTimestamp: nil},
			ts:        0,
			expected:  false,
		},
		{
			name:      "Osaka enabled at 0 - from genesis",
			chainSpec: ChainSpec{OsakaTimestamp: uint64Ptr(0)},
			ts:        0,
			expected:  true,
		},
		{
			name:      "Osaka at 20 - before activation",
			chainSpec: ChainSpec{OsakaTimestamp: uint64Ptr(20)},
			ts:        19,
			expected:  false,
		},
		{
			name:      "Osaka at 20 - at activation",
			chainSpec: ChainSpec{OsakaTimestamp: uint64Ptr(20)},
			ts:        20,
			expected:  true,
		},
		{
			name:      "Osaka at 20 - after",
			chainSpec: ChainSpec{OsakaTimestamp: uint64Ptr(20)},
			ts:        100,
			expected:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.chainSpec.IsOsakaActive(tt.ts)
			require.Equal(t, tt.expected, result, "IsOsakaActive(%d) should return %v", tt.ts, tt.expected)
		})
	}
}

func TestChainSpec_EngineRPCVersionMatrix(t *testing.T) {
	// execution-apis: Osaka → getPayloadV5; Amsterdam → newPayloadV5.
	prague := uint64Ptr(100)
	osaka := uint64Ptr(200)
	amsterdam := uint64Ptr(300)
	spec := ChainSpec{
		PragueTimestamp:    prague,
		OsakaTimestamp:     osaka,
		AmsterdamTimestamp: amsterdam,
	}
	require.NoError(t, spec.Validate())

	t.Run("before Prague: V3 for both paths", func(t *testing.T) {
		ts := uint64(50)
		require.Equal(t, 3, spec.GetPayloadEngineRPCVersion(ts))
		require.Equal(t, 3, spec.NewPayloadEngineRPCVersion(ts))
	})
	t.Run("Prague only: V4 newPayload, V4 getPayload", func(t *testing.T) {
		ts := uint64(150)
		require.Equal(t, 4, spec.GetPayloadEngineRPCVersion(ts))
		require.Equal(t, 4, spec.NewPayloadEngineRPCVersion(ts))
	})
	t.Run("Osaka: getPayload V5, newPayload still V4 until Amsterdam", func(t *testing.T) {
		ts := uint64(250)
		require.Equal(t, 5, spec.GetPayloadEngineRPCVersion(ts))
		require.Equal(t, 4, spec.NewPayloadEngineRPCVersion(ts),
			"Osaka blocks must use engine_newPayloadV4 until Amsterdam activates")
	})
	t.Run("Amsterdam: newPayload V5", func(t *testing.T) {
		ts := uint64(350)
		require.Equal(t, 5, spec.GetPayloadEngineRPCVersion(ts))
		require.Equal(t, 5, spec.NewPayloadEngineRPCVersion(ts))
	})
}

func TestChainSpec_IsAmsterdamActive(t *testing.T) {
	t.Run("disabled when nil", func(t *testing.T) {
		spec := ChainSpec{PragueTimestamp: uint64Ptr(5), OsakaTimestamp: uint64Ptr(10)}
		require.False(t, spec.IsAmsterdamActive(100))
	})
	t.Run("enabled at threshold", func(t *testing.T) {
		spec := ChainSpec{
			PragueTimestamp:    uint64Ptr(5),
			OsakaTimestamp:     uint64Ptr(10),
			AmsterdamTimestamp: uint64Ptr(30),
		}
		require.False(t, spec.IsAmsterdamActive(29))
		require.True(t, spec.IsAmsterdamActive(30))
	})
}

func TestChainSpec_Validate(t *testing.T) {
	t.Run("all nil", func(t *testing.T) {
		require.NoError(t, (ChainSpec{}).Validate())
	})
	t.Run("only prague", func(t *testing.T) {
		require.NoError(t, (ChainSpec{PragueTimestamp: uint64Ptr(10)}).Validate())
	})
	t.Run("osaka without prague", func(t *testing.T) {
		require.Error(t, (ChainSpec{OsakaTimestamp: uint64Ptr(20)}).Validate())
	})
	t.Run("osaka > prague", func(t *testing.T) {
		require.NoError(t, (ChainSpec{
			PragueTimestamp: uint64Ptr(10),
			OsakaTimestamp:  uint64Ptr(20),
		}).Validate())
	})
	t.Run("osaka == prague (co-activation)", func(t *testing.T) {
		require.NoError(t, (ChainSpec{
			PragueTimestamp: uint64Ptr(10),
			OsakaTimestamp:  uint64Ptr(10),
		}).Validate())
	})
	t.Run("osaka < prague", func(t *testing.T) {
		require.Error(t, (ChainSpec{
			PragueTimestamp: uint64Ptr(20),
			OsakaTimestamp:  uint64Ptr(10),
		}).Validate())
	})
	t.Run("amsterdam without osaka", func(t *testing.T) {
		require.Error(t, (ChainSpec{
			PragueTimestamp:    uint64Ptr(10),
			AmsterdamTimestamp: uint64Ptr(40),
		}).Validate())
	})
	t.Run("amsterdam without prague or osaka", func(t *testing.T) {
		require.Error(t, (ChainSpec{
			AmsterdamTimestamp: uint64Ptr(40),
		}).Validate())
	})
	t.Run("amsterdam < osaka", func(t *testing.T) {
		require.Error(t, (ChainSpec{
			PragueTimestamp:    uint64Ptr(5),
			OsakaTimestamp:     uint64Ptr(20),
			AmsterdamTimestamp: uint64Ptr(10),
		}).Validate())
	})
	t.Run("amsterdam == osaka (co-activation)", func(t *testing.T) {
		require.NoError(t, (ChainSpec{
			PragueTimestamp:    uint64Ptr(5),
			OsakaTimestamp:     uint64Ptr(20),
			AmsterdamTimestamp: uint64Ptr(20),
		}).Validate())
	})
	t.Run("full chain prague <= osaka <= amsterdam", func(t *testing.T) {
		require.NoError(t, (ChainSpec{
			PragueTimestamp:    uint64Ptr(10),
			OsakaTimestamp:     uint64Ptr(10),
			AmsterdamTimestamp: uint64Ptr(10),
		}).Validate())
	})
}

func uint64Ptr(v uint64) *uint64 {
	return &v
}
