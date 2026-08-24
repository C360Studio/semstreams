# GH-1063 tool wire terminal-scope correction

## Checkpoint

- Accepted #1063 inventory and design remain the basis: `docs/proposals/next-tag-test-gate-blockers-inventory.md` and
  `docs/proposals/next-tag-test-gate-blockers-design.md`.
- The owner accepted the latter on 2026-08-23 at SHA-256
  `56fa9dc95a4dbf6f3f7d121912972e036f7c1c2d55a8834e991eeafa8b37ae7a`, including #1063 Option 2:
  causal result proof, no serialization/overlap promise, no pre-tag dispatcher, and correction of outward truth.
- Mandatory review found one wording deviation and one artifact gap. This correction is advisory until independently
  reviewed and accepted.

## Measured conflict

Current durable outcome truth explicitly says the initial `approval_required` result is correlated nonterminal
coordination, is not persisted as COMPLETED, uses a phase-distinct message ID, and is followed by an approved
re-dispatch with the same CallID (`openspec/specs/agentic-tools/spec.md:435-450`). Production does exactly that
(`processor/agentic-tools/component.go:668-695`), and
`TestApprovalGateSameIDRedispatchExecutesOnceAndPublishesTerminalResult` proves zero initial executor calls, no initial
COMPLETED outcome, distinct pause/terminal message IDs, one approved execution, and terminal replay
(`processor/agentic-tools/outcomes_test.go:171-223`).

The new #1063 wording instead says every admitted call produces a correlated terminal outcome/result
(`openspec/specs/agentic-tools/spec.md:519-532`). “Admitted” is overloaded: a call can pass global/per-loop admission and
then be intercepted by `approval_required`. Read literally, the new sentence terminalizes a deliberately nonterminal
pause and contradicts current truth and production.

A second gap is durable process evidence: no fresh active #1063 OpenSpec change currently records the accepted owner
rulings, the causal test replacement, repeated Docker gate, policy guard, forced omissions, or this reviewer-directed
wording correction.

## Options

1. **Do nothing.** Leaves current capability text internally contradictory and #1063 evidence outside the OpenSpec
   change ledger. Rejected.
2. **Make `approval_required` terminal.** Would persist the pause as COMPLETED, collide with approved re-dispatch, and
   change a safety workflow. Rejected.
3. **Promise only “a correlated result” for every delivery.** This is true of the approval pause but discards the useful
   durable-terminal guarantee for executed/rejected calls. Rejected.
4. **Scope terminal correlation exactly and add a fresh active evidence change.** Preserve nonterminal approval
   coordination; quantify terminal guarantees only over terminal execution, terminal policy rejection, and completed
   replay; record all #1063 rulings and evidence durably. Recommended.

## Corrected contract

Replace the new requirement text with this exact target:

### Requirement: Wire tool execution does not infer local parallelism from acknowledgement admission

The agentic-tools `tool.execute.>` path SHALL produce the exact correlated durable terminal outcome and result for each
logical tool call that reaches terminal execution or terminal policy rejection. Redelivery of an already-COMPLETED
logical call SHALL publish that same correlated terminal result without executor re-invocation.

An initial `approval_required` interception is correlated nonterminal coordination, not terminal execution or terminal
policy rejection. It SHALL retain the existing phase-distinct result message ID, SHALL NOT create a COMPLETED outcome,
and SHALL leave the same CallID eligible for approved re-dispatch. The approved re-dispatch enters the terminal
guarantee when it reaches execution or a terminal policy rejection.

The component SHALL NOT claim that `MaxAckPending=3` supplies local executor parallelism. That value governs
delivered-but-unacknowledged admission only. The wire contract SHALL promise neither serialized execution nor execution
overlap to executor authors or direct callers.

Multiple queued calls that reach terminal execution or terminal policy rejection SHALL each produce their exact
correlated durable terminal result. Correctness SHALL be proved by exact call/result causality under a finite liveness
bound, not by elapsed wall-clock classification. The current implementation uses one native callback through outcome
persistence, result publication, and delivery settlement before that callback returns. That is nonnormative
implementation evidence, not a stable serialized-execution contract.

#### Scenario: multiple terminal wire calls settle

- **GIVEN** three wire calls with distinct call IDs
- **AND** none is intercepted for approval
- **AND** each reaches terminal execution or terminal policy rejection
- **WHEN** the wire consumer processes them
- **THEN** each logical call produces its exact correlated durable terminal result
- **AND** the proof uses no elapsed-time threshold

