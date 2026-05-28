# Lifecycle Harness Prime — Consumer Shape Sketches

**Status**: Proposed — 2026-05-28. Companion analysis to
[`lifecycle-harness-prime-design-exercise.md`](lifecycle-harness-prime-design-exercise.md).

**Purpose**: Ground the harness redesign in evidence about what
real consumers actually need a workflow instance to BE. Before
beta.85 we sketched the harness around an implicit "Definition A"
shape (id + phase + metadata). This document characterizes five
candidate consumer domains so the redesign can be informed by
what's actually demanded, not what was easy to fixture.

**Method**: For each consumer, characterize along ten axes:

1. **What it IS** — one-sentence semantic definition
2. **What it OWNS** — entities/data it coordinates
3. **State semantics** — what fields replace, what fields accumulate
4. **Operator queries** — what does the dashboard need to answer
5. **Audit needs** — who/what/when + retention
6. **Children** — does it own sub-workflows
7. **Lifetime** — how long does an instance live
8. **Write pattern** — frequency, volume
9. **Read pattern** — who reads, what slice, how often
10. **Definition tier** — A (thin id+phase), B (coordination root with subtree), C (typed computation)

## 1. Drone survey mission

| Axis | Characterization |
|---|---|
| **IS** | A coordinated remote-sensing operation: a drone executes a planned flight, captures sensor data over an area, returns home |
| **OWNS** | Drone entity ref(s); area entity ref; flight plan (waypoints, altitude, sensor schedule); per-sensor configs; capture session refs; produced artifacts (imagery, point clouds, telemetry windows); pre-flight check results; anomaly log; mission outcome |
| **State semantics** | **Replace**: phase (planning → pre-flight → flying → capturing → returning → landed → completed/aborted/failed), current waypoint, current sensor state. **Accumulate**: per-sensor capture counts, anomaly events, telemetry windows (refs only) |
| **Operator queries** | "All flying missions right now"; "current waypoint of mission M"; "captured data for mission M"; "how many missions for drone D this week"; "anomalies in last 24h across all missions" |
| **Audit** | Phase transitions with source (operator vs auto vs failure-detected); operator overrides (abort/hold/resume); anomaly events; regulatory retention (incident investigation: months to years) |
| **Children** | Capture sessions per sensor (each has own phase); pre-flight check sub-flow; landing sub-flow. Drone itself NOT a child — it's a long-lived entity referenced by the mission |
| **Lifetime** | Minutes to hours per mission; potentially many missions per day per drone |
| **Write pattern** | Phase/waypoint: frequent during flight (per-second), low-volume per write. Telemetry: VERY high-volume but lives in a separate stream, NOT in mission state. Artifacts: low-frequency, high-byte (ObjectStore) |
| **Read pattern** | Live operator dashboard: high frequency on current state. Post-mission analysis: full history replay. Cross-mission analytics: "yield per drone", "anomaly rates by area" |
| **Tier** | **B** — coordination root with owned subtree |

**Key insight**: high-volume telemetry never lives in mission state. Mission carries refs to telemetry windows + artifact storage refs. The mission entity itself is low-volume coordination state plus relationships.

## 2. semspec dev-via-spec mission

| Axis | Characterization |
|---|---|
| **IS** | A long-running development process: take a spec, decompose into work units, drive an agent chain to implement each, validate against the spec, produce code + tests + docs |
| **OWNS** | Spec ID/ref (input); target component(s); work unit children; per-work-unit state; agent loop refs (which loops drove which work); produced artifacts (code, tests, PR refs); **findings** (graph knowledge accumulated during the work); validation results; human-review hand-offs; inter-work-unit dependencies |
| **State semantics** | **Replace**: mission phase (planning → executing → validating → completed/blocked), current focus work unit, per-work-unit phase. **Accumulate**: findings (semantic knowledge), artifact refs, agent loop refs, validation results history |
| **Operator queries** | "Current state of this dev mission"; "all blocked missions"; "what spec is this mission implementing"; "findings accumulated by this mission"; "timeline of mission M + children"; "missions implementing OGC specs" |
| **Audit** | Phase transitions with source (rule vs agent vs human); agent decisions (which path was taken); human overrides; validation results history; long retention (compliance with dev practices, knowledge replay) |
| **Children** | Work units as sub-workflows with own phase. Agent loops referenced (not children in lifecycle sense — they have their own audit story in AGENT_LOOPS). PRs/artifacts referenced via ObjectStore |
| **Lifetime** | Hours to days per mission; possibly weeks for large specs |
| **Write pattern** | Coordination: infrequent (transitions per hour). Per-work-unit state: infrequent. Findings: medium frequency (accumulate as agents learn). Artifact refs: low frequency |
| **Read pattern** | Live dashboard: medium. Agent context loading: every agent loop reads mission state (high read on resume). Cross-mission analytics: "missions per spec type". Restart-recovery: full mission state load |
| **Tier** | **B** — coordination root, rich subtree (work units + findings + agent refs) |

