# Lifecycle Harness Prime — Bucket Ownership Design Exercise

**Status**: Proposed — 2026-05-28. Pre-ADR (ADR-047-prime).
Triggered by the post-beta.85 architectural review session that
surfaced graph-invisible workflow state in the shipped harness.

**Revisions**:
- 2026-05-28 (initial): bucket-ownership rubric + Path A/C framing
- 2026-05-28 (rev 1): folded
  [`lifecycle-harness-prime-consumer-sketches.md`](lifecycle-harness-prime-consumer-sketches.md)
  evidence + corrected the CAS engine-work picture (the gap is
  smaller than initially described — see "Engine work" section)

**Gate**: This document is the design-exercise artifact. Until it
lands on `main`, no further beta tags advance from beta.85, no audit
infrastructure is added to MISSIONS, and no error-message polish is
shipped on the bucket-not-found path — any of those would lock in
the parallel-bucket architecture this exercise is examining.

**Supersedes** (potentially): ADR-047's "Manager owns per-workflow
KV bucket" architectural choice. If the design exercise concludes
the choice was wrong, ADR-047 is amended into ADR-047-prime; the
beta.85 bundle becomes v0 of the harness.

## Summary

ADR-047 shipped a Lifecycle harness that owns one KV bucket per
workflow type (MISSIONS, CSAPI_SYSTEMS, etc.). The harness writes
the Participant struct as a JSON blob; Manager.Get/Update/Transition
round-trip the blob through that private bucket. This design choice
felt clean ("apps own their state schema") but in honest review it
creates **graph-invisible workflow state** — the lifecycle-managed
entity's phase never lands as a triple in ENTITY_STATES, so the
graph layer has no idea the workflow exists. Three of the four
gaps surfaced in beta.85's e2e build (audit story, bucket
provisioning, History source attribution) trace back to this single
architectural choice; the fourth (operator gateway shape duplication)
also flows from it.

This exercise examines whether the right shape was instead:
**Manager as a schema-and-discipline layer over ENTITY_STATES, with
no private bucket — state changes emit Graphables through
graph-ingest like every other write in the system**.

The central question: **when does workflow state legitimately
deserve its own bucket, and when is reaching for one an
optimization that introduces split-brain risk?**

**The consumer-sketches companion document** (see
[`lifecycle-harness-prime-consumer-sketches.md`](lifecycle-harness-prime-consumer-sketches.md))
characterized five candidate consumers — drone survey, semspec
dev-via-spec, manufacturing batch, semconnect API request, sensor
lifecycle — across ten axes. **Four of five are Tier-B coordination
roots with subtrees; NONE are Tier-A in the thin id+phase sense
beta.85 was designed around.** Per-consumer evidence strongly
supports the schema-as-discipline-layer redesign. semconnect API
request is the outlier — Tier C or doesn't belong in the harness
at all (high-volume short-lived leaf computation).

If the harness redesign is judged cleaner, ADR-047 is amended and
the beta.85 bundle is treated as v0 — semteams hasn't adopted, the
revision cost is bounded, and the cost of carrying the wrong
substrate into semspec migration is unbounded.

## Background

### How we got here

The post-beta.85 review session walked through the bundle's e2e
gate and surfaced four gaps in sequence:

1. **Manager.History always returns `Triggered=framework`** — the
   field is exposed in the API but the implementation always
   populates it with one constant value (TODO in
   `manager_query.go::History`).
2. **MISSIONS bucket must be operator-provisioned** before
   `Manager.Register` works, with a "bucket not found" error that
   doesn't tell you how to fix it.
3. **The History endpoint is a thin phase-change filter over KV
   revisions** — it duplicates information the underlying KV layer
   already provides, with the source-attribution claim being the
   only real value-add (and that claim is currently a lie).
4. **The deeper finding** (user pushback): mission state lives in
   MISSIONS bucket; nothing emits Graphables for phase changes;
   the graph layer is blind to lifecycle state.

Tracing concretely through the beta.85 e2e scenario:

- UDP `mission.command=launch` → mission-command processor emits
  Graphable → graph-ingest writes `mission.command=launch` triple
  to ENTITY_STATES ✓
- Rule fires on `triple.mission.command=launch` →
  `lifecycle_transition` → `Manager.Transition` writes new phase
  to MISSIONS bucket ✓
- **graph-ingest sees nothing. `mission.phase=flying` never lands
  as a triple.** ENTITY_STATES has the command that triggered the
  transition but does NOT have the transition's result.

The consequences:

- A graph query "show me all missions in flying" returns nothing
- A rule condition `triple.mission.phase=flying` never fires
- Community detection / inference / BM25 treat mission state as
  if it doesn't exist
- An operator looking via graph-gateway sees an entity with one
  command triple and no phase; via lifecycle-gateway sees
  phase=flying. **The graph view and the lifecycle view disagree
  about what the entity IS.**

