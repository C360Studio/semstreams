# fusion-consistency-simplification

## Why

Fusion currently carries four separately-designed consistency claims (the top
`!Ready` gate, the full `IndexStatus` envelope echo, the `IndexNotReady`
degrade sites, and — until ADR-083 Break 3 deleted it — `ViewRevision.Coherent`),
and the readiness gate offers four declared modes. Roughly thirteen concepts now
answer "is this data good enough", and nearly all of the excess sits in the one
question an eventually-consistent graph cannot answer: authoritative absence.
The cost is not theoretical: gh#597 (semsource, filed against .156) shows `Fuse`
silently dropping the top-ranked entity — "I could not hydrate it" is
indistinguishable from "it does not exist" all the way to the caller — and the
#592/#593 read-path saga shows coverage-shaped gating hurting both surfaces:
fusion returning empty-honest envelopes on every write burst (its top gate),
and the graph/query client's direct-index gate erroring on ordinary lag (one
comment frames that gate's job as the cutover window; this change redefines
the contract to exactly that). This is the deletion pass
agreed as the ADR-083 follow-up (owner directive, 2026-07-20): fusion gates on
**health**, reports **staleness**, returns ranked evidence, and **says what it
failed to hydrate**.

## What Changes

- **BREAKING** — Fusion's top gate regates on *health* instead of coverage:
  `Fuse` proceeds under ordinary catch-up lag (reporting `staleness_ms` in the
  envelope) and defers only on hard stops (`degraded`, `reset_required`),
  status-unknown, or an empty/pre-enumeration index. `Ready=false` with an
  empty envelope stops being the ordinary-lag response; consumers that used it
  as a fallback trigger see real ranked results with staleness reported instead.
