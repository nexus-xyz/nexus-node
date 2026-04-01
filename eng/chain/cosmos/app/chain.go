package app

import (
	"fmt"

	"gopkg.in/yaml.v3"

	nexus "nexus/app/types"
	"nexus/lib"
)

type Forks struct {
	EngineV4PragueTimestamp *uint64 `yaml:"prague_timestamp,omitempty"`
}

type ForksConfig struct {
	Forks Forks `yaml:"forks"`
}

func LoadChainSpec() nexus.ChainSpec {
	data, err := lib.ReadConfigFile()

	if err != nil {
		return nexus.ChainSpec{}
	}

	var config ForksConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		panic(fmt.Errorf("failed to decode config %w", err))
	}

	return nexus.ChainSpec{
		EngineV4PragueTimestamp: config.Forks.EngineV4PragueTimestamp,
	}
}
