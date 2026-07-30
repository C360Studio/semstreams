## 1. MEASURE THE MECHANISM FIRST — do not write producer code before this

- [x] 1.1 **Throwaway real-NATS probe: does the consumer ack floor advance past terminal outcomes?**
      graph-ingest sets `MaxDeliver: 3` (`processor/graph-ingest/component.go:1510-1521`) and has
      five Nak/Term paths. Answer two questions against a live nats-server via testcontainers:
      (a) does `AckFloor.Stream` advance past a message that exhausts `MaxDeliver`?
      (b) does `Term()` advance it?
      **MEASURED 2026-07-30 on both deployed server versions (2.10-alpine, 2.12-alpine), identical
      results: (a) NO. (b) YES.** And worse than "stalls": with no traffic the floor sits behind the
      poison message indefinitely (verified +5s/+10s), then on the next unrelated ack **jumps past
      the never-applied message entirely**. Wrong in BOTH directions — permanently-not-caught-up
      while idle, falsely-covered under traffic. Full table in `design.md` §D0.
- [x] 1.2 Record the measured answer in `design.md` §D0 and delete the probe. **Done: §D0 rewritten
      with the measurement table, `proposal.md`'s "What makes it computable" corrected (it asserted
      the contiguous high-water the probe disproved), probe deleted.** Fallback TAKEN:
      `NumPending + NumAckPending`, no ack-floor claim anywhere — that sum measured 0 in all 12
      observations, including both cases where the floor was wrong. New constraint the spec must
      carry: the sum means *no outstanding work*, NOT *everything was applied* — a `MaxDeliver`-parked
      message leaves both counters (gh#742 owns operator visibility for it).
- [x] 1.3 Confirm a `jetstream.Consumer` handle is reachable for `Info()`.
      **Verified: today the ONLY retained handle is `jetstreamMetrics.consumers`
      (`jetstream_metrics.go:147-155`), and `c.jsMetrics` is nil unless `WithMetrics` was called with
      a non-nil registry (`options.go:209-223`). Reading readiness from there would make the signal
      silently degrade when metrics are off — a phantom by this repo's own rule.**
      **DECISION (storage): store the `jetstream.Consumer` beside the `ConsumeContext` in the
      ALREADY-UNCONDITIONAL `c.consumers` bookkeeping** (`client.go:80-81`, populated
      `stream.go:385-390`), as ONE map value rather than parallel maps — six sites create/replace/
      delete that bookkeeping, and a missed delete would hand out a handle to a stopped consumer.
      Rejected "graph-ingest retains its own handle": `ConsumeStreamWithConfig` does not return the
      consumer and has **20+ non-test callers**, so that route needs a signature change or a parallel
      API for one caller's benefit. Rejected reusing the metrics map: conditional, and string-keyed
      by `stream:consumer` so a miss is silent.
      **DECISION (exposure) — REVISED TWICE by Fable review on PR #758; the handle stays PRIVATE:**
      `func (c *Client) OutstandingWork(ctx, streamName, consumerName string) (uint64, error)`.
      · Not a `jetstream.Consumer` lookup: that leaks consume/fetch capability to callers who need a
      number, and puts `Info().AckFloor` one field away from every holder — structurally reopening
      the class §D0 just closed. · Not `(pending, ackPending uint64, err error)` either: that
      signature needed a doc comment warning callers away from its own affordance, which means the
      signature was wrong. The sum is computed INSIDE, so gating on one half is unrepresentable.
      The "halves for observability" justification was a phantom by this repo's grep-for-the-consumer
      rule — zero consumers; the metrics path polls `Info()` itself. Widen deliberately if a real
      one appears. · Unbound/failed consumer returns an ERROR, never 0 — unknown backlog must not be
      representable as empty backlog; mapping it to `degraded` stays caller policy.
      Note `updateStats` (`jetstream_metrics.go:196-211`) already calls `consumer.Info()` on a poll —
      precedent for the call, and a reason to keep the readiness tick's own cadence separate rather
      than piggybacking on metrics collection.

## 2. Shared envelope + projection

- [x] 2.1 Add `BootstrapScope uint64` / `bootstrap_scope,omitempty` to `graph.IndexStatusResponse`
      with doc text stating: producer's own unit; `complete && scope == 0` means authoritatively
      nothing to do; the gate MUST NOT read it; it licenses nothing about absence
- [x] 2.2 Add `graph.ComputeBacklogStatus` as a SECOND named projection beside `ComputeIndexStatus`.
      Do NOT add mutually-exclusive fields to `IndexStatusInputs` (makes an invalid state
      representable) and do NOT bend `ComputeIndexStatus` (risks byte-drift in graph-index's output,
      which the current spec protects). Note `ComputeIndexStatus` computes
      `Ready = target > 0 && indexed >= target`, which is false at 0/0 — hence a separate projection
- [x] 2.3 Assert in a test that `EvaluateReadinessGate`'s verdict is identical for two envelopes
      differing only in `BootstrapScope` — this is the guard that stops it becoming a threshold knob
- [x] 2.4 `readiness.KeyGraphIngest` and `readiness.KeyRule` constants

## 3. graph-ingest producer

- [x] 3.1 Status tick mirroring `processor/graph-index/component.go:1119-1160` (one compute feeds
      both the gauges and the KV key)
- [x] 3.2 Backlog projection: sum `NumPending + NumAckPending` across every bound consumer.
      **Pending alone under-reports by up to `defaultIngestLanes(8) × ingestLaneQueueDepth(256)
      = 2048`** in-process messages (`component.go:532`, `:538`, submit `:1567`) — add a test that
      would fail if only pending were counted
- [x] 3.3 `BootstrapComplete` = existing boot-sweep latch (`component.go:663-665`, already surfaced
      in `Health()` `:911-913`) AND boot-backlog target reached. `BootstrapScope` = boot-backlog count.
      **`BootstrapScope` is captured AT BIND, not at the first status tick — this was a real defect
      found by mutation, not a style choice.** The first tick runs after `setupSubscriptions`,
      `createStatusBucket`, and goroutine scheduling, with delivery live throughout; a 200-message
      backlog drained before it **5 runs out of 5**, so the producer published `complete && scope == 0`
      — the contract's "authoritatively nothing to do" — on every start that had real work.
      A failed bind-time read leaves the capture unclaimed so the first successful tick still fills it.
- [x] 3.4 `State = degraded` on `consumer.Info()` failure, mirroring
      `processor/graph-index/watermark.go:70-79`
- [x] 3.5 `StalenessMs` from the oldest outstanding message's `meta.Timestamp` (already read at
      `component.go:1541`) — reported, never gating.
      **DEVIATION, deliberate:** reports the AGE OF THE VIEW (now − JetStream timestamp of the most
      recently APPLIED message) instead of the literal oldest-outstanding. Tracking the latter exactly
      needs per-message bookkeeping over up to 2048 in-flight messages on the ingest hot path; this is
      graph-index's own semantic for the same field (its `IndexedAt`) and costs one atomic store on the
      ack path. They coincide closely because delivery is approximately stream-ordered, it is a FLOOR
      either way, and the field is reported-never-gating so the residual cannot change a verdict.
