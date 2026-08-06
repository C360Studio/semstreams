# ADR-092: Lifecycle Poison Localization

## Status

**Accepted (2026-08-06).** This decision narrowly supersedes ADR-081's lifecycle validation-guard classification.
ADR-079 remains accepted.

## Context

Lifecycle projects exact `ENTITY_STATES` reads and workflow-pattern watch entries directly; it owns no cached
materialized view. Its Manager-wide sticky poison latch and validation-only `WatchAll` guard nevertheless made one
malformed entity block unrelated exact reads, lists, mutations, and subscriptions until process restart. The guard also
duplicated decode already performed by the read paths and contradicted the current graph-state contract that dedicated
validation watchers are not admitted.

## Decision

Lifecycle poison follows the scope of the read that observes it:

- exact operations validate only their requested entity and retain no poison state between calls;
- List filters keys by the registered workflow before exact decode and fails as a whole only for matching poison;
- each Watch or WatchEvents call owns one workflow-pattern watcher; matching poison emits no output, warns once with
  workflow, entity, revision, code, and reason, and closes only that subscription;
- unexpected transport close is subscription-local and cancellation is quiet.

Delete the Manager latch, full-authority `WatchAll`, readiness and revision barriers, degradation latch, and guard
lifecycle without replacement. Validation rides exact and workflow-pattern reads. Keep asynchronous value-channel and
WebSocket closure as the terminal contract.

No coordinator, cache, repair watcher, terminal-error channel, status, metric, configuration, gateway mapping, or
`pkg/graphview` dependency is admitted.

## Consequences

Malformed authority affects only the entity, workflow list, or subscription that actually observes it. Healthy
co-resident lifecycle work continues, and a repaired entity is evaluated on its next real read without restarting the
Manager. Operators gain a subscription-local diagnostic that identifies the malformed authority entry, while adopters
gain no new API or recovery procedure.

ADR-081 remains historical authority for graph-view subscriptions; only its classification of lifecycle's former
validation guard is superseded. ADR-079's per-entity authoritative response and retirement of dedicated contract-guard
watchers is reinforced, not changed.
