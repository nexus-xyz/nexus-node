package testutil

import (
	"github.com/ethereum/go-ethereum/common"
)

const (
	// TestJWTSecret is a deterministic 32-byte hex string (64 hex chars)
	TestJWTSecret = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	TestDenom     = "atnex"
)

var (
	// DefaultStateHash is the hash expected by tests for parent hash validation
	DefaultStateHash = common.HexToHash("0x01")
	// DefaultStateHeight is the starting height used by tests
	DefaultStateHeight uint64 = 0
	// DefaultStateTimestamp is the starting timestamp used by tests
	DefaultStateTimestamp uint64 = 1000
)
