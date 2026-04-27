package types

import "fmt"

// ChainSpec holds unix-second fork activation times for EL Engine API routing.
// Osaka enables engine_getPayloadV5; Amsterdam enables engine_newPayloadV5 (execution-apis ordering).
//
// Engine RPC version matrix (timestamp is wall-clock seconds; forks may co-activate at the same time):
//
//	┌───────────────┬─────────────────────┬──────────────────────┐
//	│ Timestamp     │ GetPayloadEngineRPC │ NewPayloadEngineRPC  │
//	├───────────────┼─────────────────────┼──────────────────────┤
//	│ < Prague      │ V3                  │ V3                   │
//	├───────────────┼─────────────────────┼──────────────────────┤
//	│ Prague        │ V4                  │ V4                   │
//	├───────────────┼─────────────────────┼──────────────────────┤
//	│ Osaka         │ V5                  │ V4 (until Amsterdam) │
//	├───────────────┼─────────────────────┼──────────────────────┤
//	│ Amsterdam     │ V5 (unchanged)      │ V5                   │
//	└───────────────┴─────────────────────┴──────────────────────┘
type ChainSpec struct {
	PragueTimestamp    *uint64
	OsakaTimestamp     *uint64
	AmsterdamTimestamp *uint64
}

// Validate returns an error if fork timestamps are inconsistent.
// Forks must activate in order: Prague ≤ Osaka ≤ Amsterdam (co-activation at the same
// timestamp is valid). Each fork requires its predecessor to be set.
func (c ChainSpec) Validate() error {
	if c.OsakaTimestamp != nil {
		if c.PragueTimestamp == nil {
			return fmt.Errorf("osaka_timestamp is set but prague_timestamp is required (forks activate in order)")
		}
		if *c.OsakaTimestamp < *c.PragueTimestamp {
			return fmt.Errorf(
				"osaka_timestamp (%d) must be >= prague_timestamp (%d)",
				*c.OsakaTimestamp, *c.PragueTimestamp)
		}
	}
	if c.AmsterdamTimestamp != nil {
		if c.OsakaTimestamp == nil {
			return fmt.Errorf("amsterdam_timestamp is set but osaka_timestamp is required (forks activate in order)")
		}
		if *c.AmsterdamTimestamp < *c.OsakaTimestamp {
			return fmt.Errorf(
				"amsterdam_timestamp (%d) must be >= osaka_timestamp (%d)",
				*c.AmsterdamTimestamp, *c.OsakaTimestamp)
		}
	}
	return nil
}

// IsPragueActive is true at or after the Prague fork (Prague-era Engine APIs, e.g. getPayloadV4 / newPayloadV4).
func (c ChainSpec) IsPragueActive(timestamp uint64) bool {
	return c.PragueTimestamp != nil && timestamp >= *c.PragueTimestamp
}

// IsOsakaActive is true at or after Osaka (engine_getPayloadV5, BlobsBundleV2, etc.).
func (c ChainSpec) IsOsakaActive(timestamp uint64) bool {
	return c.OsakaTimestamp != nil && timestamp >= *c.OsakaTimestamp
}

// IsAmsterdamActive is true at or after Amsterdam (engine_newPayloadV5 for finalize).
func (c ChainSpec) IsAmsterdamActive(timestamp uint64) bool {
	return c.AmsterdamTimestamp != nil && timestamp >= *c.AmsterdamTimestamp
}

// GetPayloadEngineRPCVersion returns the engine_getPayloadVx version for this timestamp (3, 4, or 5).
func (c ChainSpec) GetPayloadEngineRPCVersion(timestamp uint64) int {
	switch {
	case c.IsOsakaActive(timestamp):
		return 5
	case c.IsPragueActive(timestamp):
		return 4
	default:
		return 3
	}
}

// NewPayloadEngineRPCVersion returns the engine_newPayloadVx version for this timestamp (3, 4, or 5).
func (c ChainSpec) NewPayloadEngineRPCVersion(timestamp uint64) int {
	switch {
	case c.IsAmsterdamActive(timestamp):
		return 5
	case c.IsPragueActive(timestamp):
		return 4
	default:
		return 3
	}
}
