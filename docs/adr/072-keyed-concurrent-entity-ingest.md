# ADR-072: Keyed-concurrent entity ingest for graph-ingest (gh#480)

## Status

**Deferred — 2026-07-06** (was Proposed). Split decision: the low-risk, high-value
half — ingest **observability metrics + `max_ack_pending` backpressure plumbing** — ships
first as its own change (`graph-ingest-ingest-metrics`), to make the throughput problem
measurable before changing the concurrency model. This keyed-concurrency ADR is parked
pending (a) that metrics change landing and (b) the round-2 rework below.

**Three code-grounded reviews hardened this before any code.** Round 1 (NEEDS-REWORK) →
per-entity applied-sequence guard (B1), primitive panic recovery (H2), corrected
CAS/backpressure claims (H1/M1). Round 2 (NEEDS-ANOTHER-REWORK) found the sequence guard
**unsound across multiple input streams**: graph-ingest runs on 2 streams in shipped
configs (`structural.json`, `e2e-structural.json`: `objectstore.stored.entity` +
`sensor.processed.entity`) with independent `Sequence.Stream` counters, so a per-entity
guard silences the lower-sequence stream (silent data loss). Round 3 (Codex, NEEDS-REWORK)
**killed the round-2 "one pool per input port" fix**: per-port pools restore per-stream
sequence spaces but **no longer serialize the same entity across ports** — an entity
arriving on both streams lands in two lanes and races, violating the core invariant
(same entity → one lane, required because `MergeTriples` is arrival-order full-set-
replace, `graph/helpers.go`).

**Corrected resolution (fold into the mechanics below when this resumes):**
- **One GLOBAL keyed pool** (`hash(entityID) → lane`), so the same entity serializes
  through one lane across ALL input streams (cross-port race gone; cross-stream
  same-entity is arrival-order LWW exactly as serial mode is today — no regression, no
  silent drop).
- **Redelivery guard keyed by `(entityID, streamName)`** (from `msg.Metadata().Stream` +
  `.Sequence.Stream`) in an **in-memory per-lane map** updated *after* side effects — so
  it is per-stream (never compares sequences across streams → round-2 unsoundness gone),
  needs no durable `EntityState` stamp (no cross-repo schema change → round-2 M-C gone),
  and re-drives post-commit side effects on crash (routeForeignEdges etc.). Bound the map
  (LRU window > AckWait).
