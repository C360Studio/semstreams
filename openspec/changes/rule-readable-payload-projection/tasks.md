# Tasks — rule-readable-payload-projection

**Amend a task line when the work HAPPENS, not only when it succeeds.** A gate that ran and was
never recorded is indistinguishable from one that was skipped. A deliberate not-done gets `[~]`
AND a note in the spec delta.

## 1. The interface

- [ ] 1.1 Add `message.RuleReadable` to `message/behaviors.go` beside its ten siblings, documented
      in the same shape: what it is for, that it is optional and discovered by type assertion, and
      that the payload declares its own fields rather than the engine deriving them.
- [ ] 1.2 Document the content rule on the interface itself: expose structural facts; withhold
      LLM-authored and user content (ADR-036). This is the sentence an adopter reads before writing
      their first implementation.
- [ ] 1.3 `GenericJSONPayload.RuleFields()` returns `Data` (`message/generic_json.go:74`).

## 2. One projection home

- [ ] 2.1 Add a single helper that resolves rule-readable fields: `RuleReadable` first, generic data
      second, unreadable third. It returns enough for the caller to tell "empty" from "unreadable".
- [ ] 2.2 Replace all three hand-rolled type switches with it — `expression_factory.go:130`,
      `message_handler.go:445` (`extractMessageData`), `message_handler.go:412` (`extractEntityID`).
      Verify by grep that no fourth copy survives.

## 3. Observable miss

- [ ] 3.1 Surface an unreadable rule/payload-type pairing. Bounded per pairing, not per message.
- [ ] 3.2 Do not change evaluation semantics: the rule does not fire; other rules are unaffected.
- [ ] 3.3 Confirm the existing "condition legitimately false" path is untouched and produces no new
      signal.

## 4. Framework-owned payload implementations (all 15)

Structural facts only. For each, decide what a rule may match on and withhold authored/user content.

- [ ] 4.1 `LoopCompletedEvent`, `LoopFailedEvent`, `LoopCancelledEvent`, `LoopCreatedEvent`
      (`agentic/events.go`). `LoopCompletedEvent` MUST expose `role` and `outcome` — this is what
      unblocks the dead rule in task 6.1.
- [ ] 4.2 `TaskMessage`, `UserSignal`, `ApprovalPendingEvent`, `ApprovalResponse`, `ContextEvent`.
- [ ] 4.3 `ToolCall`, `ToolResult` — expose call identity, tool name, and outcome; **withhold result
      bodies**.
- [ ] 4.4 `UserMessage`, `UserResponse`, `AgentRequest`, `AgentResponse` — expose routing and
      structural fields; **withhold message text, prompts, and model output**.
- [ ] 4.5 Record the structural-vs-content decision per payload in a comment where it is not
      obvious. A future reader must be able to tell a deliberate omission from an oversight.

## 5. Proof

- [ ] 5.1 Test each spec scenario: typed payload matchable; generic unchanged; undeclared field
      unreachable; unreadable observable; repeat suppressed.
- [ ] 5.2 **Mutation-check the WIRING**: delete the `RuleReadable` branch from the helper and
      confirm a test fails. A test that only exercises the interface proves nothing about the seam.
- [ ] 5.3 Prove the `architect-editor` rule can now fire — a test driving a `LoopCompletedEvent`
      through the message path with that rule's exact conditions.
- [ ] 5.4 Verify fails-without-fix using `cp` + `md5 -q`. **Never `git stash`** — the contract
      prohibits it and the repo's stash stack holds unrelated entries.
- [ ] 5.5 Commit before mutation-checking; print `[applied]` between mutating and testing.

## 6. Consequences to confirm, not to fix here

- [ ] 6.1 Confirm `configs/rules/agentic-workflow/architect-editor.json` would now fire. Whether it
      is wired into a shipped flow is a separate question — record the answer, do not change the
      config.
- [ ] 6.2 Confirm #1045's first barrier is now unblocked for a future governance payload. Do not
      implement that payload here.

## 7. Gates

- [ ] 7.1 `task lint` (revive warnings fail CI).
- [ ] 7.2 `go test -race ./...`.
- [ ] 7.3 `go test -race -tags=integration -p 2 ./...` — CI runs BOTH.
- [ ] 7.4 `task schema:generate`, then no diff in `schemas/` or `specs/`.
- [ ] 7.5 `openspec validate rule-readable-payload-projection --strict`.

## 8. Not in scope (recorded so the archiver does not infer completion)

- [~] 8.1 The governance verdict payload (#1045). Depends on this landing; separate change.
- [~] 8.2 Retiring the `verdictPayloadFromMap` type-erasure round trip
      (`processor/agentic-loop/component.go:2333`). Now unnecessary, but removing it is its own
      change with its own proof.
- [~] 8.3 Extending rule-opacity enforcement to `$message.*` paths and to action templates
      (`typed_substitution.go`, `message_substitution.go`). A `predicate-contract` question, and a
      real hole — file it rather than leaving it implicit.
- [~] 8.4 Adopter-owned payloads outside this repo. They implement the interface when they want the
      capability; that is the correct pressure.
