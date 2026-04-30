# Orchestration Layers

SemStreams uses a two-layer orchestration model that separates concerns between reactive
orchestration and component execution. Understanding these layers and their boundaries is
essential for building maintainable, scalable systems.

> **Note**: The **Reactive Workflow Engine** handles both rules-style single
> triggers AND workflow-style multi-step orchestration using the same unified infrastructure.
> Both are defined in type-safe Go code with compile-time verification.

## The Two Layers

```text
┌─────────────────────────────────────────────────────────────┐
│  REACTIVE ENGINE (unified orchestration)                    │
│                                                             │
│  Single triggers (rules pattern):                           │
│  "When condition X, trigger action Y"                       │
│  - Watches KV state OR NATS subjects                        │
│  - Fires single actions (publish, mutate, etc.)             │
│                                                             │
│  Multi-step workflows:                                      │
│  "Execute steps A → B → C with timeouts and loop limits"    │
│  - Owns workflow state (phase, iteration count)             │
│  - Enforces timeouts, loop limits                           │
│  - Async callback correlation                               │
│                                                             │
│  All defined as type-safe Go code with ConditionFunc        │
└─────────────────────────────────────────────────────────────┘
                            │
                            ▼ spawns
┌─────────────────────────────────────────────────────────────┐
│  COMPONENTS (execution)                                     │
│  - agentic-loop: execute agent turns                        │
│  - graph: process triples                                   │
│  - etc.                                                     │
└─────────────────────────────────────────────────────────────┘
```

### Layer Responsibilities

| Layer | Responsibility | Owns | Does NOT Own |
|-------|---------------|------|--------------|
| **Reactive Engine** | State detection, multi-step orchestration | Conditions, actions, step sequence, timeouts | Actual work execution |
| **Component** | Execute single units of work | Execution mechanics | Workflow awareness |

## Reactive Patterns

The reactive engine fires rules in response to three signal kinds:

| Signal | Rule kind | Triggered by |
|---|---|---|
| KV state change | expression rule (`type: "expression"`) | A bucket key transitioning to a state where the rule's `When` predicates match |
| NATS subject match | expression rule (`type: "expression"`) | A message landing on a subject the rule subscribes to |
| Wallclock | **cron rule (`type: "cron"`)** | A cron expression's `Next()` time elapsing |

Cron rules ship in beta.27
([ADR-031](../adr/031-time-trigger-primitive.md),
[migration guide](../operations/migration-beta26-to-beta27.md)). They
share every existing rule action (`publish`, `publish_agent`,
`update_kv`, etc.) but bypass condition evaluation — there is no
"state" for a clock to be in. A `"type": "cron"` rule accepts
`schedule`, `actions`, `cooldown`, `fire_every_n_events`, `name`,
`description`, `enabled`, `metadata` and nothing else; condition-side
fields (`conditions`, `logic`, `on_enter`, `on_exit`, etc.) are
rejected at config-load time.

The reactive engine supports two coordination patterns through the same unified infrastructure:

### Single-Trigger Pattern (Rules-style)

For simple state-to-action reactions without multi-step coordination:

```go
// When architect completes, spawn editor
reactive.NewRule("architect-complete-spawn-editor").
    WatchKV("AGENT_LOOPS", "LOOP_*").
    When("architect role", func(ctx *reactive.RuleContext) bool {
        state := ctx.State.(*AgentLoopState)
        return state.Role == "architect"
    }).
    When("completed successfully", func(ctx *reactive.RuleContext) bool {
        state := ctx.State.(*AgentLoopState)
        return state.Outcome == "success"
    }).
    Publish("agent.task.editor", func(ctx *reactive.RuleContext) (message.Payload, error) {
        state := ctx.State.(*AgentLoopState)
        return &EditorTask{
            TaskID: state.TaskID,
            Prompt: fmt.Sprintf("Implement: %s", state.Result),
        }, nil
    }).
    Build()
```

This is appropriate when:
- Single condition triggers single action
- No iteration tracking needed
- No timeout enforcement required
- Fire-and-forget semantics

### Multi-Step Pattern (Workflow-style)

For coordinated sequences with loop limits and timeouts, use workflow definitions (see below).

## Workflow Layer

The reactive workflow engine handles multi-step orchestration that requires state tracking, loop limits,
and timeouts. It fills the gap between reactive rules and external orchestration systems like Temporal.

### Go-Based Type-Safe Workflows

Workflows are defined in type-safe Go code rather than JSON. This provides:

