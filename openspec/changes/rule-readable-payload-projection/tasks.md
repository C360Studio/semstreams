# Tasks — rule-readable-payload-projection

**Amend a task line when the work HAPPENS, not only when it succeeds.** A gate that ran and was
never recorded is indistinguishable from one that was skipped. A deliberate not-done gets `[~]`
AND a note in the spec delta.

## 1. The interface

- [x] 1.1 Added `message.RuleReadable`, documented in the sibling shape. **Landed in
      `message/rule_readable.go`, not `behaviors.go`**: revive's `max-public-structs` cap is 10 per
      FILE and `behaviors.go` was already at 10, so an 11th type there fails `task lint` (and CI).
      `behaviors.go` carries a pointer to the new file; `message/` is not in the rule's exclude
      list (`revive.toml:59` excludes only `test/e2e` and `agentic`).
- [x] 1.2 Content rule documented on the interface (`message/rule_readable.go`): expose/withhold
      lists, plus why (a rule cannot judge unstructured text; a coordinator can).
- [x] 1.3 `GenericJSONPayload.RuleFields()` returns `Data` (`message/generic_json.go:116`).

## 2. One projection home

- [x] 2.1 `ruleFields(msg) (map[string]any, bool)` in `processor/rule/payload_projection.go:36`.
      **Narrowed from the proposal's three steps to two**: the separate `*GenericJSONPayload`
      fallback is unreachable once 1.3 lands (only `*GenericJSONPayload` satisfies
      `message.Payload`, and it now satisfies `RuleReadable`), so a second branch re-deriving the
      same map would be a dead second interpretation of a shared type. The requirement
      "`GenericJSONPayload` remains readable through its data map" holds via 1.3 and is covered by
      `TestExpressionRuleGenericJSONPayloadUnchanged`. Flagged for the reviewer.
- [x] 2.2 Replaced. `expression_factory.go:139`, `message_handler.go:434` (`extractMessageData`),
      `message_handler.go:412` (`extractEntityID`) — **and a FOURTH copy the task did not name**,
      `test_rule_factory.go:66` (`TestRule.Evaluate`), found by the verification grep. Post-change
      grep for `GenericJSONPayload)` in non-test `processor/rule/` returns only the explanatory
      comment in `payload_projection.go:17`.

## 3. Observable miss

- [x] 3.1 `ExpressionRule.reportUnreadablePayload` (`expression_factory.go:47`) emits one
      `slog.Warn` naming rule, rule_id, payload_type and the remedy. Bounded by a per-rule
      `sync.Map` set keyed on the wire type (`expression_factory.go:39`); key space is the payload
      registry, not traffic. Per-rule rather than package-global so a config reload re-reports once
      against the new definition.
- [x] 3.2 Unchanged — the unreadable branch still `return false`s from `Evaluate`; the report is
      the only added effect. Covered by the neighbour-rule assertion in
      `TestExpressionRuleReportsUnreadablePayloadOncePerPairing`.
- [x] 3.3 `TestExpressionRuleLegitimateFalseProducesNoSignal`. Also: the `len(r.conditions) == 0`
      check moved AHEAD of projection so a rule that cannot fire anyway never reports a pairing.

## 4. Framework-owned payload implementations (all 15)

Structural facts only. For each, decide what a rule may match on and withhold authored/user content.

- [x] 4.1 All four in `agentic/rule_fields.go`. `LoopCompletedEvent` exposes `role` and `outcome`
      (`rule_fields.go:90`); proven end-to-end by task 5.3's test.
- [x] 4.2 All five in `agentic/rule_fields.go`.
- [x] 4.3 `ToolCall` (`rule_fields.go:279`): `id`, `name`, `loop_id`, `trace_id`, `approved_by`;
      withholds `arguments` (MODEL-AUTHORED) and `metadata`. `ToolResult` (`rule_fields.go:305`):
      `call_id`, `name`, `loop_id`, `trace_id`, `error_kind`, `result_hint`, `stop_loop`;
      withholds `content` (the result body), `error` (free prose) and `metadata`. Outcome is
      `error_kind`, resolved to `ToolErrorUnknown` when `Error` is set but `ErrorKind` is not —
      the default `tools.go:ToolErrorUnknown` already documents — so presence of the key means
      "this call failed" without a rule string-matching an error message.
