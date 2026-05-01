package app

import (
	"encoding/json"
	"errors"
	"time"

	upgradekeeper "cosmossdk.io/x/upgrade/keeper"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// haltEventName is the event string ops tooling matches against to route
// halt_triggered logs to PagerDuty/Slack. Changing it breaks those rules.
const haltEventName = "halt_triggered"

// haltLog is the JSON payload emitted before the x/upgrade PreBlocker panics
// with UPGRADE NEEDED at a coordinated halt height.
// The shape is a stable contract consumed by ops log-scrape alerting.
type haltLog struct {
	Event     string `json:"event"`
	PlanName  string `json:"plan_name"`
	Height    int64  `json:"height"`
	Info      string `json:"info"`
	Timestamp string `json:"timestamp"`
}

// logHaltIfTriggered emits a structured JSON log when the upgrade PreBlocker
// is about to halt the chain. A halt fires when:
//   - a plan is scheduled for the current block height
//   - the current binary has no handler registered for the plan name
//   - the height is not in the skip-upgrade set
//
// When the plan has a handler, this is a real upgrade (not a halt) and no
// log is emitted.
func logHaltIfTriggered(ctx sdk.Context, k *upgradekeeper.Keeper, webhook *webhookNotifier) {
	plan, err := k.GetUpgradePlan(ctx)
	if err != nil {
		if !errors.Is(err, upgradetypes.ErrNoUpgradePlanFound) {
			ctx.Logger().Error("halt logger: failed to read upgrade plan", "error", err)
		}
		return
	}

	blockHeight := ctx.HeaderInfo().Height
	if !plan.ShouldExecute(blockHeight) {
		return
	}
	if k.IsSkipHeight(blockHeight) {
		return
	}
	if k.HasHandler(plan.Name) {
		return
	}

	payload := haltLog{
		Event:     haltEventName,
		PlanName:  plan.Name,
		Height:    blockHeight,
		Info:      plan.Info,
		Timestamp: ctx.BlockTime().UTC().Format(time.RFC3339Nano),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		ctx.Logger().Error("halt logger: failed to marshal halt_triggered payload", "error", err)
		return
	}
	ctx.Logger().Error(string(encoded))

	// Dispatch synchronously: x/upgrade's PreBlocker halts the chain with
	// UPGRADE NEEDED immediately after we return. Firing and forgetting would
	// race the shutdown and drop the alert. The notifier has a bounded total
	// deadline (max retries × backoff + per-request timeout) so even a hung
	// Slack endpoint cannot prevent the halt.
	if webhook != nil {
		webhook.Notify(haltEventName, formatHaltTriggeredText(payload))
	}
}
