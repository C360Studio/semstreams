# Implementation conformance: rule-readable payload projection

Status: independent review round 1 returned `APPROVE` with four required fixes (round 2, all applied);
Codex review of PR #1052 returned `REQUEST CHANGES` with four items (round 3, all applied); Codex
re-review at head `54995742` returned four further items (round 4, all applied). Merge and issue closure
have not occurred.

THE DESIGN GATE IS OPEN. `inventory.md` and `design.md` in this directory are UNSIGNED: neither
`INVENTORY PASS` nor `DESIGN REVIEW PASS` has been issued, and no owner acceptance is recorded. This file
maps rulings to implementation evidence; it does not certify that the rulings were gated. A conformance
table over an ungated design is exactly as strong as the design behind it, and no stronger.

Baseline: `774c85dc`. Design under review (unsigned): `design.md`, referencing `inventory.md` at sha256
`c65bc53ac2df892d44703cf26e2645fdd8b9c0ab836f42ce7a33a93e0c3ffbf7`.

CI note, current as of head `54995742`: the `Test` job is red in `internal/maxdelivery`
(`TestThreeNodeClusterReplicasOneRetainsAndHandlesOccurrenceOnce`, a NATS 404 stream-not-found). This is a
DIFFERENT failure from the one an earlier round of this artifact recorded — that was the 30s drain timeout
in `graph-clustering` / `graph-embedding`, tracked as **#1054**, which reproduced on `main` in four of
eight consecutive runs.

Neither is attributable to this change. It touches none of those three packages, and the local integration
suite below runs ALL THREE to green on this exact tree — `internal/maxdelivery` 11.4s,
`processor/graph-clustering` 33.8s, `processor/graph-embedding` 17.6s. The branch base was independently
red before this branch ran. No re-run-to-green was attempted and no test in those packages was modified;
a locally-green package that is red in CI is evidence of environment-dependent flake, not of a fix.

## Ruling-to-implementation map

