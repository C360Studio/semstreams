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
- [x] 5.4 One read-only HTTP dump: watched keys + per-key `known`/`fresh`/`age`. **Not a verdict** —
      a verdict bakes the key list into the framework, which requirement 5 forbids.
      **BUILT on `gateway/graph-gateway` (owner call).** The spec delta carries a MUST for it
      ("The gateway MUST expose a read-only surface..."), so shipping it unbuilt would have made the
      archived spec claim a surface that did not exist — the exact phantom class this program exists
      to kill. `GET {prefix}/readiness` returns one row per configured key: envelope + `known` +
      `fresh` + `age_ms`. NO aggregate verdict field, guarded by a WIRE-level test (a struct-level
      one would not catch a future added field) that also runs in the all-ready case, which is
      precisely when someone would be tempted to add one.
      **Key list comes from CONFIG (`readiness_keys`), never a framework default** — the producer
      set is deployment-dependent, so a framework list would either report permanently-unknown
      producers a deployment does not run or silently omit ones it does. Empty list ⇒ no watchers,
      no route registered; the route's ABSENCE is a clearer signal than an endpoint that always
      answers empty. Watcher-start failure is NON-FATAL: an observability surface must not take the
      query path down with it.
      **CORRECTION to this file's earlier note:** it named `processor/graph-clustering` as the
      migration candidate. That was WRONG and the migration must not be done — its two watchers have
      deliberately ASYMMETRIC semantics (graph-index absent ⇒ defer; graph-embedding absent ⇒ benign,
      drop to structural-only, `component.go:1388-1392`), while `Set` is an all-or-nothing
      conjunction that refuses optional keys. Folding it onto `Set` would defer on an absent
      embedding key and break unopted deployments — a behavior change, not a simplification.
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
- [x] 7.3 `graph/inference/hierarchy.go:145` claims "This method has NO side effects", contradicted
      by its own `:148-151`. That comment was the cover story for gh#713

## 8. Tests

- [x] 8.1 Ack-is-terminal. **DEVIATION, measured:** the prescribed "force a write failure and assert
      `NumPending + NumAckPending` stays > 0 while it fails" assumes write failures are RETRYABLE.
      Probed 2026-07-30 — poisoned resident state AND a deleted RMW target both produce TERMINAL
      dispositions (Term), so the message leaves both counters in milliseconds and outstanding work
      correctly returns to 0. Asserting "stays > 0" would assert something FALSE about this component.
      Implemented as `TestIntegration_ReadyImpliesTheWritesAreDurable`: at the instant the producer
      first reports caught-up, all 150 published entities must already be readable in ENTITY_STATES —
      the observable consequence of ack being terminal. Still no ack-floor assertion (§D0).
      **This test caught a vacuous-green in my OWN earlier tests**: `publishEntity` published a bare
      `graph.EntityState`, which `decodeEntity` cannot decode, so every message was ack-DROPPED as
      poison and the backlog "drained" with ENTITY_STATES empty. Fixed by wrapping in `BaseMessage`
      AND registering the decoder before `Start` (`registerMergeTestPayload`).
- [x] 8.2 `BootstrapComplete` false → true → false-on-new-generation
- [x] 8.3 Absent declared key ⇒ `DeferStatusUnknown`
- [x] 8.4 Backlog counts delivered-but-unacked (would fail if only pending were counted)
- [x] 8.5 Revision fields absent for the backlog producer
- [x] 8.6 Gate verdict identical across differing `BootstrapScope`
- [x] 8.7 Count the assertions that ran, then break an input — a green new gate may have skipped
      everything. **Counted: 70 unit PASS / 0 SKIP, 9 integration PASS / 0 SKIP.** Inputs broken and
      confirmed failing for 8 guards: the consumer accessor, the pending+ack sum, the bind-time scope
      capture, the rule replay counter, the gate's BootstrapScope invariance, the e2e config-drift
      guard, the `Info()` race guard, and — the one that mattered — the durability test, which
      exposed that three earlier readiness tests had been passing over an EMPTY graph.

## 9. Spec + docs

- [ ] 9.1 Apply the `graph-index-readiness` delta; `openspec validate --all --strict`
- [x] 9.2 **ADR-088** — one page: "readiness is per-producer; aggregation belongs to the consumer."
      This is a cross-repo contract (semdragon + SemMachina both consume it) and the decision most
      likely to be re-litigated. Mechanics stay in the spec
