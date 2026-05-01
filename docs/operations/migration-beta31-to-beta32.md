# Migration Guide: beta.31 → beta.32

## Summary

Beta.32 ships tag 1 of the [ADR-032](../adr/032-policy-tenancy-cluster.md) programme —
the first slice of **Track A: rule engine as policy DSL**. Two rule-language additions land:
the `$caller.*` substitution namespace and the `deny` action type.

| Surface | Status |
|---|---|
| New substitution namespace: `$caller.*` | **Additive** — renders empty until beta.33 wires the carrier |
| New action type: `deny` | **Additive** — opt-in per rule |
| New cron metric label: `status="denied"` | **Additive** — existing alert queries unaffected unless label-filtered |
| Existing expression rules | **Unchanged** — no behavioural delta |
| Existing cron rules | **Unchanged** — no behavioural delta unless a `deny` action is added |
| Public API / payload schemas | **No breakage** |
| `ExecutionContext` struct | **Additive** — new `Caller *CallerContext` field, nil by default |

**The simplest beta.31 → beta.32 upgrade is to do nothing.** Existing rules,
tests, and deployments behave identically. The new field on `ExecutionContext`
defaults to nil; all 28 existing `ExecutionContext{...}` test fixtures compile
and pass without modification.

## What's new

### `$caller.*` substitution namespace

Three substitution tokens are now recognised in rule templates, action fields,
and condition values:

| Token | Value when `Caller` is set | Value when `Caller` is nil |
|---|---|---|
| `$caller.id` | The caller's identity string | `""` (empty) |
| `$caller.role` | The caller's role claim | `""` (empty) |
| `$caller.org` | The caller's organisation claim | `""` (empty) |

The namespace follows the same rendering contract as `$entity.*`, `$state.*`,
and `$schedule.*`: unresolved tokens (e.g. a typo in a rule file) survive
substitution and trip the warn-on-unresolved-token logger without causing
evaluation failure.

**Important — tokens render empty in beta.32.** Caller context is populated
by the rule processor at evaluation time from a `CallerContext` struct on
`ExecutionContext`. In beta.32 nothing in the framework populates that struct
from live request data — NATS-header propagation (ADR-030 Phase 2) lands in
beta.33. Until then, `$caller.*` tokens resolve to empty strings in every
evaluation path:

- **Cron-fired rules**: no caller is in scope (scheduled, not request-driven).
  Empty rendering is permanent and correct.
- **KV-watch-triggered rules**: no caller propagated on the wire today. Renders
  empty until beta.33.
- **HTTP-triggered paths** (agentic-dispatch): `IdentityFromRequest` already
  extracts an identity string (ADR-030 Phase 1, beta.22), but the bridge from
  HTTP identity to `ExecutionContext.Caller` is beta.33 work.

The value of beta.32 is the rule-language plumbing and substitution engine that
beta.33 activates end-to-end. Authors can write `$caller.*`-bearing rules now;
they will begin evaluating non-trivially once beta.33 ships.

### `deny` action type

A new action type that returns a structural verdict and short-circuits subsequent
actions in the same evaluation cycle. JSON shape:

```json
{
  "type": "deny",
  "reason": "user $caller.id is not authorised for role $caller.role"
}
```

The `reason` field supports the full substitution set — `$caller.*`, `$entity.*`,
`$state.*`, `$schedule.*` — identical to other action fields. An empty or absent
`reason` is valid.

**Verdict semantics.** The rule evaluator returns `*DenyVerdict`, a typed error
value that satisfies both:

```go
errors.Is(err, rule.ErrDenyVerdict)          // sentinel check
errors.As(err, &dv)                           // extract substituted reason string
```

Callers that want only a boolean check use `errors.Is`. Callers that need the
human-readable reason for logging or downstream use `errors.As`.

**Audit triple.** On every deny verdict the evaluator writes a best-effort graph
triple:

```
subject:  <originating rule ID>
predicate: rule.deny
object:   <substituted reason string>
```

Audit failure (NATS unavailable, graph write error) does not flip the verdict
from deny to allow. The deny stands; the audit miss is logged at Error level.

**Action-list interaction.** A `deny` at any position in a `WhileTrue` or
`actions` list terminates subsequent actions for that evaluation cycle. The
match itself is not cleared; on the next evaluation cycle, if the condition
still holds, the rule fires again and the `deny` re-evaluates. Authors who
want post-deny side effects (e.g. emit a metric before denying) should place
those actions before the `deny` in the list or restructure as a separate rule
that fires on the same condition.

