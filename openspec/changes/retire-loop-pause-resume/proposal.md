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

## Ruling conformance

Nine exported symbols leave the Tier 1 frozen `agentic` package. Two owner rulings on #1239 authorize them.
`task api:compat:report` lists a tenth removal, `CategorySignalMessage` — that one is #1231's (`78813ec7`),
already on `main`, not this change's.

- **R1** — option 1, [`issuecomment-5509878990`](https://github.com/C360Studio/semstreams/issues/1239#issuecomment-5509878990)
  (2026-09-02). Enumerates `handlePauseSignal`, `handleResumeSignal`, `PauseRequested`, `PauseRequestedBy`.
- **R2** — the widening, [`issuecomment-5516251150`](https://github.com/C360Studio/semstreams/issues/1239#issuecomment-5516251150)
  (2026-09-02). *"Extend #1251 to delete all four now"* — `cancel` becomes the entire vocabulary.

| Removed symbol | Declared on `main` at | Authorized by |
|---|---|---|
| `SignalPause` | `agentic/user_types.go:17` | R1 (the `pause` verb R1 deletes the handler for) |
| `SignalResume` | `agentic/user_types.go:18` | R1 (same, `resume`) |
| `SignalApprove` | `agentic/user_types.go:19` | R2, by name |
| `SignalReject` | `agentic/user_types.go:20` | R2, by name |
| `SignalFeedback` | `agentic/user_types.go:21` | R2, by name |
| `SignalRetry` | `agentic/user_types.go:22` | R2, by name |
| `LoopEntity.PauseRequested` | `agentic/state.go:66` | R1, by name |
| `LoopEntity.PauseRequestedBy` | `agentic/state.go:67` | R1, by name |
| `LoopEntity.StateBeforePause` | `agentic/state.go:68` | **Named by neither ruling** — see below |

**`StateBeforePause` is the one removal no ruling enumerates.** R1 names two of the three pause fields.
`StateBeforePause` is the third member of the same persisted pause record and exists solely to restore the
prior state on `resume`; with `resume` deleted, nothing can read it and nothing can write it. It is removed as
part of the pause record R1 retires rather than as a separate decision. Flagged here rather than folded in
silently — if the owner intends it kept, it is a one-field restore.

**semsage migration obligation.** semsage `processor/ui-api/http.go:182` cases on `agentic.SignalPause` and
`agentic.SignalResume` and **will not compile** on its next bump. Sister repositories are read-only to
SemStreams agents; the obligation — four edits including the operator-facing error text at `:186` — is recorded
for semsage's own maintainers in `docs/operations/migration-beta162-to-beta163.md`. No sister references the
four verbs R2 removes; semdragon publishes `SignalCancel` only.