**Key insight**: findings are first-class graph knowledge. They MUST be triples in ENTITY_STATES — that's their whole point. The mission's value-add is having accumulated and refined them. A harness that hides findings in a private bucket defeats the purpose.

**Second insight**: the mission's relationship to AGENT_LOOPS is "references not owns." Agent loops have their own private bucket (defensible per rubric: high-volume per-loop trace). Mission references them by loop_id. This is the pattern: workflow coordinates lightweight refs; heavy state lives in domain-appropriate stores.

## 3. Manufacturing batch

| Axis | Characterization |
|---|---|
| **IS** | A production run on a manufacturing line producing N units of a SKU |
| **OWNS** | SKU ref; equipment IDs (line + machines); recipe/process ref; materials consumed (lot numbers + quantities); per-unit refs (each may have own quality record); aggregate quality measurements; production schedule (start/expected-end/actual-end); operator interventions; output artifacts (test reports, certs of analysis) |
| **State semantics** | **Replace**: batch phase (scheduled → setup → producing → completing → completed/scrapped/held). **Accumulate**: units produced (counter), per-unit quality records, material consumption events, operator interventions |
| **Operator queries** | "Current batch on line L"; "batches that failed quality this week"; "material lot used in batch B" (traceability); "audit trail for batch B" (compliance); "compare yield across batches of SKU S" |
| **Audit** | Phase transitions with source (operator, automation, alarm-triggered hold); material consumption (full traceability — what went into what); quality measurements (regulated); equipment state changes. **Very long retention** (regulatory: 7-20+ years depending on industry) |
| **Children** | Per-unit could be sub-workflows (each unit has quality lifecycle). Quality test runs could be sub-workflows |
| **Lifetime** | Minutes to days per batch |
| **Write pattern** | Phase: infrequent. Counters: frequent (per-unit increments). Quality measurements: medium frequency. Material consumption: medium frequency |
| **Read pattern** | Live operator dashboard: high frequency. Cross-batch analytics: medium. Compliance queries: low frequency but deep history (years back) |
| **Tier** | **B** — coordination root with subtree; quality records are first-class state |

**Key insight**: regulatory retention is real. Audit must outlive the batch entity by years. ENTITY_STATES with KV revision history works, but the bucket needs operator-controlled long TTL. This matches ADR-047's "apps own bucket topology" principle for the ENTITY_STATES bucket itself.

**Second insight**: material traceability is graph-native. "Lot L43 → went into batches B1, B2, B7" is a triple query. Holding this in a private bucket would lose the graph's natural relationship-traversal capability.

## 4. semconnect API request lifecycle

| Axis | Characterization |
|---|---|
| **IS** | A single HTTP request to the OGC Connected Systems API (or any HTTP service riding the framework) |
| **OWNS** | Request input (path, params, headers, body); auth/tenant context; processing intermediate state; result (response body, status); error state (if any) |
| **State semantics** | **Replace**: phase (received → validating → executing → responding → completed/errored). **Accumulate**: nothing meaningful — request is mostly write-once on completion |
| **Operator queries** | "Errored requests in last hour" (filter); "requests for endpoint E this hour" (cross-entity); audit: "who accessed entity X via API" (filter + relationship) |
| **Audit** | Compliance / access audit (who hit what); performance (latency tracking). Short retention for live debug (days-weeks); archive for compliance (months) |
| **Children** | None typically. A request is a leaf — it doesn't spawn child workflow instances |
| **Lifetime** | Milliseconds to seconds per request |
| **Write pattern** | 2-5 transitions per request, total. **VERY HIGH per-second volume** (many requests/sec) |
| **Read pattern** | Live monitoring: streaming. Post-hoc audit: low frequency. Cross-request analytics: medium frequency |
| **Tier** | **C** or — more honestly — **doesn't belong in the harness at all** |

**Key insight**: API requests are millisecond-scale leaf computations with high volume. The harness's value-add (named persistent instance, restart recovery, operator API surface) doesn't apply. These are better as JetStream consumer ack semantics + standard request/response logging, not lifecycle Participants.

