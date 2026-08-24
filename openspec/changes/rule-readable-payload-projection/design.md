# Design: `message.RuleReadable` exported surface

Status: **UNSIGNED.** `DESIGN REVIEW PASS` is owner-issued and has not been granted. No owner acceptance is
recorded in this file.

What this artifact is, stated plainly because the distinction matters: the three decisions in
"Decisions requiring a ruling" were made by the owner before implementation, and the implementation
conforms to them (`conformance.md`). But this document is an IMPLEMENTER'S RECONSTRUCTION written after the
code, not the checkpoint that produced those decisions. A reconstruction is not the gate. It is offered so
the gate can be run against something reviewable, and it is left unsigned so that running it remains
necessary.

Inventory: `inventory.md` in this directory, baseline `774c85dc`,
sha256 `20efdcbb8d50757d3b88971bfa0a1a82962ec18616e42d6a8ba232b5f8b18d67`
(recompute: `sed '/^## Identity$/,$d' inventory.md | shasum -a 256`).

Why a gate at all: `message` is a framework package and `RuleReadable` is reachable by payload authors
outside this repository. New exported surface there takes owner design review before implementation.

## Problem

Per `inventory.md`: at baseline no type represents "what may a rule read from this payload"; the rule lane
instead asserts a single concrete type at four sites. A payload that is correctly registered, correctly
enveloped and correctly decoded is discarded at evaluation because it is not `*GenericJSONPayload`, and the
discard is indistinguishable from a condition that evaluated false.

The consequences are not hypothetical. A shipped, `enabled: true` rule conditions on fields that match
`LoopCompletedEvent` exactly and has never been able to fire, and the framework already carries a
typed → untyped → typed round trip whose only purpose is surviving rule evaluation.

## Target state

An optional behavior interface in the ten-member family, implemented by the payload:

```go
type RuleReadable interface {
    RuleFields() map[string]any
}
```

The engine asserts it at one place; the payload declares its own projection. The four baseline assertion
sites collapse into that one helper. A payload implementing neither the interface nor the generic surface
is REPORTED — bounded per rule and payload type — rather than evaluating false in silence.

## Options considered

**A. Do nothing.** Rules keep reading only `core.json.v1`. Rejected: it preserves a silent failure mode that
has already cost a shipped feature, and it leaves the framework paying a visible workaround (the typed →
untyped → typed round trip) whose cost grows with every typed payload added.

**B. Reuse an existing surface.** `Measurable` (`message/behaviors.go:79`) is the only baseline behavior
returning `map[string]any`, so it is the only reuse candidate. Rejected: its map is *measurements with
units* — it is paired with `Unit(measurement string) string` and its documented domain is sensor and
weather-station readings. Overloading it would give one interface two unrelated meanings and force every
rule-readable payload to answer a unit question it has no unit for. One spelling of two different facts is
as wrong as two spellings of one.

**C. Reflective default.** Derive the map from struct tags so every payload is rule-readable with no adopter
action. Rejected on two independent grounds: reflection is the wrong trade in the evaluation hot path, and —
decisively — it would expose fields the author never intended a rule to see. The risk is concrete at the
baseline: `AgentRequest` carries the entire prompt and `AgentResponse` the model output (see
`inventory.md`, content-exposure surfaces). A default that leaks by omission is worse than an opt-in that
is skipped, because the skip is visible and the leak is not.

**D. Engine-side adapter table.** Keep the knowledge in `processor/rule` as a registry mapping payload type
to extractor. Rejected: it puts the projection decision in the engine rather than with the payload's author,
so every adopter payload still requires a framework PR — the exact cost this change exists to remove — and
it re-creates a fifth copy of the thing being consolidated.

**E. Explicit `RuleReadable` interface. PROPOSED.** The payload declares its projection; the engine asserts
and asks. Joins a ten-member family with an established discovery idiom, costs an adopter one method, and
keeps the content decision with the only party who knows the payload's semantics.

## Adopter-seam inventory

The adopter: a developer outside this repository who owns a payload type and wants a rule to fire on it.

**What must they know?** That rule-readability is opt-in, and that implementing `RuleFields()` is the opt.
Nothing else — no registration call, no config key, no ordering requirement.

**What happens if they do nothing?** Before: the rule silently never fires, indistinguishable from a
condition that evaluated false. After: the same non-firing, plus a bounded warning naming their rule, their
payload type, and the remedy. The default behaviour is unchanged; only its observability is.

