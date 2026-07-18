# Poison Response Scoping

> Revised 2026-07-18 after the 5-lens adversarial review (see `adversarial-review.md`). Scope
> widened from graph-ingest's guard watcher to all three dedicated contract-guard watchers; the
> direct query client is explicitly carved out; response-scoping claims corrected.

## Why

gh#562: semboids measured a reproducible −9.1% ingest-ceiling regression (beta.146 → beta.149)
that survived both CPU-side fixes (PR #567 trusted RMW read, PR #570 write-path validation
collapse). Their follow-up measurement — ops/entity up ~+3 with per-bucket writes identical —
pointed at read-shaped per-mutation I/O they could not localize. Our code audit localized it:
the fail-closed commit (`cba784ea`, beta.147) added **three** dedicated live `WatchAll`
contract validators on ENTITY_STATES — graph-ingest's `startEntityStateGuard`, the rule
processor's `startGraphStateGuard` (unconditional, even with zero entity-watch patterns
configured), and graph-clustering's `startEntityContractWatch` (a second watcher alongside its
input watcher). semboids wires all three components on one NATS connection: three extra
full-payload deliveries plus three full validating decodes per entity write — matching their
measured delta exactly. Each guard re-validates writes that already passed the marshal-site
write gate, and each consumer of ENTITY_STATES independently validates what it consumes:
dedicated contract-guard watchers are self-observation of an already-gated path, taxed per
write on every deployment.

Separately, graph-ingest's poison *response* is disproportionate: one poisoned entity latches
the entire entity-query surface into `graph_state_reset_required`, although every query read
independently validates its own bytes, so correctness never depended on the global latch.

## What Changes

- **Retire all three dedicated contract-guard watchers** under one principle: *validation rides
  existing read points; no component holds a dedicated ENTITY_STATES contract-guard watcher.*
  - graph-ingest: boot snapshot sweep retained (synchronously validate the full snapshot,
    drain-to-close, then stop); no steady-state watcher. A deliberate stop MUST NOT be
    classified as watch loss.
  - rule processor: the dedicated full-firehose guard is removed; the sticky rule-evaluation
    kill switch is preserved, driven by validation of the entity values rules actually consume
    on their existing input path (validate-what-you-consume; poison on entities rules never
    consume cannot affect rule output).
  - graph-clustering: the contract-guard watcher (its only ENTITY_STATES watcher — the
    "input watcher" premise was corrected during implementation; clustering's input path is
    polled reads) is removed; the sticky projection latch is wired at the consuming read seam,
    latching within one detection cycle. Clustering ends with zero ENTITY_STATES watchers.
- **Scope graph-ingest's poison response per-entity.** A poisoned entity's reads and merges keep
  returning the typed `graph_state_reset_required` (bounded reasons unchanged); reads of other
  entities keep serving. Detection sites (boot sweep, query read, RMW classification on every
  mutation closure, mutation read seams) record into a per-entity poison inventory that is
  **observability-only** — never consulted by any read/write decision — feeding degraded Health
  (count + bounded sample), a single poisoned-entities gauge, once-per-entity structured ERROR,
  and a full-inventory enumeration surface (DebugStatus). Inventory entries are
  revision-stamped and clear on delete, on any newer successful commit, or on any successful
  validating read of the key (self-healing in both directions).
- **Fix the mutation read seams** that currently classify resident poison as retry-inviting
  internal errors (`entity.update`, `update_with_triples` CAS read, `create_with_triples`
  restamp read-back): all become the typed fatal classification.
- **Aggregate reads fail loudly and completely.** A multi-entity read that encounters poisoned
  entities fails as a whole with the typed error naming **every** poisoned entity encountered
  in that attempt (bounded list), and all of them are inventoried in that same attempt — never
  silent omission, never one-per-round-trip discovery.
