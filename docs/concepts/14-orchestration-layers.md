# Orchestration Layers — How We Do Workflows in semstreams

semstreams has two orchestration layers — **rules** and **components**.
There is no separate workflow engine. Multi-step patterns
(linear pipelines, conditional branches, bounded iteration loops,
async fan-out / fan-in) are expressed as **coordinated rule sets that
fire components**, with per-action `MaxIterations` providing iteration
caps and `Graphable` entity triples + KV state + ObjectStore providing
durable storage.

This document is the canonical "how we do workflows in semstreams"
pattern catalog. If you find yourself reaching for a workflow engine,
state machine, or new KV bucket, **read this first**. Most of the
time the existing primitives already cover the case.

## Why no separate workflow engine?

A reactive workflow engine (`processor/reactive/`) shipped early in
semstreams's life. It provided typed multi-phase state machines, async
callback correlation, loop limits, and timeouts. It also bypassed the
component framework: raw JetStream resources, invisible to flow
discovery, broke the flowgraph validator. The capabilities were real;
the integration discipline wasn't.

Decision (2026-03-12): retire `processor/reactive/`. Absorb the
capabilities the rule engine was missing (per-action firing caps,
conditional branching in `when` clauses, configurable state buckets).
Retirement completed in `main`; only `pkg/workflow/` (state-manager
primitives) and a legacy `workflow_trigger_payload.go` compatibility
shim remain. semspec — the early heavy reactive-workflow user — has a
sister-repo migration tracked separately.

The durable lesson: **a workflow primitive that lives outside the
component framework creates state-plumbing debt the framework can't
help you pay down later.** When you find a gap in the rule engine,
file it as engine work. Don't build app-side state machines around it
(see "The semspec trap" below).

## The Two Layers

```text
┌─────────────────────────────────────────────────────────────┐
│  RULE ENGINE  (orchestration)                               │
│                                                             │
│  Watches: KV state, NATS subjects, wallclock                │
│  Evaluates: typed conditions (unified evaluator, ADR-041)    │
│  Fires: actions (publish, publish_agent, deny, etc.)         │
│  Caps: per-action MaxIterations (default 3, explicit 0 =     │
│        unlimited)                                            │
│                                                             │
│  Rules trigger work. They don't do work.                    │
└─────────────────────────────────────────────────────────────┘
                            │
                            ▼  fires
┌─────────────────────────────────────────────────────────────┐
│  COMPONENTS  (execution)                                    │
│                                                             │
│  Receive: typed payloads on input ports                     │
│  Execute: LLM calls, graph queries, file I/O, etc.          │
│  Emit: results on output ports (KV writes, NATS publishes)   │
│                                                             │
│  Components are caller-agnostic. They don't know what       │
│  triggered them or what comes next.                         │
└─────────────────────────────────────────────────────────────┘
```

### Layer responsibilities

| Layer | Owns | Does NOT own |
|---|---|---|
| Rule engine | Trigger conditions, action sequencing, iteration caps, condition evaluation | Work execution, business logic, payload semantics |
| Component | Work execution, internal state machine, output emission | Caller identity, multi-step coordination, cross-component sequencing |

## Signal kinds the rule engine watches

Three kinds of signals fire rules:

| Signal | Rule type | Triggered by |
|---|---|---|
| KV state change | expression rule (`type: "expression"`) | A bucket key transitioning to a state where the rule's `when` predicates match |
| NATS subject match | expression rule (`type: "expression"`) | A message landing on a subject the rule subscribes to |
| Wallclock | cron rule (`type: "cron"`) | A cron expression's `Next()` time elapsing (ADR-031) |

Cron rules accept `schedule`, `actions`, `cooldown`, `fire_every_n_events`,
`name`, `description`, `enabled`, `metadata` only — condition-side fields
are rejected at config-load.

## Pattern Catalog

Five patterns cover essentially all multi-step orchestration in
semstreams. Each shows the rule shape, the state-storage shape, and
when to use it.

### Pattern 1 — Single trigger

**Shape**: `A completes → B starts (no retry, no loop)`

