# Tasks — keyed-concurrent entity ingest (gh#480, ADR-072)

> Scoping change (Proposed). Tasks unchecked; implementation follows scope approval
> (`/opsx:apply` on `feat/graph-ingest-concurrent-ingest`).

## 1. Keyed-ordered dispatch primitive (`pkg/dispatch`)

- [ ] 1.1 Add `KeyedPool[W]` (sibling to `BoundedDispatcher`): `KeyedConfig[W]{Lanes int,
      QueueDepth int, KeyOf func(W) string, Process func(ctx, W) error, Name string}` +
      `Deps{MetricsRegistry, Logger}`. `New` validates (`Lanes<1`, missing `KeyOf`/
      `Process` → error).
- [ ] 1.2 Lane routing: `lane = fnv1a(KeyOf(w)) % Lanes`; N bounded lane channels; one
      goroutine per lane draining in order and calling `Process`. `SubmitBlocking(ctx, w)`
      (blocks on full lane) + non-blocking `Submit` (returns `ErrLaneFull`).
- [ ] 1.3 `Stop(ctx)`: stop accepting, drain lanes, return when idle or ctx done.
- [ ] 1.4 **Panic recovery (review H2):** `Process` runs in a lane goroutine; the pool
      MUST `recover()` a panic in `Process`, invoke a caller disposition hook (so the
      composer can Nak the message), and keep the lane goroutine alive for later items.
      A panicking item MUST NOT crash the process or wedge its lane.
- [ ] 1.5 Determinism/ordering unit tests: same key → strict submit order on one lane
      (assert via a recording Process); distinct keys → observed concurrency (barrier).
      Backpressure test (full lane blocks SubmitBlocking). Graceful drain on Stop.
      Panic-recovery test (panicking item → disposition invoked, lane survives, later
      same-lane items still processed).

## 2. Primitive metrics

- [ ] 2.1 On `KeyedPool`, register (label `pool=Name`): `dispatch_queue_wait_seconds`
      (hist, stamp submit time on the work item, observe at process start),
      `dispatch_processing_duration_seconds` (hist), `dispatch_queue_depth` (gauge),
      `dispatch_inflight` (gauge), `dispatch_submitted_total`/`_completed_total`/
      `_dropped_total` (counters). Follow `pkg/worker/pool.go` metric style +
      `MetricsRegistry.RegisterHistogramVec`; nil-registry → default registerer.
- [ ] 2.2 Unit-assert the metrics move (submit N, drain, assert submitted==completed,
      inflight returns to 0, queue_wait/processing observed).

## 3. graph-ingest integration

- [ ] 3.1 Add `IngestLanes int json:"ingest_lanes" schema:"type:int,default:8,
      category:advanced"` to `Config` (`component.go:241`); `ApplyDefaults` → 8 when 0;
      `Validate` clamps `<1 → 1` and caps an upper bound; `DefaultConfig` sets 8.
- [ ] 3.2 In `Start`, build one `KeyedPool[ingestWork]` (`KeyOf = w.entity.ID`,
      `Process = c.processIngest`). Store on `Component`; `Stop` drains it before the KV
      store closes.
- [ ] 3.3 Refactor the consume closure (`component.go:983`): decode/extract the entity
      **once** (reuse the `extractEntityFromMessage` path), then submit
      `ingestWork{entity, msg, seq: msg.Metadata().Sequence.Stream}`. On decode/extract
      failure keep today's behavior: `errors++`, `msg.Ack()` (ack-drop), return. Move
      `ingestEntity` + `msg.Ack()` into `processIngest` (ack in the lane). **Submit failure
      MUST Nak** (review B1) — never discard the result; do NOT block the submit on the
      30s `msgCtx` (use the component ctx) so a >AckWait block can't silently drop.
- [ ] 3.4 **Redelivery-safety guard (review B1, BLOCKING):** carry each message's JetStream
      stream sequence; in the merge, drop (ack, no apply) a message whose sequence is not
      newer than the last applied to that entity. Prefer a durable stamp on the entity
      (memory/restart-safe; the CAS `Get` already reads it) over a per-lane in-memory map.
      Increment `graph_ingest_redeliveries_dropped_total`.
- [ ] 3.5 **Lifecycle ordering is correctness (review M3):** build the pool BEFORE
      subscriptions start (else first message → nil pool); in `Stop`, drain the pool
      BEFORE the KV store / NATS connection closes.
- [ ] 3.6 Confirm the decode-once refactor doesn't double-decode: `KeyOf` reads the
      already-parsed `entity.ID`; `processIngest` takes the parsed entity.

## 4. Backpressure — MaxAckPending port plumbing

- [ ] 4.1 Add `MaxAckPending int json:"max_ack_pending,omitempty"` to `JetStreamPort`
      (`component/port_jetstream.go:10`) and to `component.ConsumerConfig`
      (`port_jetstream.go:77`); copy it in `applyJetStreamConsumerConfig`
      (`port_jetstream.go:123`).
