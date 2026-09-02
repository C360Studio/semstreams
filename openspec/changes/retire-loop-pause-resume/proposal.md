# Retire the loop pause/resume signal surface

## Why

`agentic-dispatch`'s current truth still describes a mechanism that no longer exists. Under *One control-signal
payload travels the loop signal subject*, the spec says of the deleted `POST /loops/{id}/signal` endpoint:

> *"Of its three verbs, only cancel was ever implemented on the loop side … pause and resume set a loop field no
> code reads."*

That sentence was accurate when it was seeded by the #1231 change one day earlier. It is not accurate now.
Issue #1239 — owner ruling option 1, 2026-09-02 — deleted the field, the two verbs, and both handlers. There is
no longer a loop field for pause to set, and `pause` is no longer a member of the signal vocabulary at all: an
`agentic.UserSignal` carrying it now fails `Validate()`.

Leaving the clause standing would make the capability's current truth describe a dead field as though it were
merely unread, which is the same advertised-absent defect #1239 exists to close.

## What Changes

One clause of one requirement, restated to describe the vocabulary as it now is. All four
existing scenarios under the requirement remain exactly as they are, and a fifth is added for the new refusal
MUST. The requirement heading is unchanged — six `// spec:` citations point at it
(`agentic/user_types_test.go:205`, `processor/agentic-dispatch/loop_signal_test.go:15,43`,
`loop_signal_integration_test.go:74`, `loop_tracker_test.go:419`,
`processor/agentic-loop/dispatch_cancel_integration_test.go:27`), and rewording it would strand every one.

## Impact

- **Affected capability:** `agentic-dispatch` — one requirement: one clause restated, one scenario added.
- **Affected code:** this change ships *with* the deletion, not after it. **Nine** exported symbols removed from
  a Tier 1 frozen package (`agentic`): the six signal verbs `SignalPause`, `SignalResume`, `SignalApprove`,
  `SignalReject`, `SignalFeedback`, `SignalRetry`, and the three fields
  `LoopEntity.PauseRequested`, `.PauseRequestedBy`, `.StateBeforePause`. Persisted surface removed:
  the `pause_requested`, `pause_requested_by`, `state_before_pause` JSON keys. Handlers, tests, five
  README/docs tables, the package godoc, an LLM prompt, a port description, and the generated
  `schemas/agentic-loop.v1.json` all change with it.
- **Adopter impact:** BREAKING. `cancel` is now the entire signal vocabulary — a `UserSignal` carrying any of
  the other six verbs now fails `Validate()`. Approval/rejection are unaffected: they travel as
  `ApprovalResponse` (ADR-039) and were never served by this payload. **semsage
  `processor/ui-api/http.go:182` will not compile** on its next bump; the obligation is recorded in
  `docs/operations/migration-beta162-to-beta163.md`. semdragon is unaffected (publishes cancel only).
