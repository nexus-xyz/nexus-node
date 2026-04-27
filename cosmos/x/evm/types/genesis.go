package types

import (
	"fmt"
)

// DefaultGenesis returns the default genesis state
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params: DefaultParams(),
		// Default to empty hash, will be set during chain initialization
		GenesisExecutionBlockHash: "0x0000000000000000000000000000000000000000000000000000000000000000",
	}
}

// Validate performs basic genesis state validation returning an error upon any
// failure.
func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}

	if gs.GenesisExecutionBlockHash == "" {
		return fmt.Errorf("genesis execution block hash is required")
	}

	if len(gs.GenesisExecutionBlockHash) != 66 || gs.GenesisExecutionBlockHash[:2] != "0x" {
		return fmt.Errorf(
			"genesis execution block hash must be 66 characters long hex string with 0x prefix: %s",
			gs.GenesisExecutionBlockHash,
		)
	}

	// Validate current block hash if provided
	if gs.CurrentBlockHash != "" {
		if len(gs.CurrentBlockHash) != 66 || gs.CurrentBlockHash[:2] != "0x" {
			return fmt.Errorf(
				"current block hash must be 66 characters long hex string with 0x prefix: %s",
				gs.CurrentBlockHash,
			)
		}
	}

	return nil
}
