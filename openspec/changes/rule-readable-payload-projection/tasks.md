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
      `error_kind`, DERIVED through the new `ToolResult.EffectiveErrorKind()`
      (`agentic/tools.go:587`) rather than re-implemented, so presence of the key means "this call
      failed" without a rule string-matching an error message. Review round: the projection
      originally inlined the `ErrorKind`→`ToolErrorUnknown` default, which would have been the
      THIRD copy (`processor/agentic-tools/component.go:891,907` and
      `processor/agentic-loop/handlers.go:2296-2300` predate it). Lifting it onto the type stops
      the count at two, in our own package; migrating the two older call sites is filed separately
      because `processor/agentic-tools/` is adjacent to active work.
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
      mutations used `cp` + `md5 -q`. `git stash list` shows 5 unrelated entries (oldest
      2026-07-16, newest 2026-08-17), unchanged at start and end — none created, none dropped.
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
      practice, but NOT closed: content still reaches `$message.*` verbatim through
      `GenericJSONPayload.RuleFields()`, which returns its caller-supplied `Data` map unfiltered.
      What narrowed is the typed lane — the 15 framework projections withhold content by
      construction — so the remaining hole is generic payloads and adopter-authored projections,
      neither of which the engine enforces opacity on.
- [~] 8.4 Adopter-owned payloads outside this repo. They implement the interface when they want the
      capability; that is the correct pressure.

