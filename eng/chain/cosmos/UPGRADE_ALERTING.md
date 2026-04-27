# Upgrade alerting — structured log patterns

Structured log events emitted by `nexusd` across the upgrade lifecycle:
from the moment governance stores a plan (`upgrade_scheduled`) through to
the block where the chain halts for the binary swap (`halt_triggered`).

Both events are JSON objects emitted via the Cosmos SDK logger
(`cosmossdk.io/log`). Match on the `"event":"<name>"` substring — no regex
required.

## `upgrade_scheduled` — upgrade plan stored via governance

Emitted the first time the node observes a newly-stored upgrade plan in
state. Fires when a governance proposal containing
`cosmos.upgrade.v1beta1.MsgSoftwareUpgrade` passes and its plan is persisted
by `UpgradeKeeper.ScheduleUpgrade`. Validators and stakeholders must be paged
immediately — they need to prepare the new binary **before** the halt height
is reached.

Fulfils criterion *"communicated to validators and internal stakeholders
immediately"* from [ENG-1225](https://linear.app/nexus/issue/ENG-1225).
Implemented in [ENG-1446](https://linear.app/nexus/issue/ENG-1446).

### Log shape

Emitted at **INFO** level. The JSON payload is the logger message:

```json
{"event":"upgrade_scheduled","name":"v2-mainnet","height":12345678,"info":"https://github.com/nexus-xyz/nexus/releases/tag/v2.0.0"}
```

### Fields

| Field    | Type   | Description                                                |
| -------- | ------ | ---------------------------------------------------------- |
| `event`  | string | Always `upgrade_scheduled` — match key for ops rules.      |
| `name`   | string | Name of the upgrade plan (e.g. `v2-mainnet`).              |
| `height` | int64  | Block height at which the chain will halt for the upgrade. |
| `info`   | string | Free-form plan metadata (usually a URL to release notes).  |

### Semantics

- Fires **once per distinct plan payload** (name + height + info). In-process
  deduplication prevents re-emission on every block while the plan sits in
  the keeper. Re-emitted whenever any of those three fields changes — e.g.
  `MsgCancelUpgrade` followed by a new `MsgSoftwareUpgrade`, or a same-block
  reschedule that updates the `info` URL.
- Fires on **every process start** while a plan is pending — restarted
  validators are re-notified that an upgrade is coming. Intentional.
- Has a **one-block delay** relative to governance passing the proposal:
  the plan is stored during block N's tx processing and first observed by
  the `PreBlocker` of block N+1. Negligible for operator alerting, halt
  height is always many blocks away.
- Does **not** fire when the chain halts at `height`; that is a separate
  `halt_triggered` event (see below).

### Example alerting rules

Grafana Loki:

```logql
{job="nexusd"} |= `"event":"upgrade_scheduled"`
```

Vector → Slack webhook:

```toml
[transforms.upgrade_scheduled_filter]
type       = "filter"
inputs     = ["nexusd_logs"]
condition  = 'contains(.message, "\"event\":\"upgrade_scheduled\"")'

[sinks.slack_upgrades]
type           = "http"
inputs         = ["upgrade_scheduled_filter"]
uri            = "https://hooks.slack.com/services/XXX/YYY/ZZZ"
encoding.codec = "json"
```

## `halt_triggered` — chain about to halt for upgrade

Emitted in the block where x/upgrade's `PreBlocker` is about to panic with
`UPGRADE NEEDED` because:

- a plan is scheduled for the current block height, **and**
- the running binary has no handler registered for the plan name, **and**
- the height is not in the skip-upgrade set.

Fires exactly once per halt: the chain stops producing blocks immediately
after. If the current binary **has** a handler for the plan, this is a
routine upgrade (not a halt) and no log is emitted.

Implemented in [ENG-1445](https://linear.app/nexus/issue/ENG-1445).

### Log shape

Emitted at **ERROR** level. The JSON payload is the logger message:

```json
{"event":"halt_triggered","plan_name":"v2-mainnet","height":12345678,"info":"https://github.com/nexus-xyz/nexus/releases/tag/v2.0.0","timestamp":"2026-01-15T12:00:00Z"}
```

### Fields

| Field       | Type   | Description                                                |
| ----------- | ------ | ---------------------------------------------------------- |
| `event`     | string | Always `halt_triggered` — match key for ops rules.         |
| `plan_name` | string | Name of the upgrade plan that triggered the halt.          |
| `height`    | int64  | Block height at which the halt fires.                      |
| `info`      | string | Free-form plan metadata from the original governance plan. |
| `timestamp` | string | Block time in RFC3339Nano UTC.                             |

### Semantics

- Fires exactly **once** per halt (the chain panics immediately after).
- Does **not** fire on routine upgrades where a handler is registered.
- Emitted at ERROR level so level-based routing can page on-call faster
  than the INFO-level `upgrade_scheduled` heads-up.

### Example alerting rules

Grafana Loki:

```logql
{job="nexusd"} |= `"event":"halt_triggered"`
```

Vector → Slack webhook:

```toml
[transforms.halt_triggered_filter]
type       = "filter"
inputs     = ["nexusd_logs"]
condition  = 'contains(.message, "\"event\":\"halt_triggered\"")'

[sinks.slack_halt]
type           = "http"
inputs         = ["halt_triggered_filter"]
uri            = "https://hooks.slack.com/services/XXX/YYY/ZZZ"
encoding.codec = "json"
```

## Follow-ups

A dedicated notifier that posts directly to a configurable webhook URL
(Slack, PagerDuty) is tracked separately — this document covers only the
log-scrape MVP.
