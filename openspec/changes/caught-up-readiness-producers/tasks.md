## 1. MEASURE THE MECHANISM FIRST — do not write producer code before this

- [ ] 1.1 **Throwaway real-NATS probe: does the consumer ack floor advance past terminal outcomes?**
      graph-ingest sets `MaxDeliver: 3` (`processor/graph-ingest/component.go:1510-1521`) and has
      five Nak/Term paths. Answer two questions against a live nats-server via testcontainers:
      (a) does `AckFloor.Stream` advance past a message that exhausts `MaxDeliver`?
      (b) does `Term()` advance it?
      **If either is NO, the floor stalls forever on one poison message and the readiness signal
      inverts into permanently-not-caught-up — wrong in the dangerous direction.**
      Precedent: a 40-line probe settled the `DiscardNew` question in one run and falsified the
      author's own hypothesis where prose argument had not.
- [ ] 1.2 Record the measured answer in `design.md` §D0 and delete the probe. If the floor stalls,
      fall back to `NumPending + NumAckPending` with NO ack-floor claim — the outstanding-work
      number still works; only the "contiguous high-water" framing must go, and the proposal's Why
      section must be corrected rather than left asserting something measurement disproved.
- [ ] 1.3 Confirm a `jetstream.Consumer` handle is reachable for `Info()`.
      `ConsumeStreamWithConfig` stores only the `ConsumeContext` (`natsclient/stream.go:388-393`)
      and hands the `Consumer` to `trackConsumer` (`:381`). Either add a narrow accessor or have
      graph-ingest retain its own handle — decide which is cleaner and say why in the task.

## 2. Shared envelope + projection

- [ ] 2.1 Add `BootstrapScope uint64` / `bootstrap_scope,omitempty` to `graph.IndexStatusResponse`
      with doc text stating: producer's own unit; `complete && scope == 0` means authoritatively
      nothing to do; the gate MUST NOT read it; it licenses nothing about absence
- [ ] 2.2 Add `graph.ComputeBacklogStatus` as a SECOND named projection beside `ComputeIndexStatus`.
      Do NOT add mutually-exclusive fields to `IndexStatusInputs` (makes an invalid state
      representable) and do NOT bend `ComputeIndexStatus` (risks byte-drift in graph-index's output,
      which the current spec protects). Note `ComputeIndexStatus` computes
      `Ready = target > 0 && indexed >= target`, which is false at 0/0 — hence a separate projection
- [ ] 2.3 Assert in a test that `EvaluateReadinessGate`'s verdict is identical for two envelopes
      differing only in `BootstrapScope` — this is the guard that stops it becoming a threshold knob
- [ ] 2.4 `readiness.KeyGraphIngest` and `readiness.KeyRule` constants

## 3. graph-ingest producer

- [ ] 3.1 Status tick mirroring `processor/graph-index/component.go:1119-1160` (one compute feeds
      both the gauges and the KV key)
- [ ] 3.2 Backlog projection: sum `NumPending + NumAckPending` across every bound consumer.
      **Pending alone under-reports by up to `defaultIngestLanes(8) × ingestLaneQueueDepth(256)
      = 2048`** in-process messages (`component.go:532`, `:538`, submit `:1567`) — add a test that
      would fail if only pending were counted
- [ ] 3.3 `BootstrapComplete` = existing boot-sweep latch (`component.go:663-665`, already surfaced
      in `Health()` `:911-913`) AND boot-backlog target reached. `BootstrapScope` = boot-backlog count
- [ ] 3.4 `State = degraded` on `consumer.Info()` failure, mirroring
      `processor/graph-index/watermark.go:70-79`
- [ ] 3.5 `StalenessMs` from the oldest outstanding message's `meta.Timestamp` (already read at
      `component.go:1541`) — reported, never gating
- [ ] 3.6 **OMIT `IndexedRevision` / `TargetRevision`.** Add a test asserting they are absent on the
      wire; a stream sequence in a KV-revision field corrupts every read-your-writes check

## 4. rule producer

