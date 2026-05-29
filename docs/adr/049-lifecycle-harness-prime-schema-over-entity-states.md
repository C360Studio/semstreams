# ADR-049: Lifecycle Harness Prime — Schema-as-Discipline Layer over ENTITY_STATES

## Status

**Accepted** — 2026-05-29 with the v1.0.0-beta.86 tag. All six e2e
tiers green pre-tag (lifecycle, core cold-start 2/2, structural,
statistical, agentic, semantic 7m05s). Supersedes the per-workflow
KV bucket architectural choice in
[ADR-047](047-lifecycle-harness-substrate.md).

Working name throughout the design exercise: "ADR-047-prime." This
is the canonical numbered ADR; the working name remains useful in
the proposal documents as a continuity marker.

## Context

### What ADR-047 shipped

ADR-047 shipped a Lifecycle harness that owns one KV bucket per
workflow type (MISSIONS, CSAPI_SYSTEMS, etc.). The harness writes
the Participant struct as a JSON blob; Manager.Get/Update/Transition
round-trip the blob through that private bucket. The architecture
felt clean — "apps own their state schema, framework provides the
infrastructure" — and the four-PR bundle (#154 / #155 / #156 / #157)
was tagged as v1.0.0-beta.85.

### What the e2e build surfaced

In the post-bundle e2e build for the lifecycle tier, four gaps
surfaced in sequence:

1. **`Manager.History` always returns `Triggered=framework`** — the
   field is exposed in the API but the implementation always
   populates it with one constant value. The TODO in
   `manager_query.go::History`.
2. **The MISSIONS bucket must be operator-provisioned** before
   `Manager.Register` works, with a "bucket not found" error that
   doesn't tell you how to fix it.
3. **The History endpoint is a thin phase-change filter over KV
   revisions** — it duplicates information the underlying KV layer
   already provides, with the source-attribution claim being the
   only real value-add (and that claim is currently a lie).
4. **Mission state is graph-invisible.** The mission entity's
   phase never lands as a triple in ENTITY_STATES; the graph
   layer has no idea the workflow exists. Concretely in the beta.85
   e2e scenario:
   - UDP `mission.command=launch` → mission-command processor →
     graph-ingest writes `mission.command=launch` triple ✓
   - Rule fires `lifecycle_transition` → `Manager.Transition`
     writes new phase to MISSIONS bucket ✓
   - **graph-ingest sees nothing.** `mission.phase=flying`
     never becomes a triple. A graph query "show me all
     flying missions" returns nothing. A rule condition
     `triple.mission.phase=flying` never fires. The graph
     view and the lifecycle view disagree about what the
     entity IS.

Three of the four gaps trace back to a single architectural choice:
ADR-047's per-workflow KV bucket. Closing them individually would
have deepened the parallel-bucket commitment.

### The discipline lesson worth crystallizing

> **Reaching for your own bucket is easy. It happens too often.
> The framework should make the better pattern easier than the
> lazy pattern.**

Multiple subsystems have reached for private buckets by default:
agent trajectories (AGENT_LOOPS — defensible), workflow harness
(MISSIONS — examined here), semspec's hand-rolled `workflow/`
package (~7,840 LOC — the cautionary tale). The bucket pattern
feels architecturally clean but has hidden compound costs:

- Graph invisibility of the private state
- Parallel audit story
- Provisioning burden per consumer
- Duplicate gateway shapes
- Drift risk between private store and graph view

### Evidence base

This ADR's design choices rest on three companion proposal
documents drafted before this ADR:

- [`docs/proposals/lifecycle-harness-prime-design-exercise.md`](../proposals/lifecycle-harness-prime-design-exercise.md)
  — bucket-ownership rubric + Path A/C framing + amendments