- [~] 8.5 NOT IN THE TASK LIST, RECORDED SO IT IS NOT MISTAKEN FOR DONE: the other FRAMEWORK-owned
      registered payloads also remain unreadable — `governance.VerdictEvent`
      (`governance/verdict.go:133`), `agenticdispatch` (`processor/agentic-dispatch/payload_registry.go:42`),
      `gateddagexec.DispatchMessage` / `.StallEvent` (`processor/gated-dag/payload.go:110-111`), and
      `objectstore` (`storage/objectstore/stored_message.go:94`). The proposal's "all framework-owned
      payloads" reads on its own terms as broader than the 15 the tasks enumerate; this slice
      implemented exactly the 15 named. They now FAIL LOUDLY (the once-per-pairing report) instead of
      silently, which is the change's own remedy for the gap. The capability-gated `agentic/research`
      registrar (6 further first-party types, wired via `graphresearch.RegisterPayloads` in
      `cmd/semstreams/main.go` and `cmd/e2e-semstreams/main.go`) is the same class of recorded
      exclusion — rule-lane-reachable when the capability is selected, landing loud, not silent
      (added at gate round 2; see the inventory's registry census).

## 9. Review-round remediation (approved with four required fixes)

- [x] 9.1 `configs/agentic.json` — the `governance-approve-all-audit` description claimed the
      `tool_name` condition scopes a `>`-subscribed rule. False after this change:
      `ApprovalPendingEvent` exposes `tool_name` AND `call_id` (`agentic/rule_fields.go:267-268`),
      `ApprovalResponse` and `ToolResult` expose `call_id` (`:289`, `:339`). Replaced with the
      truth — scoped by the component's `agent.toolcall.proposed.>` INPUT PORT — plus the
      escalation: widening that component's inputs makes the rule emit an approve verdict whose
      `$message.call_id` is a REAL GATED call id, and verdicts correlate on payload `call_id`
      (`processor/agentic-loop/component.go:2287-2288`, demuxed to per-call waiters by `call_id`),
      so in enforce mode that is a human-approval-gate bypass. This is the shipped reference
      config (`README.md:215`, `docker/compose/agentic.yml:70`,
      `docs/basics/07-agentic-quickstart.md:81`), not an e2e
      fixture. COMMENT ONLY — rule and ports unchanged (1 line, `git diff --stat` = 1 insertion,
      1 deletion; JSON re-validated).
- [x] 9.2 Discoverability: the pointer at `behaviors.go:19` is not the catalogue. Added a
      `## Rule Interfaces` entry to `message/doc.go:99` (same shape as its siblings: methods,
      "Use when", example) plus a `RuleReadable` arm in the Runtime Discovery Pattern block
      (`doc.go:137-141`), and a table row + explanatory paragraph in
      `docs/basics/03-graphable-interface.md:246,250`. Both state the load-bearing fact: ABSENCE means
      the engine cannot read the payload at all.
- [x] 9.3 `ToolResult.EffectiveErrorKind()` added on the type (`agentic/tools.go:587`); the
      projection calls it (`agentic/rule_fields.go:344`). Empty return is the third state ("did not
      fail"), documented as the reason to branch on emptiness rather than compare a member.
      `TestEffectiveErrorKind` covers all four states. Pre-existing call sites deliberately NOT
      migrated.
- [x] 9.4 Attestation and doc corrections: `5.4` said "3 unrelated entries" where `5.2` said 5 —
      corrected to 5 (measured at start and end, oldest 2026-07-16, newest 2026-08-17). `8.3`
      overclaimed that explicit projection is now the only way content reaches `$message.*` —
      tightened, because `GenericJSONPayload.RuleFields()` still returns `Data` verbatim; what
      narrowed is the typed lane. `proposal.md:68` "all 16 framework-owned payloads" corrected to
      "the 15 agentic payloads plus `GenericJSONPayload`" — measured: the registry holds 21
      framework-owned types, 16 readable, 5 residual, matching `8.5` exactly.
      `message/rule_readable.go:40` gained an adopter-guidance paragraph on the three sharp edges:
      booleans mirror `omitempty` so `stop_loop` is absent-when-false and a rule must test absence;
      `$message.type` is polysemous across `ContextEvent`/`UserSignal`/`UserResponse`; and
      projected values feed NATS subject templates, so dotted values and RFC3339Nano timestamps are
      now reliably available where they were previously incidental.
- [x] 9.5 NEW EXPORTED SYMBOL, recorded because the exported-surface gate re-runs when the surface
      grows: `agentic.ToolResult.EffectiveErrorKind() ToolErrorKind`. On `agentic/`, not one of the
      framework packages requiring owner design review, and directed by the review.

## 10. Codex review round (PR #1052) — four required changes

- [x] 10.1 WITHHELD `LoopFailedEvent.Reason` (`agentic/rule_fields.go`). Round 2 exposed it after my
      reviewer enumerated producers and found closed literals; Codex correctly identified that as a
      property of today's callers, not a contract. Measured: `LoopFailedEvent.Validate`
      (`agentic/events.go:155-163`) constrains `LoopID`/`TaskID` only — nothing constrains `Reason` —
      and `MessageHandler.BuildFailureEvent` / `BuildFailureMessages`
      (`processor/agentic-loop/handlers.go:2594-2602`) are EXPORTED and take it as a free string, so an
      adopter can legally put prose there. No closed vocabulary introduced: that is a typed-contract
      change (validated enum, rejection before publish side effects, migration for two exported
      builders) and belongs to its own proposal. The method comment leads with "DO NOT RESTORE IT BY
      ENUMERATING ITS PRODUCERS" so the next reader does not repeat the round-2 conclusion. `outcome`
      still carries the failure signal a rule can act on. Mutation H proves the withhold-list test
      catches a restored `reason`.
- [x] 10.1a Re-audited EVERY other exposed field against the same test — constrained by a validator or
      closed type, or merely by its callers? `reason` was the only explanation-shaped unconstrained
      field in the exposed set; full result in the handoff. The test itself is now a standing rule in
      the `agentic/rule_fields.go` header, together with a second-tier caveat naming the exposed
      classification fields whose vocabulary is documented but NOT enforced (`outcome`, ContextEvent
      `type`, `result_hint`, `error_kind`, `finish_reason`) versus the three that are (UserSignal
      `type`, UserResponse `type`, AgentResponse `status`).
- [x] 10.2 Action-substitution proof BELOW THE LIFECYCLE SEAM:
      `TestMessagePathSubstitutesTypedPayloadFieldsIntoActions`
      (`processor/rule/payload_projection_test.go`) wire-encodes a typed payload, decodes it through
      the production `payloadbuiltins` decoder, and calls the unexported `handleSemanticMessage` on a
      HAND-ASSEMBLED `Processor` → `evaluateRulesForMessage` → `StatefulEvaluator` (OnEnter) →
      `ActionExecutor` substitution → publish, asserting the substituted subject token, three
      substituted body values, absence of unresolved templates, and absence of withheld content.
      Mutations I and J prove it detects both an unwired projection and a dropped `MessageData` wire.
      WHAT IT DOES NOT PROVE, corrected from an earlier overclaim of "the running processor": the
      production component factory, `Initialize`/`Start`, real NATS delivery, and real KV state — the
      test supplies `newMockKVBucket` and `mockPublisher`. That real-lifecycle gap is filed as
      **#1058**; the review explicitly permits filing it rather than building it here, and
      `test/e2e/` is another agent's active area.
- [~] 10.3 SUPERSEDED BY SECTION 12. This line previously read "EXPORTED-SURFACE DESIGN GATE materialized"
      and was marked complete. That was wrong twice over: an after-the-fact reconstruction by the
      implementer is not the gate, and marking it complete is worse than leaving it open because it
      removes the prompt to run it. The artifacts were split into `inventory.md` (inventory only, hashed)
      and `design.md` (target state, options, adopter seam), both left UNSIGNED. See section 12.
      `conformance.md` still carries the ruling-to-`file:line` table and the mutation record; those are
      implementation evidence and stand on their own.
- [x] 10.4 Propagated the review corrections into `proposal.md`: interface file location and why; four
      switches not three; no `GenericJSONPayload` fallback and why it is unreachable; and
      "Fixes by consequence" narrowed to "Removes a barrier, which is not the same as fixing a rule".
- [x] 10.5 THIRD defect on the shipped `architect-editor` rule, found by 10.2 and recorded not fixed:
      beyond being unwired and using invalid `$entity.*` tokens (6.1), its action subject
      `agent.task.$entity.task_id.editor` puts the token MID-TEMPLATE. The substitution grammar
      (`message_substitution.go:54`, `typed_substitution.go:79`) is greedy over dotted paths, so the
      literal `.editor` suffix is swallowed into the path, resolution fails, and the token is left
      verbatim with a loud unresolved-template warning. Reproduced during 10.2 development before the
      test was retargeted at the supported trailing-token shape. Config unchanged.

## 11. Codex re-review round (PR #1052 at head `54995742`)

- [x] 11.1 WITHHELD `AgentResponse.FinishReason` (`agentic/rule_fields.go`). The field carries the
      provider's raw value and TWO SUPPORTED IN-REPO LANES ALREADY DISAGREE about its vocabulary:
      `processor/agentic-model/client.go:790` writes the OpenAI chat vocabulary
      (`stop`/`length`/`tool_calls`) while `processor/agentic-model/client_responses.go:94` writes the
      Responses API status (`completed`/`incomplete`). A rule matching `finish_reason == "length"`
      therefore breaks on a config-only endpoint-mode switch inside this repository, before any
      third-party provider is involved — a stability property of the field, not an observation about
      callers. Nothing is lost: both lanes feed the same switch that produces `Status`, so the
      normalised framework classification is already exposed as `status`, and `status` IS validated
      against a closed set by `AgentResponse.Validate`. No normalisation introduced here (#1056 holds
      that option). Mutation K proves the withhold-list test catches a restored `finish_reason`.
- [x] 11.1a Sharpened the header rule this exposed: classification-shaped is NECESSARY, NOT SUFFICIENT
      — the question is who OWNS the vocabulary. Framework-owned-but-unenforced fields (`outcome`,
      ContextEvent `type`, `result_hint`, `error_kind`) stay exposed; a field whose vocabulary is the
      provider's does not. `finish_reason` is now the worked example for that distinction, as `reason`
      is for the contract-vs-callers one.
- [x] 11.2 CORRECTED the numeric-type guidance in `processor/rule/docs/custom-rules.md`. The guide said
      JSON numbers arrive as `float64` and type-asserted `.(float64)`. That is true only for
      `GenericJSONPayload`, whose map came straight from a decode; a typed projection runs AFTER decode
      and returns real Go types. MEASURED: `LoopCompletedEvent.Iterations` is `int`
      (`agentic/events.go:68`), `rule_fields.go` puts it in the map as `int`, and a scratch test
      printed `iterations int=3`. A custom rule following the old guide is SILENTLY FALSE on
      `iterations` — the exact class this change exists to end. The example now coerces via an
      `asFloat` numeric type switch covering `float32/64`, the signed and unsigned ints, and
      `json.Number` (real here: `types/component.go:79` configures `UseNumber`, and the engine's own
      `expression/evaluator.go:797` handles it). The helper was extracted and compiled standalone to
      verify the published example builds. A third bullet in the interface section states the type rule.
- [x] 11.3 CITED **#1058** and REMOVED THE OVERCLAIM. `TestMessagePathSubstitutesTypedPayloadFieldsIntoActions`
      was described in `conformance.md`, `tasks.md` 10.2 and its own doc comment as exercising "the
      running processor" / "the real Processor". It does not: it hand-assembles a `Processor` struct
      literal, calls the unexported `handleSemanticMessage`, and supplies `newMockKVBucket` and
      `mockPublisher`. All three sites now state what it proves (production decoder, then production
      code from decode through projection, evaluation, substitution and publish) and what it does not
      (component factory, `Initialize`/`Start`, real NATS, real KV). The real-lifecycle gap is #1058.
- [x] 11.4 RE-SYNCED the artifacts to head: `design.md`'s "Identified but not taken" section claimed the
      custom-rule guide was deliberately excluded, which commit `54995742` and 11.2 falsified — it now
      records the reversal rather than the stale exclusion; `conformance.md`'s CI note now names the
      CURRENT failure (`internal/maxdelivery`, NATS 404) as distinct from #1054's drain timeout, with
      both marked unattributable and neither chased; and the trailing blank line at EOF is fixed.
- [x] 11.5 Entry for commit `54995742` (`docs(rule): replace the custom-rules payload hand-wave with
      RuleReadable`), which had no task or conformance record: it rewrote the `ThresholdRule.Evaluate`
      example in `processor/rule/docs/custom-rules.md` to read through `message.RuleReadable` instead of
      an undefined `extractValue` helper, and added a "Reading payloads" section covering
      absence-is-refusal and declared-fields-only. 11.2 corrects that same example's numeric guidance.
- [x] 11.6 SPLIT the combined gate artifact and UN-SIGNED it, per the round-4 blocking item. `inventory.md`
      is new and holds inventory only — baseline `774c85dc`, every figure re-derived with `git show`/
      `git grep` against that commit, and a content hash (`sha256`
      `c65bc53ac2df892d44703cf26e2645fdd8b9c0ab836f42ce7a33a93e0c3ffbf7`) with the recompute command
      in-file so its identity is fixed. `design.md` was rewritten to hold only target state, the five
      options, the adopter-seam inventory and the two deviations, and references the inventory by hash.
      Both carry `Status: UNSIGNED`. `conformance.md` no longer says "Accepted design" and now states
      that the design gate is open. Task 10.3 is demoted to `[~]` and superseded by section 12. (State as of head `6c5865ab`:
      neither token was written except as NOT-GRANTED; §12 records the subsequent grants and the
      round-2 amendment identities.)
- [x] 11.7 PRODUCTION-SEAM half of the round-4 HIGH remediated in-tree (the e2e-TIER half stays #1058):
      `TestIntegration_RuleReadableProjectionProductionLifecycle`
      (`processor/rule/payload_projection_integration_test.go`, `-tags=integration`, testcontainers
      NATS) drives `rule.CreateRuleProcessor` from operator-shaped JSON config through
      `component.AsLifecycleComponent` → `Initialize` → `Start` (real NATS core subscription on the
      declared input port) → production-registry decode → projection → `StatefulEvaluator` OnEnter
      against the real `RULE_STATE` KV bucket → `ActionExecutor` substitution → observable publish on
      a real NATS output subject → bounded-context `Stop`. Asserts the substituted subject token
      (`$message.task_id`) and three substituted properties, absence of withheld `Result`/`Prompt`
      and of unresolved `$message.*` templates, persisted `MatchState` for both lanes (positive
      `entered`/matching, negative not-matching), and that the negative lane fires nothing — proven
      causally (single-connection publish ordering plus the negative's persisted state), not by
      sleeping. Mutation L (revert `ruleFields` to the pre-change generic-only read) makes it fail
      with "rule action output never arrived on the real output subject" while the S2 unreadable
      report fires for `agentic.loop_completed.v1`; `cp`+`md5 -q` backup/restore with `[applied]`
      printed between mutation and test, no git-destructive commands. Suite evidence on this
      pre-#1068 base: isolated run green (1.03s); full
      `go test -race -tags=integration -p 2 ./processor/rule/...` run 1 hit the 10m binary timeout
      with the goroutine dump in an EXISTING test's `CronScheduler.Stop` drain wait (this branch
      predates main's dc25bcc0 lifecycle-drain fixes; not investigated, per owner directive), run 2
      green in 43.0s with 700 passes including this test. Of #1058's four unproven bullets this
      discharges the component factory + `Initialize`/`Start`, real NATS delivery, real KV-backed
      state storage, and observable transport output; the DEPLOYED-FLOW e2e tier assertion remains
      #1058's scope.

- [x] 11.8 ROUND-5 BLOCKING finding fixed: `extractEntityID` no longer derives durable state identity
      from the projection. Baseline measured first (`git show 774c85dc:processor/rule/message_handler.go`):
      generic payloads read `Data["entity_id"]` (string) for state identity, every other payload type
      used the wire message ID. The projection change had silently widened that to any RuleReadable
      payload exposing `entity_id`, so a typed-payload author exposing it for MATCHING would collapse
      distinct messages onto one `RULE_STATE` record — second false→true evaluation becomes
      `TransitionNone`, `on_enter` suppressed. Fix restores the exact baseline split with a seam
      comment naming the two responsibilities (condition visibility + `$message.*` substitution vs
      durable state identity) and why the projection is excluded from the latter; no new typed-payload
      identity mechanism invented (deferred to a separately reviewed contract, per the review).
      `extractMessageData` keeps reading `ruleFields` (substitution data, not identity). Propagation:
      `payload_projection.go` header now lists THREE callers and names `extractEntityID` as
      deliberately not one; conformance S1 grep claim corrected (the fix reintroduces one non-test
      `GenericJSONPayload` assertion, at the identity seam); `proposal.md` four-switches bullet carries
      the round-5 correction; integration-test comment updated. Tests in
      `processor/rule/entity_state_identity_test.go` (see conformance S6): the acceptance test failed
      pre-fix with "OnEnter fired 1 times, want 2", passed post-fix; mutation M (re-couple to
      `ruleFields`) re-fails it; `cp`+`md5 -q` backup/restore with `[applied]` printed between
      mutation and test. Gates: `go test -race ./processor/rule/...` green; isolated
      `TestIntegration_RuleReadableProjectionProductionLifecycle` run green (2.2s); full integration
      suite deferred to the post-landing run per owner directive. No existing test had pinned the
      generic `entity_id` state-identity behavior (verified by grep); it is now pinned by
      `TestGenericPayloadEntityIDStateIdentityUnchanged` and `TestExtractEntityIDSeam`.

## 12. Exported-surface design gate — CLOSED 2026-08-24

- [x] 12.1 `INVENTORY PASS` — GRANTED 2026-08-24 by an independent reviewer session under
      `.agents/contracts/semstreams-reviewer.md`, after full re-derivation of the census against
      baseline `774c85dc`. Initial pass at commit `6c5865ab` (identity `c65bc53a…`) returned two
      MEDIUM corrections; the delta round REFUSED transfer after catching a reviewer-introduced
      census figure (54→53) and a self-inconsistent Identity block; round-2 amendments were
      re-derived and the verdict RE-AFFIRMED bound to identity sha256
      `20efdcbb8d50757d3b88971bfa0a1a82962ec18616e42d6a8ba232b5f8b18d67` (round 2). Round 3
      (2026-08-24, triggered by the Codex header-contradiction MEDIUM at head `4d904585`): headers
      reworded to locate grant-state solely in this record; body byte-identical; RE-AFFIRMED bound to
      identity sha256 `3e86e3e38e9bf3c4421c3b8033d2e05b2690f5c7f2a436238f071ea37e0918dd`
      (recompute command in-file). Round-2 binding is history honored at `4d904585`.
- [x] 12.2 `DESIGN REVIEW PASS` — GRANTED 2026-08-24 by the same independent reviewer, verified
      design↔code conformance at head, the adopter do-nothing path, and hash linkage; RE-AFFIRMED
      after the round-2 amendments, bound to `design.md` whole-file sha256
      `0ffbb691b80f6b40d43db4e9014b7811f79a56fc6190cbbd6624e35248fe2972` (round 2); round 3
      RE-AFFIRMED on the headers-only delta, bound to whole-file sha256
      `e2ca2fe35d02032012afbf5d78b33db31617d52edd029da12fb1a58b4650f2ce`.
- [x] 12.3 OWNER ACCEPTANCE — GRANTED 2026-08-24 by the owner, explicitly and interactively, bound
      to the two identities above: the three design decisions plus the two refined structural tests
      (validator/closed-type constraint; framework-owned vocabulary) under which `FinishReason` and
      `LoopFailedEvent.Reason` remain withheld — and CARRIED FORWARD explicitly by the owner on
      2026-08-24 to the round-3 identities (`3e86e3e3…` / `e2ca2fe3…`; headers-only delta, body
      byte-identical to the accepted round-2 content). Recorded here and in the PR #1052 gate comment. The
      reviewed artifacts are byte-exact as reviewed: any future edit to either voids the bindings and
      re-runs the sequence (recompute both hashes at any commit that touches them).
