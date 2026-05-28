# Lifecycle Harness Prime — Projection Layer Sketch

**Status**: Proposed — 2026-05-28. Companion sketch to
[`lifecycle-harness-prime-design-exercise.md`](lifecycle-harness-prime-design-exercise.md)
and [`lifecycle-harness-prime-consumer-sketches.md`](lifecycle-harness-prime-consumer-sketches.md).

**Purpose**: Make the projection-layer design concrete enough to
implement. The design exercise establishes the *what* (Manager
as schema-and-discipline layer over ENTITY_STATES); the consumer
sketches establish the *who* (Tier-B coordination roots dominate);
this document establishes the *how* (exact API surface, per-operation
implementation sketches, cost model).

**Anchor case**: Drone survey mission with:
- Own state (phase, owner, note, audit fields)
- Two reference predicates (`mission.assigned_drone`,
  `mission.target_area`) pointing at entities outside this
  workflow's lifecycle
- Two child workflow types (`capture_session`, `preflight_check`),
  each a Participant in its own right

This is the structurally hardest case in the consumer set (semspec
dev-via-spec and manufacturing batch have similar shapes; sensor
lifecycle and drone are subsets).

## Core design insight

**Separate single-entity reads from subtree expansion.** Manager.Get
returns just the entity's own projected state — bounded size,
predictable cost. Children and references are loaded via
separate Manager methods that the operator gateway composes for
the "full subtree" dashboard view. This keeps the per-operation
cost model legible AND lets consumers control depth.

## Schema declaration (recap from design exercise)

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

    // Audit trail predicates (framework stamps these on every
    // transition; readable from the entity's triples; History
    // reads them at each KV revision)
    AuditPredicates: AuditSpec{
        Source: "mission.last_transition_source",
        At:     "mission.last_transition_at",
        From:   "mission.last_transition_from",
        Note:   "mission.last_transition_note",
    },

    // Children: workflow types this workflow OWNS. Each child has
    // its own Participant declaration registered separately. The
    // LinkPredicate establishes the parent→child relationship as
    // a triple on the parent.
    ChildWorkflows: []ChildSpec{
        {Workflow: "capture_session", LinkPredicate: "mission.owns_session"},
        {Workflow: "preflight_check", LinkPredicate: "mission.owns_check"},
    },

    // References: predicates that link to other entities WITHOUT
    // lifecycle ownership. Target entities have their own lifetime;
    // this workflow only references them.
    ReferencePredicates: []ReferenceSpec{
        {Predicate: "mission.assigned_drone", TargetPattern: "*.fleet.drone.*"},
        {Predicate: "mission.target_area",    TargetPattern: "*.geo.area.*"},
    },
})
```

And the Participant struct itself — what triples project into:

```go
type MissionState struct {
    EntityID    string `json:"entity_id"    lifecycle:"id"`
    Phase       string `json:"phase"        lifecycle:"phase,predicate=mission.phase"`
    OwnerOrgID  string `json:"owner_org_id" lifecycle:"operator_writable,predicate=mission.owner_org_id"`
    Note        string `json:"note"         lifecycle:"operator_writable,predicate=mission.note"`

    // Reference fields project as scalar entity IDs. Schema's
    // ReferencePredicates drives the projection; the field tag
    // names the predicate.
    AssignedDroneID string `json:"assigned_drone_id" lifecycle:"reference,predicate=mission.assigned_drone"`
    TargetAreaID    string `json:"target_area_id"    lifecycle:"reference,predicate=mission.target_area"`

    // Audit fields project from the framework-stamped predicates.
    // Read-only from the operator API (you can't patch the audit trail).
    LastTransitionSource string    `json:"last_transition_source,omitempty" lifecycle:"readonly,predicate=mission.last_transition_source"`
    LastTransitionAt     time.Time `json:"last_transition_at,omitempty"     lifecycle:"readonly,predicate=mission.last_transition_at"`

    // NOTE: children are NOT in the projected struct. They're
    // loaded separately via Manager.Children(entityID) because:
    //   - Bounded per-Get cost matters
    //   - Caller controls depth (sometimes you want the mission
    //     without N child reads)
    //   - High-fan-out parents (manufacturing batch w/ 1000 units)
    //     would make Get prohibitively expensive otherwise
}
```

## API surface

```go
// Per-entity operations — bounded cost (1 graph read + projection)
Get(ctx, workflow, entityID) (Participant, error)
GetWithRevision(ctx, workflow, entityID) (Participant, uint64, error)