- [x] 3.6 **OMIT `IndexedRevision` / `TargetRevision`.** Add a test asserting they are absent on the
      wire; a stream sequence in a KV-revision field corrupts every read-your-writes check

## 4. rule producer

- [x] 4.1 Track sentinel observation per watcher generation. The existing `bootstrap` value is a
      goroutine-local `bool` (`entity_watcher.go:451`, flipped `:477`) that is consumed and
      discarded — it is a hook point, not existing state. Generation identity already exists
      (`managedEntityWatcher{watcher, generation}` `:196-199`, authority `:517-524`)
- [x] 4.2 `BootstrapComplete` = conjunction over currently-authoritative generations; FALSE again on
      a new generation. **Deliberately diverges from graph-index's process-lifetime latch**
      (`processor/graph-index/watermark.go:120-127`) because the watcher set is runtime-mutable via
      component-config PUT (`service/component_manager_http.go:772` →
      `entity_watcher.go:290-395`) and recreation re-runs replay (`:209` passes no `UpdatesOnly`)
- [x] 4.3 `BootstrapScope` = values replayed across bootstrap generations
- [x] 4.4 `State` from the two EXISTING sticky latches — `graphStateGuardDegraded` (`:57-69`) and
      `graphStateResetRequired`. Do not invent states