- Also fold round-2 MEDIUMs (M-A foreign-edge scope, M-D redelivery amplification,
  L-A/L-B ctx+metadata handling) and **rebase on part 1**: the processing/ingest-lag
  histograms + `max_ack_pending` plumbing already SHIPPED (#488, on main); this change
  adds ONLY the keyed-pool metrics (queue-wait vs lane, in-flight, depth), the CAS-retry
  counter, and the redelivery-drop counter.
- **Test plan MUST include a two-port (structural-shape) case**: same-entity cross-port
  serialization + a lower-sequence redelivery from one stream not dropping a valid
  message from the other. `task e2e:core` is single-stream — use the structural tier or a
  targeted two-input integration test.

Records the concurrency model for graph-ingest's entity ingest and the placement of the
primitive it composes. Mechanics (field names, lane routing, backpressure sizing) live in
the `graph-ingest` + `keyed-dispatch` capability specs via the
`graph-ingest-keyed-dispatch` openspec change, not here.

A code-grounded adversarial review (framework-ADR discipline,
`feedback_adversarial_review_framework_adr`) was run on the first draft and returned
**NEEDS-REWORK**: the keyed-lane decision and `pkg/dispatch` placement were confirmed
sound, but three graph-ingest correctness claims were wrong — a **BLOCKING** redelivery-
reorder hole (B1), a lost lane-panic recovery (H2), and a false "no concurrent same-key
CAS / retries prove keying" claim (H1), plus a factual "unlimited unacked" error (M1).
All are folded into the Decision/Consequences below (B1 → per-entity sequence guard;
H2 → panic recovery in the primitive; H1/M1 → corrected claims). Pending user sign-off
before **Accepted**; the reworked correctness model should get one more review pass.

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
- **CAS contention (same entity):** one entity ID is only ever on one lane at a time,
  so there are **no concurrent CAS writes to that entity's OWN key** — no self-inflicted
  revision-mismatch retries. *Not* an absolute "no shared-key writes" guarantee: ingesting
  entity A also writes OTHER keys — relationship-target stubs
  (`ensureRelationshipTargetsExist`), foreign edges, and shared hierarchy containers — so
  cross-lane contention on those keys is real (entity-birth, hierarchy, relationship-dense
  workloads). Those are made **safe by atomic create-if-absent + CAS retry, not by lane
  affinity** (no lost update). The `cas_retries` metric is therefore a contention
  *observability* signal, not a proof of correct keying (adversarial review H1).

**Primitive placement: a new generic keyed-ordered pool in `pkg/dispatch`**, composed
by graph-ingest — *not* hand-rolled in graph-ingest, and *not* a new mode on
`natsclient.ConsumeStreamWithConfig`.

- **Why a framework primitive (not local):** keyed-ordered dispatch is a reusable
  substrate shape (any at-least-once consumer needing per-key ordered concurrency).
  ADR-048's lesson — name the primitive rather than let consumers reinvent it. It sits
  beside `BoundedDispatcher` (unordered, KV-completion-aware); the two are distinct
  primitives, not one with a flag (keyed ordering needs N separate lane queues, a
  different structure from Pool's one shared channel).
- **Why not natsclient:** `ConsumeStreamWithConfig` has **~30 call sites across ~22
  files**. A
  keyed mode there either forces a double-decode (the partition key needs the entity ID,
  which requires the same parse the handler does) or a handler-signature change touching
  all 26. Composing the pool inside graph-ingest's callback instead keeps natsclient and
  its callers untouched and lets graph-ingest **decode once** (parse in the callback,
  reuse the parsed entity as both key source and lane payload).

**Ack moves into the lane.** graph-ingest's callback decodes once and submits
`{entity, msg}`; the lane runs `ingestEntity` then `msg.Ack()`. Still explicit,
still at-least-once; `AckExplicit` tolerates out-of-order acks across lanes. A
decode/extract failure still acks-and-counts (today's poison-drop behavior).

Two consequences of moving ack into the lane, both surfaced by the adversarial review
and folded here as hard requirements (not left to sizing):

- **Redelivery safety (B1, was BLOCKING).** A message can now sit in a bounded lane queue
  longer than `AckWait` (30s) under the sustained overload this targets → the server
  redelivers → the stale copy re-hashes to its lane and applies *after* a newer message,
  and the arrival-order full-set-replace merge overwrites the newer write. Serial ingest
  never hit this (ack was inline, ~ms). Fix: a **per-entity applied-sequence guard** —
  each message carries its JetStream stream sequence; the merge drops any message whose
  sequence is not newer than the last applied to that entity. Correctness under
  redelivery is thus decoupled from `MaxAckPending`/`AckWait` sizing (which stays as
  defense-in-depth). A submit that fails MUST Nak, never silently drop.
- **Panic recovery (H2).** `ingestEntity` now runs in a lane goroutine OUTSIDE
  `safeHandleMessage`'s `recover()`. An unrecovered panic there would crash the sole
  ENTITY_STATES writer — strictly worse than today's one-message Nak. The **primitive
  MUST recover panics in `Process`**, dispose the message (Nak), and keep the lane
  goroutine alive so keys hashing to it are not stranded.

**Bounded by default.** Lanes are configurable via `ingest_lanes`, **default > 1**
(concurrent by default; `1` opts back into serial). In-memory work is capped by the
**bounded per-lane queues + `SubmitBlocking`** (that is what prevents unbounded memory).
Separately, `max_ack_pending` today has **no config path at all** (`JetStreamPort` lacks
the field), so graph-ingest runs at the nats.go default of **1000** delivered-unacked
(M1: *not* unlimited — `-1` is unlimited). This change plumbs `max_ack_pending` through
the port config to **raise/tune that 1000 ceiling so N lanes stay fed**;
`consumer_pending_messages` (stream backlog / NumPending) is drained by lane throughput,
not bounded by `MaxAckPending`.

**Measurability is part of the decision, not a follow-up (gh#480 observability gap).**
The queue-wait-vs-processing split and `max_ack_pending` already SHIPPED in part 1 (#488,
`graph_ingest_processing_duration_seconds` + `graph_ingest_ingest_lag_seconds`). This
change adds the keyed-pool metrics — lane queue-wait (distinct from stream-backlog lag),
achieved concurrency (in-flight), queue depth — plus a **CAS-retry counter** (a
cross-entity *contention-observability* signal, NOT a proof of keying — see H1: legit
stub/foreign-edge/hierarchy writes retry) and a **redelivery-drop counter** (guard hits).

## Consequences

- A new `pkg/dispatch` keyed pool joins the substrate beside `BoundedDispatcher`
  (unordered) — "same key serial, different key parallel," with built-in metrics.
- graph-ingest's default dispatch changes from serial to N-lane concurrent — a
  **core-lane behavior change to the sole ENTITY_STATES writer**. Unit + integration
  don't exercise ingest→entity→graph-store→query, so `task e2e:core` is required before
  tag (`feedback_e2e_required_for_breaking_changes`).
- `max_ack_pending` becomes a real, operator-settable port knob for every JetStream
  input port (additive; absent = today's behavior).
- Arrival-order merge semantics (gh#466) are **unchanged** — preserved by keying (+ the
  applied-sequence guard for redeliveries), not by serialization.
- **Lifecycle ordering is normative (M3):** the pool is built before subscriptions start
  (else the first message submits to a nil pool) and drained on `Stop` before the KV
  store / NATS connection closes (else in-flight merges fail).
- **Head-of-line blocking (M2):** `SubmitBlocking` blocks the single dispatch goroutine
  when one lane is full, so a hot key can stall dispatch to all lanes — "~N×" is
  optimistic under key skew. A known limitation, covered by a skewed-key throughput test.
- **Default `ingest_lanes > 1` is safe to ship** only with B1 (sequence guard) and H2
  (panic recovery) in place — the two the review said must close first. It does not ship
  until both land + `e2e:core` is green.
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