- [`docs/proposals/lifecycle-harness-prime-consumer-sketches.md`](../proposals/lifecycle-harness-prime-consumer-sketches.md)
  — five candidate consumers (drone survey, semspec dev-via-spec,
  manufacturing batch, semconnect API request, sensor lifecycle)
  characterized along ten axes. Finding: **4 of 5 are Tier-B
  coordination roots with subtrees; NONE are Tier-A in the thin
  id+phase sense beta.85 was designed around.**
- [`docs/proposals/lifecycle-harness-prime-projection-sketch.md`](../proposals/lifecycle-harness-prime-projection-sketch.md)
  — concrete API surface + per-operation implementation sketches
  + cost model

The decisions in this ADR were walked through Q1-Q10 (design
exercise) + P1-P4 (projection sketch) before drafting; resolutions
live in the proposal docs.

## Decision

### Core architecture: Manager as schema-and-discipline layer over ENTITY_STATES

`pkg/lifecycle.Manager` is a **schema-and-discipline layer**. It
declares what a workflow IS (phases, transitions, operator-writable
predicates, identity pattern, owned child workflows, referenced
entity predicates) and provides the protocol for changing entity
state correctly. It does NOT own data. It owns the *contract* for
changing data.

State lives in ENTITY_STATES (the knowledge graph). State changes
go through graph-ingest via `UpdateEntityWithTriplesRequest` — the
same write path every other subsystem uses. graph-ingest remains
the single writer to ENTITY_STATES; the harness emits, never
writes directly.

### The bucket-ownership rubric (load-bearing principle)

When a new subsystem considers owning a private KV bucket:

**Own a bucket when:**

1. CAS atomicity over multi-field state that can't be expressed as
   a single triple-batch write. (Bar: high — AddTriplesBatch IS
   atomic per-entity.)
2. State semantics is *replace* with strict ordering that
   per-predicate latest-wins can't satisfy. (Bar: high —
   ENTITY_STATES already has per-predicate latest-wins.)
3. Retention/topology genuinely differs from the graph (long
   compliance retention with different bucket config).
4. Write rate would dominate or pollute the graph if mixed in.
   (Example: agent trajectories — defensible.)
5. The data genuinely doesn't belong in the graph (bulky payloads
   → ObjectStore + ref-triples).

**Live in ENTITY_STATES via Graphable emission when:**

1. The data IS facts the graph should reason over
2. You want graph queries / inference / community detection to
   see the state
3. You want the graph's revision history to provide audit for free
4. Multiple consumer surfaces (rules, GraphQL, dashboards, inference)
   need to read the same state through their natural interface
5. You don't need fine-grained CAS that graph-ingest's batched
   write can't provide