- [x] 4.5 Zero configured patterns ⇒ zero watchers ⇒ vacuously complete with scope 0
- [x] 4.6 **Integration test that the empty-pattern nil sentinel actually arrives from real
      JetStream.** Unit coverage exists (`entity_watcher_atomic_bootstrap_test.go:282`); no
      integration test does. The behavior depends on `UpdatesOnly` being unset — verify, don't assume.
      **VERIFIED against a real server 2026-07-30:** a watch on a pattern matching nothing in an
      empty bucket delivers the nil sentinel as its FIRST update. `complete && scope == 0` is
      therefore reachable; had it not been, every empty pattern would sit forever mid-replay and
      defer every consumer on a healthy deployment.

## 5. Consumer fold + surface

- [x] 5.1 `readiness.Set` — N watchers by key, `Start`/`Stop`/`WaitForFirst`; fold returns
      `(proceed, firstDeferKey, graph.DeferReason)` in deterministic key order, delegating each key
      to `graph.EvaluateReadinessGate`. **No new defer reasons. No optional-key flag** (an optional
      key is one you did not declare)
- [x] 5.2 Verify absent-key fail-closed works with zero new semantics: `Watcher.Read()` returns
      `Known=false, Fresh=false` (`graph/readiness/watcher.go:271-283`) → gate short-circuits to
      `DeferStatusUnknown` (`readiness_gate.go:143-145`). Add a test
- [x] 5.3 Separately-named coverage predicate for snapshot callers: `proceed && every declared key
      reports Lag == 0`. Must NOT gate any read path. Document why this does not violate ADR-085:
      that ADR banned coverage as admission control **for reads**, and explicitly defers the
      non-read case to "that consumer's evidence" — gh#712 is that evidence
- [ ] 5.4 One read-only HTTP dump: watched keys + per-key `known`/`fresh`/`age`. **Not a verdict** —
      a verdict bakes the key list into the framework, which requirement 5 forbids.
      **NOT BUILT YET — deliberately held, needs an owner call.** `Set.Dumps()` exists and is tested
      (the data half is done). The missing half is a route, and there is no in-process HTTP consumer
      that folds today: §6's consumer is an e2e stage that reads over NATS, so a route added now
      would report over an empty key list — a phantom by this repo's own grep-for-the-consumer rule.
      **The real candidate is `processor/graph-clustering`**, which already hand-rolls TWO readiness
      watchers (`component.go:585`, `:592`, `:1380`, `:1395`) and is exactly what `Set` replaces.
      Migrating it gives the process a real `Set` to expose. That is a genuine simplification but is
      NOT in this change's task list — file it, or fold it in on an owner call.
- [x] 5.5 Do NOT touch `/readyz` (`service/service_manager.go:1465-1485`)

## 6. Prove the surface has a consumer

- [x] 6.1 Migrate `test/e2e/scenarios/stages/entities.go:72-89` off its entity-count +
      critical-entity deadline poll onto the fold. **This is what makes the change "add a signal and
      delete its workaround" rather than "add a signal"** — without it the fold is a phantom by this
      repo's grep-for-the-consumer rule

## 7. Honesty fixes this change exposes

- [x] 7.1 rule `Health()` sets `Healthy = true` (`processor/rule/processor.go:552-555`) and stays
      true after the entity-watch lane latches degraded. Once the KV envelope says `degraded`, the
      two surfaces contradict each other — fix the condition