**Use**: simple handoffs. Architect produces a plan; editor implements
it. Validator approves a payload; publisher releases it.

**Implementation**: one rule with one action.

```json
{
  "name": "architect_complete_spawn_editor",
  "type": "expression",
  "when": {
    "bucket": "AGENT_LOOPS",
    "key_pattern": "COMPLETE_*",
    "conditions": [
      {"field": "role", "op": "eq", "value": "architect"},
      {"field": "outcome", "op": "eq", "value": "success"}
    ]
  },
  "actions": [
    {
      "type": "publish_agent",
      "role": "editor",
      "payload_ref": "{state.plan_objstore_ref}"
    }
  ]
}
```

**State**: lives on the `COMPLETE_{loopID}` KV entry the upstream
component wrote. The rule reads it; no new bucket.

### Pattern 2 — Linear pipeline

**Shape**: `A → B → C → D (no loop)`

**Use**: multi-stage processing where each stage's output feeds the
next. Decompose → fetch → fuse → synthesize is the
ADR-045 graph-search example.

**Implementation**: a chain of rules, each watching for the prior
stage's completion event.

```json
[
  {"name": "stage_1_kickoff", "when": {"key_pattern": "input.received.*"},
   "actions": [{"type": "publish", "subject": "component.stage_1.{id}"}]},
  {"name": "stage_2_after_1", "when": {"key_pattern": "stage_1.complete.*"},
   "actions": [{"type": "publish", "subject": "component.stage_2.{id}"}]},
  {"name": "stage_3_after_2", "when": {"key_pattern": "stage_2.complete.*"},
   "actions": [{"type": "publish", "subject": "component.stage_3.{id}"}]},
  {"name": "stage_4_after_3", "when": {"key_pattern": "stage_3.complete.*"},
   "actions": [{"type": "publish", "subject": "component.stage_4.{id}"}]}
]
```

**State**: each stage writes its output as triples on an operation
entity in an existing KV bucket (e.g., `AGENT_LOOPS`). Bulky payloads
go to ObjectStore via `ContentStorable`; only refs travel in rule
payloads. No new bucket; no parallel state machine.

### Pattern 3 — Conditional branch

**Shape**: `A → if X then B else C`

**Use**: route to different downstream actions based on the result of
a prior stage. "If validation passes, publish; otherwise, request
human review."

**Implementation**: one rule with multiple actions, each gated by an
action-level `when` clause. The unified condition evaluator (ADR-041)
makes this clean — `when` sees the same fields as rule-level
conditions.

```json
{
  "name": "route_validation_result",
  "when": {"key_pattern": "validation.complete.*"},
  "actions": [
    {
      "type": "publish",
      "subject": "publisher.release.{id}",
      "when": "$state.validation.passed == true"
    },
    {
      "type": "publish_agent",
      "role": "human_reviewer",
      "when": "$state.validation.passed == false"
    }
  ]
}
```

**State**: validation result lives on the operation entity. No new
bucket.

### Pattern 4 — Bounded iteration

**Shape**: `A → B → A → B... (max N times)`

**Use**: review-fix cycles, refine-and-retry loops, retry on
transient failure with backoff. Cap is required to prevent unbounded
loops.

**Implementation**: a rule whose action publishes the loop start,
with `max_iterations` on the action as the cap. The per-action firing
counter is keyed on a stable action ID (auto-generated fingerprint or
author-supplied).

```json
{
  "name": "review_fix_cycle",
  "when": {"key_pattern": "review.complete.*"},
  "actions": [
    {
      "type": "publish_agent",
      "role": "fixer",
      "when": "$state.review.issues_count > 0 AND $state.iteration < 3",
      "max_iterations": 3
    },
    {
      "type": "publish",
      "subject": "pipeline.complete.{id}",
      "when": "$state.review.issues_count == 0 OR $state.iteration >= 3"
    }
  ]
}
```

**Default `MaxIterations`**: 3 (framework-wide, applies when the
field is unset). Explicit `"max_iterations": 0` means unlimited.
Authors who want stable counters across rule renames set
`Action.ID` explicitly.

