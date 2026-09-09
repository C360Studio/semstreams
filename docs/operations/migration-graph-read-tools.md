# Migration Guide: direct graph-read tools signal absence (#1261)

## Who this is for

The consumer that changes is **the model**, not a Go caller. The Go exported surface of
`processor/agentic-tools/executors` is additive — one new optional interface, `KVKeyLister`; `KVGetter`,
`NewGraphQueryExecutor`, and `ListTools` are unchanged, and `task api:compat` reports no new incompatible change in
this package. `task api:compat` cannot see any of the flips below: they are JSON inside `ToolResult.Content` and
`ToolResult.Metadata`.

So read this if you operate a deployment whose agents call `query_relationships`, `query_neighbors`, or
`query_by_type`, or if you have a persona, prompt, rule, or downstream consumer that reads those results. Nothing
requires a code change to keep compiling. Four behaviours produce **different, usually smaller, result sets** than
they did before, and one previously-inert tool starts answering.

**Nothing to configure.** There is no new knob, no new bucket, no new index, and no new subject.

---

## 1. `query_relationships` rows are relationship triples only

**Before:** every triple on the entity was reported as a relationship, whatever its object was. A temperature
reading arrived at the model as an edge.

**After:** a row is a triple for which `message.Triple.IsRelationship()` holds — the object is a canonical
six-part entity ID under a reference-compatible datatype. Literal-object triples are no longer rows; they appear
under the new `predicates_present` field with `"kind": "property"`.

```jsonc
// BEFORE
{
  "entity_id": "acme.ops.gcs.robotics.drone.d1",
  "relationships": [
    {"type": "robotics.fleet.member",  "source": "acme.ops.gcs.robotics.drone.d1", "target": "acme.ops.gcs.facility.site.s1"},
    {"type": "robotics.battery.level", "source": "acme.ops.gcs.robotics.drone.d1", "target": ""}
  ],
  "count": 2,
  "direction": "both"
}

// AFTER
{
  "entity_id": "acme.ops.gcs.robotics.drone.d1",
  "direction": "outgoing",
  "relationships": [
    {"type": "robotics.fleet.member", "source": "acme.ops.gcs.robotics.drone.d1", "target": "acme.ops.gcs.facility.site.s1"}
  ],
  "count": 1,
  "predicates_present": {
    "robotics.fleet.member":  {"kind": "relationship", "registered": true, "description": "drone belongs to fleet", "role": "identity", "inverse_of": "robotics.fleet.contains"},
    "robotics.battery.level": {"kind": "property",     "registered": true, "description": "battery charge level percentage"}
  }
}
```

**What to expect:** a smaller `count` on entities that carry literal properties. The predicates did not disappear —
they moved to `predicates_present` with the kind that says why they are not edges.

An empty result now also sets `ResultHint: "empty"` and, when `relationship_type` was supplied, `filter_registered`:

```jsonc
{
  "entity_id": "acme.ops.gcs.robotics.drone.d1",
  "direction": "outgoing",
  "filter_type": "agent.lineage.parent",
  "filter_registered": true,
  "relationships": [],
  "count": 0,
  "predicates_present": { "...": "..." }
}
```

`filter_registered` reports **this process's vocabulary registry and nothing else**. It is not an authorization or
existence verdict: a predicate minted legitimately under a namespace delegation reads `false` here and is still
authoritative on the graph, and a registered predicate can be absent from every entity.

A `relationship_type` that is not a canonical `domain.category.property` is now `invalid_args` **before any read**,
where it previously produced `count: 0` — indistinguishable from a typo.

---

## 2. `query_relationships` serves `outgoing` and refuses the direction it cannot read

**Before:** `direction` defaulted to `"both"`, and `"incoming"` returned `count: 0`. The tool reads the entity's own
record, which holds own-subject assertions only, so `"incoming"` was structurally empty — the model was told
"nothing exists" when the truth was "this tool cannot see it".

**After:** the advertised enum is `["outgoing"]`.

