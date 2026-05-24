# Workflow Primitives — Design Resolutions for ADR-047

**Status**: Draft, 2026-05-24. Resolves the 8 TBD design questions
the cross-consumer sketches surfaced, before ADR-047 drafts. Each
resolution carries the reasoning + the trade-offs accepted.

## TL;DR — 8 resolutions

| # | Question | Resolution | Rationale |
|---|---|---|---|
| 1 | `Manager.List` filter signature | Generic `ListOptions{Phase, Active, Match map[string]any, UpdatedAfter, Limit}` — multi-tenancy via `Match` | Apps express tenancy app-side; framework provides general filter mechanism |
| 2 | `$now` substitution vs cron-only | Cron-only (no `$now`) | Existing cron + condition pattern covers all sketched cases without expanding substitution surface |
| 3 | Per-field operator-writability | Struct tags: `lifecycle:"operator_writable"` | Idiomatic Go; type-checked at struct definition; locally readable |
| 4 | Dashboard ship UI? | Ship API only (gateway components); defer UI | Per-product dashboards are app concerns; gateway API is the substrate |
| 5 | Atomic multi-field transitions | `lifecycle_transition` action takes optional `set: {field: value}` | Atomic by construction (single `Manager.Update` closure); contained inside the harness's rule actions |
| 6 | Arithmetic in substitution | App-side bookkeeping (e.g., `is_final_waypoint: bool` set by executor) | Fits "components own intermediate state" discipline; no rule-engine surface expansion. Revisit if 3+ consumers demand. |
| 7 | KV indexing for `Manager.List` performance | Linear scan v1; secondary index v2 when operator-demonstrated bottleneck | Don't optimize prematurely; scales handle current consumer demands |
| 8 | Phase-transition validation | Declared transitions table at `Manager.Register` time | Enforces discipline at registration; free state-machine diagram for operator dashboards; catches misconfigured transitions at runtime |

## Resolution 1 — `Manager.List` filter signature

### Decision

```go
type ListOptions struct {
    Phase        string         // empty = any phase
    Active       bool           // true = only non-terminal
    UpdatedAfter time.Time      // for incremental queries
    Match        map[string]any // field-equality matches (app-side semantics)
    Limit        int            // 0 = unlimited
    Offset       int            // pagination cursor
}

func (m *Manager) List(ctx context.Context, workflow string, opts ListOptions) ([]Participant, error)
```

### Rationale

- **Multi-tenancy without baking it in**: `Match{"org_id": "acme"}` lets apps tenant-scope queries. The framework doesn't need to know what `org_id` means; it just matches values against state struct fields via reflection or struct tags.
- **Operator API drives the design**: every `GET /workflows/{type}?phase=X&active=true&org_id=acme` query maps to `ListOptions`.
- **Pagination** matters at scale (hundreds-of-thousands of completed instances); add `Limit + Offset` from day one.

### Trade-offs accepted

- Apps that filter on app-specific fields via `Match` may hit O(N) scan cost. Resolution 7 (secondary index) handles when it bites.
- Reflection-based matching has a small overhead; can be replaced by struct-tag-declared indexable fields later.

## Resolution 2 — `$now` substitution vs cron-only

### Decision

**No `$now` substitution path.** Temporal conditions use cron rules
(`"schedule": "every 1h"`) + condition matching against state fields
that components update with current time.

### Example (semconnect sketch's `schedule_maintenance` rule):

```json
{
  "name": "schedule_maintenance",
  "when": {
    "bucket": "CSAPI_SYSTEMS",
    "schedule": "every 1h",
    "conditions": [
      {"field": "$entity.lifecycle.phase", "op": "eq", "value": "active"},
      {"field": "$entity.triple.next_maintenance_due_unix", "op": "lte", "value": "$cron_fire_time_unix"}
    ]
  },
  "actions": [...]
}
```

The cron-rule already has access to its own fire time (`$cron_fire_time_unix`).
That suffices for temporal comparisons.

### Rationale