- [x] 7.2 `processor/rule/processor.go:896-897` claims Start "ensures watchers started". False:
      `run()` closes `ready` at `:515` BEFORE `watchEntityStates` at `:518`
- [ ] 7.3 `graph/inference/hierarchy.go:145` claims "This method has NO side effects", contradicted
      by its own `:148-151`. That comment was the cover story for gh#713

## 8. Tests

- [ ] 8.1 Ack-is-terminal: assert `Ack()` is the last statement of the success path, and force a
      write failure and assert **`NumPending + NumAckPending` stays > 0** (⇒ `Ready` false) while it
      fails. **Do NOT assert on the ack floor** — §D0 measured it unusable, nothing here reads it, and
      that assertion passes for the wrong reason once the failure hits `MaxDeliver` exhaustion (floor
      also does not advance there, but the message is dropped)
- [ ] 8.2 `BootstrapComplete` false → true → false-on-new-generation
- [ ] 8.3 Absent declared key ⇒ `DeferStatusUnknown`
- [ ] 8.4 Backlog counts delivered-but-unacked (would fail if only pending were counted)
- [ ] 8.5 Revision fields absent for the backlog producer
- [ ] 8.6 Gate verdict identical across differing `BootstrapScope`
- [ ] 8.7 Count the assertions that ran, then break an input — a green new gate may have skipped
      everything

## 9. Spec + docs

- [ ] 9.1 Apply the `graph-index-readiness` delta; `openspec validate --all --strict`
- [ ] 9.2 **ADR-088** — one page: "readiness is per-producer; aggregation belongs to the consumer."
      This is a cross-repo contract (semdragon + SemMachina both consume it) and the decision most
      likely to be re-litigated. Mechanics stay in the spec
- [ ] 9.3 Adopter note stating the RULE, not the delta: ask `GRAPH_STATUS` for caught-up; one key per
      producer you depend on; fold client-side; absent = unknown = fail closed;
      `bootstrap_scope == 0` means there was nothing to do

## 10. Gates

- [ ] 10.1 `task lint` · `go vet` plain + `-tags=integration` + `-tags=live_llm`
- [ ] 10.2 `go test -race ./...` — grep `^FAIL` explicitly; the pipeline exit code reports the tail stage
- [ ] 10.3 Full `go test -race -tags=integration ./...` sweep (framework packages touched)
- [ ] 10.4 `task schema:generate`; `git diff schemas/ specs/` empty
- [ ] 10.5 **`task e2e:structural`** (graph-ingest + graph-index + rule on one stack) AND
      **`task e2e:statistical`** (adds graph-embedding + graph-clustering — the multi-watcher
      consumer path). Both, despite the not-breaking verdict
- [ ] 10.6 All gates under `GOFLAGS=-mod=readonly`

## 11. Review + integration

- [ ] 11.1 `semstreams-reviewer` — treat an internal APPROVE as necessary-not-sufficient on
      concurrency/startup code
- [ ] 11.2 Fable review — cross-plane ownership + a cross-repo contract
- [ ] 11.3 Owner-run Codex gate
- [ ] 11.4 Re-run `openspec list` and re-check HOLDs. **Verified 2026-07-30 at `87d7e0fc`: four
      changes in flight, none touching `graph-index-readiness`; `rule-entity-watcher-hardening` is
      ARCHIVED and `openspec/specs/rule-entity-watching/` exists, so the generation-scoped authority
      this design relies on IS spec'd truth.** Counts move — re-verify
- [ ] 11.5 File the follow-ups: GraphQL `graph.query.*` double-nesting
      (`gateway/graph-gateway/component.go:1688` prefix gate — gh#712 named it) ·
      `bootstrap_complete`/`staleness_ms` gauges missing on the two EXISTING producers (current spec
      requirement already not fully satisfied) · `pkg/dispatch.KeyedPool.Stats()`
      (`keyed_pool.go:430`) computed and never read — phantom by this repo's own rule ·
      stale docs describing a removed `WatchAll` guard in
      `processor/rule/docs/entity-watching.md:113-120`
