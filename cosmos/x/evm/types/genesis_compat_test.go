package types

import (
	"encoding/json"
	"testing"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
)

func TestNormalizeGenesisState_FillsEmptyExecutionHash(t *testing.T) {
	gs := GenesisState{}
	got := NormalizeGenesisState(gs)
	want := DefaultGenesis().GenesisExecutionBlockHash
	if got.GenesisExecutionBlockHash != want {
		t.Fatalf("expected GenesisExecutionBlockHash %q, got %q", want, got.GenesisExecutionBlockHash)
	}
}

// TestGenesisStateFromJSON_StdlibFallback verifies that a genesis JSON using the struct's
// snake_case json tags (as produced by older nexusd releases) is accepted via the stdlib JSON
// fallback path when the protobuf codec rejects it.
//
// The codec (protojson) requires camelCase field names; legacy files used snake_case, which
// matches the `json:"..."` tags on the generated Go struct — so stdlib json.Unmarshal succeeds
// where the codec fails.
func TestGenesisStateFromJSON_StdlibFallback(t *testing.T) {
	const legacyHash = "0x000000000000000000000000000000000000dead"

	// Build a JSON blob that uses snake_case keys (old format).
	// cdc.UnmarshalJSON expects camelCase ("genesisExecutionBlockHash") and will reject this.
	legacyJSON := json.RawMessage(`{"genesis_execution_block_hash":"` + legacyHash + `"}`)

	reg := codectypes.NewInterfaceRegistry()
	cdc := codec.NewProtoCodec(reg)

	gs, err := GenesisStateFromJSON(cdc, legacyJSON, nil)
	if err != nil {
		t.Fatalf("expected stdlib fallback to accept legacy snake_case genesis: %v", err)
	}
	if gs.GenesisExecutionBlockHash != legacyHash {
		t.Fatalf("expected GenesisExecutionBlockHash=%q, got %q", legacyHash, gs.GenesisExecutionBlockHash)
	}
}

// TestGenesisStateFromJSON_CanonicalFormat verifies that a valid protobuf-JSON genesis
// (camelCase, the format new nexusd writes) is accepted by the codec path directly — no fallback.
func TestGenesisStateFromJSON_CanonicalFormat(t *testing.T) {
	const canonicalHash = "0x000000000000000000000000000000000000beef"

	canonicalJSON := json.RawMessage(`{"genesisExecutionBlockHash":"` + canonicalHash + `"}`)

	reg := codectypes.NewInterfaceRegistry()
	cdc := codec.NewProtoCodec(reg)

	gs, err := GenesisStateFromJSON(cdc, canonicalJSON, nil)
	if err != nil {
		t.Fatalf("canonical format should be accepted directly: %v", err)
	}
	if gs.GenesisExecutionBlockHash != canonicalHash {
		t.Fatalf("expected GenesisExecutionBlockHash=%q, got %q", canonicalHash, gs.GenesisExecutionBlockHash)
	}
}
