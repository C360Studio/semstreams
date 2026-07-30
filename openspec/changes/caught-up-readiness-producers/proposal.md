## Why

Two independent consumers cannot tell **caught-up** from **merely started**.

- **gh#712 (Semdragon):** a fresh-state/restart replay captured a parity snapshot while graph-ingest
  was still applying entities. `graphSummary.total_entities > 0` was true and health was green, so
  nonempty-and-healthy read as settled. It is not.
- **gh#732 (SemMachina):** nothing signals that the rule processor's bootstrap entity-watch replay
  has finished. `Start` returning is not that signal, and measured intervals are not even
  consistently positive — replay has been observed both before and after `Start` returns, so there
  is no constant to sleep on and asserting either ordering asserts a race.

Both are asking for the same primitive, and the substrate for it already exists: ADR-083 established
readiness as watched KV state in `GRAPH_STATUS`, with `graph.IndexStatusResponse` as the envelope
and `graph.EvaluateReadinessGate` as the single home for gate semantics. **This change adds two
producer keys and one client-side fold. It is not a new readiness system.**

### gh#712's stated mechanism is wrong, and building on it would produce the wrong design

The issue attributes the race to inference stages running asynchronously. They do not. Hierarchy
inference is **fully synchronous and inside the same CAS write** —
`processor/graph-ingest/component.go:2603-2617` carries the in-code header "SYNCHRONOUS HIERARCHY
INFERENCE: Get hierarchy triples BEFORE writing entity to storage", the triples are appended at
`:2615` and marshaled at `:2625`, and `graph/inference/hierarchy.go` contains zero `go func`.
graph-ingest's only non-test goroutines are teardown, a `wg.Wait()`-bounded helper that completes
before ack, and a read-only query path. **There is no async stage mutating the graph after ack.**

So there are no inference stages to track. What Semdragon actually observed was graph-ingest
**consumption backlog**, plus gh#713's duplicate re-fire (now fixed, PR #747), plus a third
contributor neither issue names: an in-process keyed-pool queue of up to
`defaultIngestLanes(8) × ingestLaneQueueDepth(256) = 2048` messages between the consume callback and
the graph write (`component.go:532`, `:538`, submit at `:1567`). **That backlog is invisible to
`NumPending`**, so any design measuring catch-up with `NumPending` alone under-reports by up to 2048
messages.

The missing producer is therefore **graph-ingest's own caught-up envelope**, not per-stage trackers.

### What makes it computable

The ack ordering is airtight. In `processIngest` (`keyed_ingest.go:125-225`) the write happens at
`:166`, the durable guard stamp at `:211`, and **Ack at `:221`** — every failure path Naks or Terms
without acking. So `ack(seq N)` implies that message's graph write is durable, which makes the
server-maintained consumer ack floor the contiguous high-water: restart-surviving, and it accounts
for the in-memory lane queue because those messages are delivered-but-unacked.

Outstanding work is `NumPending + NumAckPending`, per bound consumer.

## What Changes

- **Two new `GRAPH_STATUS` producer keys** — `graph-ingest` (backlog readiness) and `rule`
  (bootstrap-replay completion) — published on the existing shared heartbeat.
- **A backlog producer omits the revision-lag fields.** graph-ingest consumes multiple streams whose
  sequence spaces are independent (already stated in the `graph-ingest` spec), so there is no single
  scalar revision. `IndexedRevision`/`TargetRevision` are contractually in the **ENTITY_STATES KV
  revision space** — ADR-084 D3 pins them as comparable to a caller's `kv_revision` *by a test*.
  Writing a stream sequence there is a category error that would silently corrupt every
  read-your-writes check in the system. Both are `omitempty`; they stay absent.
- **Rule reports completion per watcher generation**, not per process. Watchers are keyed
  `(bucket, pattern)` and the watcher set is **runtime-mutable** via a component-config PUT, which
  re-runs replay for a re-added pattern. A graph-index-style process-lifetime latch would report
  "bootstrapped" while a freshly-added pattern was mid-replay — gh#732's exact bug in a new costume.
- **One additive envelope field, `bootstrap_scope`** — the size of the initial build the producer
  latched against, in that producer's own unit. `bootstrap_complete && bootstrap_scope == 0` is
  precisely "authoritatively nothing to do," which is the distinction gh#732 raises and which is
  **not recoverable from the wire today for any producer**, including graph-index.
- **Aggregation is a client-side fold over a consumer-declared key list**, delegating each key to
  the existing `EvaluateReadinessGate`. No aggregate is published.
- **One read-only HTTP dump** of the watched keys plus consumer-local freshness. Not a verdict.
- **Two honesty fixes the change exposes**: rule's `Health()` reports healthy after its entity-watch
  lane latches degraded, and `processor/rule/processor.go:896-897` claims `Start` ensures watchers
  started — `run()` closes `ready` at `:515` *before* calling `watchEntityStates` at `:518`.

## Non-Goals

- **No published aggregate.** It becomes the one producer whose envelope is derived rather than
  observed, so its staleness reports the aggregator's liveness rather than the producers'. On any
  defer the consumer needs per-producer detail anyway. The producer set is also deployment-dependent
  (graph-ingest appears in 11 configs, graph-index in 8, rule in 8, graph-embedding in 4), so a
  framework-published aggregate would have to guess its own membership.
- **No staleness or lag tolerance knob.** ADR-085 deleted `max_staleness` before it shipped: a bound
  at or below the publish heartbeat is unsatisfiable. Readiness gates on health; staleness is
  reported.
- **No `/readyz` integration.** That is the orchestrator's "can this process serve" contract; folding
  data-plane coverage into it makes a healthy binary flap unready under ordinary write load.
- **No GraphQL readiness field** — blocked behind a real defect gh#712 itself noticed (the
  `data.graphSummary.data.*` double nesting), which is filed separately rather than built on.
- **No coverage of the request/reply mutation lane.** A request lane has no backlog and its writes
  commit before the reply is sent, so "caught up" says nothing about mutations a caller has not yet
  issued. Claiming otherwise would manufacture a false settled signal.
- **No envelopes for graph-clustering / spatial / temporal / structural** — four more producers, no
  named consumer for any. File when one appears.

## Impact

**NOT BREAKING.** Additive throughout: two new keys in an existing bucket (no enumeration exists —
every `NewWatcher` call site names explicit keys, and there is no `Keys()` over `GRAPH_STATUS`), one
`omitempty` field, new helpers with no signature changes, one new route.

Two ways it could accidentally become breaking, both guarded in the requirements: a
framework-declared mandatory key list would break the five configs that run graph-ingest without
graph-index; and writing the metrics MODIFY as a prohibition would break graph-index dashboards.

Run **`task e2e:structural`** (graph-ingest + graph-index + rule on one stack) and
**`task e2e:statistical`** (adds graph-embedding + graph-clustering, exercising the multi-watcher
consumer path) before merge regardless of the not-breaking verdict.

Closes gh#712. Closes gh#732.
