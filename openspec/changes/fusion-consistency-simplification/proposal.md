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
#592/#593 read-path saga shows the coverage-shaped gate erroring on ordinary
lag, which its own doc-comment says is not its job. This is the deletion pass
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
- The four gate modes collapse: `exact` and `degrade-honest` are one evaluation
  (the caller's reaction was never the gate's business), `sticky-bootstrap`
  moves into graph-index as its private bootstrap concern, `bounded-staleness`
  becomes the single freshness reading. The public gate surface shrinks to
  health + optional staleness bound.
- Score observability (gh#597 part 2, minimal slice): the resolve similarity is
  carried through instead of discarded, exposed as an opt-in per-node debug
  field, so a ranking surprise is diagnosable without bypassing the product
  surface over raw NATS.
- New ADR (084) records the decision: readiness licenses health, never absence;
  the ADR-066 "authoritative not-found" license is retired.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `fusion`: top-gate semantics (health, not coverage), unhydrated-seed
  reporting on the Response, opt-in score observability, degrade taxonomy
  shrinks with the gate-mode collapse.
- `graph-index-readiness`: the canonical-gate requirement collapses from four
  declared modes to health + optional staleness bound; the "empty =
  authoritative not-found" license language is removed; the read-path
  transient's trigger narrows to health failures.
- `graph-query`: batch entity reads must report unreturned IDs (not-found vs
  fault) instead of silently omitting them; reverse-index read gating narrows
  to health.

## Impact

- **Code**: `pkg/fusion` (engine gate, `Response`, `engine_lens.go` degrade
  sites), `pkg/fusion/fusionnats` (`Entities` reconciliation),
  `processor/graph-ingest/query.go` (batch handler not-found reporting),
  `graph/readiness_gate.go` (mode collapse), `graph/query/client.go` (health
  regate), `processor/graph-index` (absorbs sticky-bootstrap privately).
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