The framework's central claim — "semstreams is a knowledge graph
engine" — has an asterisk: "*except for lifecycle-managed
workflows, which live in their own private universe.*"

### Why this matters more than the per-gap framing suggested

Each individual gap looked like normal engineering work to close
(add an audit bucket; improve an error message; persist
source attribution). Closing them individually would have
deepened the parallel-bucket commitment. Carrying that commitment
into semspec migration (their workflow/ package is ~7,840 LOC,
exactly the case ADR-047 was built to retire) would have
reproduced the original failure mode in inverted form: instead of
a hand-rolled workflow engine bypassing the rules engine, a
framework-provided harness bypassing the knowledge graph.

The right time to examine the bucket-ownership choice is BEFORE
any external consumer pins the substrate.

### The discipline lesson worth crystallizing

> **Reaching for your own bucket is easy. It happens too often.
> The framework should make the better pattern easier than the
> lazy pattern.**

The bucket pattern feels architecturally clean ("apps own their
state"), but it has hidden compound costs that compound across
consumers:

- **Graph invisibility** of the private state
- **Parallel audit story** (the History problem)
- **Provisioning burden** per consumer (the bucket-not-found gap)
- **Duplicate gateway shapes** (lifecycle-gateway parallel to graph-gateway)
- **Drift risk** between private store and graph view

Multiple subsystems in the codebase have already reached for
private buckets:

- `AGENT_LOOPS` (agent trajectories) — defensible: high-volume
  per-loop traces that don't belong in the graph
- `MISSIONS` and other workflow-type buckets (lifecycle harness) —
  examining now
- `semspec/workflow/` (~7,840 LOC of hand-rolled) — cautionary tale
- semteams gather state (pre-bundle) — same shape

If the framework's default discipline becomes "no private bucket
unless you can defend it on the rubric — and your defense must
show graph invisibility is acceptable for this state," fewer apps
reach for it casually.

## Central question

**Should `pkg/lifecycle.Manager` own per-workflow KV buckets
(the beta.85 design), or should it be a schema-and-discipline
layer over the standard ENTITY_STATES write path with no private
bucket?**

This question is open. The exercise's job is to answer it from
evidence (the bucket-ownership rubric applied to each Participant
field, the engine work each option implies, the migration cost) —
not from preserving the shipped design.

## The bucket-ownership rubric

Working backwards from architectural principles, when does state
deserve its own bucket?

### Own a bucket when

1. **CAS atomicity over multi-field state** that can't be expressed
   as a single triple-batch write. (Caveat: AddTriplesBatch IS
   atomic per-entity. The bar for "can't be expressed" is high.)
2. **State semantics is *replace* with strict ordering**, not
   *accumulate*. (Caveat: ENTITY_STATES already has per-predicate
   latest-wins for replace semantics.)
3. **Retention/topology genuinely differs from the graph** — long
   compliance retention, controlled max-bytes, different
   replication policy.
4. **Write rate would dominate or pollute the graph** if mixed in.
   (Agent trajectories qualify: thousands of trace records per
   loop. Workflow state typically does NOT qualify: a few
   transitions per entity over hours/days.)
5. **The data genuinely doesn't belong in the graph** — bulky
   payloads (use ObjectStore), throwaway state (no), cross-cutting
   concerns (audit, metrics — debatable).

### Live in ENTITY_STATES (via Graphable emission through graph-ingest) when

1. The data IS facts that the graph layer should reason over
2. You want graph queries / inference / community detection to see
   the state
3. You want the graph's existing revision history to provide audit
   for free
4. Multiple consumer surfaces (rule conditions, GraphQL queries,
   operator dashboards, inference layers) need to read the same
   state through their natural interface
5. You don't need fine-grained CAS that graph-ingest's batched
   write can't provide

### Applying the rubric to mission state

Per-field analysis on the beta.85 mission Participant:

| Field | Behavior | Bucket case | Graph case |
|---|---|---|---|
| `phase` | replaces (planning → flying → completed) | Multi-field atomicity with other lifecycle fields | Per-predicate latest-wins already exists; rules ALREADY match on `triple.mission.phase` (or want to); inference wants to see mission-phase distribution |
| `owner_org_id` | metadata, replaces | None | Pure triple territory |
| `note` | metadata, replaces | None | Pure triple territory |
| (last) `transition_source` | metadata, replaces | None | Pure triple territory |
| (last) `transition_at` | timestamp, replaces | None | Pure triple territory |
| Audit (full history) | append-only, long retention | Real argument: retention differs from graph; cross-entity scan demand | Counter: graph already keeps revision history; "long retention" is operator-controlled per bucket regardless of which bucket; cross-entity scan is rare and addressable |

The only field with even a *plausible* bucket-needs argument is
`phase` (and only via atomicity, which is solvable). Every other
field belongs as triples on the entity. Audit lives naturally
in the entity's KV revision history.

**The honest read: there is no Participant field for mission-shape
workflows that defends a private bucket on the rubric.**

### What the consumer sketches add to the rubric

Beta.85's mission Participant is a thin Tier-A fixture (~5
fields). Real consumers are Tier-B coordination roots — they own
relationships, child workflow instances, accumulated findings,
artifact refs. Per the consumer sketches:

| Consumer | Tier | Why |
|---|---|---|
| Drone survey | B | Owns drone+area refs, capture session sub-workflows, anomaly log, artifact refs |
| semspec dev-via-spec | B | Owns work-unit sub-workflows, agent loop refs, findings (graph-first knowledge), artifact refs |
| Manufacturing batch | B | Owns per-unit sub-workflows, material lot refs, quality records, output artifacts |
| semconnect API request | C or skip | Short-lived high-volume leaf; doesn't fit harness shape |
| Sensor lifecycle | A or B | Owns location ref, calibration history, maintenance history |

Tier-B consumers have additional rubric demands beyond the
per-field analysis above:

- **Relationships** (mission → drone, mission → area, batch →
  material lot) are first-class predicates that the graph layer's
  reverse-index already supports. Holding them in a private bucket
  loses traversal capability.
- **Findings** (semspec's accumulated graph knowledge,
  manufacturing's quality measurements, drone's anomaly log) ARE
  what the graph layer is for. Hiding them in a private bucket
  defeats the whole purpose.
- **Child workflow instances** (per-unit sub-workflows, per-sensor
  capture sessions, work units under a dev-mission) want recursive
  Participant declarations. The schema layer needs to express
  child-workflow relationships, not just phase enums.

The rubric analysis stays the same; the consumer evidence
strengthens the conclusion. **For mission-shape workflows
(Tier B), the bucket-ownership rubric says no private bucket
on every axis — and the Tier-B-specific demands (relationships,
findings, children) STRENGTHEN that conclusion rather than
complicating it.**

## The proposed redesign — harness as discipline layer over ENTITY_STATES

### Core architecture

`Manager` is a **schema-and-discipline layer**. It declares what a
workflow IS (phases, transitions, operator-writable predicates,
identity pattern) and provides the protocol for changing entity
state correctly. It does NOT own data; it owns the *contract* for
changing data.

```go
// Register declares the workflow schema. No bucket allocated.
// No KV resource consumed at registration time beyond the schema map.
mgr.Register(Workflow{
    Name: "mission",
    EntityIDPattern: "*.lifecycle.gcs.mission.*",
    Phases: []string{"planning", "flying", "completed", "aborted"},
    Transitions: lifecycle.Transitions{
        "planning":  {"flying", "aborted"},
        "flying":    {"completed", "aborted"},
        "completed": {},
        "aborted":   {},
    },
    PhasePredicate: "mission.phase",
    OperatorWritablePredicates: []string{
        "mission.owner_org_id",
        "mission.note",
    },

    // NEW (added per consumer-sketch evidence): Tier-B consumers
    // own subtrees + reference other entities. Declaring these
    // up-front lets the operator API surface "mission + children +
    // referenced entities" as a single composed graph query.

    // ChildWorkflows declares workflow types that exist as children
    // of this workflow, with the predicate that establishes the
    // relationship. Manager can recursively load child state, and
    // operator queries can render the full subtree.
    ChildWorkflows: []ChildSpec{
        {Workflow: "capture_session", LinkPredicate: "mission.owns_session"},
        {Workflow: "preflight_check", LinkPredicate: "mission.owns_check"},
    },

    // ReferencePredicates declares predicates that link this
    // workflow to other entities WITHOUT lifecycle ownership
    // (the referenced entity has its own lifetime). Operator
    // queries can render the relationships; rule conditions can
    // traverse them; analytics can aggregate across them.
    ReferencePredicates: []ReferenceSpec{
        {Predicate: "mission.assigned_drone", TargetPattern: "*.fleet.drone.*"},
        {Predicate: "mission.target_area", TargetPattern: "*.geo.area.*"},
    },

    Schema: reflect.TypeOf(MissionState{}),
})

// Get reads triples from ENTITY_STATES (standard graph query path)
// and projects them into the typed Participant struct using the
// Schema. Read is a query, not a private-store lookup.
state, err := mgr.Get(ctx, "mission", "c360.test.lifecycle.gcs.mission.m001")

// Transition is a high-level protocol operation:
//   1. Read current entity from ENTITY_STATES
//   2. Validate proposed transition against Transitions table
//   3. Emit a Graphable containing the new state triples atomically
//   4. graph-ingest writes the batch
err := mgr.Transition(ctx, "mission", entityID, "flying",
    TransitionSourceRule, "command=launch")
// Internally emits Graphable with:
//   - mission.phase: "flying"
//   - mission.last_transition_source: "rule"
//   - mission.last_transition_at: 2026-05-28T15:08:43Z
//   - mission.last_transition_note: "command=launch"
//   - mission.last_transition_from: "planning"
// graph-ingest writes all five triples in one atomic batch.

// History reads ENTITY_STATES revisions for the entity, filters to
// phase-changing writes (writes where mission.phase moved). Source
// attribution comes from the triples themselves (last_transition_*
// fields stamped on each write). No parallel audit store.
events, err := mgr.History(ctx, "mission", entityID)
```

