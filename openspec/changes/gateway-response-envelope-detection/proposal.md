# Gateway response envelope detection

## Why

The GraphQL gateway unwraps the internal `QueryResponse` envelope behind a **prefix gate** that names
exactly one subject family (`graph.index.query.`), so every other subject returning that envelope is
projected with the envelope still on it — a caller reads `data.graphSummary.data.total_entities`
instead of `data.graphSummary.total_entities` (gh#762). The enumeration *is* the bug: a new query
family silently inherits the wrong shape.

Three reasons this is now, and not later. It is the **last breaking item before the
v1.0.0-beta.159 lockstep tag** — tagging without it means adopters conform to the double-nested shape
at .159 and get broken again at .160, buying a second lockstep for one paragraph of code. It is a
**blocker on consumer surface**: `caught-up-readiness-producers` (gh#712/#732) deliberately declined
to add a GraphQL readiness field, because building a new consumer surface on a nesting bug bakes it
in. And the class is **invisible by construction** (gh#768) — no e2e stage asserts response *shape*,
so stages read through whatever shape exists and a regression cannot be seen.

## What Changes

- **BREAKING** — the gateway unwraps the `QueryResponse` envelope by **detecting the envelope on the
  response**, not by matching the subject against a prefix list. Adapted consumers reading
  `data.<field>.data.*` move to `data.<field>.*`. This is breaking for every currently-affected
  subject, which is the point: the shape becomes uniform across query families.
- Detection is **conservative and total**: it must not unwrap a legitimate payload that merely
  carries a top-level `data` field. The envelope is discriminated on its full marshalled shape
  (`data` + always-present `timestamp`, optional `request_id`, and nothing else), not on the presence
  of `data` alone. A payload that fails discrimination is passed through untouched.
- **Retire the dead error branch.** The gateway's unwrap declares
  `struct { Data json.RawMessage; Error string }` and returns a GraphQL error when `Error != ""`. The
  real `graph.QueryResponse[T]` is `{Data, RequestID, Timestamp}` — **it has no `error` field**, so
  that branch has never fired. Errors arrive by the natsclient convention instead (a response body
  prefixed `error: `). The adjacent comment describing `{data: T, error: string, timestamp: time}`
  documents a shape that does not exist.
- **e2e response-shape stage** (gh#768), landing in the same change as its fix and **falsifiable**:
  RED against current main, green after. It asserts exact unwrapped shape — top-level fields, no
  `data.data.*` — for representative subjects in **both** families.
- Adopter note documenting the shape change, per the sister-lockstep obligation.

## Capabilities

- **New Capabilities**: `gateway-response-projection` — how the GraphQL gateway projects internal
  NATS query responses into GraphQL field values, and what a caller may rely on about that shape.
  No existing capability owns this: `graph-query` owns query *strategy* (its Purpose scopes it to the
  semantic path and other strategies as touched), and `graph-index` owns index storage. The gateway's
  projection contract has no home today, which is a contributing cause — an unowned contract is one
  nobody checks.
- **Modified Capabilities**: none. No requirement of `graph-query`, `graph-index`, or
  `graph-index-readiness` changes; this is a gateway projection contract, and the producers keep
  emitting exactly what they emit today.

## Impact

- `gateway/graph-gateway/component.go` — `handleNATSResponseWithExtensions`, the prefix gate at
  `:1720` and the `graph.query.prefix` special case immediately after it.
- **Producers are untouched.** `graph.NewQueryResponse` and every handler keep their current output.
- **Consumers** (sister repos): a shape change on every affected GraphQL field. Guidance published as
  an adopter note; conforming is the adopter's job and further problems arrive as new issues, per the
  task-list residency rule. Activated by the tag, alongside gh#753.
- `test/e2e/` — new shape stage; tier placement decided in design.

Three findings from scoping that constrain the design, recorded here because each one would
otherwise be rediscovered:

1. **Subject enumeration is not statically sound.** `handleQuerySemantic` and `handleQuerySpatial`
   proxy to downstream subjects and `return response, nil` verbatim, so a `graph.index.query.*`
   envelope can surface under a `graph.query.*` subject. Which subjects double-nest is therefore a
   property of the *response*, not of the subject — which is what makes detection correct and
   prefix-append wrong, independent of taste.
2. **The families are not symmetric.** `graph.query.prefix` returns `PrefixQueryResponse`
   (`{entities, next_cursor}`), its own struct and *not* a `QueryResponse[T]`, and it already has a
   separate unwrap path. A blanket prefix widen would corrupt it. Detection must leave it alone.
3. **`graph.query.capabilities` has no producer-side registration** — the gateway routes to a subject
   no handler serves. Either a dead route or a gap; it is confirmed and dispositioned during
   implementation, not assumed.

## Non-goals

- **Not changing the envelope itself.** `graph.QueryResponse[T]` stays as it is; this changes only
  what the gateway does with it at the projection boundary.
- **Not adding the GraphQL readiness field** that gh#712/gh#732 deferred. This unblocks it; it does
  not deliver it.
- **Not migrating sister repos.** Publish the breaking note and the guidance; adopters conform.
- **Not a general GraphQL schema rework**, and not touching the MCP or NATS Direct access patterns.
- **Not retiring the `graph.query.prefix` special case.** It is a genuinely different shape and stays
  a distinct path; folding it in is a separate question.

## Consuming products

`semsource` (lead v1 product, GraphQL consumer), `semdragon` (named in the tag milestone as the
adopter that would otherwise adapt twice), and `semconnect`. All three consume the gateway's GraphQL
surface, which is why the shape change is gated on the lockstep tag rather than shipped piecemeal.