**Where do they find out?** The catalogue, not the source file — `message/doc.go`,
`docs/basics/03-graphable-interface.md`, and the rule-authoring guide at
`processor/rule/docs/custom-rules.md`. (An implementation review round found the first pass had documented
the interface only at its declaration, which is not where an adopter looks.)

**What SHOULD they have to know — ideally nothing?** Not achievable here, deliberately. The only design
requiring zero adopter knowledge is option C, and it is rejected precisely because "nothing to know" would
mean "every struct field is rule-visible without its author's consent". This is the case where the adopter
MUST act, because the act IS the consent.

**Are we asking the caller to predict something the framework could observe?** No. `RuleFields()` reports
what the payload holds when asked; there is no size limit, subject, bucket, deadline or readiness state for
the adopter to compute in advance.

**What does the surface cost them if they implement it wrongly?** Two sharp edges the implementation
surfaced and now documents: values keep the projection's Go types (a typed payload yields `int` where
`GenericJSONPayload` yields post-JSON `float64`), and a projection that exposes an unstable or
externally-owned vocabulary hands rules a value that changes under configuration.

## Decisions requiring a ruling

Recorded as the decisions the owner made before implementation. Their presence here is a record for review,
NOT an acceptance — see Status.

1. Explicit projection, never reflective. No `reflect`, no marshal round trip to build the map.
2. All 15 framework-owned agentic payloads implement it now, not a lazy subset — an adopter cannot add a
   method to a framework type, so every one skipped is a framework PR they must wait on. The remaining
   first-party types outside this set are deliberate exclusions — the five non-agentic registrar types
   (recorded in `tasks.md` 8.5) and the capability-gated `agentic/research` family (recorded in the
   inventory's registry census). Decision 2's own rationale applies to them, and each lands loud
   (once-per-pairing unreadable report), not silent.
3. Structural facts only; authored and user content withheld (ADR-036). Where the call is not obvious, the
   omission is recorded in a comment so a future reader can tell it from an oversight.

Implementation experience refined decision 3 into two tests that were not stated up front and are offered
back for the ruling: a field is structural only if a VALIDATOR OR CLOSED TYPE constrains it (not if today's
callers happen to pass literals), and a classification field additionally requires that the FRAMEWORK OWN
its vocabulary. `LoopFailedEvent.Reason` and `AgentResponse.FinishReason` are withheld under those two
tests respectively.

## Deviations from the proposed design

Both were raised during implementation, escalated rather than executed, and accepted by IMPLEMENTATION
REVIEW. That is a different gate from this one; neither is design-gate acceptance.

| Deviation | Cause | Disposition |
|---|---|---|
| Interface ships in `message/rule_readable.go`, not `message/behaviors.go` | revive `max-public-structs` caps a FILE at 10 (`revive.toml:55-59`); `behaviors.go` was at exactly 10 and `message/` is not in the exclude list | Accepted by implementation review, which reproduced the constraint by appending an 11th interface and running the pinned linter |
| Helper asserts `RuleReadable` only; no separate `*GenericJSONPayload` fallback | `GenericJSONPayload` implements the interface, and only `*GenericJSONPayload` satisfies `message.Payload`, so the fallback is unreachable | Accepted by implementation review, which attacked the unreachability four ways |

## Exported surface proposed

`message.RuleReadable`, one method: `RuleFields() map[string]any`.

`agentic.ToolResult.EffectiveErrorKind() ToolErrorKind` was added mid-flight at review direction and
re-enters the gate here, because scope is what shipped rather than what was planned. It is on `agentic/`,
not a package requiring owner design review, and it consolidates an existing normalisation rather than
introducing a new concept.

## Adopter guide: an exclusion that was reversed

`processor/rule/docs/custom-rules.md` is the guide for writing a custom `Rule`. Its worked example called an
`extractValue(payload, field)` helper the document never defined — the payload-reading step was a
hand-wave, which is precisely the gap this interface fills.

An earlier revision of this artifact recorded that as deliberately out of scope. That exclusion no longer
holds: the guide was updated to read through `message.RuleReadable`, and a later review round corrected the
numeric-type guidance in the same example. Both changes are in this branch. The reversal is retained rather
than edited away because it is part of the record the gate should see.
