# Lifecycle Harness — semconnect Sketch

**Status**: Research draft, 2026-05-24. Companion to
[`workflow-primitives-robotic-sketch.md`](workflow-primitives-robotic-sketch.md).
Tests the Lifecycle harness against a third consumer class
(event-driven, mostly short-lived) per user direction to validate it
doesn't over-engineer short-lived paths AND does fit long-lived
resource lifecycles.

## Two lifecycle shapes in semconnect

Critical reframe from the original P5 sketch:

The P5 sketch assumed semconnect's workflows are **per-request**
(validation → processing → response, lifetime ~500ms). That's true
for HTTP request handling — and the harness must STAY OUT of that
path. But semconnect also has **per-resource lifecycle** workflows
that span days, weeks, months: a registered System sensor moves
through calibration → active → maintenance → degraded → retired
phases over its operational lifetime.

The original P5 "workflow primitives over-engineer this case" finding
was **only half right**: it applies to per-request, NOT to
per-resource. The harness must support both shapes — opt-in for
per-request (don't force lifecycle on HTTP spans), naturally fit
for per-resource.

## Shape 1 — Per-request lifecycle (DOES NOT use harness)

```
HTTP request → validate → query/mutate graph → render response
                            (10-100ms total)
```

Implementation: `gateway/cs-api/systems_post.go::handleSystemsPost`
parses SensorML, validates, calls `graph-ingest` to write triples,
returns 201. End-to-end inside one HTTP handler. No multi-phase
lifecycle. No restart recovery concern (HTTP retries). No operator-
visible "in-progress requests" view.

**Lifecycle harness verdict for this shape**: opt-out. The HTTP
handler is a regular Go function; it does not implement Participant.
Whatever entity it creates (a System resource) MAY be a Participant,
but the request itself is not.

This is by design. The harness's value-add is for instances that
**persist across request boundaries**. A request that completes in
its own HTTP span gains nothing from the harness; forcing it in
would be net-negative LOC + operator API noise (thousands of
"completed" instances per second cluttering listings).

## Shape 2 — Per-resource lifecycle (fits the harness)

When a sensor System is POSTed to `/systems`, it's created. But the
sensor itself goes through operational lifecycle phases:

```
registered → calibrating → active ↺ maintenance ↺ degraded → retired
                ↓
            failed (calibration never passed)
```

Triples on the System entity:
- `system.lifecycle.phase = "active"`
- `system.lifecycle.registered_at`
- `system.lifecycle.last_calibration_at`
- `system.lifecycle.next_maintenance_due`
- `system.lifecycle.observation_count_24h`

Components participating:
- **registration-validator**: validates POST, writes initial state with `phase=registered`
- **calibration-orchestrator**: runs calibration on schedule + on first registration, sets `phase=calibrating` then `active`/`failed`
- **maintenance-scheduler**: cron rule, transitions `active → maintenance` when due
- **maintenance-executor**: runs maintenance procedure, transitions back to `active`
- **observation-quality-monitor**: watches observation stream, transitions `active → degraded` on threshold breach
- **retirement-handler**: handles `DELETE /systems/{id}` and operator retire commands

These are not HTTP handlers (except registration-validator which is
co-located with `POST /systems`). They're background components
that participate in the System's lifecycle, dispatched by rules.

### App-side state

```go
package csapilifecycle

type SystemState struct {
    EntityID_ string `json:"entity_id"`
    SystemID  string `json:"system_id"`     // CS API resource ID
    Workflow_ string `json:"workflow"`      // "csapi-system"
    Version   string `json:"version"`       // OGC CS API conformance class

    // Lifecycle
    Phase_      string     `json:"phase"`
    RegisteredAt time.Time `json:"registered_at"`
    UpdatedAt   time.Time `json:"updated_at"`
    RetiredAt   *time.Time `json:"retired_at,omitempty"`

    // SensorML reference
    SensorMLRef string `json:"sensorml_ref"` // ObjectStore ref to full SensorML

    // Calibration
    LastCalibrationAt *time.Time `json:"last_calibration_at,omitempty"`
    CalibrationCount  int        `json:"calibration_count"`
    LastCalibrationOK bool       `json:"last_calibration_ok"`

    // Maintenance
    NextMaintenanceDue *time.Time `json:"next_maintenance_due,omitempty"`
    MaintenanceCount   int        `json:"maintenance_count"`

    // Quality
    ObservationCount24h int     `json:"observation_count_24h"`
    QualityScore        float64 `json:"quality_score"`  // 0-1.0
    DegradedSince       *time.Time `json:"degraded_since,omitempty"`

    // Operator context
    OwnerOrgID   string `json:"owner_org_id"`
    DeployedTo   string `json:"deployed_to,omitempty"`  // physical location/deployment
}

func (s *SystemState) EntityID() string { return s.EntityID_ }
func (s *SystemState) Workflow() string { return s.Workflow_ }
func (s *SystemState) Phase() string    { return s.Phase_ }
func (s *SystemState) KVBucket() string { return "CSAPI_SYSTEMS" }
func (s *SystemState) KVKey() string    { return "system." + s.SystemID }

func (s *SystemState) IsTerminal() bool {
    return s.Phase_ == "retired" || s.Phase_ == "failed"
}
```

Per-tenant nuance: the `OwnerOrgID` field tracks which customer
owns the sensor. Multi-tenant deployments use this for visibility
filtering. The harness's `Manager.List` could accept a filter; the
operator API can scope per-tenant naturally.

### Components

`registration-validator` (the only HTTP-adjacent one):

```go
// Called from gateway/cs-api/systems_post.go after SensorML validation
// passes and the System entity has been ingested into the graph.
func (c *Component) onSystemRegistered(ctx context.Context, systemID, sensorMLRef, orgID string) error {
    entityID := fmt.Sprintf("...system.%s", systemID)
    initialState := &SystemState{
        EntityID_:    entityID,
        SystemID:     systemID,
        Workflow_:    "csapi-system",
        Phase_:       "registered",
        RegisteredAt: time.Now().UTC(),
        UpdatedAt:    time.Now().UTC(),
        SensorMLRef:  sensorMLRef,
        OwnerOrgID:   orgID,
    }
    return c.lifecycle.Create(ctx, initialState)
}
```

So the HTTP handler is short-lived (returns immediately after Create
returns). The lifecycle BEGINS when Create returns. Subsequent
phases (calibration, etc.) happen via rules + components in the
background.

`calibration-orchestrator`:

```go
func (c *Component) handleCalibrate(ctx context.Context, systemID string) error {
    participant, err := c.lifecycle.Get(ctx, systemID)
    if err != nil { return err }
    sys := participant.(*SystemState)

    // Transition to calibrating
    if err := c.lifecycle.Transition(ctx, sys.EntityID_, "calibrating"); err != nil {
        return err
    }

    // Run calibration (long-running — minutes to hours)
    ok, calibrationResult, err := c.runCalibrationProcedure(ctx, sys.SensorMLRef)
    if err != nil {
        return c.lifecycle.Fail(ctx, sys.EntityID_, err.Error())
    }

    // Update result fields + transition
    return c.lifecycle.Update(ctx, sys.EntityID_, func(p lifecycle.Participant) error {
        s := p.(*SystemState)
        now := time.Now().UTC()
        s.LastCalibrationAt = &now
        s.LastCalibrationOK = ok
        s.CalibrationCount++
        if ok {
            s.Phase_ = "active"
            s.NextMaintenanceDue = ptr(now.Add(90 * 24 * time.Hour))  // 90-day schedule
        } else {
            s.Phase_ = "failed"
            s.RetiredAt = &now
        }
        return nil
    })
}
```

`observation-quality-monitor` (cross-cutting watcher):

```go
func (c *Component) onObservationReceived(ctx context.Context, obs *Observation) error {
    // Recompute quality score from recent observations
    score := c.computeQualityScore(obs)

    // Threshold breach → transition to degraded
    if score < c.config.QualityThreshold {
        return c.lifecycle.Update(ctx, obs.SystemID, func(p lifecycle.Participant) error {
            sys := p.(*SystemState)
            if sys.Phase_ == "active" {
                sys.Phase_ = "degraded"
                now := time.Now().UTC()
                sys.DegradedSince = &now
            }
            sys.QualityScore = score
            sys.ObservationCount24h++  // approx; real-impl uses windowed counter
            return nil
        })
    }
    // Recovery — quality came back
    return c.lifecycle.Update(ctx, obs.SystemID, func(p lifecycle.Participant) error {
        sys := p.(*SystemState)
        if sys.Phase_ == "degraded" && score >= c.config.QualityRecoveryThreshold {
            sys.Phase_ = "active"
            sys.DegradedSince = nil
        }
        sys.QualityScore = score
        return nil
    })
}
```

This is the watcher pattern again. The component does NOT subscribe
to a NATS subject directly; it's called from the observations
ingestion pipeline (which is a per-observation component already).

### Rule pack (skeleton)

```json
[
  {
    "name": "kickoff_calibration_on_registration",
    "when": {
      "bucket": "CSAPI_SYSTEMS",
      "conditions": [
        {"field": "$entity.lifecycle.phase", "op": "eq", "value": "registered"}
      ]
    },
    "actions": [
      {"type": "publish", "subject": "component.calibration-orchestrator.{entity.id}"}
    ]
  },
  {
    "name": "schedule_maintenance",
    "when": {
      "bucket": "CSAPI_SYSTEMS",
      "schedule": "every 1h",
      "conditions": [
        {"field": "$entity.lifecycle.phase", "op": "eq", "value": "active"},
        {"field": "$entity.triple.next_maintenance_due", "op": "lte", "value": "$now"}
      ]
    },
    "actions": [
      {"type": "lifecycle_transition", "phase": "maintenance"},
      {"type": "publish", "subject": "component.maintenance-executor.{entity.id}"}
    ]
  },
  {
    "name": "retirement_on_command",
    "when": {
      "bucket": "CSAPI_SYSTEMS",
      "conditions": [
        {"field": "$entity.triple.operator_command", "op": "eq", "value": "retire"},
        {"field": "$entity.lifecycle.terminal", "op": "eq", "value": false}
      ]
    },
    "actions": [
      {"type": "lifecycle_transition", "phase": "retired"}
    ]
  }
]
```

A few rules, mostly short. Schedule cron handled via the
`"schedule": "every 1h"` syntax (mixing cron with KV-state matching
is the hybrid form ADR-031 + ADR-041 enable).

### Operator API (free with the harness)

```
GET /workflows/csapi-system?org_id=acme&phase=active
→ list of acme's active sensors

GET /workflows/csapi-system/{system_id}
→ full SystemState including calibration history, quality score, next maintenance

POST /workflows/csapi-system/{system_id}/calibrate
→ triggers operator-initiated recalibration

POST /workflows/csapi-system/{system_id}/retire
→ triggers retirement

GET /workflows/csapi-system/{system_id}/history
→ phase transitions over the sensor's lifetime
```

The drone-survey operator API and the CS API operator API
**share a shape**. A multi-tenant deployment with both running could
expose ONE unified dashboard that pulls from `Manager.List`.

That's the cross-product win the harness creates. semconnect-co
running drone-survey-co's deployments + their own sensor fleet get
ONE operator view, not two custom dashboards.

## Critical test: does the harness over-engineer per-request paths?

The per-request shape (POST → validate → store → respond) does NOT
implement Participant. It's a regular HTTP handler. The KV write
happens to be on a Participant-implementing entity (the System), but
the request itself isn't lifecycle-shaped.

**Verdict**: harness stays opt-in. Per-request paths are
untouched. Per-resource lifecycle gets the harness.

This is the key design property: **lifecycle participation is a
property of the ENTITY, not the COMPONENT or REQUEST**. Multiple
components can participate in one entity's lifecycle without each
one being a "workflow participant" in a heavyweight sense. The
short-lived components (HTTP handlers) can read/write the entity
without claiming participation; the long-lived components
(calibration-orchestrator, maintenance-executor) participate via
the harness's API.

## Gaps the semconnect sketch surfaced (new vs drone sketch)

### Gap 9 — Multi-tenancy / org-scoped filtering

drone-survey-co might have N customers; semconnect-as-fleet-mgmt
has obvious multi-tenancy. The harness's `Manager.List(workflow)`
needs filtering: `Manager.List(workflow, filter)`.

**Sketch**:

```go
type ListFilter struct {
    Phase     string            // empty = any
    Active    bool              // only non-terminal
    OrgID     string            // operator-tenant filter
    UpdatedAfter time.Time      // for incremental queries
    Match     map[string]any    // field equality matches
}

func (m *Manager) List(ctx context.Context, workflow string, filter ListFilter) ([]Participant, error)
```

Filter fields specific to ORG_ID etc. are app-side concerns. The
harness can support field-equality matches via reflection or struct
tags but shouldn't bake in multi-tenancy as a framework concept —
it stays opt-in through filter API.

### Gap 10 — `$now` substitution / clock primitive

`{"field": "$entity.triple.next_maintenance_due", "op": "lte",
"value": "$now"}` — the `$now` substitution isn't defined. semspec
likely needs it too for deadline checks. Either:

- Add `$now`, `$today` substitution paths in the rule engine
- Or rely on cron rules to be the temporal trigger (no clock in
  conditions; only schedules)

Lean **cron-as-clock**: the cron rule fires every 1h, then condition
checks `next_maintenance_due` against actual wallclock (in the rule
engine internally, not via substitution). Smaller addition than a
general `$now` substitution; matches the cron-rule's existing
"schedule + condition" shape.

### Gap 11 — `phase != "X"` AND `lifecycle.terminal != true` is verbose

Several rules check "not terminal AND not already in target phase."
The drone sketch had four such conditions on one rule. Repetitive.

**Resolution**: convenience substitution `$entity.lifecycle.entering`
that's true on the transition into a phase (KV revision-1 had a
different phase). Rules can match `entering` instead of guarding.

