package keeper_test

import (
	"testing"

	"nexus/x/evm/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestGenesis(t *testing.T) {
	genesisState := types.GenesisState{
		Params:                    types.DefaultParams(),
		GenesisExecutionBlockHash: "0x0000000000000000000000000000000000000000000000000000000000000000",
	}

	f := initFixture(t)
	err := f.keeper.InitGenesis(f.ctx, genesisState)
	require.NoError(t, err)
	got, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NotNil(t, got)

	require.EqualExportedValues(t, genesisState.Params, got.Params)
	require.Equal(t, genesisState.GenesisExecutionBlockHash, got.GenesisExecutionBlockHash)
}

func TestGenesisWithExecutionBlockHash(t *testing.T) {
	testHash := "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"
	genesisState := types.GenesisState{
		Params:                    types.DefaultParams(),
		GenesisExecutionBlockHash: testHash,
	}

	f := initFixture(t)
	err := f.keeper.InitGenesis(f.ctx, genesisState)
	require.NoError(t, err)

	// Verify block state was set
	hasBlockState, err := f.keeper.HasBlockState(f.ctx)
	require.NoError(t, err)
	require.True(t, hasBlockState)

	blockState, err := f.keeper.GetBlockState(f.ctx)
	require.NoError(t, err)
	require.Equal(t, testHash, blockState.Hash.Hex())
	require.Equal(t, uint64(0), blockState.Height)
	require.Equal(t, uint64(0), blockState.Timestamp)
}

func TestExportGenesisWithBlockState(t *testing.T) {
	f := initFixture(t)

	// Set up initial genesis state
	genesisState := types.GenesisState{
		Params:                    types.DefaultParams(),
		GenesisExecutionBlockHash: "0x1111111111111111111111111111111111111111111111111111111111111111",
	}

	err := f.keeper.InitGenesis(f.ctx, genesisState)
	require.NoError(t, err)

	// Update block state to simulate blockchain progression
	testBlockState := types.NewBlockState(
		common.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222"),
		123,
		1234567890,
	)
	err = f.keeper.SetBlockState(f.ctx, testBlockState)
	require.NoError(t, err)

	// Export genesis
	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NotNil(t, exported)

	// Verify exported values
	// The genesis execution block hash should be the current block hash when exporting
	require.Equal(t, testBlockState.Hash.Hex(), exported.GenesisExecutionBlockHash)
	require.Equal(t, testBlockState.Hash.Hex(), exported.CurrentBlockHash)
	require.Equal(t, testBlockState.Height, exported.CurrentBlockHeight)
	require.Equal(t, testBlockState.Timestamp, exported.CurrentBlockTimestamp)
}

func TestExportGenesisWithoutBlockState(t *testing.T) {
	f := initFixture(t)

	// Only set params, no block state
	err := f.keeper.Params.Set(f.ctx, types.DefaultParams())
	require.NoError(t, err)

	// Export genesis
	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NotNil(t, exported)

	// Verify that block state fields are empty when no block state exists
	require.Equal(t, "", exported.CurrentBlockHash)
	require.Equal(t, uint64(0), exported.CurrentBlockHeight)
	require.Equal(t, uint64(0), exported.CurrentBlockTimestamp)
}

func TestInitGenesisWithCurrentBlockState(t *testing.T) {
	testHash := "0x3333333333333333333333333333333333333333333333333333333333333333"
	testHeight := uint64(456)
	testTimestamp := uint64(9876543210)

	genesisState := types.GenesisState{
		Params:                    types.DefaultParams(),
		GenesisExecutionBlockHash: "0x1111111111111111111111111111111111111111111111111111111111111111",
		CurrentBlockHash:          testHash,
		CurrentBlockHeight:        testHeight,
		CurrentBlockTimestamp:     testTimestamp,
	}

	f := initFixture(t)
	err := f.keeper.InitGenesis(f.ctx, genesisState)
	require.NoError(t, err)

	// Verify current block state was set (not genesis execution block hash)
	blockState, err := f.keeper.GetBlockState(f.ctx)
	require.NoError(t, err)
	require.Equal(t, testHash, blockState.Hash.Hex())
	require.Equal(t, testHeight, blockState.Height)
	require.Equal(t, testTimestamp, blockState.Timestamp)
}

func TestInitGenesisWithoutCurrentBlockState(t *testing.T) {
	genesisHash := "0x4444444444444444444444444444444444444444444444444444444444444444"
	genesisState := types.GenesisState{
		Params:                    types.DefaultParams(),
		GenesisExecutionBlockHash: genesisHash,
		// CurrentBlockHash is empty, should fall back to genesis execution block hash
	}

	f := initFixture(t)
	err := f.keeper.InitGenesis(f.ctx, genesisState)
	require.NoError(t, err)

	// Verify genesis execution block hash was used with default height and timestamp
	blockState, err := f.keeper.GetBlockState(f.ctx)
	require.NoError(t, err)
	require.Equal(t, genesisHash, blockState.Hash.Hex())
	require.Equal(t, uint64(0), blockState.Height)
	require.Equal(t, uint64(0), blockState.Timestamp)
}

func TestGenesisExportImportCycle(t *testing.T) {
	f := initFixture(t)

	// Set up initial state
	originalGenesis := types.GenesisState{
		Params:                    types.DefaultParams(),
		GenesisExecutionBlockHash: "0x5555555555555555555555555555555555555555555555555555555555555555",
	}

	err := f.keeper.InitGenesis(f.ctx, originalGenesis)
	require.NoError(t, err)

	// Simulate blockchain progression
	progressedBlockState := types.NewBlockState(
		common.HexToHash("0x6666666666666666666666666666666666666666666666666666666666666666"),
		789,
		1111111111,
	)
	err = f.keeper.SetBlockState(f.ctx, progressedBlockState)
	require.NoError(t, err)

	// Export the current state
	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)

	// Create a new fixture and import the exported state
	f2 := initFixture(t)
	err = f2.keeper.InitGenesis(f2.ctx, *exported)
	require.NoError(t, err)

	// Verify the imported state matches the original progressed state
	importedBlockState, err := f2.keeper.GetBlockState(f2.ctx)
	require.NoError(t, err)
	require.Equal(t, progressedBlockState.Hash.Hex(), importedBlockState.Hash.Hex())
	require.Equal(t, progressedBlockState.Height, importedBlockState.Height)
	require.Equal(t, progressedBlockState.Timestamp, importedBlockState.Timestamp)

	// Export again and verify consistency
	exported2, err := f2.keeper.ExportGenesis(f2.ctx)
	require.NoError(t, err)
	require.Equal(t, exported.CurrentBlockHash, exported2.CurrentBlockHash)
	require.Equal(t, exported.CurrentBlockHeight, exported2.CurrentBlockHeight)
	require.Equal(t, exported.CurrentBlockTimestamp, exported2.CurrentBlockTimestamp)
}
