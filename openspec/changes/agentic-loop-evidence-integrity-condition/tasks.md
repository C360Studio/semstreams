# Tasks — agentic-loop-evidence-integrity-condition

**Amend a task line when the work HAPPENS, not only when it succeeds.** A gate that ran and was
never recorded is indistinguishable from one that was skipped. A deliberate not-done gets `[~]`
AND a note in the spec delta, because `[~]` stops the implementer but not the archiver.

## 1. Vocabulary

- [x] 1.1 Declare `LoopEvidenceIntegrity = "agent.loop.evidence-integrity"` in
      `vocabulary/agentic/predicates.go`, adjacent to `LoopTerminalReason` and documented in the
      same shape: what it classifies, that it is stamped only on observed incompleteness, that
      absence licenses nothing, and the closed value set.
- [x] 1.2 Register it in `vocabulary/agentic/register.go`, mirroring the `LoopTerminalReason`
      registration at `:439`. Rule-visible (this is a classification a rule must branch on), NOT
      rule-opaque.
- [x] 1.3 Confirm no existing predicate already covers this — `grep` the `agent.loop.*` family for
      evidence/audit/integrity before adding.

## 2. Per-loop observation

- [x] 2.1 Record observed audit loss per loop. `reportTrajectoryAuditFailure`
      (`processor/agentic-loop/trajectory_observability.go:30-48`) already receives the
      `trajectoryAuditFailure` value carrying `LoopID`; add a per-loop marker as a FOURTH SIBLING
      of the existing Health-latch / metric / log fan-out.
- [x] 2.2 The marker MUST NOT be derived from the metric counter or by re-evaluating any predicate
      (spec: derived from the same observed failure value). Cross-check against the defect in
      gh#1033 for the shape to avoid.
- [x] 2.3 Bound the marker's lifetime to the loop. It must not leak across loops or grow without
      limit in a long-running process; clear it when the loop reaches terminal. Enforced, not just
      asserted (review round 2, Finding B): `releaseLoopTransientState` clears it at every terminal,
      AND the recorder's `emit` chokepoint flags reports issued on a done context as `Late`, which
      `reportTrajectoryAuditFailure` honours by skipping the MARK only, so an abandoned audit
      attempt cannot re-insert a marker after that release. See 7.2 and 8.1.

## 3. Terminal write

- [x] 3.1 Stamp `agent.loop.evidence-integrity = "incomplete"` in the terminal triple set in
      `processor/agentic-loop/graph_writer.go`, on the same mutation that carries
      `agent.loop.outcome` (see `:626` for the `LoopTerminalReason` precedent).
- [x] 3.2 Verify it is stamped on ALL terminal paths that carry outcome — completion, failure, and
      cancellation each build their own triple set (`:566-582`, `:604-637`, `:645-659`). A loop that
      fails or is cancelled can still have lost evidence.
- [x] 3.3 Confirm no `complete` value is ever written on any path.

## 4. Tests

- [x] 4.1 Test the four spec scenarios: observed loss → `incomplete` on the terminal write; no loss
      → no triple; multi-stage failures → exactly one unqualified triple; failed condition write →
      work still transitions/publishes/ACKs.
- [x] 4.2 Cover all three terminal paths from 3.2, not just completion.
- [x] 4.3 Mutation-check the WIRING, not the primitive: delete the stamping CALL and confirm a test
      fails. A test that only exercises the predicate constant proves nothing about the wire.
      Four mutations run, each restored and checksum-verified:
      (1) `appendEvidenceIntegrity` CALL deleted from all three builders → the three
      `TestBuildLoopTerminalTriples_ObservedAuditLossStampsIncomplete` subtests +
      `TestTrajectoryAuditFailureMultipleStagesMarkOnce` fail;
      (2) the component's read `c.trajectoryAuditLoss.observed(loopID)` replaced by constant `false`
      at all three stamp sites → the unit suite stays GREEN and all three
      `TestTerminalStampCarriesObservedAuditLoss_Integration` subtests fail. This is the wiring the
      builder tests cannot see, and the reason the component-seam test exists;
      (3) the fourth-sibling `c.trajectoryAuditLoss.observe(failure.LoopID)` deleted from
      `reportTrajectoryAuditFailure` → marker tests, lifetime tests, and all seam subtests fail;
      (4) `c.trajectoryAuditLoss.release(loopID)` deleted from `releaseLoopTransientState` → all
      three `TestTerminalPathsReleaseObservedAuditLoss` subtests fail.
