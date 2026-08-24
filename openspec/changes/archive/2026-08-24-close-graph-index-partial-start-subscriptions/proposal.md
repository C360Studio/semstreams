# Change: Close graph-index partial-Start subscription ownership

Status: architecture accepted by the owner on 2026-08-22 after independent SemStreams inventory and design review.

Baseline: `b56df74c430d2822239446197554aa4b81059caa`.

Accepted evidence:

- `docs/proposals/gh989-graph-index-failed-start-inventory.md`, SHA-256
  `00de531276a2e13e5509bae8168d83794b36947c2a0211829481ad6cbe1f2b1f` (`INVENTORY PASS`).
- `docs/proposals/gh989-graph-index-failed-start-design.md`, SHA-256
  `d8574c69664734ead58e7c81b31787cfb6a880a180440cfef2fce1207d6008bc`.

## Why

PR #999 / commit `c84a9de7` supplied most graph-index failed-Start ownership mechanics: private runtime authority is
published before acquisition, earlier subscriptions are retained, cleanup drains before cancellation, and incomplete
cleanup can be retried by a later `Stop`.

Two exact gaps remain. No deterministic graph-index test fails query subscription 2 after subscription 1 succeeds,
so the incident class and duplicate-responder absence are not proved. Failed-Start rollback also invents a
`context.Background()` root instead of using the accepted parent-aware `internal/lifecyclecleanup` policy.

Issue #986 was implemented by merged PR #997 / commit `81178583`; component composition is boot-only and fail-closed.
This change must not restore manager or component Start retry.

## What changes

- Add one package-private concrete `subscribeForRequests` test seam to graph-index and retain the current production
  fallback and eight-subject acquisition order.
- Route graph-index failed-Start rollback through `lifecyclecleanup.RollbackFailedStart` using the Start parent.
- Add deterministic real-NATS causal tests that fail incoming subscription acquisition after outgoing succeeds,
  prove drain-before-cancel, prove retained authority after rollback expiry and later caller-context Stop, and prove a
  clean direct retry has one responder per subject.
- Add graph-index capability truth for partial query-responder ownership.

## Production ownership

- `processor/graph-index/component.go`
- `processor/graph-index/query.go`

Test ownership is one focused graph-index test file. OpenSpec and proposal artifacts are technical-writer owned.

## Non-goals

- No successful-running Stop change.
- No service, ComponentManager, sibling component, or `natsclient` production or test change.
- No query subject, handler, KV topology, readiness, configuration, schema, port, payload, or public API change.
- No public lifecycle state, cleanup knob, failure index, or subscription count.
- No manager Start retry or runtime component activation.
- No adopter migration or sister-repository change.
- No LLM persona, role, prompt, model call, runtime agent, ops agent, or scenario.
- No claim that adjacent lifecycle issues or owners are complete.
- No ADR; ADR-095 already owns the lifecycle decision.
