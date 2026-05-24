# Workflow Primitives Design Exercise — Decision

**Status**: Draft decision document, 2026-05-24. Resolves the open question from
[`workflow-primitives-design-exercise.md`](workflow-primitives-design-exercise.md).
Recommends **outcome C+** — BoundedDispatcher ships now as a deliberately
named substrate primitive; the dormant `pkg/workflow` instance-state surface
gets a wire-it-or-cut-it decision in the same tag bundle; first-class
workflow runtime defers to a second design exercise once production evidence
accrues across ≥2 consumer classes.

**Gate**: This document is the artifact the proposal asks for. Until it
lands on main, ADR-046 Phase 2, GH #151, and any new rule-engine "fan-out
patch" tag remain frozen.

## TL;DR

Six pattern sketches were run across (a) rules+components only, (b) +
BoundedDispatcher, (c) +workflow primitives. Tier classifications:

| Pattern | Best variant | Gap tier | Note |
|---|---|---|---|
| P1 ADR-045 R0-R6 graph-research | (a) | 0 | Clean today; sketch is purely confirmatory |
| P2 dynamic-N investigators | (b) | 2 | Code-component fan-out via `pkg/worker`; BoundedDispatcher names what's already idiomatic |
| P3 drone survey mission | (b)+pkg/workflow wired | 2 (with caveat) | Workflow-shaped, but the substrate already covers it; first-class runtime would be operator-UX, not capability |
| P4 manufacturing batch | (b)+pkg/workflow wired | 2 (with caveat) | Same shape as P3; per-widget instances cluster the demand |
| P5 semconnect API request | (a) or trivial (b) | 0 | Workflow primitives over-engineer the short-lived case |
| P6 semspec scenario-orchestrator | (b) | 2 | BoundedDispatcher + rule primitives replace ~600 LOC verbatim |

**Five of six patterns land in tier 2** once BoundedDispatcher exists.
Tier-3 demand from P3/P4/P6 is real but is **operator-UX-shaped** (named
instance list, lifecycle introspection, versioning) — not
runtime-capability-shaped. The framework already has the underlying
primitives (`pkg/workflow.State` + `StateManager`) and they are essentially
dead code today. The honest path is to commit the substrate (BoundedDispatcher),
make the wire-it-or-cut-it call on `pkg/workflow`, and let a second design
exercise — gated on real cross-consumer adoption — answer whether
operator-UX-shaped workflow primitives ship.

**One surprising finding drives the framing shift**: the codebase already
contains `pkg/workflow.State` (full lifecycle: ID, WorkflowID, Phase,
Iteration, MaxIter, StartedAt, CompletedAt, Error, Context), `StateManager`
with optimistic concurrency, `Participant` interface, and a stale
`trigger_workflow` rule action pointing at the retired
`processor/reactive/`. Nothing wires through to rules. Nothing is operator-
visible. Only `agentic-loop` imports the package, and only to satisfy the
`Participant` interface with no actual state writes. The exercise is
not "should we build workflow primitives?" — it's "what do we do with the
ones we already have?"

## Background and reading

This document descends from:

- [`workflow-primitives-design-exercise.md`](workflow-primitives-design-exercise.md) — the proposal that gated this work
- `project_rules_engine_design_review` memory — the original pause-point analysis
- `feedback_reactive_patches_vs_engine_completion` memory — the discipline trigger
- [`docs/concepts/14-orchestration-layers.md`](../concepts/14-orchestration-layers.md) — current pattern catalog (no workflow layer)
- [`docs/concepts/25-phased-agentic-chains.md`](../concepts/25-phased-agentic-chains.md) — phased agentic chain pattern
- [`docs/adr/028-orchestration-architecture.md`](../adr/028-orchestration-architecture.md) — three-layer orchestration
- [`docs/adr/046-parallel-fan-out-and-gated-dag-dispatch.md`](../adr/046-parallel-fan-out-and-gated-dag-dispatch.md) — Phase 2 gated on this exercise
- `semspec/processor/scenario-orchestrator/component.go` — surviving 600 LOC, prior art
- `pkg/workflow/state.go` + `pkg/worker/pool.go` — the surprising-finding files

## Surprising finding: workflow primitives already exist in the codebase

### `pkg/workflow/state.go` — full instance state surface, dormant

```go
type State struct {
    ID          string            // unique execution identifier
    WorkflowID  string            // workflow definition this instance follows
    Phase       string            // current phase/step
    Iteration   int               // loop count
    MaxIter     int               // iteration cap
    StartedAt   time.Time
    UpdatedAt   time.Time
    CompletedAt *time.Time        // nil = still running
    Error       string            // last error message
    Context     map[string]any    // workflow-specific data
}
```

With `StateManager`: `Get`, `Create`, `Put`, `Update` (optimistic concurrency
via revision), `Transition(phase)`, `IncrementIteration`, `Complete`, `Fail`,
`List`, `Delete`. Default bucket `WORKFLOW_STATE`. This is what a
first-class workflow API would expose — and it's already on disk.

### `pkg/workflow/participant.go` — components declare workflow membership

```go
type Participant interface {
    WorkflowID() string
    Phase() string
    StateManager() *StateManager
}
```

Plus `ParticipantRegistry` for topology discovery. The discoverability
substrate is there.

### `pkg/worker/pool.go` — generic bounded worker pool, already shipping

Generic `Pool[T]`, bounded queue with backpressure, context-aware
cancellation, statistics + optional Prometheus metrics, dual-tracking
observability. This is BoundedDispatcher minus the explicit KV-twofer
completion-watch integration.

### `processor/rule/actions.go` — legacy `trigger_workflow` action

```go
ActionTypeTriggerWorkflow = "trigger_workflow"
// Publishes to workflow.trigger.<workflow_id>
```

Pointed at `processor/reactive/`, which was **retired** in 2026-03-12.
Nothing consumes the subject anymore. The action exists; the runtime
behind it does not. `processor/rule/workflow_trigger_payload.go` is the
matching payload type.

### Current import graph

