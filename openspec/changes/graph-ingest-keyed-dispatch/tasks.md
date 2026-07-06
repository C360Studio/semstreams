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
      NOTE (round-3): ONE **global** pool shared across all input ports — NOT one pool
      per port (per-port pools split a cross-stream entity into two lanes → race).
- [ ] 3.3 Refactor the consume closure (`component.go:983`): decode/extract the entity
      **once** (reuse the `extractEntityFromMessage` path), then submit
      `ingestWork{entity, msg, stream: meta.Stream, seq: meta.Sequence.Stream}` (from
      `msg.Metadata()`). On decode/extract/metadata failure keep today's behavior:
      `errors++`, `msg.Ack()` (ack-drop), return. Move `ingestEntity` + `msg.Ack()` into
      `processIngest`, run on the POOL ctx (not the 30s `msgCtx` — else `MergeEntity`'s
      `ctx.Err()` aborts every merge, review L-B). **Submit failure MUST Nak** (review B1)
      — never discard the result; don't block the submit on `msgCtx`.
- [ ] 3.4 **Redelivery-safety guard (review B1 BLOCKING, round-2+3 corrected):** an
      in-memory per-lane map keyed by **`(entityID, streamName)`** → last applied stream
      sequence; drop (ack, no apply) a message not newer than the last applied **from that
      same stream**. Keyed per-`(entity, stream)` so it never compares across the
      independent per-stream sequence spaces (structural.json runs 2 input streams). NO
      durable `EntityState` stamp (would commit in-CAS before side effects → skip them on
      crash, and is a cross-repo schema change). Update the map AFTER the post-commit side
      effects (`updateSuffixIndex`/`ensureRelationshipTargetsExist`/`routeForeignEdges`)
      so a mid-apply crash re-drives them. LRU-bound the map (window > AckWait). Increment
      `graph_ingest_redeliveries_dropped_total`.
- [ ] 3.5 **Lifecycle ordering is correctness (review M3):** build the pool BEFORE
      subscriptions start (else first message → nil pool); in `Stop`, drain the pool
      BEFORE the KV store / NATS connection closes.
- [ ] 3.6 Confirm the decode-once refactor doesn't double-decode: `KeyOf` reads the
      already-parsed `entity.ID`; `processIngest` takes the parsed entity.

## 4. Backpressure — MaxAckPending (SHIPPED in part 1)

- [x] 4.1–4.3 `max_ack_pending` port plumbing + `stream.go` `!= 0` fix + round-trip test
      **shipped in part 1 (#488)**. This change only SIZES it as the keyed backpressure
      ceiling (`≈ Lanes × QueueDepth × k` against AckWait, so a full pipeline drains
      before redelivery — defense-in-depth atop the sequence guard).

## 5. graph-ingest metrics (base SHIPPED in part 1 — add only the keyed-pool ones)

- [x] 5.0 `graph_ingest_processing_duration_seconds` + `graph_ingest_ingest_lag_seconds`
      (stream-backlog wait) **shipped in part 1 (#488)**. Do NOT re-add.
- [ ] 5.1 Add ONLY the keyed-pool metrics: `dispatch_queue_wait_seconds` (LANE queue
      wait — distinct from part 1's stream-backlog `ingest_lag`), `dispatch_inflight`,
      `dispatch_queue_depth`; `graph_ingest_cas_retries_total` (counter, on
      `ErrKVRevisionMismatch` retry, `mutations.go:553-576`/`UpdateWithRetry`) —
      documented as **cross-entity contention observability, NOT a keying-correctness
      proof** (review H1); `graph_ingest_redeliveries_dropped_total` (counter, task 3.4).
      Follow the `sync.Once` getter pattern (`component.go:41-61`).

## 6. Tests

- [ ] 6.1 Primitive unit tests (task 1.4, 2.2).
- [ ] 6.2 graph-ingest ordering integration: interleave two entities' updates with
      `ingest_lanes>1`; assert same-entity final state is the newer write and cross-entity
      concurrency occurs (drive through the real consume→pool→ingest wire, not a helper).
- [ ] 6.3 Backward-compat: `ingest_lanes=1` reproduces serial behavior; existing
      graph-ingest integration tests pass unchanged.
- [ ] 6.4 Backpressure: producer faster than ingest → in-flight capped by lane queue +
      max_ack_pending, no unbounded growth.
- [ ] 6.5 **Redelivery reorder, same stream (review B1):** a redelivered older-sequence
      message for an entity that already has a newer write applied is dropped; entity
      keeps the newer state. Drive the real merge path.
- [ ] 6.5b **Two-input-stream cases (review round-2/3, MEDIUM #5) — the shipped
      structural shape:** stand up graph-ingest with TWO input ports
      (`objectstore.stored.entity` + `sensor.processed.entity`, per `structural.json`).
      Assert: (a) a valid newer message for entity E from stream B at a LOW sequence is
      NOT dropped by a prior high-sequence apply from stream A (per-stream guard); (b) the
      same entity E arriving on both streams serializes through one lane (no concurrent
      apply / no lost update). This is the case per-port pools would have broken.
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
- [ ] 7.3 **Core-lane behavior change → e2e before tag** (default lanes>1 changes the sole
      ENTITY_STATES writer; see [[feedback_e2e_required_for_breaking_changes]]). Run
      **`task e2e:structural`** (exercises the TWO-input-stream graph-ingest shape) — NOT
      just `e2e:core`, which is single-stream and would miss the cross-stream case
      (review MEDIUM #5). Confirm green.
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
