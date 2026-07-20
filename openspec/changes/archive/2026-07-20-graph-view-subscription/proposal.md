# Proposal: Graph View Subscription — Shared Read-Side Fan-Out

## Why

SemStreams' write side is hardened and single-writer (graph-ingest sole
`ENTITY_STATES` writer; #480 ingest ceiling; #562 write-path fan-out); its read
side is not. Serving many clients a live view of a busy bucket means one
`WatchAll` per client — **O(N × writeRate)** redundant serialize/decode/buffer,
N independent slow-consumer drop points, operator-visible staleness under load
(#579). That per-client trap is live in this repo (agentic-dispatch AGENT_LOOPS
SSE `http.go:902`; message-logger KV-watch SSE) and in semboids' graphstream.
Beyond it, the corrected sweep found **12 steady-state single-watcher
hand-rolls** in four shapes (serving projections, per-revision pipelines,
work-triggers, a bounded-cache invalidation feed) with no shared seam and no
consistent coherence story — the serving-shaped ones each independently
re-risking the read-through-cache stale-repopulation race just fixed in
graph-ingest (ADR-079 wave / PR #583), and already divergent on poison
handling, watcher-loss, and readiness. This change **locks the contract** for
one shared primitive so products stop mis-rolling per-client watchers. The
build is **owner-gated** (design-first; ADR-081 records the decision).

## What Changes

- Add a **graph-view-subscription** capability: a shared, coalesced, validated
  read-model with snapshot+delta subscriber fan-out, delta-only trigger attach,
  coherent point reads, per-subscriber at-most-once backpressure, and an honest
  degraded-path contract (readiness gate, caught-up watermark, watcher-loss
  fail-closed, per-key poison surfacing). Home: `pkg/graphview`
  (domain-agnostic — bucket + decode func injected). NOT `pkg/projection`
  (taken by ADR-056).
- Define the coherence contract binding snapshot to delta stream (G1–G4) plus
  the degraded-path guarantees (G5 fail-closed, G6 poison surfacing), applying
  the ADR-079 ABA pattern (`processor/graph-ingest/component.go:2943`) at both
  the attach seam and the tick seam.
- Decode is **validating, once, amortized across N subscribers** — owner-only
  trusted decode (#562) is forbidden on the view path by its own API contract.
- Compose existing substrate where layering permits: revision-coalescer
  *semantics* lifted into `pkg/graphview` (the graph-index implementation is
  unexported/processor-layer); a plain apply-sequence under the projection
  mutex (not `revlag`); ADR-066/ADR-079 degraded-path doctrine.
- **Contract locked first** (design gate, tasks §1); the build phases
  (tasks §2) were owner-gated and green-lit 2026-07-19 after the 5-lens
  review and confirmation against the #579 evidence.

## Capabilities

### New Capabilities

- `graph-view-subscription`: single-watcher validated projection, view-rate
  coalescing with tombstones in-lane, snapshot/delta consistency, read-after-
  write coherence across attach AND tick seams, readiness gating + caught-up
  watermark, watcher-loss fail-closed, per-key poison surfacing, per-subscriber
  backpressure, coherent point reads, view lifecycle/ownership, and coexistence
  with raw `WatchAll`.

## Impact

- New capability spec `openspec/specs/graph-view-subscription/spec.md` (on
  archive); ADR-081.
- Migration scope is **four-class, verified per site** (ADR-081 table):
  per-client serving surfaces convert (first mover: AGENT_LOOPS SSE);
  serving projections convert (COMMUNITY_INDEX 2→1, EMBEDDING_INDEX 2→1 with
  delta-only trigger attach); the graph-query bounded cache converts **only in
  multi-reader processes** (#571 PARTIALLY subsumed — single-reader processes
  would trade a 1000-entry cache for a full-bucket projection); per-revision
  pipelines and the lifecycle guard **stay raw** (ADR-081 Alternative 5 —
  forcing them on would shift full-bucket memory into processes that never
  held it and break every-revision validation semantics).
- Coexists with, does not replace, raw `WatchAll` in `natsclient`.

## Non-goals

- Not changing NATS per-consumer delivery/flow-control semantics.
- Not replacing raw `WatchAll` for independent-filter / historical-replay /
  per-revision-delivery / independent-ack consumers — explicitly including the
  graph-index/spatial/temporal/graph-embedding pipelines and the lifecycle
  validation guard.
- Not a shared raw-rate feed for those pipelines (a different primitive;
  rejected for v1 as problem-shifting — ADR-081 Alternative 5).
- Not an event log — intermediate per-key revisions may be coalesced away;
  full history stays with the raw KV lane / JetStream (#340).
- Cross-process re-publish (`graph.view.<name>.delta`) is DEFERRED, not built;
  this lock does not pre-approve that subject shape.
- Bulk one-shot reads / pagination out of scope (#176 — different axis).
- The two `natsclient` ergonomics from #579 (attribute the slow-consumer log;
  expose pending-limit config) are split out as independent small fixes.
- No pluggable backpressure policies, query language, persistence tiers, or
  multi-region (rejected as over-engineering — ADR-081).
