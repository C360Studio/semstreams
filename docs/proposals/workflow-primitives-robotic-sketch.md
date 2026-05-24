# Lifecycle Harness — Robotic Example Sketch

**Status**: Research draft, 2026-05-24. Continues
[`workflow-primitives-decision.md`](workflow-primitives-decision.md) following
user pushback that the "+component" axis was waved off. Tests the proposed
Lifecycle harness shape against a fully non-LLM consumer (drone survey
mission) to validate the interface holds beyond agentic patterns.

**Not for commit yet**. This is a working sketch to surface gaps, not
the final design. Discovered gaps feed the decision-doc amendment.

## Setup

Survey company operates a fleet of N drones. Each customer order
triggers ONE survey mission: fly polygon P at quality Q, capture
sensor data at each waypoint, land. Hours-long. Operator monitors via
dashboard.

The mission is the workflow instance. The harness's job is to make the
mission listable, introspectable, restartable, and rule-driveable
without each operator inventing the convention.

## Domain & lifecycle

Phases (non-parameterized strings; counters are separate fields):

```
created → planning → planned → flying → captured ↺ landing → landed → completed
                                  ↓ (any non-terminal phase + abort signal)
                              aborting → safe-landed
                                  ↓ (hardware fault)
                              failed
```

Terminal phases: `completed`, `failed`, `safe-landed`.

The `flying ↺ captured` loop iterates per waypoint until the last,
then transitions to `landing`. Abort signals (weather, battery,
operator) transition from any non-terminal phase to `aborting`,
which triggers safe-land compensation.

## App-side state

```go
package dronesurvey

type MissionState struct {
    // Identity
    EntityID_ string `json:"entity_id"`  // 6-part graph ID
    MissionID string `json:"mission_id"`
    Workflow_ string `json:"workflow"`   // "drone-survey"
    Version   string `json:"version"`    // "v1" or "v2" — process definition pin

    // Lifecycle (framework reads via Participant)
    Phase_      string     `json:"phase"`
    StartedAt   time.Time  `json:"started_at"`
    UpdatedAt   time.Time  `json:"updated_at"`
    CompletedAt *time.Time `json:"completed_at,omitempty"`

    // Operator inputs
    CustomerOrder  string `json:"customer_order"`
    PolygonGeoJSON string `json:"polygon_geojson"`
    Quality        string `json:"quality"`   // "draft" / "production"

    // Plan output
    WaypointsRef   string `json:"waypoints_ref"`  // ObjectStore ref
    TotalWaypoints int    `json:"total_waypoints"`

    // Runtime
    CurrentWaypointIndex int    `json:"current_waypoint_index"`
    DroneID              string `json:"drone_id"`

    // Telemetry (sampled; latest only on the entity)
    LastBatteryPercent int       `json:"last_battery_percent"`
    LastTelemetryAt    time.Time `json:"last_telemetry_at"`
    LastWeatherCheck   string    `json:"last_weather_check"`

    // Capture output
    DataRefs []string `json:"data_refs"`  // ObjectStore refs per waypoint

    // Abort context
    AbortReason  string `json:"abort_reason,omitempty"`
    AbortTrigger string `json:"abort_trigger,omitempty"` // "weather"/"battery"/"operator"
}

// Participant interface implementation
func (m *MissionState) EntityID() string  { return m.EntityID_ }
func (m *MissionState) Workflow() string  { return m.Workflow_ }
func (m *MissionState) Phase() string     { return m.Phase_ }
func (m *MissionState) KVBucket() string  { return "MISSIONS" }
func (m *MissionState) KVKey() string     { return "mission." + m.MissionID }

func (m *MissionState) IsTerminal() bool {
    switch m.Phase_ {
    case "completed", "failed", "safe-landed":
        return true
    }
    return false
}
```

**App owns the schema fully.** Framework reads only the Participant
interface methods. ~30 lines of interface satisfaction; the 30-field
struct is untouched.

## Components

### `mission-planner`

