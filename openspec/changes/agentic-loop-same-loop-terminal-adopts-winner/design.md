# Design — agentic-loop-same-loop-terminal-adopts-winner (#1399)

## 0. The ruling (verbatim)

Owner ruling, #1146 issuecomment-5854830449, 2026-09-27 (transcribed by the Fable judgment session; the owner's words
govern):

> **Q2 — ruled: the W2 fix lands through the shared owner, as its own beta.163 PR, independent of consolidation
> work.** `createTerminalMarker` (`processor/agentic-loop/terminal_owner.go:296-306`) adopts a saved terminal of a
> *different kind for the same loop* instead of refusing it; only a foreign loop ID still refuses; `settleTerminal`
> (`state.go:2007`) re-seats the entity's state and clears the losing outcome's fields. Test outcome: saved failure
> preserved, record terminal with the saved reason, saved event republished, the lane still consuming for a second
> loop. **This supersedes** #1362 ruling 2 (issuecomment-5802753726, "a marker whose kind differs … is poison …
> quarantined") for the same-loop case, and the 2026-09-27 acceptance of the latch as part of the W2 (a) bound (#1377
> issuecomment-5854244561). #1362 ruling 1 (cold cancel arm Retries) is untouched.

## 1. Change points

| Site | Before | After |
|---|---|---|
| `terminal_owner.go` `createTerminalMarker` | `saved.loopID() != loopID \|\| saved.kind() != candidate.kind()` refuses (→ Fatal → Quarantine → latch) | only `saved.loopID() != loopID` refuses; a same-loop saved terminal of any kind is adopted, `return saved, true, nil`; the "Terminal adopted" Warn line gains `candidate_kind` beside `kind` |
| `terminal_owner.go` synthetic decide | set when `saved.completed != nil && candidate.syntheticDecide != nil` | unchanged: a saved failure or cancel adopted over a completed candidate carries none; a saved completion adopted over a failed or cancelled candidate carries none either (a residual, § 4) |
| `state.go` `settleTerminal` | adopted content written over an entity assumed to be of the same kind | `State` and `Outcome` set from the adopted kind; completion clears `Error` and the cancel fields; failure clears `Result` and the cancel fields; cancel sets `CancelledBy`/`CancelledAt`, `Error = "cancelled by user"` (as `writeRecordCancelled` writes it) and clears `Result` |
| `commitTerminalSteps` | — | unchanged in shape: adopted → `terminalPublication` republishes the saved event; `settleTerminal` re-seats before `persistLoopState`; `recordCommittedTerminal` counts from the adopted outcome, so `loops_failed_total{reason}` and `LoopFailedEvent.Reason` agree |
| `component.go` carrier comment, `doc.go` COMPLETE_ section, migration note | "next terminal of the same kind", "another outcome is quarantined" | "of any kind", adoption |

Untouched: `adoptDurableCancel`, `settleUncancellableLoop` (#1362 ruling 1).

## 2. Spec edits

Block 1 ("Loop input classes settle after owner-specific durable done"): the scenario "A durable terminal with a
non-terminal record is owed to #1377" — "adopts it by loop identifier and terminal kind" → "by loop identifier …,
whatever terminal kind it derives"; "a same-kind later terminal" → "the loop's next terminal of any kind".

Block 2 ("The loop record names its outstanding request"): the adoption sentence (different kind → adopted, re-seated,
republished, acknowledged; foreign loop ID → refused, quarantined); W1's convergence sentence ("the loop's next
terminal of any kind converges the record"); the W2 sentence (the `decide` completion adopts the saved failure; the
latch sentence removed). Scenarios: "A redelivered terminal adopts the published terminal by identity" (by loop ID);
the W1 scenario (retitled "… of any kind"); "A terminal of a different kind meeting a durable terminal is refused"
(retitled "… adopts it", plus a foreign-loop-ID arm); the sweeper scenario's `decide` arm.

## 3. Tests (production entry paths, `-race`)

- `approval_cap_sweep_integration_test.go`: the lane-latch test inverted and renamed — on a `startRaceLane` process,
  the approved `decide` result is acknowledged, the marker is the saved failure byte for byte, the saved
  `agent.failed` event is published once and no `agent.complete`, the record is `failed` with `max_iterations` and no
  gate, the lane is not drained and health is not latched, and a second loop's tool result is acknowledged and
  applied. The unit-level fourth arm of `TestASweepAtTheIterationCapWhosePublishFailedSettlesOnTheNextAnswer` follows.
- `lost_terminal_record_integration_test.go` (T1): the different-kind arm — B's model-error failure adopts A's
  durable completion and converges the record `complete`.
- `terminal_owner_test.go`: the (b) other-kind arm becomes adoption (a cancel marker adopted by a completion) and a
  new foreign-loop-ID refusal arm is the control (no such test existed before); `settleTerminal` unit tests re-seat
  each adopted kind over an entity of another kind.
- `terminal_tool_redelivery_integration_test.go` (#1362 H1): the completion redelivered first now adopts the crashed
  cancel and converges the record; the cancel's redelivery is then acknowledged on the terminal record.
- Mutation evidence per site: the refusal condition restored in `createTerminalMarker`; the re-seat removed from
  `settleTerminal`.

## 4. Declared residuals

- A saved completion adopted by a terminal of another kind gets its completion triples but no #133 synthetic-decide
  triples: `createTerminalMarker` attaches them only when `candidate.syntheticDecide != nil`, and the marker does not
  record whether one was owed. Under W1 the original commit's stamp ran before the lost record write, so nothing is
  missing; after a crash between the marker `Create` and `stampTerminal` the synthetic decide is lost. Before this
  change that delivery was quarantined and the record never converged, so the residual is strictly narrower than
  what it replaces.
- The attempt's trajectory records the candidate's terminal (`recordHandlerResultTrajectory` runs before the owner),
  so a `decide` completion adopted as the saved failure is audited as the completion the handler derived. The same
  holds today for a same-kind content difference. It is in contract: `openspec/specs/agentic-loop/spec.md:455-458`
  — a `loop.terminal` fact means only "that one terminal outcome was observed and recorded", a redelivery MAY append
  another, and no terminal fact is a seal.
- A lane's own post-commit log line (the cancel lane's "Loop cancelled") still names the lane's candidate; the
  owner's "Terminal adopted" and "Loop terminal committed" lines name the committed kind.
