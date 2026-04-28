package testutil

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// SetupJWT creates a temp jwt.hex file and exports the env var.
func SetupJWT(t *testing.T) {
	t.Helper()
	tempDir := t.TempDir()
	jwtFile := filepath.Join(tempDir, "jwt.hex")
	if err := os.WriteFile(jwtFile, []byte(TestJWTSecret), 0644); err != nil {
		t.Fatalf("failed to write JWT file: %v", err)
	}
	t.Setenv("EVM_ENGINE_JWT_SECRET_PATH", jwtFile)
}

// BuildPayload creates a valid ExecutableData with all required fields
func BuildPayload() engine.ExecutableData {
	return engine.ExecutableData{
		ParentHash:       DefaultStateHash,
		FeeRecipient:     common.HexToAddress(RandomHex(20)),
		StateRoot:        common.HexToHash(RandomHex(32)),
		ReceiptsRoot:     common.HexToHash(RandomHex(32)),
		LogsBloom:        make([]byte, 256),
		Random:           common.HexToHash(RandomHex(32)),
		Number:           1,
		GasLimit:         30000000,
		GasUsed:          21000,
		Timestamp:        DefaultStateTimestamp + 1,
		ExtraData:        []byte("test data"),
		BaseFeePerGas:    big.NewInt(1000000000),
		BlockHash:        common.HexToHash(RandomHex(32)),
		Transactions:     [][]byte{},
		Withdrawals:      []*types.Withdrawal{},
		BlobGasUsed:      &[]uint64{0}[0],
		ExcessBlobGas:    &[]uint64{0}[0],
		ExecutionWitness: nil,
	}
}

// RandomHex generates a random hex string of length `length`
func RandomHex(length int) string {
	bytes := make([]byte, length)
	_, err := rand.Read(bytes)
	if err != nil {
		panic(err)
	}
	return "0x" + hex.EncodeToString(bytes)
}

func BuildPayloadString() string {
	return PayloadToString(BuildPayload())
}

// PayloadToString converts the payload to a JSON string
func PayloadToString(engine.ExecutableData) string {
	payload := BuildPayload()

	bytes, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}

	return string(bytes)
}