- [x] 4.4 Verify fails-without-fix, and confirm the mechanism actually captured the change before
      trusting a red run. DEVIATION on mechanism: `.agents/contracts/semstreams-developer.md`
      workflow rule 7 prohibits `git stash` in any form (it destroys untracked work — new test files
      are routinely untracked). Used the contract-mandated `cp` backup + `md5 -q` instead, which is
      strictly stronger here: every mutation was applied to a committed tree, `[applied]` printed
      between mutating and testing, and restoration proved by matching checksums on all four files
      plus a clean `git status` against the commit.

## 5. Gates

- [x] 5.1 `task lint` clean (revive warnings fail CI). Ran: go vet, go fmt, revive, fixed-port
      guard, natsclient request guard — all clean, exit 0.
- [x] 5.2 `go test -race ./...` — unit. 152 packages `ok`, 0 `FAIL`.
- [x] 5.3 `go test -race -tags=integration -p 2 ./...` — CI runs BOTH suites. exit 0, 152
      packages `ok`, 0 `FAIL`.
- [x] 5.4 `task schema:generate` then confirm no diff in `schemas/` or `specs/`. Generation
      succeeded; `git status --short` empty afterwards — no drift.
- [x] 5.5 `openspec validate agentic-loop-evidence-integrity-condition --strict`. Valid before
      and after implementation.
- [x] 5.6 Predicate audit — `cmd/predicate-audit` if the new predicate needs classification there.
      `agent.loop.evidence-integrity` is three-part and extracts clean: zero findings against it
      (489 candidates). The audit as a whole exits 1 on two PRE-EXISTING findings in files this
      change does not touch — `internal/graphmutation/protocol.go:26` (`entity.reconcile`) and
      `processor/rule/actions.go:46` (`reconcile_predicates`), both arity violations present at the
      base commit `7b6ff1e1`. `task predicate:audit` is not a CI gate (`.github/workflows/ci.yml`
      runs vet/fmt/revive/port-guard/request-guard, tests, build, schema); left as found.

## 6. Not in scope (recorded so the archiver does not infer completion)

- [~] 6.1 The other three candidate agentic-loop conditions — input fidelity, graph visibility,
      governance coverage. Deliberately deferred; this change proves the pattern on one.
- [~] 6.2 Any governance condition. BLOCKED: `Message`/`Violation`
      (`processor/agentic-governance/filter.go:37-58`, `violation.go:18`) carry no entity or loop
      ID and the package has zero `Graphable` implementations, so there is no addressable subject.
- [~] 6.3 A general reportable-conditions capability spec or ADR. Gated on a product naming a
      condition it will branch on — writing the contract before a consumer exists is the shape that
      produced `COMPONENT_CAPABILITIES`, retired at `8dfb0d7c`.
- [~] 6.4 A closed-value-set declaration on `PredicateMetadata` (`vocabulary/predicates.go:351-366`
      has free-text `Range` only). Real gap, but a framework-wide surface change that should not
      ride this change.

## 7. Review round 2 (APPROVE with two MEDIUM findings)

- [x] 7.1 **Finding A — total evidence loss produced no condition on any loop.** When Start finds
      the trajectory fact bucket unusable it nils the recorder (`component.go:785-800`); the
      Start-time report carries no `LoopID`, `trajectoryProviderAvailable()` returns `true` while
      the recorder is nil, and `recordTrajectoryBatchWithin` returns before any per-loop report. A
      process recording NOT ONE trajectory fact emitted a graph byte-identical to a healthy one —
      verbatim the state the proposal opens by naming. Fixed with a component-wide latch on
      `loopAuditLoss` (`observeAllLoops`), set at the single site that nils the recorder and
      consulted through the SAME `observed()` reader, so no stamp site can honour half the fact.
      One bool, set at most once, never released, no map growth.