- The substitution layer is string-based; adding `$now` opens the door to typed expressions, which is its own ADR.
- Cron rules are the existing temporal trigger primitive (ADR-031). Pairing them with condition matching covers all surveyed cases.
- Apps that want fine-grained time comparisons store unix timestamps as state fields; rules match against them.

### Trade-offs accepted

- Slightly more verbose than `$now` for some patterns.
- Apps must compute and store `*_unix` fields they want to compare. Discipline issue, not framework gap.

## Resolution 3 — Per-field operator-writability mechanism

### Decision

Struct tags on state fields:

```go
type SystemState struct {
    EntityID_    string `json:"entity_id" lifecycle:"id"`
    Phase_       string `json:"phase"     lifecycle:"phase,readonly"`
    OwnerOrgID   string `json:"owner_org_id" lifecycle:"operator_writable"`
    DeployedTo   string `json:"deployed_to,omitempty" lifecycle:"operator_writable"`
    
    // No tag = not operator-writable (default-deny)
    LastCalibrationAt *time.Time `json:"last_calibration_at,omitempty"`
}
```

### Rationale

- **Default-deny**: fields not tagged are not operator-writable. Apps opt in explicitly. Matches the security default-deny principle.
- **Locally readable**: struct definition tells the whole story; no scanning a `OperatorWritableFields()` method body.
- **Standard Go idiom**: matches `json:`, `validate:`, etc. tags developers already know.
- **Manager enforces**: `Manager.UpdateFromOperator(entityID, patch)` rejects patches that touch non-tagged fields.

### Trade-offs accepted

- Tag noise on struct definitions.
- App must remember to tag new fields they want operator-writable (default-deny means failure mode is "operator gets 403", which is safe).
- Reflection cost is small (struct tags are cached).

## Resolution 4 — Cross-product dashboard ship UI?

### Decision

**Ship API only.** Gateway components implement:

- `GET /workflows` — list registered workflow types with counts
- `GET /workflows/{type}` — list instances of type with `ListOptions` query params
- `GET /workflows/{type}/{id}` — get specific instance
- `GET /workflows/{type}/{id}/history` — phase-transition history via KV revision replay
- `POST /workflows/{type}/{id}/state` — operator-writable patch (Resolution 3)
- `POST /workflows/{type}/{id}/transition` — explicit operator transition (Resolution 8)
- `WebSocket /workflows/{type}?stream=true` — live updates

No UI ships with the framework. Per-product UIs build on the API.

### Rationale

- The OGC CS API gateway is one UI; a drone fleet ops UI is another; semspec's plan dashboard is another. Different product UX needs; one framework UI satisfies none.
- The API is the substrate that makes per-product UIs possible.
- Shipping a UI in the framework adds front-end LOC + framework UI debt + per-product customization debt.

### Trade-offs accepted

- No turnkey operator UX out of the box.
- Apps must invest in their own UI work (or use a generic JSON-table viewer for early-stage operations).
- May leave the framework feeling "incomplete" to operators expecting a workflow-tool's dashboard.

## Resolution 5 — Atomic multi-field transitions in rule actions

### Decision

`lifecycle_transition` action takes an optional `set` field:

```json
{
  "type": "lifecycle_transition",
  "phase": "flying",
  "set": {
    "current_waypoint_index": "$entity.triple.current_waypoint_index + 1"
  }
}
```

Implementation: the action executor calls `Manager.Update(entityID, closure)` where the closure both applies the `set` patches AND transitions the phase, all inside one optimistic-concurrency-protected KV write.

### Rationale

- **Atomic by construction**: single closure = single KV revision bump.
- **Contained in the lifecycle action**: doesn't expand the broader rule-engine's transaction model.
- **Matches drone sketch Gap 1 resolution option C** — best of the three options I sketched there.
- **Resolves Resolution 6's arithmetic gap** for the lifecycle-action case (the `set` value can be a substitution expression).

### Trade-offs accepted

- `$entity.triple.X + 1` in the `set` value requires arithmetic in substitution. **Wait — this conflicts with Resolution 6** which says no arithmetic in substitution.

