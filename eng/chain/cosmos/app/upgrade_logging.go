package app

import (
	"encoding/json"
	"errors"
	"sync"

	upgradekeeper "cosmossdk.io/x/upgrade/keeper"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// upgradeEventName is the event string ops tooling matches against to route
// upgrade_scheduled logs to PagerDuty/Slack. Changing it breaks those rules.
const upgradeEventName = "upgrade_scheduled"

// upgradeLog is the JSON payload emitted when x/upgrade stores a new
// governance-approved plan (ENG-1225 / ENG-1446). The shape is a stable
// contract consumed by ops log-scrape alerting.
type upgradeLog struct {
	Event  string `json:"event"`
	Name   string `json:"name"`
	Height int64  `json:"height"`
	Info   string `json:"info"`
}

// upgradeNotifier holds the minimum state needed to log an upgrade_scheduled
// event exactly once per plan.
//
// Why a struct (and not a stateless function like logHaltIfTriggered):
// halt_triggered is naturally one-shot — it only fires when the current block
// height equals the plan height, and the chain halts immediately after. There
// is no equivalent one-shot condition for "a plan is stored": once governance
// passes the plan, it sits in the keeper for potentially thousands of blocks
// until the halt height. A stateless check would emit on every one of those
// blocks, which spams any log sink wired directly to Slack (no upstream
// dedup). The mutex + last-logged fields below let us emit once per distinct
// plan without requiring ops-layer throttling.
//
// State is in-memory only. On process restart we re-log any pending plan —
// this is desired: validators coming back online should be reminded that an
// upgrade is scheduled.
type upgradeNotifier struct {
	keeper *upgradekeeper.Keeper

	mu           sync.Mutex
	lastLoggedOK bool
	lastName     string
	lastHeight   int64
	lastInfo     string
}

func newUpgradeNotifier(k *upgradekeeper.Keeper) *upgradeNotifier {
	return &upgradeNotifier{keeper: k}
}

// MaybeLogNewPlan reads the currently scheduled upgrade plan and emits a
// structured JSON log the first time it is observed. Safe to call every block;
// no-ops when no plan is stored or when the stored plan matches the one last
// logged this process lifetime.
func (n *upgradeNotifier) MaybeLogNewPlan(ctx sdk.Context) {
	if n == nil || n.keeper == nil {
		return
	}

	plan, err := n.keeper.GetUpgradePlan(ctx)
	if err != nil {
		if errors.Is(err, upgradetypes.ErrNoUpgradePlanFound) {
			// No plan scheduled — reset so the next plan triggers a fresh log.
			n.forgetLastPlan()
		} else {
			// Unexpected storage error: log it and return. This path must never
			// halt block production, so we do not propagate the error.
			ctx.Logger().Error("upgrade logger: failed to read upgrade plan", "error", err)
		}
		return
	}

	// Skip when the plan is already due this block — logHaltIfTriggered in the
	// same PreBlocker will fire halt_triggered for that case. Emitting
	// upgrade_scheduled at the same height would produce a misleading alert
	// (or a duplicate next to halt_triggered) on a post-restart block.
	if plan.ShouldExecute(ctx.HeaderInfo().Height) {
		return
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	// Dedup on all user-visible fields (name, height, info). Including info
	// ensures a same-block cancel + reschedule with the same (name, height)
	// but an updated info payload — e.g. corrected release notes URL — still
	// re-emits, since forgetLastPlan would not have run in that case.
	if n.lastLoggedOK &&
		n.lastName == plan.Name &&
		n.lastHeight == plan.Height &&
		n.lastInfo == plan.Info {
		return
	}

	payload := upgradeLog{
		Event:  upgradeEventName,
		Name:   plan.Name,
		Height: plan.Height,
		Info:   plan.Info,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		ctx.Logger().Error("upgrade logger: failed to marshal upgrade_scheduled payload", "error", err)
		return
	}
	ctx.Logger().Info(string(encoded))

	n.lastLoggedOK = true
	n.lastName = plan.Name
	n.lastHeight = plan.Height
	n.lastInfo = plan.Info
}

// forgetLastPlan clears the notifier's memory of the last-logged plan so that
// if a new plan is scheduled later we log it again even if it matches the
// previously-cancelled one.
func (n *upgradeNotifier) forgetLastPlan() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.lastLoggedOK = false
	n.lastName = ""
	n.lastHeight = 0
	n.lastInfo = ""
}
