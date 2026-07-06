# Tasks — keyed-concurrent entity ingest (gh#480, ADR-072)

> Scoping change (Proposed). Tasks unchecked; implementation follows scope approval
> (`/opsx:apply` on `feat/graph-ingest-concurrent-ingest`).

## 1. Keyed-ordered dispatch primitive (`pkg/dispatch`)

- [x] 1.1 Add `KeyedPool[W]` (sibling to `BoundedDispatcher`): `KeyedConfig[W]{Lanes int,
      QueueDepth int, KeyOf func(W) string, Process func(ctx, lane int, W) error,
      OnPanic func(W, any), Name string}` + `KeyedDeps{MetricsRegistry, Logger}`.
      `NewKeyedPool` validates (`Lanes<1`, `QueueDepth<1`, missing `KeyOf`/`Process` →
      `ErrInvalidConfig`).
- [x] 1.2 Lane routing: `lane = fnv1a(KeyOf(w)) % Lanes`; N bounded lane channels; one
      goroutine per lane draining in order and calling `Process`. **Pass the assigned lane
      index into `Process`** (in its signature) so a composer can shard per-lane state by
      the pool's OWN routing — lock-free, since at most one goroutine runs a lane (review
      shard-safety; graph-ingest's guard tier-1 depends on it). `SubmitBlocking(ctx, w)`
      (blocks on full lane) + non-blocking `Submit` (returns `ErrLaneFull`).
- [x] 1.3 `Stop(ctx)`: stop accepting, drain lanes, return when idle or ctx done.
- [x] 1.4 **Panic recovery (review H2):** `Process` runs in a lane goroutine; the pool
      MUST `recover()` a panic in `Process`, invoke a caller disposition hook (so the
      composer can Nak the message), and keep the lane goroutine alive for later items.
      A panicking item MUST NOT crash the process or wedge its lane.
- [x] 1.5 Determinism/ordering unit tests: same key → strict submit order on one lane
      (assert via a recording Process); distinct keys → observed concurrency (barrier).
      Backpressure test (full lane blocks SubmitBlocking). Graceful drain on Stop.
      Panic-recovery test (panicking item → disposition invoked, lane survives, later
      same-lane items still processed).

## 2. Primitive metrics

- [x] 2.1 On `KeyedPool`, register (label `pool=Name`): `dispatch_queue_wait_seconds`
      (hist, stamp submit time on the work item, observe at process start),
      `dispatch_processing_duration_seconds` (hist), `dispatch_queue_depth` (gauge),
      `dispatch_inflight` (gauge), `dispatch_submitted_total`/`_completed_total`/
      `_dropped_total` (counters). Follow `pkg/worker/pool.go` metric style; per-pool
      `pool=Name` const-label via `RegisterHistogram`/`RegisterGauge`/`RegisterCounter`;
      nil-registry → metrics created but unregistered (observing is a harmless no-op).
- [x] 2.2 Unit-assert the metrics move (submit N, drain, assert submitted==completed,
      inflight returns to 0, queue_wait/processing observed).

## 3. graph-ingest integration

- [x] 3.1 Add `IngestLanes int json:"ingest_lanes" schema:"type:int,default:8,
      category:advanced"` to `Config` (`component.go:294`); `ApplyDefaults` → 8 when 0;
      `Validate` clamps `<1 → 1` and caps an upper bound; `DefaultConfig` sets 8.
- [x] 3.2 In `Start`, build one `KeyedPool[ingestWork]` (`KeyOf = w.entity.ID`,
      `Process = c.processIngest`). Store on `Component`; `Stop` drains it before the KV
      store closes.
      NOTE (round-3): ONE **global** pool shared across all input ports — NOT one pool
      per port (per-port pools split a cross-stream entity into two lanes → race).
- [x] 3.3 Refactor the consume closure (`component.go:1041`): decode/extract the entity
      **once** (reuse the `extractEntityFromMessage` path), then submit
      `ingestWork{entity, msg, stream: meta.Stream, seq: meta.Sequence.Stream}` (from
      `msg.Metadata()`). On decode/extract/metadata failure keep today's behavior:
      `errors++`, `msg.Ack()` (ack-drop), return. Move `ingestEntity` + `msg.Ack()` into
      `processIngest`, run on the POOL ctx (not the 30s `msgCtx` — else `MergeEntity`'s
      `ctx.Err()` aborts every merge, review L-B). **Submit failure MUST Nak** (review B1)
      — never discard the result; don't block the submit on `msgCtx`.