- **BREAKING** — Reverse-index reads (`graph/query` client) regate on health
  the same way: the classified `index_not_ready` transient fires for genuine
  health failures (the gh#474 cutover window, its documented job), not for
  ordinary lag. This deliberately reopens and supersedes the #592 close-out
  ("retry the transient" stops being the answer to plain catch-up lag).
- **BREAKING** — No silent hydration omission (gh#597 part 1): batch entity
  hydration reports the IDs it could not return (with a not-found vs error
  distinction at the handler), `fusionnats.Entities` reconciles the response
  against the requested set, and the fusion `Response` carries what failed to
  hydrate so a dropped seed is visible, never inferable-as-absent.
- The four gate modes collapse, and then the collapse goes one step further
  than first scoped. `exact` and `degrade-honest` were one evaluation (the
  caller's reaction was never the gate's business) and `sticky-bootstrap` moves
  into graph-index as its private bootstrap concern; `bounded-staleness` was
  initially reparameterized as a declared freshness requirement, and is now
  **deleted outright** (ADR-085, owner-agreed 2026-07-21). The gate takes a
  status reading and nothing else.
- **BREAKING** — `max_staleness` and the `Freshness` type are removed with no
  replacement, along with the `over_staleness` and `staleness_unknown` defer
  reasons. Freshness gating only ever existed to serve the absence license this
  change retires; with the license gone it had exactly one call site, and that
  consumer (community detection) is the safest possible reader of a stale view.
  The knob's own satisfiability floor — a bound at or below the publish
  heartbeat is unsatisfiable, measured at ~52% of ticks at 3s — was the tell.
  Community detection now runs whenever the index is healthy and records the
  view age it ran at. **`max_staleness` never shipped in a tag**, so sister
  repos migrate `index_lag_tolerance` → nothing and never meet the intermediate
  field.
- `pkg/graphview` gains the reporting half of the same principle: it already
  gated correctly (bootstrap and fail-closed only, never age) but exposed no
  time dimension at all. It now reports the applied revision's KV write time as
  an atomic pair, carried on snapshots. Gating there is untouched. This retires
  the parked ADR-082 G5 follow-up as a reporting task rather than the gating
  task it was originally framed as.
- The envelope gains `bootstrap_complete` (additive): the gh#474
  cutover/bootstrap window becomes wire-observable, which is what makes
  health-only gating safe for direct-index clients and keeps the
  authoritatively-empty graph serving.
- Score observability (gh#597 part 2, minimal slice): the resolve similarity is
  carried through instead of discarded, exposed as an opt-in per-node debug
  field, so a ranking surprise is diagnosable without bypassing the product
  surface over raw NATS.
- Two ADRs record the decisions. **ADR-084**: readiness licenses health, never
  absence; the ADR-066 "authoritative not-found" license is retired.
  **ADR-085**: gate on health, report freshness — the freshness parameter
  ADR-084 kept is deleted, because retiring the license removed its only
  justification while leaving its machinery standing.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `fusion`: top-gate semantics (health, not coverage), unhydrated-seed
  reporting on the Response, opt-in score observability, degrade taxonomy
  shrinks with the gate-mode collapse.
- `graph-index-readiness`: the canonical-gate requirement collapses from four
  declared modes to health alone; the "empty = authoritative not-found" license
  language is removed; the view-rate consumer's distinct readiness
  interpretation is removed with its tolerance; the read-path transient's
  trigger narrows to health failures.
- `graph-view-subscription`: adds a currency-reporting surface (applied
  revision paired with its KV write time, carried on snapshots) that no API
  gates on. Existing gates unchanged.
- `graph-query`: batch entity reads must report unreturned IDs (not-found vs
  fault) instead of silently omitting them; reverse-index read gating narrows
  to health.

## Impact

- **Code**: `pkg/fusion` (engine gate, `Response`, `engine_lens.go` degrade
  sites), `pkg/fusion/fusionnats` (`Entities` reconciliation + Status
  unknown-vs-wiring split), `processor/graph-ingest/query.go` (batch handler
  missing-reporting), `graph/index_status.go` + producers
  (`bootstrap_complete`), `graph/readiness_gate.go` (mode collapse),
  `graph/query/client.go` (health regate),
  `processor/graph-clustering/component.go` (gate call-site migration,
  zero-default preserved), `processor/graph-index` (responder unchanged;
  publishes the bootstrap bit), `processor/research-graph-execute`
  (second batch consumer, reconciles instead of blessing silent omission).
- **Consumers (sem\*)**: semsource is the primary consumer (doc_context /
  code_search lenses, MCP gateway, UI) — its `Ready=false → fall back` and
  retry-the-transient paths change meaning and its scorecard gains real
  diagnostics; semconnect's conformance probes read the same envelope;
  semboids is telemetry-only and unaffected by the fusion surface. Owner
  manages sister-repo migration; this change ships the framework side +
  migration doc. **No tag for semsource before this change lands** (owner
  sequencing decision, 2026-07-20).
- **Docs/specs**: ADR-084 (decision), migration notes extending the ADR-083
  wave, deltas to the three capabilities above. ADR-083's D4 mode table is
  superseded in place by the collapsed gate.

## Non-goals

- No snapshot isolation or coherence claims for fusion — coherent-view
  consumers use `pkg/graphview` (ADR-081). The Coherent deletion (ADR-083
  Break 3) is already shipped and is not revisited here.
- No re-adding an absence license under a new name: nothing in this change may
  let a caller conclude "not in the response ⟹ not in the graph". Unhydrated
  reporting says *what wasn't returned*, never *what doesn't exist*.
- No transport changes: GRAPH_STATUS KV distribution, the shared watcher,
  `max_staleness`, and the heartbeat cadence (ADR-083) are untouched.
- No full scoring explain-plan: part 2 ships only the minimal similarity/rank
  passthrough; lens scoring internals stay private.
- No product-domain semantics (Product Boundary): lens vocabularies and
  domain predicates stay in the products.
- No sister-repo code in this change: lockstep PRs are owner-managed and
  follow the merge, as with the ADR-083 wave.