### `cronFireStatusDenied` — new cron metric label value

Cron-fired rules that emit a deny verdict now report a distinct status label:

```
semstreams_cron_rule_fires_total{rule_id="...", status="denied"}
```

Status taxonomy after beta.32:

| Label value | Meaning |
|---|---|
| `success` | Actions ran, no error or verdict |
| `denied` | A `deny` action short-circuited evaluation (**new**) |
| `error` | A downstream action returned an error |
| `panic` | An action panicked (programming-bug signal) |
| `cooldown_skipped` | Cooldown gate blocked the tick |
| `inflight_skipped` | Previous fire still running at next tick |

`status="denied"` is semantically distinct from `status="error"`: "a rule said
no" vs "something is broken." Denied fires count toward the duration histogram
(the deny action executed). Alert rules that fire on `status="error"` are not
affected by denied fires.

## Behavioural notes and caveats

- **`deny` in a `WhileTrue` action list terminates for the current cycle, not
  forever.** If the triggering condition persists across evaluation cycles, the
  rule will re-evaluate and the `deny` will fire again. This is consistent with
  the existing `WhileTrue` contract (actions re-run while the condition holds).

- **Dashboard queries filtering cron metrics by `status="error"` will not
  capture denied fires.** If a unified "rule did not complete normally" view
  is needed, update Prometheus queries to `status=~"error|denied"`.

- **`CallerContext.Source` is not present in beta.32.** A `Source` field
  (provenance tag: `"http_header" | "body_claim" | "ctx" | "default"`) was
  considered for audit-trail authenticity. It is deferred to beta.33, where
  NATS-header propagation gives `Source` a concrete value. Beta.32 structs
  have no `Source` field; adding it in beta.33 is additive and will not
  require a migration.

- **`$caller.tenant` is not a beta.32 token.** The ADR-032 Decision section
  describes `$caller.tenant` as a Track B surface (tenant-aware identity,
  beta.35+). The three tokens shipped now — `$caller.id`, `$caller.role`,
  `$caller.org` — cover the Track A policy expressiveness use cases.

## Migration steps

**None required.** Beta.32 is pure additive. No payload schemas changed. No
KV bucket additions. No new Prometheus collectors (only a new label value on
an existing counter). No configuration format changes.

Existing `ExecutionContext{...}` literal constructions in test code compile
without modification — the new `Caller *CallerContext` field is nil by default
and the struct is not exhaustive-initialised anywhere in the framework test
suite.

## Forward look

Beta.32 is tag 1 of a six-tag programme landing on branch
`feat/adr-032-programme`. The full schedule:

| Tag | Beta | Scope |
|---|---|---|
| 1 | beta.32 | `$caller.*` substitution + `deny` action + `cronFireStatusDenied` (this tag) |
| 2 | beta.33 | Identity-as-struct + NATS-header propagation — activates `$caller.*` end-to-end |
| 3 | beta.34 | Shadow mode at rule level + `count_in_window` operator + `negate` condition + filter-output bridge |
| 4 | beta.35 | Org-wiring pass — **HARD BREAKING** for products adopting tenant-aware `Dependencies` |
| 5 | beta.36 | JetStream cluster docs + reconnect defaults + replication runbook |
| 6 | beta.37 | Cert-based auth + per-org-account hardening |

Tag 2 (beta.33) is the critical activation step: once NATS-header propagation
lands, rules authored today with `$caller.*` tokens will begin evaluating
against real caller identity. Tag 4 (beta.35) is the only breaking change in
the programme and will have its own coordinated migration guide for semteams,
semspec, and semdragon.

## Related

- [ADR-032: Policy DSL, Multi-Tenant Identity, and Cluster Substrate](../adr/032-policy-tenancy-cluster.md)
- [ADR-030: HTTP Middleware and Identity Pattern](../adr/030-http-middleware-and-identity-pattern.md)
- [ADR-028: Orchestration Architecture](../adr/028-orchestration-architecture.md)
- [migration-beta26-to-beta27.md](migration-beta26-to-beta27.md) — cron rule type
  (`$schedule.*` substitution namespace and `cronFireStatusDenied`'s predecessor taxonomy)
- [docs/operations/08-agentic-components.md](08-agentic-components.md) — approval flow
  and identity context background