Or — and probably better — accept the verbosity for clarity.
Adding `entering` adds substitution complexity for marginal gain.
**Defer; revisit if 3+ consumers want it.**

### Gap 12 — Quality monitors and observations-stream coupling

`observation-quality-monitor` is called per observation. It reads
+ writes the System entity. For a sensor doing 100Hz observations,
that's 100 Updates/sec per sensor.

KV updates are cheap (NATS-native, single-revision-bump). But the
rule engine fires on every KV update; the quality-monitor rule
firing 100Hz per sensor × N sensors = burdensome.

**Resolution**: the quality monitor SHOULD write to a counter
predicate (windowed) on the entity, NOT trigger lifecycle
transitions on every observation. Transitions happen when the
counter hits a threshold. This is the same discipline as the rate-
limit governance pattern — **don't fire rules on every
observation**.

This is a discipline issue, not a harness issue. The harness
provides the Update API; using it well is component-author work.

### Gap 13 — Operator-writable fields scope

In the drone sketch I flagged `OperatorWritableFields()` as a
Participant interface method. The semconnect sketch shows the
nuance: operators want to write `OwnerOrgID` on transfer, but
NOT `LastCalibrationAt`. Per-field operator-writability needs
either:
- Per-field tag (`lifecycle:"operator_writable"`)
- A method returning the allowed set
- A declarative ACL via registration