```
processor/agentic-loop  → pkg/workflow  (Participant boilerplate only)
processor/graph-index   → pkg/worker    (real use)
                          pkg/workflow  (no other importer)
                          pkg/worker    (no other importer)
```

`pkg/workflow` is vestigial. `pkg/worker` has one real consumer but isn't
named "the framework's bounded-concurrency primitive" — it's just a
worker pool one component happens to use.

### What this reframes

The exercise was scoped around "should we build workflow primitives?"
The honest framing is: **the primitives exist; the wiring doesn't.** Three
sub-questions:

1. **Should BoundedDispatcher be named first-class?** It already exists
   as `pkg/worker.Pool` minus the KV-twofer wrapper. The naming question
   is whether to claim it as substrate (and possibly migrate
   `pkg/worker` consumers to a re-exported name) or to ship a thin
   wrapper that's KV-twofer-aware.

2. **Should `pkg/workflow` be wired through?** The State + StateManager
   are full-featured but invisible to the rule engine. Either we wire
   rule actions (`workflow_create`, `workflow_transition`, `workflow_complete`)
   + conditions (`$workflow.state.phase == "X"`) + an operator gateway
   surface, OR we delete the package because dead code claiming to be
   a workflow API is worse than no claim at all.

3. **If we wire `pkg/workflow`, does the rule engine need new
   primitives to compose with it?** This is the original five-tag-pile
   question, now reframed: are `for_each`, `.length`, Subject override,
   array operators primitives that compose with named workflow
   instances, or are they composable enough that workflow instances are
   redundant?

The decision below answers (1) yes, claim it; (2) defer with a sunset
clock — wire-it-or-cut-it in the next tag bundle, decision based on
production evidence; (3) defer until (2) resolves.

## Pattern sketches

For each pattern, sketches in three variants:

- **(a)** Rules + components only — the discipline today
- **(b)** Rules + components + BoundedDispatcher named substrate primitive
- **(c)** Workflow primitives layered on top (`pkg/workflow` wired + new
  rule actions + operator API)

Evaluated on: clarity, completeness, operator audit, instance tracking,
restart recovery, framework cohesion, honest-vs-hidden state plumbing.

---

### Pattern 1 — ADR-045 R0-R6 graph-research chain

**Shape**: Sequential 5-phase agent chain. classify → route → execute →
assess → synthesize. Per-loop entity in `AGENT_LOOPS` carries decision
triples. Seven rules drive transitions; `read_loop_result` carries
inter-phase data. Internal-only (no external composition).

#### (a) Rules + components only

Five components register; seven rules wire transitions. Phase 1 is
in-flight on this exact shape — PRs #131 and #135 merged. State lives
on the loop entity in `AGENT_LOOPS`; bulky outputs go to ObjectStore
via `ContentStorable`; rules carry references.

```text
research_graph(topic, hints?)
  → R0: bootstrap (publish_agent role=nl_classify)
  → R1: route_search on classify decision
  → R2: retighten loop (max_iter=2)
  → R3: execute_subqueries
  → R4: assess + refine loop (max_iter=5)
  → R5: synthesize
  → R6: continuation to parent
```

Operator audit: rule fire log + KV state on the loop entity. Restart
recovery: the rule engine's existing bootstrap-and-replay mechanism
re-fires from current state. No new primitives needed.

**Verdict**: Clean. Tier 0.

#### (b) +BoundedDispatcher

Identical to (a). The chain is sequential per topic; there's nothing
to dispatch in parallel at the chain level. BoundedDispatcher is
irrelevant here. If a future variant fans out subtopic investigators,
that's a sub-pattern of P2, not a change to P1.

**Verdict**: No delta. Tier 0.

#### (c) +Workflow primitives

If `pkg/workflow.State` were the lifecycle anchor instead of the loop
entity in `AGENT_LOOPS`, what changes?

- Each research_graph call creates a `State{WorkflowID: "graph-research",
  ID: <uuid>, Phase: "classify"}`
- Phase transitions use `StateManager.Transition(phase)` instead of
  stamping decision triples
- Operator lists workflows via `workflow.list({type: "graph-research"})`
- Versioning: `WorkflowDef{ID: "graph-research", Version: "v2"}` pinned
  per instance

**Cost**: Two parallel state surfaces (loop entity in AGENT_LOOPS AND
workflow state in WORKFLOW_STATE) for the same instance. Either the
rule engine has to write to both (sync risk) or one becomes the
shadow of the other (one becomes dead). This is **precisely the
semspec failure mode** — a parallel state machine that drifts from rules.

The operator-UX gains (named instance list, versioning) are real but
not load-bearing for the sequential agentic case. The graph already
carries everything an operator could query (phase as a triple, error
as a triple, started/completed as triples), and a tools.list_loops
gateway query would expose them.

**Verdict**: (c) introduces parallel-state failure mode for zero
practical gain. Tier 0 stays tier 0.

---

### Pattern 2 — semteams research-pack dynamic-N investigators

**Shape**: Decompose topic into N subtopics (N varies per call,
typically 3-7); spawn one investigator per subtopic in parallel; each
investigator runs `web_search` + `bash` + `scratchpad`; aggregate
findings into a synthesizer. Per Path X, dynamic-N → component-internal
fan-out.

#### (a) Rules + components only

Either:

1. **Sequential fan-out via coordinator-as-iterator**: coordinator
   respawns per subtopic, each phase processes one subtopic, takes
   N×T wall-clock. The pattern shipped pre-beta.82. Slow but works.
