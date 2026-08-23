# Design gate: `message.RuleReadable` exported surface

Status: owner-decided before implementation; materialized here after the fact. This artifact does not
re-open the decision — it gives it an identity, a baseline, and a reviewable inventory, which the change
previously carried only as `proposal.md` prose.

Baseline: `774c85dc` (branch base). Every count and `file:line` below was re-derived against that commit
with `git grep <pattern> 774c85dc -- <path>`, not carried forward from the original scoping pass.

Why a gate at all: `message` is a framework package, and `RuleReadable` is reachable by payload authors
outside this repository. New exported surface there takes owner design review before implementation.

## Repository-first surface inventory

**Does an owner for this responsibility already exist?** Measured at baseline, `message/` declares 18
interfaces. Ten are the optional behavior family this one joins:

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

The remaining eight (`ContentStorable`, `BinaryStorable`, `FederationMeta`, `Message`, `Meta`, `Payload`,
`Storable`, `TripleGenerator`) are structural or semantic contracts, not optional capabilities.

**No owner exists.** `git grep "RuleReadable\|RuleFields" 774c85dc` returns nothing. The responsibility
— "what may a rule read from this payload" — was not represented by any type; it was hard-coded as a
concrete-type assertion at four sites:

| Site | `file:line` at baseline |
|---|---|
| `ExpressionRule.Evaluate` | `processor/rule/expression_factory.go:130` |
| `extractEntityID` | `processor/rule/message_handler.go:412` |
| `extractMessageData` | `processor/rule/message_handler.go:444` |
| `TestRule.Evaluate` | `processor/rule/test_rule_factory.go:66` |

That enumeration is complete for the rule lane: `git grep "\.Payload()" 774c85dc -- processor/rule/`
returns exactly those four production reads plus one prose mention in
`processor/rule/docs/custom-rules.md:176`.

## Adopter-seam inventory

The adopter: a developer outside this repository who owns a payload type and wants a rule to fire on it.

**What must they know?** That rule-readability is opt-in, and that implementing `RuleFields()` is the opt.
Nothing else — no registration call, no config key, no ordering requirement.

**What happens if they do nothing?** Before this change: the rule silently never fired, indistinguishable
from a condition that evaluated false. After: the same non-firing, plus a bounded `WARN` naming their rule,
their payload type, and the remedy. The default is unchanged; only its observability is.

**Where do they find out?** The catalogue, not the source file — `message/doc.go` `## Rule Interfaces`,
`docs/basics/03-graphable-interface.md`, and the interface doc comment. (Review round 2 finding: the first
pass had the interface documented only at its declaration, which is not where an adopter looks.)

**What SHOULD they have to know — ideally nothing?** Not achievable here, and deliberately so. The one
design that requires zero adopter knowledge is a reflective default, and it is rejected below precisely
because "nothing to know" would mean "every struct field is now rule-visible without its author's consent".
This is the case where the adopter MUST act, because the act is the consent.

**Are we asking the caller to predict something the framework could observe?** No. `RuleFields()` reports
what the payload holds at the moment it is asked; there is no size limit, subject, deadline, or readiness
state for the adopter to compute in advance.

## Options considered

**A. Do nothing.** Rules keep reading only `core.json.v1`. Rejected: the shipped
`configs/rules/agentic-workflow/architect-editor.json` is `enabled: true` and has never been able to fire,
and the framework was already paying the workaround visibly — `f8a798f5` wrapped a typed governance intent
in `GenericJSONPayload` and `verdictPayloadFromMap` converts it back, a typed→untyped→typed round trip
whose only purpose is surviving rule evaluation. Do-nothing preserves a silent failure mode that has
already cost a shipped feature.

**B. Reuse an existing surface.** `Measurable` (`message/behaviors.go:79`) is the only baseline behavior
returning `map[string]any`, so it is the only reuse candidate. Rejected: its map is *measurements with
units* — it is paired with `Unit(measurement string) string`, and its documented domain is sensor and
weather-station readings. Overloading it to mean "fields a rule may match on" would give one interface two
unrelated meanings and force every rule-readable payload to answer a unit question it has no unit for.
A second spelling of an existing fact is wrong at birth; so is one spelling of two different facts.

**C. Reflective default.** Derive the map from struct tags, so every payload is rule-readable with no
adopter action. Rejected on two independent grounds: reflection is the wrong trade in the evaluation hot
path, and — decisively — it would expose fields the author never intended a rule to see. The concrete case
is live: `Violation.OriginalContent` carries up to 500 characters of raw user text, and `AgentRequest`
carries the entire prompt. A default that leaks by omission is worse than an opt-in that is skipped.

**D. Engine-side adapter table.** Keep the knowledge in `processor/rule` — a registry mapping payload type
to extractor. Rejected: it puts the projection decision in the engine rather than with the payload's
author, so every adopter payload still requires a framework PR, which is the exact cost this change exists
to remove. It also re-creates the fifth copy of the thing the change is consolidating.

**E. Explicit `RuleReadable` interface. CHOSEN.** The payload declares its own projection; the engine
asserts and asks. Joins a ten-member family with an established discovery idiom, costs an adopter one
method, and keeps the content decision with the only party who knows the payload's semantics.

## Owner decisions accepted before implementation

1. Explicit projection, never reflective. No `reflect`, no marshal round trip to build the map.
2. All 15 framework-owned agentic payloads implement it now, not a lazy subset — an adopter cannot add a
   method to a framework type, so every one skipped is a framework PR they must wait on.
3. Structural facts only; authored and user content withheld (ADR-036). Where the call is not obvious,
   the omission is recorded in a comment so a future reader can tell it from an oversight.

## Deviations raised during implementation and accepted at review

| Deviation | Cause | Disposition |
|---|---|---|
| Interface lives in `message/rule_readable.go`, not `behaviors.go` | revive `max-public-structs` caps a FILE at 10 (`revive.toml:55-59`); `behaviors.go` was at exactly 10 and `message/` is not in the exclude list | ACCEPTED — reviewer reproduced by appending an 11th interface and running the pinned linter |
| Helper asserts `RuleReadable` only; no `*GenericJSONPayload` fallback | `GenericJSONPayload` implements the interface, and only `*GenericJSONPayload` satisfies `message.Payload`, so the fallback is unreachable | ACCEPTED — reviewer attacked unreachability four ways; a hypothetical carrier satisfying `Payload` but not `RuleReadable` would not be `*GenericJSONPayload` either |

## Surface added

`message.RuleReadable` with a single method `RuleFields() map[string]any`.

`agentic.ToolResult.EffectiveErrorKind() ToolErrorKind` was added mid-flight at review direction; it
re-enters the gate here because scope is what shipped, not what was planned. It is on `agentic/`, not a
package requiring owner design review, and it consolidates an existing normalisation rather than
introducing a new concept.

## Identified but not taken

`processor/rule/docs/custom-rules.md:176-179` is the adopter-facing guide for writing a custom `Rule`
implementation. Its worked example calls an `extractValue(payload, field)` helper that the document never
defines — the payload-reading step is a hand-wave, which is precisely the gap `RuleReadable` fills. Adding
a pointer there is a two-line change with real adopter value, and it is deliberately NOT in this change:
custom `Rule` implementations are a different surface from payload projection. Recorded for the owner.
