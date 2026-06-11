# Reactive Workflows

> **Legacy reference.** This page describes the retired
> `processor/reactive` workflow engine. Current SemStreams orchestration uses
> coordinated rules over lifecycle-managed graph entities; see
> [Orchestration Layers](../concepts/14-orchestration-layers.md) and
> [ADR-049](../adr/049-lifecycle-harness-prime-schema-over-entity-states.md).

The reactive workflow engine enables multi-step coordination using typed Go functions instead of JSON configuration with string interpolation. Workflows are defined in Go code, providing compile-time type safety and eliminating serialization bugs.

## What Reactive Workflows Do

Reactive workflows:
- Coordinate multi-step processes with typed state
- React to KV state changes and NATS messages
- Use Go functions for conditions, payloads, and state mutations
- Enforce loop limits and timeouts
- Track execution progress with iteration counters

## Quick Example

A simple linear workflow (pending → processing → completed):

```go
func buildLinearWorkflow() *reactive.Definition {
    return reactive.NewWorkflow("linear-example").
        WithDescription("Simple linear workflow").
        WithStateBucket("EXAMPLE_STATE").
        WithStateFactory(func() any { return &LinearWorkflowState{} }).
        WithTimeout(5 * time.Minute).
        AddRule(reactive.NewRule("start-processing").
            WatchKV("EXAMPLE_STATE", "linear-example.*").
            When("phase is pending", reactive.PhaseIs("pending")).
            When("no pending task", reactive.NoPendingTask()).
            PublishAsync(
                "processor.input",
                func(ctx *reactive.RuleContext) (message.Payload, error) {
                    state := ctx.State.(*LinearWorkflowState)
                    return &ProcessRequest{
                        TaskID: state.ID,
                        Input:  state.Input,
                    }, nil
                },
                "example.process-result.v1",
                func(ctx *reactive.RuleContext, result any) error {
                    state := ctx.State.(*LinearWorkflowState)
                    if res, ok := result.(*ProcessResult); ok {
                        state.Output = res.Computed
                    }
                    state.Phase = "completed"
                    state.Status = reactive.StatusCompleted
                    return nil
                },
            ).
            MustBuild()).
        MustBuild()
}
```

## Trigger Modes

Reactive workflows support three trigger modes:

| Mode | Description | Use Case |
|------|-------------|----------|
| **KV Watch** | React to state changes in KV | Inter-rule coordination |
| **Subject Consumer** | React to NATS messages | Entry points, callbacks |
| **Combined** | Message + state condition | Async callback with context |

### KV Watch Trigger

```go
reactive.NewRule("on-state-change").
    WatchKV("WORKFLOW_STATE", "workflow.*").
    When("phase is processing", reactive.PhaseIs("processing")).
    // ...
```

### Subject Trigger

```go
reactive.NewRule("on-message").
    OnSubject("workflow.trigger.>", func() any { return &TriggerMessage{} }).
    // ...
```

### Combined Trigger

```go
reactive.NewRule("on-callback").
    OnSubject("workflow.callback.>", func() any { return &CallbackResult{} }).
    WithStateLookup("WORKFLOW_STATE", func(msg any) string {
        return msg.(*CallbackResult).ExecutionID
    }).
    When("phase is awaiting", reactive.PhaseIs("awaiting")).
    // ...
```

## Condition Helpers

Built-in condition functions for common checks:

```go
// Phase and status checks
reactive.PhaseIs("pending")
reactive.StatusIs(reactive.StatusRunning)

// Iteration limits
reactive.IterationLessThan(3)

// Task state
reactive.NoPendingTask()
reactive.HasPendingTask()

// Field equality (generic)
reactive.StateFieldEquals(
    func(s any) string { return s.(*MyState).Verdict },
    "approved",
)

// Combinators
reactive.And(condA, condB)
reactive.Or(condA, condB)
reactive.Not(cond)
```

## Action Types

| Action | Description | Usage |
|--------|-------------|-------|
| `PublishAsync` | Publish request, await callback | External service calls |
| `Publish` | Fire-and-forget publish | Notifications |
| `Mutate` | Update state only | Phase transitions |
| `Complete` | Mark execution complete | Terminal states |
| `CompleteWithMutation` | Mutate then complete | Final state update |

### PublishAsync Example

```go
reactive.NewRule("call-reviewer").
    PublishAsync(
        "reviewer.input",  // Subject to publish to
        buildPayload,      // PayloadBuilderFunc
        "review.result.v1", // Expected result type
        handleResult,      // StateMutatorFunc
    )
```

### Mutate Example

```go
reactive.NewRule("transition-phase").
    Mutate(reactive.ChainMutators(
        reactive.IncrementIterationMutator(),
        reactive.PhaseTransition("next-phase"),
    ))
```

## Loop Patterns

For iterative workflows (review → fix → review...), use iteration tracking:

```go
func buildReviewLoop() *reactive.Definition {
    const maxIterations = 3

    return reactive.NewWorkflow("review-loop").
        WithMaxIterations(maxIterations).
        AddRule(reactive.NewRule("request-review").
            When("under max iterations", reactive.IterationLessThan(maxIterations)).
            PublishAsync(/* ... */)).
        AddRule(reactive.NewRule("handle-needs-work").
            When("verdict is needs_work", reactive.StateFieldEquals(
                func(s any) string { return s.(*ReviewState).Verdict },
                "needs_work",
            )).
            When("under max iterations", reactive.IterationLessThan(maxIterations)).
            Mutate(reactive.ChainMutators(
                reactive.IncrementIterationMutator(),
                reactive.PhaseTransition("reviewing"),
            ))).
        AddRule(reactive.NewRule("handle-max-iterations").
            When("verdict is needs_work", /* ... */).
            When("at max iterations", reactive.Not(reactive.IterationLessThan(maxIterations))).
            Mutate(func(ctx *reactive.RuleContext, _ any) error {
                state := ctx.State.(*ReviewState)
                state.Status = reactive.StatusEscalated
                state.Error = "max iterations exceeded"
                return nil
            })).
        MustBuild()
}
```