```go
type Component struct {
    lifecycle *lifecycle.Manager
    planner   *waypointPlanner
}

func (c *Component) handlePlanRequest(ctx context.Context, missionID string) error {
    participant, err := c.lifecycle.Get(ctx, missionID)
    if err != nil { return err }
    mission := participant.(*MissionState)

    waypoints, err := c.planner.Compute(mission.PolygonGeoJSON, mission.Quality, mission.Version)
    if err != nil {
        // Failure → terminal phase via framework helper
        return c.lifecycle.Fail(ctx, missionID, fmt.Sprintf("planning: %v", err))
    }

    ref, err := c.storeWaypoints(ctx, waypoints)
    if err != nil { return err }

    // Atomic update — phase + waypoints ref + total count
    return c.lifecycle.Update(ctx, missionID, func(p lifecycle.Participant) error {
        m := p.(*MissionState)
        m.WaypointsRef = ref
        m.TotalWaypoints = len(waypoints)
        m.Phase_ = "planned"
        return nil
    })
}
```

Component owns its slice of work + the phase transition it produces
(`planning → planned`). The framework handles KV serialization,
optimistic concurrency, completion event emission.

### `waypoint-executor`

```go
func (c *Component) handleFlyRequest(ctx context.Context, missionID string) error {
    participant, err := c.lifecycle.Get(ctx, missionID)
    if err != nil { return err }
    mission := participant.(*MissionState)

    waypoint := c.loadWaypoint(mission.WaypointsRef, mission.CurrentWaypointIndex)

    if err := c.flyToAndCapture(waypoint); err != nil {
        // Don't transition here — abort flow owns "*" → "aborting"
        return err
    }

    capturedDataRef, err := c.storeCaptureData(ctx)
    if err != nil { return err }

    return c.lifecycle.Update(ctx, missionID, func(p lifecycle.Participant) error {
        m := p.(*MissionState)
        m.DataRefs = append(m.DataRefs, capturedDataRef)
        m.Phase_ = "captured"
        return nil
    })
}
```

Note: this component does NOT decide whether to advance to the next
waypoint or transition to landing. That's a rule decision (see
"advance vs land" rules below). Component does work; rule sequences.

### `weather-monitor` (parallel watcher)

```go
func (c *Component) onWeatherUpdate(ctx context.Context, status string) error {
    if status != "abort" { return nil }

    // Find all active missions of this workflow type
    missions, err := c.lifecycle.List(ctx, "drone-survey", lifecycle.FilterActive)
    if err != nil { return err }

    for _, p := range missions {
        // Signal abort by writing trigger triple — rules pick up
        c.publishAbortSignal(p.EntityID(), "weather")
    }
    return nil
}
```

Watcher is a normal component. The harness's `Manager.List(workflow,
filter)` API gives it the active-mission set for free. Watcher emits
abort SIGNALS via triples; rules transition phase. Single-writer
discipline preserved.

### `safe-land-executor`

```go
func (c *Component) handleAbort(ctx context.Context, missionID string) error {
    participant, err := c.lifecycle.Get(ctx, missionID)
    if err != nil { return err }
    mission := participant.(*MissionState)

    // Compensation logic — land at current position
    if err := c.safelyLand(mission.DroneID); err != nil {
        return c.lifecycle.Fail(ctx, missionID, fmt.Sprintf("safe-land: %v", err))
    }

    return c.lifecycle.Update(ctx, missionID, func(p lifecycle.Participant) error {
        m := p.(*MissionState)
        m.Phase_ = "safe-landed"
        return nil
    })
}
```

Standard component shape: read state → do work → write state.

### Component registration

```go
// At process bootstrap
mgr := lifecycle.NewManager(natsClient, logger)

mgr.Register("drone-survey", func() lifecycle.Participant {
    return &MissionState{}
})

