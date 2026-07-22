## Context

`graph-embedding` is an ADR-066 revision-lag readiness producer. Its low-water
watermark (`pkg/revlag`, shared with graph-index) advances on **every** terminal
outcome — generated, failed, and no-text/skipped — and this is deliberate:
`readiness.go:20-25,64` documents that keying completion off "started" or holing
on a failure would let one permanently-failing or telemetry-only entity pin the
watermark forever. The readiness projection is the shared
`graph.ComputeIndexStatus` (`graph/index_status.go:166`), whose state rule is
`ready ? ready : (stuck ? degraded : building)` — **"ready wins."**

The failure (#613): embedding's `stuck` detector is completions-liveness only
(`trackEmbeddingProgress`, `readiness.go:176` — "no terminal completion for 120s
while not caught up"). Nothing projects *failures* into the state. So when
semembed is down at cold start, every entity reaches a terminal `failed`, the
watermark reaches target, `ready` is true, and "ready wins" publishes
`State: ready` over zero usable vectors. The `graph-index-readiness` spec already
requires `failedCount > 0 → degraded` **unconditional (not gated on Ready)** — but
the shared projection does not actually enforce that; graph-index only satisfies
it because a failed *required index write* leaves a watermark hole, so its `ready`
is never true under failure. Embedding is the first producer whose watermark
reaches target *with* failures, which is exactly the case the shared function
never handled.

Second, the failure *detail* already exists and is unreachable: `SaveFailed`
(`storage.go:346`) writes `Status: failed` + `ErrorMsg` per entity, and
`StatusFailed` is read by nothing (`worker.go:441` processes only
`StatusPending`). A bare `degraded` bit does not tell an operator "semembed is
down" from "three poison entities."

Consumers: `graph-embedding` publishes to `GRAPH_STATUS/graph-embedding`; fusion
(`pkg/fusion/fusionnats`) and graph-query (`graph/query/client.go`) hold that
watch. Products running embedding and gating on it: semsource, semboids.

## Goals / Non-Goals

**Goals**
- Embedding never reports `ready` while it holds failed records; `FailedCount > 0
  → degraded`, enforced in the shared projection.
- `degraded` is legible: how many failed, of what kinds, since when (always-on),
  and which entities (drill-down).
- Failed records recover without operator action on the normal re-delivery path.
- Concurrent byte-identical content pays one embedder call (#630).
- Cross-lane dedup-key identity (already true) is locked by a test; #627 closed.

**Non-Goals**
- Changing the watermark's advance-on-every-terminal behavior (deadlock
  avoidance) — the fix is orthogonal to it.
- Changing graph-index's readiness semantics — it keeps its hole-based degraded;
  it simply passes `FailedCount = 0` into the widened input struct.
- A durable repair loop that retries failures without re-delivery — that is #625
  (Epic C). Here, recovery rides the existing ENTITY_STATES re-delivery.
- Distributed cross-process singleflight — process-local this increment.
- Building #627 Option-2 (digest-key fetch-skip) — re-homed, deferred.
- A configurable degraded threshold — owner-settled `FailedCount > 0`.

## Decisions

### D1 — `FailedCount` input to the shared `ComputeIndexStatus`

Add `FailedCount uint64` to `IndexStatusInputs` and project it **before** "ready
wins":

```go
switch {
case in.FailedCount > 0:
    state = IndexStateDegraded   // unconditional — spec's "not gated on Ready"
case ready:
    state = IndexStateReady
case in.Stuck:
    state = IndexStateDegraded
}
```

`Ready` stays coverage-accurate (full coverage with failures IS covered); `State`
carries the health verdict. Consumers gate on `State` (ADR-085 "coverage inert at
the gate"), so `Ready: true, State: degraded` is a coherent, honest envelope.

- **Why not a watermark hole (mirror graph-index)?** It reintroduces the deadlock
  `readiness.go:20-25` exists to prevent — a permanently un-embeddable or no-text
  entity would pin the watermark and report `building`/large-lag forever.
- **Why not force `Ready = false`?** `Ready` is defined as exact revision coverage
  (`graph-index-readiness` spec). Failures with full coverage are covered; the
  problem is health, and health lives in `State`.
- **Blast radius on graph-index:** none. `IndexStatusInputs` is a struct;
  graph-index's literal omits the new field (defaults `0`), so its state path is
  unchanged. It already gets `degraded` via the hole + `Stuck`.

### D2 — `FailedCount` is a **current-failed** gauge, not a cumulative counter

`degraded` must clear on recovery, so the input must be the count of entities
*currently* in `failed` (net of those later regenerated/deleted), not total
failures ever. Maintain an in-memory mutex-guarded
`failed map[entityID]failureInfo{reason, at}` in the component:

- terminal `failed` → `failed[entityID] = {reason, at}`
- terminal `generated`/`deleted`/`skipped` → `delete(failed, entityID)`
- `FailedCount = len(failed)`; the reason histogram and first-failure time (L2)
  are derived from the same map; the L1 `failed` gauge is set to `len`.

**Seeding across restart:** a one-time bootstrap scan of `EMBEDDING_INDEX` for
`Status == failed` populates the map (precedent: `storage.go:665` already iterates
records). This gives an accurate `FailedCount` immediately after bootstrap,
independent of re-delivery timing; before bootstrap completes the component
reports `building`, which dominates, so there is no false-`ready` window.

- **Why the map, not an atomic counter?** The map subsumes count (`len`), reason
  breakdown (L2), and the debug-enumerate seed (L3) in one structure, and it makes
  the +/- transition unambiguous without a prior-status read.

### D3 — Plumb the terminal *outcome* through the completion callback

`completeEmbedding(entityID, sourceRevision)` (and the `onTerminal` callback,
`worker.go:155,410`) carry no outcome, so the component cannot route map updates.
Widen the terminal callback to carry the outcome and (for failures) the reason:

```go
type TerminalOutcome int // Generated | Failed | Skipped | Deleted
// onTerminal(entityID string, sourceRevision uint64, outcome TerminalOutcome, reason string)
```

`completeEmbedding` still advances the watermark for **all** outcomes (unchanged),
and additionally updates the failed-map per D2. The worker classifies the reason
at `markFailed` and passes it through.

### D4 — Reprocess `StatusFailed` on re-delivery

`worker.go:441` gates on `record.Status != StatusPending`. Widen it to also
accept `StatusFailed`, so a re-delivered entity (restart re-delivers via
`WatchAll` `DeliverLastPerSubject`, or a new revision arrives) re-embeds. On
success the terminal `generated` outcome removes it from the failed-map →
`FailedCount` drops → `degraded` clears once the last failure resolves. The
`SaveGenerated` revision-CAS (`storage.go:378`) already tolerates the equal-
revision retry. No self-loop: reprocessing is driven by re-delivery, not by the
worker re-queuing itself; a persistently-failing entity stays `failed` (correctly
`degraded`) until the next re-delivery or the #625 repair loop.

### D5 — Reason classification: a bounded enum on the `Record`

Add `Reason string` (omitempty) to `Record` (`storage.go:70`), set by `SaveFailed`
(new `reason` param). Classify at each `markFailed` site (`worker.go:459,539,556,
562,672`) into a **bounded** enum:

| Site | Reason |
|---|---|
| text extraction failed (`:459`) | `content_error` |
| dedup check failed (`:539`) | `internal` |
| generation failed (`:556`, from `gerr`) | `connection_refused` / `timeout` / `dimension_mismatch` / `embedder_error` |
| no embedding returned (`:562`) | `embedder_error` |
| save failed (`:672`) | `internal` |

A small `classifyEmbedErr(err) string` maps the embedder error shape to the
network/timeout/dimension buckets, defaulting to `embedder_error`. The **raw
`ErrorMsg` is never a metric label** (unbounded → cardinality blowup); the bounded
`reason` is. This mirrors inc 2's fusion `body_hydration_failures_total{reason}`.

### D6 — L2 envelope fields (GRAPH_STATUS)

Add to `IndexStatusResponse` (`graph/index_status.go:34`), all additive/omitempty
(wire-compatible):

```go
FailedCount    uint64            `json:"failed_count,omitempty"`
FailedReasons  map[string]uint64 `json:"failed_reasons,omitempty"` // bounded ≤ enum size
FirstFailureAt string            `json:"first_failure_at,omitempty"`
```

Populated by `computeEmbeddingStatus` from the failed-map. Bounded cardinality
(≤ enum size) keeps the watched key compact — **no per-entity list on
GRAPH_STATUS**. graph-index leaves them zero-valued (omitted).

### D7 — L1 metrics

`metrics.go`, per-registry register-or-get (no process-global):
- `..._embedding_failed` gauge — set to `len(failed)` on each transition.
- `..._embedding_failures_total{reason}` counter — incremented at `markFailed`
  with the classified reason.

### D8 — L3: production escape hatch (fusion/graph-query) vs opt-in debug enumerate

`/query-pattern`: the failure **aggregate** is status (a KV-watched envelope) —
fusion and graph-query already hold the `GRAPH_STATUS/graph-embedding` watch, so
the L2 breakdown reaches operators through their existing production status relay
with **no new endpoint**. The **per-entity** list is a bounded read over the
durable `EMBEDDING_INDEX` records, exposed via the existing message-logger
`/kv/{bucket}` surface with a `Status==failed` filter — and message-logger is a
**debug surface, off by default** (an operator enables it at reboot). Production
observability = L1 + L2 + the fusion/graph-query aggregate relay, complete with
message-logger off.

- **Why not a bespoke production per-entity endpoint now?** The aggregate answers
  the production triage question ("outage vs a few poison entities"); the
  per-entity list is a forensic need served by the debug tier. Building a new
  production responder speculatively violates measure-before-building. If a
  production per-entity need emerges, extend fusion's existing missing/unhydrated
  reporting (a query touching entity X reports `embedding_failed: reason`) rather
  than a standalone API — filed, not built here.

### D9 — #627 regression test (no production change)

`truncateAtWord` is already the single rune-safe routine for both lanes
(`worker.go:793,884`). Add a test that embeds byte-identical over-cap content
through the inline lane and through a `StorageRef` (offloaded) lane and asserts
identical `DedupKey` (and identical embedded bytes). Then close #627; re-home its
Option-2 (digest-key fetch-skip) as a deferred optimization.

### D10 — #630 process-local singleflight

Wrap the embedder `Generate` call (`worker.go:~556`) in a `singleflight.Group`
keyed by the dedup key: the first of K workers holding byte-identical content
makes the one remote call; peers wait and share the resulting vector, then each
performs its **own** `SaveGenerated` (distinct entityIDs, revision-CAS). Test: K
concurrent workers, identical content, a counting fake embedder asserts exactly
one `Generate`. Distributed (cross-process KV reservation) is deferred — the
dedup key already collapses *sequential* cross-process dupes; only *simultaneous*
cross-process identical content stampedes, the rarest slice; file the distributed
variant only if measured.

## Risks / Trade-offs

- **`Ready: true` with `State: degraded`** could mislead a consumer that reads
  `Ready` instead of `State`. → The `graph-index-readiness` spec already mandates
  State-gating (ADR-085); the canonical gate uses `State`. Documented in the spec
  delta; no consumer in-repo reads `Ready` past the gate.
- **failed-map memory under a mass outage** (all entities failed) → the map is
  entityID→small struct, same order as `EMBEDDING_INDEX`; bounded by graph size,
  acceptable. No per-entity data leaves GRAPH_STATUS.
- **Reason misclassification** for an unrecognized embedder error → defaults to
  `embedder_error`; refine the classifier over time. Bounded either way.
- **Persistent failure stays `degraded`** with no auto-retry between re-deliveries
  → correct (it IS degraded); the durable repair loop is #625.
- **Rollback safety:** `Record.Reason` is additive (old workers ignore it);
  `FailedCount` is in-memory (not persisted); envelope fields are omitempty. A
  rolled-back worker reverts to the pre-change behavior (reports `ready` over
  failures — the old bug) without crashing or corrupting data. No overloaded-field
  hazard like #635/#638.

## Migration Plan

1. Widen `IndexStatusInputs` / `IndexStatusResponse` (additive). graph-index
   compiles unchanged (new input defaults 0).
2. Land worker/storage/readiness changes behind the same schema; no data
   migration — `EMBEDDING_INDEX` records gain an optional `reason` field on the
   next failure write; existing records are unaffected.
3. **BREAKING behavior:** embedding readiness flips `ready → degraded` under
   failures. Per CLAUDE.md, a relevant e2e tier must be green before tag — the
   statistical/semantic embedding tier, extended with a semembed-down → degraded +
   failure-detail scenario.
4. Rollback: revert the binary; safe per the rollback-safety note above.

## Open Questions

- Final reason enum values — confirm against the actual embedder error shapes
  during implementation (the table in D5 is the starting set).
- Should graph-index later *also* pass its `failedCount` into the new input to
  make the shared projection the single enforcement point (retiring reliance on
  the hole)? Out of scope here (Non-goal), worth a follow-up once embedding proves
  the path.
- Is a production per-entity drill-down needed at v1, or do the L2 aggregate + the
  opt-in debug enumerate suffice? Proceeding on "suffice"; file if a real
  operator need appears.
