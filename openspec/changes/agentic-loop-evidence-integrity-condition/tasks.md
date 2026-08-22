# Tasks — agentic-loop-evidence-integrity-condition

**Amend a task line when the work HAPPENS, not only when it succeeds.** A gate that ran and was
never recorded is indistinguishable from one that was skipped. A deliberate not-done gets `[~]`
AND a note in the spec delta, because `[~]` stops the implementer but not the archiver.

## 1. Vocabulary

- [ ] 1.1 Declare `LoopEvidenceIntegrity = "agent.loop.evidence-integrity"` in
      `vocabulary/agentic/predicates.go`, adjacent to `LoopTerminalReason` and documented in the
      same shape: what it classifies, that it is stamped only on observed incompleteness, that
      absence licenses nothing, and the closed value set.
- [ ] 1.2 Register it in `vocabulary/agentic/register.go`, mirroring the `LoopTerminalReason`
      registration at `:439`. Rule-visible (this is a classification a rule must branch on), NOT
      rule-opaque.
- [ ] 1.3 Confirm no existing predicate already covers this — `grep` the `agent.loop.*` family for
      evidence/audit/integrity before adding.

## 2. Per-loop observation

- [ ] 2.1 Record observed audit loss per loop. `reportTrajectoryAuditFailure`
      (`processor/agentic-loop/trajectory_observability.go:30-48`) already receives the
      `trajectoryAuditFailure` value carrying `LoopID`; add a per-loop marker as a FOURTH SIBLING
      of the existing Health-latch / metric / log fan-out.
- [ ] 2.2 The marker MUST NOT be derived from the metric counter or by re-evaluating any predicate
      (spec: derived from the same observed failure value). Cross-check against the defect in
      gh#1033 for the shape to avoid.
- [ ] 2.3 Bound the marker's lifetime to the loop. It must not leak across loops or grow without
      limit in a long-running process; clear it when the loop reaches terminal.

## 3. Terminal write

- [ ] 3.1 Stamp `agent.loop.evidence-integrity = "incomplete"` in the terminal triple set in
      `processor/agentic-loop/graph_writer.go`, on the same mutation that carries
      `agent.loop.outcome` (see `:626` for the `LoopTerminalReason` precedent).
- [ ] 3.2 Verify it is stamped on ALL terminal paths that carry outcome — completion, failure, and
      cancellation each build their own triple set (`:566-582`, `:604-637`, `:645-659`). A loop that
      fails or is cancelled can still have lost evidence.
- [ ] 3.3 Confirm no `complete` value is ever written on any path.

## 4. Tests

- [ ] 4.1 Test the four spec scenarios: observed loss → `incomplete` on the terminal write; no loss
      → no triple; multi-stage failures → exactly one unqualified triple; failed condition write →
      work still transitions/publishes/ACKs.
- [ ] 4.2 Cover all three terminal paths from 3.2, not just completion.
- [ ] 4.3 Mutation-check the WIRING, not the primitive: delete the stamping CALL and confirm a test
      fails. A test that only exercises the predicate constant proves nothing about the wire.
- [ ] 4.4 Verify fails-without-fix via `git stash` (not checkout), and confirm the stash actually
      captured the change before trusting a red run.

## 5. Gates

- [ ] 5.1 `task lint` clean (revive warnings fail CI).
- [ ] 5.2 `go test -race ./...` — unit.
- [ ] 5.3 `go test -race -tags=integration -p 2 ./...` — CI runs BOTH suites.
- [ ] 5.4 `task schema:generate` then confirm no diff in `schemas/` or `specs/`.
- [ ] 5.5 `openspec validate agentic-loop-evidence-integrity-condition --strict`.
- [ ] 5.6 Predicate audit — `cmd/predicate-audit` if the new predicate needs classification there.

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