- [x] 9.3 Adopter note stating the RULE, not the delta: ask `GRAPH_STATUS` for caught-up; one key per
      producer you depend on; fold client-side; absent = unknown = fail closed;
      `bootstrap_scope == 0` means there was nothing to do

## 10. Gates

- [x] 10.1 `task lint` · `go vet` plain + `-tags=integration` + `-tags=live_llm`
- [x] 10.2 `go test -race ./...` — grep `^FAIL` explicitly; the pipeline exit code reports the tail stage
- [ ] 10.3 Full `go test -race -tags=integration ./...` sweep (framework packages touched)
- [x] 10.4 `task schema:generate`; `git diff schemas/ specs/` empty
- [x] 10.5 **`task e2e:structural`** (graph-ingest + graph-index + rule on one stack) AND
      **`task e2e:statistical`** (adds graph-embedding + graph-clustering — the multi-watcher
      consumer path). Both, despite the not-breaking verdict.
      **BOTH GREEN 2026-07-30, exit 0.** `entity_load_poll_count=0` on both: the migrated stage
      returns on its FIRST check because every declared producer already reports caught-up — which
      is only reachable with all three keys Known + Fresh + healthy + `Lag == 0`. That is the two new
      envelopes working end to end on a real stack, not just in tests. Also `entities_missing=0`,
      `data_loss_percent=0`, `validation_errors=0` on both.
- [x] 10.6 All gates under `GOFLAGS=-mod=readonly`

## 11. Review + integration

- [ ] 11.1 `semstreams-reviewer` — treat an internal APPROVE as necessary-not-sufficient on
      concurrency/startup code
- [ ] 11.2 Fable review — cross-plane ownership + a cross-repo contract
- [ ] 11.3 Owner-run Codex gate
- [x] 11.4 Re-run `openspec list` and re-check HOLDs. **RE-VERIFIED 2026-07-30 at PR-head (counts
      moved since the earlier check — #761 landed mid-session and the archive sweep changed the
      queue).** Four other changes in flight: `predicate-raw-key-representation` 10/14,
      `predicate-contract-enforcement` 42/44, `graph-index-replacement-semantics` 15/19,
      `poison-response-scoping` complete. **NONE of the four carries a `graph-index-readiness`
      delta** (checked per change, not assumed), so no HOLD applies to this change's capability.
      `openspec/specs/rule-entity-watching/` still exists, so the generation-scoped watcher authority
      §4 relies on remains spec'd truth rather than in-flight surface.
- [x] 11.5 File the follow-ups. **FILED 2026-07-30, each VERIFIED against code before filing rather
      than copied from this list — and one was wrong:**
      · **#762** GraphQL `graph.query.*` keeps its `QueryResponse` envelope → `data.<field>.data.*`.
        Mechanism located: the unwrap gate at `gateway/graph-gateway/component.go:1720` matches only
        `graph.index.query.`, so `graph.query.summary` (GraphQL `graphSummary`) is never unwrapped.
        The line reference in this task's original text had gone stale; the gate had moved.
      · **#763** `graph-index` / `graph-embedding` expose no `bootstrap_complete` / `staleness_ms`
        gauges — a requirement the CURRENT spec already states and the code does not satisfy. The
        two producers this change adds have the same gap, so they are folded into the same issue.
      · **#764** `BoundedDispatcher.Stats()` chain dead-ends. **CORRECTED:** this list said
        "`KeyedPool.Stats()` computed and never read", which is FALSE — it has a caller at
        `pkg/dispatch/dispatcher.go:235`. The phantom is one level up: `BoundedDispatcher.Stats()`
        has no caller outside the package. Filing the assumed shape would have sent someone to the
        wrong function.
      · **#765** `processor/rule/docs/entity-watching.md:113-120` describes an "Authoritative
        `WatchAll` guard" the code no longer has, and asserts a bootstrap ordering guarantee that
        does not exist — actively reinforcing the wrong model gh#732 was filed to correct.
      **NOT filed, by owner decision: the upstream nats.go `Consumer.Info()` race.** Noted LOCALLY
      instead, on `guardedConsumer` in `natsclient/client.go`, including the load-bearing fact that
      it is **still present in v1.52.0** (we pin v1.48.0; the unsynchronized `p.info = resp.ConsumerInfo`
      is byte-identical across four releases). The note exists because the likeliest way that guard
      dies is someone bumping the dependency and assuming upstream fixed it.
