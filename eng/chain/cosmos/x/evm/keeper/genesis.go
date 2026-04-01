package keeper

import (
	"context"
	"errors"

	"nexus/x/evm/types"

	"github.com/ethereum/go-ethereum/common"
)

// InitGenesis initializes the module's state from a provided genesis state.
func (k Keeper) InitGenesis(ctx context.Context, genState types.GenesisState) error {
	// Set module parameters
	if err := k.Params.Set(ctx, genState.Params); err != nil {
		return err
	}

	if genState.GenesisExecutionBlockHash == "" {
		return errors.New("genesis execution block hash is required")
	}

	// Use current block state if provided, otherwise use genesis execution block hash
	var blockState types.BlockState
	if genState.CurrentBlockHash != "" {
		// Import existing block state
		blockHash := common.HexToHash(genState.CurrentBlockHash)
		blockState = types.NewBlockState(blockHash, genState.CurrentBlockHeight, genState.CurrentBlockTimestamp)
	} else {
		// Initialize with genesis execution block hash
		blockHash := common.HexToHash(genState.GenesisExecutionBlockHash)
		blockState = types.NewBlockState(blockHash, 0, 0)
	}

	if err := k.SetBlockState(ctx, blockState); err != nil {
		return err
	}

	return nil
}

// ExportGenesis returns the module's exported genesis.
func (k Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	var err error

	genesis := &types.GenesisState{}

	// Get module parameters
	genesis.Params, err = k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	// Export current block state if it exists
	blockState, err := k.GetBlockState(ctx)
	if err != nil {
		// If no block state exists yet, use default genesis
		defaultGenesis := types.DefaultGenesis()
		genesis.GenesisExecutionBlockHash = defaultGenesis.GenesisExecutionBlockHash
		return genesis, nil
	}

	// Use the current block hash as the next "genesis" execution
	// block hash. This is useful for imports once the chain is
	// already running.
	genesis.GenesisExecutionBlockHash = blockState.Hash.Hex()

	genesis.CurrentBlockHash = blockState.Hash.Hex()
	genesis.CurrentBlockHeight = blockState.Height
	genesis.CurrentBlockTimestamp = blockState.Timestamp

	return genesis, nil
}