- **Compile-time safety**: Field references and type mismatches are caught by the compiler
- **IDE support**: Autocomplete, refactoring, and go-to-definition work naturally
- **Zero `json.RawMessage`**: Direct struct field access replaces string interpolation
- **Debuggability**: Standard Go debugging tools and stack traces
- **2 serialization boundaries**: vs. 9+ in the previous JSON-based approach

The reactive engine uses typed Go functions for conditions, payload building, and state mutations,
eliminating the data corruption issues that plagued string-based template interpolation.

### What Workflows Do Well

- **Step sequencing**: Execute A → B → C in order
- **Loop limits**: Stop after N iterations of a cycle
- **Workflow timeouts**: Fail the entire workflow after T seconds
- **Step timeouts**: Fail individual steps that run too long
- **State tracking**: Know which step we're on, how many iterations completed

### What Workflows Should NOT Do

- **Execute work directly**: Workflows spawn components; they don't do the work
- **Complex conditionals**: Use rules for condition evaluation, workflows for sequencing
- **Human tasks**: Not a BPM engine; use external systems for human-in-the-loop

### Workflow Definition Pattern

Workflows are composed of typed rules that watch KV state and fire actions. Each rule:

1. **Watches** a KV bucket pattern (e.g., `"review-fix.*"`)
2. **Evaluates** conditions against typed state (e.g., `state.Phase == "reviewing"`)
3. **Fires** an action when conditions match (e.g., publish async task)
4. **Mutates** state when the action completes (e.g., update phase, increment iteration)

The engine coordinates these rules through shared KV state, providing:

- Automatic state persistence and recovery
- Loop iteration tracking
- Timeout enforcement
- Concurrent execution safety with optimistic locking

### Example: Review-Fix Cycle with Loop Limit

A workflow is required when there's iteration with a termination condition:

```go
func ReviewFixCycleWorkflow() *reactive.Definition {
    const maxIterations = 3

    return reactive.NewWorkflow("review-fix-cycle").
        WithDescription("Review code, fix issues, re-review until clean or max attempts").
        WithStateBucket("REVIEW_FIX_STATE").
        WithStateFactory(func() any { return &ReviewFixState{} }).
        WithMaxIterations(maxIterations).
        WithTimeout(300 * time.Second).

        // Rule 1: Request review
        AddRule(reactive.NewRule("request-review").
            WatchKV("REVIEW_FIX_STATE", "review-fix.*").
            When("phase is reviewing", reactive.PhaseIs("reviewing")).
            When("under max iterations", reactive.IterationLessThan(maxIterations)).
            When("no pending task", reactive.NoPendingTask()).
            PublishAsync(
                "agent.task.reviewer",
                func(ctx *reactive.RuleContext) (message.Payload, error) {
                    state := ctx.State.(*ReviewFixState)
                    return &ReviewRequest{
                        CodeID: state.CodeID,
                        Prompt: "Review the code for issues",
                    }, nil
                },
                "review.result.v1",
                func(ctx *reactive.RuleContext, result any) error {
                    state := ctx.State.(*ReviewFixState)
                    reviewResult := result.(*ReviewResult)
                    state.IssuesFound = reviewResult.IssuesCount
                    state.ReviewFeedback = reviewResult.Feedback
                    if reviewResult.IssuesCount == 0 {
                        state.Phase = "completed"
                        state.Status = reactive.StatusCompleted
                    } else {
                        state.Phase = "fixing"
                    }
                    return nil
                },
            ).
            MustBuild()).

        // Rule 2: Fix issues if found
        AddRule(reactive.NewRule("fix-issues").
            WatchKV("REVIEW_FIX_STATE", "review-fix.*").
            When("phase is fixing", reactive.PhaseIs("fixing")).
            When("no pending task", reactive.NoPendingTask()).
            PublishAsync(
                "agent.task.fixer",
                func(ctx *reactive.RuleContext) (message.Payload, error) {
                    state := ctx.State.(*ReviewFixState)
                    return &FixRequest{
                        CodeID:   state.CodeID,
                        Feedback: state.ReviewFeedback,
                        Prompt:   "Fix the issues:\n\n" + state.ReviewFeedback,
                    }, nil
                },
                "fix.result.v1",
                func(ctx *reactive.RuleContext, result any) error {
                    state := ctx.State.(*ReviewFixState)
                    state.Phase = "reviewing"
                    state.Iteration++
                    return nil
                },
            ).
            MustBuild()).

        // Rule 3: Handle max iterations exceeded
        AddRule(reactive.NewRule("max-iterations-exceeded").
            WatchKV("REVIEW_FIX_STATE", "review-fix.*").
            When("phase is reviewing", reactive.PhaseIs("reviewing")).
            When("at max iterations", reactive.Not(reactive.IterationLessThan(maxIterations))).
            Mutate(func(ctx *reactive.RuleContext, _ any) error {
                state := ctx.State.(*ReviewFixState)
                state.Status = reactive.StatusEscalated
                state.Error = "max review iterations exceeded"
                return nil
            }).
            MustBuild()).

        MustBuild()
}

// ReviewFixState tracks review-fix cycle execution
type ReviewFixState struct {
    reactive.ExecutionState
    CodeID         string `json:"code_id"`
    IssuesFound    int    `json:"issues_found"`
    ReviewFeedback string `json:"review_feedback"`
}
```

