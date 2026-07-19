# Proposal: Graph View Subscription — Shared Read-Side Fan-Out

## Why

SemStreams' write side is hardened and single-writer (graph-ingest sole
`ENTITY_STATES` writer; #480 ingest ceiling; #562 write-path fan-out); its read
side is not. Serving many clients a live view of a busy graph currently means one
`WatchAll` per client — **O(N × writeRate)** redundant serialize/decode/buffer, N
independent slow-consumer drop points, operator-visible staleness under load
(#579). The framework has hand-rolled every piece of the fix ~7× (`graph/query/
client.go:221`, `processor/graph-query/community_cache.go:49`, `pkg/lifecycle/
manager.go:90`, graph-index, graph-embedding, graph-clustering, graph-inference)
with no shared seam and no consistent coherence story — each independently
re-risks the read-through-cache stale-repopulation race just fixed in graph-ingest
(ADR-079 wave / PR #583). This change **locks the contract** for one shared
primitive so products stop mis-rolling per-client watchers. The build is
**owner-gated** (design-first; ADR-081 records the decision).

## What Changes

- Add a **graph-view-subscription** capability: a shared, coalesced, cached
  read-model with snapshot+delta subscriber fan-out and per-subscriber
  at-most-once backpressure. Home: `pkg/graphview` (domain-agnostic — bucket +
  decode func injected). NOT `pkg/projection` (taken by ADR-056).
- Define the coherence contract binding snapshot to delta stream (G1–G4),
  reusing the ADR-079 ABA invalidation-generation pattern
  (`processor/graph-ingest/component.go:2943-3006`) as prior art.
- Compose existing substrate: `pkg/revlag.Watermark` (sequencing),
  revision-coalescer semantics (`processor/graph-index/revision_coalescer.go`), a
  projection map, the #562 trusted-decode fast path.
- **No build in this change** — contract only. Build is a separate, owner-gated
  change (see ADR-081; `tasks.md` here lists the gated implementation phases).

## Capabilities

### New Capabilities

- `graph-view-subscription`: the view lifecycle (one-watcher projection), the
  view-rate coalescing contract, the snapshot/delta consistency contract, the
  read-after-write coherence contract, per-subscriber at-most-once backpressure,
  and coexistence with raw `WatchAll`.

## Impact

- New capability spec `openspec/specs/graph-view-subscription/spec.md` (on
  archive); ADR-081.
- Subsumes #571's watcher-tax half (graph-query reads from the shared view
  instead of its own ENTITY_STATES `WatchAll`; the whole-client poison latch
  stays with #571).
- Coexists with, does not replace, raw `WatchAll` in `natsclient`.
- Downstream (deferred, opt-in): graph-query, graph-gateway (#211 MCP), and any
  multi-client product become consumers; existing hand-rolls migrate
  incrementally. Verified per-site migration table in ADR-081 — the correctness/
  de-dup win is broad; the fan-out win is concentrated on the two multiply-watched
  buckets (COMMUNITY_INDEX, ENTITY_STATES); two sites (graph-ingest bootstrap
  sweep, lifecycle per-workflow watchers) are out of scope.

## Non-goals

- Not changing NATS per-consumer delivery/flow-control semantics.
- Not replacing raw `WatchAll` for independent-filter / historical-replay /
  independent-ack consumers.
- Not an event log — intermediate per-key revisions may be coalesced away; full
  history stays with the raw KV lane / JetStream (#340).
- Cross-process re-publish (`graph.view.<name>.delta`) is DEFERRED, not built.
- Bulk one-shot reads / pagination out of scope (#176 — different axis).
- The two `natsclient` ergonomics from #579 (attribute the slow-consumer log;
  expose pending-limit config) are split out as independent small fixes.
- No pluggable backpressure policies, query language, persistence tiers, or
  multi-region (rejected as over-engineering — ADR-081).