- [x] 7.2 **Finding B — an abandoned audit attempt could re-mark a released loop.**
      `recordTrajectoryBatchWithin` abandons its goroutine on budget expiry; three emit paths were
      not `ctx.Err()`-guarded. Fixed at the recorder's single `emit` chokepoint (ctx threaded
      through `fail` and `evidenceFailure`) rather than at the three instances, which closes the
      class: EVERY emit an abandoned attempt could make is classified, not just the three that
      motivated the finding. SUPERSEDED IN PART by 8.1 — round 2 suppressed the whole report at this
      chokepoint, which also swallowed the ERROR line, the counter increment, and the Health latch.
      Round 3 narrowed it to the mark alone. The surviving claim is the one that matters: the budget
      branch already reports the LOSS synchronously, in time for the terminal write, which the late
      report is not — so the late MARK adds nothing and can only do harm. It does NOT follow that
      the late report adds nothing; its classification is new information (see 8.2).
- [x] 7.3 Spec delta widened for both findings — the ADDED requirement now covers the startup
      determination as observed loss and states the release-wins rule, with a scenario for each.
      Recorded in the DELTA, not as a `[~]` note, because `[~]` stops the implementer and not the
      archiver.
- [x] 7.4 Corrected the recorded justification for the handler's own discard site. The prior reason
      ("`startTrajectory` never errors so the branch is dead") described `handlers.go:849-852`, a
      different branch; the deferred discard at `:854-858` is LIVE on every error return in
      `HandleTask`. The correct reason — `MessageHandler` holds no `*Component` and no
      `trajectoryRecorder`, so `reportTrajectoryAuditFailure` is unreachable from inside
      `HandleTask` and the loop ID is created a few lines earlier — now lives in the
      `releaseLoopTransientState` doc comment where a future reader will look.
- [x] 7.5 Correction-propagation sweep: the widened semantics invalidated the narrower claim in the
      predicate doc comment, the registry description, `appendEvidenceIntegrity`'s doc, the
      component stamp comment, and the proposal's What Changes bullet. All five re-synced.

## 8. Review round 3 (APPROVE; one MEDIUM ruled, two LOW, one NIT)

- [x] 8.1 **MEDIUM — the round-2 drop was too wide; narrowed to the marker sink.** `emit` returned
      before `r.report`, so a late discovery lost its ERROR line, its `{stage,kind,reason}`
      increment, AND its Health latch — not just its marker. The MODIFIED requirement's unamended
      opening sentence ("Every trajectory audit failure SHALL emit `ERROR` ... increment ... and
      latch") was therefore literally false. Owner ruled AGAINST the cheaper spec amendment: a late
      ERROR, counter increment, and Health latch are all still TRUE; only the MARK is wrong when
      late, because only it can re-mark a released loop. Implemented with `Late bool` on
      `trajectoryAuditFailure`, set at the `emit` chokepoint and honoured by
      `reportTrajectoryAuditFailure`, which now skips ONLY
      `c.trajectoryAuditLoss.observe(...)`. Concrete case this restores: a `store.Put` backend
      error at T+240ms is reported as `evidence_put/backend_error` instead of being replaced by the
      synthetic `fact_create/timeout`, so a payload-size rejection is not diagnosed as latency. No
      spec amendment was needed — the ADDED requirement only ever forbade MARKING.
- [x] 8.2 Corrected the doc overstatement that made the wide drop look safe. A late report
      duplicates the LOSS; it never duplicated the CLASSIFICATION. `emit`'s doc now says so
      explicitly, which is the sentence that keeps the narrowing from being "simplified" back.
- [x] 8.3 **LOW — sixth stale claim, the one Finding A refuted.**
      `TestTrajectoryAuditFailureWithoutLoopIDMarksNothing`'s comment still asserted that bucket
      acquisition failure "belongs to the other three sinks: there is no entity to stamp". Post-fix
      it marks every loop via `observeAllLoops`. Re-scoped the comment to the case the test body
      actually exercises (`provider_resolve` with a recorder present) and pointed at the
      integration test that proves the other one. The one-directional correction loop caught in the
      act: the type doc got the right framing in round 2 and this did not inherit it.
- [x] 8.4 **LOW — seventh stale claim.** `proposal.md` Impact still said "(per-loop latch, terminal
      write)"; re-synced to name the component-wide latch and the `Late` threading.
