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
    // Submit failure MUST Nak (redeliver), never silently drop. Use a submit
    // context NOT bound to msgCtx's 30s message timeout (a block past AckWait would
    // otherwise return an ignored error → message neither enqueued nor acked). See B1.
    if err := c.ingestPool.SubmitBlocking(c.ctx, ingestWork{entity: entity, msg: msg, seq: msg.Metadata().Sequence.Stream}); err != nil {
        _ = msg.Nak()   // let the server redeliver rather than lose the message
    }
})
```

- **Decode-once:** the closure decodes/extracts the entity; the lane reuses the parsed
  `*graph.EntityState`. No double parse (the `KeyOf` reads `entity.ID`, already parsed).
- **Ack moves into the lane** (`processIngest`): still explicit, still at-least-once,
  still acks on ingest error (today's semantics — a failed merge logs + increments
  `c.errors` and acks). **Panic recovery must move with it** (see below): the lane's
  `Process` runs OUTSIDE `safeHandleMessage`'s `recover()`, so the primitive — not
  graph-ingest — MUST recover panics in `Process`.

### Redelivery safety (BLOCKING — adversarial review B1)

Moving ack into the lane makes JetStream **redelivery reachable under the exact
overload this feature targets**: a message that sits in a bounded lane queue longer
than `AckWait` (default 30s, `stream.go:316`) is redelivered by the server, re-hashes
to its lane, and is processed *after* a newer message for the same entity already
landed — and because `MergeTriples` is arrival-order newer-wins with **full-set-replace**
for multi-valued predicates (`graph/helpers.go:100-107`), the stale redelivery
wholesale-overwrites the newer relationship set. This is the corruption the ADR exists
to prevent, re-entered through the ack-timing change. It is NOT "panic-only" (the prior
draft's error).

**Fix — a per-entity applied-sequence guard, so correctness does not depend on
`MaxAckPending`/`AckWait` sizing.** Each message carries its JetStream stream sequence
(`msg.Metadata().Sequence.Stream`, monotonic per stream). The merge MUST drop (ack, no
apply) any message whose stream sequence is **not newer** than the last sequence already
applied to that entity. Two viable implementations (decide in impl):
- **Durable (preferred):** stamp the applied stream sequence on the entity; the CAS
  `Get` already reads the entity, so compare-and-skip is free, memory-safe, and
  restart-safe.
- **In-lane map:** a per-lane `map[entityID]uint64` (lane-goroutine-local, lock-free
  since each lane owns a disjoint key set) — simpler, but grows with distinct-entity
  cardinality (needs bounding) and resets on restart.
Sizing `MaxAckPending` to drain within `AckWait` stays as defense-in-depth, but the
sequence guard is what makes redelivery *safe* rather than *rare*.

## Correctness

| Concern | Resolution |
|---|---|
| Per-entity order (arrival-order merge, gh#466) | same `entity.ID` → same lane → serial submit order. Dispatch is single-goroutine (`consumer.Consume`, nats.go one dispatcher), so submit order == arrival order. Preserved. |
| Redelivery reorder (**B1, BLOCKING**) | ack-in-lane makes AckWait-expiry redelivery reachable under overload → stale re-apply overwrites newer via full-set-replace. Fixed by the **per-entity applied-sequence guard** above (drop seq ≤ last-applied), NOT by ack timing. |
| Same-entity CAS | same key never runs on two lanes ⇒ **no concurrent CAS on that entity's OWN key** ⇒ no self-inflicted `ErrKVRevisionMismatch` retry. |
| Cross-entity CAS (**H1** — claim corrected) | ingesting entity A ALSO writes OTHER keys: relationship-target stubs (`ensureRelationshipTargetsExist`, `component.go:1886/1999-2045`, unconditional), foreign edges (`routeForeignEdges`), and shared hierarchy containers (`EnableHierarchy`). So cross-lane contention on those keys is real (esp. entity-birth, hierarchy, relationship-dense). It is made **safe by atomic create-if-absent (`Create`→`ErrKVKeyExists` no-op) + CAS retry, NOT by lane affinity** — no lost update, but the "no concurrent same-key CAS" claim is only true for an entity's own key. `cas_retries_total` is therefore a **contention-observability** signal, not a keying-correctness proof (legit cross-entity writes make it non-zero). |
| At-least-once ack | ack in the lane after `ingestEntity`; `AckExplicit` tolerates out-of-order acks across lanes. Submit failure Naks (never silent-drop). |
| Panic in lane (**H2**) | `Process` runs outside `safeHandleMessage.recover()`; an unrecovered lane panic would crash the sole writer. The **primitive MUST recover panics in `Process`**, Nak (or otherwise dispose) the message, and keep the lane goroutine alive so future keys hashing there are not stranded. |
| Poison message | decode failure in the closure → ack-drop + `errors++` (today's behavior, kept). |
| Lifecycle ordering (**M3** — correctness, not cleanup) | pool MUST be built BEFORE subscriptions start (else first message submits to a nil pool); `Stop` MUST drain lanes BEFORE the KV store / NATS connection closes (else in-flight merges fail). Both normative. |
| Suffix index (**L1**, pre-existing) | `updateSuffixIndex` (`component.go:2563`) is last-writer-wins `Put` keyed by suffix (not entity ID) in a separate bucket — two entities sharing a suffix race across lanes as they already did when serial (LWW, no CAS). Noted, not newly broken; no fix. |
| Head-of-line blocking (**M2**) | `SubmitBlocking` blocks the single dispatch goroutine when one lane is full → a hot key stalls dispatch to all lanes. No deadlock/reorder, but ~N× is optimistic under key skew — a known limitation; the throughput test uses a skewed-key corpus, not just uniform. |

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
- `graph_ingest_cas_retries_total` counter — CAS-conflict retries. This is a
  **contention-observability** signal, NOT a proof of correct keying (H1): an entity's
  *own* CAS is never concurrent, but legitimate cross-entity referential writes
  (relationship-target stubs, foreign edges, shared hierarchy containers) DO touch
  shared keys and can retry. Read it as "how much cross-entity contention is happening,"
  and expect ~0 only on workloads without hierarchy / dense relationships / entity-birth
  churn (e.g. steady-state semboids). A spike is a workload signal, not necessarily a bug.
- `graph_ingest_redeliveries_dropped_total` counter — messages dropped by the applied-
  sequence guard (B1); a healthy small number under overload, a large number means
  MaxAckPending/AckWait are badly sized.
- existing `entities_updated_total` remains the throughput signal.

Metric plumbing follows the existing `sync.Once` getter + `MetricsRegistry` pattern
(`component.go:41-61`, `RegisterHistogramVec` per `pkg/worker/pool.go:139`).

## Backpressure — what `MaxAckPending` actually does (M1)

Correction to the prior draft: graph-ingest is **not** "unlimited unacked" today —
nats.go defaults `MaxAckPending` to **1000** (not unlimited; `-1` is unlimited). And
`consumer_pending_messages` is stream backlog (NumPending, *undelivered*), which
`MaxAckPending` does not bound at all — only the lanes (throughput) drain it. So the
honest purpose of plumbing `max_ack_pending` is to **raise/tune the 1000 delivered-
unacked ceiling so N lanes can stay fed**, paired with the bounded per-lane queues that
cap in-memory work. Memory safety comes from the bounded lane queues + `SubmitBlocking`,
not from `MaxAckPending`. (Also fix the stale "0 means unlimited" comment at
`stream.go:46` — 0 means server-default 1000.)

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
3. **QueueDepth default** and the `MaxAckPending` value — size against AckWait (30s) as
   defense-in-depth to minimize redeliveries; correctness under redelivery is already
   guaranteed by the applied-sequence guard (B1), so this is a tuning knob, not a
   correctness dependency.
4. **Applied-sequence guard storage** — durable stamp on the entity (memory-safe,
   restart-safe, preferred) vs per-lane in-memory map (simpler, needs cardinality
   bounding). Decide in impl.
5. **Panic disposition in `Process`** — the primitive recovers and Naks; confirm a
   recovered panic does not wedge the lane (lane goroutine must continue).
