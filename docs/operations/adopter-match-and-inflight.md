# Adopter note — asking SemStreams what a rule owes, and what work is in flight

**Audience:** SemMachina and semdragon (gh#731, gh#733), and any repo building a
recovery or reconciliation pass over SemStreams state.

**Status:** additive, NOT breaking. Nothing you have today stops working.

This note states **the rules**, not the diff. See
`openspec/specs/{rule-engine,agentic-loop}/` for the mechanics and ADR-088 for the
measurement that shaped the second one.

## Rule 1 — ask the framework whether a rule matches; do not rebuild the pipeline

```go
// no lifecycle lookup — $entity.lifecycle.* conditions are REFUSED, not guessed
matched, err := rule.Matches(ctx, def, entityState)

// with one — those conditions resolve
matched, err := rule.MatchesWithLifecycle(ctx, def, entityState, lookup)
```

**Pick by what you can answer, not by what you can pass.** The pair is split so the
limitation is in the name you type: a lookup-less call cannot answer lifecycle
conditions, and you find that out at the call site rather than at runtime. Passing
nil to `MatchesWithLifecycle` is refused — including a typed nil — and points you
back at `Matches`.

`ctx` is not decoration: the lookup performs KV/graph I/O, so without it a degraded
backend wedges your recovery pass indefinitely. Pass a deadline.

Reaching for `expression.Evaluator` directly loses condition-value substitution and
lifecycle resolution, and inherits the evaluator's "empty condition list passes"
semantics. Each loss reports a confident wrong verdict, and every one of them fails
in the direction that makes you act.

## Rule 2 — `Matches` answers OBLIGATION, not INSTANT

| Question | Who answers it |
|---|---|
| *Does this pack still owe this entity work?* | `rule.Matches` |
| *Would this rule fire right now?* | the running rule engine |

The difference is cooldown. A rule inside its cooldown window **still owes the
entity the hop** — it fires when the window expires — so cooldown is a rate limiter,
not a match negation, and `Matches` does not apply it. If you need the instant
answer, this is not the function you want.

Consequence: a `Matches` verdict can differ from a live engine's only by matching
where the engine would be cooling down, never the reverse.

## Rule 3 — three outcomes, not two

Both primitives distinguish **"no"** from **"could not tell"**. Do not collapse them.

| `rule.Matches` | Means |
|---|---|
| `false, nil` | evaluated; conditions do not hold; nothing owed. Safe to act on. |
| `_, err` | could not evaluate — unresolvable `$state.*` / `$prev.*` / `transition`; a lifecycle field whose lookup was absent **or failed**; an unresolved condition-value template; an absent `Required` field; a failed operator. **Not** a statement about obligation. |

Two of those deserve emphasis, because both once returned a confident `false`:
a **supplied lookup that fails** is not resolved state (an unregistered participant
or a transient KV error must not read as "nothing owed"), and an **unresolved value
template** would otherwise be compared by `eq`/`contains` as an ordinary string.

A **disabled** definition is not an error — it returns `false, nil`. It cannot fire,
so it owes nothing.

Treat an error as *cannot tell — leave it alone*. That is the safe action in both
directions.

## Rule 4 — in-flight work: ask over NATS, never derive the consumer name

```go
subject := agenticloop.InFlightQuerySubjectFor(deployment) // deployment = the loop's ConsumerNameSuffix
raw, err := client.RequestClassified(ctx, subject, req, timeout)
```

**The subject is deployment-addressed, and that is load-bearing.** Request/reply uses
plain subject subscription, so a shared subject would let every agentic-loop in the
account reply and you would keep whichever landed first — an arbitrary deployment's
answer, delivered with full confidence. The suffix is the right selector because it
already determines which durable consumer exists: loops sharing it bind the same
consumer and report the same count; loops with different suffixes are different
deployments. Empty suffix maps to `agenticloop.DefaultDeployment`.

Request `{"subject":"agent.task.*"}`; a known answer decodes to
`{subject, outstanding, inFlight}`. The consumer name, its subject sanitizing, and
`ConsumerNameSuffix` stay inside the component — a copied derivation does not fail
to compile when it drifts, it fails to find a consumer, and that reads as idle.

The answer is scoped to **this deployment's** consumer; `ConsumerNameSuffix` is what
distinguishes two deployments on one subject.

## Rule 5 — unknown is never zero

This is the one that costs the most to get wrong.

| Condition | Branch on | Means |
|---|---|---|
| no consumer bound for the subject | `errors.Is(err, agenticloop.ErrInFlightUnknownNoConsumer)` | this deployment has no agentic-loop for it |
| consumer state unreadable | `errors.Is(err, agenticloop.ErrInFlightUnknownUnreadable)` | not observed this attempt |
| **no responders** | `natsclient.IsNoResponders(err)` | the loop component is not running |

None of them means "nothing is in flight". The third is the dangerous one: a stopped
loop does **not** mean the work is gone — messages may be sitting on the stream with
nobody to answer for them, which is precisely when a recovery pass is running and
most able to do harm.

An unknown answer carries **no payload**, so there is no `outstanding: 0` to misread.
Branch on the sentinels, never on message text; they survive the wire.

Do not reconstruct this from `AckFloor`. It was measured against both deployed NATS
versions and misreports in *both* directions — stalling behind a
`MaxDeliver`-exhausted message while idle, then leaping *past* that never-applied
message on the next unrelated ack (ADR-088). `AGENT_LOOPS` `state=running` is not a
substitute either: only a handler transitions a loop out of `running`, so a crashed
process leaves a stale entry indistinguishable from live work.

## Rule 6 — gate on readiness BEFORE trusting an in-flight answer

The two halves compose, and the order matters:

1. **Readiness first** — the loop's ADR-066 envelope in `GRAPH_STATUS`
   (see [adopter-caught-up-readiness](adopter-caught-up-readiness.md)) answers
   *is this component's answer trustworthy yet*.
2. **In-flight second** — answers *what is it*.

Asking the second without the first is a cold-start read: you get a well-formed
answer from a component that has not finished catching up. Absent readiness is
unknown, and unknown defers.