## State Types

Workflow states embed `reactive.ExecutionState` and implement `StateAccessor`:

```go
type MyWorkflowState struct {
    reactive.ExecutionState          // ID, Phase, Status, Iteration, etc.
    Input  string `json:"input"`    // Custom fields
    Output string `json:"output,omitempty"`
}

// GetExecutionState implements reactive.StateAccessor to avoid reflection.
func (s *MyWorkflowState) GetExecutionState() *reactive.ExecutionState {
    return &s.ExecutionState
}
```

Always implement `GetExecutionState()` for state types. Without it, the engine falls back to reflection on every state access.

### ExecutionState Fields

| Field | Type | Description |
|-------|------|-------------|
| `ID` | `string` | Execution identifier |
| `WorkflowID` | `string` | Workflow definition ID |
| `Phase` | `string` | Current execution phase |
| `Status` | `ExecutionStatus` | Running, Completed, Failed, Escalated |
| `Iteration` | `int` | Loop counter |
| `PendingTaskID` | `string` | Active async task (if any) |
| `Error` | `string` | Error message (if failed) |
| `CreatedAt` | `time.Time` | Start time |
| `UpdatedAt` | `time.Time` | Last update time |
| `Deadline` | `*time.Time` | Timeout deadline |

## Testing

Use the testutil package for unit tests without NATS:

```go
func TestMyWorkflow(t *testing.T) {
    engine := testutil.NewTestEngine(t)
    def := buildMyWorkflow()

    if err := engine.RegisterWorkflow(def); err != nil {
        t.Fatalf("RegisterWorkflow failed: %v", err)
    }

    // Create initial state
    state := &MyWorkflowState{
        ExecutionState: reactive.ExecutionState{
            ID:         "exec-001",
            WorkflowID: "my-workflow",
            Phase:      "pending",
            Status:     reactive.StatusRunning,
        },
        Input: "test input",
    }

    // Trigger by storing state in KV
    key := "my-workflow.exec-001"
    err := engine.TriggerKV(context.Background(), key, state)
    if err != nil {
        t.Fatalf("TriggerKV failed: %v", err)
    }

    // Assert state
    engine.AssertPhase(key, "pending")
    engine.AssertStatus(key, reactive.StatusRunning)
}
```

### Test Helpers

```go
// State assertions
engine.AssertPhase(key, "completed")
engine.AssertStatus(key, reactive.StatusCompleted)
engine.AssertIteration(key, 3)

// Wait for async
engine.WaitForPhase(key, "completed", 5*time.Second)
engine.WaitForStatus(key, reactive.StatusCompleted, 5*time.Second)

// Message assertions
engine.AssertPublished("processor.input")
engine.AssertPublishedCount("processor.input", 1)
engine.AssertNoPublished("error.>")

// Custom assertions
engine.AssertStateAs(key, &MyWorkflowState{}, func(t *testing.T, state any) {
    s := state.(*MyWorkflowState)
    if s.Output != "expected" {
        t.Errorf("unexpected output: %s", s.Output)
    }
})
```

## Architecture

```
Engine
├── TriggerLoops
│   ├── KVWatchLoop (per bucket/pattern)
│   └── SubjectConsumerLoop (per subject)
│
├── Evaluator
│   ├── BuildRuleContext (typed state + message)
│   └── EvaluateConditions (AND logic)
│
├── Dispatcher
│   ├── PublishAsync (with callback tracking)
│   ├── Publish (fire-and-forget)
│   ├── Mutate (KV update only)
│   └── Complete (terminal state)
│
├── CallbackHandler
│   ├── TaskID correlation
│   ├── Typed result deserialization
│   └── StateMutator invocation
│
└── Store
    ├── ExecutionState persistence
    ├── Optimistic concurrency (KV revision)
    └── Timeline tracking
```

## Comparison with JSON Workflows

| Aspect | JSON Workflows | Reactive Workflows |
|--------|---------------|-------------------|
| Type safety | Runtime (payload registry) | Compile-time (Go types) |
| Field references | String interpolation | Go field access |
| Conditions | JSON expressions | Go functions |
| Serialization | 9+ boundaries | 2 boundaries |
| Error detection | Load/runtime | Compile time |
| Debugging | String inspection | Go debugger |

## Working Examples

See `/cmd/e2e-semstreams/workflows.go` for production-ready workflow examples including:

- **Cold Storage Alert**: Multi-condition monitoring (temperature + zone checks)
- **High Humidity Alert**: Threshold-based alerting with sensor type filtering
- **Low Pressure Alert**: Critical system monitoring with cooldown periods
- **Notify Technician**: Rule-triggered workflow demonstrating rule→workflow integration

These examples show real-world patterns for:

- KV watch triggers on entity states
- JetStream subject consumers for workflow triggers
- Composite conditions with property lookups
- Publish actions with typed payload builders
- Cooldown configuration to prevent alert flooding
- Complete actions for terminal workflow states

## Detailed Reference

- [processor/reactive/](../../processor/reactive/) - Package source code
- [cmd/e2e-semstreams/workflows.go](../../cmd/e2e-semstreams/workflows.go) - Working examples