- [x] 8.5 **NIT** — section order fixed: deliberate not-dones (6) now precede the review-round
      records, so an archiver reading top-to-bottom hits them first.
- [x] 8.6 Recorded the STRONGER reason the one-way latch is sound, supplied by the re-review:
      `Start` is one-shot (`component.go:458-465`, `lifecycleUsed` → `ErrAlreadyStarted`), so
      `initializeKVBuckets` cannot re-run and the latch cannot be re-evaluated in-process. That is
      structural, not policy — a policy can be revised, a compile-visible guard cannot be evaded.
      Captured in `observeAllLoops`' doc comment, demoting the policy reason to the weaker of the
      two.

## 9. Review round 4 (APPROVE-scoped; one MEDIUM, one NIT)

- [x] 9.1 **MEDIUM — `Late` had a prohibition with no positive guard.** Mutation C4 (unconditional
      `failure.Late = true` at `trajectory_recorder.go:320-322`) passed the ENTIRE suite, unit and
      integration. Reproduced independently before fixing: `go test -race -count=1
      ./processor/agentic-loop/...` all `ok`; `go test -race -count=1 -tags=integration
      ./processor/agentic-loop/` `ok` in 31.7s. The failure hiding behind it is the worst one this
      change has — an ObjectStore `Put` rejected at T+10ms, well inside the budget: the batch
      completes, `recordTrajectoryBatchWithin` takes `<-done` with `ctx.Err()==nil` and returns
      WITHOUT reporting, so the recorder's own emit is the ONLY report. Flagged Late it skips the
      mark and the terminal write stamps nothing despite a real, observed, in-budget audit failure.
      Fixed with `TestInBudgetAuditFailureMarksItsLoopThroughEmit`, driven through
      `recordTrajectoryBatchWithin` (the classification happens in `emit`; a direct fan-out call
      cannot see it) and tabled over BOTH emit families — evidence capture and immutable fact
      create — so the guard holds the class. Extends the existing `trajectoryTestStore` with
      `putErrBefore` (rejects the write and stores nothing, so lost-reply re-verification does not
      recover it), mirroring the bucket fake's `createErrBefore`/`createErrAfter` naming.
- [x] 9.2 Why the suite went blind, recorded so the shape is recognisable: round 3 made `Late`
      load-bearing on the mark and simultaneously invisible to the tests that had guarded this
      seam. The recorder tests that caught round-2's blanket drop worked because the report stopped
      firing at all; after the narrowing the report still fires and only the flag differs, so they
      pass. `TestOnTimeAuditFailureMarksAndReachesEverySink` calls `reportTrajectoryAuditFailure`
      directly and so pins the fan-out, never the classification. The house shape applies:
      MUST NOT needs a POSITIVE guard, and the guard must run through the code that SETS the flag.
- [x] 9.3 **NIT — eighth stale claim, one layer further out.** `graph_writer.go`'s
      `appendEvidenceIntegrity` doc and `loopAuditLoss`' type doc both still described the mark as
      an ungated fourth sibling "fed by the same `trajectoryAuditFailure` value". After round 3 it
      is the one sink of the four gated on `!Late`. Both now say so, and both state the converse
      (an in-budget failure is often the only report of itself). `appendEvidenceIntegrity` also now
      records what absence means precisely: no loss observed IN TIME — still never a completeness
      claim.