// Subtree expansion — bounded but operator-controlled
Children(ctx, parentEntityID, opts ChildOptions) ([]ChildResult, error)
References(ctx, entityID) ([]ReferenceStub, error)

// State changes — emit through graph-ingest, CAS-on-condition
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
Watch(ctx, workflow string) (<-chan Participant, error)

// History — graph revisions filtered to phase changes; source
// attribution reads from the audit predicates
History(ctx, workflow, entityID string) ([]TransitionEvent, error)
```

## Per-operation sketches

### Get — single-entity projection

```go
func (m *Manager) Get(ctx context.Context, workflow, entityID string) (Participant, error) {
    schema, err := m.lookupSchema(workflow)
    if err != nil { return nil, err }

    // Read entity via standard graph read path. Single KV.Get
    // on ENTITY_STATES. Returns nil-on-not-found wrapped as
    // ErrEntityNotFound.
    entity, _, err := m.graphReader.GetWithRevision(ctx, entityID)
    if err != nil { return nil, err }

    // Construct fresh Participant struct via reflection on the
    // registered Go type. Apps don't construct Participants
    // themselves on read — Manager projects from triples.
    p := reflect.New(schema.GoType).Interface().(Participant)

    // Project triples → struct fields via the schema's projection
    // index (built once at Register time from struct tag walk).
    if err := schema.projectTriples(entity.Triples, p); err != nil {
        return nil, fmt.Errorf("project mission %q: %w", entityID, err)
    }
    return p, nil
}
```

**Cost**: 1 KV.Get + 1 reflection-driven projection. Bounded by
the entity's triple count, which is bounded by the workflow's
schema (declared predicates + audit + reference scalars).
Typically 5-15 triples per entity.

**Projection mechanics** (in `schema.projectTriples`):
- Each Schema's struct fields scanned at Register time; field tags parsed into `predicateName → fieldIndex` map
- On Get: walk the entity's triples, look up `predicate → field`, `reflect.Value.SetX` the field
- Type coercion for known shapes (time.Time from RFC3339 string, etc.)
- Missing triples → zero-value field (no error — entity may legitimately not have that predicate yet)

### Children — subtree expansion, paginated

```go
type ChildOptions struct {
    Workflow string  // optional filter: only this child workflow type
    Limit    int     // default 0 = unlimited; recommend always setting
    Offset   int
}

type ChildResult struct {
    Workflow string      `json:"workflow"`
    State    Participant `json:"state"`
}