### Re-resolution after the conflict

Two cleaner forms:

```json
// Form A — explicit increment action
{
  "type": "lifecycle_transition",
  "phase": "flying",
  "set": {
    "current_waypoint_index": {"op": "increment"}
  }
}

// Form B — pre-computed value (component writes the next-index field)
{
  "type": "lifecycle_transition",
  "phase": "flying",
  "set": {
    "current_waypoint_index": "$entity.triple.next_waypoint_index"
  }
}
```

**Resolution 5 final**: `set` accepts either a literal substitution
(`"$entity.triple.field"`) or a typed operation object
(`{"op": "increment"}`, `{"op": "decrement"}`, `{"op": "set", "value": "..."}`).

This keeps the substitution layer string-based and gives the
specific arithmetic ops a defined surface inside the lifecycle action.

## Resolution 6 — Arithmetic in substitution

### Decision

**No arithmetic in substitution.** Two paths instead:

1. **App-side bookkeeping** for cases like "is this the last iteration?" — the executor sets a bool field; rules match against it.
2. **Typed operation objects** in the `set` field of `lifecycle_transition` (Resolution 5) for increment/decrement style mutations.

### Example (drone sketch revised):

```go
// Component side
func (c *waypointExecutor) handleFly(ctx, missionID) error {
    // ... fly to waypoint ...
    return c.lifecycle.Update(ctx, missionID, func(p Participant) error {
        m := p.(*MissionState)
        m.Phase_ = "captured"
        m.IsFinalWaypoint = (m.CurrentWaypointIndex == m.TotalWaypoints - 1)
        return nil
    })
}
```

```json
// Rule side — match the bool, no arithmetic
{
  "name": "land_after_last_waypoint",
  "when": {
    "conditions": [
      {"field": "$entity.lifecycle.phase", "op": "eq", "value": "captured"},
      {"field": "$entity.triple.is_final_waypoint", "op": "eq", "value": true}
    ]
  },
  "actions": [{"type": "lifecycle_transition", "phase": "landing"}]
}
```

### Rationale

- Keeps substitution layer simple (string replace only).
- Matches "components own intermediate state" discipline — the executor knows whether it just hit the final waypoint; better to encode that in state than re-compute in rule conditions.
- Avoids the slippery slope toward typed expressions in rules.

### Trade-offs accepted

- App-side bookkeeping discipline required.
- Apps that naively try `value: "$entity.triple.X - 1"` will fail loudly (substitution returns the literal string, condition fails).
- The discipline is documentable; the failure mode is loud, not silent.

## Resolution 7 — KV indexing for `Manager.List` performance

### Decision

**v1**: Linear KV scan. `Manager.List(ctx, workflow, opts)` scans the
workflow's KV bucket, applies filter in-process, returns results.

**v2**: Secondary index, ship when operator-demonstrated bottleneck.
Implementation: the framework maintains an index bucket keyed by
`(workflow, phase, [match_field])`. Updates to a Participant's
indexable fields trigger index updates atomically (via KV
optimistic-concurrency on the index bucket).

### Rationale

- **Current consumer scales don't need the index**: drone fleet (~10s
  to ~1000s of missions per company), semspec (~1000s of plans), semconnect
  (~1000s of systems). Linear scan is fine.
- **Operator-demonstrated bottleneck = real evidence**: don't ship an
  index until someone shows the operational impact. Avoids speculation-
  driven design.
- **v2 is forward-compatible**: same `Manager.List` API; index is
  transparent.

### Trade-offs accepted

- Apps at unexpected scale early may hit the linear-scan cost.
- Mitigation: `Limit` + `Offset` in `ListOptions` lets apps paginate;
  most operator queries are small windows.

## Resolution 8 — Phase-transition validation

### Decision

Declared transitions table at `Manager.Register` time:

