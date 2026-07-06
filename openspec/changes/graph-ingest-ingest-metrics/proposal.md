# graph-ingest ingest observability + backpressure knob (gh#480, part 1)

## Why

gh#480 characterizes graph-ingest as ingest-bound (~670 msg/s, box ~92% idle,
`consumer_pending_messages` climbing 0→87k, e2e publish→visible p99 ~32s) but flags a
concrete **observability gap**: graph-ingest exposes throughput counters
(`entities_updated_total`, `mutation_rejections_total`) but **no per-message
processing-duration histogram**, so queue wait cannot be separated from processing time
at the component. Consumers must infer end-to-end latency downstream (semboids added an
app-side ENTITY_STATES watch probe to do exactly this).

This change ships the **low-risk, high-value half** of gh#480 first — the metrics that
make the ingest pipeline measurable in place, plus the `max_ack_pending` backpressure
knob — ahead of the keyed-concurrent-dispatch work (ADR-072, deferred). It is additive,
has no ordering/concurrency hazard, and lets the throughput fix be measured against real
numbers rather than inferred.

## What Changes

- **`graph_ingest_processing_duration_seconds`** (histogram) — time spent in
  `ingestEntity` (the merge + CAS write), i.e. *processing* time. On the gh#480 profile
  this is ~1% of an idle machine; the histogram makes that visible per-message rather
  than as an aggregate guess.

- **`graph_ingest_ingest_lag_seconds`** (histogram) — the age of a message when
  graph-ingest begins processing it (`now − msg.Metadata().Timestamp`), i.e. *queue
  wait* — how long the message sat in the stream/delivery buffer before ingest reached
  it. This is the split the issue asks for: paired with processing-duration it separates
  backlog latency (the 0→87k climb) from per-message compute, and it works **today** in
  the serial path (no concurrency change needed to measure it).

- **`max_ack_pending` port plumbing.** Today there is **no config path** to
  `MaxAckPending`: `JetStreamPort` (`component/port_jetstream.go`) has no such field, so
  every JetStream input runs at the nats-server default (1000 delivered-unacked). Add
  `max_ack_pending` to `JetStreamPort` → `component.ConsumerConfig` →
  `StreamConsumerConfig` (already honored at `natsclient/stream.go:320`) so operators can
  tune consumer backpressure per port. Additive; absent = today's behavior. Also fix the
  stale `stream.go:46` comment ("0 means unlimited" → server-default 1000; `-1` =
  unlimited).

## Impact

- **Affected specs:** `graph-ingest` (ADDED: an ingest-measurability requirement —
  processing-duration + ingest-lag histograms that separate queue wait from processing).
  The `max_ack_pending` port field is additive plumbing, not a capability contract
  (JetStreamPort has no spec; one field doesn't warrant seeding one).
- **Affected code:** `processor/graph-ingest/component.go` (two histograms wired via the
  existing `sync.Once` getter + `MetricsRegistry` pattern; observe around the consume
  closure); `component/port_jetstream.go` (`max_ack_pending` field + copy in
  `applyJetStreamConsumerConfig`); graph-ingest's `ConsumerConfig`→`StreamConsumerConfig`
  mapping; `natsclient/stream.go` comment only.
- **No behavior change:** metrics are read-only; `max_ack_pending` defaults to today's
  behavior. No ordering, ack, or dispatch change. Safe to ship without an e2e tier (unit
  + schema gate suffice); no ADR (additive observability + a config field).
- **Sets up ADR-072:** the keyed-dispatch change adds lane-depth / in-flight / queue-wait
  (pool) metrics on top of these, and uses `max_ack_pending` for its bounded backpressure.

## Non-goals

- Keyed-concurrent dispatch / the throughput fix itself (ADR-072, deferred — this is the
  measurement + backpressure foundation only).
- `cas_retries_total` — deferred to the keyed-dispatch change (it is ~0 in the serial
  path and needs retry-count plumbing through the CAS helper; it earns its keep only once
  concurrency can cause cross-entity contention).
- Lane/queue-depth/in-flight metrics — meaningless without the pool; they land with
  ADR-072.