### What the harness still provides

- **Schema declaration** — the workflow IS-a contract, in code, at startup
- **Transition validation** — read current, validate against table, emit
- **Operator-write protection** — the operator API accepts patches only on
  predicates declared `OperatorWritablePredicates`
- **Typed projection** — reads project triples into the Participant struct
  using the Schema reflection map
- **Restart recovery** — implicit (state lives in ENTITY_STATES which is
  already durable; nothing additional to recover)
- **Operator API surface** — `/workflows/{type}/{id}` etc., but now as a
  workflow-aware projection over graph-gateway's existing surface, not
  a parallel HTTP API
- **History** — KV-revision-replay over ENTITY_STATES filtered to
  phase-change events

### What the harness STOPS providing

- Per-workflow KV bucket ownership (no MISSIONS bucket)
- A separate write path (no `kvNATSStore.Update`; emission goes through
  the same NATS subject graph-ingest reads)
- A parallel audit store (no MISSIONS_AUDIT; audit is the graph's
  revision history)
- A standalone gateway component (lifecycle-gateway becomes a thin
  workflow-aware filter over graph-gateway, OR is retired in favor
  of graph-gateway with workflow-aware query helpers)

### What the operator gets

- `GET /workflows` — lists registered workflow types + per-phase counts
  (computed from a graph query, not a bucket scan)
- `GET /workflows/{type}/{id}` — the entity projected as the Participant
  struct (graph query → reflection projection)
