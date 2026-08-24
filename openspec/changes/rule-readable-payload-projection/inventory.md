# Surface inventory: rule-readable payload projection

Status: this artifact does not self-certify. `INVENTORY PASS` is issued by an independent reviewer, and
owner acceptance is a distinct, separate gate; whether either has been granted — and to which exact
identity — is recorded ONLY in the bound gate record (`tasks.md` §12), keyed to the content hash printed
in this file's Identity section. Nothing in this file's body asserts or implies a verdict.

Baseline: `774c85dc`. Every figure below was re-derived against that commit with `git show <rev>:<path>` or
`git grep <pattern> 774c85dc -- <path>`. Nothing is carried forward from an earlier scoping pass.

Scope: this file records WHAT EXISTS at the baseline on the surface the change touches. It contains no
target state, no options, no recommendation, and no acceptance. Those live in `design.md`, which references
this file by the hash printed at the end.

## Interfaces declared in `message/`

`git grep "^type .* interface {" 774c85dc -- message/` returns 18. Ten form the optional behavior family
that is discovered by type assertion:

| Interface | `file:line` at baseline | Returns |
|---|---|---|
| `Locatable` | `message/behaviors.go:23` | two floats |
| `Timeable` | `message/behaviors.go:34` | `time.Time` |
| `Observable` | `message/behaviors.go:44` | four scalars |
| `Correlatable` | `message/behaviors.go:66` | string |
| `Measurable` | `message/behaviors.go:79` | `map[string]any` + unit lookup |
| `Deployable` | `message/behaviors.go:91` | string |
| `Processable` | `message/behaviors.go:99` | int + `time.Time` |
| `Traceable` | `message/behaviors.go:112` | three strings |
| `Expirable` | `message/behaviors.go:127` | `time.Time` + `time.Duration` |
| `IndexingProfiler` | `message/behaviors.go:152` | string |

The other eight are structural or semantic contracts rather than optional capabilities:
`ContentStorable` (`message/content_storable.go:44`), `BinaryStorable` (`:134`), `FederationMeta`
(`message/federation.go:22`), `Message` (`message/message.go:20`), `Meta` (`message/meta.go:13`),
`Payload` (`message/payload.go:50`), `Storable` (`message/storable.go:64`), `TripleGenerator`
(`message/triple.go:123`).

Exactly one of the ten returns a map: `Measurable`, whose map is measurements paired with
`Unit(measurement string) string`.

## Existing owner for "what may a rule read from this payload"

`git grep "RuleReadable\|RuleFields" 774c85dc` returns no matches. No type represents the responsibility at
the baseline.

The behaviour is instead hard-coded as a concrete-type assertion at four sites:

| Site | `file:line` at baseline |
|---|---|
| `ExpressionRule.Evaluate` | `processor/rule/expression_factory.go:130` |
| `extractEntityID` | `processor/rule/message_handler.go:412` |
| `extractMessageData` | `processor/rule/message_handler.go:444` |
| `TestRule.Evaluate` | `processor/rule/test_rule_factory.go:66` |

The enumeration is complete for the rule lane: `git grep "\.Payload()" 774c85dc -- processor/rule/`
returns 53 hits: the four production (non-test) reads above, 48 hits in `*_test.go` files, and one prose
mention at `processor/rule/docs/custom-rules.md:176`.

## Payload registry census

`payloadbuiltins.Register` (`payloadbuiltins/register.go` at baseline) aggregates six registrars:
`message`, `agentic`, `agenticdispatch`, `gateddagexec`, `objectstore`, `governance`.

`agentic.RegisterPayloads` registers 15 payload types
(`git show 774c85dc:agentic/payload_registry.go | grep -c "Domain: Domain, Category:"`). At the baseline
the six registrars total 21 types (1+15+1+2+1+1). One further first-party family exists OUTSIDE
payloadbuiltins: the capability-gated `agentic/research` registrar adds 6 types, wired via
`graphresearch.RegisterPayloads` in both framework binaries (`cmd/semstreams/main.go:767`,
`cmd/e2e-semstreams/main.go:373`); it is rule-lane-reachable when that capability is selected and is
recorded HERE as a deliberate exclusion from the projection set (`tasks.md` 8.5 records the non-agentic
registrar exclusions).

## Existing consumers affected

`configs/rules/agentic-workflow/architect-editor.json` at baseline: `id=architect_complete_spawn_editor`,
`enabled=true`, conditions on `$message.role` and `$message.outcome`. Those names match
`LoopCompletedEvent.Role` and `.Outcome` (`agentic/events.go`), and `agent.complete.*` carries
`LoopCompletedEvent`, a registered typed payload.

The framework's existing workaround for the same constraint is visible in two places:
`payloadToBaseMessageBytes` (`processor/agentic-loop/governance_dispatcher.go`) marshals a typed
`ProposedToolCallPayload` into a `map[string]any`, and `verdictPayloadFromMap`
(`processor/agentic-loop/component.go`) converts it back.

## Content-exposure surfaces present at baseline

Fields a projection would have to make an explicit decision about, recorded because they bound the risk:
`AgentRequest.Messages` (full prompt), `AgentResponse.Message` (model output), `ToolResult.Content` (result
body), `ToolCall.Arguments` (model-authored), `UserMessage.Content` (user text), and the open
caller-populated `Metadata` maps on the loop events, `TaskMessage`, `ToolCall` and `ToolResult`.

## Identity

Content hash of this file, excluding this Identity section, over the exact bytes above:

    sha256 = 3e86e3e38e9bf3c4421c3b8033d2e05b2690f5c7f2a436238f071ea37e0918dd

Recompute with:

    sed '/^## Identity$/,$d' inventory.md | shasum -a 256
