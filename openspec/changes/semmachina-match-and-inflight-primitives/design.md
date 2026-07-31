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

## §1 Fable design review — APPROVED 2026-07-31

The gate is closed; implementation may start. Three answers, each amending a decision below.

**Q1 — no variadic.** *(Owner ruling 2026-07-31 later refined the shape further — see D6. §1's
HOLDING survives intact; only the spelling changed, from one nil-able parameter to a split pair that
puts answerability in the function name.)* `Matches(def Definition, state *graph.EntityState, lifecycle *lifecycle.Manager) (bool, error)`.
The options-variadic failed the widen-deliberately rule on its own evidence: exactly one "option"
existed, and it was not an option. The lifecycle `Manager` determines **answerability** — whether
`$entity.lifecycle.*` resolves or pre-scan-errors — not flavor. A dependency that changes which
questions can be answered belongs in the signature, visibly. `nil` is an honest "I don't have one",
and D2's pre-scan then names any lifecycle field as unresolvable, which is the correct outcome. An
options struct is introduced when a real second dependency arrives, under the same review.

**Q2/D4 — accepted, with the argument re-grounded.** See the rewritten D4: the consumer-cost-asymmetry
framing was load-bearing and should not have been. It is now a corollary.

**Q4 — neither proposed shape.** The query is served by the component over NATS request/reply. See the
rewritten D3.

**Endorsed without change:** D1's shared-helper extraction *with the tests-unmodified tripwire*, D2's
pre-scan deriving its prefix set from the evaluator's own constants, D5's production-answer
resolution, and the one-PR complete-system scope.

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

**Decision:** take option (2), and serve it **from the component over NATS request/reply** — the third
shape, which dissolves the component-method-vs-package-function dilemma rather than choosing a horn.

Both proposed shapes were flawed. A package-level function needs `ConsumerNameSuffix` and the stream
name passed in, which *relocates* the consumer-side reconstruction into a parameter list instead of
deleting it. A component method assumes the caller holds a component handle, which a boot-time
recovery pass may not — and may not even be in-process.

The house pattern already resolves this: **components execute and do not know their caller.** The
agentic-loop component already serves `agentic.query.trajectory` through `SubscribeForRequests`
(`component.go:375`, handler `:1796`), so an in-flight query subject follows an established surface
rather than inventing one, and is the NATS Direct shape the `/query-pattern` rubric prescribes for
this kind of read. The component derives its own consumer name internally via the shared helper: **no
name, no config, and no handle crosses any boundary**, the derivation is deleted from every caller
rather than relocated, and an out-of-process recovery pass is served for free.

`sanitizeSubject` and the assembled name stay unexported. The existing `setupConsumer` call site
(`component.go:761-764`) and the new query handler share one internal helper, so the query cannot
address a different consumer than the component binds — that shared helper is the real fix, since the
drift gh#733 fears is between *two derivations*, not between a derivation and a caller.

### D3a — Unknown is not zero, and no-responders is unknown

**Decision (Fable, Q4 constraint):** a no-responders reply SHALL be surfaced as *unknown*, never as
zero outstanding work — stated once in the spec and inherited by every path that can fail to observe.

Moving the query onto the wire introduces a failure mode the in-process shapes did not have: the
agentic-loop component may be down. `natsclient.IsNoResponders` (`natsclient/errors.go:333`) detects
it. A down loop component emphatically does **not** mean nothing is in flight — messages may be
sitting in the stream with nobody to answer for them, which is precisely when a recovery pass is most
likely to be running and most likely to do damage.

This is the same rule that shaped `OutstandingWork`'s error-not-`(0, nil)` return for an unbound
consumer, and the same rule as `ErrConsumerNotFound`-is-not-idle. Three instances of one invariant:
**an absent measurement must never render as a measurement of absence.** It is stated once as a
requirement and the specific cases cite it.

**Consequence for the consumer, recorded because it composes:** the recovery pass gates on the loop's
readiness producer *first* — the ADR-066 envelope gh#732 just shipped — and only then queries
in-flight state. The two halves of this change compose: readiness answers "is this component's answer
trustworthy yet", in-flight answers "what is it". Asking the second without the first is the
cold-start read bucket, and it fails closed.

### D4 — The primitive answers obligation; production answers instant (cooldown not applied)

**Decision (Fable-approved, argument re-grounded):** cooldown is not applied, and the contract states
what the primitive answers rather than caveating what it approximates.

**Cooldown is a rate limiter, not a match negation.** A rule inside its cooldown window still owes the
entity the hop — it fires when the window expires. So the two paths are not "production" and "a
permissive approximation of production"; they answer **two different questions**:

| | Question | Cooldown |
|---|---|---|
| Stateless `Matches` | *Does this pack still owe this entity work?* — **obligation** | irrelevant |
| Running engine | *Would this rule fire right now?* — **instant** | applies |

For gh#731's consumer — a boot-time recovery pass deciding whether a parked turn is stranded or still
owed a hop — **obligation is the more correct answer**, not a tolerable approximation of the instant
one. A rule mid-cooldown means work is genuinely still coming; classifying that entity as stranded
would be wrong, and it is wrong for a reason about the domain, not about which direction is safer.

