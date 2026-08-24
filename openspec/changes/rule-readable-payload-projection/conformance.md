# Implementation conformance: rule-readable payload projection

Status: independent review round 1 returned `APPROVE` with four required fixes (round 2, all applied);
Codex review of PR #1052 returned `REQUEST CHANGES` with four items (round 3, all applied); Codex
re-review at head `54995742` returned four further items (round 4, all applied). Merge and issue closure
have not occurred.

THE DESIGN GATE IS CLOSED (2026-08-24). An independent reviewer session granted `INVENTORY PASS`
(inventory identity sha256 `20efdcbb8d50757d3b88971bfa0a1a82962ec18616e42d6a8ba232b5f8b18d67`, baseline
`774c85dc`) and `DESIGN REVIEW PASS` (design.md whole-file sha256
`0ffbb691b80f6b40d43db4e9014b7811f79a56fc6190cbbd6624e35248fe2972`) across two delta rounds — the first
delta REFUSED re-affirmation over a census figure and a self-inconsistent identity block, both corrected
and re-verified. OWNER ACCEPTANCE was granted explicitly on 2026-08-24, bound to those identities.
The full record is tasks.md §12; the reviewed artifacts remain byte-exact as reviewed. This file maps
rulings to implementation evidence; the gate record above is what certifies they were gated.

Baseline: `774c85dc`. Accepted design: `design.md`, referencing `inventory.md` at sha256
`20efdcbb8d50757d3b88971bfa0a1a82962ec18616e42d6a8ba232b5f8b18d67`.

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
| S1 — one projection implementation in the engine | Exactly one home interprets a payload on the rule lane. | `ruleFields` at `processor/rule/payload_projection.go:40`; its three callers are `expression_factory.go:172`, `message_handler.go:456` (`extractMessageData`), `test_rule_factory.go:69` (`TestRule`). `extractEntityID` was a fourth caller until the round-5 finding (S6): it now reads the generic payload's legacy `entity_id` directly, because it answers durable state IDENTITY, not field visibility — the one non-test `payload.(*message.GenericJSONPayload)` assertion outside the explanatory comment, documented at both seams. | CONFORMS |
| S2 — unreadable is observable, bounded per rule/type | The engine surfaces the pairing once per rule and payload type, never per message. | `ExpressionRule.reportUnreadablePayload` at `processor/rule/expression_factory.go:62`, bounded by the per-rule `sync.Map` at `:52` keyed on the wire type. `TestExpressionRuleReportsUnreadablePayloadOncePerPairing` asserts the report identifies rule and type, that a repeat of 50 adds zero reports, and that a neighbour rule evaluates independently. | CONFORMS |
| S3 — evaluation semantics unchanged | The rule does not fire, the engine does not halt, other rules are unaffected, and a legitimately-false condition produces no new signal. | The unreadable branch still returns `false`. `TestExpressionRuleLegitimateFalseProducesNoSignal` covers the quiet path; the neighbour assertion in the report test covers isolation. `len(r.conditions) == 0` was moved ahead of projection so a rule that cannot fire never reports. | CONFORMS |
| S4 — generic payloads keep working | Every rule valid before the change is valid after it. | `GenericJSONPayload.RuleFields()` returns `Data` (`message/generic_json.go:127`). `TestExpressionRuleGenericJSONPayloadUnchanged` covers match and non-match. Mutation B (swap the helper back to the pre-change generic-only assertion) leaves these green while failing only the typed-payload tests. | CONFORMS |
| S5 — declared values reach action substitution, not only conditions | A projected field resolves in the rule's action templates, proven through the production lifecycle. | Two tests. `TestMessagePathSubstitutesTypedPayloadFieldsIntoActions` (unit) proves the seam below the lifecycle: production decoder, hand-assembled `Processor`, mock KV/publisher, mutations I/J. `TestIntegration_RuleReadableProjectionProductionLifecycle` (`processor/rule/payload_projection_integration_test.go`, `-tags=integration`, testcontainers NATS) proves the PRODUCTION lifecycle: `rule.CreateRuleProcessor` from operator-shaped JSON config, `Initialize`/`Start`, a real NATS core subscription on the declared input port, wire bytes built by `BaseMessage.MarshalJSON` and decoded by the factory-wired production-registry decoder, `StatefulEvaluator` OnEnter against the real `RULE_STATE` KV bucket (persisted `MatchState` asserted for both lanes), `$message.*` substitution observed on a real NATS output subscription (subject token + three properties), withheld `Result`/`Prompt` absent from the published wire, and a negative-lane payload proven evaluated-and-not-fired by single-connection publish ordering plus its persisted `IsMatching=false` state — causal synchronization, no bare sleeps. NOT covered, and NOT claimed: the deployed Docker e2e TIER stage — that remains **#1058**'s scope. | CONFORMS (e2e tier: #1058) |
| S6 — projection is excluded from durable state identity (round-5 BLOCKING finding) | The projection contract (condition visibility + `$message.*` substitution) must not also control the RULE_STATE key; a typed payload exposing `entity_id` for matching must not collapse distinct messages onto one durable state record. | `extractEntityID` (`processor/rule/message_handler.go:430`) restored to the exact pre-projection baseline split (`git show 774c85dc:processor/rule/message_handler.go`): `*message.GenericJSONPayload` keeps its legacy `Data["entity_id"]` state identity; every other payload uses the wire message ID whatever its projection exposes; no new typed-payload identity mechanism invented (separately reviewed contract, per the review). Seam comment states the two responsibilities. Tests (`processor/rule/entity_state_identity_test.go`): `TestTypedPayloadProjectionEntityIDDoesNotControlStateIdentity` — two distinct wire messages exposing the SAME projected `entity_id`, condition matches on that very field, OnEnter fires twice, two `RULE_STATE` records keyed by wire message ID, none keyed by the projected value (failed pre-fix: "OnEnter fired 1 times, want 2"); `TestGenericPayloadEntityIDStateIdentityUnchanged` — generic lane still collapses by design (1 fire, record keyed by `entity_id`); `TestExtractEntityIDSeam` — string/absent/non-string generic triple + typed exclusion. No pre-existing test pinned this seam; verified by grep before writing them. | CONFORMS |

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
| M | re-couple `extractEntityID` to `ruleFields` (restore the round-5 defect) | `TestTypedPayloadProjectionEntityIDDoesNotControlStateIdentity` — "OnEnter fired 1 times, want 2 — distinct messages sharing a projected entity_id collapsed onto one durable state record"; `TestExtractEntityIDSeam/typed_projection...` — projected value returned instead of the wire message ID |

(K — the restored-`finish_reason` mutation — is cited at `tasks.md` 11.1 from the round-4 remediation but
was never entered in this table; it is not re-run or reconstructed here.)
