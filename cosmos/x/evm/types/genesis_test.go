package types_test

import (
	"testing"

	"nexus/x/evm/types"

	"github.com/stretchr/testify/require"
)

func TestGenesisState_Validate(t *testing.T) {
	tests := []struct {
		desc     string
		genState *types.GenesisState
		valid    bool
	}{
		{
			desc:     "default is invalid",
			genState: types.DefaultGenesis(),
			valid:    true,
		},
		{
			desc: "valid genesis state",
			genState: &types.GenesisState{
				Params:                    types.DefaultParams(),
				GenesisExecutionBlockHash: "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			},
			valid: true,
		},
		{
			desc: "valid genesis state with execution block hash",
			genState: &types.GenesisState{
				Params:                    types.DefaultParams(),
				GenesisExecutionBlockHash: "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			},
			valid: true,
		},
		{
			desc: "invalid genesis execution block hash - too short",
			genState: &types.GenesisState{
				Params:                    types.DefaultParams(),
				GenesisExecutionBlockHash: "0x1234",
			},
			valid: false,
		},
		{
			desc: "invalid genesis execution block hash - no 0x prefix",
			genState: &types.GenesisState{
				Params:                    types.DefaultParams(),
				GenesisExecutionBlockHash: "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			},
			valid: false,
		},
		{
			desc: "invalid genesis execution block hash - too long",
			genState: &types.GenesisState{
				Params:                    types.DefaultParams(),
				GenesisExecutionBlockHash: "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef00",
			},
			valid: false,
		},
		{
			desc: "empty genesis execution block hash is invalid",
			genState: &types.GenesisState{
				Params:                    types.DefaultParams(),
				GenesisExecutionBlockHash: "",
			},
			valid: false,
		},
		{
			desc: "valid genesis state with current block state",
			genState: &types.GenesisState{
				Params:                    types.DefaultParams(),
				GenesisExecutionBlockHash: "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
				CurrentBlockHash:          "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
				CurrentBlockHeight:        123,
				CurrentBlockTimestamp:     1234567890,
			},
			valid: true,
		},
		{
			desc: "valid genesis state with empty current block hash",
			genState: &types.GenesisState{
				Params:                    types.DefaultParams(),
				GenesisExecutionBlockHash: "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
				CurrentBlockHash:          "",
				CurrentBlockHeight:        0,
				CurrentBlockTimestamp:     0,
			},
			valid: true,
		},
		{
			desc: "invalid current block hash - too short",
			genState: &types.GenesisState{
				Params:                    types.DefaultParams(),
				GenesisExecutionBlockHash: "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
				CurrentBlockHash:          "0x1234",
				CurrentBlockHeight:        123,
				CurrentBlockTimestamp:     1234567890,
			},
			valid: false,
		},
		{
			desc: "invalid current block hash - no 0x prefix",
			genState: &types.GenesisState{
				Params:                    types.DefaultParams(),
				GenesisExecutionBlockHash: "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
				CurrentBlockHash:          "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
				CurrentBlockHeight:        123,
				CurrentBlockTimestamp:     1234567890,
			},
			valid: false,
		},
		{
			desc: "invalid current block hash - too long",
			genState: &types.GenesisState{
				Params:                    types.DefaultParams(),
				GenesisExecutionBlockHash: "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
				CurrentBlockHash:          "0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef123456789000",
				CurrentBlockHeight:        123,
				CurrentBlockTimestamp:     1234567890,
			},
			valid: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			err := tc.genState.Validate()
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