Lean struct tags — least friction, type-checked, locally readable.
Validation happens in the Manager's `Update` path when the call
comes via operator API.

## Verdict on the semconnect sketch

**The harness fits both shapes cleanly**:

- **Per-request paths** stay out of the harness entirely (opt-out
  by not implementing Participant). The 15,500 LOC of CS API HTTP
  handlers untouched.
- **Per-resource lifecycle** fits the harness naturally. The
  SystemState struct is app-side; the Manager provides KV + rule +
  operator API integration.

**Cross-consumer evidence**: drone-survey-co (zero LLM, robotic) +
semconnect (event-driven, HTTP+resource-lifecycle) + semspec (LLM,
plan/requirement/task workflow) all map to the same harness shape.
Three distinct consumer classes, same Participant interface, same
Manager API. **That's outcome B' evidence.**

**Net delta from drone sketch**: 5 more gaps surfaced (multi-tenancy
filter, $now substitution, terminal-guard verbosity, observation-
storm rate-limit discipline, per-field operator-writability). Three
of five are bounded narrow additions. Two (multi-tenancy semantics,
$now-vs-cron) deserve clearer treatment in the eventual ADR.

## What's still untested

- **Cross-resource lifecycle**: a Deployment in CS API owns N
  Systems. The Deployment lifecycle includes "all sensors active"
  → "deployment-operational". This is parent/child workflow shape
  again, same as semspec's Plan owns Requirements. Same Gap 4
  resolution should apply.