- **Ingest arrivals for a poisoned entity are Nak'd, not Term'd** (MaxDeliver-bounded): resident
  poison is a repairable condition; valid arrivals during the repair window must not be
  permanently destroyed. Structurally-invalid candidates (the message's own fault) remain Term.
- **agentic-loop reaction rescoped**: the component-wide latch-and-hold-until-restart on first
  sight of the wire code becomes per-loop failure handling (the loop that touched the poisoned
  entity fails; task intake continues), matching the code's new per-entity meaning.
- **Operator repair verb documented**: capture-before-delete (History=1 — delete destroys the
  bytes), delete + recreate via the canonical mutation API (the only wire repair path —
  `UpdateEntity` has no production callers), guard-bucket reset on stream reseed, mass-poison
  escalation to the existing clean-wipe/reseed contract (docs/operations/17) above a threshold.
  There is deliberately no automatic deletion.

Behavioral change, not a wire break: no payload, subject, header, or API shape changes (the
typed error's `EntityID` field is additive). Failure-mode semantics of the entity query surface
change (global latch → per-entity refusal + degraded Health), which is a safety-posture decision;
the 5-lens adversarial review ran 2026-07-18 and its verdicts + dispositions are recorded in
`adversarial-review.md`.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `graph-ingest`: guard-watcher lifecycle (boot sweep + drain-to-close, no steady-state watch),
  per-entity poison response, revision-stamped observability-only inventory, aggregate
  all-poisoned-IDs semantics, Nak-on-resident-poison disposition, mutation read-seam
  classification, Health/metrics/enumeration. Existing structural fail-closed gate requirements
  untouched.
- `graph-clustering`: contract validation rides the input watcher; dedicated contract watch
  removed; sticky projection latch semantics unchanged.
- `graph-state-contract` (pending capability owned by the active change
  `predicate-contract-enforcement`): ADDED requirements — per-entity response scope for the
  authoritative graph-ingest query surface (self-contained definitions of "authoritative read
  surface" vs "projection owner" vs "watch-maintained derived-view reader"), the
  no-dedicated-guard-watcher principle, EntityID stamping. **Hard ordering dependency: that
  change's `graph-state-contract` delta must sync/archive before this one** (our requirements
  reference the sticky projection contract it introduces).
- `predicate-contract` (same owner): MODIFIED — the beta-cutover language "existing
  noncanonical ENTITY_STATES MUST block readiness… queries remain not-ready until clean
  reingest" is rescoped: whole-view readiness-blocking binds projection/replay consumers;
  the authoritative graph-ingest surface applies per-entity refusal per this change.

## Impact

- **Code**: `processor/graph-ingest/` (guard lifecycle, inventory, query scoping, mutation
  seams, keyed-ingest disposition), `processor/rule/entity_watcher.go` (guard retirement),
  `processor/graph-clustering/component.go` (watcher fold), `processor/agentic-loop/`
  (latch rescope), `graph/state_contract.go` (EntityID field + doc comment), runbook +
  operations docs.
- **Explicitly unchanged**: the marshal-site write gate; per-read validating decode on every
  lane; RMW poison classification; projection owners' sticky whole-view contract (graph-index,
  clustering, and every replay/projection consumer); the rule kill switch's stickiness once
  poison is actually consumed.
- **Explicitly carved out**: `graph/query/client.go` — a watch-maintained derived-view reader
  (its cache depends on its watcher for invalidation); it keeps its watcher and whole-client
  sticky latch under projection-owner semantics. Its per-write tax across five embedding
  consumers (incl. semsource supersession) is filed as follow-up work, named in ADR-079.
- **Consumers**: semboids is the acceptance instrument — their firehose A/B (beta.146 vs
  candidate) measures the macro recovery; with all three watchers retired the read-shaped
  per-mutation delta in their deployment goes to zero (verified: semboids embeds no query
  client). semsource/semconnect: improved graph-ingest query availability during poison
  incidents; semsource's own direct-read path (query client) is unchanged until the follow-up.
  Deployments running rule/agentic components during a poison incident: rule evaluation stays
  sticky once poison is consumed (restart per runbook); agentic-loop no longer wedges on
  first sight.

## Non-goals

- **No auto-delete or auto-repair.** A validator reflex that deletes authoritative state turns
  every future contract tightening into a mass-deletion event on upgrade (the contract
  tightened three times in four releases), and the delete IS an event — it fans out through KV
  watchers and reads downstream as legitimate removal. Deletion stays an audited operator verb.
- **No weakening of the write gate or per-read validating decode** — the fail-closed invariant
  keeps both remaining enforcement points.
- **No change to projection owners' sticky poison contract**, including the direct query
  client's whole-client latch (carved out above).
- **No change to ADR-055/056 ownership rules** or the structural identity gate.
- **No query-client migration** (cache-coherence redesign; five consumers) — follow-up.
- **No foreign-edge/OWNER_CLAIMS work** — measured irrelevant to this regression.
