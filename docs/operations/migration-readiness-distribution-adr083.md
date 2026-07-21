# Breaking Change: Readiness Is KV State and Licenses Health, Not Absence (ADR-083 + ADR-084)

This is the SemStreams-local release note for **one wave with two changes**:
`readiness-distribution-and-staleness-contract` (ADR-083, follow-on to gh#590) moved
readiness onto KV state, and `fusion-consistency-simplification` (ADR-084) then retired
the thing readiness had been wrongly asked to license. They ship under one tag so
consumers migrate the readiness surface **once**.

It is a clean pre-v1 break throughout: no dual distribution path, no consumer fallback
poll, no deprecation window. Breaks 1 and 2 fail **loudly** — a no-responders transient
and a config decode error. Break 3 removes a wire field and Breaks 4–6 change
SEMANTICS, so those get explicit consumer checklists: a semantics change compiles fine
and is exactly the kind that reaches production silently.

**If you read only one section**, read
[Break 4](#break-4--readiness-gates-on-health-not-coverage): `Ready == false` no longer
means "fall back".

## What changed, and why

graph-index's ADR-066 readiness envelope used to be answered per request on the
`graph.index.query.status` subject. In a single-binary deployment that request travels
the same NATS connection as the ENTITY_STATES firehose, so it timed out under exactly
the load that makes readiness interesting — and a timed-out gate logged the same line
as a genuinely not-ready index (gh#590).

Readiness is now published as watchable state: producers write the envelope to the
`GRAPH_STATUS` KV bucket every heartbeat, and consumers hold the last value instead of
paying a round-trip per decision. Separately, the clustering tolerance moves from a
revision count to wall time, because the correct revision count shifts 2–4× with
`coalesce_ms` alone and linearly with write rate.

See [ADR-083](../adr/083-readiness-as-distributed-state.md) for the decision and
`openspec/specs/graph-index-readiness/spec.md` for current behavior.

## Break 1 — the `graph.index.query.status` subject is REMOVED

The subject and its `handleQueryStatusNATS` handler are gone. Only the *status*
subject was removed; the read-query subjects (`graph.index.query.incoming`, `byName`,
`outgoing`, …) and their classified-transient retry contract are untouched.

**Symptom if unmigrated:** a no-responders / request-timeout classified transient on
every status request.

### Operator probe

```bash
# Before
nats request graph.index.query.status ''

# After — the KV twofer: Get is the point-in-time probe, Watch is the live feed,
# and History 3 gives you the last few transitions after an incident.
nats kv get GRAPH_STATUS graph-index
nats kv watch GRAPH_STATUS graph-index
nats kv history GRAPH_STATUS graph-index
```

`graph-embedding` publishes its own envelope to the `graph-embedding` key in the same
bucket. One key per producer.

### In-process consumers

Use the shared watcher — it is the single consumer-side code path, and it is what
guarantees your freshness rule matches everyone else's:

```go
import "github.com/c360studio/semstreams/graph/readiness"

w := readiness.NewWatcher(natsClient, readiness.KeyGraphIndex)
if err := w.Start(ctx); err != nil { /* wiring error, not a readiness answer */ }
defer w.Stop()

reading := w.Read()
if !reading.Fresh {
    // UNKNOWN — fail closed. This is the old no-responders branch.
    // reading.Err and reading.Age say why, for the log line.
}
proceed, reason := graph.EvaluateReadinessGate(
    graph.StatusReading{Status: reading.Status, Fresh: reading.Fresh})
```

The gate takes the reading and nothing else — there is no mode, tolerance, or
freshness argument to pass. `reading.Age` is still yours for the log line; it just
is not a gate input.

Do not hand-roll a `Get` loop. `Get` and `Watch` are equally stale on a
tick-published key, and `Get` adds a round-trip on the very connection whose
saturation caused gh#590.

**Fail-closed semantics for UNKNOWN are unchanged.** Anything
unknown (no bucket, no key, deleted key, backend fault, undecodable value) still fails
closed. What a *received* envelope means did change — see Break 4.

State distribution adds one case a request could not have: a feed gone quiet past the
freshness window — a producer that died holding a `Ready` key — which is also unknown,
so it also fails closed.

### Deployments without graph-index

A consumer deployed without its producer has no key and is permanently unknown. That
is correct and unchanged; the existing `allow_ungated_reads` escape covers it, exactly
as it covered the unreachable-subject case before.

## Break 2 — `index_lag_tolerance` is removed, with no replacement

graph-clustering's revision-count tolerance (ADR-082, shipped in .156) is **deleted**.
Delete the key; there is nothing to set in its place.

```jsonc
// Before — revisions
{ "index_lag_tolerance": 500 }

// After — no tolerance key at all
{ }
```

**Symptom if unmigrated:** the component fails to start with a loud decode error
naming what happened. It does not silently ignore the removed key.

**If you tracked this work in flight, note the collapse.** An intermediate design
renamed this field to a duration-valued `max_staleness`, and earlier drafts of this
document described that rename. `max_staleness` was never tagged — it existed only on
`main` between releases — and ADR-085 deleted it before it shipped. Sister repos
upgrading from .156 go straight from `index_lag_tolerance` to nothing and never need to
learn the intermediate field. A config carrying *either* key fails startup loudly.

**What replaces it: nothing, because there is nothing to tune.** Community detection
now runs whenever the index is healthy, at whatever age the view happens to be, and
reports that age on `semstreams_graph_clustering_staleness_at_detection_ms` for every
run. Readiness gates on health alone — `degraded`, `reset_required`, an incomplete
initial build, an unrecognized `state`, or a dead status feed still defer, exactly as
before, and none of those was ever answerable by a tolerance. See ADR-085 for why the
tolerance's own satisfiability floor was the evidence it should not exist.

## Break 3 — the fusion graph facet no longer claims coherence

`view_revision.coherent` is REMOVED from the fusion graph projection (the opt-in
`WantGraph` facet). `view_revision.start` and `view_revision.end` remain, as plain
observations: the indexed revision sampled before seed resolution and re-sampled after
the facet's last graph fetch.

```jsonc
// Before
"view_revision": { "start": 41, "end": 41, "coherent": true }

// After — observations only; no coherence claim exists on this wire
"view_revision": { "start": 41, "end": 41 }
```

The field was deleted, not re-tuned, because it was never soundly provable: fusion
assembles a projection from N independent reads with no snapshot, so two revision
samples agreeing could never establish that no read between them was stale. ADR-083's
heartbeat distribution turned the unsound signal into a vacuous one (both samples read
the same held value, so it became ~always true), but the unsoundness predates the
transport change. See ADR-083's Consequences.

**Consumer checklist:**

- A strict decoder that requires the `coherent` key fails decode — loud, fix by
  dropping the field.
- A lenient decoder reads it as absent/false. Any logic gated on `coherent == true`
  goes permanently quiet; in particular, any path that used the claim to license
  **deleting or reconciling items absent from the projection** must be rebuilt, not
  re-gated. Absence from a fusion projection is never authoritative — a seed the
  engine failed to hydrate is indistinguishable from one that does not exist (gh#597).
- A consumer that genuinely needs a coherent single-revision view should use
  `pkg/graphview` (ADR-081), which has real snapshot/revision semantics. Retrieval
  fusion is best-effort ranked evidence.

## Break 4 — readiness gates on HEALTH, not coverage

**This is the one that changes behavior without changing a signature.** It compiles,
it type-checks, and it silently alters what your fallback path does.

`Ready` reports COVERAGE: the index has applied every committed revision up to its
target. Callers had been using it as a proxy for "is this index sound to read from",
and those are different questions. Coverage was never evidence of soundness — an index
caught up to every revision ever committed still knows nothing about a source that
never published — and under continuous write `Ready` is false essentially always, so
the proxy failed exactly when the graph was busiest. That is what made semsource retry
the read-path transient (#592) and what made fusion return empty envelopes from a
perfectly healthy graph.

Reads now gate on **health**: fresh status, no hard stop, and initial build complete.

| Situation | Before | Now |
|---|---|---|
| Healthy index, caught up | serves | serves |
| Healthy index, behind under write | **withholds** | **serves**, reporting `staleness_ms` |
| Initial build / gh#474 cutover incomplete | withholds | withholds (`bootstrap_complete=false`) |
| `degraded` / `reset_required` | withholds | withholds |
| Status unknown (feed dead/absent) | withholds | withholds |

### What `Ready == false → fall back` becomes

If your code looks like this, it is now wrong:

```go
if !status.Ready {
    return fallbackToGrep()   // WRONG after ADR-084
}
```

`Ready == false` is the steady state of a busy index. Ask the question you actually
meant:

```go
// "Is this index sound to read from?" — the health question, and the only
// question the gate answers.
proceed, reason := graph.EvaluateReadinessGate(
    graph.StatusReading{Status: st, Fresh: fresh})
if !proceed { return fallbackToGrep() }   // reason says WHY

// "How current is what I just read?" — REPORT it, never gate on it.
myResult.StalenessMs = st.StalenessMs

// "Is MY write visible?" — the one sound per-entity check.
if status.IndexedRevision >= myRevision { /* my write is indexed */ }
```

`bootstrap_complete` is a NEW envelope field. An envelope without it reads `false` and
therefore fails closed — which is why producers and consumers must move together.

## Break 5 — gate modes collapse into health alone

`GateMode`, `GateConfig`, and the four `Gate*` constants are gone. They were four
dressings of one conflation. So is the freshness parameter that briefly replaced them:

| Before | Now |
|---|---|
| `GateExact` | nothing — pass the reading; a healthy index serves however far behind |
| `GateBoundedStaleness` + `GateConfig{MaxStaleness: d}` | nothing — report `staleness_ms` on your result instead |
| `GateDegradeHonest` | nothing — it always evaluated as exact; degrading is a caller choice |
| `GateStickyBootstrap` | nothing — stickiness is a consumer-local latch, not a gate concern |

The gate takes a `graph.StatusReading{Status, Fresh}` and returns a verdict. There is
no second argument.

**If you tracked this in flight**, an intermediate design replaced the modes with a
`graph.Freshness` declaration (`FreshnessExact` / `FreshnessWithin(d)` /
`FreshnessNone`) and a `StatusReading.Age` field, and earlier drafts of this document
showed that API. None of it tagged. ADR-085 deleted it: freshness gating existed only
to serve the absence license ADR-084 retired, it had one call site, and a bound at or
below the publish heartbeat turned out to be unsatisfiable. Consumers go straight from
`Gate*` constants to no argument at all.

Defer reason `empty` is now `bootstrap_incomplete`. If you alert on
`defer_total{reason="empty"}`, retarget it. Drop any `over_staleness` or
`staleness_unknown` alert — neither reason exists; the surviving set is `hard_stop`,
`status_unknown`, `unrecognized_state`, `bootstrap_incomplete`.

## Break 6 — partial hydration is reported

`graph.query.batch` replies gain `missing: [{id, reason}]` naming every requested ID
that did not hydrate, and fusion responses gain `unhydrated`. Both are additive and
omitted when nothing was lost, so a fully-hydrated response is byte-unchanged.

Previously an ID whose read came back not-found was simply absent from a shorter list,
and no consumer could tell "this does not exist" from "this was not read" (gh#597).

**Neither field licenses the inverse inference.** `not_found` says this read did not
find the key. It does not say the entity never existed, and an `unhydrated` seed is a
statement about the read, not about the world. Fusion deliberately synthesizes NO miss
when every seed failed to hydrate.

Two related fixes ride along:

- `fusionnats.Entities` now returns entities in REQUESTED order. The engine ranks by
  position, and graph-ingest returns cache hits first, so cache residency alone could
  demote the top resolve seed.
- `RetrievalClient.Resolve` returns `[]Seed{ID, Similarity, HasSimilarity}` instead of
  `[]string`, and `Entities` returns a `Hydration` struct. Fusion responses can carry
  per-node `rank` and `similarity` via the opt-in `include_scores` request field.

## Diagnosing a defer

The clustering defer path is now structured. One log line carries `status_known`,
`status_age`, `state`, `lag`, `staleness_ms`, `reason`, and the watch/bucket error
when present, and `defer_total{reason}` counts by
`hard_stop | status_unknown | bootstrap_incomplete | unrecognized_state`. Every one of
the four names a distinct operator action, and none is answered by tuning a number.

### Reading the staleness metrics

`staleness_at_detection_ms` now records on **every** detection run rather than only the
runs a tolerance admitted, so it is a continuous view-age signal rather than a
near-threshold one. Alert on it if your product has a genuine currency requirement —
but alert knowing it is a **floor**: an undelivered revision cannot age it, so a
totally stalled feed looks arbitrarily current by this measure alone. Total stalls
surface as `state=degraded` via the stuck-watermark detector, not here.

`detection_duration_seconds` is still worth watching alongside it. Community detection's
run time scales with community SIZE, so as a graph consolidates a run can grow
several-fold (a semboids adoption run measured 4.4s climbing to 23.7s over one 90s
window), and a long run competes with the indexer for the same box, so the view is
staler at the next tick. Under ADR-082/083 that interaction could push a fixed
tolerance into deferring for reasons unrelated to index health — the tuning-dynamics
problem filed as gh#605. With the tolerance gone the interaction is still real but no
longer *gates* anything: both numbers rise, detection keeps running, and you are
looking at a throughput signal rather than a misconfigured knob.

`bootstrap_incomplete` means the producer has not finished its initial build in its
current process lifetime — a cold start or a gh#474 format cutover. It replaced
`empty`, whose `TargetRevision == 0` proxy was wrong in both directions: false during a
cutover, and true for the authoritatively-empty graph it then deferred forever.

`status_unknown` means the *feed* is the problem (bucket missing, producer down,
watcher starved) — not the index. That distinction is the one gh#590 spent three
investigation cycles recovering by hand.

## Upgrade order

`sem*` repos are house-managed and upgrade in lockstep. A mixed-version window is
accepted and safe in both directions: an un-upgraded consumer sees no responders, a
new consumer without a new producer sees a missing key. Both are fail-closed and
logged.

1. Upgrade SemStreams; confirm `nats kv get GRAPH_STATUS graph-index` returns an
   envelope that updates on the heartbeat.
2. Migrate consumers off the removed subject onto `graph/readiness`.
3. Delete `index_lag_tolerance` from every graph-clustering config; there is no
   replacement key.
4. Retarget any monitoring or conformance probe that requested the status subject.
5. Drop `view_revision.coherent` from fusion graph-facet decoders; move any
   delete-absent-items reconciliation onto `pkg/graphview` or remove it.
6. **Audit every `!Ready → fall back` branch** (Break 4). This is the step that does
   not announce itself: nothing fails to compile, and the symptom is a fallback path
   that stopped firing — or one that keeps firing on a healthy graph.
7. Drop `GateMode`/`GateConfig` call sites — `EvaluateReadinessGate` takes a status
   reading and nothing else (Break 5; the intermediate `Freshness` declaration these
   were first migrated to was deleted by ADR-085 before it tagged). Retarget
   `defer_total{reason="empty"}` alerts to `bootstrap_incomplete`, and drop any
   `over_staleness` or `staleness_unknown` alert — neither reason exists.
8. Optionally consume `missing` / `unhydrated` (Break 6) — additive, and the only way
   to tell a short result from a partial one.
