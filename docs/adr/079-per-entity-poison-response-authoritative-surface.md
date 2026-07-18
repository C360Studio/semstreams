# ADR-079: Per-Entity Poison Response and the Retirement of Dedicated Contract-Guard Watchers

## Status

**Accepted (2026-07-18).** The 5-lens adversarial review ran against the
`poison-response-scoping` OpenSpec change (verdicts in its `adversarial-review.md`); the core
sufficiency claim HELD, the scope was widened to the findings, and the implementation passed
the semstreams-reviewer pre-merge review (APPROVE-WITH-FIXES; both MEDIUM findings fixed:
verify-after-record closing the last record/clear interleaving, plus covering tests for the
cache-invalidation and suffix-lane scenarios).

## Context

The fail-closed wave (beta.147–.150) shipped ENTITY_STATES with layered poison defense. Two
layers are enforcement — the marshal-site write gate (nothing invalid commits) and per-read
validating decode (nothing invalid is served) — and the adversarial review could not construct
a serve or launder counterexample against that pair. The remaining layers are detection and
response, and they overshot: one commit added **three dedicated live `WatchAll` contract
guards** (graph-ingest self-watch; rule processor full-firehose guard that runs even with zero
entity-watch patterns; a second graph-clustering watcher beside its input watcher), each
re-delivering every entity write full-payload on the shared connection and re-running the full
validating decode — a measured per-write tax (gh#562: ~+3 msgs/entity, a −6–9% ingest-ceiling
share for semboids). And graph-ingest's response was surface-global: one poisoned entity
latched every entity query into `graph_state_reset_required`, although every read independently
validates its own bytes.

## Decision

1. **Enforcement is byte-authoritative.** Refusal to serve or merge derives solely from
   decoding the bytes actually stored under the touched key — the write gate and per-read
   validating decode are the two enforcement points, and they are sufficient. No watcher,
   latch, or inventory gates a read or write decision.
2. **No dedicated contract-guard watchers — validation rides existing read points.** The sole
   writer detects resident poison in a boot snapshot sweep (validate everything, then stop);
   every consumer validates what it consumes on its own input/replay path; a component that
   consumes no entity state holds no ENTITY_STATES watcher. All three dedicated guards are
   retired under this one principle.
3. **Poison response scope is a property of the reader class.** The **authoritative read
   surface** (graph-ingest's query lanes, serving stored values per-request) refuses exactly
   the poisoned entities and recovers on repair without restart; its operator signal is a
   revision-stamped, observability-only, self-healing inventory (degraded Health + one gauge +
   per-entity ERROR + debug enumeration), with aggregate reads failing loudly naming every
   poisoned entity. A **projection owner** — anything serving a derived view built from watched
   or replayed state, explicitly including watch-maintained derived-view readers like
   `graph/query/client.go` whose cache depends on its watcher — keeps sticky whole-view
   reset-required semantics with restart recovery: a projection must distrust its entire
   derivation once its input was poisoned; the authoritative store re-checks truth per read.
4. **Poison classifications are typed-fatal everywhere, and repairable conditions don't destroy
   data.** Mutation seams that encounter resident poison return the typed code (never
   retryable-internal); ingest arrivals blocked by resident poison are Nak'd (bounded), not
   Term'd; consumers react at the scale the code now means (agentic-loop fails the touching
   loop, not its whole task intake).
5. **Repair is an audited operator verb, never a validator reflex.** Capture-before-delete
   (History=1), then delete + recreate via the canonical mutation API — the only wire repair
   path. Auto-delete is rejected permanently: the contract has tightened repeatedly, a delete
   reflex turns every tightening into a mass-deletion event, and the delete IS an event via the
   KV twofer. Mass-poison escalates to the documented clean-wipe/reseed contract.

## Consequences

- The read-shaped per-mutation watcher tax goes to zero in a semboids-shaped deployment
  (three guards retired); their firehose A/B is the acceptance instrument for gh#562. A
  shortfall now cleanly indicts a non-watcher contributor.
- Known residual, filed as follow-up: `graph/query/client.go` keeps its watcher (its cache
  requires it) — the per-write tax persists in the five processes that embed it until that
  client's cache coherence is redesigned.
- Detection of out-of-band writes moves from write-time to first-touch/boot on the
  authoritative surface (containment unchanged); consumers that watch state for work still
  detect at delivery time.
- Operators alert on the poisoned-entities gauge and degraded Health, enumerate via debug
  status, and repair per-entity without restart; co-resident sticky consumers (projections,
  rule kill switch once poison is consumed) still restart per their class contract.
- The reader-class split becomes a named cross-repo contract (`graph-state-contract`
  capability) products can rely on.
