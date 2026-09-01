# Inventory verification — what the architect re-checked, added, and struck

base: 5389ec5d102ea5327e477d5c2b202630f29e25d9

The contract permits starting from an explorer file (owner ruling A, 2026-08-30, #1180) on the condition that
the architect treats its zero-hit searches as claims to spot-check, adds what it missed, strikes what it
over-included, and says which. This file is that record. It does not restate the inventories; read them first.

## Spot-checked and CONFIRMED

Each re-derived from source in this worktree, not relayed.

| Claim | Source file | Re-check |
|---|---|---|
| `SignalMessage.Validate` is an unconditional `return nil` | carriers | `loop_tracker.go:594-596` — confirmed verbatim |
| URL-path loop id at `http.go:646-647,743-744`, form never checked | carriers | confirmed; `handleGetLoop` at `:606` is a third, un-named instance of the same spelling |
| `CreateLoopWithID` overwrites three maps unconditionally | attach | `state.go:170,171,180` — confirmed; the doc comment `:142-150` states it in prose |
| `canUserControlLoop` has exactly one caller | attach | `component.go:1204-1216`; only `commands.go:73` |
| `handleStatusCommand` performs no ownership check | attach | `commands.go:148-180` — confirmed, existence only |
| `processor/agentic-dispatch` has zero `errs.Classified` uses | precedent | `git grep -c 'errs.Classified' -- 'processor/agentic-dispatch/'` → no file matches, exit 1 |
| `persistLoopState` is best-effort | brief | `processor/agentic-loop/component.go:1989-2009` — every failure path logs and returns; no error is propagated |
| Absence of an `AGENT_LOOPS` record is a legitimate state | brief | `terminal_settlement.go:296-302` — confirmed |
| `identity.go:52-59` resolves caller-asserted identity | brief | confirmed; `IdentityFromRequest` = ctx → body `user_id` → `"http-user"` |
| `Approve` is advertised and unread | brief | `config.go:37`; `component.go:1186`; the only `hasPermission` call sites pass `"submit_task"` or a command's declared permission, and no command declares `"approve"` (`commands.go:22,30,38,46`) |
| Default `Approve: ["*"]` | brief | `config.go:101` — confirmed, so enforcing it breaks no default deployment |
| `DeleteLoop` has zero production callers | brief / #1233 | `git grep DeleteLoop` → declaration `state.go:340` + `recovery_test.go:530` + `state_test.go:202,218,233`. Confirmed |
| `reconcileTerminalRoute` is the merge precedent | brief | `terminal_settlement.go:56-81`, `mergeRouteField` `:39-53` |
| `pkg/lifecycle` create-vs-exists shape | precedent | `manager.go:290-297`, `errors.go:44-50`; four production consumers of the sentinel |
| Rule-engine tasks carry no loop id | brief | `processor/rule/actions.go:1713-1720` — `TaskMessage` literal sets no `LoopID`; `ParentLoopID` only, at `:1733-1735` |

## ADDED — facts neither inventory recorded, found while verifying

1. **`BaseMessage.MarshalJSON` is where `TaskMessage.Validate` actually runs on the dispatch side.**
   `message/base_message.go:221-226`. `inventory-carriers.md` traces the #1225 gauge leak to
   `json.Marshal(baseMsg)` at `component.go:975` but does not record WHY a marshal call is a validation seam.
   Without that link the ordering fix looks arbitrary. `git grep '\.Validate()' -- 'processor/agentic-dispatch/*.go'`
   returns only `component.go:239` (config) and `payload_registry.go:32` — dispatch never calls
   `TaskMessage.Validate` by name at all.

2. **The durable record carries the loop's owner, so post-restart ownership is decidable.**
   `agentic/state.go:82-84` declares `UserID`/`ChannelType`/`ChannelID` on `LoopEntity`;
   `processor/agentic-loop/handlers.go:495` calls `SetUserContext`, which stamps them at `state.go:929-940`.
   Neither inventory states this, and the whole merge-based design rests on it: without it, a post-restart
   attach could establish existence but never ownership, and fail-closed would refuse every legitimate resume.
   This is P8.

3. **A single terminal-release point already exists and is correctly placed.**
   `processor/agentic-loop/trajectory_handler_wiring.go:38-55`, deferred at `component.go:1447,1613,1813,2128`.
   `inventory-attach.md` enumerates `LoopManager`'s methods but not the component-level release, so #1233 read
   as "nothing releases anything" when the truth is "a correctly placed release exists and does not cover the
   loop maps". That changes the fix from new machinery to one extension.

4. **`SnapshotExpiredApprovals` cannot lose a candidate to terminal release.** `state.go:253-278`, skip at
   `:258`. Needed to close the obvious objection to #1233's fix.

5. **`read_loop_result` reads the durable bucket, not the in-memory map.**
   `processor/agentic-tools/loop_result.go:34-50`. The other precondition for releasing at terminal.

6. **Three late-arrival readers change behaviour under terminal release, and only three.** Enumerated in
   `design.md` §Piece 4. `ResolveApprovalIfPending` (`state.go:321-323`) errors on an absent loop where a
   present-terminal loop returns a clean stale-drop (`approval_response_handler.go:64-75`); the late tool-result
   path (`component.go:1806-1817`) is the same shape.

7. **`POST /loops/{id}/signal` is inert today — a payload-type mismatch on a seam this change gates.**
   `LoopTracker.SendSignal` (`loop_tracker.go:616-648`) publishes an `agenticdispatch.SignalMessage`
   (category `signal_message`, `loop_tracker.go:600`) on a hardcoded `"agent.signal." + loopID` (`:634`).
   agentic-loop's only handler for that port (`component.go:897-898` → `handleSignalMessage` `:2070`) asserts
   `*agentic.UserSignal` (category `signal`, `agentic/user_types.go:145`) at `:2077-2081` and logs *"Unexpected
   payload type"* and returns otherwise. The chat `/cancel` command publishes the correct type
   (`commands.go:112`). Neither inventory connects the two spellings: `inventory-carriers.md` records both types
   separately and notes `SignalMessage`'s registration, but does not observe that they share one subject and
   that only one is consumed. Raised as `design.md` §Open question 3 rather than absorbed.

8. **Line pins inherited from `inventory-attach.md` (base `24ab736e`) are stale in two places at the current
   base, and `findings-decisive-questions.md` inherited them.** `CreateLoopWithID`'s three map writes are
   `state.go:170,171,180`, not `:171,:172,:179`; `handlers.go`'s system-prompt add is `:876-881` and the
   context-manager fetch is `:875`, not `:872-877`. Several `terminal_settlement.go` pins in
   `inventory-attach.md` are marked "approx" and are off by 1-4: `mergeRouteField` is `:39`, `loadPersistedLoop`
   is `:83`, `isLoopRecordAbsent` is `:300`. The two committed inventory files are left as written at their own
   bases; every pin in the target state uses the corrected values, and `inventory-problem-shape.md` verifies
   clean at 43/43.

## Pins for the additions above

- `processor/agentic-dispatch/loop_tracker.go:594` — `func (s *SignalMessage) Validate() error {`
- `processor/agentic-dispatch/loop_tracker.go:595` — `return nil`
- `message/base_message.go:222` — `func (m *BaseMessage) MarshalJSON() ([]byte, error) {`
- `message/base_message.go:223` — `// Validate before serializing - invalid messages cannot be published`
- `agentic/state.go:82` — `UserID      string `json:"user_id,omitempty"`
- `processor/agentic-loop/handlers.go:495` — `if err := h.loopManager.SetUserContext(loopID, task.ChannelType, task.ChannelID, task.UserID); err != nil {`
- `processor/agentic-loop/state.go:940` — `entity.UserID = userID`
- `processor/agentic-loop/trajectory_handler_wiring.go:52` — `func (c *Component) releaseLoopTransientState(loopID string) {`
- `processor/agentic-loop/trajectory_handler_wiring.go:39` — `// It is the single terminal-release point so a future terminal path cannot`
- `processor/agentic-loop/state.go:258` — `if loop.State != agentic.LoopStateAwaitingApproval || loop.PendingApproval == nil {`
- `processor/agentic-loop/state.go:322` — `if !exists {`
- `processor/agentic-loop/state.go:340` — `func (m *LoopManager) DeleteLoop(loopID string) error {`
- `processor/agentic-tools/loop_result.go:40` — `// without having it injected into their prompt. Supports paging (max_bytes,`
- `processor/agentic-loop/handlers.go:875` — `cm := h.loopManager.GetContextManager(loopID)`
- `processor/agentic-loop/state.go:170` — `m.loops[loopID] = &entity`
- `processor/agentic-loop/state.go:171` — `m.pendingTools[loopID] = make(map[string]bool)`
- `processor/agentic-loop/state.go:180` — `m.contextManagers[loopID] = NewContextManager(loopID, model, m.contextConfig, opts...)`
- `processor/agentic-dispatch/loop_tracker.go:600` — `return message.Type{Domain: agentic.Domain, Category: agentic.CategorySignalMessage, Version: agentic.SchemaVersion}`
- `processor/agentic-dispatch/loop_tracker.go:634` — `subject := "agent.signal." + loopID`
- `processor/agentic-loop/component.go:898` — `handler = adaptVoidInputHandler(c.handleSignalMessage)`
- `processor/agentic-loop/component.go:2077` — `signalPtr, ok := baseMsg.Payload().(*agentic.UserSignal)`
- `processor/agentic-loop/component.go:2079` — `c.logger.Error("Unexpected payload type", "type", fmt.Sprintf("%T", baseMsg.Payload()))`
- `processor/agentic-dispatch/commands.go:112` — `signal := agentic.UserSignal{`
- `processor/agentic-dispatch/config.go:101` — `Approve:    []string{"*"}, // Everyone can approve`
- `processor/agentic-dispatch/component.go:1186` — `case "approve":`
- `processor/agentic-dispatch/config.go:37` — `Approve    []string `json:"approve"`
- `processor/rule/actions.go:1713` — `task := agentic.TaskMessage{`

## STRUCK — over-included or mis-scoped

1. **#1226 (`rule/expression` hand-rolled `isValidEntityID`).** Listed under Adjacent claims in both
   inventories. It is a different predicate on a different grammar, on a surface this change does not touch.
   Struck from scope; it stays its own issue.

2. **`agentic.UserMessage.Validate`'s missing token checks** (`agentic/user_types.go:65-81`,
   `inventory-carriers.md` §Claimed gap). Recorded there as a gap; it is not one this change should close.
   A refusal at decode has no submitter to answer and would reintroduce exactly the silent-drop class #1225 is
   about. Moved from "gap" to the spec's explicit carve-out list, with that reason attached.

3. **`HTTPMessageRequest` having no `Validate`** (`http.go:32-46`). Same disposition: its token fields reach the
   gate at the seam that can answer the client synchronously. Carve-out, not gap.

4. **`Loop` / `completionWire`** (`loop_wire.go:14,105`). Correctly flagged "noted for completeness" by the
   carriers inventory; formally struck as read-projection types with no intake path.

## Verifier status of the four pre-existing evidence files

Run at base `5389ec5d`, reported rather than fixed — the committed files stay as their authors wrote them:

1. `inventory-carriers.md` — **clean**, `pins=95 ok=95`, no drift. The brief's claim holds.
2. `inventory-attach.md` — **does NOT verify**: `pins=63 ok=5 drift=5 malformed=53 unparsed=8`. Its evidence is
   embedded in prose bullets rather than written in the pin grammar, so `task inventory:verify` cannot re-check
   it after any commit. Five genuine drifts: `commands.go:87`, `component.go:777`,
   `openspec/specs/entity-id-contract/spec.md:677`, and two in `docs/adr/105-...md` (`:35`, `:59`). The facts it
   asserts were re-derived by hand for this target state; the file itself is unmaintainable as pinned evidence.
3. `inventory-precedent.md` — **cannot be verified**: `base: 0a40ddf3` is a short sha and the script requires 40
   hex. Its pins were spot-checked by hand instead (see the table above).
4. `findings-decisive-questions.md` — **cannot be verified**: its `base:` line carries prose after the sha. It
   also inherited the stale `state.go:171-180` and `handlers.go:872-885` pins from `inventory-attach.md`; the
   conclusions are unaffected, the line numbers are not.

Both files this pass wrote verify clean: `inventory-problem-shape.md` `pins=43 ok=43`,
`inventory-verification.md` `pins=27 ok=27`.

## Searches

### Zero-hit claims re-run

- `git grep -c 'errs.Classified' -- 'processor/agentic-dispatch/'` → 0 files (exit 1). **CONFIRMED.**
- `git grep -n 'regexp.*[Ll]oop'` → 0. **CONFIRMED** — no hand-rolled loop-token shape check exists anywhere.
- `openspec/specs/agentic-dispatch/` does not exist — `ls openspec/specs/` → 52 capability directories, no
  `agentic-dispatch`. **CONFIRMED**, and it is why this change seeds the capability.
- `inventory-attach.md`'s reported `git grep -ln "loop" openspec/specs/*/spec.md` → 0 is a **bad search**, not a
  fact: the same pattern run here matches. The follow-up targeted grep it ran reached the right answer
  (`entity-id-contract` is the only spec with attach-relevant text), so the conclusion stands and only the
  recorded search is wrong. Recorded so the reviewer does not re-derive a false negative.

### NOT RUN

- `gopls` structural passes. `inventory-carriers.md` ran `gopls references` on `looptoken.Valid`; every
  additional caller/reference enumeration in this verification used `git grep -n` and `sed -n`. Structural
  completeness for `LoopManager`'s 71 methods is not claimed beyond the readers enumerated in `design.md`
  §Piece 4.
- Sister-repo sweep for products that author `reply_to` or rely on the unenforced `approve` permission. Per the
  standing ruling a sister pass sizes a migration note and never gates a design; it belongs with task 9.4.
- `git log -S` on `CreateLoopWithID`'s overwrite or on `DeleteLoop`'s disuse. No claim is made here about when
  or why either arose beyond what ADR-105 and the code comments already state.