2. **Parallel via `for_each` + sibling-counter join**: beta.82's
   `for_each` over `coordinator.decision.subtopics` triple spawns N
   investigators in parallel; sibling completion counter + `length_eq`
   join fires synthesizer when count matches expected. The pattern
   shipped beta.83+beta.84 (#147, #149). Works but rule-pack-heavy.

For (2) — the parallel case — every investigator is an agentic-loop
instance, fully isolated, per-loop trajectory + governance + tool
allowlist. Audit is per-loop. Partial-success handling: investigator
that hits action_allowlist's `needs_clarification` writes a punt
triple; counter still increments; synthesizer reads triples and decides
whether N-1 evidence is sufficient.

This is honest. It works on production data (semteams gatherer chain).

**Verdict**: Tier 1 — closed by the beta.80-84 primitive additions.
The remaining sharp edge is the testing-coverage class addressed by
PR #150's `test/reference_configs_test.go` lint. Operator audit:
loop-by-loop in graph; counter triple on parent.

#### (b) +BoundedDispatcher

This is where the substrate primitive earns its name. Two distinct
fan-out shapes:

**Shape 1 — agent-loop fan-out at the rule layer** (today's path
via `for_each`):

Per-investigator audit is per-loop (good). Per-investigator governance
via `action_allowlist` on the rule action (good). Per-investigator
model-tier choice via the rule's `model` field (good). The rule
engine already does what's needed; BoundedDispatcher doesn't help at
this layer because each investigator IS already a separate loop with
its own JetStream-consumer-bounded concurrency.

**Shape 2 — component-internal fan-out** (Path X discipline):

A component that internally runs N parallel pieces of work — e.g., a
hypothetical `parallel_research` component that batches N URL fetches
internally rather than spawning N agent loops. Here BoundedDispatcher
is exactly the right primitive: bounded concurrency, KV-twofer-aware
completion signaling, integrates with the component framework. Per-
work-item audit goes to KV; the component emits one aggregated result.

The shape-1-vs-shape-2 choice is an application concern, not a
framework concern. The framework's job is to make BOTH cleanly
expressible. Shape 1 works today (rules + agentic-loop's existing
JetStream-consumer concurrency). Shape 2 works today via raw
`pkg/worker.Pool`; BoundedDispatcher names it.

**Verdict**: Tier 2. BoundedDispatcher promotion makes shape 2
idiomatic; rule primitives close shape 1.

#### (c) +Workflow primitives

If each investigator were a `pkg/workflow.State` instance instead of
an agentic-loop entity, the gain would be operator-listable
investigator instances. But the loop entity in `AGENT_LOOPS`
**already provides this** — `read_loop_result(loop_id)` and
loop-listing queries already exist as graph operations.

The workflow-primitive framing adds: per-instance lifecycle hooks
(`OnPhaseTransition`, `OnComplete`). The hook framework would be new
LOC the framework manages. None of the production sketches we've
seen demand hooks that the rule engine can't already provide.

**Verdict**: (c) duplicates AGENT_LOOPS lifecycle, no clear win.
Tier 2 stays tier 2.

---

### Pattern 3 — Drone survey mission (robotic, long-running)

**Shape**: Operator triggers survey of polygon P with quality Q. Drone
plans waypoints, flies them, captures sensor data per waypoint, lands.
Hours-long. Weather/battery checks may abort mission early
(safe-land at current waypoint, mark mission paused). Resumable after
reboot. Operator dashboard shows live mission state, current waypoint,
battery level, ETA. Mission definitions versioned (v1: lawnmower
pattern, v2: adaptive spiral).

Zero LLM dependency. Pure workflow semantics.

#### (a) Rules + components only

Components: `mission-planner`, `waypoint-executor`, `sensor-collector`,
`weather-monitor`, `battery-monitor`, `mission-recorder`.

State on a `mission` entity in a `MISSIONS` KV bucket (new bucket).
The entity carries: id, status (planned/in_progress/paused/aborted/
completed), polygon, quality, waypoint_list (object-store ref), current_waypoint_index,
battery_level_telemetry, last_telemetry_at, version.

Rules:

- `R1`: on mission.created → mission-planner (plan waypoints) → mission.planned
- `R2`: on mission.planned → waypoint-executor (fly to current_waypoint) → mission.waypoint_complete
- `R3`: on mission.waypoint_complete + more_waypoints → R2 (next waypoint), via `update_triple` action
- `R4`: on mission.waypoint_complete + no_more_waypoints → mission.completed
- `R5`: on battery.low OR weather.unsafe → mission.abort → safe-land component
- `R6`: on mission.abort + safe-land complete → mission.paused

Restart recovery: rule engine bootstrap-and-replay re-fires from
current state (entity in MISSIONS bucket). Mission resumes from
`current_waypoint_index`. **This works today** if the rule engine
can express "next waypoint" via the loop primitives we already
have (a triple-incrementing pattern via `update_triple`).

Operator dashboard: dedicated `mission-dashboard` GraphQL/MCP query
component reads MISSIONS bucket + writes recent telemetry to a
WebSocket port. **The framework already supports this** —
GraphQL gateway, MCP gateway, WebSocket output all ship.

Versioning: mission entity carries a `version` field; mission-planner
config branches on it. App-level concern.

What's missing for (a)? Honestly: not much. The rule engine has
`update_triple` (since beta.83 with subject override + array operators).
The agentic primitives we shipped for fan-out (`.length`, condition
substitution) compose for "all waypoints completed" checks.

What feels missing: the **labeling** of "this is a workflow." The
mission entity in MISSIONS is the workflow instance, but nothing in
the framework calls it that. Operators learn the pattern by reading
the rule pack.

**Verdict**: Tier 1. One mission-specific KV bucket + the existing
rule primitives is enough. The "workflow primitive" gap here is
**purely operator-UX**: a generic "list missions, kill mission X,
show mission Y's history" surface that doesn't have to be
mission-specific would be valuable.

#### (b) +BoundedDispatcher

Single-mission case: no fan-out, no parallel work, no need. A
fleet-of-drones case (10 missions concurrently) is identical to
single-mission × 10; rule engine handles per-mission isolation.

BoundedDispatcher would be relevant for a `multi-drone-coordinator`
component that internally fans out to N drones, but that's a
specific application-level component.

**Verdict**: No delta from (a). Tier 1.

#### (c) +Workflow primitives

This is where (c) shines IF the framing is right.

`pkg/workflow.State` instance per mission:

```go
state := &workflow.State{
    ID:         missionUUID,
    WorkflowID: "drone-survey-v2",
    Phase:      "planning", // → "flying" → "completed"/"aborted"/"paused"
    Context:    map[string]any{"polygon": ..., "current_waypoint": 0, ...},
}
```

Operator API (new framework surface):

```
GET /workflows?type=drone-survey-v2&status=in_progress
GET /workflows/{id}
POST /workflows/{id}/abort
GET /workflows/{id}/history
```

Rule actions (new):

```json
{"type": "workflow_create", "workflow_id": "drone-survey-v2", "context": {...}}
{"type": "workflow_transition", "instance_id": "...", "phase": "flying"}
{"type": "workflow_complete", "instance_id": "..."}
```

Rule conditions (new):

```json
{"field": "$workflow.state.phase", "op": "eq", "value": "flying"}
```

This is genuinely valuable for operator UX. Compensation hooks (on
abort: safe-land) can be declared per-phase instead of as separate
rules. Versioning is explicit (`WorkflowDef` registry).

**BUT** — every piece of this is already expressible with the rule
engine's existing primitives + a thin gateway component over the
mission entity. The workflow-primitive framing **doesn't add capability**;
it adds **UI/UX legibility**.

The question is: is operator-UX legibility a framework concern?

**Argument for**: Yes — when 3+ consumer classes (robotic, agentic,
event-driven) each build a different convention for "list my running
instances," the framework should provide one. Otherwise each
operator dashboard reinvents queries against per-app bucket conventions.

**Argument against**: No — operator dashboards are app concerns.
SemConnect's OGC CS API dashboard is different from a drone fleet
ops dashboard is different from semteams's research-pack chain
viewer. A framework-generic "workflow list" surface satisfies none
of them well.

**Verdict on (c) for P3**: Genuine appeal, but the gain is UX
legibility, not capability. Tier 2 (substrate-level) if we ship
gateway components that read the existing AGENT_LOOPS / MISSIONS /
app-specific buckets and present a unified surface. Tier 3 only if
we genuinely believe app dashboards should converge on a framework-
generic API.

This pattern is the one where (c) is most tempting, and where the
temptation needs the most scrutiny. **The honest classification: Tier
2 with operator-UX caveat**. The substrate is there (pkg/workflow.State
fields); the wiring is missing; whether to wire it should follow
evidence not anticipation.

---

### Pattern 4 — Manufacturing batch run (robotic, hybrid)

**Shape**: Batch of widgets enters facility. Each widget visits N
stations in order (cut → drill → polish → inspect → pack). Some
stations process widgets in parallel (8 cutters, 4 drills, 2 polishers,
1 inspector). Inspection failure triggers rework subflow (back to
drill, re-inspect). Days-long batch lifecycle. Process versions (v1:
metal alloy A, v2: composite). Operator real-time view of station
status + widget locations.

#### (a) Rules + components only

State distribution:

- Batch entity in `BATCHES` KV: id, status, version, widget_ids
- Per-widget entity in `WIDGETS` KV: id, batch_id, current_station,
  station_history, status, rework_count
- Per-station entity in `STATIONS` KV: id, current_widget_id,
  state (idle/processing/blocked), throughput

Rules:

- Widget → next station: `R_advance` matches widget completion at
  station N, fires station N+1 input
- Station load balancing: `R_assign` matches widget at station-N input
  + station-N-instance idle, fires assign
- Inspection failure: `R_rework` matches inspect.status=fail, fires
  drill assignment + increment rework_count

Bounded concurrency at the station level: each station is a JetStream
consumer with `MaxAckPending=N` (one cutter = ack-bounded to 1; 8
cutters share a queue group with 8 consumers, framework-native).

**This works today** with the rule engine. The complexity is in the
state model, not in the orchestration runtime.

**Verdict**: Tier 0 for orchestration. The work is application-level
modeling (entities, station configs, rule pack).

#### (b) +BoundedDispatcher

If a station's worker pool is implemented as a component that internally
manages parallel widget processing (instead of N JetStream consumers
on a queue group), BoundedDispatcher is the right primitive. The
component receives a widget, hands to the pool, emits completion event.

This is a legitimate alternative to JetStream-consumer-bounded-concurrency.
Either works. BoundedDispatcher gives the component more control
(per-widget cancellation, dynamic pool sizing, per-pool metrics).

**Verdict**: Tier 2. BoundedDispatcher names a real choice.

#### (c) +Workflow primitives

Per-widget `pkg/workflow.State` instance:

```go
state := &workflow.State{
    ID:         widgetUUID,
    WorkflowID: "batch-process-v2",
    Phase:      "drilling",  // station-by-station phases
    Context:    map[string]any{"batch_id": ..., "rework_count": ..., ...},
}
```

Per-batch `pkg/workflow.State` with `Context["widget_ids"] = [...]`.

Operator views N batches × M widgets × K stations. The `workflow.list`
API would return thousands of instances. **A framework-generic API
that's not domain-aware doesn't help here** — operators need
station-utilization views, widget-throughput views, batch-completion-
ETA views. None of those fall out of a generic workflow list.

The compensation hook framing (on rework: re-route to drill) is
nicer to express as a workflow-aware "on phase=rework_needed →
transition to drilling, increment rework_count" than as two rules
(R_inspect_fail, R_after_rework_route). One line of workflow config
vs. two rules. Modest win.

**Verdict**: Tier 2 + operator-UX caveat (same as P3). The workflow-
primitive framing adds modest expressivity, real operator-UX cost
(framework now owns workflow-list semantics, must maintain across
all consumer dashboards).

---

### Pattern 5 — semconnect API request lifecycle

**Shape**: HTTP request arrives. validation → processing → response.
Error compensation (rollback if mid-processing failure). No LLM. Not
long-running (seconds to minutes per request). Per-request observability
matters.

#### (a) Rules + components only

Per-request entity in `API_REQUESTS` KV (or on the request span). Rules
fire validator → processor → responder. Compensation via a
`compensation_required` triple + R_compensate rule.

**Trivially works**.

**Verdict**: Tier 0.

#### (b) +BoundedDispatcher

If the processor has internal parallelism (e.g., a fan-out to N
downstream services), BoundedDispatcher applies. Otherwise no.

**Verdict**: Tier 0 in the simple case; Tier 2 if processing is
inherently parallel.

#### (c) +Workflow primitives

Per-request workflow instance. The whole `pkg/workflow.State` surface
attaches to a request that lives for ~500ms. The `Iteration`,
`MaxIter`, `Phase` fields all encode lifecycle that's already
captured by the existing HTTP request span.

**Workflow primitives over-engineer this case.** A request span +
existing observability (Prometheus + slog + OTEL) is exactly the right
fit. Adding `workflow.State` per request adds a KV write per request
on the hot path for zero gain.

**Verdict**: Workflow primitives must NOT be the default. If they ship,
they ship as opt-in. Tier 0 forever for this pattern.

This is the canary that says outcome B over-engineers if applied
universally. Outcome B has to ship with explicit "use this only for
long-running named instances" framing, or it bloats short-lived paths.

---

### Pattern 6 — semspec scenario-orchestrator (hybrid, prior art)

**Shape**: ~600 LOC component. Receives orchestration trigger.
Reconciles completed requirements from `EXECUTION_STATES` (KV scan).
Applies DAG gating (`filterReadyRequirements`: not-yet-complete AND
all deps complete). Dispatches ready requirements with bounded
concurrency (`sem := make(chan struct{}, MaxConcurrent)`). Listens to
`EXECUTION_STATES` KV watch for completions; re-fires via plan-manager.

#### (a) Rules + components only

To replicate without BoundedDispatcher: every requirement is a
separate JetStream consumer with `MaxAckPending=1`; the queue group
provides bounded concurrency. DAG gating: each requirement rule checks
`array_contains(completed_deps, every_required_dep)` (composable
from existing primitives once `.triples` ships per #151).

This works **except** for the DAG-edge-condition expressiveness gap
the user is currently sitting on. The semspec orchestrator does this
in code (`filterReadyRequirements`); a pure-rules version needs
`.triples` (plural substitution) or equivalent.

**Verdict**: Tier 1 — closed by one more rule primitive (`.triples`).

#### (b) +BoundedDispatcher

This is the case BoundedDispatcher was designed for. The 600 LOC
becomes:

```go
type Component struct {
    dispatcher *worker.Pool[*Requirement]  // bounded by MaxConcurrent
    // ... trigger subscriber, KV watcher
}

func (c *Component) onTrigger(t Trigger) error {
    ready := filterReadyRequirements(t.Reqs, c.completedReqIDs())
    for _, req := range ready {
        c.dispatcher.Submit(req)  // bounded by pool size; returns ErrQueueFull on overflow
    }
}

func (c *Component) processRequirement(ctx context.Context, req *Requirement) error {
    // existing triggerRequirementExecution logic
}
```

The KV completion watcher is unchanged. The `dispatchRequirements`
goroutine pool + semaphore + WaitGroup becomes pool.Submit. ~150 LOC
shrinks to ~30 LOC. The orchestration semantics stay identical;
the code becomes idiomatic framework usage.

Additionally, if `.triples` or equivalent enumeration primitive
ships, the `filterReadyRequirements` function could be expressed
declaratively in the rule (matching on `$entity.triple.deps.completed.length
== $entity.triple.deps.required.length`). Then the orchestrator
component thins further — it just dispatches what rules tell it to,
no DAG logic of its own.

**Verdict**: Tier 2. BoundedDispatcher + `.triples` (or equivalent)
replaces ~400 LOC of the orchestrator. The remaining ~200 LOC is
domain logic (prereq context building, completion event synthesis).

#### (c) +Workflow primitives

Each requirement is a `pkg/workflow.State` instance. Plan-level
workflow instance with `Context["requirement_ids"] = [...]`.

This is what semspec **almost** did with its retired
`workflow/reactive/` (7,264 LOC) — and what `scenario-orchestrator`
intentionally avoided. The lesson there is direct: framework-managed
workflow state that drifts from rule semantics is the failure mode.
If we ship workflow primitives and semspec adopts them, we have to
keep them in lockstep with the rule engine forever, AND with whatever
operator API surfaces over them.

The win from (c) here is real (operator-listable plan instances) but
the cost is the integration discipline burden, which is precisely
what bit semspec.

**Verdict**: Tier 2 with operator-UX caveat. The win is named-instance
operator UX; the cost is framework-level state-semantics maintenance
across every consumer class.

---

## Evidence matrix

| Pattern | Variant (a) | Variant (b) | Variant (c) | Best | Tier | Tier-3 cluster? |
|---|---|---|---|---|---|---|
| P1 ADR-045 graph-research | Clean | No delta | Parallel-state risk | (a) | 0 | No |
| P2 dynamic-N investigators | Works | Names component-internal idiom | Duplicates AGENT_LOOPS | (b) | 2 | No |
| P3 drone survey mission | Works (Tier 1 with mission bucket) | No delta | Adds UX-legibility, no capability | (b)+optional UX layer | 2 (+UX) | Yes (UX-shaped) |
| P4 manufacturing batch | Works | Names station internal-pool | Modest hook expressivity | (b) | 2 (+UX) | Yes (UX-shaped) |
| P5 semconnect API request | Trivial | Trivial | Over-engineers | (a) | 0 | No (anti-cluster) |
| P6 semspec scenario-orchestrator | Tier 1 with `.triples` | -400 LOC of dispatch code | Recreates retired semspec shape | (b) | 2 | Yes (UX-shaped) |

### Cross-cutting observations

1. **Five of six patterns cleanly land in tier 0-2.** BoundedDispatcher
   names the substrate primitive that closes most production fan-out
   demand. The rule-engine primitives shipped beta.80-84 (+`.triples`
   or equivalent if #151 is taken as the natural sibling) close the
   declarative-gating demand. This is **outcome A's core claim**, and
   the sketches support it.

2. **Tier-3 demand is operator-UX-shaped, not capability-shaped.** P3,
   P4, P6 each have a real "operator wants to list/introspect/version
   named instances by type" demand. None of those require a new
   runtime — they require API surface over existing state. The
   framework already has the state primitives (`pkg/workflow.State`,
   AGENT_LOOPS, MISSIONS-style per-domain buckets); it lacks the
   gateway components + naming convention to present them uniformly.

3. **The pattern that breaks workflow-primitives is P5.** A workflow
   runtime that's the default would force short-lived paths to pay
   for long-lived semantics. Outcome B has to ship as opt-in to avoid
   this — and the moment workflow primitives are opt-in for some
   patterns and not others, the framework has the same "two kinds of
   workflow" split it currently has between AGENT_LOOPS-style and
   would-be WORKFLOW_STATE-style. Same drift risk as semspec, just on
   a different axis.

4. **`pkg/workflow` is a chekhov's gun.** A primitive set sitting in
   the codebase for an unspecified consumer for an unspecified
   timeframe is design rot. Either it gets a consumer in a known
   timeframe, or it goes. The exercise must produce a wire-it-or-cut-
   it call.

5. **The five-tag-pile's primitive additions DO compose for the
   sketches.** `for_each`, `.length`, condition.Value substitution,
   Subject override, array operators, tool_choice — every one shows
   up in at least two sketches as a load-bearing piece. The
   feedback memo's call to reframe them as "complete the rule
   engine's multi-valued primitive set" rather than "Phase 1 patches"
   is supported by the sketches. They're not patches; they're the
   engine's primitive set settling.

6. **The semspec trap recurs in (c) for every pattern.** Every (c)
   sketch above hit some variant of "now the framework has two
   parallel state surfaces (workflow.State + the rules/entities-side
   state)." Even when (c) adds value (P3 operator UX, P6 named
   instances), the cost is the dual-surface maintenance burden — and
   semspec's 7,264-LOC cautionary tale grew from exactly that.

7. **BoundedDispatcher is uncontroversial.** It exists today as
   `pkg/worker.Pool` (one consumer). Promoting it to first-class
   substrate primitive doesn't introduce new failure modes; it names
   what's already idiomatic and unblocks the semspec scenario-
   orchestrator refactor. The naming itself is the work.

## Recommendation: Outcome C+

> Outcome C from the proposal, **with the wire-it-or-cut-it call on
> `pkg/workflow` made explicit and bound to a knowable timeframe**.

### What ships now (one tag)

1. **Promote `pkg/worker.Pool` to first-class substrate as
   `pkg/dispatch.BoundedDispatcher`** (or keep the `pkg/worker` name
   with a re-export + documentation, whichever feels less disruptive).
   Add the KV-twofer-aware completion wrapper so components composing
   it can declaratively react to per-work-item completions. Document
   it in `docs/concepts/14-orchestration-layers.md` as the substrate
   primitive for components that do internal parallel work.
   **~150-250 LOC** including the wrapper, tests, and docs.

2. **Decide `.triples` (or equivalent enumeration primitive) NOW**, in
   the same tag — but framed as **"the last primitive in the rule
   engine's multi-valued set"**, not as a #151 patch. Choose whichever
   form (substitution suffix vs `read_loop_children` tool) cleans the
   ADR-046 Phase 1 join story without re-opening the testing-coverage
   class. Lean toward `.triples` because it mirrors `.length`'s
   substitution shape and the existing tests can extend without
   rewrites.

3. **Sunset clock on `pkg/workflow`**: add a deprecation notice at
   the top of `pkg/workflow/doc.go` and a triage memo: if no
   first-party consumer has wired it through (i.e., a rule action +
   gateway query backed by it) by **2026-08-24** (3 months), it
   gets deleted along with `processor/rule/workflow_trigger_payload.go`
   and the dead `executeTriggerWorkflow` action.

4. **Sunset clock + decision document for ADR-046 Phase 2 / GH #151**.
   #151's filed proposal is superseded by the `.triples` decision in
   step 2. Close it with a pointer to the bundle. The Phase 2
   `fan_out_gated` framing is reframed as "BoundedDispatcher +
   `.triples` already give consumers everything they need for
   declarative gated dispatch; if a true gated-DAG primitive
   eventually surfaces, it'll be component-shaped (scenario-
   orchestrator pattern with BoundedDispatcher inside) rather than
   rule-action-shaped."

5. **Updates to `CLAUDE.md` and `docs/concepts/14-orchestration-layers.md`**
   reframing the orchestration discipline:

   > *Rules sequence. Components parallelize. Components compose
   > `BoundedDispatcher` for bounded-concurrency parallel work.
   > Higher-order workflow primitives (named instances, lifecycle
   > tracking, versioning, operator-listable workflows) are NOT
   > framework concerns at this time — the substrate is sufficient
   > for known consumer classes. If a workflow-primitives demand
   > emerges from ≥2 consumer classes in production over the next
   > quarter, a second design exercise re-opens the question.*

6. **Tag name**: `v1.0.0-beta.85` (or whatever the next slot is)
   titled what it IS: **"BoundedDispatcher + `.triples` enumeration
   primitive — completing the rule engine's multi-valued primitive
   set + naming the substrate concurrency primitive"**.
   ~400-600 LOC bundle, single tag, one cohesive narrative for
   semteams + semspec + future consumers.

### What defers

- **`pkg/workflow` wire-through OR removal**: decision point at
  2026-08-24. If a consumer surfaces a real need in that window, the
  primitives get rule integration + gateway components + an ADR-047
  treatment. If not, the package goes.
- **Workflow-primitive-as-first-class outcome (Outcome B)**: gated on
  ≥2 consumer-class production evidence. Robotic consumers don't
  exist yet; semteams is the one agentic consumer; semconnect is
  starting up. A second design exercise opens when two of these
  three (or a new entrant) hit the same workflow-UX-shaped demand
  that they can't solve with rules + components + BoundedDispatcher.

### Why outcome C+ over A or B

**Why not pure A** ("rules + BoundedDispatcher is sufficient,
permanent")? Because closing the door on workflow primitives
permanently overshoots what the evidence supports. The tier-3 demand
in P3/P4/P6 IS real — operator UX legibility for long-lived
named-instance workflows is a coherent ask. It's just that the
demand is **anticipatory**, not yet production-evidenced. Outcome A
locks in a "no" before that evidence has a chance to surface.

**Why not pure B** ("workflow primitives ship as deliberate
framework layer")? Three reasons:

1. **P5 (semconnect) breaks under B-as-default.** Short-lived API
   requests don't need workflow lifecycle. B has to ship as opt-in,
   which means the framework has two-shape state semantics from day
   one — the same drift risk as semspec.
2. **The workflow-UX demand is anticipatory.** Robotic consumers
   don't exist in this codebase yet; the "drone survey mission" is
   a sketch, not a use case. Building a workflow layer for
   sketches violates the YAGNI discipline.
3. **`pkg/workflow` already exists and isn't used.** Shipping more
   workflow primitive surface when the existing surface is dead
   code says nothing about whether the existing surface is the
   right shape. Wire the existing surface first; learn from the
   adoption (or absence of it); design from evidence.

**Why C+ specifically**: C ships the unambiguous-win substrate piece
(BoundedDispatcher) and the unambiguous-completion piece (`.triples`)
NOW, in a deliberately-named bundle, so consumers are unblocked and
the five-tag-pile pattern stops. The "C+" extension binds the
dormant `pkg/workflow` surface to a knowable decision date, so the
chekhov's gun problem resolves either way. And the workflow-
primitives question stays open without freezing forever — when
evidence accrues, a second exercise runs and either ships outcome
B or sunsets the question permanently.

This is the lowest-regret path. The bundle is small enough to ship
in one tag, names what it IS, and resolves the immediate consumer
pressure (semteams unblocked, semspec has a refactor target, #151
closed cleanly).

## What changes per the recommendation

| Item | Change |
|---|---|
| `CLAUDE.md` | Update "Orchestration Boundaries" section with the `BoundedDispatcher` reframe and the explicit "workflow primitives are not a framework concern at this time" statement. Add the `pkg/workflow` sunset date. |
| `docs/concepts/14-orchestration-layers.md` | Add BoundedDispatcher to the pattern catalog. Add "When to use a component-internal pool vs JetStream-consumer concurrency" decision. Reaffirm "no workflow layer" with the explicit out-of-scope framing. |
| `docs/concepts/25-phased-agentic-chains.md` | Update the 2026-05-24 note to point at this decision; remove the "if workflow primitives ship" hypothetical from the lede. |
| `docs/proposals/workflow-primitives-design-exercise.md` | Mark "Resolved by `workflow-primitives-decision.md`." Status: closed. |
| `docs/adr/046-parallel-fan-out-and-gated-dag-dispatch.md` | Amend Phase 2: superseded by BoundedDispatcher + `.triples`. Phase 1 shipped as-is; Phase 2 closed as not-needed-in-current-form. |
| ADR-047 | NOT drafted (outcome was not B). |
| GH #151 | Close with pointer to the bundle PR. Implementation goes in the bundle, not against #151's filed shape. |
| `pkg/workflow` | Sunset notice in `doc.go`. Decision date 2026-08-24. |
| `processor/rule/workflow_trigger_payload.go` + `executeTriggerWorkflow` | Sunset notice in package comment. Same decision date. |
| `semspec/processor/scenario-orchestrator/` | Filed as refactor candidate: lift to BoundedDispatcher in a follow-up PR (semspec-side work). |
| `project_rules_engine_design_review` memory | Update with the resolution; close the gate. |
| `project_workflow_primitives_decision` memory | NEW — captures the outcome + reasoning + sunset-date follow-ups. |

## BoundedDispatcher spec (the one new primitive)

Lives in `pkg/dispatch/` (new package, OR extends `pkg/worker/` with a
re-export). Building on the existing `pkg/worker.Pool[T]`:

```go
package dispatch

import (
    "context"
    "github.com/c360studio/semstreams/pkg/worker"
    "github.com/c360studio/semstreams/natsclient"
)

// BoundedDispatcher is a framework-provided primitive for bounded-concurrency
// parallel work that integrates with KV-twofer completion semantics.
//
// Use this when a component does internal parallel work over a known list
// (decomposed subtasks, station-internal widget processing, scenario-
// requirement dispatch). For at-the-rule-layer fan-out (one rule, N
// downstream agents), use the rule engine's for_each primitive instead.
type BoundedDispatcher[W any] struct {
    pool        *worker.Pool[W]
    completion  *CompletionWatcher  // optional KV-twofer integration
    logger      *slog.Logger
    metrics     *Metrics
}

type Config[W any] struct {
    MaxConcurrent int              // worker pool size
    QueueSize     int              // bounded queue depth
    Process       func(context.Context, W) error
    
    // Optional: KV-twofer completion integration. If set, dispatcher
    // watches CompletionKVBucket for matching key writes and fires
    // OnComplete when a work item's completion key appears.
    CompletionKVBucket string
    CompletionKeyForWorkItem func(W) string
    OnComplete func(context.Context, W) error
}

func New[W any](ctx context.Context, cfg Config[W], deps Deps) (*BoundedDispatcher[W], error) {
    // wraps pkg/worker.Pool[W], adds optional completion watcher
}

func (d *BoundedDispatcher[W]) Submit(work W) error {
    return d.pool.Submit(work)
}

func (d *BoundedDispatcher[W]) Stats() Stats { ... }
```

**Properties** (reaffirming what `pkg/worker` already does + what the
completion wrapper adds):

- NOT a workflow engine (no DAG semantics, no branching, no lifecycle)
- NOT a rule-engine extension (rules don't gain new fan-out primitives)
- IS a substrate primitive components compose into their internal
  fan-out logic
- IS KV-twofer-aware (optional CompletionWatcher closes the
  read-completion-from-KV pattern semspec's scenario-orchestrator
  spells out manually today)
- Generic over work type W
- Bounded queue with backpressure (`ErrQueueFull` on overflow)
- Statistics + optional Prometheus metrics (inherits from `pkg/worker`)

**Migration path for `pkg/worker` consumers**: graph-index keeps its
existing `pkg/worker.Pool` usage (no completion needed). New uses
prefer `pkg/dispatch.BoundedDispatcher` (or whatever the final naming
lands on). semspec scenario-orchestrator becomes the first downstream
test case.

## Sister-project guidance

### For semteams

1. **Pin to beta.85 (the bundle tag) when it ships.** Research-pack
   gets `.triples` for any remaining join-shape need; nothing else
   in the framework needs to change.
2. **Don't wait on workflow primitives.** They're not coming in this
   tag, possibly not at all. Design research-pack around rules +
   components + per-loop AGENT_LOOPS state.
3. **If research-pack grows operator-listable instance demand**
   (e.g., "show me all running research chains"), bring it as
   evidence to the workflow-primitives second exercise. Don't
   build per-research-pack-app-side state machines.

### For semspec

1. **scenario-orchestrator refactor is now scoped.** Replace the
   ~150 LOC dispatch goroutine pool + semaphore + WaitGroup with
   `pkg/dispatch.BoundedDispatcher`. The DAG-gating logic
   (`filterReadyRequirements`) stays in code OR moves to declarative
   rule expression now that `.triples` ships. Either is acceptable;
   the engineering call is local to semspec.
2. **Use semspec's refactor as the first BoundedDispatcher consumer
   downstream.** Surface any gaps in the primitive's shape; iterate
   on the framework side if needed.

### For semconnect

Unaffected. Continue building OGC CS API endpoints as before. If
request-lifecycle observability matters beyond what HTTP spans
provide, that's app-level instrumentation, not framework workflow
primitives.

## Open questions resolved

The four open questions from ADR-046 Phase 2's "deferred" section:

1. **Where does the DAG live?** — In the entity that owns the work
   items, as triples. `array_contains` + `.triples` + `length_eq`
   compose for "all my deps complete" without a separate DAG
   primitive.
2. **Completion source-of-truth bucket and key shape?** — Existing
   per-domain buckets (AGENT_LOOPS, MISSIONS-style). No new
   framework bucket.
3. **Failure semantics per node?** — Per-rule policy via conditions
   on outcome. No orchestrator-level setting.
4. **Cheap-model-substrate integration?** — Rules filter on
   `coordinator.decision.synthetic` triple. Composable, no
   special case.

All four collapse to "existing rule expressivity plus the `.triples`
primitive in this bundle." No `fan_out_gated` action ships.

The original four-question list from `project_rules_engine_design_review`:

1. **Is the framework discipline right?** — Yes, with refinement:
   "no separate workflow engine" stands; the implicit
   "rules-and-components-cover-everything" claim is refined to
   "rules + components + BoundedDispatcher cover everything we
   currently need; workflow-UX primitives are a future open question
   gated on cross-consumer evidence."
2. **What's the complete primitive set the rules engine needs?** —
   `for_each` (shipped), `.length` (shipped), Subject override
   (shipped), array operators (shipped), `tool_choice` (shipped),
   condition.Value substitution (shipped), `.triples` (in this
   bundle). After this bundle, the primitive set is "complete enough"
   for known consumer patterns — no more reactive primitive additions
   without a deliberate design exercise.
3. **Are some "missing primitives" actually composable from
   existing ones?** — Yes (DAG-edge condition, multi-entity
   aggregation in many cases). Don't add primitives where
   composition suffices.
4. **Should `.triples` ship as part of completing the primitive set,
   OR ship now as #151's fix?** — As part of the bundle. NOT as a
   #151 patch. This is the discipline shift from the feedback memo.

The two big-reframe questions (5 — primitive-complete tag size; 6 —
should substitution be a typed expression language) are answered:

5. **The right "primitive complete" tag size is THIS BUNDLE.**
   ~400-600 LOC, one tag, deliberately named. After this, no more
   reactive primitive additions to the rules engine without an ADR
   amendment.
6. **Typed substitution expression language** is deferred. The
   string-replace surface is workable for known use cases. If a
   future ADR proposes typed expressions, it gets its own exercise.
   Not in this bundle.

## What NOT to do next

- Don't ship `.triples` as a separate "fix #151" tag. Bundle it with
  BoundedDispatcher per this decision.
- Don't ship workflow primitives. Wait for cross-consumer evidence.
- Don't extend `pkg/workflow` while it's still unused. Either wire it
  through OR let the sunset clock run; don't grow more dead surface.
- Don't write workflow-engine framing back into CLAUDE.md. The
  reframe is "no workflow layer at this time, substrate primitive
  named," not "workflow engine coming soon."

## Output produced by this session

This document, plus follow-up work:

1. ✅ Decision document (this file)
2. Updates to `docs/concepts/25-phased-agentic-chains.md` — remove
   "if workflow primitives ship" hypothetical
3. Updates to `docs/proposals/workflow-primitives-design-exercise.md`
   — mark resolved
4. Updates to `CLAUDE.md` — orchestration section reframe
5. Memory updates:
   - `project_rules_engine_design_review` — close gate, point at decision
   - `project_workflow_primitives_decision` (NEW) — capture outcome
6. (Code, to be done in the bundle tag, not in this session):
   - `pkg/dispatch/` (new) with `BoundedDispatcher`
   - `.triples` substitution primitive on `processor/rule/`
   - Sunset notice on `pkg/workflow`
   - Sunset notice on `processor/rule/workflow_trigger_payload.go` +
     `executeTriggerWorkflow`

## References

- [`workflow-primitives-design-exercise.md`](workflow-primitives-design-exercise.md) — the proposal this resolves
- [`docs/concepts/14-orchestration-layers.md`](../concepts/14-orchestration-layers.md)
- [`docs/concepts/25-phased-agentic-chains.md`](../concepts/25-phased-agentic-chains.md)
- [`docs/adr/028-orchestration-architecture.md`](../adr/028-orchestration-architecture.md)
- [`docs/adr/046-parallel-fan-out-and-gated-dag-dispatch.md`](../adr/046-parallel-fan-out-and-gated-dag-dispatch.md)
- `pkg/workflow/state.go`, `pkg/worker/pool.go`
- `processor/rule/actions.go` (`executeTriggerWorkflow`)
- `semspec/processor/scenario-orchestrator/component.go`