| Call | Before | After |
|---|---|---|
| `direction` omitted | served as `both` | served as `outgoing`, and `"direction": "outgoing"` is echoed in the result |
| `"direction": "outgoing"` | served | served, unchanged |
| `"direction": "incoming"` | `count: 0` | `invalid_args` naming the incoming owner |
| `"direction": "both"` | served | `invalid_args` naming the incoming owner |
| any other value | silently treated as `both` | `invalid_args` |

```jsonc
// AFTER — an explicit incoming or both
{
  "error": "direction \"incoming\" is not served by query_relationships: it reads the entity's own record, which holds outgoing assertions only. Incoming relationships are owned by the graph.query.relationships operation over INCOMING_INDEX.",
  "error_kind": "invalid_args"
}
```

**If you need incoming relationships**, the owner is the admitted `graph.query.relationships` operation over
`INCOMING_INDEX` (`processor/graph-query`). It is not re-homed onto this tool.

**Measured adopter impact:** no in-tree caller passes `direction` to this tool. `configs/domains/{iot,logistics,
robotics}.json` mention the word in natural-language query examples, and `test/e2e/scenarios/tiered_structural.go`
uses the GraphQL gateway's own `RelationshipDirection` — neither reaches this executor.

---

## 3. `query_neighbors` honours `filter_type` and observes a 64KB budget

**Before:** `filter_type` compared a `type` key the graph authority never writes, so it never filtered anything.
There was no width bound. A target that failed to read — absent or transient — was silently skipped.

**After:**

- `filter_type` matches the identity's **type segment** with the same grammar, builder, and matcher as
  `query_by_type` (see §4). A caller who was passing it gets a **smaller set, possibly empty**, where it previously
  got everything.
- A `filter_type` that is not one to three canonical segments is `invalid_args`.
- The result is bounded by a **64KB model-facing content cap** measured on the emitted JSON itself — the length of
  the string you receive, not the compact bytes of the records inside it. Records past the cap are given back, and
  the result reports `truncated` and `frontier_remaining` and sets `ResultHint: "too_large"`. Narrow with `depth` or
  `filter_type`.
- A target absent from `ENTITY_STATES` is listed in `unresolved` rather than omitted.
- **Zero neighbors plus a non-empty `unresolved` is NOT classified `empty`.** `ResultHint: "empty"` means the
  neighborhood is empty; a walk whose targets all exist as edges but are absent from `ENTITY_STATES` reports them
  and carries no hint, because "broaden your filter" is the wrong instruction for "these records are not resident".
- A **transient** read failure now fails the whole call as a network error, where it was previously skipped —
  producing a smaller graph reported as complete.
- An **absent start entity** is now `not_found`, where it previously produced `count: 0`. Classifying that zero as
  `empty` would have made it actively wrong — "try a broader filter" for an entity that does not exist — so it takes
  the not-found classification `query_entity` and `query_relationships` already give the same input.

```jsonc
// BEFORE
{"source_entity": "…", "neighbors": {"…": {}}, "count": 12, "depth": 2, "filter_type": "temperature"}

// AFTER
{
  "source_entity": "acme.ops.gcs.facility.site.s1",
  "neighbors": {"acme.ops.gcs.environmental.temperature.t1": {}},
  "count": 1,
  "depth": 2,
  "unresolved": ["acme.ops.gcs.environmental.temperature.gone"],
  "truncated": false,
  "frontier_remaining": 0,
  "filter_type": "temperature",
  "pattern": "*.*.*.*.temperature.*"
}
```

`query_neighbors` deliberately sets **no** `has_more` and does not declare itself paginated. A traversal frontier is
not a resumable position and this executor holds no server-side traversal state, so announcing continuation without a
token the caller can pass back would be worse than silence. Width is reported through `truncated` +
`frontier_remaining`.

The budget is a **model-facing content cap** in the same class as `bashMaxOutputBytes` and `httpMaxTextSize`, and
like both of those it bounds the emitted string — not a prediction of the NATS transport bound. A result under the
cap that still trips the transport bound takes the component's existing oversize path unchanged; the two compose.