- **Datastream lifecycle**: separate from System lifecycle. A
  Datastream is registered to a System but has its own active/
  retired/orphaned states. Multiple Participant types per
  workflow. The harness handles by registering each as a separate
  workflow type with separate buckets.
- **OGC conformance impact**: does the OGC test suite (run in
  `conformance/`) care about lifecycle endpoint semantics? Likely
  not — CS API doesn't specify lifecycle endpoints, that's an
  operator UX layer above the spec. But worth checking before the
  harness changes shape.

## Position before commit

Two consumer-class sketches (drone-survey + semconnect) plus
semspec's 7,900 LOC of hand-rolled prior art together give
**strong evidence for outcome B'**:

- The harness shape is genuine framework substrate, not a
  workflow engine
- Three consumer classes (robotic, event-driven, agentic) all map
  to the same interface
- Per-request short-lived paths stay opt-out (no over-engineering)
- Operator UX wins cross-product (single dashboard convention)

The scope estimate from the drone sketch (~1800-2650 LOC) holds.
The decision-doc amendment should reframe from C+ to B'.

## Recommendation refinement

C+ → **B' (constrained workflow primitive — Lifecycle harness)**:

| Piece | Status |
|---|---|
| BoundedDispatcher substrate | KEEP — still needed |
| `.triples` enumeration | KEEP — still needed |
| Sunset pkg/workflow | REVERSE — wire through as `pkg/lifecycle` |
| New: Participant interface + Manager | ADD — the harness substrate |
| New: `lifecycle_transition` rule action | ADD — explicit rule integration |
| New: `$entity.lifecycle.*` substitution | ADD — condition matching |
| New: gateway components for operator API | ADD — list/get/watch over Participants |
| CLAUDE.md reframe | ADJUST — "rules sequence, components parallelize, components compose harness for lifecycle entities; no workflow engine, harness for lifecycle is framework" |
| Workflow engine (DSL/runtime) | OUT — still NOT shipping |

Bundle size: ~1800-2650 LOC across 2-3 PRs over 2-3 weeks. ADR-047
covers the harness. ADR-048 covers BoundedDispatcher + `.triples`
(or co-located).

## Open questions before committing to ADR-047 draft

1. **Multi-tenancy semantics** — is `Manager.List(workflow, filter)`
   sufficient, or does the framework need first-class
   tenancy-aware Participant fields? Lean former; tenancy is
   app-side.
2. **`$now` substitution vs cron-only** — make the call before
   the ADR.
3. **Parent/child workflows (Gap 4 + cross-resource Deployment)**
   — separate ADR amendment, or fold into ADR-047 as optional
   `ParentEntityID()` Participant method?
4. **Per-field operator-writability mechanism** — struct tags vs
   method vs registration ACL.
5. **Cross-product dashboard** — does the framework ship the
   dashboard component, or just the API and let each product
   build its own UI? Lean ship API (gateway components), defer UI.

The semconnect sketch confirms the direction; these five are
design choices remaining for ADR-047 itself.
