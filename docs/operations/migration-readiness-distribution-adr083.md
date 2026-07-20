# Breaking Change: Readiness Is KV State, Staleness Is Time (ADR-083)

This is the SemStreams-local release note for the
`readiness-distribution-and-staleness-contract` change (follow-on to gh#590). It is a
clean pre-v1 break: there is no dual distribution path, no consumer fallback poll, and
no deprecation window. Both breaks fail **loudly** — a no-responders transient and a
config decode error — never silently.

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
    reading.Status, reading.Fresh, graph.GateExact, graph.GateConfig{})
```

Do not hand-roll a `Get` loop. `Get` and `Watch` are equally stale on a
tick-published key, and `Get` adds a round-trip on the very connection whose
saturation caused gh#590.

**Fail-closed semantics are unchanged.** A received not-ready still defers; anything
unknown (no bucket, no key, deleted key, backend fault, undecodable value) still fails
closed. State distribution adds one case a request could not have: a feed gone quiet
past the freshness window — a producer that died holding a `Ready` key — which is also
unknown, so it also fails closed.

### Deployments without graph-index

A consumer deployed without its producer has no key and is permanently unknown. That
is correct and unchanged; the existing `allow_ungated_reads` escape covers it, exactly
as it covered the unreachable-subject case before.

## Break 2 — `index_lag_tolerance` becomes `max_staleness`

graph-clustering's revision-count tolerance (ADR-082, shipped in .156) is replaced by
a duration.

```jsonc
// Before — revisions
{ "index_lag_tolerance": 500 }

// After — wall time. Empty or "0" means exact catch-up, identical to the old default.
{ "max_staleness": "3s" }
```

**Symptom if unmigrated:** the component fails to start with a loud decode error
naming the replacement field. It does not silently ignore the removed key.

There is no automatic translation, because none is correct: the revision count that
corresponded to a given staleness depended on the write rate and `coalesce_ms` of the
deployment that set it. Pick the time bound you actually want the view to be within.

`max_staleness` only ever *relaxes* the caught-up requirement. The hard stops —
`degraded`, `reset_required`, and an empty/pre-enumeration index — defer under every
tolerance, unchanged from ADR-082.

## Diagnosing a defer

The clustering defer path is now structured. One log line carries `status_known`,
`status_age`, `state`, `lag`, `staleness_ms`, `reason`, and the watch/bucket error
when present, and `defer_total{reason}` counts by
`hard_stop | over_staleness | status_unknown | empty`.

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
3. Swap `index_lag_tolerance` for `max_staleness` in every graph-clustering config.
4. Retarget any monitoring or conformance probe that requested the status subject.