- `GET /workflows/{type}/{id}/history` — KV revisions filtered to phase
  changes, with source attribution intact (it's in the triples)
- `POST /workflows/{type}/{id}/state` — validated patch → emission
- `POST /workflows/{type}/{id}/transition` — validated transition →
  emission
- `GET /workflows/{type}/{id}?stream=true` — WebSocket subscribing to
  ENTITY_STATES KV watch on the entity's key, filtered to
  phase-relevant changes

Same operator UX. Different substrate.

## The CAS / atomicity story

This was the strongest objection to the redesign. On honest
re-examination, the CAS infrastructure is mostly already there
and the gap is smaller than initially described.

### What graph-ingest already provides

CAS infrastructure exists three layers deep:

1. **`natsclient.KVStore.UpdateWithRetry`** — automatic
   merge-with-retry on conflict. Used everywhere in graph-ingest.
2. **`graph-ingest.updateEntityAtRevision(entity, expectedRev)`**
   — internal CAS-protected primitive at
   `processor/graph-ingest/component.go:1186`. Takes an expected
   revision, calls `KVStore.Update` with that rev, returns
   `ErrKVRevisionMismatch` on conflict. Used today by the entity
   mutation handlers.
3. **`graph-ingest.handleEntityUpdate`** (the `UpdateEntityRequest`
   handler) — uses the pattern: `fetchEntityState → updateEntityAtRevision(currentRev)`.
   Returns "entity not found (concurrent modification or delete)"
   if anything changed between fetch and write. This IS the
   CAS-on-condition shape, but the caller doesn't get to specify
   "I expected phase=planning" — the handler internally captures
   the current rev before write.

Additionally:

- **`AddTriplesBatch` is atomic per-entity** — one CAS per
  distinct `triple.Subject`. All triples for one entity land or
  none do. Readers see consistent state.
- **Multi-field updates are achievable** — emit one Graphable
  with all the triples for the transition (phase + last_transition_*
  fields), graph-ingest writes them atomically.
- **Reader consistency** — every read is one `KV.Get` on the
  entity key; partial-batch state cannot be observed.

### What graph-ingest does NOT currently provide

Two request-types in graph-ingest have different CAS semantics:

- **`AddTriplesBatch`**: merge-with-retry via internal
  `UpdateWithRetry`. **Always** succeeds in merging new triples
  into current state. Cannot reject on "current state isn't what
  I expected." Correct for the typical "facts accumulate" graph
  model.
- **`UpdateEntity`**: read-then-write-at-rev. CAS-on-condition,
  BUT writes the **whole** entity blob — clobbers other triples
  added by other writers between Manager's read and write
  (e.g. mission-command processor adding `mission.command`
  triple to the same entity).
- **`UpdateEntityWithTriples`**: delta semantics (AddTriples +
  RemoveTriples lists) BUT uses internal `UpdateWithRetry` —
  silently merges-on-conflict. Manager can't say "fail if phase
  has moved."

**The actual missing piece**: an `ExpectedRevision` field on
`UpdateEntityWithTriplesRequest`. When set, the handler uses
the existing `updateEntityAtRevision` primitive (single-pass CAS
at the given rev); when zero, current behavior (internal
`UpdateWithRetry`). The shape:

```go
type UpdateEntityWithTriplesRequest struct {
    Entity            *EntityState     `json:"entity"`
    AddTriples        []message.Triple `json:"add_triples,omitempty"`
    RemoveTriples     []string         `json:"remove_triples,omitempty"`
    ExpectedRevision  uint64           `json:"expected_revision,omitempty"` // NEW
    TraceID           string           `json:"trace_id,omitempty"`
    RequestID         string           `json:"request_id,omitempty"`
}
```

Handler branches on `ExpectedRevision != 0`. Existing callers
ignore the field; no breaking change. Scope: ~20-30 LOC +
round-trip tests.

### The race this prevents

Without CAS-on-condition on the delta path:

- R1 fires `lifecycle_transition(planning → flying)`; reads
  phase=planning, validates.
- R2 fires `lifecycle_transition(planning → aborted)` concurrently;
  reads phase=planning, validates.
- Both submit deltas via `UpdateEntityWithTriples`. Internal
  `UpdateWithRetry` makes both eventually succeed; whichever
  writes last wins on phase.
- **Audit shows both transitions happened from the same start
  state** — a state-machine inconsistency.

With CAS-on-condition:

- R1 writes successfully (rev N → rev N+1).
- R2 writes with `ExpectedRevision=N`; gets `ErrKVRevisionMismatch`.
- Manager.Transition for R2 retries the read-validate loop, sees
  phase=flying, recognizes planning→aborted is no longer valid
  from current state, returns `ErrInvalidTransition`.
- State-machine semantics preserved; audit shows only the
  transition that actually fired.

For the consumer write rates in the sketches (handfuls per hour
for typical lifecycle workflows), this race is unlikely but not
impossible. For state-machine correctness it's required.

### Three paths to handle this

**Path A: Manager uses `UpdateEntityWithTriples` + `ExpectedRevision` (small engine work)**

`Manager.Transition` does read-validate-write-at-rev with
optimistic retry:

```go
for retries := 0; retries < N; retries++ {
    entity, rev := readEntity(entityID)              // standard graph read
    if !valid(entity.Phase, newPhase) {
        return ErrInvalidTransition
    }
    err := updateEntityWithTriples(updated-entity,
        AddTriples:       transitionTriples,
        ExpectedRevision: rev,                       // <-- the new bit
    )
    if errors.Is(err, ErrKVRevisionMismatch) { continue }
    return err
}
```

Requires the small engine change above (~20-30 LOC). CAS
infrastructure beneath is already there.

**Path B: Per-entity serialization at the Manager layer**

`Manager` maintains an in-process keyed-mutex (`sync.Map` of
`*sync.Mutex` keyed by entityID) so concurrent transitions on
the same entity serialize. Cross-process serialization still
needs Path A. Combine with A for the multi-replica case if
keyed-mutex helps reduce CAS conflicts.

**Path C: Treat lifecycle as the rare case that DOES need a
private bucket** (the existing beta.85 design, but with explicit
justification rather than as default)

If we judge that even the small engine work is out-of-scope,
lifecycle becomes the exceptional case that DOES justify a
private bucket on rubric item #1 (CAS atomicity). BUT — and
this is the load-bearing follow-up — even when the harness owns
a private bucket, it MUST also emit Graphables for graph
visibility. Two writes, framework-coordinated: the private CAS
bucket is source-of-truth for transition validation; the
ENTITY_STATES emission gives the graph layer the visibility it
needs.

Path C is hybrid. It accepts the parallel-state cost as the
price of CAS rigor, but eliminates the graph-invisibility cost.

**Recommendation**: Path A. The engine work is tiny (~20-30 LOC
to expose a primitive that already exists internally), the
optimistic-retry pattern is well-trodden, and it eliminates the
parallel-state cost entirely. The consumer-sketch write rates
all support optimistic concurrency without contention concerns.

## Engine work this implies

If Path A:

1. **`UpdateEntityWithTriplesRequest.ExpectedRevision`** — new
   optional field. Handler branches: if non-zero, route through
   the existing `updateEntityAtRevision` primitive (single-pass
   CAS at that rev, returns `ErrKVRevisionMismatch` on mismatch);
   if zero, current behavior (`UpdateWithRetry` merge-with-retry).
   **~20-30 LOC + round-trip tests.** Existing callers unaffected.
2. **Manager.emit helper** — small helper in `pkg/lifecycle` that
   constructs the `UpdateEntityWithTriplesRequest` from the
   current Participant struct + transition triples and
   request/replies on the graph-mutation subject. ~50 LOC.
3. **Schema reflection projection** — Manager projects triples
   into the Participant struct using the registered Schema type
   on every Get. Inverse of the current struct-to-blob marshal.
   **~100 LOC** + tests.
4. **Workflow schema additions** — `ChildSpec` and `ReferenceSpec`
   shapes per the schema-declaration section above.
   `Manager.Children(parentID)` / `Manager.References(entityID)`
   helpers traverse the declared predicates via the standard graph
   query path. **~80 LOC** + tests.
5. **lifecycle-gateway component** — refactor to projection-over-graph
   pattern. Endpoints become workflow-aware composed graph queries:
   `GET /workflows/{type}` is a graph query for entities matching
   the workflow's `EntityIDPattern`; `GET /workflows/{type}/{id}`
   reads the entity + child workflows + referenced entities via
   the schema's `ChildWorkflows`/`ReferencePredicates`; `GET
   .../history` reads ENTITY_STATES revisions filtered to
   phase-change events. **~300 LOC** (smaller than the current
   gateway because the heavy lifting is in graph-gateway already).
6. **Migration tooling** — for any pre-beta.85 adopters with data
   in MISSIONS bucket, a one-shot migration tool that reads the
   bucket and re-emits each entity as triples through graph-ingest.
   Since semteams hasn't adopted, the only adopter is the e2e
   fixture; migration is "delete bucket + reseed."

Estimated scope: **~550-700 LOC code + ~400 LOC tests** +
ADR-047-prime rewrite + concept doc 14 update. The CAS engine
work is the smallest piece by far (~20-30 LOC); most of the
work is the projection layer + gateway refactor + child/reference
schema plumbing.

## The rare exception case (when private bucket IS justified)

Not every state shape fits ENTITY_STATES. Cases where a private
bucket IS the right answer, with the discipline that they MUST be
defended on the rubric:

- **Agent trajectories (AGENT_LOOPS)** — high-volume per-loop
  trace records. Per-iteration LLM-judgment lifecycle, not declared
  state-machine phases. Write rate would dominate the graph
  (thousands of trace updates per loop). Trajectories die with the
  loop (`COMPLETE_*` cleanup). Defensible.
- **Hypothetical: real-time control state with sub-ms read
  requirements** — would need direct KV pinning, bypassing
  graph-ingest's processing path. No current consumer.
- **Hypothetical: cross-shard distributed transactions** — would
  need a dedicated coordination bucket. No current consumer.

The discipline: **if a new subsystem wants to own a bucket, it
files an ADR that defends the choice on the rubric.** Default
answer is "live in ENTITY_STATES via Graphable emission."

## When the harness ISN'T the right shape

The consumer-sketches synthesis surfaced semconnect API request
lifecycle as an outlier: short-lived (ms-to-s), high-volume
(many requests/sec), leaf computation (no children), minimal
state (just phase + input/output). It doesn't benefit from the
harness's value-adds (named persistent instance, restart
recovery, operator API surface). Forcing it into the harness
would either:

- Pollute ENTITY_STATES with millions of short-lived request
  entities (rubric item #4: write rate dominates)
- Require a private bucket exception for API requests (rubric
  item #1: not actually justified — there's nothing about the
  state shape that needs CAS)

The right answer: **the harness is NOT the right shape for this
consumer**. API request lifecycles are better served by:

- JetStream consumer ack semantics for processing state
- Standard request/response logging for audit
- Metrics + tracing for performance observability
- Optional graph-side projection IF a long-lived report-entity
  needs to summarize request flows (rare)

ADR-047-prime should document this with a "When NOT to use the
harness" rubric:

- Lifetime < 1 second → probably not harness
- No relationships to other entities → probably not harness
- No phase transitions with operator meaning → probably not harness
- No restart-recovery requirement → probably not harness
- Volume > 100 instances/sec sustained → almost certainly not
  harness

The discipline mirrors the bucket-ownership rubric: the harness
is opt-in per entity type. Apps choose which entity types are
workflow-shaped and which aren't.

## Migration path

### If Path A (UpdateEntityWithTriples grows ExpectedRevision)

1. **PR 1: `UpdateEntityWithTriplesRequest.ExpectedRevision`** —
   additive optional field; handler branches on non-zero to use
   the existing `updateEntityAtRevision` primitive. Existing
   callers unaffected. ~20-30 LOC + tests. Lands independently;
   tested in isolation.
2. **PR 2: pkg/lifecycle redesign** — Manager.Register schema
   declaration (with new `ChildWorkflows` + `ReferencePredicates`
   fields); Manager.Get/Transition/etc. via graph-read +
   `UpdateEntityWithTriples`-with-`ExpectedRevision` emission;
   Schema reflection projection. Removes `kvNATSStore`-related
   code paths from pkg/lifecycle.
3. **PR 3: lifecycle-gateway refactor to projection-over-graph** —
   endpoints become workflow-aware composed graph queries using
   `ChildWorkflows` + `ReferencePredicates`. `GET /workflows/{type}/{id}`
   loads entity + children + referenced entities in one composed
   query. `GET .../history` reads ENTITY_STATES revisions filtered
   to phase changes.
4. **PR 4: greenfield rip** — delete MISSIONS bucket; delete
   bucket-provisioning code from e2e binary; delete
   `pkg/lifecycle.kvNATSStore`; delete History's
   KV-revision-on-private-bucket path (replaced by
   ENTITY_STATES-revision path). Update mission Participant to
   the projection-based shape.
5. **Tag v1.0.0-beta.86** — single bundle, completes the v0 →
   v1 redesign. ADR-047 amended to ADR-047-prime; beta.85
   retroactively marked v0.

### If Path C (private bucket retained as the rare-case exception)

1. **PR 1: Manager.Transition also emits Graphable for graph
   visibility** — every transition writes BOTH MISSIONS (CAS
   source-of-truth) AND emits triples to ENTITY_STATES via
   graph-ingest. Framework coordinates; consumers don't see the
   two writes.
2. **PR 2: lifecycle-gateway History reads from ENTITY_STATES
   revisions** (the graph's audit trail) rather than reconstructing
   from MISSIONS revisions. Drops the always-`framework` Triggered
   field; sources come from the emitted triples.
3. **PR 3: ADR amendment** — document the hybrid design with
   explicit rubric defense for retaining MISSIONS as CAS source.
4. **Tag v1.0.0-beta.86** — bundles the audit fix + graph
   visibility.

Path C is smaller in scope but locks in parallel state forever.
Path A is larger but is the architecturally complete answer.

## What this means for beta.85

beta.85 is tagged on GitHub. Per [[feedback_never_retag]], the Go
module proxy pins on first fetch — re-tagging is risky. The user
has confirmed semteams (the only realistic consumer) has not
pinned. Pragmatic options:

1. **Leave beta.85 as a v0 milestone tag.** Add a release-note
   amendment ("v0 of lifecycle harness; v1 lands in beta.86 with
   ADR-047-prime"). semteams holds at beta.84.
2. **Force-push the tag onto a beta.86-equivalent commit.** Only
   safe because no one has fetched. Loses semver hygiene.
3. **Yank socially in release notes; let beta.86 be the canonical
   adoption point.** Cleanest; matches what major projects do
   when a tag turns out to need rework.

The user's call. (1) and (3) are conservative; (2) is faster but
abnormal.

## Open questions for the design session

These are the questions the exercise should answer concretely
before ADR-047 is amended:

**Q1: Path A vs Path C? — STRONG LEAN PATH A**
The CAS engine work in graph-ingest is **smaller than initially
described** — it's exposing a primitive that already exists
internally (`updateEntityAtRevision`) via a new optional field
on `UpdateEntityWithTriplesRequest`. ~20-30 LOC. The "engine
work is risky/large" concern that motivated Path C as a fallback
mostly evaporates on closer inspection. Path A is strongly
recommended unless there's a reason not surfaced yet to keep
parallel state. Decision still needs to be made explicitly.

**Q2: What is the lifecycle-gateway's future? — STRONG LEAN PROJECTION**
Per the consumer-sketches synthesis: every operator query across
all five sketched consumers is a graph query in disguise ("all
flying missions", "sensors at zone Z", "findings by mission M",
"material lot → batches"). The current lifecycle-gateway
reinvents a parallel HTTP API. The right shape is **projection
over graph-gateway** — workflow-aware composed queries that use
the schema's `ChildWorkflows` and `ReferencePredicates` to render
the full subtree. Smaller code (~300 LOC vs current ~600+) AND
delivers richer operator UX (full subtree views, cross-entity
queries, relationship traversal — all for free from the graph
layer's existing capabilities).

**Q3: Source-attribution schema in the triple-land world.**
With state in ENTITY_STATES via emission, source attribution
becomes per-triple metadata. Options:
- Stamp `mission.last_transition_source` etc. as triples (proposal
  above) — simple, queryable, slightly verbose
- Use Triple.Source field (already exists) — terse but conflates
  framework provenance with the writing actor
- Add audit predicates per transition (`mission.transition.{seq}.source`)
  — full audit trail but predicate explosion

**Q4: How does Schema reflection projection handle evolution?**
If a workflow adds a new operator-writable predicate, do existing
entities (with no triple for the new predicate) project as
zero-value? Default? Error? Affects forward/backward compatibility.

**Q5: Atomicity of "Create"?**
Manager.Create today writes a fresh Participant blob. In the
ENTITY_STATES world, Create emits multiple triples for an entity
that may already have unrelated triples (from other subsystems).
Is "Create" semantically "I'm declaring the lifecycle dimension
of this entity"? Or does it conflict with existing triples?

**Q6: What about the workflow-not-yet-graph-resident case?**
A workflow instance that exists in MISSIONS but has never been
written to ENTITY_STATES — can this happen? Should Create be the
first write to land triples? Does Get-before-Create fail loudly?

**Q7: Migration for the rare exception case (Path C path).**
If we keep MISSIONS as CAS source-of-truth AND emit to
ENTITY_STATES, how do we recover from a mid-flight crash that
wrote MISSIONS but failed to emit (or vice versa)? Reconciler
job that re-emits from MISSIONS revisions? Two-phase commit?

**Q8: Performance / latency impact.**
Going through graph-ingest adds NATS round-trip latency vs
direct KV write. Per-transition latency goes from ~1ms (direct
KV) to ~5-10ms (NATS publish + graph-ingest CAS + ack).
Acceptable for the typical workflow rate (< 1 transition/sec
per entity), but worth quantifying.

**Q9: Does graph-ingest's single-writer invariant still hold?**
Today graph-ingest is the single writer to ENTITY_STATES. With
lifecycle emitting through it, the invariant still holds (lifecycle
goes through graph-ingest, not around it). Confirm there's no
hidden write path that breaks this.

**Q10: What's the migration guidance for the rare exception case
(future apps that DO need a private bucket)?**
ADR-047-prime should document the rubric and provide a worked
example of "here's how to defend a private bucket on the rubric"
so future ADRs can follow the pattern.

## Reading order for the design session

Read in order before working through the questions:

1. This document (you're here)
2. [`lifecycle-harness-prime-consumer-sketches.md`](lifecycle-harness-prime-consumer-sketches.md)
   — companion analysis: five candidate consumers characterized
   along ten axes; cross-cutting findings inform every
   recommendation in this exercise
3. `project_adr_047_048_bundle_e2e_handoff` memory — the e2e
   build session that surfaced the gaps
4. `docs/adr/047-lifecycle-harness-substrate.md` — the architectural
   choice this exercise is examining
5. `docs/concepts/02-kv-twofer.md` — the architectural principle
   the harness should be honoring
6. `docs/concepts/14-orchestration-layers.md` — the existing
   substrate catalog (currently includes the harness as-shipped)
7. `processor/graph-ingest/mutations.go` (handleEntityUpdate +
   handleEntityUpdateWithTriples) + `component.go:1186`
   (updateEntityAtRevision) — the existing CAS infrastructure
   that Path A exposes via a new `ExpectedRevision` field
8. `pkg/lifecycle/manager.go` — the current Manager impl
9. `pkg/lifecycle/kv_store_nats.go` — the private-store layer this
   would retire
10. CLAUDE.md "Orchestration Boundaries" + "Architectural Identity
    (Not an Event Bus)" sections — re-read the principles with
    fresh eyes; the "single writer to ENTITY_STATES" + "KV twofer"
    discipline are what's at stake
11. The four PR descriptions for beta.85 bundle (#154-#157) — what
    actually shipped

## What this exercise is NOT

- A claim that the lifecycle harness as a concept is wrong (it's
  not — the schema + transitions + operator API surface IS
  framework value-add)
- A claim that ADR-047 was a mistake (the *concept* was right;
  the *substrate choice* was wrong)
- A retrospective on the bundle's other architectural choices
  (BoundedDispatcher, `.triples`, lifecycle_* rule actions — those
  stand independently)
- A breaking change to consumers (semteams hasn't adopted; e2e
  fixture is the only "consumer" today and is internally rewritable)

## Success criteria for the exercise

The exercise lands when:

- [ ] All 10 open questions have a concrete answer (recommendation
  or "defer until X")
- [ ] Path A vs Path C is chosen with documented rationale
  (Path A is the strong pre-exercise lean given the corrected
  engine-work picture; explicit confirmation or pushback required)
- [ ] If Path A: `UpdateEntityWithTriplesRequest.ExpectedRevision`
  design is sketched to PR-shippable detail (handler branching,
  error shape, round-trip test plan)
- [ ] If Path A: Schema reflection projection design is sketched
  to PR-shippable detail (including `ChildSpec` and
  `ReferenceSpec` shapes)
- [ ] lifecycle-gateway disposition is decided (projection over
  graph-gateway is the strong pre-exercise lean)
- [ ] "When the harness ISN'T the right shape" rubric is
  documented in ADR-047-prime
- [ ] ADR-047-prime draft exists at `docs/adr/047-lifecycle-harness-substrate.md`
  (or a sibling `047-prime`) with the chosen design
- [ ] Migration plan for the e2e fixture is concrete
- [ ] Decision on beta.85 disposition (leave / yank / force-tag)
  is made

## Related context

- [`lifecycle-harness-prime-consumer-sketches.md`](lifecycle-harness-prime-consumer-sketches.md)
  — companion analysis informing every recommendation in this
  exercise
- [[project_adr_047_048_bundle_e2e_handoff]] — the e2e session
  that surfaced the gaps
- [[project_adr_047_048_bundle_progress]] — canonical bundle state
- ADR-047 — the architectural choice being examined
- `feedback_warning_not_fail_masks_integration_drift` — the
  discipline that surfaced "always-`framework` Triggered field"
  as a real defect
- `feedback_reactive_patches_vs_engine_completion` — discipline
  to honor; this exercise IS the deliberate completion pass
- `docs/concepts/02-kv-twofer.md` — the architectural principle
  the redesign honors
- `processor/graph-ingest/component.go:1186` (`updateEntityAtRevision`)
  — the existing internal CAS primitive that Path A exposes via
  the new `ExpectedRevision` field