- [x] 3.4 **Redelivery-safety guard (review B1 BLOCKING; round-2+3 keying; round-4 B2/B3
      durability):** a **two-tier** applied-sequence guard keyed by **`(entityID,
      streamName)`** → last-applied stream sequence; drop (ack, no apply) a message not
      newer than the last applied **from that same stream**. Keyed per-`(entity, stream)`
      so it never compares across the independent per-stream sequence spaces
      (structural.json runs 2 input streams).
      - **Tier 1 (in-memory):** per-lane map sharded by the pool's lane index (task 1.2/3.2)
        → lock-free. Fast path, zero extra KV op on an in-process redelivery.
      - **Tier 2 (durable):** on an in-memory MISS (cold / cache-evicted / post-restart),
        read the durable `(entityID, streamName) → seq` stamp from graph-ingest's OWN KV
        bucket (task 3.7). This is what closes B2 (restart) and B3 (high-cardinality
        eviction) — an in-memory-only guard re-opens the overwrite there.
      - Update BOTH tiers AFTER the post-commit side effects
        (`updateSuffixIndex`/`ensureRelationshipTargetsExist`/`routeForeignEdges`) and
        BEFORE ack, so a mid-apply crash re-drives them. **Order: durable write FIRST, then
        tier-1, then ack** — on a durable-write failure, Nak and leave tier-1 un-updated so
        the two tiers never diverge (a tier-1 update ahead of a failed durable write would
        let a post-restart older redelivery slip past an empty/stale durable stamp).
        **Per-message** durable write (a batched flush loses un-flushed stamps on crash →
        re-opens B2); never ack past an unpersisted stamp.
      - NO stamp inside the entity's own `EntityState` record (would commit in-CAS before
        side effects → skip them; cross-repo schema change — round-2 M-B/M-C). The durable
        stamp is a bare `uint64` in graph-ingest's own bucket.
      - In-memory LRU size is a cache knob only (NOT correctness — the durable tier is the
        backstop, so correctness is independent of eviction policy + `MaxDeliver`/`AckWait`
        sizing). Increment `graph_ingest_redeliveries_dropped_total` on a guard drop.
- [x] 3.7 **Durable guard bucket:** provision a graph-ingest-owned KV bucket at `Start`
      (`(entityID, streamName)` composite key → last-applied `uint64` stream seq) for the
      guard's durable tier (task 3.4). Owned by graph-ingest (operational state,
      bucket-ownership rubric); no cross-repo schema change. No-TTL is correct for
      `MaxDeliver=0`; any TTL must be ≥ `AckWait × MaxDeliver` and then requires finite
      `MaxDeliver` — decide against measured bucket growth, default no-TTL.
- [x] 3.5 **Lifecycle ordering is correctness (review M3):** build the pool BEFORE
      subscriptions start (else first message → nil pool). In `Stop`: (1) **cancel the
      submit ctx FIRST** so a consume callback parked in `SubmitBlocking` unblocks + Naks
      (else the synchronous consumer callback blocks teardown until timeout); (2) drain the
      pool; (3) THEN close the KV store / NATS connection.
- [x] 3.6 Confirm the decode-once refactor doesn't double-decode: `KeyOf` reads the
      already-parsed `entity.ID`; `processIngest` takes the parsed entity.

## 4. Backpressure — MaxAckPending (SHIPPED in part 1)