---

## 4. `query_by_type` is served

**Before:** advertised and never implemented. Every call returned the same stub.

**After:** it lists the entity IDs whose identity carries the requested type segment, sorted, paged, with the
pattern it matched.

```jsonc
// BEFORE — every call, always
{
  "entity_type": "temperature",
  "limit": 5,
  "entities": [],
  "count": 0,
  "note": "Type-based queries require entity type index. Use query_entity or query_entities with known IDs.",
  "suggested_ids": []
}

// AFTER
{
  "entity_type": "temperature",
  "pattern": "*.*.*.*.temperature.*",
  "limit": 5,
  "matched": 12,
  "entity_ids": ["acme.ops.gcs.environmental.temperature.t1", "…"],
  "count": 5
}
```

with `ResultHint: "too_large"` and

```jsonc
"metadata": {"entity_type": "temperature", "limit": 5, "has_more": true, "next_cursor": "<opaque>"}
```

`entities`, `note`, and `suggested_ids` are **gone**. Identities only — follow up with `query_entity` or
`query_entities` to read any of them.

### The `entity_type` grammar

One to three dot-separated canonical segments, read **right to left** against the six-part entity ID
(`org.platform.system.domain.type.instance`, ADR-102):

| `entity_type` | Pattern built | Pins |
|---|---|---|
| `temperature` | `*.*.*.*.temperature.*` | type |
| `environmental.temperature` | `*.*.*.environmental.temperature.*` | domain + type |
| `gcs.environmental.temperature` | `*.*.gcs.environmental.temperature.*` | system + domain + type |

A `*`, a `>`, an empty segment, a non-canonical byte, or more than three tokens is `invalid_args` **before any key
scan** — where the stub accepted anything and answered the same nothing.

### Continuation

`query_by_type` declares `Paginated: true` and follows the framework's existing pagination contract rather than a
body-level truncation flag.

- `has_more` is in `ToolResult.Metadata` on **every** successful call, `false` included.
- When matches remain beyond the page, `next_cursor` carries an **opaque** token. Pass it back verbatim as the
  `cursor` argument. Do not parse, construct, or modify it — it uses the same encoding as the graph prefix-listing
  cursor, and that encoding may change.
- `ResultHint: "too_large"` is still set on a page that does not exhaust the match, so the model is told both to
  narrow and that it may continue.
- `matched` is the **whole** match count on every page, never the remainder.
- A `cursor` that is **not a token this tool issued** is `invalid_args` — never a silent reset to page 1, which would
  page a caller over page 1 forever. That set is wider than "does not decode": the token must also decode to a
  canonical six-part entity ID, because arbitrary base64 decodes fine and would land as a keyset position somewhere
  in or outside the match.

Cost, stated plainly: NATS KV has no ranged scan, so each page is a full filtered key scan that is freshly sorted
and then sliced. Paging is O(N) per page — the same profile `graph.query.prefix` accepts today, here over identities
only and bounded by `limit` (max 100).

### Binding requirement

`query_by_type` needs a KV binding that implements `KVKeyLister`. The production adapter
(`graphQueryKVAdapter`) does, through `graph.CatalogReader.ListKeysFiltered`. A binding that does not returns a
classified **internal error naming the binding** — never an empty listing, which would be a positive signal
("nothing of that type exists") the tool never established.

---

## Checklist

- [ ] Any prompt, persona, or rule that told an agent to use `direction: incoming` or `direction: both` on
      `query_relationships` — remove it, or route the question to `graph.query.relationships`.
- [ ] Any consumer parsing `query_by_type`'s `entities` / `note` / `suggested_ids` — switch to `entity_ids`.
- [ ] Any consumer that treated every `query_relationships` row as an edge — the literal-object rows now live in
      `predicates_present` with `"kind": "property"`.
- [ ] Any agent passing `filter_type` to `query_neighbors` and relying on the old (inert) behaviour of getting
      everything — it now filters.
- [ ] Nothing to configure.