func (m *Manager) Children(ctx context.Context, parentEntityID string, opts ChildOptions) ([]ChildResult, error) {
    // Read parent entity to discover children
    parent, _, err := m.graphReader.GetWithRevision(ctx, parentEntityID)
    if err != nil { return nil, err }

    parentSchema := m.findSchemaForEntity(parentEntityID)
    if parentSchema == nil {
        return nil, fmt.Errorf("entity %q not lifecycle-managed", parentEntityID)
    }

    // Collect (workflow, childEntityID) pairs from parent's
    // child-link triples. Stable order by (workflow, childID)
    // so pagination is deterministic across calls.
    type childRef struct{ workflow, entityID string }
    var refs []childRef
    for _, childSpec := range parentSchema.ChildWorkflows {
        if opts.Workflow != "" && opts.Workflow != childSpec.Workflow {
            continue
        }
        for _, t := range parent.Triples {
            if t.Predicate != childSpec.LinkPredicate { continue }
            childID, ok := t.Object.(string)
            if !ok { continue }
            refs = append(refs, childRef{childSpec.Workflow, childID})
        }
    }
    sort.Slice(refs, func(i, j int) bool {
        if refs[i].workflow != refs[j].workflow {
            return refs[i].workflow < refs[j].workflow
        }
        return refs[i].entityID < refs[j].entityID
    })

    // Apply pagination
    if opts.Offset >= len(refs) { return nil, nil }
    end := len(refs)
    if opts.Limit > 0 && opts.Offset+opts.Limit < end {
        end = opts.Offset + opts.Limit
    }
    page := refs[opts.Offset:end]

    // Load each child via standard Get path. N+1 read pattern.
    // For high-fan-out parents, callers pass Limit to bound cost.
    // Optimization (deferred): graph-gateway batch-read endpoint
    // that takes a list of IDs and returns all in one composed
    // query. File when measurement justifies it.
    results := make([]ChildResult, 0, len(page))
    for _, r := range page {
        child, err := m.Get(ctx, r.workflow, r.entityID)
        if err != nil {
            // Log + skip — one bad child shouldn't kill the
            // whole response. Operator dashboards render the
            // missing-child indicator without losing siblings.
            m.logger.Warn("child load failed",
                slog.String("parent", parentEntityID),
                slog.String("child", r.entityID),
                slog.String("error", err.Error()))
            continue
        }
        results = append(results, ChildResult{Workflow: r.workflow, State: child})
    }
    return results, nil
}
```

**Cost**: 1 parent read + N child reads (N bounded by Limit if
set; otherwise bounded by parent's child-triple count). For
typical workflows (5-20 children) this is fine. For high-fan-out
(manufacturing batch w/ 1000 units), the gateway is expected to
paginate (`?limit=20&offset=0`).

**Why N+1 is acceptable for v1**:
- Most consumer workflows have small child counts (≤20)
- Operator dashboards naturally paginate large lists anyway
- Batch-read optimization is additive when measured demand
  justifies (don't pre-optimize)

### References — light stubs, not full entity loads

```go
type ReferenceStub struct {
    EntityID  string `json:"entity_id"`
    Predicate string `json:"predicate"`
    // The following are populated only if the target entity is
    // itself lifecycle-managed (matches a registered workflow's
    // EntityIDPattern). Otherwise empty — operator dashboards
    // render the bare entity_id and link to graph-gateway for
    // the non-workflow entity.
    Workflow string `json:"workflow,omitempty"`
    Phase    string `json:"phase,omitempty"`
}

