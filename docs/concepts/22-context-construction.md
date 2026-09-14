# Context Construction

Building focused context for agentic tasks using SemStreams building blocks.

## The Problem

Traditional agentic systems suffer from context problems:

1. **Discovery overhead** - Agents spend tokens figuring out what they need
2. **Unknown budgets** - Token count isn't known until runtime
3. **Context pollution** - Accumulated conversation dilutes relevant information
4. **Redundant queries** - Multiple agents rediscover the same entities

Consider a code review workflow with 3 parallel reviewers. Each agent:

- Queries the graph for relevant entities
- Discovers relationships
- Builds its own context

This wastes tokens on discovery, creates inconsistent contexts, and makes token budgets unpredictable.

## The Pattern: Embed Context, Don't Discover It

The solution is to construct context **before** dispatching agents:

```text
┌─────────────────┐      ┌───────────────────┐      ┌─────────────────┐
│   Task Arrives  │─────▶│ Build Context     │─────▶│ Dispatch Agent  │
│                 │      │ (consumer logic)  │      │ (with context)  │
└─────────────────┘      └───────────────────┘      └─────────────────┘
                                │
                                │ Uses pkg/context utilities:
                                │ - BatchQueryEntities
                                │ - FormatEntitiesForContext
                                │ - EstimateTokens
                                │
                                ▼
                         ┌──────────────────┐
                         │ ConstructedContext │
                         │ - Content (string) │
                         │ - TokenCount (int) │
                         │ - Entities (IDs)   │
                         │ - Sources (trace)  │
                         └──────────────────┘
```

Benefits:

- **Estimated token budgets** - Estimate context size before dispatch
- **Fresh context per task** - No pollution from prior work
- **Source tracking** - Trace which entities informed decisions
- **Consistent context** - Multiple agents can share the same base context

## Building Blocks

SemStreams provides utilities in `pkg/context/` that any consumer can use.

### Token Estimation

Estimate context size to manage token budgets:

```go
import "github.com/c360studio/semstreams/pkg/context"

// Check if content fits
if context.FitsInBudget(content, 8000) {
    // Use as-is
}

// Truncate to fit
truncated := context.TruncateToBudget(content, 8000)

// Estimate tokens
tokens := context.EstimateTokens(content)
```

### Budget Allocation

Distribute tokens across sections:

```go
budget := context.NewBudgetAllocation(10000)
budget.Allocate("system_prompt", 500)
budget.Allocate("entities", 4000)
budget.Allocate("relationships", 2000)
remaining := budget.Remaining() // 3500 for conversation
```

### Batch Graph Queries

Efficiently fetch entities and relationships:

```go
result, err := context.BatchQueryEntitiesWithOptions(ctx, graphClient, entityIDs,
    context.BatchQueryOptions{
        IncludeRelationships: true,
        Depth:                1,
        MaxConcurrent:        10,
    })
// result.Entities: map[string]json.RawMessage
// result.Relationships: []Relationship
// result.NotFound: []string
```

### Context Formatting

Prepare content for LLM consumption:

```go
opts := context.FormatOptions{
    MaxTokens:      8000,
    PrettyPrint:    true,
    SectionHeaders: true,
}

// Format entities
content, tokens, err := context.FormatEntitiesForContext(entities, opts)

// Or build full ConstructedContext
constructed, err := context.BuildContextFromBatch(result, opts)
```

## ConstructedContext

The `ConstructedContext` type wraps everything needed for embedded context:

```go
type ConstructedContext struct {
    Content       string          // Formatted string for LLM
    TokenCount    int             // Estimated token count
    Entities      []string        // Entity IDs included
    Sources       []ContextSource // Provenance tracking
    ConstructedAt time.Time       // For cache management
}
```

Source types track where context came from:

- `graph_entity` - From a knowledge graph entity
- `graph_relationship` - From graph relationships
- `document` - From a document or chunk

## Consumer Pattern

SemStreams provides the HOW (building blocks). Consumers decide the WHAT (relevance).

**Why this separation?**

"What's relevant" is domain knowledge:

- Code review system: recent commits, related files, SOPs
- Logistics system: current missions, nearby assets, schedules
- Healthcare system: patient history, protocols, medications

A framework shouldn't embed domain-specific heuristics.

**Consumer implementation:**

```text
SemSpec flow:
1. Task arrives: "Review authentication changes"
2. SemSpec's context planner analyzes task (domain logic)
3. Determines: "I need auth-related entities, recent commits, SOPs"
4. Uses pkg/context utilities to fetch and format
5. Embeds ConstructedContext in TaskMessage
6. Dispatches agent with pre-built context
```

## Integration with Workflows

Context construction belongs to the application component that understands the task. It selects relevant
entities and bodies, retrieves them through composed query dependencies, and formats the result before dispatch.

For a Go producer constructing an `agentic.TaskMessage`, the `Context` field accepts a `ConstructedContext`.
The agent loop consumes its nonempty `Content` as supplied context. This is an explicit task-construction seam;
it is not a workflow-step interpreter.

For rule-driven composition, a rule can trigger the component responsible for preparing and dispatching work.
`TaskMessage.Context` is not a `rule.Action` field. Use the current
[action definition](../../processor/rule/actions.go), and keep full content with its producer when coordination
can carry a reference.

When several agents need different evidence, the application chooses separate contexts or a shared body.
That choice does not determine execution concurrency or aggregation behavior.

Token utilities use estimates. An estimated context budget does not establish the exact count of the final model
request, which also contains prompts, tool definitions and other messages.
See [Orchestration Layers](14-orchestration-layers.md) for current composition patterns and
[Building context with SemSource](../basics/09-building-semsource.md) for a source-backed application.

## Example: Building Context for Code Review

An application selects relevant file IDs, retrieves entities through its composed query adapter, and formats the
result within an estimated budget. Use the maintained [batch query helpers](../../pkg/context/batch.go) and
[formatting implementation](../../pkg/context/format.go) as API references, then attach the constructed result
through the explicit task seam above. The application determines which evidence matters and how to handle a
query or formatting failure.

## Related Documentation

- [Agentic Systems](13-agentic-systems.md) - Overview of agentic loop
- [Orchestration Layers](14-orchestration-layers.md) - Rules, components and per-item dispatch
- [pkg/context README](../../pkg/context/README.md) - Package documentation