#### Scenario: approval-required is a nonterminal correlated pause

- **GIVEN** a wire call that passes global and per-loop admission
- **AND** `approval_required` intercepts it before execution
- **WHEN** the initial delivery settles
- **THEN** the component publishes the existing correlated approval-required result with its phase-distinct message ID
- **AND** it creates no COMPLETED outcome
- **AND** an approved re-dispatch with the same CallID remains eligible for terminal execution

#### Scenario: acknowledgement admission is three

- **GIVEN** agentic-tools uses its component-owned `MaxAckPending=3`
- **WHEN** the consumer is observed
- **THEN** the value bounds delivered-but-unacknowledged messages
- **AND** no executor-concurrency claim is inferred

This correction changes specification scope only. It does not change allowlist policy, approval filtering, message IDs,
CallID reuse, outcome persistence, ACK/NAK behavior, executor invocation, MaxAckPending, consumer shape, or production
component code.

## Outward wording alignment

The #1063-edited README, package GoDoc, and concepts guide SHALL avoid “every admitted call reaches a terminal result.”
They may state the two levels explicitly:

- every wire response remains correlated to its CallID;
- an approval-required response is a nonterminal pause;
- a logical call reaching execution or terminal policy rejection receives a correlated durable terminal result;
- an approved re-dispatch uses the same CallID and later enters that terminal guarantee;
- no serialization or overlap promise is made.

The multiple-result diagram/scenario SHALL say its calls are not approval-intercepted. JetStream tuning language remains
admission-only. No new adopter terminology, knob, payload, or status surface is introduced.

## Fresh active OpenSpec change

Create, before merge, `openspec/changes/gh1063-correct-tool-wire-terminal-scope/` with:

- `proposal.md` — reviewer finding, internal contradiction, artifact gap, no production behavior change;
- `design.md` — accepted #1063 rulings, options above, exact terminal/nonterminal boundary, adopter seam, stop
  conditions;
- `tasks.md` — conservative unchecked tasks until each exact artifact/evidence exists;
- `conformance.md` — evidence table below with exact commands/results/durations and no inferred greens;
- `specs/agentic-tools/spec.md` — a `MODIFIED` requirement containing the complete corrected requirement and all three
  scenarios above.

Do not edit the archived durable-outcome change to carry this later correction. Do not archive the fresh change until
its exact delta, docs, test evidence, strict validation, independent review, and owner acceptance are recorded. The
current-spec wording must match the accepted delta when projected; current spec alone is not the durable change record.

### Required task ledger

```markdown
# Tasks: GH-1063 tool wire terminal-scope correction

- [ ] 1. Record the accepted #1063 inventory/design hashes and owner rulings in proposal/design.
- [ ] 2. Add the full agentic-tools MODIFIED requirement scoping terminal correlation to execution, terminal policy
  rejection, and completed replay while preserving nonterminal approval_required.
- [ ] 3. Align README, package GoDoc, concepts guide, and JetStream tuning wording with that exact boundary and no
  local-parallelism promise.
- [ ] 4. Verify `TestApprovalGateSameIDRedispatchExecutesOnceAndPublishesTerminalResult` proves pause nonterminality and
  approved terminal completion without production changes.
- [ ] 5. Record the historical timing RED and causal multiple-result GREEN with exact selected test output.
- [ ] 6. Record the race count-20 Docker integration gate with exact command, result, duration, and
  missing/duplicate/unexpected result state.
- [ ] 7. Record policy-guard RED/GREEN and all forced-omission results, including restoration evidence.
- [ ] 8. Record production-diff inspection proving no agentic-tools runtime behavior change.
- [ ] 9. Run and record contract tests and `openspec validate --all --strict --no-interactive`.
- [ ] 10. Obtain independent review and owner acceptance before archive/merge-readiness claims.
```

### Required conformance evidence table

