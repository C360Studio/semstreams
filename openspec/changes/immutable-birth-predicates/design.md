# Design — immutable-birth-predicates (gh#818)

## Context

See `proposal.md`. The load-bearing verified facts (`c05a11fb`):

- Eight request/reply lanes (`mutations.go:78-135`), one bucket (`ENTITY_STATES`), every
  handler already metered through `meteredMutation` with typed classified rejections and a
  closed error-code set (`graph/mutation_responses.go:86-174`).
- The Graphable merge replaces per (subject, predicate) wholesale (`MergeTriples`,
  `graph/helpers.go:108-141`) — and already carries the one working precedent for this
  change: the indexing profile is dropped from the incoming triple set before merge when the
  resident entity carries one (`component.go:2574-2583`). Enforcement, metric, and test
  patterns for "preserve the stored value against a newer-wins merge" all exist there.
- Predicate-contract enforcement already runs in graph-ingest on every lane
  (`predicate_contract_rejections_total{lane}` — `mutations.go:182-197`), so a
  vocabulary-declared classification has an existing declaration→enforcement channel; the
  vocabulary registry already carries per-predicate booleans (`RuleOpaque`,
  `vocabulary/predicates.go:414-425`).
- sem* products embed the framework in-process, so vocabulary registered by the product
  binary is visible to graph-ingest without any wire hop.
- The add lane's six-field tuple dedup (`message.DedupeAppendTriples`) already makes exact
  duplicate appends a committed no-op.

## Goals / Non-Goals

**Goals:** one declarable, entity-agnostic immutability policy; enforcement at the single
writer (graph-ingest) covering all eight lanes + merge + delete; deterministic, audited
refusal; idempotent replay.

**Non-Goals:** privileged teardown (ADR-068 lane); owner-lease semantics; any per-entity or
per-pattern scoping; protection against direct KV access (operator ACL boundary, documented,
not simulated).

## Decisions

### D1 — Declaration home: the vocabulary registry, not the projection contract

`WithImmutable(true)` beside `WithRuleOpaque`, surfaced on predicate metadata. Rationale:
gh#818 says the need is "vocabulary-shaped and not specific to a world or entity ID"; the
vocabulary is already the product-neutral predicate authority; and graph-ingest already
enforces vocabulary-derived policy per lane, so no new declaration→enforcement channel is
invented. The projection `Contract.BirthPredicates` alternative was rejected: contracts are
client-side authorization shapes, the global contract registry has zero production callers,
and graph-ingest reads none of it — homing enforcement there would re-create the
half-client-half-server ambiguity this change exists to end.

