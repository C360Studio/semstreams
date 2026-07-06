# ADR-072: Keyed-concurrent entity ingest for graph-ingest (gh#480)

## Status

**Proposed — 2026-07-06.** Records the concurrency model for graph-ingest's entity
ingest and the placement of the primitive it composes. Mechanics (field names, metric
names, lane routing, backpressure sizing) live in the `graph-ingest` + `keyed-dispatch`
capability specs via the `graph-ingest-keyed-dispatch` openspec change, not here.

Pending a code-grounded adversarial review (framework-ADR discipline,
`feedback_adversarial_review_framework_adr`) and user sign-off before **Accepted**.

Scopes gh#480.

## Context

`graph-ingest` entity ingest tops out at **~670 msg/s** on a 12-core node that sits
**~92% idle**. The ceiling is structural, not CPU: `ConsumeStreamWithConfig` →
`consumer.Consume(cb)` dispatches **serially**, and graph-ingest's callback runs the
full `MergeEntity` — a `Get` for the live revision + a revision-checked CAS `Put`,
**two sequential KV round-trips** — inline before ack (`component.go:983`). Throughput
is bound to `1 / (2 × KV RTT)`; the merge/CAS compute is ~1% of the idle machine
(gh#480 30s profile). Producers above ~670 entity/s back up unboundedly
(`consumer_pending_messages` 0→87k in the profile); the publish side (gh#470) sustains
full rate — the substrate is the wall.

Two facts constrain the fix:

1. **graph-ingest is the sole writer of ENTITY_STATES**, and `graph.MergeTriples`
   (`graph/helpers.go:108`) is **arrival-order** newer-wins — the incoming message wins
   a `(subject,predicate)` conflict *regardless of timestamp* (gh#466). So **naive**
   concurrent dispatch is a correctness bug: two updates to one entity processed out of
   order let the *older* one win the last CAS. Same-entity ordering is real (semboids
   republishes each boid at 30Hz).

2. **No keyed/ordered concurrency primitive exists.** `pkg/worker.Pool` and
   `BoundedDispatcher` (ADR-048) are both a single shared channel + N *unordered*
   workers — no key affinity, no ordering. Using either directly would produce exactly
   the out-of-order corruption in (1), plus CAS-conflict retry storms from concurrent
   writes to the same key.

## Decision

**Keyed-ordered lanes.** Partition ingest by **entity ID**: `lane = hash(entityID) % N`;
each lane is a serial goroutine, lanes run in parallel. Same entity → same lane →
serial in arrival order; different entities → parallel.

This single choice resolves all three axes at once:

- **Throughput:** N lanes overlap the KV round-trips → ~N× until a KV-server or core
  limit, on a box that was 11/12 idle.
- **Ordering:** key→lane affinity preserves per-entity arrival order — the property the
  arrival-order merge *requires*. (Naive concurrency does not have this.)
- **CAS contention:** one entity ID is only ever on one lane at a time, so there are
  **no concurrent CAS writes to the same key** — contention drops to ~0, strictly
  *better* than serial-plus-naive-concurrency. Cross-lane keys are distinct KV keys.

**Primitive placement: a new generic keyed-ordered pool in `pkg/dispatch`**, composed
by graph-ingest — *not* hand-rolled in graph-ingest, and *not* a new mode on
`natsclient.ConsumeStreamWithConfig`.

- **Why a framework primitive (not local):** keyed-ordered dispatch is a reusable
  substrate shape (any at-least-once consumer needing per-key ordered concurrency).
  ADR-048's lesson — name the primitive rather than let consumers reinvent it. It sits
  beside `BoundedDispatcher` (unordered, KV-completion-aware); the two are distinct
  primitives, not one with a flag (keyed ordering needs N separate lane queues, a
  different structure from Pool's one shared channel).
- **Why not natsclient:** `ConsumeStreamWithConfig` has **26 production call sites**. A
  keyed mode there either forces a double-decode (the partition key needs the entity ID,
  which requires the same parse the handler does) or a handler-signature change touching
  all 26. Composing the pool inside graph-ingest's callback instead keeps natsclient and
  its callers untouched and lets graph-ingest **decode once** (parse in the callback,
  reuse the parsed entity as both key source and lane payload).

**Ack moves into the lane.** graph-ingest's callback decodes once and submits
`{entity, msg}`; the lane runs `ingestEntity` then `msg.Ack()`. Still explicit,
still at-least-once; `AckExplicit` tolerates out-of-order acks across lanes. A
decode/extract failure still acks-and-counts (today's poison-drop behavior); only a
panic Naks for redelivery (`safeHandleMessage`, unchanged).

**Bounded by default.** Lanes are configurable via `ingest_lanes`, **default > 1**
(concurrent by default; `1` opts back into serial). In-flight work is capped by bounded
per-lane queues **and** a `max_ack_pending` consumer knob — which today has **no config
path at all** (`JetStreamPort` lacks the field), so graph-ingest currently runs with
NATS-default unlimited unacked. This change plumbs `max_ack_pending` through the port
config so concurrency cannot grow unbounded memory.

**Measurability is part of the decision, not a follow-up (gh#480 observability gap).**
The primitive and graph-ingest expose Prometheus metrics that separate **queue wait**
(`dispatch_queue_wait_seconds`) from **processing time**
(`graph_ingest_processing_duration_seconds`), plus queue depth, achieved concurrency,
and a **CAS-retry counter** that *proves* the no-contention claim (stays ~0 under
correct keying). The prior state could only infer end-to-end latency downstream.

## Consequences

- A new `pkg/dispatch` keyed pool joins the substrate beside `BoundedDispatcher`
  (unordered) — "same key serial, different key parallel," with built-in metrics.
- graph-ingest's default dispatch changes from serial to N-lane concurrent — a
  **core-lane behavior change to the sole ENTITY_STATES writer**. Unit + integration
  don't exercise ingest→entity→graph-store→query, so `task e2e:core` is required before
  tag (`feedback_e2e_required_for_breaking_changes`).
- `max_ack_pending` becomes a real, operator-settable port knob for every JetStream
  input port (additive; absent = today's behavior).
- Arrival-order merge semantics (gh#466) are **unchanged** — preserved by keying, not
  by serialization.
- **Considered and rejected:** (a) naive unkeyed concurrency — corrupts same-entity
  order + retry storms; (b) keyed mode inside `natsclient.ConsumeStreamWithConfig` —
  26-caller blast radius + double-decode; (c) graph-ingest-local ad-hoc lanes —
  reinvents a reusable primitive (ADR-048 anti-pattern). **Deferred (gh#480 options
  2/3):** batch/pipeline KV writes and a server-side merge op — complementary, not
  needed once lanes overlap the RTTs; the server-side op has no native NATS primitive.
- Product half (semboids re-running its graph dial to confirm the ingest-bound
  classification clears) lands once the fix ships.

## Open questions for implementation (resolved in the specs, not here)

- Default lane count (start 8), per-lane queue depth, and the `max_ack_pending`
  multiplier sized against `AckWait` — tuned against the semboids repro using the new
  inflight/queue-wait metrics.
- Aggregate vs per-lane queue-depth metric labelling (start aggregate).