- [x] 4.4 `UserMessage` (`rule_fields.go:329`): routing/threading ids and timestamp; withholds
      `content`, `attachments`, `metadata`. `UserResponse` (`rule_fields.go:352`): ids, `type`;
      withholds `content`, `blocks`, and `actions` (their `Label` is human-authored).
      `AgentRequest` (`rule_fields.go:377`): `request_id`, `loop_id`, `role`, `model`,
      `max_tokens`, `temperature`, `timeout`; withholds `messages` (THE prompt) plus `tools`,
      `tool_choice`, `response_format` as nested config with no scalar rule shape.
      `AgentResponse` (`rule_fields.go:403`): `status`, `finish_reason`, `retry_count`, nested
      `token_usage`; withholds `message` (model output) and `error`.
- [x] 4.5 Every projection carries a `Withheld:` line naming each omitted field and why; the file
      header states the standing convention (mirror the JSON wire; withhold authored content and
      open caller-populated maps) and instructs the next field-adder to decide there.
      `TestAgenticRuleFieldsWithholdContent` pins each decision as an assertion, and
      `TestProjectionTableCoversEveryRegisteredPayload` keeps that table honest against the
      registry.

## 5. Proof

- [x] 5.1 `processor/rule/payload_projection_test.go` — one test per scenario:
      `TestExpressionRuleMatchesRuleReadableTypedPayload`,
      `TestExpressionRuleGenericJSONPayloadUnchanged`,
      `TestExpressionRuleUndeclaredFieldNotReachable`,
      `TestExpressionRuleReportsUnreadablePayloadOncePerPairing` (covers both the report and the
      suppression, plus "other rules unaffected").
- [x] 5.2 Done post-commit. SIX mutations, each with `cp` backup + `md5 -q` restoration proof and
      `[applied]` printed between mutating and testing; no `git stash` (the 5 pre-existing stash
      entries are unchanged, oldest 2026-07-16, newest 2026-08-17). Evidence in the handoff.
      Mutation F was written AFTER a self-review of the fix found a real convention break in
      `AgentRequest`/`AgentResponse` (non-`omitempty` wire fields emitted conditionally); the fix
      added `TestZeroPayloadProjectionCoversMandatoryWireFields`, and F proves that test catches
      the defect it was written for.
- [x] 5.3 `TestShippedArchitectEditorRuleFiresOnLoopCompletedEvent` reads the SHIPPED
      `configs/rules/agentic-workflow/architect-editor.json` off disk, builds the rule through
      `NewExpressionRule`, wire-encodes a `LoopCompletedEvent` in a `BaseMessage`, decodes it
      through the production `payloadbuiltins` decoder, and asserts the rule fires — and that
      `result`/`prompt` did not reach the projection.
- [x] 5.4 Done. The pre-implementation run was itself the fails-without-fix evidence (the new
      tests failed with the unreadable WARN naming `agentic.loop_completed.v1`); the post-commit
      mutations used `cp` + `md5 -q`. `git stash list` confirms 3 unrelated entries — untouched.
- [x] 5.5 Committed before mutating; `[applied]` printed between each mutation and its test run.

## 6. Consequences to confirm, not to fix here