**Authority split (gh#818's explicit ask):** declaring `Immutable` grants nothing. The
predicate-contract spec already pins "mutation-lane access is the trust boundary"; who may
reach the mutation subjects — and therefore who may seed — is host NATS ACL policy. A package
that declares a predicate immutable but has no mutation-lane access cannot seed it; a writer
with lane access seeds by writing first. The docs deliverable states this split verbatim.

### D2 — Semantics: first-write-freezes, lane-neutral, caller-independent

The protected unit is **(entity, immutable predicate)**: the first committed write of an
immutable predicate on an entity freezes that predicate's canonical value set on that entity.
No lane is privileged for seeding (create lanes, update lanes, and Graphable first-arrival
all seed identically) and no caller is exempt afterward — including the seeder. This keeps
immutability a property of the fact, orthogonal to identity, and avoids every token/lease
entanglement (Non-goals).

Frozen basis: the **canonical object value set with datatype**, order-independent for
multi-valued predicates. Envelope volatiles (timestamp, confidence, source, context) do not
participate in equality — a replayed seed from a rebuilt package must converge (idempotent
replay) even when its timestamps differ. The exact basis is pinned by a round-trip test, not
prose. Conflicting append (a new value joining a frozen set) is a refusal: the frozen unit is
the set, or the acceptance criterion "conflicting append is rejected" is unmeetable.

### D3 — Enforcement disposition per lane class

- **Request/reply lanes (all eight): reject the whole request** with the new stable code
  when it would replace, remove, or conflict-append a frozen predicate. Atomic and
  deterministic — no partial application of a request that is partly illegal. Exact-value
  touches are committed no-ops on that predicate (the add lane's dedup already behaves this
  way; update lanes value-compare before refusing).
- **Graphable merge: preserve-and-continue**, generalizing the indexing-profile
  drop-before-merge: incoming triples for frozen predicates are dropped, the rest of the
  arrival merges, the drop increments a metric and logs the three facts. Rejecting the whole
  arrival was considered and refused: this lane cannot return an error to its producer, and
  discarding an arrival's unrelated facts converts one protected predicate into data loss.
  The silent-exclusion concern is answered by the metric + Warn (and the indexing-profile
  precedent has run this way in production since ADR-054).
- **Delete: refuse** while the carrier holds any frozen predicate, same stable code, naming
  the predicates. Teardown is the retention system's lane (Non-goals).

### D4 — The refusal is a first-class classified outcome

New code in the closed set: `immutable_predicate` (`errs.ErrorInvalid` class — the request is
permanently wrong, retry cannot help). The detail names entity, predicate, and lane — the
three facts an operator needs (the gh#837 lesson; same rule as gh#810's capture message).
Metering rides `meteredMutation` unchanged (`mutation_rejections_total{subject,
reason="immutable_predicate"}`); the Graphable drop gets
`immutable_drops_total{lane="graphable"}`. Label hygiene follows the existing rule: predicate
strings stay out of metric labels, in the log line only.

### D5 — Enforcement point: one shared gate, called from every write path

One function (entity-state view + incoming delta → refusal or filtered delta) called from
the eight handlers, the merge path, and delete — not eight hand-rolled checks. The
refresh-a-guard's-baseline-on-every-write-path lesson applies: the gate takes the *resident*
entity as read inside each lane's existing CAS closure, so a retry re-evaluates against the
state it is about to replace, and TOCTOU cannot reopen the hole the guard closes.

## Risks / Trade-offs

- **[A frozen mistake is frozen]** a package seeds a wrong value; no mutation lane can fix
  it. → By design (that is what immutable means); remediation is the retention/deletion lane
  or a new entity. Documented.
- **[Graphable drop surprises a producer]** a producer re-emitting an entity with a changed
  frozen value sees its change silently not-applied (metric aside). → Mitigation: metric +
  Warn with three facts; the producer-side contract is documented ("emit the seeded value or
  omit the predicate"). This is the indexing-profile contract, already lived-with.
- **[Vocabulary registration ordering]** enforcement requires the classification to be
  registered before graph-ingest serves mutations. → Vocabulary registration is init/boot
  time in every embedding binary today (the namespace-authority "immutable authoring policy"
  is already boot-frozen); a predicate declared immutable after facts exist freezes them
  as-is at declaration — first-write-freezes evaluates against whatever is resident, so late
  declaration is safe (it can only widen protection, never orphan it).
- **[Performance]** every mutation now consults per-predicate metadata. → The vocabulary
  lookup is an in-process map read; the gate runs inside closures that already unmarshal the
  resident entity. No new I/O.

## Migration Plan

Additive and opt-in per predicate: zero declared immutable predicates = zero behavior change.
No schema/wire change; no new subjects. SemMachina adopts by adding `WithImmutable(true)` to
its truth-predicate declarations and re-running its task 1.5 acceptance against real
graph-ingest. Rollback = remove the declaration (facts thaw; nothing is corrupted).

## Open Questions

- Whether the privileged teardown lane (ADR-068) should recognize frozen predicates as a
  distinct class or treat carrier entities like any other retained entity — deferrable; does
  not change this change's specs or tasks (refusal is the contract either way). **Owner
  ruling welcome but not blocking.**