**If a consumer forces them into the harness**: the per-request write volume would dominate ENTITY_STATES. This might be the rare case that DOES justify a private bucket on rubric item #4 (write rate). But the right answer is probably "don't use the harness for this shape" rather than "build special accommodation."

## 5. Sensor lifecycle

| Axis | Characterization |
|---|---|
| **IS** | A long-lived sensor entity's lifecycle from initial deployment through retirement |
| **OWNS** | Sensor identity (serial/model/manufacturer); deployment location ref (geo entity); calibration history; maintenance history; current health status; output stream ref (the data stream the sensor produces); current configuration |
| **State semantics** | **Replace**: phase (provisioned → calibrating → active → degraded → maintenance → active → retired); health status (frequent); configuration. **Accumulate**: calibration records, maintenance records |
| **Operator queries** | "All sensors at zone Z"; "degraded sensors"; "when was sensor S last calibrated"; "what data stream from sensor S"; "sensors by manufacturer" |
| **Audit** | Calibration audit (regulated for accuracy claims); maintenance audit; long retention (sensor lifetime: years) |
| **Children** | Calibration sessions could be sub-workflows (each has phase). Maintenance events could be sub-workflows. **Data captures are NOT children** — they're a separate high-volume continuous stream |
| **Lifetime** | Years per sensor |
| **Write pattern** | Phase: rare (months between transitions). Health: medium (periodic checks). Calibration/maintenance: rare events |
| **Read pattern** | Live dashboard: medium. Per-sensor history: medium. Cross-sensor analytics: low |
| **Tier** | **A** or **B** — closest to A if calibration/maintenance are inline records; B if they're sub-workflows |

**Key insight**: sensor data (the high-volume readings) is NOT part of the lifecycle. Sensor's "lifecycle" is sparse state changes on the sensor entity itself plus periodic calibration/maintenance events. The data stream is a separate concern entirely.

**Second insight**: this is the cleanest case for tier-A framing in our candidate set. But even here, the operator-relevant relationships (sensor → location, sensor → stream, sensor → calibration history) all want to be queryable via graph queries. Triples in ENTITY_STATES still wins.

## Synthesis

### Cross-cutting findings

**1. Four of five consumers are Tier B (coordination root with subtree).**
Drone survey, semspec dev-via-spec, manufacturing batch, sensor lifecycle (arguably) all coordinate relationships to other entities and own per-instance child state. semconnect API request is the outlier — Tier C or "shouldn't be in the harness at all."

**2. NO consumer in this set is Tier A in the thin id+phase+metadata sense** that beta.85 was designed around. Even sensor lifecycle — the closest fit — has rich relationships (location, stream, calibration history) that want to be graph-queryable.

**3. High-volume per-entity data never lives in the workflow state.**
Drone telemetry → separate stream + window refs. Captured imagery → ObjectStore + refs. Manufacturing per-unit quality measurements → sub-workflow per unit OR aggregate counters. Agent loop traces → AGENT_LOOPS private bucket (defensible on rubric). Sensor data → separate stream.

This means the "private bucket for atomicity" concern is bounded — high-volume state is already out of the workflow; what's left is low-volume coordination state where AddTriplesBatch atomicity is sufficient.

**4. Relationships are first-class.**
Every consumer has refs to other entities (drone, area, sensor, sku, equipment, spec, agent loops, geo location, output stream). These are NATURALLY triples in ENTITY_STATES — that's the whole point of the graph layer. A harness that holds these in a private struct loses the graph's relationship-traversal capability and forces every operator query to be bucket-specific.

**5. Findings/knowledge are first-class.**
semspec dev-via-spec's findings are the most explicit example, but every consumer accumulates knowledge: drone survey's anomaly log + capture metadata; manufacturing's quality data + lot traceability; sensor's calibration history. This knowledge IS what the graph layer is for. A private bucket hides it.

**6. Audit retention varies wildly but is operator-controlled per bucket.**
- API request: days to months
- Drone survey: months to years
- semspec mission: months to years (knowledge replay)
- Manufacturing: 7-20+ years (regulatory)
- Sensor: years (sensor lifetime)

Operator-controlled per-bucket TTL on ENTITY_STATES (or a derivative bucket) covers this uniformly. No new mechanism needed.

**7. Write patterns are uniformly LOW for coordination state.**
Excluding high-volume data that already lives elsewhere:
- Drone survey: ~per-second during flight, hours
- semspec mission: handfuls per hour, days
- Manufacturing: per-unit counter + occasional phase, days
- API request: 2-5 writes per request (but many requests — see below)
- Sensor: months between phase changes