**State**: iteration count is tracked by the rule engine itself
(`MatchState.ActionIterations`); no app-side counter needed.

### Pattern 5 — Async fan-out / fan-in

**Shape**: `A → (B, C, D in parallel) → E when all done`

**Use**: parallel sub-tasks with a synchronization point. Fan out to
multiple workers; gather results before proceeding.

**Implementation**: one rule with multiple `publish` actions for the
fan-out; a second rule on a "synchronizer" key pattern for the
fan-in. The synchronizer is updated by each parallel branch and the
fan-in rule fires when all expected branches have written.

```json
[
  {
    "name": "fanout_to_workers",
    "when": {"key_pattern": "fanout.start.*"},
    "actions": [
      {"type": "publish", "subject": "component.worker_b.{id}"},
      {"type": "publish", "subject": "component.worker_c.{id}"},
      {"type": "publish", "subject": "component.worker_d.{id}"}
    ]
  },
  {
    "name": "fanin_when_all_complete",
    "when": {
      "key_pattern": "fanout.synchronizer.*",
      "conditions": [
        {"field": "completed_count", "op": "eq", "value": 3}
      ]
    },
    "actions": [
      {"type": "publish", "subject": "component.aggregator.{id}"}
    ]
  }
]
```

**State**: each worker writes its result to the operation entity and
increments the synchronizer count. The fan-in rule reads the count.
No new bucket; the operation entity carries the synchronization
state.

## State Storage Boundaries

Three categories of data, with different storage patterns. **The
discipline here is what keeps you out of the semspec trap** (see
below).

| Category | Storage | Rule-observable? | In knowledge graph? |
|---|---|---|---|
| **Domain entities** | `ENTITY_STATES` KV | Yes | Yes (`Graphable`) |
| **Operational results** | Component-specific KV (e.g., `AGENT_LOOPS`) | Yes | No |
| **Events** | JetStream streams | No (rules watch KV, not streams) | No |
| **Bulky payloads** | ObjectStore via `ContentStorable`; ref-triples on owning entity | Indirectly (via refs) | Refs only |

### Domain entities (`ENTITY_STATES`)

Semantic domain objects implementing `Graphable`:

- 6-part hierarchical entity ID (`org.platform.domain.system.type.instance`)
- Persist across multiple events
- Queryable in the knowledge graph

**Only `graph-ingest` writes to `ENTITY_STATES`.**

### Operational results (component-specific KV)

Execution outcomes that are not semantic domain entities:

- Use `COMPLETE_{id}` key pattern for rules observability
- Stored in component-specific buckets:
  - `AGENT_LOOPS`: agent + research operation state (`COMPLETE_{loopID}`)
  - Other components may register their own buckets when warranted
- Transient — represent what happened, not what exists

Rules can watch multiple buckets via the `entity_watch_buckets`
config:

```json
{
  "entity_watch_buckets": {
    "ENTITY_STATES": ["telemetry.>"],
    "AGENT_LOOPS": ["COMPLETE_*", "research.*"]
  }
}
```

### Events (JetStream)

Immediate notifications for downstream processing:

- Published to streams for subscribers
- Not directly observable by rules
- Examples: `agent.complete.*`, `graph.ingest.*`

### Bulky payloads (ObjectStore via `ContentStorable`)

Per ADR-028: **rules carry references, never content.** If a payload
might exceed ~16KB or contain freeform text/code/artifacts, write it
to ObjectStore and put the ref-triple on the owning entity. The rule
payload carries only the entity ID and ref. Components reading the
payload dereference on demand.

### Anti-pattern: writing operational results to `ENTITY_STATES`

Pollutes the knowledge graph with non-semantic data. Breaks
`Graphable` contract. Makes graph queries less meaningful.

```go
// WRONG
entityBucket.Put(ctx, "workflow.review.exec123", completionData)

// RIGHT — operational results in component bucket with COMPLETE_ prefix
agentLoopsBucket.Put(ctx, "COMPLETE_exec123", completionData)
```

## Rules of Thumb

### 1. Rules trigger; they don't orchestrate inline

A rule fires one set of actions, not a sequence of stateful steps.