- [x] 6.1 CONDITIONS: yes — proven by 5.3's test against the shipped file. WIRING: no. No flow
      config references the pack; `grep -rn "configs/rules" configs/` shows `rules_files` entries
      for `research-graph`, `deep-research`, `lifecycle`, `lessons`, `cron` and `example-fan-out`,
      none for `agentic-workflow`. SECOND DEFECT, recorded not fixed: the rule's `on_enter`
      templates use `$entity.task_id`, `$entity.model`, `$entity.result`, which are not valid
      substitution tokens — the entity namespace is `$entity.id` and `$entity.triple.<predicate>`
      (`typed_substitution.go:79,118`) — and the message path has a nil entity anyway. Removing the
      readability barrier does not make this rule's ACTIONS correct. Config unchanged, per the task.
- [x] 6.2 Unblocked. The barrier was "a rule can only read `*GenericJSONPayload`", and it is what
      forces the two live type-erasure round trips: `payloadToBaseMessageBytes`
      (`processor/agentic-loop/governance_dispatcher.go:592`) marshals a typed
      `ProposedToolCallPayload` into a `map[string]any` solely because "rule conditions and
      `$message.*` substitution already consume `GenericJSONPayload.Data`"
      (`governance_dispatcher.go:585`), and `decodeVerdictPayload`/`verdictPayloadFromMap`
      (`component.go:2313,2333`) convert it back. A typed governance payload implementing
      `message.RuleReadable` now needs neither. Not implemented here (8.1/8.2).

## 7. Gates

- [x] 7.1 `task lint` — PASS (0 problems). Note: the FIRST run failed with
      `max-public-structs` on `message/behaviors.go`, which is why 1.1 moved the interface to its
      own file.
- [x] 7.2 `go test -race ./...` — PASS, no FAIL lines.
- [x] 7.3 `go test -race -tags=integration -p 2 -count=1 ./...` — PASS. 153 `ok` packages,
      0 FAIL, 0 SKIP, 8m59s wall. Docker up; `processor/rule`, `agentic`, `message`,
      `processor/agentic-loop` all in the ok set.
- [x] 7.4 `task schema:generate` — completed; `git status --short schemas/ specs/` and
      `git diff --stat schemas/ specs/` both empty. Expected: no operator-facing config changed.
- [x] 7.5 `openspec validate rule-readable-payload-projection --strict` — "Change
      'rule-readable-payload-projection' is valid".

## 8. Not in scope (recorded so the archiver does not infer completion)

- [~] 8.1 The governance verdict payload (#1045). Depends on this landing; separate change.
- [~] 8.2 Retiring the `verdictPayloadFromMap` type-erasure round trip
      (`processor/agentic-loop/component.go:2333`). Now unnecessary, but removing it is its own
      change with its own proof.
- [~] 8.3 Extending rule-opacity enforcement to `$message.*` paths and to action templates
      (`typed_substitution.go`, `message_substitution.go`). A `predicate-contract` question, and a
      real hole — file it rather than leaving it implicit. NOT FILED by this slice: issue filing is
      the owner's, and the developer contract's "a filed issue does not discharge an in-PR
      guarantee" cuts the other way too. Carried in the handoff as follow-up. Partly narrowed in
      practice: an explicit projection is now the only way content reaches `$message.*`, and the
      15 framework projections withhold it, so the hole is adopter-authored projections only.
- [~] 8.4 Adopter-owned payloads outside this repo. They implement the interface when they want the
      capability; that is the correct pressure.

- [~] 8.5 NOT IN THE TASK LIST, RECORDED SO IT IS NOT MISTAKEN FOR DONE: the other FRAMEWORK-owned
      registered payloads also remain unreadable — `governance.VerdictEvent`
      (`governance/verdict.go:133`), `agenticdispatch` (`processor/agentic-dispatch/payload_registry.go:42`),
      `gateddagexec.DispatchMessage` / `.StallEvent` (`processor/gated-dag/payload.go:110-111`), and
      `objectstore` (`storage/objectstore/stored_message.go:94`). The proposal's "all framework-owned
      payloads" reads on its own terms as broader than the 15 the tasks enumerate; this slice
      implemented exactly the 15 named. They now FAIL LOUDLY (the once-per-pairing report) instead of
      silently, which is the change's own remedy for the gap.
