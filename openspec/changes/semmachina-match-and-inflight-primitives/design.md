# Design — SemMachina match and in-flight primitives

## Context

Two sister-repo consumers (SemMachina, and semdragon through the same integration) are building a
boot-time recovery pass that must decide, for a turn parked mid-chain, whether the substrate still
owes it work or whether it is stranded. Neither half of that question has a framework answer, so both
are being reconstructed consumer-side, and both reconstructions fail silently.

Constraints that shape everything below:

- **gh#761's exported-surface contract binds both symbols**, and framework packages additionally
  require Fable design review BEFORE implementation. This change deliberately stops at that gate.
  Session 18 shipped three symbols that predate the rule and never got the pass; this is the change
  that does not add a fourth.
- **`natsclient.OutstandingWork(ctx, stream, consumer) (uint64, error)` already exists**, built by the
  readiness increment (#758, `52cf2abf`). Half of gh#733 is already solved; what remains is that
  calling it requires a consumer name the caller cannot legitimately obtain.
- **Both new symbols have named callers at birth**, so neither is a phantom export.

### The measurement that changes gh#733's shape

gh#733 as filed says the loop consumer's "acknowledgement floor is the only authoritative answer".
That premise was falsified after the issue was written. #758 D0 probed `AckFloor.Stream` against both
deployed NATS versions (2.10 and 2.12, identical results) and found it wrong in **both** directions:
it does not advance past a `MaxDeliver`-exhausted message, so it sits behind that message while the
consumer is idle; then on the next unrelated ack it leaps *past* the never-applied message. It never
means "everything at or below this is durably handled". Rejection recorded in ADR-088.

Implementing gh#733 as written would therefore ship the exact defect the previous increment spent its
budget removing. The design below reads `NumPending + NumAckPending` via `OutstandingWork`, and the
spec makes the ack-floor prohibition normative so it cannot return as an optimization.

## Goals / Non-Goals

**Goals**

- One upstream call replaces a consumer's four-step reconstruction of the rule evaluation pipeline.
- One upstream call replaces a consumer's copy of the loop consumer-name derivation.
- Both refuse rather than fabricate when they cannot answer.
- Zero behavior change on the stateful rule path and on ack semantics.

**Non-Goals**

- No rule match state, cooldown bookkeeping, or `MatchState` access (gh#731 disclaims these).
- No second evaluation pipeline. If the stateless path re-implements matching, the change has failed.
- No exported consumer name, and no sharing or rebinding of the loop's consumer.
- No implementation past the Fable gate in this change.

## Decisions

### D1 — Lift the pre-processing into a shared helper; do not copy it

`ExpressionRule.EvaluateEntityState` (`processor/rule/expression_factory.go:170`) performs four steps
between a `Definition` and the evaluator. Verified at HEAD:

| Step | Site | What a bare-evaluator caller loses |
|---|---|---|
| `SubstituteConditionValues` | `expression_factory.go:196` | `value: "$entity.triple.foo.length"` reaches the operator as literal template text and coerce-errors |
| `PopulateLifecycleStateFields` → `EvaluateWithStateAndMessage` | `:203-208` | `$entity.lifecycle.*` never resolves (ADR-047) |
| Cooldown short-circuit | `:176-178` | — (stateful; see D4) |
| `len(conditions)==0 → false` | `:180-182` | evaluator returns **true** for the same input (`evaluator.go:134-136`) |

**Decision:** extract steps 1, 2 and 4 into an unexported helper that both `EvaluateEntityState` and
the new entry point call. The stateless path is then definitionally incapable of drifting from
production, which is the entire point of the issue.

*Alternative rejected:* have the stateless path call `EvaluateEntityState` on a throwaway
`ExpressionRule`. It reaches the right pipeline but drags in `lastTriggered`, `shouldTrigger` and the
factory's lifecycle-manager wiring — constructing a stateful object to answer a stateless question,
and one whose `shouldTrigger` write (`:220`) would have to be reasoned about on every future edit.

### D2 — Refuse unresolvable conditions with an error, and do it at the condition level

`evaluator.go:191-198` returns `false, nil` for an unresolved `$state.*` / `$prev.*` /
`$entity.lifecycle.*` field when `Required` is unset. Correct for the stateful caller, which supplied
the map; wrong for a stateless caller, which never had the chance.

**Decision:** the stateless entry point pre-scans the definition's conditions and returns an error
naming the first unresolvable field, **before** evaluating anything. Pre-scanning rather than
inspecting the evaluator's return keeps `evaluator.go`'s stateful behavior untouched — the spec makes
"the stateful path SHALL NOT change" normative, and a pre-scan is the only shape that cannot violate it.

*Alternative rejected:* a `strict` flag threaded into the evaluator. It puts a caller-mode branch
inside the hot stateful path to serve a cold out-of-band one, and any bug in it is a production rule
bug.

### D3 — Expose the in-flight *answer*, not the consumer name

gh#733 offers two options and calls (2) the better shape. The exported-surface contract agrees on two
independent grounds: *"never return a capability where the caller needs a value"*, and *"return the
answer, not the components"*. `ConsumerNameFor(...) string` is a component the caller must then
combine with a stream name and a client to get what it actually wanted, and it freezes the derivation
as a public contract forever.

**Decision:** take option (2). `sanitizeSubject` and the assembled name stay unexported; the existing
call site (`component.go:761-764`) and the new query share one internal helper so the query cannot
address a different consumer than the component binds. That shared helper is the real fix — the drift
gh#733 fears is between *two derivations*, not between a derivation and a caller.

### D4 — Cooldown is not applied, and the contract says so out loud

A stateless caller has no rule instance, so `r.lastTriggered` does not exist for it. Options were to
apply cooldown (impossible without instance state), error when a definition declares one (refuses a
common, benign case), or evaluate conditions on their merits and document the gap.

**Decision:** evaluate on the merits and document. The resulting disagreement is one-directional —
the stateless verdict can be **permissive** relative to a running engine (matching where a live rule
would be cooling down) and never the reverse. For gh#731's consumer that is the safe direction:
`true` means "the pack still owes this entity a hop", so the recovery pass keeps its hands off; a
false negative is what strands an entity. Documented, one-directional, and safe-side is acceptable;
undocumented would not be.

**This is the decision most worth Fable's attention** — it is the one place the primitive knowingly
answers a slightly different question than production, and the argument rests on the consumer's cost
asymmetry rather than on a framework invariant.

### D5 — Empty condition list resolves to the production answer

The two paths disagree (`true` in the evaluator, `false` in the wrapper). The caller is asking what
production would do, so the stateless path returns `false`. The spec states it so the next reader does
not "fix" it toward the evaluator.

### D6 — Signature shapes

Both return `(value, error)` — two correlated returns, under the three-or-more-is-a-struct threshold,
with no component the doc comment must warn callers away from. Exact spellings are deliberately left
open for Fable (see Open Questions), since naming is part of what that review is for.

## Risks / Trade-offs

- **A shared helper changes the stateful path's code even though it must not change its behavior.**
  → The refactor lands with the existing rule-evaluation tests unmodified; if any assertion needs
  editing to stay green, that is evidence of a behavior change, not of a stale test. Call it out
  rather than adjust it.
- **The pre-scan and the evaluator could disagree about what counts as unresolvable.** → Derive the
  pre-scan's prefix set from the same constants the evaluator branches on, not a second literal list.
  A duplicated prefix list is this change's own drift hazard, in miniature.
- **`OutstandingWork` counts the consumer, not the subject** — so the count only answers the subject
  question if the binding is 1:1. → **Checked, and it is:** `setupConsumer` derives the consumer name
  from the subject (`component.go:761`) and binds `FilterSubject: subject` (`:822`), so one consumer
  filters exactly one subject and its outstanding count is that subject's. No wording narrowing
  needed. The residual is `ConsumerNameSuffix` (`:762-764`), which lets two deployments hold distinct
  consumers on the same subject — the query answers "**this** deployment's outstanding work", which is
  what the caller wants, but it is a distinction the contract must state rather than leave implied.
- **Cooldown permissiveness (D4) is safe for the named consumer, not universally.** → The contract
  states the direction, so a future consumer with the opposite asymmetry can see the mismatch before
  adopting it.

## Migration Plan

Purely additive; nothing to migrate. No NATS state, schema, wire format, or configuration changes. No
existing caller's behavior changes. Rollback is removal of the new symbols.

Sequencing: Fable design review → implementation → reviewer → owner Codex round → merge. Per the
baton, both issues land in **one PR** (`PR scope = complete system`).

## Open Questions

1. **Exact exported names and signatures** — deferred to Fable by design. gh#731 sketches
   `Matches(def, state, opts...) (bool, error)`; the options-variadic is the part most worth
   challenging, since today there is exactly one option (the lifecycle `Manager`).
2. **D4 — is documented one-directional cooldown permissiveness acceptable**, or should a definition
   declaring a cooldown be refused outright? The former serves the known consumer; the latter is
   stricter and cannot mislead a future one.
3. ~~**Consumer-to-subject cardinality**~~ — **RESOLVED against the code:** 1:1. The consumer name is
   derived from the subject and `FilterSubject` is that subject, so the outstanding count is the
   subject's. No requirement rewording needed.
4. **Does the in-flight query belong on the component or at package level?** This is the sharpest
   remaining question, and the cardinality check above is what sharpens it: `ConsumerNameSuffix` is
   **component config**, so a package-level function cannot name the right consumer without being
   handed the suffix and the stream name — which is the consumer-side reconstruction gh#733 exists to
   delete, merely relocated to a parameter list. A component method has the config but assumes the
   caller holds a component handle, which a boot-time recovery pass may not. Neither shape is
   obviously right; picking wrong reintroduces the defect in a new place.
