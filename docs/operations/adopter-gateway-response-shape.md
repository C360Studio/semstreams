# Adopter note — GraphQL gateway response shape (BREAKING, gh#762)

**Applies to:** consumers of the graph-gateway GraphQL surface (`semsource`, `semdragon`,
`semconnect`).
**Lands in:** the `v1.0.0-beta.159` sister-lockstep wave. Conform once, at the tag.

## What changed

One GraphQL field returned its payload wrapped in the internal `QueryResponse` envelope, so
callers had to take an extra `.data` hop that the other fields never required. The gateway now
removes that envelope uniformly, by detecting it on the reply rather than matching the subject.

**If you read the field below, your read path changes. Nothing else does.**

## 1. Fields that CHANGE — update these read paths

| GraphQL field | Before | After |
|---|---|---|
| `graphSummary` | `data.graphSummary.data.total_entities`<br>`data.graphSummary.data.entity_types`<br>`data.graphSummary.data.entity_sample_truncated` | `data.graphSummary.total_entities`<br>`data.graphSummary.entity_types`<br>`data.graphSummary.entity_sample_truncated` |

**One field. That is the entire blast radius on the GraphQL surface.** `graph.query.summary` returned
the envelope while the gateway unwrapped only `graph.index.query.*`.

> An earlier draft of this note listed a second field, `byName`. That was wrong and is corrected
> here: `graph.query.byName` is a real NATS subject but the gateway does not route to it — there is
> no `byName` branch in the query router, no field mapping, and no such field in the schema. A
> NATS-Direct consumer of `graph.query.byName` talks to graph-query without passing through this
> gateway and is **unaffected by this change**. If you already edited a `byName` GraphQL read path
> on the strength of that draft, revert it.

The envelope's own metadata (`timestamp`, and `request_id` when present) is no longer visible to
GraphQL callers at all. It was never part of the intended contract — it leaked because the envelope
did.

## 2. Fields ALREADY FLAT — no change, and no action

These returned the envelope and were already being unwrapped by the old prefix gate. Their shape is
byte-identical before and after.

| GraphQL field | Backing subject |
|---|---|
| `predicates` | `graph.index.query.predicateList` |
| `predicateStats` | `graph.index.query.predicateStats` |
| `entitiesByPredicate` | `graph.index.query.predicate` |
| `compoundPredicateQuery` | `graph.index.query.predicateCompound` |

## 3. Fields UNCHANGED BY DESIGN — do not "fix" these

**This column exists because it is the one instinct omits.** These fields never carried the
envelope, so they never had a `.data` hop to remove. If you find yourself adding one to a read path
here, or stripping a level, you are introducing a bug rather than adopting a change.

| GraphQL field | Shape | Why it is not affected |
|---|---|---|
| `entitiesByPrefix` | bare `[Entity]` array | `PrefixQueryResponse` (`{entities, next_cursor}`) is its own type, not `QueryResponse[T]`, and keeps its own separate unwrap |
| `entity`, `entityByAlias` | entity object | served from graph-ingest, never enveloped |
| `relationships` | relationship object | raw marshal |
| `pathSearch` | `PathSearchResult` | composite, raw marshal |
| `entityIdHierarchy` | hierarchy object | raw marshal |
| `spatialSearch`, `temporalSearch` | arrays | served by the spatial/temporal indexes, never enveloped |
| `similaritySearch`, `findSimilar` | search results | served by graph-embedding, never enveloped |
| `globalSearch`, `localSearch`, `searchGraph` | GraphRAG results | raw marshal |
| `trajectory` | `Trajectory` object | served by agentic-loop over `agentic.query.trajectory`, never enveloped |

## How to check your client

Grep your read paths for a repeated hop, which is the whole signature of the change:

```bash
grep -rn 'graphSummary\.data\|\bdata\.data\b' <your-repo>
```

Anything that matches is in table 1 and needs the extra hop removed. Anything that does not match is
in table 2 or 3 and needs nothing.

## Why this was breaking rather than additive

The envelope and the unwrapped payload both decode cleanly into a permissive target, so a client
that decodes into a struct will not fail loudly — it will silently read a zero value for every field
that moved. There is no compatibility shim: pre-1.0, per the project's cross-product breaking-change
policy, the shape is corrected once and adopters conform at the lockstep tag.

## Why the gateway no longer decides this from the subject

Query handlers proxy. graph-query's `semantic`, `spatial`, `similar` and `byName` handlers forward to
a downstream subject and return that reply verbatim, so an envelope produced under
`graph.index.query.*` reaches the gateway under a `graph.query.*` subject. Whether a reply carries
the envelope is a property of the **reply**, not of the subject that served it — so a
subject-keyed rule is wrong for some reply no matter how the list is maintained. Detection-by-reply
also protects proxied subjects the gateway does not route today (such as `graph.query.byName`) if
they are ever added — a soundness property of the fix, not a second live bug; see the correction
note in §1.

Detection is deliberately conservative: a reply is the envelope only if it carries both `data` and
`timestamp` **and** every one of its keys is drawn from `{data, request_id, timestamp}`. A payload
of yours that happens to have a top-level `data` field is therefore left alone.

## Problems

Issues you hit adopting this belong in `semstreams` as new issues, referencing gh#762. Migration
tracking for the wave is gh#753.
