# ADR-047: Lifecycle Harness Substrate

## Status

**SUPERSEDED by [ADR-049](049-lifecycle-harness-prime-schema-over-entity-states.md)**
— 2026-05-28. The "Manager owns per-workflow KV bucket"
architectural choice in this ADR shipped as v1.0.0-beta.85
(four-PR bundle #154/#155/#156/#157). The e2e build for the
lifecycle tier surfaced that the choice produces **graph-invisible
workflow state** — the lifecycle-managed entity's phase never
lands as a triple in ENTITY_STATES; the graph layer doesn't see
the workflow. ADR-049 redesigns the harness as a
schema-and-discipline layer over ENTITY_STATES (state changes
emit through graph-ingest like every other write), preserving the
Participant/Transitions/operator-API concepts from this ADR while
retiring the private-bucket substrate choice. beta.85 is
retroactively marked v0 of the harness; v1 ships in beta.86 on
the ADR-049 substrate.

The concepts in this ADR that carry forward into ADR-049:
- Participant interface (small contract on app domain structs)
- Transitions table with structural Validate
- Struct-tag parser (`lifecycle:"id|phase|readonly|operator_writable|indexable"`)
- Operator API surface (workflows + instances + state patch +
  transition + history + WebSocket stream)
- Phase drift detection
- The workflow-substrate concept itself

What changes in ADR-049:
- Manager does NOT own per-workflow KV buckets
- State changes go through graph-ingest via
  `UpdateEntityWithTriplesRequest`
- Schema declarations grow `ChildWorkflows`, `ReferencePredicates`,
  `AuditPredicates`
- History reads the fixed recent transition-record window in the current
  ENTITY_STATES value (ADR-049 amendment, gh#843)
- lifecycle-gateway slims to thin composition over graph-gateway
- Create semantics reframe to "add lifecycle dimension" (allows
  coexisting with existing non-lifecycle triples on the same entity)

The history below this status block is preserved as the design-process
record for what was decided + shipped in beta.85. The architectural
analysis remains valid as background; the per-workflow bucket
recommendation is no longer the canonical position — ADR-049 is.

---

**Original status (Accepted 2026-05-28)** — Shipped as the 4-PR
bundle (#154 / #155 / #156 / #157) and tagged v1.0.0-beta.85 with
lifecycle e2e tier green (`task e2e:lifecycle` — gateway +
rule-engine + Manager round-trip across 8 stages including
UDP-driven rule transition + operator transition + history replay
+ WebSocket live update).
Originally proposed 2026-05-24; resolves the workflow-primitives design
exercise ([proposal](../proposals/workflow-primitives-design-exercise.md),
[decision draft](../proposals/workflow-primitives-decision.md),
[robotic sketch](../proposals/workflow-primitives-robotic-sketch.md),
[semconnect sketch](../proposals/workflow-primitives-semconnect-sketch.md),
[semspec mapping](../proposals/workflow-primitives-semspec-mapping.md),
[design resolutions](../proposals/workflow-primitives-design-resolutions.md)).
Supersedes the C+ recommendation in the decision draft.

Companion ADR-048 covers BoundedDispatcher + `.triples` substrate
primitives.

## Context

### The problem

semstreams ships rules + components as orchestration substrate
(ADR-028). The implicit assumption was that
named-instance-with-lifecycle workflow shapes (drone missions,
manufacturing batches, scenario executions, API request lifecycles,
sensor lifecycles) would be composable from rules + components alone,
with each consumer inventing per-product convention for state
storage, KV key shape, terminal-state detection, restart recovery,
and operator visibility.

**This assumption was wrong**, and the framework has accumulated
evidence the wave-off was wrong:

1. **semspec hand-rolled ~7,840 LOC of workflow harness code** in
   `semspec/workflow/` to compensate for the framework not providing
   one. Two pieces are explicitly tagged in comments as "should move
   to framework" (`kv_helpers.go::WaitForKVBucket`,
   `dispatchretry/retry.go`); the rest is convention reinvention.

2. **`pkg/workflow.State` exists in semstreams** but is dead code.
   Only `agentic-loop` imports it, and only to satisfy `Participant`
   boilerplate that returns `nil` and a constant string. The shape
   is wrong (one `Context map[string]any` for app state when
   consumers need 40-field typed structs); the wiring is missing
   (no rule actions, no operator API).

3. **Cross-consumer sketches converge**: a drone-survey mission,
   a semconnect sensor lifecycle, and semspec's prior art all map
   to the same Participant interface and Manager API shape. Three
   distinct consumer classes (robotic / event-driven / agentic) all
   demand the same substrate.

4. **The five-tag pile (beta.80-84)** added rule-engine primitives
   for fan-out / multi-valued state without explicit framing as
   "complete the multi-valued primitive set." When deliberately
   examined, those primitives + a Lifecycle harness + BoundedDispatcher
   compose to cover every surveyed pattern; without the harness,
   consumers continue hand-rolling.

### What this is not

This ADR is **not**:

- A workflow engine — no runtime, no DSL, no state-machine
  interpreter
- A separate event bus — the harness uses existing NATS KV
  primitives
- A "process orchestrator" — orchestration stays in the rule engine
- A replacement for components — components remain the execution
  layer
- A breaking change to the rule engine — new actions are additive

It is a **substrate convention layer** that lets consumers declare
workflow-shaped entities and get framework infrastructure (KV
storage, restart recovery, operator API, rule integration) for free.

### Participation is per-entity, not per-deployment

semstreams is general-purpose stream-processing infrastructure;
the Lifecycle harness has zero dependencies on agentic-loop or any
other consumer class. Apps that ship no agentic features at all
(pure UDP-ingest + processor + HTTP-egress deployments) get the
full harness benefit by implementing `Participant` on their domain
state structs. The `pkg/lifecycle` package MUST NOT import any
`processor/agentic-*` package — verified by import lint at PR-1
land time.

`Participant` is opt-in per ENTITY-TYPE. Within a single app:

- Entity types with declared phases + restart recovery + operator
  visibility needs (drone missions, sensor lifecycles, manufacturing
  batches, scenario executions, API request lifecycles) implement
  `Participant` and use `Manager`.
- Entity types without that shape (raw telemetry samples, log
  entries, agent loops whose lifecycle is per-iteration LLM
  judgment rather than declared state-machine phases, transient
  inputs) stay outside the harness — they pay zero cost and the
  harness imposes no requirements on them.

Agentic-loop intentionally stays outside `Participant` in this
bundle: its lifecycle shape (per-iteration model judgment, dynamic
trajectory) doesn't fit the declared-transitions-table abstraction
cleanly. This is an entity-shape fit decision, not a framework
constraint — a future agentic role with declared phases could
implement `Participant` if the fit emerges. The 3 vestigial
`pkg/workflow` lines in agentic-loop are deleted in PR 2 because
they were never load-bearing, not because agentic-loop is being
excluded from harness participation as a matter of principle.

Apps must NOT assume the harness implies an agentic dependency,
and apps using the harness do NOT pull in agentic-loop or any
other consumer-class package.

### Greenfield assumption

`pkg/workflow` (the dormant existing surface) is unused except for
3 trivial lines in `agentic-loop`. This ADR's migration is
rip-and-replace — no back-compat shims, no parallel runtimes. The
existing 3 lines get deleted in the rule-integration PR.

## Decision

Ship `pkg/lifecycle` as the framework's workflow-shaped substrate.
Adopt the 8 design resolutions ([design resolutions doc](../proposals/workflow-primitives-design-resolutions.md))
verbatim.

### Public API

```go
package lifecycle

// Participant is the contract a lifecycle-tracked entity satisfies.
// Apps implement this on their domain state structs to get framework
// infrastructure (KV storage, rule integration, operator API).
type Participant interface {
    EntityID() string         // 6-part graph entity ID
    Workflow() string         // workflow type identifier
    Phase() string            // current lifecycle phase
    IsTerminal() bool         // true if entity is in terminal phase
    KVBucket() string                  // KV bucket this entity lives in
    KVKey(entityID string) string      // KV key shape for the given entity ID

    // ParentEntityID returns the parent workflow instance ID, or
    // empty for root workflows. Enables parent/child workflow
    // relationships (semspec's Plan-owns-Requirements pattern).
    ParentEntityID() string
}

// Transitions declares valid phase transitions per workflow type.
// from-phase → []to-phases. Terminal phases have empty out-edges.
type Transitions map[string][]string

// Manager is the framework-provided harness over Participant
// implementations.
type Manager struct{ /* unexported fields */ }

func NewManager(natsClient *natsclient.Client, logger *slog.Logger) *Manager

// Register tells the manager about a workflow type. Factory
// produces zero-value instances for KV deserialization;
// transitions table validates Transition calls and powers the
// state-machine introspection API.
func (m *Manager) Register(
    workflow string,
    factory func() Participant,
    transitions Transitions,
) error

// ---- Lifecycle operations ----
//
// All ops take an explicit workflow string. The workflow argument
// disambiguates entity IDs that may collide across registered
// workflow types within a single Manager — the harness does NOT
// derive workflow from entityID, since multi-workflow buckets and
// per-workflow ID conventions both exist in practice.

func (m *Manager) Get(ctx context.Context, workflow, entityID string) (Participant, error)
func (m *Manager) Create(ctx context.Context, initial Participant) error
func (m *Manager) Update(ctx context.Context, workflow, entityID string,
    mutator func(Participant) error) error
func (m *Manager) UpdateFromOperator(ctx context.Context, workflow, entityID string,
    patch map[string]any) error
func (m *Manager) Transition(ctx context.Context, workflow, entityID, newPhase string,
    source TransitionSource, note string) error
func (m *Manager) Complete(ctx context.Context, workflow, entityID string) error
func (m *Manager) Fail(ctx context.Context, workflow, entityID, reason string) error

// ---- Query operations ----

type ListOptions struct {
    Phase  string
    Active bool
    Match  map[string]any
    Limit  int
    Offset int
}

func (m *Manager) List(ctx context.Context, workflow string,
    opts ListOptions) ([]Participant, error)
func (m *Manager) Watch(ctx context.Context, workflow string) (<-chan Participant, error)
func (m *Manager) History(ctx context.Context, workflow, entityID string) ([]TransitionEvent, error)

// ---- Parent/child relationships ----
//
// Children and Ancestors scan across ALL registered workflows
// (a parent in workflow A may have children in workflow B — e.g.
// semspec's Plan-owns-Requirements). Complexity is
// O(sum-of-bucket-sizes) per call; apps with intra-workflow
// parent-child relationships should prefer
// List(workflow, Match{"parent_field": parentID}) which stays
// within one bucket.

func (m *Manager) Children(ctx context.Context, parentEntityID string) ([]Participant, error)
func (m *Manager) Ancestors(ctx context.Context, entityID string) ([]Participant, error)

// ---- Workflow introspection ----

type WorkflowDef struct {
    Workflow               string
    Transitions            Transitions
    KVBucket               string
    OperatorWritableFields []string // sorted JSON field names
}

func (m *Manager) GetWorkflowDefinition(workflow string) (WorkflowDef, error)
func (m *Manager) ListWorkflows() []WorkflowDef

// ---- Support types ----

type TransitionEvent struct {
    From      string
    To        string
    At        time.Time
    Triggered string  // "rule" / "operator" / "component" / "framework"
    Note      string
}
```

### Struct-tag conventions

Apps annotate state-struct fields:

```go
type SystemState struct {
    EntityID_   string `json:"entity_id"   lifecycle:"id"`
    Phase_      string `json:"phase"       lifecycle:"phase,readonly"`
    OwnerOrgID  string `json:"owner_org_id" lifecycle:"operator_writable,indexable"`
    // ... no tag = not operator-writable (default-deny)
}
```

Tag values:
- `id` — marks the EntityID field (one per struct)
- `phase` — marks the Phase field (one per struct)
- `readonly` — never operator-writable
- `operator_writable` — opt-in operator-writability via
  `Manager.UpdateFromOperator`
- `indexable` — flagged for secondary indexing when scale demands
  (v2 work; v1 ignores)

### Rule engine extensions

New action types in `processor/rule/actions.go`:

```go
const (
    ActionTypeLifecycleTransition = "lifecycle_transition"
    ActionTypeLifecycleComplete   = "lifecycle_complete"
    ActionTypeLifecycleFail       = "lifecycle_fail"
)
```

`lifecycle_transition` action shape:

```json
{
  "type": "lifecycle_transition",
  "phase": "flying",
  "set": {
    "current_waypoint_index": {"op": "increment"},
    "drone_id": "drone-7"
  }
}
```

The `set` field is optional. Each entry is either a literal value
(string, number, bool) or a typed operation object
(`{"op": "increment"}`, `{"op": "decrement"}`, `{"op": "set", "value": "..."}`).

`set` operations execute atomically inside a single
`Manager.Update` closure with optimistic concurrency. The phase
transition is the same atomic write.

New substitution paths:

- `$entity.lifecycle.phase` — current phase
- `$entity.lifecycle.terminal` — bool, true if in terminal phase
- `$entity.lifecycle.workflow` — workflow type string
- `$entity.lifecycle.workflow_def` — workflow definition (rarely
  needed in rules; mostly for tooling)

### Operator API

Gateway components implement HTTP endpoints under
`/workflows/{type}`:

| Endpoint | Behavior |
|---|---|
| `GET /workflows` | List registered workflow types with instance counts by phase |
| `GET /workflows/{type}?phase=X&active=true&limit=N&offset=M&{match}` | List instances matching `ListOptions` |
| `GET /workflows/{type}/{id}` | Get full instance state |
| `GET /workflows/{type}/{id}/history` | Phase transitions over the instance's lifetime |
| `GET /workflows/{type}/{id}/children` | Child instance summaries |
| `POST /workflows/{type}/{id}/state` | Operator patch (validates against `operator_writable` tags) |
| `POST /workflows/{type}/{id}/transition` | Explicit operator-initiated transition (validates against transitions table) |
| `WebSocket /workflows/{type}?stream=true` | Live updates via `Manager.Watch` |

The gateway components are NOT shipped as a single binary; each
operator deployment chooses HTTP/GraphQL/MCP gateway placement.
Framework provides Go-API gateway components that operators wire
into their existing gateway-component setup.

### Temporal conditions

No `$now` substitution. Use cron rules + state-stored timestamps:

```json
{
  "name": "schedule_maintenance",
  "type": "cron",
  "schedule": "every 1h",
  "when": {
    "bucket": "CSAPI_SYSTEMS",
    "conditions": [
      {"field": "$entity.lifecycle.phase", "op": "eq", "value": "active"},
      {"field": "$entity.triple.next_maintenance_due_unix", "op": "lte",
       "value": "$cron_fire_time_unix"}
    ]
  },
  "actions": [
    {"type": "lifecycle_transition", "phase": "maintenance"}
  ]
}
```

### Arithmetic in conditions

No arithmetic in substitution. App-side bookkeeping handles
last-iteration-style conditions: executors set `is_last_*: bool`
fields when they hit terminal indices; rules match the bool.

### Phase-transition validation

`Manager.Register` requires a `Transitions` table; `Manager.Transition`
rejects edges not in the table. Terminal phases (empty out-edges in
the table) align with `Participant.IsTerminal()`.

`Manager.GetWorkflowDefinition` exposes the table for operator
dashboards — the state-machine diagram is derivable directly.

### KV indexing

v1: linear KV scan in `Manager.List`. Applies filter in-process.
Pagination via `Limit + Offset`.

v2 (deferred): secondary index keyed by `(workflow, phase,
match_field)` for fast filter resolution at scale. Indexable fields
opt-in via struct tag (`lifecycle:"indexable"`). Triggered when an
operator demonstrates a bottleneck — not before.

### Scaling cliff (operator guidance)

The v1 linear-scan paths in `Manager.List`, `Manager.Children`, and
`Manager.Ancestors` are O(bucket-size) per call. Two scaling cliffs
operators should know about:

- **`Manager.List` with `Match`**: ~10K active instances per
  workflow is the rough threshold where the per-call scan cost
  starts mattering for operator-dashboard responsiveness. Cheap
  filters (`Phase`, `Active`) are applied before
  `Match`'s reflection step, so workflows whose dashboards
  predominantly filter on phase will scale further. File a
  bottleneck issue when an operator observes List-call latency
  in dashboard refresh; the v2 secondary-index work consumes
  `lifecycle:"indexable"`-tagged fields and is the upgrade path.
- **`Manager.Ancestors` and `Manager.Children`**: cross-workflow
  scans are O(sum-of-bucket-sizes) per call because they walk
  every registered workflow's bucket. Apps with deep parent-
  child chains in high-cardinality workflows feel this; apps
  whose parent-child relationships stay within a single workflow
  should prefer `List(workflow, Match{"parent_field": parentID})`
  for the children case (stays within one bucket and benefits
  from the v2 secondary index when it lands).

Neither `Manager.Get` nor `Manager.Update` is in this scaling
class — they're O(1) per call, suitable for per-message
coordinator hotpaths. `List`/`Watch`/`History`/`Children`/
`Ancestors` are operator-API-shaped (dashboard refresh, debugging,
audit) not per-message-shaped.

### Phase drift detection

`Manager.Get` (and the `Get` path used by `List`) validate that the
loaded entity's `Phase()` is declared in the registered
`Transitions` table. Drift surfaces as a `slog.Warn` log line
naming the entity, the undeclared phase, and the declared phase
set. Detection is log-only in v1 — apps wanting structured drift
detection (e.g. a `Degraded bool` field on the returned wrapper)
add it as a future API extension. The log signal is enough to
make the silent degradation visible without a wire-format change;
the `Degraded`-bool precedent (PR #137 / GH #120) is the
upgrade path when an operator demonstrates the need.

### Migration from `pkg/workflow`

Greenfield rip-and-replace:

1. Delete `pkg/workflow/` entirely
2. Delete `processor/agentic-loop/component.go` lines 1837-1845
   (`Phase()` returning `"agentic-execution"`, `StateManager()`
   returning nil) and the `pkg/workflow` import at line 23
3. Delete `processor/rule/workflow_trigger_payload.go` (legacy
   shim for retired `processor/reactive/`)
4. Delete `executeTriggerWorkflow` from `processor/rule/actions.go`
   (action type `trigger_workflow` was used only by the retired
   reactive engine)
5. Document the rip in the bundle PR description

Total deletion: ~340 LOC (`pkg/workflow/` + the dead shims).

## Worked example: drone-survey mission

The [robotic sketch](../proposals/workflow-primitives-robotic-sketch.md)
covers this end-to-end. Highlights:

App-side state struct + Participant impl: ~80 LOC including the
30-field `MissionState` struct and 7 interface methods.

Component shapes: each component (mission-planner, waypoint-
executor, safe-land-executor, weather-monitor) is a standard
semstreams component that calls `Manager.Get`, does work, then
`Manager.Update` or `Manager.Transition`. The harness handles all
KV serialization, optimistic concurrency, completion event
emission.

Rule pack: 9 rules orchestrate the lifecycle (kickoff_planning,
start_flying_after_plan, fly_current_waypoint, advance_after_capture,
land_after_last_waypoint, trigger_landing_component,
complete_after_landing, abort_compensation_dispatch,
trigger_safe_land). Each rule is short — phase-guard condition
matching plus one or two actions.

Operator API: `GET /workflows/drone-survey?active=true&org_id=acme`
returns active missions; per-mission detail at
`GET /workflows/drone-survey/{id}`; history endpoint shows phase
transitions over the mission's lifetime; `POST /abort` triggers
compensation flow.

End-to-end the consumer ships ~200 LOC of app-side code
(state struct + components + rules) for a complete workflow. The
framework provides the ~1400 LOC of harness substrate (estimated)
that makes it possible.

## Bundle plan

Three PRs over 2-3 weeks; one tag at the end.

### PR 1 — `pkg/lifecycle` substrate (~500-700 LOC)

- `Participant` interface
- `Manager` struct + Register/Get/Create/Update/Transition/Complete/Fail
- `Transitions` table validation
- `ListOptions`, `List`, `Watch`, `History` query ops
- `Children` / `Ancestors` parent/child ops
- `GetWorkflowDefinition` / `ListWorkflows` introspection
- Struct-tag parsing (`lifecycle:` tag namespace)
- Tests against a mock-driven fixture

No rule integration yet; the package is directly Go-API-testable.

### PR 2 — Rule integration + migration (~250-300 LOC)

- `lifecycle_transition`, `lifecycle_complete`, `lifecycle_fail`
  action types + executors
- Substitution paths (`$entity.lifecycle.*`,
  `$cron_fire_time_unix`)
- Action-config validation
- DELETE: `pkg/workflow/`, `processor/rule/workflow_trigger_payload.go`,
  `executeTriggerWorkflow`, agentic-loop's vestigial methods
- Tests for each action + condition substitution

### PR 3 — Operator gateway components (~250-350 LOC)

- HTTP handlers for `/workflows/*` endpoints
- WebSocket support for live updates
- Operator-patch validation against `operator_writable` tags
- Tests for each endpoint + the patch validation

### Tag — `v1.0.0-beta.85` (or next slot)

Titled what it IS: **"Lifecycle harness substrate +
BoundedDispatcher + `.triples` — workflow-shaped framework
primitives (ADR-047 + ADR-048)"**

ADDITIVE in semstreams (with one greenfield rip). Sister-project
guidance follows.

## Consequences

### Positive

- **Three consumer classes covered with one substrate**: agentic
  (semspec), event-driven (semconnect), robotic (drone fleet) all
  map to the same Participant + Manager surface.
- **~540 LOC of harness code replaced per-consumer**: semspec
  alone has ~540 LOC of harness shape that the framework now
  provides. Other consumers don't reinvent it.
- **Cross-product operator UX**: one workflow dashboard convention
  across products. A multi-tenant deployment can present
  drone-survey + sensor-fleet + plan-executions under one ops view.
- **Operator history by default**: `Manager.History` reads the fixed recent
  transition-record window retained in the current entity.
- **Restart recovery by default**: rule engine bootstraps from
  the workflow KV bucket; instances resume from current phase.
- **Discipline enforcement**: declared transitions tables make
  workflow state machines explicit at registration time.
- **Reduced workflow migration cost**: when convention evolves,
  a single framework release migrates all consumers (semspec's
  7,264-LOC `workflow/reactive/` retirement was per-consumer
  because conventions were per-consumer; this avoids the next
  occurrence).

### Negative

- **Framework surface expansion**: ~1700-2400 LOC of new code +
  ~500 LOC of docs across 3 PRs.
- **New ADR cluster**: ADR-047 + ADR-048 + concept doc 14 update;
  reviewers must absorb the harness shape.
- **Rule-engine action surface grows**: 3 new action types,
  4 new substitution paths. Action validators + tests scale.
- **Struct-tag noise**: app-side state structs carry `lifecycle:`
  tags. Idiomatic Go but extra characters per field.
- **Transitions table maintenance**: apps must enumerate
  transitions explicitly; adding a phase requires updating the
  table.
- **Linear KV scan v1**: at unexpected scale, `Manager.List` is
  O(N). v2 secondary index defers cost but consumers at
  millions-of-instances scale will need v2 sooner than later.
- **Discipline pressure**: easy to mis-use by treating workflow
  primitives as a workflow engine. Concept doc + ADR must make
  the harness-not-engine framing load-bearing.

### Risks

- **Naming confusion**: `pkg/lifecycle` is the package; "workflow"
  is the user-facing term in operator API + struct tags. Risk of
  internal inconsistency. Mitigation: package docs make naming
  explicit; gateway components consistently use "workflow."
- **Over-engineering creep**: the harness is positioned as
  bounded; future "small additions" could grow it into a
  workflow engine. Mitigation: every harness addition requires an
  ADR-047 amendment with cross-consumer evidence justification.
- **Operator UI gap**: framework ships API; per-product UIs are
  app-side work. Operators may expect a turnkey dashboard.
  Mitigation: concept doc 14 explicitly frames API-only; provide
  reference table-viewer example in docs.
- **Cross-product dashboard fragmentation**: API is uniform but
  per-product dashboards may diverge. Mitigation: document
  best-practice UI conventions; sister-projects share patterns.

## Open questions

The 8 design resolutions are settled. The following are
ADR-draft-phase choices the implementation work resolves:

1. **Exact JSON schemas** for the 3 new rule actions — drafted in
   PR 2; reviewed during PR review.
2. **Test fixture strategy** — fake `natsclient` + table-driven
   transitions vs integration-only. Lean fake-driven for unit
   tests + integration for end-to-end.
3. **Concept doc 14 update structure** — add Lifecycle harness as
   a new pattern in the catalog, or replace the existing patterns
   with the harness-flavored versions? Lean additive; preserve the
   existing pattern catalog and add a new section.
4. **Documentation strategy** — a new concept doc for the harness,
   or fold into concept doc 14? Lean new concept doc (27 or
   similar) for the harness specifically; concept 14 updates with
   forward-pointer.
5. **ADR-048 scope** — co-locate BoundedDispatcher + `.triples` or
   separate? Lean co-locate (small, related, single substrate
   completion narrative).

## Related decisions

- ADR-028 — Orchestration architecture (the three-layer rule
  skeleton + coordinator + ops that this ADR builds on)
- ADR-031 — Cron rules (temporal trigger primitive; the harness
  reuses for `schedule_maintenance`-style patterns)
- ADR-041 — Unified condition evaluator (the substitution layer
  this ADR extends with `$entity.lifecycle.*`)
- ADR-044 — OGC Connected Systems framework split (semconnect as
  one of the three evidence-providing consumer classes)
- ADR-045 — Graph search rule chain (agentic-loop consumer of
  pkg/workflow boilerplate that this ADR rips)
- ADR-046 — Parallel fan-out + gated DAG dispatch (Phase 1
  `for_each` shipped; Phase 2 superseded by this ADR +
  BoundedDispatcher per workflow-primitives-decision.md)
- ADR-048 (planned) — BoundedDispatcher + `.triples` substrate
  primitives (companion to this ADR)

## References

- [Workflow Primitives Design Exercise](../proposals/workflow-primitives-design-exercise.md) — gating proposal
- [Workflow Primitives Decision (C+ — superseded)](../proposals/workflow-primitives-decision.md) — initial recommendation; this ADR supersedes
- [Lifecycle Harness — Robotic Sketch](../proposals/workflow-primitives-robotic-sketch.md) — drone-survey worked example
- [Lifecycle Harness — semconnect Sketch](../proposals/workflow-primitives-semconnect-sketch.md) — event-driven worked example
- [semspec/workflow Mapping](../proposals/workflow-primitives-semspec-mapping.md) — 540 LOC harness slice verification
- [Workflow Primitives — Design Resolutions](../proposals/workflow-primitives-design-resolutions.md) — the 8 settled resolutions
- `pkg/workflow/` — dormant existing surface (to be deleted)
- `semspec/workflow/` — 7,840-LOC hand-rolled prior art
- `processor/rule/actions.go::executeTriggerWorkflow` — legacy
  shim (to be deleted)
- `processor/agentic-loop/component.go:1837-1845` — vestigial
  Participant boilerplate (to be deleted)