func (m *Manager) References(ctx context.Context, entityID string) ([]ReferenceStub, error) {
    entity, _, err := m.graphReader.GetWithRevision(ctx, entityID)
    if err != nil { return nil, err }

    schema := m.findSchemaForEntity(entityID)
    if schema == nil {
        return nil, fmt.Errorf("entity %q not lifecycle-managed", entityID)
    }

    var stubs []ReferenceStub
    for _, refSpec := range schema.ReferencePredicates {
        for _, t := range entity.Triples {
            if t.Predicate != refSpec.Predicate { continue }
            targetID, ok := t.Object.(string)
            if !ok { continue }
            stub := ReferenceStub{
                EntityID:  targetID,
                Predicate: refSpec.Predicate,
            }
            // Light "is this lifecycle-managed?" check via pattern
            // match. If yes, optionally fetch phase only.
            if targetWf := m.matchEntityIDToWorkflow(targetID); targetWf != "" {
                stub.Workflow = targetWf
                if targetEntity, _, err := m.graphReader.GetWithRevision(ctx, targetID); err == nil {
                    targetSchema, _ := m.lookupSchema(targetWf)
                    stub.Phase = extractTripleScalar(targetEntity.Triples, targetSchema.PhasePredicate)
                }
            }
            stubs = append(stubs, stub)
        }
    }
    return stubs, nil
}
```

**Cost**: 1 source-entity read + 1 light read per referenced
lifecycle-managed entity. References to non-lifecycle entities
(drone, area) don't trigger extra reads — operator dashboards
render the bare entity_id and let users follow the link.

**Why light stubs not full reads**: references are display-level
context. "Mission M is assigned to drone D in area Z" doesn't
need drone D's full state; it needs D's identity. If the operator
wants drone D's details they click through to graph-gateway.

### Composed gateway view — GET /workflows/{type}/{id}

The lifecycle-gateway endpoint composes the three primitives:

```go
func (g *Gateway) handleGetInstance(w http.ResponseWriter, r *http.Request, workflow, entityID string) {
    mission, err := g.manager.Get(r.Context(), workflow, entityID)
    if err != nil { g.writeError(w, err); return }

    refs, err := g.manager.References(r.Context(), entityID)
    if err != nil {
        g.logger.Warn("references load failed", "entity", entityID, "error", err)
        // Don't fail the whole response on reference load failure
    }

    // Children are paginated. Default page size (e.g. 50) returned
    // inline; full list reachable via /workflows/{type}/{id}/children
    children, err := g.manager.Children(r.Context(), entityID, ChildOptions{Limit: 50})
    if err != nil {
        g.logger.Warn("children load failed", "entity", entityID, "error", err)
    }
    childCount := g.manager.ChildCount(r.Context(), entityID)

    view := MissionView{
        State:      mission,
        References: refs,
        Children:   children,
        ChildCount: childCount,
        // If we truncated, point to the full list
        ChildrenPaginated: len(children) < childCount,
    }
    g.writeJSON(w, http.StatusOK, view)
}
```

**Total cost for the composed view (Tier-B mission, typical case)**:
- 1 mission read
- 0-2 reference-target reads (one per declared reference predicate, only if target is lifecycle-managed)
- 1 parent re-read for Children expansion (could be cached from the first read in an optimization pass)
- N child reads, N bounded by page limit (default 50)
- 1 ChildCount query (could be a count-only graph query, cheap)

For a typical mission with 5 children + 2 references → ~9 reads.
For a manufacturing batch with 1000 children → page of 50 → ~53 reads
on the first page; subsequent pages are 50 reads each. **Operator-controlled cost.**

### List — discovery across a workflow type

```go
func (m *Manager) List(ctx context.Context, workflow string, opts ListOptions) ([]Participant, error) {
    schema, err := m.lookupSchema(workflow)
    if err != nil { return nil, err }

    // Graph query for all entities matching the pattern. Uses
    // graph-gateway's existing pattern-match capability. Returns
    // IDs only by default; full entity load comes via Get per ID.
    entityIDs, err := m.graphReader.QueryEntityIDs(ctx, schema.EntityIDPattern, opts.MatchFilter)
    if err != nil { return nil, err }

    // Pagination
    if opts.Offset >= len(entityIDs) { return nil, nil }
    end := len(entityIDs)
    if opts.Limit > 0 && opts.Offset+opts.Limit < end {
        end = opts.Offset + opts.Limit
    }
    page := entityIDs[opts.Offset:end]

    // Project each. N+1 read pattern same as Children. Same
    // optimization deferral: batch-read primitive if measured
    // demand justifies.
    results := make([]Participant, 0, len(page))
    for _, id := range page {
        p, err := m.Get(ctx, workflow, id)
        if err != nil { continue }
        results = append(results, p)
    }
    return results, nil
}
```

**Cost**: 1 pattern-query + N entity reads, N bounded by Limit.

**Active-only filter**: `opts.Active = true` adds a predicate
match on phase against the schema's TerminalPhases. The graph
query layer handles this — no per-result filtering needed.

### Watch — KV-watch over ENTITY_STATES filtered to workflow pattern

```go
func (m *Manager) Watch(ctx context.Context, workflow string) (<-chan Participant, error) {
    schema, err := m.lookupSchema(workflow)
    if err != nil { return nil, err }

    // KV watch on ENTITY_STATES with the workflow's pattern.
    // ENTITY_STATES already exists; no new bucket needed.
    kvUpdates, err := m.graphBucket.Watch(ctx, schema.EntityIDPattern + ".*")
    if err != nil { return nil, err }

    out := make(chan Participant)
    go func() {
        defer close(out)
        for entry := range kvUpdates {
            if entry.Operation() == jetstream.KeyValueDelete { continue }
            var entity graph.EntityState
            if err := json.Unmarshal(entry.Value(), &entity); err != nil { continue }
            p := reflect.New(schema.GoType).Interface().(Participant)
            if err := schema.projectTriples(entity.Triples, p); err != nil {
                m.logger.Warn("watch projection failed", "entity", entry.Key(), "error", err)
                continue
            }
            select {
            case out <- p:
            case <-ctx.Done(): return
            }
        }
    }()
    return out, nil
}
```

**Cost**: per-update CPU for the projection (cheap). NATS handles
the watcher mechanics.

**Vs beta.85's Watch on private bucket**: identical pattern,
different bucket. Beta.85 watched MISSIONS; prime watches
ENTITY_STATES with a workflow-pattern filter. Lower NATS overhead
(one shared bucket vs many per-workflow buckets) and the watcher
sees writes from ALL sources (rule-driven, operator-driven,
processor-emitted), not just Manager writes.

### History — phase-change events from graph revisions with source attribution

```go
func (m *Manager) History(ctx context.Context, workflow, entityID string) ([]TransitionEvent, error) {
    schema, err := m.lookupSchema(workflow)
    if err != nil { return nil, err }

    // Read ALL KV revisions for the entity from ENTITY_STATES.
    // Already supported by the underlying KV bucket via History().
    revisions, err := m.graphBucket.History(ctx, entityID)
    if err != nil { return nil, err }

    events := make([]TransitionEvent, 0, len(revisions))
    var previousPhase string
    for _, rev := range revisions {
        if rev.IsDelete() {
            events = append(events, TransitionEvent{
                From: previousPhase, To: "<deleted>", At: rev.CreatedAt,
                Triggered: TransitionSourceFramework,
            })
            previousPhase = "<deleted>"
            continue
        }
        var entity graph.EntityState
        if err := json.Unmarshal(rev.Value, &entity); err != nil { continue }

        currentPhase := extractTripleScalar(entity.Triples, schema.PhasePredicate)
        if currentPhase == previousPhase { continue } // not a phase change

        // Source attribution comes from the audit triples — they
        // were stamped on this revision when Manager.Transition
        // wrote it. NOT a constant; the real source is here.
        triggered := extractTripleScalar(entity.Triples, schema.AuditPredicates.Source)
        at := parseTime(extractTripleScalar(entity.Triples, schema.AuditPredicates.At))
        note := extractTripleScalar(entity.Triples, schema.AuditPredicates.Note)
        from := extractTripleScalar(entity.Triples, schema.AuditPredicates.From)
        // Trust the stamped From over reconstruction (handles
        // out-of-order revision processing gracefully)
        if from == "" { from = previousPhase }

        events = append(events, TransitionEvent{
            From:      from,
            To:        currentPhase,
            At:        at,
            Triggered: TransitionSource(triggered),
            Note:      note,
        })
        previousPhase = currentPhase
    }
    return events, nil
}
```

**Cost**: 1 KV.History (bounded by bucket's history depth + entity's
revision count) + per-revision JSON unmarshal + scalar extraction.

**The History TODO is solved**: source attribution is in the
audit triples that Manager.Transition stamped at each revision.
Read them back; no parallel audit bucket needed.

**Retention**: ENTITY_STATES bucket's history depth is operator-
configurable per ADR-047 ("apps own bucket topology"). For
compliance-heavy domains (manufacturing 7-20yr), operators
configure a large history depth OR provision a derivative audit
bucket that snapshots ENTITY_STATES periodically. That decision
stays with operators; framework provides the primitive.

### Transition — read-validate-emit-at-rev loop

```go
func (m *Manager) Transition(ctx context.Context, workflow, entityID, newPhase string,
    source TransitionSource, note string) error {
    return m.TransitionWith(ctx, workflow, entityID, newPhase, source, note, nil)
}