```go
mgr.Register("drone-survey", func() Participant {
    return &MissionState{}
}, lifecycle.Transitions{
    "created":     {"planning", "failed"},
    "planning":    {"planned", "failed"},
    "planned":     {"flying", "aborting"},
    "flying":      {"captured", "aborting", "failed"},
    "captured":    {"flying", "landing", "aborting"},
    "landing":     {"landed", "aborting", "failed"},
    "landed":      {"completed"},
    "aborting":    {"safe-landed", "failed"},
    "completed":   {},  // terminal
    "safe-landed": {},  // terminal
    "failed":      {},  // terminal
})
```

`Manager.Transition(entityID, newPhase)` validates the from→to edge
against the table. Invalid transitions return error; transitions
NOT in the table are rejected.

Terminal phases (`Transitions[phase] = {}`) prevent further
transitions; matches `Participant.IsTerminal()` from day one.

### Rationale

- **Catches misconfigured rules**: a rule that transitions `safe-landed → planning` is impossible (table rejects).
- **Free state-machine diagram**: `Manager.GetWorkflowDefinition(workflow)` returns the table → operator dashboard renders it.
- **Enforces design discipline at registration**: makes the workflow's state machine explicit and visible.
- **Backstops the harness**: terminal-detection (`IsTerminal`) and transition-validity become consistent (terminal phase = no out-edges).

### Trade-offs accepted

- App must enumerate transitions explicitly. Adds setup boilerplate.
  Mitigation: registration boilerplate is small; the table is itself
  documentation.
- Evolving the state machine (adding a phase) requires updating the
  table. This is a feature, not a bug — operators see the change
  immediately.

## Composite design — the harness shape settled

After 8 resolutions, the Lifecycle harness has this surface:

### Interface (apps implement)

```go
package lifecycle

type Participant interface {
    EntityID() string
    Workflow() string
    Phase() string
    IsTerminal() bool
    KVBucket() string
    KVKey() string
    
    // OPTIONAL — for parent/child workflows (per user Q2 answer)
    ParentEntityID() string  // empty if root
}
```

### Framework provides

```go
package lifecycle

type Manager struct{...}

func NewManager(natsClient, logger) *Manager

func (m *Manager) Register(workflow string, factory func() Participant, transitions Transitions) error

// Lifecycle ops
func (m *Manager) Get(ctx, entityID) (Participant, error)
func (m *Manager) Create(ctx, initial Participant) error
func (m *Manager) Update(ctx, entityID, mutator func(Participant) error) error
func (m *Manager) UpdateFromOperator(ctx, entityID, patch map[string]any) error
func (m *Manager) Transition(ctx, entityID, newPhase string) error  // validates against Transitions
func (m *Manager) Complete(ctx, entityID) error
func (m *Manager) Fail(ctx, entityID, reason string) error

// Query ops
func (m *Manager) List(ctx, workflow string, opts ListOptions) ([]Participant, error)
func (m *Manager) Watch(ctx, workflow string) <-chan Participant
func (m *Manager) History(ctx, entityID) ([]TransitionEvent, error)

// Parent/child (Q2 answer)
func (m *Manager) Children(ctx, parentEntityID string) ([]Participant, error)
func (m *Manager) Ancestors(ctx, entityID string) ([]Participant, error)

// Workflow introspection (Resolution 8)
func (m *Manager) GetWorkflowDefinition(workflow string) (WorkflowDef, error)
func (m *Manager) ListWorkflows() ([]WorkflowDef, error)

type WorkflowDef struct {
    Workflow    string
    Transitions Transitions
    Schema      *jsonschema.Schema  // optional, from struct tag derivation
}

type Transitions map[string][]string

type TransitionEvent struct {
    From      string
    To        string
    At        time.Time
    Triggered string  // "rule" / "operator" / "component" / "framework"
    Note      string
}

type ListOptions struct {
    Phase        string
    Active       bool
    UpdatedAfter time.Time
    Match        map[string]any
    Limit        int
    Offset       int
}
```

### Rule actions (rule engine extends)

