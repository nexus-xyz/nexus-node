package app

import (
	"github.com/hashicorp/go-metrics"

	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	slashingkeeper "github.com/cosmos/cosmos-sdk/x/slashing/keeper"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// updateValidatorMetrics iterates all validators and updates telemetry gauges.
// Per-validator (labeled by moniker+address):
//   - validator.bonded, validator.unbonding, validator.unbonded — exactly one is 1, others 0
//   - validator.jailed, validator.tombstoned — 0 or 1
//
// Network-level:
//   - network.validators{bond_status="bonded|unbonding|unbonded"}
//   - network.validators.jailed
//   - network.validators.tombstoned
//
// Metrics are emitted only when app telemetry is enabled
// (telemetry.enabled = true and prometheus-retention-time > 0).
func updateValidatorMetrics(
	ctx sdk.Context,
	stakingKeeper *stakingkeeper.Keeper,
	slashingKeeper *slashingkeeper.Keeper,
) {
	var bonded, unbonding, unbonded, jailed, tombstoned int

	err := stakingKeeper.IterateValidators(ctx, func(_ int64, val stakingtypes.ValidatorI) (stop bool) {
		isBonded, isUnbonding, isUnbonded := float32(0), float32(0), float32(0)
		switch val.GetStatus() {
		case stakingtypes.Bonded:
			bonded++
			isBonded = 1
		case stakingtypes.Unbonding:
			unbonding++
			isUnbonding = 1
		case stakingtypes.Unbonded:
			unbonded++
			isUnbonded = 1
		}

		isJailed := float32(0)
		if val.IsJailed() {
			jailed++
			isJailed = 1
		}

		isTombstoned := float32(0)
		consAddr, err := val.GetConsAddr()
		if err == nil {
			if info, err := slashingKeeper.GetValidatorSigningInfo(ctx, consAddr); err == nil && info.Tombstoned {
				tombstoned++
				isTombstoned = 1
			}
		}

		labels := []metrics.Label{
			telemetry.NewLabel("moniker", val.GetMoniker()),
			telemetry.NewLabel("address", val.GetOperator()),
		}
		telemetry.SetGaugeWithLabels([]string{"validator", "bonded"}, isBonded, labels)
		telemetry.SetGaugeWithLabels([]string{"validator", "unbonding"}, isUnbonding, labels)
		telemetry.SetGaugeWithLabels([]string{"validator", "unbonded"}, isUnbonded, labels)
		telemetry.SetGaugeWithLabels([]string{"validator", "jailed"}, isJailed, labels)
		telemetry.SetGaugeWithLabels([]string{"validator", "tombstoned"}, isTombstoned, labels)
		return false
	})
	if err != nil {
		return
	}

	telemetry.SetGaugeWithLabels(
		[]string{"network", "validators"},
		float32(bonded),
		[]metrics.Label{telemetry.NewLabel("bond_status", "bonded")},
	)
	telemetry.SetGaugeWithLabels(
		[]string{"network", "validators"},
		float32(unbonding),
		[]metrics.Label{telemetry.NewLabel("bond_status", "unbonding")},
	)
	telemetry.SetGaugeWithLabels(
		[]string{"network", "validators"},
		float32(unbonded),
		[]metrics.Label{telemetry.NewLabel("bond_status", "unbonded")},
	)
	telemetry.SetGaugeWithLabels([]string{"network", "validators", "jailed"}, float32(jailed), nil)
	telemetry.SetGaugeWithLabels([]string{"network", "validators", "tombstoned"}, float32(tombstoned), nil)
}