| Ruling | Binding result | Final evidence | Result |
|---|---|---|---|
| D1 — explicit projection, never reflective | The payload declares its own field map; no `reflect`, no marshal/unmarshal round trip to build it. | `message/rule_readable.go:59` declares the single-method interface. `grep -c reflect` over the projection path returns 0 in `processor/rule/payload_projection.go` and 1 each in `message/rule_readable.go` / `agentic/rule_fields.go` — both prose, in the comment explaining the rejection. No `reflect` import exists in any of the three. | CONFORMS |
| D2 — all 15 framework agentic payloads, not a subset | Every payload registered by `agentic.RegisterPayloads` implements the interface. | `agentic/rule_fields.go` declares 15 `RuleFields` methods. `TestEveryRegisteredAgenticPayloadIsRuleReadable` enumerates from `RegisterPayloads` — the owning registration, not a hand-written list — and fails on any member that does not implement it. | CONFORMS |
| D3 — structural facts only; content withheld (ADR-036) | Prompts, message text, model output and tool-result bodies do not reach `RuleFields()`; non-obvious omissions carry a comment. | Every projection in `agentic/rule_fields.go` carries a `Withheld:` line naming each omitted field and why. `TestAgenticRuleFieldsWithholdContent` pins all 15 as assertions; `TestProjectionTableCoversEveryRegisteredPayload` keeps that table honest against the registry. | CONFORMS |
| D3a — the contract test, not the caller census (round 3) | A field is structural only if a validator or closed type constrains it — not if today's callers happen to pass literals. | `LoopFailedEvent.Reason` is WITHHELD: `Validate` (`agentic/events.go`) constrains `LoopID`/`TaskID` only, and exported `BuildFailureEvent`/`BuildFailureMessages` (`processor/agentic-loop/handlers.go`) take it as a free string. Rule and rationale are stated at the file header and at the method so the enumeration is not re-run to the same wrong conclusion. Audit of every other exposed field recorded in the handoff: `reason` is the only explanation-shaped unconstrained field in the exposed set. | CONFORMS |
| S1 — one projection implementation in the engine | Exactly one home interprets a payload on the rule lane. | `ruleFields` at `processor/rule/payload_projection.go:34`; its four callers are `expression_factory.go:172`, `message_handler.go:414` (`extractEntityID`), `message_handler.go:438` (`extractMessageData`), `test_rule_factory.go:69` (`TestRule`). `git grep "GenericJSONPayload)" -- processor/rule/` on non-test files returns only the explanatory comment at `payload_projection.go:17`. | CONFORMS |
| S2 — unreadable is observable, bounded per rule/type | The engine surfaces the pairing once per rule and payload type, never per message. | `ExpressionRule.reportUnreadablePayload` at `processor/rule/expression_factory.go:62`, bounded by the per-rule `sync.Map` at `:52` keyed on the wire type. `TestExpressionRuleReportsUnreadablePayloadOncePerPairing` asserts the report identifies rule and type, that a repeat of 50 adds zero reports, and that a neighbour rule evaluates independently. | CONFORMS |
| S3 — evaluation semantics unchanged | The rule does not fire, the engine does not halt, other rules are unaffected, and a legitimately-false condition produces no new signal. | The unreadable branch still returns `false`. `TestExpressionRuleLegitimateFalseProducesNoSignal` covers the quiet path; the neighbour assertion in the report test covers isolation. `len(r.conditions) == 0` was moved ahead of projection so a rule that cannot fire never reports. | CONFORMS |
| S4 — generic payloads keep working | Every rule valid before the change is valid after it. | `GenericJSONPayload.RuleFields()` returns `Data` (`message/generic_json.go:127`). `TestExpressionRuleGenericJSONPayloadUnchanged` covers match and non-match. Mutation B (swap the helper back to the pre-change generic-only assertion) leaves these green while failing only the typed-payload tests. | CONFORMS |
| S5 — declared values reach action substitution, not only conditions | A projected field resolves in the rule's action templates, proven through the production lifecycle. | Two tests. `TestMessagePathSubstitutesTypedPayloadFieldsIntoActions` (unit) proves the seam below the lifecycle: production decoder, hand-assembled `Processor`, mock KV/publisher, mutations I/J. `TestIntegration_RuleReadableProjectionProductionLifecycle` (`processor/rule/payload_projection_integration_test.go`, `-tags=integration`, testcontainers NATS) proves the PRODUCTION lifecycle: `rule.CreateRuleProcessor` from operator-shaped JSON config, `Initialize`/`Start`, a real NATS core subscription on the declared input port, wire bytes built by `BaseMessage.MarshalJSON` and decoded by the factory-wired production-registry decoder, `StatefulEvaluator` OnEnter against the real `RULE_STATE` KV bucket (persisted `MatchState` asserted for both lanes), `$message.*` substitution observed on a real NATS output subscription (subject token + three properties), withheld `Result`/`Prompt` absent from the published wire, and a negative-lane payload proven evaluated-and-not-fired by single-connection publish ordering plus its persisted `IsMatching=false` state — causal synchronization, no bare sleeps. NOT covered, and NOT claimed: the deployed Docker e2e TIER stage — that remains **#1058**'s scope. | CONFORMS (e2e tier: #1058) |

No `DEVIATION` is recorded. Two design deviations were raised, escalated, and accepted at review; both are
recorded in `design.md` rather than here because they changed the design, not the ruling.

## Mutation record

Each used a `cp` backup with `md5 -q` restoration proof and printed `[applied]` between mutating and
testing. No `git stash` at any point — the stack held 5 unrelated entries at start and end.

| # | Mutation | Detected by |
|---|---|---|
| A | delete the `RuleReadable` branch from `ruleFields` | 8 tests, including the two pre-existing `TestExpressionRuleEvaluation` / `Cooldown` |
| B | swap that branch for the pre-change generic-only assertion | exactly the 4 typed-payload tests; generic tests stay green |
| C | drop the `role` key from `LoopCompletedEvent.RuleFields` | shipped-config test + 2 others |
| D | remove the once-per-pairing `LoadOrStore` guard | "pairing reported 100 more times on repeat; want 0" |
| E | invent a projection key with no wire field | `TestRuleFieldsMirrorWireNames` |
| F | emit a non-`omitempty` wire field conditionally | `TestZeroPayloadProjectionCoversMandatoryWireFields` |
| G | remove the Unknown default from `EffectiveErrorKind` | the PROJECTION test — proving the projection derives rather than copying |
| H | restore `reason` to the `LoopFailedEvent` projection | `TestAgenticRuleFieldsWithholdContent/LoopFailedEvent` |
| I | revert the helper to generic-only, against the action-substitution seam | `TestMessagePathSubstitutesTypedPayloadFieldsIntoActions` — 0 published, want 1 |
| J | drop `MessageData` from the stateful-evaluation call | same test — subject and all three body values left as unresolved templates |
| L | swap the `ruleFields` branch for the pre-change generic-only read, against the production lifecycle | `TestIntegration_RuleReadableProjectionProductionLifecycle` — no output ever arrived on the real output subject; the S2 unreadable report fired for `agentic.loop_completed.v1` |

(K — the restored-`finish_reason` mutation — is cited at `tasks.md` 11.1 from the round-4 remediation but
was never entered in this table; it is not re-run or reconstructed here.)