- [ ] 4.1 Track sentinel observation per watcher generation. The existing `bootstrap` value is a
      goroutine-local `bool` (`entity_watcher.go:451`, flipped `:477`) that is consumed and
      discarded — it is a hook point, not existing state. Generation identity already exists
      (`managedEntityWatcher{watcher, generation}` `:196-199`, authority `:517-524`)
- [ ] 4.2 `BootstrapComplete` = conjunction over currently-authoritative generations; FALSE again on
      a new generation. **Deliberately diverges from graph-index's process-lifetime latch**
      (`processor/graph-index/watermark.go:120-127`) because the watcher set is runtime-mutable via
      component-config PUT (`service/component_manager_http.go:772` →
      `entity_watcher.go:290-395`) and recreation re-runs replay (`:209` passes no `UpdatesOnly`)
- [ ] 4.3 `BootstrapScope` = values replayed across bootstrap generations
- [ ] 4.4 `State` from the two EXISTING sticky latches — `graphStateGuardDegraded` (`:57-69`) and
      `graphStateResetRequired`. Do not invent states
- [ ] 4.5 Zero configured patterns ⇒ zero watchers ⇒ vacuously complete with scope 0
- [ ] 4.6 **Integration test that the empty-pattern nil sentinel actually arrives from real
      JetStream.** Unit coverage exists (`entity_watcher_atomic_bootstrap_test.go:282`); no
      integration test does. The behavior depends on `UpdatesOnly` being unset — verify, don't assume

## 5. Consumer fold + surface

- [ ] 5.1 `readiness.Set` — N watchers by key, `Start`/`Stop`/`WaitForFirst`; fold returns
      `(proceed, firstDeferKey, graph.DeferReason)` in deterministic key order, delegating each key
      to `graph.EvaluateReadinessGate`. **No new defer reasons. No optional-key flag** (an optional
      key is one you did not declare)
- [ ] 5.2 Verify absent-key fail-closed works with zero new semantics: `Watcher.Read()` returns
      `Known=false, Fresh=false` (`graph/readiness/watcher.go:271-283`) → gate short-circuits to
      `DeferStatusUnknown` (`readiness_gate.go:143-145`). Add a test
- [ ] 5.3 Separately-named coverage predicate for snapshot callers: `proceed && every declared key
      reports Lag == 0`. Must NOT gate any read path. Document why this does not violate ADR-085:
      that ADR banned coverage as admission control **for reads**, and explicitly defers the
      non-read case to "that consumer's evidence" — gh#712 is that evidence
- [ ] 5.4 One read-only HTTP dump: watched keys + per-key `known`/`fresh`/`age`. **Not a verdict** —
      a verdict bakes the key list into the framework, which requirement 5 forbids
- [ ] 5.5 Do NOT touch `/readyz` (`service/service_manager.go:1465-1485`)

## 6. Prove the surface has a consumer

- [ ] 6.1 Migrate `test/e2e/scenarios/stages/entities.go:72-89` off its entity-count +
      critical-entity deadline poll onto the fold. **This is what makes the change "add a signal and
      delete its workaround" rather than "add a signal"** — without it the fold is a phantom by this
      repo's grep-for-the-consumer rule

## 7. Honesty fixes this change exposes

- [ ] 7.1 rule `Health()` sets `Healthy = true` (`processor/rule/processor.go:552-555`) and stays
      true after the entity-watch lane latches degraded. Once the KV envelope says `degraded`, the
      two surfaces contradict each other — fix the condition
- [ ] 7.2 `processor/rule/processor.go:896-897` claims Start "ensures watchers started". False:
      `run()` closes `ready` at `:515` BEFORE `watchEntityStates` at `:518`
- [ ] 7.3 `graph/inference/hierarchy.go:145` claims "This method has NO side effects", contradicted
      by its own `:148-151`. That comment was the cover story for gh#713

## 8. Tests

- [ ] 8.1 Ack-is-terminal: assert `Ack()` is the last statement of the success path, or force a
      write failure and assert the ack floor does not advance
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
