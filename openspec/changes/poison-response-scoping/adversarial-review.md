# Adversarial Review Evidence — poison-response-scoping

Five independent adversarial lenses (poison-path safety, concurrency/lifecycle, operational
signal, cross-contract, performance) ran against the pre-revision artifact set on 2026-07-18,
each instructed to break specific claims. This document records verdicts and the disposition of
every finding. The artifact set was revised in place to the dispositions below; the pre-revision
text is recoverable from git history.

## Verdict table

| Claim | Verdict | Disposition |
|---|---|---|
| C1 Write gate + per-read decode jointly sufficient (no serve, no launder) | **HELD** at core — all 9 write sites gated, write validator ≡ read validator, trusted-decode laundering impossible by case | Three periphery edges fixed (below) |
| C2 Observability-only inventory safe | **HELD** — nothing consults it; clear-on-commit sound | Hygiene gaps fixed (revision-stamped entries, clear-on-read) |
| C3a Snapshot-then-stop preserves boot detection | **HELD** — Start ordering clean, nil-marker semantics verified vs nats.go v1.48.0 | Documented: History=1 assumption, last-revision-wins drain, cross-process post-marker window |
| C3b Deliberate stop cannot be misclassified | **WEAKENED** — naive impl forks into a connection-wedging leak (blocked update callback holding w.mu) OR watch-lost misclassification | Design now mandates drain-to-close after Stop; mock watcher must close its channel |
| C3c Mutex-map latch replacement race-free | **WEAKENED** — record-after-clear interleaving leaves stale-unhealthy entry; entityQueryMu protocol becomes dead code | Revision-stamped inventory entries; delete the dead protocol outright; named test inversion |
| C3d Nothing else depends on the guard goroutine | **HELD** — flags settle before any subscription exists | — |
| C4a Health+gauge replaces the outage signal | **WEAKENED** — no crash-loop risk anywhere (verified exhaustively), but signal is first-touch not write-time, and no-restart claim was graph-ingest-only | Claims scoped; runbook directs alerting at gauge; co-resident sticky consumers named |
| C4b Mass-poison ergonomics | **WEAKENED** — no bulk verb, no enumeration surface | DebugStatus enumeration added; runbook escalates to clean-wipe contract above threshold |
| C4c Re-poison/flap behavior | **WEAKENED** — re-log implied not asserted; stale-unhealthy hole | Re-poison scenario added; clear-on-successful-read added |
| C4d Aggregate error names one entity | **BROKEN** as specified — N repair round-trips; fix is O(n) walk over already-materialized errors | Aggregate names ALL poisoned IDs + inventories all in one attempt |
| C4e semboids workflow dependence | **HELD** — their rig unaffected; one attribution phrasing flag | gh#562 reply must not present the watcher localization as semboids' conclusion |
| C5a Composes with predicate-contract-enforcement | **BROKEN** — its `predicate-contract` delta ("MUST block readiness… Queries remain not-ready") binds graph-ingest; "projection owner" undefined; gate 1.2 checked the wrong artifact class | `predicate-contract` MODIFIED delta added; definitions added; hard archive-ordering dependency declared |
| C5b StateContractError wire surface | **WEAKENED** — no wire break (headers/code only cross the wire); Error() string reaches Health LastError; Status string undecided | Round-trip tests enumerated; Status decision recorded; state_contract.go doc comment fix tasked |
| C5c Other readers of the typed contract | **BROKEN** — agentic-loop latches component-wide on first wire-code sight and holds tasks until restart; graph/query/client.go is an untouched authoritative-surface latch + live watcher | agentic-loop rescope tasked; client.go classified + carved out with follow-up filed |
| C5d Archive-order hygiene | **WEAKENED** — zero heading collisions either order, but cross-capability contradiction and a dangling reference if ours archives first | Ordering dependency declared; our requirement made self-contained |
| C5e Sister products only benefit | **WEAKENED** — nobody relies on the latch, but semsource embeds query.Client (restart-only recovery persists there) | Consumer claims scoped |
| C6a Watcher is the dominant per-mutation delta | **WEAKENED** — commit `cba784ea` added THREE watchers: graph-ingest guard, rule `startGraphStateGuard` (unconditional), clustering `startEntityContractWatch` (second watcher). +3 deliveries/write = semboids' +3 msgs exactly; original scope recovered ~1/3 | **Scope widened to all three watchers** |
| C6b Fix adds no hot-path cost | **WEAKENED** — clear-on-commit = mutex per mutation across 8 lanes as drafted | Atomic emptiness fast-path now REQUIRED |
| C6c Boot cost unchanged | **HELD** — drain already synchronous in Start; no deadline + Health blocked during drain (pre-existing) documented | Drain progress observability noted |
| C6d Post-change op count .146-equivalent | **BROKEN** for original scope (2 of 3 watchers remained) | Resolved by scope widening; residual table in design |

## Key facts established

- The fail-closed commit `cba784ea` (beta.147) added three live full-firehose `WatchAll`
  validators on ENTITY_STATES: `processor/graph-ingest/component.go:1184`,
  `processor/rule/entity_watcher.go:50` (runs even with zero entity-watch patterns),
  `processor/graph-clustering/component.go:1095` (in addition to clustering's input watcher).
  semboids wires all three components on one connection: +3 varz msgs/entity, matching their
  measurement exactly. graph-index gained no new watcher in the window.
- `graph/query/client.go` (same commit) is a fourth in-window watcher + process-lifetime
  whole-client latch, embedded by graph-query, graph-gateway, agentic-tools,
  research-graph-classify, fusionnats — and semsource's supersession processor. Its cache
  depends on that watcher for invalidation, so snapshot-then-stop does not transplant; it is
  carved out (classified as a watch-maintained derived-view reader with projection-owner
  semantics) and its per-write tax is filed as follow-up work. semboids embeds no query client
  (grep-verified), so the gh#562 A/B is unaffected by the carve-out.
- The old global latch only ever gated graph-ingest's own query lanes — ingest, mutation, and
  every external reader were never latch-protected (each validates per-value independently).
- `Component.UpdateEntity` has zero production callers: the only wire repair for a poisoned key
  is delete + recreate. The "Put-based repair lane" in the pre-revision design was fiction.
- Mutation read seams `mutations.go:717`, `:1060`, `:552` return retry-inviting internal errors
  for resident poison instead of the typed fatal code.
- Arrivals for a poisoned entity are currently Term'd — permanent loss of valid data during a
  repairable window; revised design Naks them (MaxDeliver-bounded) instead.
- ENTITY_STATES History=1: snapshot-marker completeness leans on it, and delete destroys
  forensic bytes — runbook mandates capture-before-delete.
- No component-health consumer restarts anything on Healthy=false (verified: ComponentManager
  ignores component health; onHealthChange never fired; docker probes are service-level only) —
  the crash-loop risk of degraded Health is nil in-repo.

## Dispositions requiring follow-up outside this change

1. `graph/query/client.go` per-write watcher + whole-client latch across five embedding
   consumers (cache-coherence redesign required) — file as its own issue; named in ADR-079.
2. `fetchEntitiesConcurrent` returns ctx.Err() after successful completion (pre-existing,
   harmless fail-closed) — noted, not tasked.
3. Boot drain has no deadline and blocks Health() for its duration (pre-existing) — noted in
   design; observability improvement only if it bites.
