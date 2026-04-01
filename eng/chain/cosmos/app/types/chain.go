package types

// Defines activation timestamps for each fork.
type ChainSpec struct {
	// EngineV4PragueTimestamp is the unix timestamp at which V0 activates, switching the
	// engine API from V3 to V4 (Prague). Must match pragueTime on the EL.
	EngineV4PragueTimestamp *uint64
}

func (c ChainSpec) IsEngineV4PragueEnabled(timestamp uint64) bool {
	return c.EngineV4PragueTimestamp != nil && timestamp >= *c.EngineV4PragueTimestamp
}