This requires a workflow because:
- Multiple steps with ordering (review → fix → review)
- Loop that can repeat (fix → review → fix → review)
- Loop limit (max 3 iterations with type-safe iteration tracking)
- Workflow timeout (300 seconds total)
- State tracking (which phase, iteration count, issues found)

## Component Layer

Components execute single units of work. They are workflow-agnostic — a component doesn't know or
care whether it's being invoked by a rule, a workflow, or directly.

### What Components Do

- **Execute work**: Process messages, call LLMs, write files
- **Manage internal state**: Tool execution, context, pending operations
- **Report results**: Publish completion events with outcomes

### What Components Should NOT Do

- **Know about workflows**: Components are isolated units
- **Coordinate with other components**: That's the workflow's job
- **Enforce cross-step timeouts**: Only per-operation timeouts

### Example: agentic-loop Component

The agentic-loop component executes agent turns:

```text
Input: Task message on agent.task.*
Process: Model calls, tool execution, iteration
Output: Completion event on agent.complete.*
```

The component:
- Manages its own state machine (exploring → planning → executing → complete)
- Enforces per-loop iteration limits and timeouts
- Publishes results without knowing what triggered it or what comes next

## Rules of Thumb

### 1. Rules Trigger, They Don't Orchestrate

A rule should fire one action, not manage a sequence. If you find yourself writing rules that
depend on previous rule firings to build up state, you need a workflow.

**Anti-pattern**: Rule A sets `step=1`, Rule B watches for `step=1` and sets `step=2`...

**Correct**: Rule A triggers a workflow that manages step progression.

### 2. Workflows Coordinate, They Don't Execute

Workflows spawn components; they don't do the work themselves. If your workflow step is doing
complex processing inline, that logic belongs in a component.

**Anti-pattern**: Workflow step contains 100 lines of processing logic.

**Correct**: Workflow step publishes to a component that does the processing.

### 3. Components Are Workflow-Agnostic

A component doesn't know if it's in a workflow or standalone. It receives input, does work,
publishes output. This isolation enables reuse and testing.

**Anti-pattern**: Component checks `workflow_id` to decide behavior.

**Correct**: Component behaves the same regardless of caller.

### 4. State Ownership Is Exclusive

Only one layer owns any piece of state. If both rules and workflows are tracking the same state,
you have a design problem.

| State | Owner |
|-------|-------|
| Trigger conditions | Rules |
| Step progress, iteration count | Workflow |
| Execution state (pending tools, etc.) | Component |

### 5. If It Needs a Loop Limit, It's a Workflow

Simple handoffs (A completes → B starts) use rules. Cycles with termination conditions
(A → B → A → B... until X or N times) use workflows.

**Rule**: `when architect completes → spawn editor`

**Workflow**: `reviewer → fixer → reviewer → fixer... max 3 iterations`

## Use Case Examples

### Semspec-Driven Development

A semspec workflow involves multiple agent passes with iteration limits:

```text
1. Parse semspec for tasks (once)
2. For each task:
   a. Architect designs approach
   b. Editor implements
   c. Reviewer checks
   d. If issues: fix and re-review (max 3 times)
3. Report completion
```

**Layer mapping**:
- **Reactive Workflow**: Owns the multi-step process, loop limits, overall timeout (Go definition)
- **Rules**: Can trigger the workflow when semspec is approved
- **Components**: agentic-loop executes each agent role

### Simple Agent Handoff

Architect produces a plan, editor implements it (no retry loop):

```text
1. Architect completes with plan
2. Editor receives plan, implements
3. Done
```

**Layer mapping**:
- **Rules**: `when architect completes → publish_agent editor`
- **Components**: agentic-loop executes architect, then editor

No workflow needed — it's a simple A → B handoff without loops.

### Data Pipeline with Validation

Ingest data, validate, retry if malformed, fail after 3 attempts:

```text
1. Receive data
2. Validate format
3. If invalid and attempts < 3: request correction, goto 2
4. If invalid and attempts >= 3: fail with error
5. If valid: process and store
```

**Layer mapping**:
- **Reactive Workflow**: Owns validation loop, attempt counter, timeout (Go definition)
- **Rules**: Can trigger workflow on new data arrival
- **Components**: validator component, storage component

## Debugging Orchestration Issues

### Symptom: Action Fires Multiple Times Unexpectedly

**Likely cause**: Rule is re-triggering because state oscillates.

**Check**: Is the rule's action modifying state that causes the condition to re-match?

**Fix**: Use idempotent state updates or track "already processed" flags.

### Symptom: Workflow Never Completes

**Likely cause**: Missing termination condition or loop limit.

**Check**: Does every loop have a max_iterations? Does every branch eventually reach completion?

**Fix**: Add explicit loop limits and ensure all conditional branches terminate.

### Symptom: Component Behaves Differently in Workflow vs. Standalone

**Likely cause**: Component has workflow awareness it shouldn't.

**Check**: Is the component checking context about its caller?

**Fix**: Remove caller awareness; component behavior should be input-determined only.

## State Storage Boundaries

The system distinguishes between three categories of data with different storage patterns:

| Category | Storage | Rules Observable? | In Knowledge Graph? |
|----------|---------|-------------------|---------------------|
| **Domain Entities** | `ENTITY_STATES` KV | Yes | Yes (Graphable) |
| **Operational Results** | Component-specific KV | Yes | No |
| **Events** | JetStream streams | No | No |

### Domain Entities (ENTITY_STATES)

Semantic domain objects that implement the `Graphable` interface:
- Have 6-part hierarchical entity IDs (e.g., `acme.ops.robotics.gcs.drone.001`)
- Persist across multiple events
- Queryable in knowledge graph
- Examples: sensors, documents, zones, relationships

**Only `graph-ingest` writes to ENTITY_STATES.**

### Operational Results (Component KV)

Execution outcomes that are NOT semantic entities:
- Use `COMPLETE_{id}` key pattern for rules observability
- Stored in component-specific buckets:
  - `AGENT_LOOPS`: Agent completion state (`COMPLETE_{loopID}`)
  - `REACTIVE_WORKFLOW_STATE` (configurable): Workflow execution state managed by `pkg/workflow.StateManager`
- Transient — represent what happened, not what exists
- Examples: agent completion, workflow completion, step results

**Reactive workflows use configurable state buckets:**

The reactive workflow engine uses `pkg/workflow.StateManager` with configurable bucket names (default:
`REACTIVE_WORKFLOW_STATE`). Each workflow definition specifies its state bucket via `WithStateBucket()`.

**Rules can watch these buckets using `entity_watch_buckets` config:**

```json
{
  "entity_watch_buckets": {
    "ENTITY_STATES": ["telemetry.>"],
    "REACTIVE_WORKFLOW_STATE": ["review-fix.*"],
    "AGENT_LOOPS": ["COMPLETE_*"]
  }
}
```

### Events (JetStream)

Immediate notifications for downstream processing:
- Published to streams for subscribers
- Not directly observable by rules (rules watch KV, not streams)
- Examples: `agent.complete.*`, `workflow.events`

### Anti-Pattern: Mixing Categories

Do NOT write operational results to ENTITY_STATES:
- Pollutes knowledge graph with non-semantic data
- Breaks Graphable contract (no entity ID, no triples)
- Makes graph queries less meaningful

**Example anti-pattern**:
```go
// WRONG: Writing workflow completion to ENTITY_STATES
entityBucket.Put(ctx, "workflow.review.exec123", completionData)
```

**Correct pattern**:
```go
// RIGHT: Writing to component-specific bucket
// For reactive workflows, use StateManager
stateManager.UpdateState(ctx, "review-fix.123", func(state any) error {
    s := state.(*ReviewFixState)
    s.Status = reactive.StatusCompleted
    return nil
})

// For agent loops, use COMPLETE_ prefix
agentLoopsBucket.Put(ctx, "COMPLETE_exec123", completionData)
```

## References

- [Advanced Guide: Reactive Workflows](../advanced/10-reactive-workflows.md) — Usage guide and examples
- [Concept: Agentic Systems](./13-agentic-systems.md) — Agentic loop fundamentals
- [Concept: Rule-Driven Artifacts](./18-rule-driven-artifacts.md) — Pattern for emitting markdown / JSON / webhook artifacts from rule actions