- [x] 4.1–4.3 `max_ack_pending` port plumbing + `stream.go` `!= 0` fix + round-trip test
      **shipped in part 1 (#488)**. This change only SIZES it as the keyed backpressure
      ceiling (`≈ Lanes × QueueDepth × k` against AckWait, so a full pipeline drains
      before redelivery — defense-in-depth atop the sequence guard).

## 5. graph-ingest metrics (base SHIPPED in part 1 — add only the keyed-pool ones)

- [x] 5.0 `graph_ingest_processing_duration_seconds` + `graph_ingest_ingest_lag_seconds`
      (stream-backlog wait) **shipped in part 1 (#488)**. Do NOT re-add.
- [x] 5.1 Add ONLY the keyed-pool metrics: `dispatch_queue_wait_seconds` (LANE queue
      wait — distinct from part 1's stream-backlog `ingest_lag`), `dispatch_inflight`,
      `dispatch_queue_depth`; `graph_ingest_cas_retries_total` (counter, on
      `ErrKVRevisionMismatch` retry, `mutations.go:553-576`/`UpdateWithRetry`) —
      documented as **cross-entity contention observability, NOT a keying-correctness
      proof** (review H1); `graph_ingest_redeliveries_dropped_total` (counter, task 3.4).
      Follow the `sync.Once` getter pattern (`component.go:41-67`). NOTE: a guard drop is a
      pool-`completed` (the lane picks up + acks), NOT a pool-`dropped_total`; document that
      `dispatch_dropped_total` and `redeliveries_dropped_total` count disjoint events so
      operators don't sum them.
      DONE: the `dispatch_*` pool metrics flow automatically (the pool is built with
      `KeyedDeps.MetricsRegistry`), `graph_ingest_redeliveries_dropped_total` is wired +
      documented, and `graph_ingest_cas_retries_total` is now wired WITHOUT a natsclient
      change: MergeEntity's CAS callback counts its own re-invocations (`casAttempt > 1` =
      the prior revision-checked Put lost the CAS and retried). Documented as cross-entity
      contention observability, not a keying proof (H1). Slight over-count on a rare Get
      network-blip retry is acceptable for an observability signal.
      FOLLOW-UP (review MINOR): the `dispatch_*` pool metrics are fresh objects registered
      under a fixed `pool="graph_ingest"` key, so after a component Stop+Start the new
      pool's objects hit the registry's already-registered early-return and silently stop
      exporting. No correctness/panic impact. Fix later by reusing metric objects across
      pool instances (package singletons like the `getX` getters) or unregister-on-Stop.

## 6. Tests

> COVERAGE: the guard's CORRECTNESS CORE + the assembled wire are tested.
> - `keyed_pool_test.go` — primitive (ordering, distinct-lane concurrency, backpressure,
>   drain, panic recovery, metrics). (6.1 ✓)
> - `keyed_ingest_test.go` — laneGuard bounded eviction, guardKey, in-memory-tier
>   staleness + per-stream independence.
> - `keyed_ingest_integration_test.go` (real NATS) — durable-tier restart/eviction
>   survival (B2/B3), durable per-stream independence, the ASSEMBLED wire happy path
>   (publish→consumer→pool→processIngest→ENTITY_STATES), and same-entity ordering through
>   the wire (25 rapid updates converge on the last write — no reorder).
> - Full graph-ingest integration suite passes UNCHANGED under the refactor (6.3 ✓).
> - `e2e:structural` GREEN — the shipped TWO-input-stream shape (objectstore + sensor),
>   `validation_errors:0`, `data_loss_percent:0` (covers 6.5b's two-stream shape end to end).
> DELIBERATELY NOT built as bespoke tests: forcing a REAL AckWait redelivery (6.5) or a
> hard throughput SLA (6.7) deterministically in-process is flaky/low-value — the guard's
> stale-drop is proven at the helper level (in-mem + durable seq-compare) and the two-stream
> shape by e2e:structural. 6.6 (component doesn't crash on a Process panic) is covered by
> the primitive's panic-recovery test + graph-ingest's `OnPanic`→Nak wiring.

- [x] 6.1 Primitive unit tests (task 1.4, 2.2).
- [x] 6.2 graph-ingest ordering integration: many rapid updates for one entity via
      `ingest_lanes>1` converge on the LAST write (same-entity serialization); the wire
      happy path also proves cross-entity ingestion. Driven through the real
      consume→pool→ingest wire (`TestIntegration_KeyedIngest_SameEntityUpdatesStayOrdered`
      + `_PublishedEntityIngestsThroughPool`), not a helper.
- [x] 6.3 Backward-compat: full graph-ingest integration suite passes unchanged (72s green);
      `ingest_lanes=1` is config-tested (`TestConfig_IngestLanes`) and is the trivial N=1
      case of the same lane code.
- [~] 6.4 Backpressure: `SubmitBlocking` + bounded lane queues cap in-flight; unit-tested at
      the primitive level (`TestKeyedPool_Backpressure`). Full producer-faster-than-ingest
      growth-bounding is exercised implicitly by e2e:structural; no bespoke growth test.
- [~] 6.5 Redelivery reorder (B1): stale-drop proven at the guard level (in-mem + durable
      seq-compare unit/integration tests). Forcing a real AckWait redelivery in-process is
      flaky; not built as a bespoke test.
- [~] 6.5b **Two-input-stream cases (review round-2/3, MEDIUM #5) — the shipped
      structural shape:** stand up graph-ingest with TWO input ports
      (`objectstore.stored.entity` + `sensor.processed.entity`, per `structural.json`).
      Assert: (a) a valid newer message for entity E from stream B at a LOW sequence is
      NOT dropped by a prior high-sequence apply from stream A (per-stream guard); (b) the
      same entity E arriving on both streams serializes through one lane (no concurrent
      apply / no lost update). Per-stream independence is unit+integration tested; the full
      two-stream shape is covered end-to-end by **`e2e:structural` (GREEN)** — the shipped
      objectstore+sensor config. Not duplicated as a bespoke in-process two-stream test.
- [x] 6.5c **Durable guard: restart (review B2) + high-cardinality eviction (B3):**
      `TestIntegration_IngestGuard_DurableSurvivesRestart` — apply seq N, wipe the in-memory
      tier (restart/eviction), redeliver an older seq < N → dropped via the durable tier;
      warms the cache; a newer seq still applies. Real NATS durable bucket.
- [~] 6.6 **Panic recovery (review H2):** covered by the primitive's
      `TestKeyedPool_PanicRecovery` (lane survives + disposition invoked) + graph-ingest's
      `OnPanic`→`msg.Nak()` wiring. Forcing a real ingestEntity panic through the wire isn't
      built (no clean panic injection); the two guarantees compose to "component doesn't crash".
- [~] 6.7 Throughput smoke: directional-only and CI-host-variance-prone; not built as a
      bespoke in-process test. Real throughput is validated by the semboids repro (task 7.6)
      + the new inflight/queue-wait metrics.

## 7. Spec + gates + close

- [ ] 7.1 `openspec validate graph-ingest-keyed-dispatch --strict`.
- [ ] 7.2 Gates: `go test -race` (pkg/dispatch, graph-ingest, component), `task lint`,
      schema no-drift (`ingest_lanes`/`max_ack_pending` are schema'd — regenerate + diff),
      `go vet -tags=integration`.
- [x] 7.3 **Core-lane behavior change → e2e before tag** (default lanes>1 changes the sole
      ENTITY_STATES writer; see [[feedback_e2e_required_for_breaking_changes]]).
      **`task e2e:structural` GREEN** (`validation_errors:0`, `entities_missing:0`,
      `data_loss_percent:0`; verify-entity-count 30s-timeout→3ms). NOTE: this run also
      surfaced an INDEPENDENT first-message-loss footgun (idempotent consumers fell to the
      framework `"new"` DeliverPolicy default when a JSON config omits `deliver_policy`) —
      fixed + split to its own PR #491 (merged to main); ADR-072 rebased onto it.
- [x] 7.4 semstreams-reviewer pre-merge (APPROVE — no blockers): re-checked the adversarial-review
      findings closed: per-entity ordering under keying; **the two-tier applied-sequence
      guard actually prevents redelivery reorder (B1)** AND survives restart (B2) +
      in-memory eviction (B3) via the durable tier; **panic in a lane is recovered + Nak'd,
      not a crash (H2)**; lane lifecycle ordering — build-before-subscribe, **Stop cancels
      the submit ctx before draining** (no teardown hang), drain-before-close (M3);
      per-lane guard state is sharded by the pool's lane index, not an independent hash
      (no shard race); ack-in-lane at-least-once + poison-ack-drop + submit-failure-Nak +
      durable-write-failure-Nak; bounded in-flight; the `cas_retries` metric is documented
      as contention observability not a keying proof (H1); metrics separate queue wait from
      processing; no regression to the other ~30 `ConsumeStreamWithConfig` call sites.
- [ ] 7.5 Archive → promote `graph-ingest` (ADD) + `keyed-dispatch` (new) into
      `openspec/specs/`. PR; CI; merge; tag (e2e:core gate first).
- [ ] 7.6 Confirm on gh#480 + re-run the semboids repro (`task sweep HZ=30 BOIDS=200`):
      ingest-bound classification clears; record achieved entity/s + the new histograms.

## 8. ADR

- [ ] 8.1 ADR-072 (Accepted after adversarial review): the keyed-ordered concurrency
      model for the sole ENTITY_STATES writer, primitive placement in `pkg/dispatch`,
      per-entity ordering + CAS-contention elimination, default-lanes>1 posture.