**Anti-pattern**: Rule A sets `step=1`, Rule B watches for `step=1`
and sets `step=2`, Rule C watches for `step=2`...

**Correct**: a multi-step pattern is a coordinated set of rules where
each rule fires a component; state lives on the operation entity.
See Pattern 2 (linear pipeline).

### 2. Components execute; they don't coordinate

A component does one thing. If a component is dispatching work to
other components inline, that orchestration belongs in the rule
layer.

**Anti-pattern**: a component that, after finishing its work, calls
into another component's API directly.

**Correct**: the component emits its completion event; a rule
watches for that event and fires the next component.

### 3. Components are caller-agnostic

A component doesn't know if it's standalone or part of a multi-step
pattern. Same component, same behavior, regardless of caller.

**Anti-pattern**: `if msg.workflow_id != "" { ... }` inside a
component.

**Correct**: behavior differences are configured (component config),
not branched on caller identity.

### 4. State ownership is exclusive

Only one layer owns a piece of state.

| State | Owner |
|---|---|
| Trigger conditions | Rule engine |
| Iteration counters | Rule engine (per-action `MatchState`) |
| Execution state inside a component | Component |
| Domain entities | `graph-ingest` (writes to `ENTITY_STATES`) |
| Operational results | Component that produced them (writes to its own bucket) |

### 5. If you need a new bucket, ask twice

Adding a new KV bucket is a discipline-load decision, not a syntactic
one. The semspec trap (below) is what happens when new buckets
proliferate without the framework being able to see them. Before
adding one:

- Can the data live on an existing entity as triples?
- Can the data live in an existing component's bucket
  (`AGENT_LOOPS`, etc.) with a distinct key prefix?
- Can bulky content live in ObjectStore with a ref-triple on an
  existing entity?

If the answer to all three is "no," and the bucket is genuinely
component-owned operational state, register it via `entity_watch_buckets`
and document it in the component's docs. **Never** create app-side
state buckets that the rule engine isn't configured to watch.

### 6. Engine gaps file as engine work

If the rule engine can't express something you need (e.g., reading
an evidence-array length inside a `when` clause), **file it as a
rule-engine improvement**, not an app-side workaround. The semspec
trap is the cautionary tale (below).

## The semspec trap (don't repeat it)

semspec was an early adopter, predating the mature rule engine. To
work around rule-engine limitations, it built **its own plan and
execution state machines** — roughly 7,264 LOC of `workflow/reactive/`
code with its own state plumbing alongside the rule engine.

That code is now a migration blocker. It imports the retired
`processor/reactive/` engine, maintains its own state, has its own
audit surface, and is invisible to flow discovery and the flowgraph
validator. The team can't dig out anytime soon.

**The lesson**: when the framework is missing something, the answer
is engine work upstream, not app-side scaffolding downstream. Every
time an app adds its own state machine "just for this one case," the
framework loses the ability to help it later — debugging,
observability, restart safety, validation, all degrade.

This document is the canonical "how to do workflows in semstreams"
answer. If the answer isn't here, propose adding it. If a pattern
genuinely needs a new primitive, propose adding the primitive to the
rule engine via ADR + engine ticket. Don't carve out a parallel
state-machine path.

## Debugging Orchestration Issues

### Symptom: action fires multiple times unexpectedly

**Likely cause**: rule re-triggers because state oscillates after the
action runs.

**Check**: is the action modifying state that causes the rule's
condition to re-match?

**Fix**: idempotent state updates, or track "already processed"
flags on the entity. Verify `MaxIterations` is set on the action.

### Symptom: chain stalls partway through

**Likely cause**: a stage completed but the next rule isn't watching
the right key pattern, or the rule's `when` clause doesn't match the
emitted state.

**Check**: read the KV bucket; confirm the stage's completion key was
written; trace the rule engine's evaluation log for the next rule.

**Fix**: align the rule's key pattern and `when` predicates with what
the prior stage actually writes.

### Symptom: component behaves differently in chain vs. standalone

**Likely cause**: component has caller awareness it shouldn't.

**Check**: does the component branch on a workflow ID, caller role,
or any field that varies by caller?

