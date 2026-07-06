# Keyed-concurrent entity ingest for graph-ingest (gh#480, ADR-072)

## Why

`graph-ingest` entity ingest tops out at **~670 msg/s** on a single node while the
box sits **~92% idle** (0.9 of 12 cores). The ceiling is structural, not CPU: each
entity update is one `consumer.Consume` callback run **serially** doing **two
sequential NATS KV round-trips** — a `Get` for the live revision + a revision-checked
CAS `Put`. Throughput is bounded by `1 / (2 × KV RTT)`; the merge/CAS compute is
~1% of an already-idle machine (gh#480 profile). Any producer whose steady-state
entity rate exceeds ~670/s backs up unboundedly (semboids' graph dial goes
ingest-bound; publish-side gh#470 sustains full rate with zero drops — the substrate
is the wall).

The fix is to overlap the KV round-trips across many in-flight messages. But
graph-ingest is the **sole writer of ENTITY_STATES**, and `graph.MergeTriples`
(`graph/helpers.go:108`) is **arrival-order** newer-wins — the incoming message wins
a `(subject,predicate)` conflict regardless of timestamp. So **naive** concurrent
dispatch corrupts state: two updates to the same entity processed out of order let
the *older* one win the last CAS. Per-entity ordering is therefore mandatory, not
optional (semboids republishes each boid at 30Hz — same-entity ordering is real).

## What Changes

- **New generic keyed-ordered dispatch primitive** (`pkg/dispatch` sibling of
  `BoundedDispatcher`; `pkg/worker.Pool` and `BoundedDispatcher` are both a single
  shared channel + N *unordered* workers — keyed-ordered is genuinely new). It routes
  each work item to `lane = hash(key) % N`; each lane is a serial goroutine, lanes run
  in parallel. Same key → same lane → **serial, ordered**; different keys → parallel.
  Bounded per-lane queues + graceful drain on stop. Ships with built-in Prometheus
  metrics (below). ADR-048-aligned: name the reusable primitive rather than hand-roll
  it in graph-ingest.

- **graph-ingest composes it**, keyed by entity ID. The consume closure
  (`component.go:983`) decodes the entity **once**, submits `{entity, msg}` keyed by
  `entity.ID`, and the lane runs `ingestEntity` then `msg.Ack()` — **ack moves into the
  lane** (today the closure acks synchronously after `handleMessage`). natsclient and
  its 26 other `ConsumeStreamWithConfig` callers are **untouched**. Three wins at once:
  N× throughput (overlapped RTTs), **per-entity order preserved** (the merge requires
  it), and **CAS contention eliminated** (same key never runs concurrently → no retry
  storms — strictly better than the issue's naive "concurrent dispatch").

- **Configurable lanes, default > 1.** A new `ingest_lanes` component config
  (default `8`, `1` = today's serial behavior for opt-out). Existing deployments get
  the throughput fix automatically; the default change to the sole-writer path is why
  this needs the e2e:core gate (below).

- **Bounded in-flight backpressure — plumb `MaxAckPending`.** Today there is **no
  config path to `MaxAckPending`** at all: `JetStreamPort`
  (`component/port_jetstream.go`) has no such field, so graph-ingest runs with the
  NATS default (unlimited unacked). With concurrency this must be bounded. Add
  `max_ack_pending` to `JetStreamPort` → `component.ConsumerConfig` →
  `StreamConsumerConfig` (already honored at `natsclient/stream.go:320`), and pair it
  with the bounded lane queues so total in-flight is capped and memory can't blow up
  (the 0→87k `consumer_pending_messages` climb in the issue).

- **Comprehensive Prometheus metrics (first-class — gh#480 observability gap).** The
  issue's core measurement gap is that queue wait can't be separated from processing
  time. The primitive and graph-ingest expose:
  - `graph_ingest_processing_duration_seconds` (histogram) — per-message
    `ingestEntity` time (merge + CAS), i.e. *processing* time.
  - `dispatch_queue_wait_seconds` (histogram) — submit→lane-pickup latency, i.e.
    *queue wait* — the exact split the issue asks for.
  - `dispatch_queue_depth` (gauge) — queued items (aggregate and/or per-lane);
    backpressure visibility, pairs with `consumer_pending_messages`.
  - `dispatch_inflight` / `dispatch_active_lanes` (gauge) — concurrency actually
    achieved.
  - `dispatch_submitted_total` / `_completed_total` / `_dropped_total` (counters).
  - `graph_ingest_cas_retries_total` (counter) — CAS-conflict retries; **proves** the
    no-contention claim (should stay ~0 with correct keying; a spike means the lane
    key is wrong).
  Together these let an operator read throughput (`entities_updated_total` rate),
  processing vs queue time, achieved concurrency, backpressure, and contention — and
  make the fix measurable in place, not inferred downstream.

## Impact

- **Affected specs:** `graph-ingest` (ADDED: keyed-concurrent ingest with per-entity
  ordering, configurable lanes, bounded backpressure, measurability); `keyed-dispatch`
  (new capability — the ordered-lane primitive contract). The `max_ack_pending` port
  field is plumbing under graph-ingest's bounded-backpressure requirement, not a
  separate capability (JetStreamPort has no existing spec; one field doesn't warrant
  seeding one).
- **Affected code:** new `pkg/dispatch` keyed pool (+ metrics); `processor/graph-ingest/
  component.go` (compose the pool, decode-once, ack-in-lane, `ingest_lanes` config,
  histograms, CAS-retry counter); `component/port_jetstream.go` (`max_ack_pending`
  plumb); `natsclient/stream.go` (no change — `MaxAckPending` already honored).
- **Correctness:** per-entity ordering preserved by key→lane affinity; CAS contention
  eliminated (same key serial); arrival-order merge semantics unchanged; ack still
  at-least-once (moved into the lane, still explicit). Decode-once avoids a double
  parse.
- **Core-lane behavior change → e2e:core required before tag** (default lanes > 1
  changes the sole ENTITY_STATES writer's dispatch; unit+integration don't exercise
  ingest→entity→graph-store→query — see `feedback_e2e_required_for_breaking_changes`).
  Add a throughput/ordering integration test; run `task e2e:core`.
- **Consumers:** semboids' graph dial should clear its ingest-bound classification at
  `boids × Hz` well above ~670/s; confirm on the repro. No wire/API change for other
  sem\* repos.

## Non-goals

- **Server-side merge** (gh#480 option 3) — collapsing read-for-revision + write into
  one round-trip needs a NATS server-side op that doesn't exist natively; deferred.
- **Batch/pipeline KV writes** (gh#480 option 2) — orthogonal, complementary; not
  needed once lanes overlap the RTTs. Deferred.
- **Changing arrival-order merge semantics** (gh#466) — out of scope; the design
  preserves it by keying.
- **Touching the other 25 `ConsumeStreamWithConfig` callers** — the primitive is
  composed by graph-ingest only; other consumers opt in later if they need keyed
  ordering.