| Ruling or obligation | Required exact evidence | Status rule |
|---|---|---|
| No local parallelism promise | accepted design hash/ruling; current one-callback implementation citation; corrected spec/docs | pending until all cited |
| MaxAckPending is admission only | jetstream-consumer-policy citation; corrected agentic-tools text; tuning-doc citation | pending until aligned |
| Approval pause remains nonterminal | spec lines 447-450; component lines 668-695; selected approval pause/approved execution test output | must be GREEN |
| Terminal execution/rejection is correlated and durable | component terminal rejection lines 646-665; execution persistence lines 699-725; selected outcome tests | must be GREEN |
| Causal multiple-call proof | selected `TestIntegration_MultipleToolCallsProduceAllResults` output, exact IDs/content, no missing/duplicate/unexpected/error result | must be GREEN |
| Repeated Docker proof | exact race count-20 command, exit result, duration, and active state if interrupted | must be GREEN; first failure stops |
| Timing/sleep debt removed | historical 811.816917ms RED; ten-run ~0.64s serial-compatible evidence; policy baseline diff and selected guard | factual, not rerun-until-green |
| Forced omissions | one omitted publication names missing ID; restored sleep fails policy guard; restored parallelism claim is rejected in conformance review; exact restoration evidence | every omission and restoration recorded |
| No production behavior change | relevant `component.go`, outcome, ACK/NAK, approval, message-ID and MaxAckPending diff inspection | must show no behavior delta |
| Durable artifact truth | complete active change plus strict OpenSpec validation | must be GREEN |

The conformance record SHALL use `PENDING`, `PASS`, `FAIL`, or `BLOCKED` per row and SHALL not convert package-level
`ok` output into evidence unless the named test is shown. It SHALL record owner rulings verbatim or by accepted artifact
hash; chat memory is not durable task truth.

## Focused evidence commands

```text
go test -v ./processor/agentic-tools \
  -run '^TestApprovalGateSameIDRedispatchExecutesOnceAndPublishesTerminalResult$' \
  -count=1 -timeout=30s

go test -v -race -tags=integration ./processor/agentic-tools \
  -run '^TestIntegration_MultipleToolCallsProduceAllResults$' \
  -count=20 -failfast -timeout=90s

go test -v ./test/testinfra \
  -run '^TestInfrastructurePolicyGuard$' \
  -count=1 -timeout=30s

go test ./test/contract/... -count=1 -timeout=120s
openspec validate --all --strict --no-interactive
```

If the repeated Docker test stops advancing exact result IDs, capture the missing-ID state and abort immediately rather
than waiting for the package timeout or retrying.

## Adopter/default behavior

Specific adopter: an external executor author or loop integrator consuming `tool.result.*`.

- A normal call that reaches execution receives the same correlated durable terminal result as today.
- A terminal allowlist/per-loop policy rejection receives the same correlated durable terminal rejection as today.
- An initial approval-required call receives the same correlated **nonterminal** pause as today, creates no COMPLETED
  record, and may be re-dispatched with the same CallID after approval.
- A completed redelivery receives the same stored terminal result without re-execution.
- If the adopter does nothing, configuration and runtime behavior are unchanged; there is no migration, new field,
  timeout, subject, worker count, or executor-safety obligation.
- The adopter should know only whether a result is the existing approval pause or a terminal result. They should not
  infer terminality from “admitted,” infer local concurrency from MaxAckPending, or predict execution overlap. Existing
  typed/error approval semantics carry the distinction; internal callback shape remains nonnormative.

## Stop conditions

Stop and return to owner review if any work would:

1. persist the initial approval pause as COMPLETED, change its message ID, change same-CallID approved re-dispatch, or
   otherwise alter approval behavior;
2. change production `component.go`, executor dispatch, ACK/NAK, outcome storage, MaxAckPending, subjects, timeouts, or
   lifecycle;
3. broaden the terminal guarantee to every globally/per-loop-admitted delivery without the approval exception;
4. weaken executed or terminally rejected calls from durable terminal correlation to an unqualified “some result”;
5. reintroduce elapsed-time or concurrency classification;
6. produce any missing, duplicate, unexpected, or error-bearing result in the repeated Docker gate—record the first
   failure and do not retry it away;
7. fail to reproduce a forced omission or fail to restore the isolated mutation;
8. leave any outward surface implying that approval-required is terminal or that MaxAckPending provides local
   parallelism;
9. lack exact owner-ruling, Docker, omission, validation, or production-diff evidence in the active conformance artifact;
10. fail strict OpenSpec validation or independent review.

No ADR, payload, communication path, orchestration change, or canonical decision skill is triggered. This is a
current-truth and durable-evidence correction only.

## Owner acceptance

The owner accepted this correction on 2026-08-23 at independently reviewed SHA-256
`22375a461578b6100a96d838d6726c2d4f2f10bedcfe80b483fc7914e9117332`. That acceptance authorizes the corrected
specification wording, outward documentation alignment, and fresh OpenSpec task/conformance evidence only. It does not
authorize production agentic-tools behavior changes.
