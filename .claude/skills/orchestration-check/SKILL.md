---
name: orchestration-check
description: Determine whether logic belongs in a rule (single trigger or chain), a component, or somewhere else. Use when adding orchestration logic, designing multi-step processes, or reviewing boundary violations.
argument-hint: [pattern or logic being evaluated]
---

# Orchestration Layer Check

## What pattern are you evaluating?

$ARGUMENTS

## The Two Layers

semstreams has **rules** and **components**. There is no separate
workflow engine — `processor/reactive/` was retired. Multi-step
patterns are expressed as coordinated rule sets firing components,
with per-action `MaxIterations` as the iteration cap.

| Layer | Responsibility | Owns | Does NOT Own |
|-------|---------------|------|--------------|
| **Rule Engine** | State detection, trigger conditions, action sequencing, iteration caps | Trigger conditions, action sequence, `MaxIterations`, condition evaluation | Work execution, business logic, payload semantics |
| **Component** | Execute single units of work | Execution mechanics, internal state machine | Caller identity, multi-step coordination |

## Quick Decision

| Pattern | Use |
|---------|-----|
| A completes → B starts (no retry, no loop) | Single rule, one action |
| A → B → C → D (no loop) | Rule chain (one rule per transition) |
| A → if X then B else C | One rule, action-level `when` clauses (ADR-041) |
| A → B → A → B... (max N times) | Rule chain with per-action `MaxIterations` cap |
| Fan-out + fan-in synchronization | Fan-out rule + synchronizer-key rule |
| Execute LLM call, graph query, file I/O, etc. | Component |

## The 6 Rules

1. **Rules trigger; they don't orchestrate inline.** A rule fires
   one set of actions, not a sequence of stateful steps.
   - Anti-pattern: Rule A sets `step=1`, Rule B watches for `step=1`
     and sets `step=2`, Rule C watches for `step=2`...
   - Fix: each rule fires a component; state lives on the operation
     entity, not in step counters.

2. **Components execute; they don't coordinate.** Components do one
   thing and emit a result. They don't dispatch to other components
   inline.
   - Anti-pattern: a component calling another component's API
     directly after finishing its work.
   - Fix: the component emits its completion event; a rule fires
     the next component.

3. **Components are caller-agnostic.** Same component, same
   behavior, regardless of who triggered it.
   - Anti-pattern: `if msg.workflow_id != "" { ... }` inside a
     component.
   - Fix: behavior differences come from configuration, not caller
     identity.

4. **State ownership is exclusive.** Only one layer owns a piece
   of state.

   | State | Owner |
   |-------|-------|
   | Trigger conditions | Rule engine |
   | Iteration counters | Rule engine (per-action `MatchState`) |
   | Execution state inside a component | Component |
   | Domain entities | `graph-ingest` (writes to `ENTITY_STATES`) |
   | Operational results | Component that produced them (writes to its own bucket) |

5. **If you need a new KV bucket, ask twice.** Can the data live on
   an existing entity as triples? Can it live in an existing
   component's bucket (`AGENT_LOOPS`, etc.) with a distinct key
   prefix? Can bulky content live in ObjectStore with a ref-triple?
   If all three are "no" and it's genuinely component-owned
   operational state, register the new bucket in
   `entity_watch_buckets` and document it. **Never** create
   app-side state buckets the rule engine isn't configured to watch.

6. **Engine gaps file as engine work; never as app-side state
   plumbing.** If the rule engine can't express something you need
   (e.g., an evidence-array length predicate in `when`), file it as
   a rule-engine improvement. The semspec trap (7,264 LOC of
   `workflow/reactive/` that became a migration blocker) is the
   cautionary tale.

## Anti-Patterns

- Rule chains that build up step counters across multiple firings
  (state belongs on the operation entity, not in rule-fired step
  numbers)
- Components dispatching to other components inline (rules
  coordinate, components execute)
- Components branching on caller identity (configure behavior,
  don't introspect caller)
- Both rules and entity triples tracking the same state (exclusive
  ownership violated)
- App-side state machines around rule-engine limitations (the
  semspec trap)
- New KV buckets without `entity_watch_buckets` registration
  (invisible to the rule engine)

## State Storage Boundaries

| Category | Storage | Rule-observable? | In Knowledge Graph? |
|----------|---------|------------------|---------------------|
| Domain entities | `ENTITY_STATES` KV (only `graph-ingest` writes) | Yes | Yes (`Graphable`) |
| Operational results | Component-specific KV (e.g., `AGENT_LOOPS` with `COMPLETE_*`) | Yes (via `entity_watch_buckets`) | No |
| Events / work items | JetStream streams | No (rules watch KV, not streams) | No |
| Bulky payloads | ObjectStore via `ContentStorable`; ref-triple on owning entity | Indirectly (via refs) | Refs only |

Per ADR-028: **rules carry references, never content.** If a payload
might exceed ~16KB or contain freeform text, put it in ObjectStore
and pass a ref. Do NOT write operational results to `ENTITY_STATES`
— it pollutes the knowledge graph.

## Example: Single trigger (Pattern 1)

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

## Example: Bounded iteration (Pattern 4)

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

`MaxIterations` default is 3 framework-wide; explicit `0` means
unlimited. Stable per-action counters survive rule renames if
`Action.ID` is set explicitly.

## Full pattern catalog + worked examples

Read `docs/concepts/14-orchestration-layers.md` for the canonical
"How we do workflows in semstreams" reference, including:

- All 5 patterns with JSON rule definitions
- The semspec trap (what app-side state machines cost long-term)
- State-storage boundary discipline
- Debugging orchestration issues
- Worked examples (agent handoff, review-fix retry, data pipeline
  validation, ADR-045 graph-search decomp+fusion)
