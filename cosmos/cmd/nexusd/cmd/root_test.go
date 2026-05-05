package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/spf13/cobra"

	"nexus/genesis"
)

// newStartCmdForTest builds a minimal cobra command tree that mirrors the
// flag layout used by materializeEmbeddedGenesis: a root with persistent
// --chain and --home flags, and a `start` leaf. Flags are declared directly
// on the leaf so tests can call Flags().Set without invoking cobra.Execute.
func newStartCmdForTest(t *testing.T, home string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "start"}
	cmd.Flags().String(FlagChain, "", "")
	cmd.Flags().String(flags.FlagHome, home, "")
	return cmd
}

func TestMaterializeEmbeddedGenesis_NoOpWhenChainUnset(t *testing.T) {
	home := t.TempDir()
	start := newStartCmdForTest(t, home)

	if err := materializeEmbeddedGenesis(start); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "config", "genesis.json")); !os.IsNotExist(err) {
		t.Fatal("genesis.json must not be created when --chain is unset")
	}
}

func TestMaterializeEmbeddedGenesis_WritesForLocalnetOnStart(t *testing.T) {
	home := t.TempDir()
	start := newStartCmdForTest(t, home)
	if err := start.Flags().Set(FlagChain, "localnet"); err != nil {
		t.Fatal(err)
	}

	if err := materializeEmbeddedGenesis(start); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(home, "config", "genesis.json"))
	if err != nil {
		t.Fatalf("genesis.json was not written: %v", err)
	}
	want, err := genesis.Genesis("localnet")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("written genesis does not match embedded localnet genesis")
	}
}

func TestMaterializeEmbeddedGenesis_MaterializesTestnet(t *testing.T) {
	home := t.TempDir()
	start := newStartCmdForTest(t, home)
	if err := start.Flags().Set(FlagChain, "testnet"); err != nil {
		t.Fatal(err)
	}
	if err := materializeEmbeddedGenesis(start); err != nil {
		t.Fatalf("--chain testnet must be accepted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "config", "genesis.json")); err != nil {
		t.Fatal("testnet genesis must be materialized on start")
	}
}

func TestMaterializeEmbeddedGenesis_RejectsUnknownNetwork(t *testing.T) {
	home := t.TempDir()
	start := newStartCmdForTest(t, home)
	if err := start.Flags().Set(FlagChain, "does-not-exist"); err != nil {
		t.Fatal(err)
	}
	if err := materializeEmbeddedGenesis(start); err == nil {
		t.Fatal("--chain with unknown network must be rejected")
	}
}

// Validation runs before the command-name gate, so non-start commands also
// get a clear error if the user passes a bogus --chain value. Materialization
// itself, however, only happens for `start`.
func TestMaterializeEmbeddedGenesis_NonStartCommandSkipsWriteButValidates(t *testing.T) {
	home := t.TempDir()
	query := &cobra.Command{Use: "query"}
	query.Flags().String(FlagChain, "", "")
	query.Flags().String(flags.FlagHome, home, "")

	if err := query.Flags().Set(FlagChain, "localnet"); err != nil {
		t.Fatal(err)
	}
	if err := materializeEmbeddedGenesis(query); err != nil {
		t.Fatalf("valid --chain on non-start command should not error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "config", "genesis.json")); !os.IsNotExist(err) {
		t.Fatal("non-start commands must not materialize genesis")
	}

	if err := query.Flags().Set(FlagChain, "test"); err != nil {
		t.Fatal(err)
	}
	if err := materializeEmbeddedGenesis(query); err == nil {
		t.Fatal("invalid --chain value must error on any command")
	}
}

func TestMaterializeEmbeddedGenesis_OverwritesDifferingFile(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(home, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(configDir, "genesis.json")
	if err := os.WriteFile(target, []byte(`{"chain_id":"stale"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	start := newStartCmdForTest(t, home)
	if err := start.Flags().Set(FlagChain, "localnet"); err != nil {
		t.Fatal(err)
	}
	if err := materializeEmbeddedGenesis(start); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := genesis.Genesis("localnet")
	if !bytes.Equal(got, want) {
		t.Fatal("differing existing file must be overwritten with embedded bytes")
	}
}