The one-directional-safety property (the stateless verdict can differ from a live engine only by
matching where the engine would be cooling down, never the reverse) is now a **corollary** of the
above rather than the load-bearing beam. It was doing too much work in the first draft: an argument
resting on one named consumer's cost asymmetry does not survive that consumer changing its mind.

Framing it as a distinct question also fixes the discoverability problem. A future consumer that
genuinely needs the instant answer is not reading a caveat and hoping it does not apply to them —
they are reading a different question and can immediately tell it is not theirs.

*Alternative rejected:* refuse definitions that declare a cooldown. It makes the primitive useless
for its named consumer over a semantically benign feature, and it would refuse on the basis of a
field that does not affect the question actually being asked.

### D5 — Empty condition list resolves to the production answer

The two paths disagree (`true` in the evaluator, `false` in the wrapper). The caller is asking what
production would do, so the stateless path returns `false`. The spec states it so the next reader does
not "fix" it toward the evaluator.

### D6 — Signature shapes (settled at §1)

```go
// processor/rule — SPLIT PAIR (owner ruling, 2026-07-31, superseding the single
// nil-able parameter §1 approved)
func Matches(ctx context.Context, def Definition, state *graph.EntityState) (bool, error)
func MatchesWithLifecycle(ctx context.Context, def Definition, state *graph.EntityState,
    lookup LifecycleLookup) (bool, error)
```

§1's holding is PRESERVED and strengthened: the lifecycle lookup determines **answerability**, not
flavor, so it must be visible — and the split puts it in the function NAME rather than in an argument
a caller can pass as `nil`. This is the stdlib pattern (`http.NewRequest` /
`NewRequestWithContext`).

Three shapes were considered, ranked by where answerability is visible:

| Shape | Answerability visible |
|---|---|
| Split pair (**chosen**) | in the name |
| Single nil-able parameter (§1's approval) | in the signature — but `nil` reads as complete |
| `...MatchOption` variadic (§1 rejected) | nowhere — a lookup-less call compiles and reads as finished |

`ctx` is first, per Codex finding 5: the lookup performs KV/graph I/O, and without a caller context a
degraded backend wedges a boot-time recovery pass indefinitely.

**`lookup` is REQUIRED on the lifecycle entry point.** Absent is not a degraded mode; it is a call to
the wrong function, so it is refused loudly and the caller is pointed at `Matches`. The check catches
a TYPED nil too — `var m *lifecycle.Manager` is a non-nil interface holding a nil pointer, and
`LookupByEntityID` dereferences its receiver immediately. Note the split does NOT make typed-nil
unrepresentable, it makes it a loud input error instead of a panic.

`LifecycleLookup` (narrow, read-only) rather than the concrete `*lifecycle.Manager`: resolution
performs exactly two lookups while `LifecycleManager` also carries `TransitionWith`/`Complete`/`Fail`,
and — decisively — `lifecycle.NewManager` requires a `*natsclient.Client` while `newManagerForTest` is
unexported, so a concrete parameter would make Codex finding 2's REQUIRED unregistered-participant
and transient-lookup tests impossible at unit level. `*lifecycle.Manager` satisfies the interface, so
the call site §1 intended is unchanged.

`Definition` is same-package (`rule_factory.go:15`); `*graph.EntityState` is the type imported locally
as `gtypes` (`expression_factory.go:10`); `*lifecycle.Manager` is `pkg/lifecycle/manager.go:43`.

The in-flight query is a NATS request/reply subject served by the component (D3), so its "signature"
is a request/response payload pair plus the handler shape the component already uses for
`agentic.query.trajectory`: `func (c *Component) handleX(context.Context, []byte) ([]byte, error)`,
with `errs.Classified` carrying the error class to the wire. Both returns stay under the
three-or-more-is-a-struct threshold.

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

## Open Questions — all closed at §1

1. ~~**Exact exported names and signatures**~~ — **CLOSED.** No variadic; the lifecycle `Manager` is a
   named parameter because it governs answerability. See D6.
2. ~~**D4 — is documented cooldown permissiveness acceptable?**~~ — **CLOSED, and the question was
   slightly wrong.** Permissiveness is not what is being accepted: the primitive answers *obligation*
   where production answers *instant*, and for the named consumer obligation is the more correct
   question. See the rewritten D4.
3. ~~**Consumer-to-subject cardinality**~~ — **CLOSED against the code before review:** 1:1. The
   consumer name is derived from the subject and `FilterSubject` is that subject, so the outstanding
   count is that subject's.
4. ~~**Component method or package-level function?**~~ — **CLOSED: neither.** The component serves the
   query over NATS request/reply, which deletes the derivation from every caller instead of relocating
   it and survives an out-of-process caller. See D3, and D3a for the no-responders constraint that
   moving onto the wire introduces.

**New constraint arriving with Q4's answer, tracked here so it is not lost in D3a:** no-responders is
*unknown*, never zero. The recovery pass must gate on the loop's readiness envelope (gh#732) before
trusting an in-flight answer.