// Components receive the manager via deps
planner := &Component{lifecycle: mgr, planner: newPlanner()}
executor := &Component{lifecycle: mgr}
```

One registration call per workflow type. Factory tells the manager
how to deserialize from KV when serving operator queries or watcher
streams.

## Rule pack (workflow orchestration)

```json
[
  {
    "name": "kickoff_planning",
    "when": {
      "bucket": "MISSIONS",
      "conditions": [
        {"field": "$entity.lifecycle.phase", "op": "eq", "value": "created"}
      ]
    },
    "actions": [
      {"type": "publish", "subject": "component.mission-planner.{entity.id}"}
    ]
  },
  {
    "name": "start_flying_after_plan",
    "when": {
      "bucket": "MISSIONS",
      "conditions": [
        {"field": "$entity.lifecycle.phase", "op": "eq", "value": "planned"}
      ]
    },
    "actions": [
      {"type": "lifecycle_transition", "phase": "flying"}
    ]
  },
  {
    "name": "fly_current_waypoint",
    "when": {
      "bucket": "MISSIONS",
      "conditions": [
        {"field": "$entity.lifecycle.phase", "op": "eq", "value": "flying"}
      ]
    },
    "actions": [
      {"type": "publish", "subject": "component.waypoint-executor.{entity.id}"}
    ]
  },
  {
    "name": "advance_after_capture",
    "when": {
      "bucket": "MISSIONS",
      "conditions": [
        {"field": "$entity.lifecycle.phase", "op": "eq", "value": "captured"},
        {"field": "$entity.triple.current_waypoint_index", "op": "lt", "value": "$entity.triple.total_waypoints - 1"}
      ]
    },
    "actions": [
      {"type": "increment_triple", "predicate": "current_waypoint_index"},
      {"type": "lifecycle_transition", "phase": "flying"}
    ]
  },
  {
    "name": "land_after_last_waypoint",
    "when": {
      "bucket": "MISSIONS",
      "conditions": [
        {"field": "$entity.lifecycle.phase", "op": "eq", "value": "captured"},
        {"field": "$entity.triple.current_waypoint_index", "op": "eq", "value": "$entity.triple.total_waypoints - 1"}
      ]
    },
    "actions": [
      {"type": "lifecycle_transition", "phase": "landing"}
    ]
  },
  {
    "name": "trigger_landing_component",
    "when": {
      "bucket": "MISSIONS",
      "conditions": [
        {"field": "$entity.lifecycle.phase", "op": "eq", "value": "landing"}
      ]
    },
    "actions": [
      {"type": "publish", "subject": "component.landing-executor.{entity.id}"}
    ]
  },
  {
    "name": "complete_after_landing",
    "when": {
      "bucket": "MISSIONS",
      "conditions": [
        {"field": "$entity.lifecycle.phase", "op": "eq", "value": "landed"}
      ]
    },
    "actions": [
      {"type": "lifecycle_complete"}
    ]
  },
  {
    "name": "abort_compensation_dispatch",
    "when": {
      "bucket": "MISSIONS",
      "conditions": [
        {"field": "$entity.triple.abort_trigger", "op": "neq", "value": ""},
        {"field": "$entity.lifecycle.terminal", "op": "eq", "value": false},
        {"field": "$entity.lifecycle.phase", "op": "neq", "value": "aborting"},
        {"field": "$entity.lifecycle.phase", "op": "neq", "value": "safe-landed"}
      ]
    },
    "actions": [
      {"type": "lifecycle_transition", "phase": "aborting"}
    ]
  },
  {
    "name": "trigger_safe_land",
    "when": {
      "bucket": "MISSIONS",
      "conditions": [
        {"field": "$entity.lifecycle.phase", "op": "eq", "value": "aborting"}
      ]
    },
    "actions": [
      {"type": "publish", "subject": "component.safe-land-executor.{entity.id}"}
    ]
  }
]
```

9 rules total. Pattern is clean: phase guards trigger work; work
completes by writing the next phase; rule re-evaluates.

## Operator API (free with the harness)

The framework provides gateway components reading from `Manager.List` /
`Manager.Get` / `Manager.Watch` / `Manager.History`:

```
GET /workflows
→ [
    {"type": "drone-survey", "active": 12, "completed": 47, "failed": 1},
    {"type": "semspec-task", "active": 47, "completed": 1244, "failed": 12}
  ]

GET /workflows/drone-survey?status=active
→ [
    {"entity_id": "...", "mission_id": "abc", "phase": "flying",
     "current_waypoint": 7, "total_waypoints": 12, "drone_id": "...",
     "last_battery_percent": 67, "started_at": "..."},
    ...
  ]

GET /workflows/drone-survey/{id}
→ {full MissionState JSON}

GET /workflows/drone-survey/{id}/history
→ [
    {"phase": "created", "at": "..."},
    {"phase": "planning", "at": "..."},
    {"phase": "planned", "at": "..."},
    ...
  ]

