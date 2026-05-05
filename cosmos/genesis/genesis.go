// Package genesis provides the genesis files for built-in Nexus networks
// embedded into the nexusd binary.
package genesis

import (
	"embed"
	"fmt"
	"sort"
)

//go:embed chain-specs/localnet/cosmos.json chain-specs/devnet/cosmos.json chain-specs/testnet/cosmos.json chain-specs/mainnet/cosmos.json
var fs embed.FS

// genesisPaths maps a network name to the embedded cosmos genesis path.
var genesisPaths = map[string]string{
	"localnet": "chain-specs/localnet/cosmos.json",
	"devnet":   "chain-specs/devnet/cosmos.json",
	"testnet":  "chain-specs/testnet/cosmos.json",
	"mainnet":  "chain-specs/mainnet/cosmos.json",
}

// Names returns the sorted list of network names that have an embedded genesis.
func Names() []string {
	names := make([]string, 0, len(genesisPaths))
	for n := range genesisPaths {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// IsEmbedded reports whether the given network name has an embedded genesis.
func IsEmbedded(name string) bool {
	_, ok := genesisPaths[name]
	return ok
}

// Genesis returns the embedded genesis bytes for the given network.
// Returns an error if the network is not embedded.
func Genesis(name string) ([]byte, error) {
	path, ok := genesisPaths[name]
	if !ok {
		return nil, fmt.Errorf("no embedded genesis for %q (available: %v)", name, Names())
	}
	return fs.ReadFile(path)
}
