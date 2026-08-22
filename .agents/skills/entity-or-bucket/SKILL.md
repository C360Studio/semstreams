---
name: entity-or-bucket
description: Decide whether a fact lives in the graph as entity triples or in a private/operational KV bucket, and how a rule reads it. Use when adding any new durable state, proposing a new bucket, or when a fact needs to be rule-readable.
argument-hint: [description of the fact being stored]
---

# Entity or Bucket

## What fact are you storing?

$ARGUMENTS

## The default

**Live in `ENTITY_STATES` as triples about an identified entity.** A private or
operational KV bucket is an *exception* that must name its ground.

This is not a preference. It is the load-bearing rubric from
`docs/adr/049-lifecycle-harness-prime-schema-over-entity-states.md` (generalized
beyond lifecycle by ADR-055), and it carries a burden of proof: **new subsystems
wanting a private bucket file an ADR that defends the choice on the rubric.**

## Own a bucket only when you can name the ground

1. **CAS atomicity** over multi-field state that cannot be expressed as a single
   triple-batch write. *Bar: high — `AddTriplesBatch` IS atomic per-entity.*
2. **Strict-ordering replace** semantics that per-predicate latest-wins cannot
   satisfy. *Bar: high — `ENTITY_STATES` already has per-predicate latest-wins.*
3. **Retention or topology genuinely differs** from the graph — e.g. a longer
   compliance window, or a history depth that IS the data (`STORAGE_REPORT`
   History 10 exists to be a growth series, not to preserve accretion).
4. **Write rate would dominate or pollute the graph** if mixed in.
   *Example: agent trajectories — defensible.*
5. **Bulky payload** that does not belong in the graph — content to ObjectStore
   or a private bucket, with a ref-triple in the graph pointing at it.
6. **Bootstrap circularity** — the fact gates whether the graph plane may be read
   at all, so it cannot itself live on that plane. *Example: `GRAPH_STATUS`
   readiness; the rule processor's own graph evaluation is gated on it.*

Ground 6 is an addition to ADR-049's original five, recorded here because
`GRAPH_STATUS` fits none of the first five and its real justification was
unwritten.

## Live in ENTITY_STATES when

1. The data IS facts the graph should reason over.
2. You want graph queries, inference, or community detection to see it.
3. You want one queryable current state shared by graph consumers. This does
   **not** give you durable audit: `ENTITY_STATES` is History 1.
4. **Multiple consumer surfaces — rules, GraphQL, dashboards, inference — need
   the same state through their natural interface.**
5. You do not need fine-grained CAS that graph-ingest's batched write cannot
   provide.

## The test that does NOT work

**Do not classify by "does it accrete or is it replaced".** It mis-predicts.
`TOOL_CALL_OUTCOMES` and `AGENT_LOOPS`' `COMPLETE_{loopID}` records are both
write-once and identity-bearing — never wholly replaced — yet both correctly live
in buckets, on grounds 4 and 5. Accretion versus replacement is a *consequence* of
which ground applies, never the test.

**The unit is the FACT, not the subject.** One subject legitimately has facts on
both planes. An agent loop has facts *about the run* in the graph (outcome, model,
tokens, cost) and the *content the run produced* in `AGENT_LOOPS`, retrieved by a
key the graph handed you. That is not a violation — it is "rules carry references,
not payloads" working as designed.

## Consumption: rules read two typed planes, and neither is operational KV

Placement decides storage. It does **not** decide consumability, and you cannot
fix a placement problem by widening what rules watch.

Rules evaluate over exactly two typed planes:

- **Authoritative `EntityState`** — decoded triples. `$entity.triple.*`,
  `.length`, `.triples`. The watch grammar IS the six-position entity-ID grammar
  (`ValidateEntityIDPattern`), which is why a bucket key like `COMPLETE_rg_abc123`
  or `graph-index` cannot be watched — it is not an entity ID.
- **Messages** — dotted-path access into an arbitrary payload
  (`$message.*`, ADR-039).

Operational KV is a third plane with **no condition grammar**. The evaluator itself
is not the obstacle — it already accepts arbitrary payload fields — but nothing
decodes a bucket value into them, and per-revision watch semantics have no
counterpart to a rule's `on_enter`.

**So: needing a fact to be rule-readable is a reason to place it well, never a
reason to widen the watch allowlist.** ADR-049 makes this explicit — "multiple
consumer surfaces (rules, ...) need to read the same state" is item 4 on the
*live-in-`ENTITY_STATES`* list. Rules-readability is a placement INPUT.

### When a bucket ground genuinely applies AND rules need to see it

This is real — `GRAPH_STATUS` cannot live in the graph (ground 6) yet gating rules
on index readiness would be useful. The answer is **not** a third watch lane:

- **Publish the condition as a message.** Rules already read that lane with the
  generic path grammar. One publish, no new machinery, no second definition of
  what a condition means.
- **Or emit a companion triple** on an entity that legitimately exists, alongside
  the operational store.

Either way the operational bucket keeps its ground and the fact becomes readable.
Adding a third watch lane would mean a per-bucket decoder, undesigned trigger
semantics, and a second home for condition meaning — the parallel-channel shape
this repo retires rather than builds.

## Before you propose a new bucket

- [ ] Which numbered ground (1–6) does it meet? If none, it goes in the graph.
- [ ] Is the ground about the FACT, or about a subject that also has graph facts?
- [ ] Does anything need to react to it? If so, how does it reach the entity or
      message plane — as a triple, or as a published message?
- [ ] Is it framework-owned state? Then it needs a catalog descriptor
      (`framework-bucket-catalog`) binding owner, write policy, and retention.
- [ ] Ground 1–6 met and non-obvious? ADR-049 says file an ADR defending it.

## See also

- `docs/adr/049-lifecycle-harness-prime-schema-over-entity-states.md` — the rubric
- `openspec/specs/framework-bucket-catalog/spec.md` — ownership, retention, and
  the must-exist reader seam, once you have decided to own a bucket
- `/kv-or-stream` — a different question: fact-vs-work-request, not where state lives