POST /workflows/drone-survey/{id}/abort
→ {publishes abort signal; returns 202}
```

The history endpoint is interesting. It comes from KV history
replay — `bucket.History(key)` returns all revisions; the harness
synthesizes phase-transition events from revision diffs. **Free
audit trail.**

The `POST .../abort` is a privileged operator action; the framework
provides the endpoint, the app provides the abort-signal triple
shape (in this case `abort_trigger` predicate).

## Tricky cases the sketch passes

### 1. Abort compensation

Weather monitor publishes abort_trigger triple. `abort_compensation_dispatch`
rule matches `abort_trigger != "" AND !terminal AND phase != aborting`.
Transitions to `aborting`. `trigger_safe_land` rule fires.
`safe-land-executor` runs. Transitions to `safe-landed`. Terminal.

The guard on `phase != "aborting"` prevents the rule from re-firing
on the entity it just transitioned. The guard on `lifecycle.terminal`
prevents firing on already-aborted/completed/failed missions. Both
of these are existing rule-engine condition shapes.

### 2. Restart recovery

Rule engine bootstraps from MISSIONS bucket. Each mission's current
phase tells rules where to pick up. `flying` mission gets
`fly_current_waypoint` re-fired; component handles "drone is
already at this waypoint" idempotently (component design discipline,
not framework).

### 3. Versioning (v1 → v2 process)

`Version` field on `MissionState`. `mission-planner` reads it and
branches algorithm. Operator dashboards show version column.
Framework persists; app handles logic.

### 4. Multi-mission concurrent operations

Each mission has its own entity. Rules fire per-entity. The
`waypoint-executor` consumer has `MaxAckPending=N` (one per drone)
on its JetStream consumer. Bounded concurrency is JetStream-native;
BoundedDispatcher is alternative substrate if more control needed.

### 5. Operator manual override

`POST /workflows/{id}/state` with a partial JSON merge → framework
applies via `Manager.Update`. App can declare which fields are
operator-writable (a struct tag or a registration option).

Alternative: operator publishes a NATS signal triple; a rule
processes it. More aligned with the orchestration discipline; less
ergonomic from a UI perspective. Probably want both available;
operator API is sugar for the rule-driven path.

### 6. KV value size

`MissionState` is small (~hundreds of bytes). For larger states
(semspec's TaskExecution can reach 4-8KB), KV is still fine — NATS
default limit is 1MB. For state that needs bulky carrying (file
contents, captured imagery), the existing `ContentStorable`
pattern handles it: refs as triples on the entity, content in
ObjectStore. The harness doesn't need to know.

## Gaps discovered in the harness sketch

### Gap 1 — Atomic multi-field transitions in rule actions

`advance_after_capture` rule has TWO actions:
1. `increment_triple` on `current_waypoint_index`
2. `lifecycle_transition` to `flying`

These need to be atomic. If only (1) succeeds, the mission has a
bumped index but stays in `captured` phase — `advance_after_capture`
re-fires next tick, double-increment.

**Options**:
- **A**: Rule engine supports "transaction" semantics — actions in a
  rule succeed-or-rollback as a unit. Significant rule-engine
  surface addition.
- **B**: Rule actions chain via single optimistic-concurrency token;
  if any action fails, subsequent ones skip. Smaller addition.
- **C**: `lifecycle_transition` action takes optional field updates
  in its signature: `{"type": "lifecycle_transition", "phase": "flying",
  "increment": {"current_waypoint_index": 1}}`. Composes
  state update with transition in one action. Cleanest for the
  Lifecycle harness specifically.

C is the smallest addition and matches the harness's `Manager.Update`
closure semantics naturally.

### Gap 2 — Arithmetic in substitution

`{"field": "$entity.triple.current_waypoint_index", "op": "eq",
"value": "$entity.triple.total_waypoints - 1"}` — the `- 1` isn't
supported. Two paths:

- **A**: Add expression support to substitution layer (typed
  expressions). Big change.
- **B**: Add operator `eq_minus_one` / `eq_plus_one` etc. Doesn't
  generalize.
- **C**: Frame the comparison differently — `last_waypoint_reached:
  bool` field on the state that the executor sets to `true` when it
  hits the final waypoint. Rule matches the boolean. App-side
  bookkeeping, but matches the "components own intermediate phase"
  discipline.
- **D**: A `length_minus_one` substitution suffix or an
  `is_last_iteration` predicate. Narrow but composable.

The choice depends on how often arithmetic shows up. In the drone
sketch it's once. In a real workflow library it likely shows up
many times. **Lean A (expression substitution), but flag as a
separate ADR if it's a multi-tag commitment.**

### Gap 3 — Operator manual-override API

The framework provides `Manager.Update`. The operator API endpoint
needs to call it. But: which fields are operator-writable? Allowing
arbitrary patches on the State struct lets an operator brick a
mission by writing inconsistent state.

**Resolution**: `Participant` interface gains an `OperatorWritableFields()
[]string` method. Or use struct tags (`lifecycle:"operator_writable"`).
Framework enforces. Apps opt in to what's safe.

### Gap 4 — Cross-instance correlation (parent/child workflows)

semspec's PrereqContext: Plan owns N Requirements; each Requirement
owns N Tasks. The drone-survey sketch doesn't exercise this, but
robotic batch processing would (Batch owns N Widgets; each Widget
moves through M stations).

**Sketch**:
```go
type Participant interface {
    EntityID() string
    Workflow() string
    Phase() string
    IsTerminal() bool
    KVBucket() string
    KVKey() string

    // OPTIONAL — for parent/child workflows
    ParentEntityID() string  // empty if root
}
```

Framework's `Manager.Children(parentID)` returns child instances.
Framework's `Manager.Ancestors(entityID)` walks up. Useful for
operator dashboards and rule conditions.

Manager.WaitForChildren(parentID, predicate) — useful for fan-in
patterns. Or compose from `Manager.Watch(workflow)` + filter
client-side.

### Gap 5 — Phase-transition validation

Currently, `Manager.Transition(entityID, "flying")` accepts any
phase string. A rule could transition `safe-landed → planning`
which is nonsensical.

**Options**:
- **A**: App declares valid transitions in registration:
  `mgr.Register("drone-survey", factory, lifecycle.Transitions{...})`.
  Framework validates on every transition.
- **B**: App validates internally in the Update closure (returns
  error for invalid transitions). App-side enforcement.
- **C**: No validation; framework trusts rules to behave. Discipline
  in rule design, not framework.

Lean A — declaring valid transitions makes the workflow graph
introspectable (operator dashboard can show the state machine
diagram for free). Adds modest LOC.

### Gap 6 — Watcher-style components and `List` performance

`weather-monitor` calls `Manager.List("drone-survey", filter)` on
every weather update. For 12 active missions this is cheap. For
12,000 it's not.

**Resolution**: framework provides indexed views. `Manager.Watch`
already returns a live stream; `Manager.List` can be backed by a
secondary KV index keyed by `(workflow, phase)` for fast filtering.
Operationally simple — secondary index is itself a KV bucket
maintained by the framework.

### Gap 7 — Cron-rule integration

Drone-survey might want a periodic battery-check rule: every
30 seconds, scan all active missions, alert if any below
threshold. semstreams has cron rules (ADR-031). They'd need to
work with `lifecycle.list` / `lifecycle.iter` substitution paths.

Existing cron rule + `for_each` over `Manager.List` result should
compose. The framework needs a substitution path that lets a cron
rule iterate active missions.

### Gap 8 — Component registration discoverability

Components that implement Lifecycle handlers (mission-planner,
waypoint-executor) need to be discoverable for flow-graph
validation. semstreams's component framework has `Discoverable`
interface; lifecycle components add a `Handles(workflow, phase)`
method? Or are they just regular components that happen to call
`Manager.Update`?

Lean: regular components, no extra discoverability. The harness
isn't a component-framework extension; it's a substrate components
*use*. Flow-graph validation works at the port/subject level as
today.

## What still hasn't been examined

The drone-survey sketch validated the interface holds against a
zero-LLM consumer. Not yet validated:

- **Versioned process migration** — what happens when v1 missions are
  in-flight and v2 ships. Sketch assumes app handles. Is there a
  framework concern around per-instance version pinning vs
  per-deployment process definition?
- **Multi-tenancy** — drone-survey-co with multiple customers. Each
  customer's missions need isolation. semspec has TraceID; the
  harness doesn't yet have a multi-tenant story.
- **Deadlines and timeouts** — a mission that hasn't transitioned in
  N hours should alert. Cron rule + `last_telemetry_at` field could
  do it. Framework concern or app concern?
- **Compensation rollback chains** — abort triggers safe-land. What
  if safe-land fails? Recursive compensation? The drone case has a
  flat compensation; richer cases (multi-stage rollback) might want
  declared compensation graphs. semspec has scenario-level rollback
  shapes; need to look at those.
- **Cross-workflow events** — drone-survey completes; another
  workflow (post-flight-analysis) starts. Today done via rules
  watching the lifecycle.completed event. Pattern works; framework
  doesn't need to do more.

## Implications for the harness sketch

**The interface holds.** Eight gaps surfaced, six are narrow and
bounded (atomic transitions, operator-writable fields, parent/child,
transition validation, indexed views, cron substitution). Two are
more open (arithmetic in substitution, versioned process migration)
— those deserve their own ADRs if shipped.

**Implementation scope estimate** (revised from C+ as honest):

| Piece | Scope | LOC est |
|---|---|---|
| `pkg/lifecycle` (replaces `pkg/workflow`) | Substrate primitive | 400-600 |
| Participant interface + Manager + KV indexing | Core | 250-350 |
| Rule actions (`lifecycle_transition`, `lifecycle_complete`, `lifecycle_fail`) | Rule integration | 150-200 |
| Rule substitution (`$entity.lifecycle.*`) | Rule integration | 100-150 |
| Operator gateway components | Operator API | 200-300 |
| `BoundedDispatcher` (still) | Substrate | 150-250 |
| `.triples` (still) | Rule primitive | 50-100 |
| Tests, examples, docs | Quality | 500-700 |
| **Total** | | **~1800-2650 LOC** |

That's 3-5× C+'s scope. It's not a one-tag bundle. It's a tagged
sequence — maybe 3 tags over 2-3 weeks — with a clear ADR per
chunk.

This is roughly what outcome B was sketched as. The drone example
mostly affirms B-shaped scope while keeping the discipline ("harness
not engine, components compose into rules, no DSL").

## Open questions for the next research turn

1. **Should the harness allow `pkg/workflow.State` as a base struct
   apps embed, or is an interface-only contract better?** Embedding
   gives apps free fields (Iteration, MaxIter, CompletedAt) but
   constrains schema. Interface-only is cleaner but apps must
   reinvent the common fields. Lean interface-only with framework
   convenience helpers for common shapes.
2. **Is `Workflow()` returning a string the right discriminator, or
   should it be a typed registry?** Strings are simpler; typed
   gives compile-time checking. Strings + factory registration is
   what semspec already does.
3. **Does the rule action `lifecycle_transition` deserve to be in the
   rule engine OR should it be a generic `update_field` action with
   the harness watching for phase-field writes?** First is more
   intentional; second is more orthogonal.
4. **Is the `Participant` name correct or do we want something more
   evocative?** `Lifecycle`, `WorkflowState`, `Instance`,
   `LifecycleEntity` are candidates. Naming matters for adoption.
5. **Versioning, multi-tenancy, deadlines — separate ADRs or part of
   this?** Probably separate. The harness is foundational; those
   are extensions.

## Position before commit

The harness shape genuinely holds against the robotic example. The
+component axis IS framework territory; the wave-off in
[`workflow-primitives-decision.md`](workflow-primitives-decision.md)
was wrong. The decision doc needs a substantial amendment OR
replacement with a revised recommendation toward outcome B'
(constrained workflow primitive = Lifecycle harness, NOT workflow
engine).

The scope is ~1800-2650 LOC across the framework, replacing ~500
LOC of generic harness across N consumers (semspec alone has 500 of
the 7,900 that's harness-shaped; other consumers will repeat).
**Net cost positive in framework LOC; net cost negative in
consumer-aggregate LOC across N products.**

Before committing to this direction, want answers on:
- Q1-Q5 above (interface shape, naming, action shape, etc.)
- Whether semconnect's near-term work would benefit from the
  harness (validates "across consumer classes")
- Whether a second design exercise around versioning/multi-tenancy
  should precede the harness or follow

Recommended next research turn: **examine semspec/workflow in detail
to confirm the 500-LOC harness-shaped slice estimate, and bring at
least one more consumer class (likely semconnect API request
lifecycle in long-form) against the harness shape**.
