# Design — keyed-concurrent entity ingest (gh#480, ADR-072)

## Problem restated

`ConsumeStreamWithConfig` → `consumer.Consume(cb)` dispatches serially; graph-ingest's
closure (`component.go:983`) runs `handleMessage(data)` then `msg.Ack()` inline, so the
next message waits for the full 2-RTT CAS merge. Latency-bound at `1/(2×RTT)` ≈ 670/s,
11/12 cores idle.

## The keyed-ordered primitive

`pkg/dispatch` gains a keyed pool (working name `KeyedPool[W]`), a sibling to
`BoundedDispatcher` — both bounded, but this one **key-partitioned and ordered**.

```
Submit(work W)  →  lane = hash(KeyOf(work)) % Lanes  →  laneQueue[lane] (bounded)
each lane: a single goroutine draining its queue in order, calling Process(work)
```

- **Ordering:** items with the same key always hash to the same lane and are processed
  serially in submit order. Different keys spread across lanes and run concurrently.
- **Config:** `Lanes int` (worker count), `QueueDepth int` (per-lane bound), `KeyOf
  func(W) string`, `Process func(ctx, W) error`. `SubmitBlocking` for backpressure
  (blocks when the target lane is full) plus a non-blocking `Submit`.
- **Shutdown:** `Stop(ctx)` stops accepting, drains in-flight lanes, returns when all
  lanes are idle or ctx expires.
- **Hashing:** FNV-1a over the key string, `% Lanes`. Even spread for entity-ID keys;
  no consistent-hashing need (lane count is fixed for a process lifetime).

Why not extend `pkg/worker.Pool`: Pool is one shared channel + N workers pulling
without affinity (`pkg/worker/pool.go:80,226`). Keyed ordering needs N *separate*
lane queues; that's a structurally different pool, so it's a new type beside Pool, not
a mode on it. `BoundedDispatcher` stays the unordered/KV-completion primitive.

## graph-ingest integration (the first and only composer)

Today (`component.go:983`):
```go
ConsumeStreamWithConfig(ctx, cfg, func(msgCtx, msg) {
    c.handleMessage(msgCtx, subject, msg.Data())   // decode + extract + ingest (2 RTT)
    msg.Ack()                                        // inline, serial
})
```

After:
```go
// Start: build the pool once
c.ingestPool = dispatch.NewKeyedPool(ctx, dispatch.KeyedConfig[ingestWork]{
    Lanes: c.config.IngestLanes, QueueDepth: ...,
    KeyOf:   func(w ingestWork) string { return w.entity.ID },
    Process: c.processIngest,          // ingestEntity(entity) + w.msg.Ack()
}, deps)

// consume closure: decode ONCE, submit, return fast (no inline CAS, no inline ack)
ConsumeStreamWithConfig(ctx, cfg, func(msgCtx, msg) {
    entity, err := c.decodeEntity(msg.Data())   // the extract path, once
    if err != nil { c.errors++; msg.Ack(); return }   // poison → ack-drop (today's behavior)
    _ = c.ingestPool.SubmitBlocking(msgCtx, ingestWork{entity: entity, msg: msg})
})
```

- **Decode-once:** the closure decodes/extracts the entity; the lane reuses the parsed
  `*graph.EntityState`. No double parse (the `KeyOf` reads `entity.ID`, already parsed).
- **Ack moves into the lane** (`processIngest`): still explicit, still at-least-once,
  still acks on ingest error (today's semantics — a failed merge logs + increments
  `c.errors` and acks, no redelivery). Only a panic Naks (via `safeHandleMessage`,
  unchanged).
- **Backpressure:** `SubmitBlocking` blocks the consume goroutine when the target lane
  is full → natsclient stops dispatching → paired with `MaxAckPending` this caps total
  in-flight. Set `MaxAckPending ≈ Lanes × QueueDepth × k` so lanes stay fed without
  unbounded unacked growth.

## Correctness

| Concern | Resolution |
|---|---|
| Per-entity order (arrival-order merge, gh#466) | same `entity.ID` → same lane → serial submit order. Preserved. |
| CAS contention | same key never runs on two lanes ⇒ **no concurrent CAS on one key** ⇒ no `ErrKVRevisionMismatch` retry storms. Cross-lane keys are distinct KV keys. Contention *drops to ~0* (measured by `cas_retries_total`). |
| At-least-once ack | ack in the lane after `ingestEntity`; `AckExplicit` tolerates out-of-order acks across lanes. |
| Poison message | decode failure in the closure → ack-drop + `errors++` (today's behavior, kept). |
| Redelivery ordering | graph-ingest acks even on ingest error, so the only redelivery is a panic-Nak (a bug path). A redelivered message re-hashes to the same lane; document that panic-redelivery may reorder relative to a lane already past that revision — acceptable and rare (CAS would reject a stale-revision write anyway). |
| Shutdown | `Stop` drains lanes before the KV store closes; consume context cancel stops new dispatch first. |

## Metrics (gh#480 observability gap — first-class)

Primitive (`pkg/dispatch`, label `pool` name):
- `dispatch_queue_wait_seconds{pool}` histogram — submit→pickup (**queue wait**).
- `dispatch_processing_duration_seconds{pool}` histogram — `Process` time.
- `dispatch_queue_depth{pool}` gauge — queued items (aggregate; per-lane optional).
- `dispatch_inflight{pool}` gauge — lanes currently in `Process`.
- `dispatch_submitted_total` / `_completed_total` / `_dropped_total{pool}` counters.

graph-ingest:
- `graph_ingest_processing_duration_seconds` histogram — `ingestEntity` (merge+CAS)
  time (**processing**, the split the issue names — pair with `dispatch_queue_wait`).
- `graph_ingest_cas_retries_total` counter — CAS-conflict retries; **proves** keying
  removed contention (expect ~0).
- existing `entities_updated_total` remains the throughput signal.

Metric plumbing follows the existing `sync.Once` getter + `MetricsRegistry` pattern
(`component.go:41-61`, `RegisterHistogramVec` per `pkg/worker/pool.go:139`).

## Config surface

- graph-ingest `Config` (`component.go:241`) gains
  `IngestLanes int json:"ingest_lanes" schema:"type:int,default:8,category:advanced"`
  (auto-schema'd; `Validate` clamps `<1 → 1`, sane upper bound). `1` = serial (opt-out).
- `JetStreamPort` (`port_jetstream.go:10`) gains `MaxAckPending int
  json:"max_ack_pending"`; `applyJetStreamConsumerConfig` copies it to
  `component.ConsumerConfig` (new field) → graph-ingest maps it into
  `StreamConsumerConfig.MaxAckPending` (already honored at `stream.go:320`).

## Open questions for implementation

1. **Default lane count** — start at `8` (issue: 11/12 idle; KV-RTT-bound, so ~8 lanes
   should approach a KV-server or core limit before diminishing). Tune against the
   semboids repro; the histogram/inflight metrics make the sweet spot visible.
2. **Per-lane vs aggregate queue-depth metric** — aggregate first (cheap); add
   per-lane label only if imbalance is suspected.
3. **QueueDepth default** and the `MaxAckPending` multiplier `k` — size against AckWait
   (30s default) so a full pipeline drains before redelivery.