func (m *Manager) TransitionWith(ctx context.Context, workflow, entityID, newPhase string,
    source TransitionSource, note string, mutator func(Participant) error) error {

    schema, err := m.lookupSchema(workflow)
    if err != nil { return err }

    var lastErr error
    for retry := 0; retry < updateRetries; retry++ {
        // Read current state
        entity, currentRev, err := m.graphReader.GetWithRevision(ctx, entityID)
        if err != nil { return err }

        currentPhase := extractTripleScalar(entity.Triples, schema.PhasePredicate)
        if currentPhase == "" {
            return fmt.Errorf("%w: entity has no %s triple", ErrInvalidTransition, schema.PhasePredicate)
        }
        if schema.Transitions.IsTerminal(currentPhase) {
            return fmt.Errorf("%w: cannot transition from terminal phase %q", ErrTerminalPhase, currentPhase)
        }
        if !schema.Transitions.IsValidTransition(currentPhase, newPhase) {
            return fmt.Errorf("%w: %s → %s not declared", ErrInvalidTransition, currentPhase, newPhase)
        }

        // Project + run mutator (if any) for atomic multi-field updates
        p := reflect.New(schema.GoType).Interface().(Participant)
        if err := schema.projectTriples(entity.Triples, p); err != nil { return err }
        if mutator != nil {
            if err := mutator(p); err != nil { return err }
        }

        // Build the transition delta. Includes phase change +
        // audit triples + any mutator-changed fields (extracted
        // by diffing the projected struct against the entity's
        // current triples).
        now := time.Now()
        delta := []message.Triple{
            triple(entityID, schema.PhasePredicate, newPhase),
            triple(entityID, schema.AuditPredicates.Source, string(source)),
            triple(entityID, schema.AuditPredicates.At, now.Format(time.RFC3339Nano)),
            triple(entityID, schema.AuditPredicates.From, currentPhase),
        }
        if note != "" {
            delta = append(delta, triple(entityID, schema.AuditPredicates.Note, note))
        }
        if mutator != nil {
            delta = append(delta, schema.diffMutatedTriples(entity.Triples, p)...)
        }

        // Emit via graph-ingest's UpdateEntityWithTriples handler
        // with the new ExpectedRevision field. CAS at currentRev.
        err = m.emitUpdate(ctx, graph.UpdateEntityWithTriplesRequest{
            Entity:           &graph.EntityState{ID: entityID, Version: entity.Version + 1},
            AddTriples:       delta,
            ExpectedRevision: currentRev,
        })
        if err == nil { return nil }
        if errors.Is(err, natsclient.ErrKVRevisionMismatch) {
            // Concurrent writer beat us. Re-read + re-validate.
            lastErr = err
            continue
        }
        return err
    }
    return fmt.Errorf("%w: workflow=%q entity_id=%q after %d retries (last: %v)",
        ErrUpdateRetriesExhausted, workflow, entityID, updateRetries, lastErr)
}
```

**Cost per successful transition**: 1 read + 1 NATS round-trip +
1 KV write (graph-ingest handles the latter via standard path).

**Cost per CAS conflict**: extra read + extra emit per retry, up
to `updateRetries`. For consumer-sketches write rates, conflicts
are rare.

**Atomicity property**: phase + audit fields + mutator-changed
fields land in one AddTriplesBatch atomic write. Readers see
either pre-transition or post-transition state, never half.

## Cost model summary

| Operation | Reads | Writes | Notes |
|---|---|---|---|
| Get(entity) | 1 | 0 | Bounded by entity's triple count (typically 5-15) |
| Children(parent, limit=N) | 1 + N | 0 | Operator-controlled depth |
| References(entity) | 1 + R | 0 | R = lifecycle-managed reference targets only |
| List(workflow, limit=N) | 1 query + N gets | 0 | Pattern match on EntityIDPattern |
| Watch(workflow) | 0 | 0 | NATS KV watcher; per-update projection CPU |
| History(entity) | 1 (history call) | 0 | Per-revision JSON unmarshal + scalar extract |
| Transition | 1 + 1 | 1 | Conflict retry adds 1 read + 1 emit per retry |
| Composed GetInstance (gateway view, typical) | ~9 | 0 | 1 mission + ~2 refs + 1 reread + ~5 children |

## Optimization deferrals (do NOT build until measured)

- **Graph-gateway batch-read endpoint** — takes list of entity IDs,
  returns all in one composed query. Optimizes Children and List
  N+1 patterns. Build when high-fan-out consumers actually hit
  read-amplification ceilings.
- **Secondary indexes for non-pattern queries** — "all missions
  owned by org X" today requires loading every mission and
  filtering by OwnerOrgID. If demand justifies, ADR-047-prime
  v2 could add operator-declared indexable predicates +
  framework-maintained index buckets. Defer until measured.
- **Caching of immutable workflow definitions** — schemas are
  declared once at Register time; the projection metadata
  (predicate → field index) is cached then.
- **Snapshot history bucket for very long retention** — manufacturing
  batches with 7-20yr regulatory retention may exceed ENTITY_STATES'
  practical history depth. Framework could provide a periodic
  snapshot mechanism. Defer until an actual long-retention
  consumer surfaces.

## Edge cases

**E1: Entity has no `mission.phase` triple yet (mid-creation race)**
Get returns the projected struct with Phase=zero-value. Transition
returns ErrInvalidTransition with a helpful message
("entity has no mission.phase triple"). The Create path
(see E5) ensures the phase triple is set on first write.

**E2: Schema has a field with no corresponding triple**
Projection sets zero-value. Not an error — entity may not have
written that predicate yet. Documented in the field tag conventions.

**E3: Triple has a predicate the schema doesn't declare**
Projection ignores it. The entity legitimately has triples from
other sources (mission-command processor stamps `mission.command`;
the rule that reads it doesn't need it in the projected struct).
Round-trip preserves these — Manager doesn't clobber unknown
triples because it uses delta semantics (`AddTriples`), not
whole-entity overwrite.

**E4: Multiple triples with same predicate (cardinality)**
For phase / reference / audit predicates → take the latest
revision's value (per-predicate latest-wins semantics already in
ENTITY_STATES). For child-link predicates → ALL matching triples
are children (cardinality-many is the natural shape; a mission
owns multiple capture_sessions).

**E5: Create — first write to land triples**
`Manager.Create(initial Participant)` projects the initial
Participant's non-zero fields into triples, calls
`UpdateEntityWithTriplesRequest` with `ExpectedRevision=0`
(create-if-absent semantics). Returns ErrEntityAlreadyExists on
conflict. The entity may have triples from other writers added
before Create; Create only adds the lifecycle triples (it doesn't
clobber existing data).

**E6: Reference to a non-lifecycle entity (drone)**
References returns a stub with empty Workflow/Phase. Operator
dashboard renders bare entity_id linked to graph-gateway.

**E7: Reference to a lifecycle entity from a different workflow**
References returns a stub with the target's Workflow + Phase.
Operator dashboard renders a link to the lifecycle-gateway for
that workflow type.

**E8: Child workflow not registered**
Manager.findSchemaForEntity returns nil for child IDs whose
workflow wasn't registered. Children skips them with a Warn log
("child workflow %q not registered; skipping %q").

**E9: Pattern collision (entity matches multiple workflow patterns)**
findSchemaForEntity matches against all registered EntityIDPatterns.
If multiple match, longest-prefix wins; if still ambiguous, Warn
log + first-registered wins. This is a configuration smell —
workflows should have non-overlapping patterns by convention.

**E10: Transition during entity delete**
Transition's read-validate-emit-at-rev loop catches this — the
emit fails with `ErrKVRevisionMismatch` (or
`ErrKVKeyNotFound` after delete), Manager returns
ErrEntityNotFound. No partial state.

## Implications for ADR-047-prime

- Manager API surface above replaces beta.85's
  `Get/Create/Update/UpdateFromOperator/Transition/Complete/Fail/List/Watch/History/Children/Ancestors`
- Children and References become first-class on the schema (not
  implicit via cross-workflow scans)
- Operator gateway gets a richer composed-view shape (mission +
  children + references in one response)
- The "Ancestors" method in beta.85 is replaced by "follow the
  parent_entity_id link" — `ParentEntityID()` on Participant
  becomes optional; consumers who want explicit parent links
  declare a parent-reference predicate in their schema
- History stamps source attribution at write time; reads
  reconstruct it from the triples; the always-`framework` bug
  is structurally fixed
- pkg/lifecycle imports graph + graph-ingest-client packages
  (it's no longer KV-direct)

## What this sketch DOES NOT cover

- The graph-ingest-client wrapper Manager uses to emit
  `UpdateEntityWithTriplesRequest` (small wrapper around the
  existing NATS request/reply path; sketch as part of PR 2 in
  the migration plan)
- The exact reflection-driven projection code (mechanical;
  worked example in the Engine work section)
- The Watch filter mechanics in detail (KV.Watch already supports
  subject patterns; Schema.EntityIDPattern translates directly)
- Edge case for `lifecycle:"id"` field — the EntityID is in the
  bucket key, not as a triple object; projection special-cases
  to populate from the key
- Test plan for the projection layer (covered when ADR-047-prime
  draft lands)

## Open questions specific to the projection layer

**P1: Should the projected struct expose ALL triples on the entity,
or only those declared in the schema?**

Today's sketch: only declared. Justification: schema is the
contract; unknown predicates aren't part of the workflow's
state model. Counter: operators might want to see all triples
for debugging.

Resolution: declared-only by default, with an optional
`Manager.GetRaw(entityID)` that returns the full entity.Triples
for debug/audit access via the gateway.

**P2: Should Children include indirect descendants (grandchildren)
or just immediate children?**

Today's sketch: immediate only. Recursive descent would be O(depth)
reads. Operator dashboards rendering full subtrees can iterate
Manager.Children themselves.

Resolution: immediate only in v1; recursive helper as an
opt-in optimization later.

**P3: Should References be transitive (deep follow)?**

Today's sketch: depth-1. Following references could explode
(drone → sensor → calibration session → previous session → ...).

Resolution: depth-1 only; consumers walk the graph themselves
for deeper traversals.

**P4: How does the projection layer handle Schema evolution?**

If a workflow adds a new operator-writable predicate, existing
entities (with no triple for that predicate) project as
zero-value. New transitions stamp the predicate. Old History
events lack the predicate. Acceptable — projection is forward-
compatible.

Schema FIELD removal: if a field is removed from the Go struct,
the entity may still have the triple in ENTITY_STATES (orphan).
Manager ignores it on projection (E3). Cleanup is operator's
choice (manual triple deletion if desired).

Resolution: documented as forward-additive; field removal leaves
orphan triples (acceptable).

## Related context

- [`lifecycle-harness-prime-design-exercise.md`](lifecycle-harness-prime-design-exercise.md)
  — establishes the *what*
- [`lifecycle-harness-prime-consumer-sketches.md`](lifecycle-harness-prime-consumer-sketches.md)
  — establishes the *who*
- `processor/graph-ingest/component.go:1186` (`updateEntityAtRevision`)
  — the CAS primitive Manager.Transition uses
- `processor/graph-ingest/mutations.go` (handleEntityUpdateWithTriples)
  — the request handler that needs `ExpectedRevision` exposed
- `pkg/lifecycle/manager.go` — the current Manager impl this
  redesign replaces