- [ ] 4.2 graph-ingest maps `ConsumerConfig.MaxAckPending` →
      `StreamConsumerConfig.MaxAckPending` (`component.go:972` mapping block); already
      honored at `natsclient/stream.go:320`. Default sized `≈ Lanes × QueueDepth × k`
      against AckWait so a full pipeline drains before redelivery.
- [ ] 4.3 Round-trip test: a port config with `max_ack_pending` reaches
      `StreamConsumerConfig` (guard the plumbing gap the issue flagged).

## 5. graph-ingest metrics

- [ ] 5.1 `graph_ingest_processing_duration_seconds` (hist) around `ingestEntity`
      (merge+CAS); `graph_ingest_cas_retries_total` (counter, on `ErrKVRevisionMismatch`
      retry, `mutations.go:553-576`/`UpdateWithRetry`) — documented as **cross-entity
      contention observability, NOT a keying-correctness proof** (review H1);
      `graph_ingest_redeliveries_dropped_total` (counter, task 3.4). Wire the pool's
      metrics through `deps.MetricsRegistry`. Follow the `sync.Once` getter pattern
      (`component.go:41-61`).
- [ ] 5.2 Fix the stale `natsclient/stream.go:46` comment ("0 means unlimited" → "0 =
      server default 1000; -1 = unlimited") (review M1).

## 6. Tests

- [ ] 6.1 Primitive unit tests (task 1.4, 2.2).
- [ ] 6.2 graph-ingest ordering integration: interleave two entities' updates with
      `ingest_lanes>1`; assert same-entity final state is the newer write and cross-entity
      concurrency occurs (drive through the real consume→pool→ingest wire, not a helper).
- [ ] 6.3 Backward-compat: `ingest_lanes=1` reproduces serial behavior; existing
      graph-ingest integration tests pass unchanged.
- [ ] 6.4 Backpressure: producer faster than ingest → in-flight capped by lane queue +
      max_ack_pending, no unbounded growth.
- [ ] 6.5 **Redelivery reorder (review B1):** simulate a redelivered older-sequence
      message for an entity that already has a newer write applied; assert the guard
      drops it and the entity keeps the newer state. Drive the real merge path.
- [ ] 6.6 **Panic recovery (review H2):** a `Process` panic Naks the message and leaves
      the lane processing subsequent items; the component does not crash.
- [ ] 6.7 Throughput smoke (integration, in-process NATS): N-lane ingest of a mixed-key
      corpus completes materially faster than lanes=1 (directional, not a hard SLA — CI
      hosts vary; log the achieved rate + inflight). Include a **skewed-key** corpus
      (one hot key) to exercise head-of-line blocking (review M2) — assert no deadlock,
      correctness holds; the speedup is expected to be lower than uniform.

## 7. Spec + gates + close

- [ ] 7.1 `openspec validate graph-ingest-keyed-dispatch --strict`.
- [ ] 7.2 Gates: `go test -race` (pkg/dispatch, graph-ingest, component), `task lint`,
      schema no-drift (`ingest_lanes`/`max_ack_pending` are schema'd — regenerate + diff),
      `go vet -tags=integration`.
- [ ] 7.3 **Core-lane behavior change → `task e2e:core`** before tag (default lanes>1
      changes the sole ENTITY_STATES writer; see
      [[feedback_e2e_required_for_breaking_changes]]). Confirm green.
- [ ] 7.4 semstreams-reviewer pre-merge, specifically re-checking the adversarial-review
      findings closed: per-entity ordering under keying; **the applied-sequence guard
      actually prevents redelivery reorder (B1)**; **panic in a lane is recovered + Nak'd,
      not a crash (H2)**; lane lifecycle ordering (build-before-subscribe, drain-before-
      close) (M3); ack-in-lane at-least-once + poison-ack-drop + submit-failure-Nak;
      bounded in-flight; the `cas_retries` metric is documented as contention observability
      not a keying proof (H1); metrics separate queue wait from processing; no regression
      to the other ~30 `ConsumeStreamWithConfig` call sites.
- [ ] 7.5 Archive → promote `graph-ingest` (ADD) + `keyed-dispatch` (new) into
      `openspec/specs/`. PR; CI; merge; tag (e2e:core gate first).
- [ ] 7.6 Confirm on gh#480 + re-run the semboids repro (`task sweep HZ=30 BOIDS=200`):
      ingest-bound classification clears; record achieved entity/s + the new histograms.

## 8. ADR

- [ ] 8.1 ADR-072 (Accepted after adversarial review): the keyed-ordered concurrency
      model for the sole ENTITY_STATES writer, primitive placement in `pkg/dispatch`,
      per-entity ordering + CAS-contention elimination, default-lanes>1 posture.