**Fix**: remove caller awareness. Behavior differences must come from
configuration, not caller identity.

### Symptom: looking for "the workflow ID"

**There isn't one.** Multi-step patterns in semstreams are identified
by the operation entity ID (e.g., the loop ID on the research-pipeline
entity in `AGENT_LOOPS`). If you're reaching for a separate workflow
identifier, you're probably about to recreate the semspec trap.

## Use Case Examples

### Simple agent handoff (Pattern 1)

```text
Architect completes with plan → editor receives plan, implements.
```

Layer mapping:
- Rule: `when architect completes → publish_agent editor`
- Components: `agentic-loop` executes architect, then editor

### Multi-step agent chain with retry (Patterns 2 + 4)

```text
For each task: architect → editor → reviewer → if issues, fix and re-review (max 3) → done.
```

Layer mapping:
- Rules: one per transition; one with `max_iterations: 3` for the fix→review loop
- Components: `agentic-loop` for each role
- State: a task entity in an existing bucket carries phase, iteration,
  feedback

### Data pipeline with validation retry (Pattern 4)

```text
Ingest → validate → if invalid and attempts < 3, request correction, goto validate → if valid, process.
```

Layer mapping:
- Rules: ingest trigger, validation router (Pattern 3 conditional),
  retry action with `max_iterations: 3`
- Components: validator, corrector, processor
- State: an ingestion entity carries attempts and validation result

### Graph search decomp+fusion (Patterns 2 + 3 + 4, ADR-045)

```text
research_graph(topic, hints?)
  → nl_classify  (reuses existing graph/query.Classifier)
  → route_search (LLM examines candidates, emits one of 4 actions)
  → branch:
       synthesize_directly  → synthesize → return
       retighten            → loop back to nl_classify (max 2)
       walk_seeds           → execute → assess → refine? (max 5) → synthesize → return
       decompose            → execute → assess → refine? (max 5) → synthesize → return
```

Layer mapping:
- Rules: seven rules (R0–R6 in ADR-045) coordinating the chain;
  conditional branch on `route_search` decision; conditional branch
  on `assess_sufficiency`; retighten loop with `max_iterations: 2`
  (R2); refine loop with `max_iterations: 5` (R4); continuation rule
  fires the parent
- Components: `nl_classify` (wraps existing classifier),
  `route_search`, `execute_subqueries`, `assess_sufficiency`,
  `synthesize_answer`
- State: a research-pipeline entity in `AGENT_LOOPS`; classifier
  candidates + multi-hop evidence in ObjectStore via
  `ContentStorable`; refs as triples on the entity

See [ADR-045](../adr/045-graph-search-rule-chain.md) for the full
design and the classify-and-route rationale.

## References

- [/orchestration-check](../../.claude/skills/orchestration-check/SKILL.md)
  — decision skill for choosing between patterns
- [/kv-or-stream](../../.claude/skills/kv-or-stream/SKILL.md) —
  facts vs requests heuristic
- [/new-payload](../../.claude/skills/new-payload/SKILL.md) —
  payload registry checklist
- [Concept: KV Twofer](02-kv-twofer.md) — single KV write = state +
  events + history
- [Concept: Streams vs KV Watches](03-streams-vs-kv-watches.md) —
  facts vs requests
- [Concept: Agentic Systems](13-agentic-systems.md) — agentic loop
  fundamentals
- [Concept: Rule-Driven Artifacts](18-rule-driven-artifacts.md) —
  emitting markdown/JSON/webhook artifacts from rule actions
- [ADR-028: Agentic Orchestration Architecture](../adr/028-orchestration-architecture.md)
- [ADR-031: Time-Trigger Primitive (cron rules)](../adr/031-time-trigger-primitive.md)
- [ADR-041: Unified Condition Evaluator](../adr/041-unified-condition-evaluator.md)
- [ADR-045: Graph Search Decomp+Fusion via Rule-Chain + Components](../adr/045-graph-search-rule-chain.md)
- Memory: `project_reactive_workflow_retirement` — historical context
  on the workflow engine retirement
