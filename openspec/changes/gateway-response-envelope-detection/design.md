# Design — gateway response envelope detection

## Context

`handleNATSResponseWithExtensions` in `gateway/graph-gateway/component.go` projects an internal NATS
query response into a GraphQL field value. Between "response arrived" and "write it out" it performs
shape adjustments, and one of them is gated on a subject prefix:

```go
// Unwrap QueryResponse envelope for graph.index.query.* subjects
// These handlers return QueryResponse[T] with {data: T, error: string, timestamp: time}
if strings.HasPrefix(subject, "graph.index.query.") { ... }

// Unwrap entities envelope for collection responses (e.g. graph.query.prefix)
if subject == "graph.query.prefix" { ... }
```

Two families are routed through this function — `graph.index.query.*` (4 subjects, gated) and
`graph.query.*` (~15 subjects, not gated) — and the envelope is not a property of either family.

**Constraints already fixed before this design starts.** Envelope *detection*, not prefix-append
(owner/Fable, recorded on gh#762). Breaking for adapted consumers, so it lands inside the
v1.0.0-beta.159 lockstep wave and not after it. The gh#768 stage is this fix's merge gate and must be
RED against main before the fix.

**Stakeholders**: `semsource`, `semdragon`, `semconnect` consume the GraphQL surface.

## Goals / Non-Goals

**Goals.** One shape rule for every query family, expressed as a property of the response.
`data.<field>.data.*` becomes `data.<field>.*` uniformly. A new query family inherits correct shape
with no gateway edit. The class becomes visible to CI.

**Non-Goals.** Changing `graph.QueryResponse[T]`. Delivering the deferred GraphQL readiness field.
Migrating sisters. Retiring the `graph.query.prefix` path. Reworking the GraphQL schema.

## Decisions

### D1 — Discriminate on the exact marshalled key set, not on the presence of `data`

`graph.QueryResponse[T]` marshals to exactly:

```go
type QueryResponse[T any] struct {
    Data      T         `json:"data"`
    RequestID string    `json:"request_id,omitempty"`
    Timestamp time.Time `json:"timestamp"`
}
```

A response is the envelope **iff** its top-level object has `data` AND `timestamp`, and every key is
drawn from `{data, request_id, timestamp}`. Anything else passes through untouched.

*Why not `has("data")`.* That is the whole hazard of detection: any payload that legitimately carries
a top-level `data` field would be silently stripped of one nesting level, converting a cosmetic bug
into data loss. `timestamp` is **not** `omitempty`, so a real envelope always carries it — it is a
free, always-present discriminator. Requiring the key set to be closed also means a payload that
happens to have `data` + `timestamp` *plus* other fields is not an envelope and is left alone.

*Alternative considered — have producers tag the envelope* (`"kind":"query_response"`). Strictly more
robust, and rejected for this change: it is a wire change across every producer and every adopter,
i.e. a second breaking change to solve a problem the closed key set already solves. Worth revisiting
if a real collision is ever found; D1's test makes that discoverable.

### D2 — The detector lives next to the type it describes, in `graph`, not in the gateway

`graph/query_contracts.go` owns the envelope. The predicate that recognizes it goes there
(`graph.IsQueryResponseEnvelope` / `graph.UnwrapQueryResponse` or similar), and the gateway calls it.

*Why.* A detector in the gateway is a second, independent description of a shape defined elsewhere —
the same drift that produced this bug, relocated. Co-locating means a field added to `QueryResponse`
lands beside the predicate that must account for it. The `hasStatefulPrefix` consolidation in gh#731
is the in-repo precedent: one list, two readers.

### D3 — Unwrap exactly once, never iteratively

If the unwrapped `Data` is itself envelope-shaped, it is returned as-is.

*Why.* Looping "while it looks like an envelope" would strip legitimate nesting from any payload whose
own data happens to match, and the depth of unwrapping would then depend on user data. One envelope
is applied by the producer; one is removed here. Symmetry is the rule, not shape-chasing.

### D4 — Detection runs before the `graph.query.prefix` special case, and cannot claim it

`PrefixQueryResponse` is `{entities, next_cursor}` — its own struct, not a `QueryResponse[T]`. It
fails D1's discriminator on both required keys, so ordering is not load-bearing for correctness, but
the sequence is fixed and **tested** rather than left to reading: a future field addition must break a
test, not a deployment.

### D5 — Delete the dead error branch; do not repair it

The unwrap declares `Error string` and errors out when non-empty. `QueryResponse[T]` **has no `error`
field**, so the branch has never fired, and the comment describing `{data, error, timestamp}`
documents a shape that does not exist. Errors reach the gateway by the natsclient convention — a
response body prefixed `error: ` — which is handled elsewhere.

*Why delete rather than wire up.* This is the phantom class (a branch with no producer): wiring it
would invent an error channel no producer emits. The correct disposition is deletion plus a comment
naming the real error path. **Before deleting, confirm by grep that no producer sets an `error` key on
this envelope** — a phantom is only phantom once the producer side is checked, not once the type is
read.

### D6 — The e2e stage asserts shape, and proves itself by failing

A stage that reads through both shapes is worthless; that permissiveness is why this bug survived
weeks. The stage asserts *exact* top-level fields for representative subjects in both families and
explicitly asserts the **absence** of `data.data.*`. Its falsifiability is a deliverable: recorded RED
against main before the fix, green after. A stage that has never been seen red is not a gate.

## Risks / Trade-offs

- **A current payload collides with D1's discriminator** (top-level `data` + `timestamp`, no other
  keys, but not an envelope) → enumerate the actual marshalled shape of every subject routed through
  this function and assert non-collision in a table-driven test. This is the one risk that silently
  loses data, so it is discharged by enumeration, not by reasoning about likelihood.
- **A subject is fixed that no adopter knew was broken**, so a consumer "correctly" reading
  `data.<field>.data.*` breaks → intended and unavoidable; it is the breaking change. Mitigated by the
  adopter note enumerating every affected field and by landing inside the lockstep wave.
- **`graph.query.capabilities` has no producer-side registration** → confirm during implementation
  whether it is a dead route or a gap. If dead, delete it (grep-for-the-consumer); if a gap, file it.
  Do not "fix" it inside this change.
- **A wire-level regression escapes struct-level tests** → both shapes unmarshal cleanly into a
  permissive target, so assertions are made against raw JSON keys, never against a decoded struct.
  gh#762 says this explicitly and it is the single most repeatable way to get a false green here.

## Migration Plan

1. Land detector + gateway change + e2e stage + adopter note in one PR (PR scope = complete system).
2. Record the stage RED against main, then green with the fix, in the PR body.
3. Merge inside the .159 wave; the tag activates adopter migration alongside gh#753.
4. **Rollback**: revert the gateway call site — the `graph` detector is additive and harmless on its
   own. Rollback is only clean *before* the tag; after it, adopters have conformed and reverting
   becomes a second breaking change. This is why it precedes the tag rather than following it.

## Open Questions

1. **Which e2e tier owns the shape stage** — `core` (cheapest, runs everywhere, gateway is core
   surface) vs `structural`. Leaning `core`: the assertion needs no inference tier, and a gate that
   runs on every tier invocation catches more.
2. **Does the adopter note enumerate affected GraphQL fields exhaustively, or state the rule?**
   Leaning exhaustive — an adopter diffing their client wants the list, and the list is derivable
   once. To be confirmed with the enumeration from D1's risk mitigation, which produces it anyway.
3. **`request_id` is `omitempty` and unused by producers** — none of the enumerated call sites set it.
   Possible fourth phantom; out of scope here, worth a follow-up issue rather than a silent fix.
