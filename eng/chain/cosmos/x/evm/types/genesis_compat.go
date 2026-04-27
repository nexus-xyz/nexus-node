package types

import (
	"encoding/json"
	"fmt"

	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/codec"
)

// GenesisStateFromJSON decodes the EVM module's genesis JSON for InitGenesis / ValidateGenesis.
// It accepts JSON produced by older nexusd releases (alternate key casing, omitted fields) and
// fills defaults so an upgraded binary can load the same on-disk genesis.json after a coordinated
// container upgrade without rewriting the file.
//
// logger may be nil (e.g. in ValidateGenesis which has no sdk.Context); the fallback warning is
// skipped in that case.
func GenesisStateFromJSON(cdc codec.JSONCodec, bz json.RawMessage, logger log.Logger) (GenesisState, error) {
	var gs GenesisState
	if err := cdc.UnmarshalJSON(bz, &gs); err != nil {
		// Legacy files: stdlib JSON uses the struct's `json:` tags from generated protos.
		if err2 := json.Unmarshal(bz, &gs); err2 != nil {
			return gs, fmt.Errorf("evm genesis: codec unmarshal: %w; json fallback: %v", err, err2)
		}
		if logger != nil {
			logger.Warn(
				"evm genesis decoded via stdlib JSON fallback (legacy format)",
				"codec_error", err.Error(),
			)
		}
	}
	return NormalizeGenesisState(gs), nil
}

// NormalizeGenesisState applies defaults for fields that older genesis files may omit.
func NormalizeGenesisState(gs GenesisState) GenesisState {
	if gs.GenesisExecutionBlockHash == "" {
		gs.GenesisExecutionBlockHash = DefaultGenesis().GenesisExecutionBlockHash
	}
	return gs
}
