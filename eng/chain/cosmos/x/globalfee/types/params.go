package types

import (
	"encoding/json"
	"fmt"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type Params struct {
	MinimumGasPrice sdk.DecCoin `json:"minimum_gas_price"`
}

func DefaultParams() Params {
	return Params{MinimumGasPrice: sdk.NewDecCoinFromDec(ChainDenom, math.LegacyZeroDec())}
}

func (p Params) Validate() error {
	if err := p.MinimumGasPrice.Validate(); err != nil {
		return fmt.Errorf("minimum_gas_price: %w", err)
	}
	if p.MinimumGasPrice.Denom != ChainDenom {
		return fmt.Errorf("minimum_gas_price: denom must be %s, got %s", ChainDenom, p.MinimumGasPrice.Denom)
	}
	return nil
}

type GenesisState struct {
	Params Params `json:"params"`
}

func DefaultGenesis() GenesisState {
	return GenesisState{Params: DefaultParams()}
}

func (gs GenesisState) Validate() error {
	return gs.Params.Validate()
}

func MarshalGenesis(gs GenesisState) (json.RawMessage, error) {
	return json.Marshal(gs)
}

func UnmarshalGenesis(bz json.RawMessage) (GenesisState, error) {
	var gs GenesisState
	if err := json.Unmarshal(bz, &gs); err != nil {
		return GenesisState{}, err
	}
	return gs, nil
}