**Default answer: live in ENTITY_STATES.** New subsystems wanting
a private bucket file an ADR that defends the choice on the rubric.
The harness is NOT one of the exception cases (see "Applied to
lifecycle workflows" below).

### Applied to lifecycle workflows: no private bucket

Per-field analysis on a Tier-B mission (the dominant consumer
shape per the sketches):

| Field | Lives as | Rationale |
|---|---|---|
| `phase` | triple in ENTITY_STATES | Per-predicate latest-wins covers replace semantics; rules can match on `triple.mission.phase`; inference sees the distribution |
| `owner_org_id` | triple | Pure metadata |
| `note` | triple | Pure metadata |
| `last_transition_source` etc. | triples (framework-stamped) | Source attribution comes for free in History via revision replay |
| References (drone, area) | triples (entity ID objects) | First-class graph relationships |
| Children (capture_session) | triples (parent.owns_child = child_id) | First-class subtree relationships |
| Audit (full history) | KV revisions on ENTITY_STATES | No parallel audit bucket; operator-controlled bucket history depth |

No field defends a private bucket on the rubric. The harness emits
through graph-ingest like every other subsystem.

### The CAS-on-condition engine primitive

The harness needs the ability to say "write this delta IFF the
entity's current revision is still N" to handle the state-machine
race (two rules concurrently transitioning the same entity from
the same start state). The CAS infrastructure already exists
internally in graph-ingest (`updateEntityAtRevision` at
`processor/graph-ingest/component.go:1186`); it just isn't
exposed on the delta-mutation handler.

Add an optional field to `UpdateEntityWithTriplesRequest`:

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

`handleEntityUpdateWithTriples` branches on `ExpectedRevision != 0`:
non-zero routes through the existing `updateEntityAtRevision`
(single-pass CAS at that rev, returns `ErrKVRevisionMismatch` on
mismatch); zero preserves current behavior (`UpdateWithRetry`
merge-with-retry). Existing callers unaffected.

Scope: ~20-30 LOC + round-trip tests. Additive, no breaking change.

### Schema declaration

```go
mgr.Register(Workflow{
    Name:            "mission",
    EntityIDPattern: "*.lifecycle.gcs.mission.*",
    Phases:          []string{"planning", "flying", "completed", "aborted"},
    Transitions:     missionTransitions,
    PhasePredicate:  "mission.phase",

    Schema: reflect.TypeOf(MissionState{}),

    OperatorWritablePredicates: []string{
        "mission.owner_org_id",
        "mission.note",
    },

    // Framework stamps these on every transition; readable from
    // the entity's triples at each KV revision; History reconstructs
    // the timeline + source attribution from them.
    AuditPredicates: AuditSpec{
        Source: "mission.last_transition_source",
        At:     "mission.last_transition_at",
        From:   "mission.last_transition_from",
        Note:   "mission.last_transition_note",
    },

    // Workflows this workflow OWNS. Each child has its own
    // Participant declaration. The LinkPredicate establishes
    // the parent→child relationship as a triple on the parent.
    ChildWorkflows: []ChildSpec{
        {Workflow: "capture_session", LinkPredicate: "mission.owns_session"},
        {Workflow: "preflight_check", LinkPredicate: "mission.owns_check"},
    },

    // Predicates linking to other entities WITHOUT lifecycle
    // ownership (the target has its own lifetime).
    ReferencePredicates: []ReferenceSpec{
        {Predicate: "mission.assigned_drone", TargetPattern: "*.fleet.drone.*"},
        {Predicate: "mission.target_area",    TargetPattern: "*.geo.area.*"},
    },
})
```

The Participant struct itself uses field tags to map predicates
to struct fields:

```go
type MissionState struct {
    EntityID    string `json:"entity_id"    lifecycle:"id"`
    Phase       string `json:"phase"        lifecycle:"phase,predicate=mission.phase"`
    OwnerOrgID  string `json:"owner_org_id" lifecycle:"operator_writable,predicate=mission.owner_org_id"`
    Note        string `json:"note"         lifecycle:"operator_writable,predicate=mission.note"`

    // References project as scalar entity IDs
    AssignedDroneID string `json:"assigned_drone_id" lifecycle:"reference,predicate=mission.assigned_drone"`
    TargetAreaID    string `json:"target_area_id"    lifecycle:"reference,predicate=mission.target_area"`

    // Audit fields (read-only via operator API)
    LastTransitionSource string    `json:"last_transition_source,omitempty" lifecycle:"readonly,predicate=mission.last_transition_source"`
    LastTransitionAt     time.Time `json:"last_transition_at,omitempty"     lifecycle:"readonly,predicate=mission.last_transition_at"`

    // Children NOT in the projected struct — loaded via
    // Manager.Children(entityID) so per-Get cost stays bounded
}
```

### Manager API surface

```go
// Per-entity (bounded cost)
Get(ctx, workflow, entityID) (Participant, error)
GetWithRevision(ctx, workflow, entityID) (Participant, uint64, error)
GetRaw(ctx, entityID) (graph.EntityState, error)        // debug escape hatch (P1)

// Subtree (operator-controlled)
Children(ctx, parentEntityID, opts ChildOptions) ([]ChildResult, error)  // depth-1 (P2)
References(ctx, entityID) ([]ReferenceStub, error)                       // depth-1 (P3)

// State changes (CAS-on-condition via UpdateEntityWithTriplesRequest)
Create(ctx, initial Participant) error
Transition(ctx, workflow, entityID, newPhase string, source TransitionSource, note string) error
TransitionWith(ctx, workflow, entityID, newPhase string, source TransitionSource, note string,
    mutator func(Participant) error) error
UpdateFromOperator(ctx, workflow, entityID string, patch map[string]any) error
Complete(ctx, workflow, entityID string) error
Fail(ctx, workflow, entityID, reason string) error

// Discovery
List(ctx, workflow, opts ListOptions) ([]Participant, error)
ListWorkflows() []WorkflowDef
GetWorkflowDefinition(workflow string) (WorkflowDef, bool)

// Streaming
Watch(ctx, workflow string) (<-chan Participant, error)  // KV.Watch on ENTITY_STATES filtered by EntityIDPattern

// History — graph revisions filtered to phase changes; source
// attribution reads from the audit predicates stamped at write time
History(ctx, workflow, entityID string) ([]TransitionEvent, error)
```

Implementation sketches for each are in
[`lifecycle-harness-prime-projection-sketch.md`](../proposals/lifecycle-harness-prime-projection-sketch.md).

### Create semantics: "add lifecycle dimension," not "instantiate entity"

`Manager.Create(initial Participant)` declares that an entity is
now lifecycle-managed by adding the initial phase + audit triples.
The entity MAY already exist in ENTITY_STATES with triples from
other processors (e.g. the mission-command processor stamping
`mission.command` before any lifecycle action fires). Create
coexists with those triples — it adds the lifecycle dimension
without clobbering existing data.

Flow:
1. Read current entity (may be empty or have non-lifecycle triples)
2. If the schema's `PhasePredicate` already has a triple →
   return `ErrAlreadyExists` (we don't lifecycle-manage twice)
3. Emit `AddTriples(initial lifecycle triples)` via
   `UpdateEntityWithTriplesRequest` with `ExpectedRevision=currentRev`
4. On CAS conflict, retry the read + check loop

Implications:

- An entity can be referenced (or have triples) before it has a
  lifecycle. Create attaches lifecycle when the workflow needs it.
- ErrAlreadyExists fires iff the phase triple exists — not iff
  the entity exists. This is intentional.
- The beta.85 model ("Create a brand-new entity") is a strict
  subset of this model. Callers wanting beta.85 behavior just
  call Create on an entity that has no triples at all — same UX.

`Manager.Get` on an entity with no phase triple returns
`ErrEntityNotLifecycleManaged` (a distinct error from
`ErrEntityNotFound`, which means the entity has no triples at all).
Operator must explicitly Create before Transition.

> **Note on Q5 framing**: This reframing was tried in this ADR per
> user direction ("try it and ask if it looks forced"). On
> reflection, the framing is more abstract than beta.85's
> Create-or-error semantics but is strictly more capable. The
> "lifecycle attaches to an entity that may already have other
> facts" model handles the forward-reference + processor-stamp-first
> cases naturally that beta.85 handled awkwardly or not at all.
> Honest read: not forced; modestly more abstract. Open to revisit
> if implementation makes it feel artificial.

### lifecycle-gateway: keep, slim, project

The lifecycle-gateway component stays — workflow-awareness (schema,
operator-patch validation against `operator_writable`, transition
validation against the Transitions table) is distinct framework
value that belongs in its own component. But the component slims
considerably:

- Handlers become **thin compositions over Manager primitives**
  (`Get + Children + References` for the composed view)
- No private state model — all reads route through graph-ingest's
  existing read path
- The composed `GET /workflows/{type}/{id}` endpoint returns
  `MissionView{State, References, Children, ChildCount}` with
  pagination on children
- Operator-write endpoints (`POST .../state`,
  `POST .../transition`) validate against the schema then emit
  via Manager.Transition / Manager.UpdateFromOperator
- WebSocket stream watches ENTITY_STATES (via KV.Watch filtered
  by the workflow's EntityIDPattern)
- Estimated size: ~300 LOC (down from current ~600+)

### When the harness is NOT the right shape

Per the consumer-sketches finding, semconnect API request lifecycle
is the outlier: short-lived (ms-to-s), high-volume (many
requests/sec), leaf computation (no children), minimal state.
Forcing it into the harness would either pollute ENTITY_STATES
with millions of short-lived entities OR require a private-bucket
exception that's not actually justified on the rubric.

The harness is NOT the right shape when ANY of these apply:

- Lifetime < ~1 second → probably not harness
- No relationships to other entities → probably not harness
- No phase transitions with operator meaning → probably not harness
- No restart-recovery requirement → probably not harness
- Volume > ~100 instances/sec sustained → almost certainly not harness

Alternatives for these cases: JetStream consumer ack semantics,
standard request/response logging, metrics + tracing, optional
graph-side report-entity projection if a long-lived summary is
needed.

### Defending a private bucket (the rare exception path)

If a new subsystem wants to own a private KV bucket, the discipline
is: **file an ADR that defends the choice on the rubric.**

The reference worked example is **AGENT_LOOPS** (agent trajectories).
The defense:

- **Rubric item #4 — write rate dominates the graph.** Each loop
  emits thousands of trace records per iteration; mixing into
  ENTITY_STATES would dominate the graph's write pattern with
  trace data that isn't graph-relevant.
- **State lifecycle is per-iteration LLM-judgment** (not declared
  state-machine phases). The Participant/Transitions contract
  doesn't fit; the data is fundamentally fragment-of-execution,
  not workflow-instance state.
- **Graph visibility is acceptable to lose.** Trace records are
  inspectable via specific gateway tools (`read_loop_result`)
  rather than graph queries. Operators don't query "show me all
  loops with trace X" via the knowledge graph.
- **Trajectories die with the loop** (`COMPLETE_*` cleanup).
  Retention semantics differ fundamentally from graph audit
  history.

Future ADRs proposing a private bucket should produce a similar
defense.

## Migration

### From beta.85 (private MISSIONS bucket) to beta.86 (schema over ENTITY_STATES)

Five-PR sequence, ~550-700 LOC code + ~400 LOC tests total:

**PR 1: `UpdateEntityWithTriplesRequest.ExpectedRevision`** —
~20-30 LOC. Additive optional field; handler branches on non-zero
to use the existing `updateEntityAtRevision` primitive. Existing
callers unaffected. Lands independently; tested in isolation.

**PR 2: `pkg/lifecycle` redesign** — ~250-300 LOC + tests.
Manager.Register schema declaration with new `ChildWorkflows` +
`ReferencePredicates` + `AuditPredicates` fields; Manager
operations rewritten via graph-read + `UpdateEntityWithTriples`
emission; Schema reflection projection layer. Removes
`kvNATSStore`-related code paths.

**PR 3: `lifecycle-gateway` refactor** — ~150 LOC delta (shrinks
from current). Handlers become thin compositions; composed view
endpoint returns full subtree via Children + References.

**PR 4: greenfield rip** — ~50 LOC delta (mostly deletion). Delete
MISSIONS bucket; delete bucket-provisioning code from e2e binary;
delete `pkg/lifecycle.kvNATSStore`; delete History's
KV-revision-on-private-bucket path. Update e2e mission Participant
to projection-based shape.

**PR 5: Tag v1.0.0-beta.86** — single bundle, completes v0 → v1
redesign. ADR-049 status flips Accepted. ADR-047 status flips
Superseded. beta.85 retroactively marked v0.

### beta.85 disposition

beta.85 is tagged on GitHub. Per [[feedback_never_retag]] the Go
module proxy pins on first fetch — re-tagging risks confusion.
semteams has confirmed they're holding at beta.84 and will not
adopt the bundle until the redesign lands. Pragmatic options
considered (leave / yank / force-tag); the chosen path:

- **beta.85 stays as a v0 milestone tag** with a release-note
  amendment marking it "v0 of lifecycle harness; superseded by
  v1.0.0-beta.86 with ADR-049."
- semteams remains at beta.84 until beta.86 ships.
- No tag re-roll. The git history is honest: beta.85 was tagged,
  the e2e tier surfaced the architectural issue, ADR-049 was
  drafted, beta.86 ships the corrected substrate.

### Post-migration discipline gates

Before beta.86 tags:

- `task e2e:lifecycle` green (the scenario from beta.85 still
  passes against the redesigned substrate)
- `task e2e:core`, `task e2e:structural`, `task e2e:statistical`,
  `task e2e:semantic`, `task e2e:agentic` all green — confirms
  the `ExpectedRevision` engine change doesn't regress any
  existing tier
- Lint clean, schema-generate no-diff, unit tests + race detector
  pass
- ADR-047 status flipped Superseded; ADR-049 status flipped Accepted

## Consequences

### Positive

- **Graph-visible workflow state.** Every transition lands as
  triples in ENTITY_STATES; the graph layer sees lifecycle state
  natively. Queries like "show me all flying missions" work
  through GraphQL without per-workflow special-casing. Rule
  conditions can match on workflow state directly.
- **Audit is the graph's revision history.** No parallel audit
  bucket; no separate retention story; History reads from
  ENTITY_STATES revisions with source attribution recovered from
  the audit-predicate triples stamped at each write. The
  always-`framework` Triggered bug is structurally fixed.
- **Single source of truth.** No drift risk between MISSIONS and
  ENTITY_STATES — there's only one store.
- **Smaller harness code.** lifecycle-gateway shrinks (~600 → ~300
  LOC) by composing over graph-gateway primitives instead of
  reinventing them. pkg/lifecycle loses the kvNATSStore layer
  (~150 LOC).
- **Bucket-provisioning gap dissolves.** No per-workflow buckets
  to provision. Apps using the harness pay only the graph-ingest
  emission cost.
- **Schema declares richer structure.** ChildWorkflows + Reference
  Predicates make subtree composition first-class; operator views
  render full mission + children + references in one composed
  query.
- **Discipline crystallized.** The bucket-ownership rubric +
  "defending a private bucket" worked example raise the bar for
  reaching for private buckets across the codebase.

### Negative (accepted tradeoffs)

- **Per-transition latency increases.** Direct KV write (~1ms) →
  NATS round-trip through graph-ingest (~5-10ms). Acceptable for
  all consumer-sketched write rates (handfuls per hour per entity);
  documented for future reference. Optimization deferred.
- **N+1 reads on Children/List.** Each child loaded via separate
  Manager.Get. For typical workflows (5-20 children) acceptable;
  for high-fan-out (manufacturing batch w/ 1000 units) operators
  paginate. Batch-read optimization deferred until measured.
- **Reflection on every Get.** Projection layer uses reflect to
  populate Participant struct fields from triples. Hot-path cost
  acceptable for workflow read rates; potentially optimizable
  via codegen later.
- **Schema evolution is forward-additive only.** Removed schema
  fields leave orphan triples in ENTITY_STATES; operator manually
  cleans up via triple removal if desired. Documented limitation.
- **API request lifecycle pattern doesn't fit.** Short-lived
  high-volume leaf computations are an explicit anti-pattern for
  the harness. Documented in "When the harness is NOT the right
  shape."

### Neutral / changed

- **Manager.Get returns the projected struct from triples** rather
  than reading a private blob. Identical caller experience; different
  substrate.
- **Manager.Create is "add lifecycle dimension"** rather than
  "instantiate new entity." Strictly more capable (handles the
  forward-reference + processor-stamp-first cases) but requires
  callers to internalize the reframing.
- **graph-ingest gains one optional field.** Backwards-compatible;
  existing callers unaffected.

## Open questions deferred

These were considered during the design exercise and explicitly
deferred to post-beta.86 work:

- **Batch-read primitive in graph-gateway** — would optimize
  Children + List N+1 patterns. File when measured demand
  justifies.
- **Secondary indexes for non-pattern queries** — e.g. "all
  missions owned by org X" currently requires loading every
  mission and filtering by OwnerOrgID. ADR-049 v2 could add
  operator-declared indexable predicates + framework-maintained
  index buckets.
- **Snapshot history bucket for multi-decade retention** —
  manufacturing's 7-20yr regulatory retention may exceed
  ENTITY_STATES history depth. Framework could provide a periodic
  snapshot mechanism; defer until an actual long-retention
  consumer surfaces.
- **Recursive Children helper** — depth-1 only in v1; recursive
  descent helper deferred as opt-in optimization.
- **Codegen for projection** — reflect on every Get is acceptable
  for workflow read rates; codegen optimization can land later
  if measured.

## Relationship to other ADRs

- **Supersedes** [ADR-047](047-lifecycle-harness-substrate.md)'s
  "Manager owns per-workflow KV bucket" architectural choice.
  ADR-047's other claims (Participant interface, Transitions
  table validation, operator-writable struct tags, the
  workflow-substrate concept itself) carry forward into ADR-049.
- **Companion to** [ADR-048](048-bounded-dispatcher-and-triples-substrate.md).
  ADR-048's BoundedDispatcher + `.triples` substrate are
  independent of the bucket-ownership choice; both ship as part
  of the v1.0.0-beta.86 bundle alongside this redesign.
- **Honors** [ADR-028](028-orchestration-architecture.md)'s
  "rules sequence, components parallelize" discipline. Manager
  is a discipline layer, not a runtime — it doesn't have its
  own goroutines or scheduling logic.
- **Reinforces** the "single writer to ENTITY_STATES" invariant
  (graph-ingest remains the only writer; the harness emits
  through it).
- **Reinforces** the KV twofer principle (one write = state +
  events + history; the harness benefits from this rather than
  building a parallel twofer over a private bucket).

## What this ADR is NOT

- A claim that the lifecycle harness as a concept is wrong (it's
  not — the schema + transitions + operator API surface IS
  framework value-add; the consumer sketches confirm the demand).
- A claim that ADR-047 was a mistake (the concept was right; the
  bucket-ownership choice was wrong).
- A retrospective on the bundle's other architectural choices
  (BoundedDispatcher, `.triples`, lifecycle_* rule actions stand
  independently and ship unchanged in beta.86).
- A breaking change to consumers (semteams hasn't adopted; e2e
  fixture is the only "consumer" and is internally rewritable).

## References

- Proposal trilogy that produced this ADR:
  - [`lifecycle-harness-prime-design-exercise.md`](../proposals/lifecycle-harness-prime-design-exercise.md)
  - [`lifecycle-harness-prime-consumer-sketches.md`](../proposals/lifecycle-harness-prime-consumer-sketches.md)
  - [`lifecycle-harness-prime-projection-sketch.md`](../proposals/lifecycle-harness-prime-projection-sketch.md)
- Existing infrastructure that this ADR composes over:
  - `processor/graph-ingest/component.go:1186` —
    `updateEntityAtRevision` (the CAS primitive exposed via new
    `ExpectedRevision` field)
  - `processor/graph-ingest/mutations.go` —
    `handleEntityUpdateWithTriples` (the handler the field
    extends)
  - `natsclient/kv.go` — `UpdateWithRetry` (the underlying
    CAS-with-retry primitive)
- Discipline memories applied:
  - `feedback_warning_not_fail_masks_integration_drift`
  - `feedback_reactive_patches_vs_engine_completion`
  - `feedback_never_retag`
  - `feedback_e2e_required_for_breaking_changes`
- Architectural principles honored:
  - `docs/concepts/02-kv-twofer.md`
  - `docs/concepts/14-orchestration-layers.md`
  - CLAUDE.md "Orchestration Boundaries" + "Architectural Identity
    (Not an Event Bus)" sections
