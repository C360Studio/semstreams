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
      AND the recorder's `emit` chokepoint now drops reports issued on a done context, so an
      abandoned audit attempt cannot re-insert a marker after that release. See 7.2.

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
      class: EVERY emit an abandoned attempt could make is now dropped. This completes a discipline
      the file already applies at `record`'s other checkpoints (`:133`, `:174`, `:196`, `:204`
      return without emitting once ctx is done) — those emit sites were the gaps in it. Nothing is
      lost: the budget branch already reports the loss synchronously, in time for the terminal
      write, which the late report is not.
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