- `lifecycle_transition` — transitions phase, with optional `set` (Resolution 5)
- `lifecycle_complete` — marks terminal complete
- `lifecycle_fail` — marks terminal fail with reason

### Rule condition substitution

- `$entity.lifecycle.phase` — current phase
- `$entity.lifecycle.terminal` — bool
- `$entity.lifecycle.workflow` — workflow type
- `$cron_fire_time_unix` — for cron rules (Resolution 2)

### Operator API (gateway components extend)

- `GET /workflows`, `GET /workflows/{type}`, `GET /workflows/{type}/{id}`
- `GET /workflows/{type}/{id}/history`
- `POST /workflows/{type}/{id}/state` — operator patch (Resolution 3)
- `POST /workflows/{type}/{id}/transition` — operator-initiated transition
- `WebSocket /workflows/{type}` — live updates

## What's resolved; what's not

Resolved (this doc):
- Filter signature
- $now vs cron
- Operator-writable mechanism
- Dashboard UI scope
- Atomic transitions in rule actions
- Arithmetic in substitution
- KV indexing strategy
- Phase-transition validation

NOT resolved (still ADR-draft phase work):
- Naming details (`pkg/lifecycle` vs `pkg/workflow` reuse — lean
  rename to `pkg/lifecycle` for clarity)
- Migration path from existing `pkg/workflow.State` users (only
  agentic-loop today; trivial)
- Exact JSON schemas for rule actions
- Test fixtures + integration testing approach
- Documentation strategy (concept doc 14 update, new concept doc?)

These are ADR-draft concerns; they don't change the design shape
above.

## Bundle size estimate (refined from sketches)

| Component | LOC |
|---|---|
| `pkg/lifecycle` package (Participant + Manager + Transitions + ListOptions + History) | ~400-550 |
| Rule actions (`lifecycle_transition`, `lifecycle_complete`, `lifecycle_fail`) | ~150-200 |
| Substitution paths (`$entity.lifecycle.*`) | ~80-120 |
| Operator gateway components | ~250-350 |
| BoundedDispatcher (pkg/dispatch or pkg/worker promote + KV completion wrapper) | ~150-200 |
| `.triples` enumeration primitive | ~50-100 |
| Sunset migration (executeTriggerWorkflow + workflow_trigger_payload removal) | ~50 |
| Tests across all of the above | ~600-800 |
| Docs (concept doc 14 update, new concept docs, ADR-047) | ~500 docs |
| **Total** | **~1700-2400 LOC code + ~500 LOC docs** |

That's close to the original ~1800-2650 estimate, with code shaved
slightly through tighter scope.

## PR sequencing for the bundle

1. **PR 1 — `pkg/lifecycle` substrate** (~500 LOC + tests). The
   Participant interface, Manager, Transitions, ListOptions,
   History. No rule integration yet; the package can be exercised
   via direct Go API for testing.
2. **PR 2 — Rule integration** (~250 LOC + tests). `lifecycle_*`
   actions, substitution paths. Sunset `executeTriggerWorkflow` +
   `workflow_trigger_payload`. Wire `pkg/workflow` → migration
   path notice (only agentic-loop affected; trivial).
3. **PR 3 — Operator gateway components** (~300 LOC + tests). HTTP
   handlers reading from Manager. Concept doc 14 update.
4. **PR 4 — BoundedDispatcher + `.triples`** (~200 LOC + tests).
   Co-located if scope fits; otherwise PR 4a + 4b.
5. **(Tag) — `v1.0.0-beta.85`** or whatever the next slot. Titled
   what it IS: "Lifecycle harness + BoundedDispatcher + `.triples`
   — workflow-shaped substrate."

Two to three weeks of code work, tractable in the
[[feedback_reactive_patches_vs_engine_completion]] discipline (deliberate
engine completion, not reactive patches).

## Next step

ADR-047 draft. Captures:
- The 8 resolutions above
- The harness shape
- Migration path
- Bundle plan
- Worked example (drone or semconnect, choose one for the ADR
  worked-example slot)

Once ADR-047 lands, the bundle work begins.
