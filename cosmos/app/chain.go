package app

import (
	"fmt"

	"gopkg.in/yaml.v3"

	nexus "nexus/app/types"
	"nexus/lib"
)

type Forks struct {
	PragueTimestamp    *uint64 `yaml:"prague_timestamp,omitempty"`
	OsakaTimestamp     *uint64 `yaml:"osaka_timestamp,omitempty"`
	AmsterdamTimestamp *uint64 `yaml:"amsterdam_timestamp,omitempty"`
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

	spec := nexus.ChainSpec{
		PragueTimestamp:    config.Forks.PragueTimestamp,
		OsakaTimestamp:     config.Forks.OsakaTimestamp,
		AmsterdamTimestamp: config.Forks.AmsterdamTimestamp,
	}
	if err := spec.Validate(); err != nil {
		panic(err)
	}
	return spec
}