For four of five consumers, per-entity contention is naturally LOW. Optimistic concurrency at the Manager layer (Path A in the design exercise) is well-suited. The exception is API requests, which probably shouldn't be in the harness.

**8. Read patterns are HEAVY on relationship traversal and aggregation.**
- "All flying missions" → query across mission entities
- "Sensors at zone Z" → relationship query
- "Yield per drone" → cross-entity aggregation
- "Findings accumulated by mission M" → relationship traversal
- "Material lot L → batches" → reverse relationship

These ARE graph queries. The graph-gateway already exposes this via GraphQL. The lifecycle-gateway as designed reinvents a parallel query API. A workflow-aware projection over graph-gateway would deliver the same operator UX without duplication.

### Pattern that emerges

A workflow instance in this codebase IS:

> A named entity in the knowledge graph that:
> - Has a declared lifecycle (phases + transitions)
> - Owns relationships to other entities (children, references, artifacts)
> - Accumulates findings/state over its lifetime
> - Has operator-meaningful "what's its current state" + "what's its timeline" surface
> - May spawn child workflow instances (sub-workflows)
> - Has audit retention typically longer than the entity is "active"

That's NOT id+phase+metadata. That's a coordination root in the graph. Which means:

- Mission state belongs IN the graph (triples in ENTITY_STATES) so all the graph's existing capabilities apply
- Schema declaration needs to express children + references, not just phase enum
- Audit IS the graph's revision history (with optional retention extension)
- Operator API is a workflow-aware projection over the graph, not a parallel surface

### Implications for the design exercise

This evidence amends the design exercise as follows:

**Q1 (Path A vs C)**: Path A is strongly recommended. Four of five consumers have low per-entity write rates; optimistic concurrency at Manager layer handles them cleanly. Manufacturing's per-unit counter is the highest-frequency case and even that's seconds-scale, not millisecond-scale.

**Q2 (lifecycle-gateway future)**: Strong evidence for "projection over graph-gateway." Every operator query in the sketches is a graph query in disguise; building a parallel API duplicates capability without adding value.

**Schema declaration shape** needs an addition we didn't surface in the design exercise:
- Beta.85 schema: phase enum + operator-writable fields
- Proposed prime: + **declared child workflow types** (e.g. mission OWNS work_unit instances) + **declared reference predicates** (e.g. mission REFERENCES drone, area)
- This makes operator queries like "mission + its children + their phases" a single composed graph query

**The API request case argues for an explicit "not for this" guidance** in the harness docs. The rubric should help consumers decide "this isn't workflow-shaped, use X instead."

**The "findings as first-class graph knowledge" finding** argues against any design that holds workflow state outside the graph. semspec dev-via-spec is the strongest case: findings ARE the deliverable. They must be triples.

### What this DOESN'T resolve

- **CAS semantics for the Manager retry loop** — Path A's engine work in graph-ingest (ExpectedRevision) is still required; consumer sketches don't change that
- **Migration story for beta.85 e2e fixture** — small (delete bucket + reseed); unchanged
- **beta.85 disposition** — still the user's call (leave as v0 / yank / force-tag)
- **The rare exception case** — sketches don't argue against having ONE in the framework (AGENT_LOOPS); they argue the lifecycle harness shouldn't be one

## Recommendation

Amend the design exercise:

1. **Replace the implicit "Definition A" anchor with explicit "Definition B as default; Definition C as rare exception with rubric defense."**
2. **Add child-workflow + reference declarations to the schema shape.**
3. **Strengthen Path A as the primary recommendation** — consumer write patterns support it cleanly.
4. **Position lifecycle-gateway redesign as projection-over-graph-gateway** rather than "either refactor or retire."
5. **Add a "when the harness ISN'T the right shape" section** referencing the API-request anti-pattern.

The design exercise's bucket-ownership rubric stands. The consumer evidence strengthens its recommendation rather than complicating it.

## Related context

- [[project_adr_047_048_bundle_e2e_handoff]] — the e2e session that surfaced the gap
- `docs/proposals/lifecycle-harness-prime-design-exercise.md` — the design exercise these sketches inform
- `docs/proposals/workflow-primitives-robotic-sketch.md` — prior art for drone-survey patterns
- `docs/proposals/workflow-primitives-semspec-mapping.md` — prior art for semspec's workflow shape
- `docs/proposals/workflow-primitives-semconnect-sketch.md` — prior art for OGC API patterns
