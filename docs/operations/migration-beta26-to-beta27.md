# Migration Guide: beta.26 → beta.27

## Summary

Beta.27 is the **time-trigger primitive** tag. It implements
[ADR-031](../adr/031-time-trigger-primitive.md) Option A: a new
`"type": "cron"` rule kind layered into the existing rule processor,
with no changes to existing expression rules and no payload-registry
additions.

| Surface | Status |
|---|---|
| New rule kind: `"type": "cron"` | **Additive** — opt-in per rule |
| New KV bucket: `RULE_SCHEDULES` | **Additive** — created on demand |
| New substitution namespace: `$schedule.*` | **Additive** — cron rules only |
| New Prometheus metrics: `semstreams_cron_*` | **Additive** — 7 collectors |
| New Grafana dashboard: `cron-scheduler.json` | **Additive** — opt-in import |
| Existing expression rules | **Unchanged** — no behavioural delta |
| Public API | **No breakage** |

**The simplest beta.26 → beta.27 upgrade is to do nothing.** Existing
deployments without cron rules see no behaviour change. Activation
requires authoring a `"type": "cron"` rule.

Two latent bugs in the hot-reload parser were also fixed; see
[Drive-by fixes](#drive-by-fixes) below. Neither was reachable from
beta.26 production state because both required cron rules to trigger.

## What ships

### The cron rule kind

```json
{
  "id": "weekly-planning-prompt",
  "type": "cron",
  "name": "Weekly planning prompt",
  "enabled": true,
  "schedule": "0 9 * * MON",
  "actions": [
    {
      "type": "publish_agent",
      "role": "planning-coach",
      "model": "default",
      "prompt": "It's $now — your weekly planning block."
    }
  ]
}
```

Cron rules accept **only**: `schedule`, `actions`, `cooldown`,
`fire_every_n_events`, `name`, `description`, `enabled`, `metadata`.
Forbidden fields (`conditions`, `logic`, `on_enter`, `on_exit`,
`on_recovery`, `while_true`, `rerun_on_recovery`, `max_iterations`,
`entity.*`, `related_patterns`) cause `NewCronRule` to return a typed
error at config-load time. Failing loud at config-load beats silently
ignoring fields the author thought were doing something.

### Cron expression flavour

POSIX 5-field plus descriptors: `@hourly`, `@daily`, `@weekly`,
`@monthly`, `@yearly`, plus `@every <duration>` for fast-cycling
schedules (`@every 1m`, `@every 30s`). Quartz seconds-precision and
"first Tuesday of month" are deferred per ADR-031.

### `$schedule.*` substitution namespace

| Token | Value | First-fire behaviour |
|---|---|---|
| `$schedule.id` | The firing rule's ID | (always populated) |
| `$schedule.spec` | The cron expression | (always populated) |
| `$schedule.last_fired_at` | Prior fire's wallclock, RFC3339 UTC | empty string |
| `$now` | Current wallclock, RFC3339 UTC | (always populated) |

`$schedule.last_fired_at` renders as the **empty string** on the
first fire (not the Go zero-time literal `0001-01-01T00:00:00Z`).
Authors detect first-fire by branching on emptiness; rendering an
empty token in a NATS subject path produces a consecutive-dot
fail-fast at publish rather than a silently-accepted bad timestamp.

The `$schedule.*` namespace is intentionally disjoint from
`$entity.*` / `$related.*` / `$state.*`. A cron-only `$schedule.*`
token reaching an expression-rule path (or vice versa) survives
substitution and trips the warn-on-unresolved-token logger — same
posture as a missing `$entity.triple.*` field.

### `RULE_SCHEDULES` KV bucket

Per-rule last-fired timestamps persist in a new bucket created on
demand. JSON shape:

```json
{
  "rule_id": "weekly-planning-prompt",
  "schedule_spec": "0 9 * * MON",
  "last_fired_at": "2026-04-27T09:00:00Z"
}
```

Two access patterns are supported:

1. **In-process callers** hold a `*ScheduleTracker` reference (e.g.
   from a custom processor wrapper) and call `LastFiredAt(ctx, ruleID)`.
   `ScheduleTracker.Bucket()` exposes the raw `jetstream.KeyValue`
   handle so callers can use `Watch` for live updates or `ListKeys`
   for filtered scans.
2. **Out-of-process callers** read the bucket directly using the
   exported `rule.ScheduleBucketName` constant + `rule.LastFireRecord`
   JSON shape. Both are part of the framework's public contract;
   renames require a migration. Useful for governance startup hooks
   that need to issue catch-up sweeps after long downtime.

### Missed-fire policy: log-only

Per ADR-031 product direction, missed fires across scheduler downtime
are **logged at Warn**, not auto-replayed. A missed Monday planning
prompt isn't worth a Tuesday catch-up. The next regularly scheduled
fire happens on its normal cadence.

The missed-fire detector runs once at `Start()`, walks the rule's
schedule from the persisted `last_fired_at` forward, counts expected
fires up to a cap of 100, and emits a single Warn per rule. The
companion metric `semstreams_cron_rule_missed_fires_total{rule_id}`
increments by the detected count; `semstreams_cron_rule_missed_fires_capped_total{rule_id}`
increments when a rule hits the cap (long-downtime indicator).

### Cross-restart cooldown semantics

The cooldown gate respects the persisted last-fired timestamp. A rule
with a 1h cooldown that fired one minute before process restart will
skip its next post-restart fire on the cooldown gate. Implementation:
`restoreFromTracker` hydrates `entry.lastFiredNanos` from the
persisted record before `cron.Start()` ticks. Same code path that
emits missed-fire warnings.

### Prometheus metrics + Grafana dashboard

Seven new collectors under `semstreams_cron_*`:

| Metric | Type | Labels |
|---|---|---|
| `rule_fires_total` | Counter | `rule_id`, `status` |
| `rule_fire_duration_seconds` | Histogram | `rule_id` |
| `rule_registered` | Gauge | (none) |
| `rule_missed_fires_total` | Counter | `rule_id` |
| `rule_missed_fires_capped_total` | Counter | `rule_id` |
| `rule_next_fire_timestamp_seconds` | Gauge | `rule_id` |
| `scheduler_running` | Gauge | (none) |

Status taxonomy: `success` / `error` / `panic` / `cooldown_skipped`
/ `inflight_skipped`.

- **`status="error"`**: a downstream action returned an error
  (NATS unreachable, validation failed). Aggregate "expected failures."
- **`status="panic"`**: an action panicked. The recover keeps the
  scheduler goroutine alive but **this is a programming-bug signal**.
  Page on `rate(semstreams_cron_rule_fires_total{status="panic"}[5m]) > 0`;
  fix the code, don't tune the alert. Cron-side code does not panic
  deliberately — the recover exists because action dispatch crosses
  uncontrolled boundaries (NATS, KV, HTTP/LLM via `publish_agent`)
  and robfig/cron does not recover from panics in jobs.
- **`status="cooldown_skipped"`**: the configured cooldown gate
  blocked a tick. Operator-by-design.
- **`status="inflight_skipped"`**: a previous fire was still running
  when this tick arrived (CAS rejection). Operator-error indicator —
  actions are slower than schedule, configure a cooldown explicitly
  or speed up the actions.

A Grafana dashboard `configs/grafana/dashboards/cron-scheduler.json`
ships with seven panels including a dedicated panic-rate stat (red
threshold above zero, paging-friendly).

## Retrofit-safe seams

The MVP is single-user but three explicit seams keep the future
per-tenant fan-out path purely additive:

1. **KV key shape.** `RULE_SCHEDULES` is keyed by bare `{ruleID}` for
   MVP. Future per-entity fan-out adds `.{entityID}` suffix; existing
   single-fire records remain valid. The `scheduleKey()` chokepoint
   in `schedule_tracker.go` is the only edit-site needed when the
   suffix lands.
2. **Substitution namespace.** `$schedule.*` is cron-context-only;
   `$entity.*` is populated only when an entity is in scope (today:
   never for cron; tomorrow: per `for_each` iteration). The future
   `$trigger.*` namespace for richer tick metadata (e.g.
   `$trigger.attempt`, `$trigger.scheduled_for`) lands as a sibling
   shim, not as overload.
3. **Schedule scope.** `Definition.Schedule` is a global cron
   expression today. Adding `Definition.ForEach` later is opt-in and
   orthogonal — rules that don't set it stay at the current
   single-fire semantics.

The MVP does not implement any per-tenant scoping. It simply does
not paint corners that a future per-tenant pass would have to back
out of.

## Worked examples

### Heartbeat (smallest)

`configs/rules/cron/heartbeat-example.json`. Every-minute heartbeat on
`system.cron.heartbeat`, exercising every `$schedule.*` token. Useful
as a liveness probe target and as a copy-paste starting point.

### Governance ops-role coordinator

`configs/rules/cron/governance-example.json`. Daily kill-switch sweep
that uses cron → `publish_agent` → governance ops-role coordinator
(per ADR-028). The cron rule is a clock; the coordinator does the
actual scan + emits structured triples. Worked starting point for
kill-switch sweeps and chain-hash audit windows.

## Drive-by fixes

Two latent bugs were uncovered by the integration test suite. Both
required cron rules to trigger; neither was reachable from beta.26
production state.

### `definitionFromMap` panicked on null `conditions` field

`Definition.Conditions` has no `omitempty` JSON tag, so a Definition
with no conditions JSON-marshals as `"conditions": null`. The
hot-reload parser's `conditionsVal.([]any)` type assertion panicked
on nil. Pre-beta.27 the panic was only reachable via cron rules (the
only Type that legitimately has no conditions); cron rules are new,
so no production deployments were affected.

Fixed with explicit nil-guard plus defensive type assertions
throughout `definitionFromMap` (`type`, `description`,
`entity.watch_buckets[i]`). Malformed KV records now return a typed
error rather than panicking the watcher reconcile goroutine.

### Seed-then-reconcile reset cron rule cooldown state

Hot-reload's startup path seeds `InlineRules` to KV, then the watcher's
reconcile pass reads them back and re-applies them. Pre-beta.27 the
re-apply unconditionally Deregister+Register-ed every cron rule, which
reset `entry.lastFiredNanos` to 0 — producing a transient "ignored
cooldown" window right at process startup.

Fixed with a `reflect.DeepEqual` idempotency check in
`applyCronRuleChange`. `loadRules` now also populates
`rp.ruleConfigs[def.ID]` at load time so the check sees a populated
entry on the first reconcile.

## Upgrade procedure

For deployments without cron rules: **no action required.** Existing
expression rules continue to behave identically.

For deployments adding cron rules:

1. Author one or more `"type": "cron"` rules in your flow's
   `inline_rules`, `rules_files`, or hot-reload KV bucket.
2. (Optional) Import `configs/grafana/dashboards/cron-scheduler.json`
   into Grafana for the operator dashboard.
3. (Optional) Configure an alert on
   `rate(semstreams_cron_rule_fires_total{status="panic"}[5m]) > 0`
   to page on programming bugs in cron-fired action paths.
4. Restart the rule processor. The `RULE_SCHEDULES` KV bucket is
   created on first start; missed-fire detection runs once on each
   subsequent start.

## Related

- [ADR-031: Time-Trigger Primitive for Reactive Rules](../adr/031-time-trigger-primitive.md)
- [Concept: Orchestration Layers](../concepts/14-orchestration-layers.md)
- [Concept: Rule-Driven Artifacts](../concepts/18-rule-driven-artifacts.md)
- GitHub: c360studio/semstreams#15 — the tracking issue.
