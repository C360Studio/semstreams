# context

Building blocks for context construction in agentic systems.

## Overview

The `context` package implements the "embed context, don't make agents discover it" pattern. It provides utilities for:

- **Token estimation** - Manage context budgets precisely
- **Batch graph queries** - Efficiently fetch entities and relationships
- **Context formatting** - Prepare content for LLM consumption
- **Source tracking** - Track provenance of context

SemStreams provides the HOW (building blocks), consumers decide the WHAT (relevance).

## Quick Start

```go
import "github.com/c360studio/semstreams/pkg/context"

// Query entities from the graph
result, err := context.BatchQueryEntitiesWithOptions(ctx, graphClient, entityIDs,
    context.BatchQueryOptions{
        IncludeRelationships: true,
        Depth:                1,
    })
if err != nil {
    return err
}

// Build constructed context with token tracking
opts := context.FormatOptions{
    MaxTokens:      8000,
    PrettyPrint:    true,
    SectionHeaders: true,
}
constructed, err := context.BuildContextFromBatch(result, opts)
if err != nil {
    return err
}

// Embed in TaskMessage - token count is exact
task.Context = constructed
fmt.Printf("Context uses %d tokens\n", constructed.TokenCount)
```

## API Reference

### Core Types

| Type | Description |
|------|-------------|
| `ConstructedContext` | Formatted context with token count and source tracking |
| `Source` | Tracks where context originated (entity, relationship, document) |
| `BatchQueryResult` | Results from batch entity queries |
| `BudgetAllocation` | Tracks token budget allocation across sections |
| `FormatOptions` | Configures context formatting |

### Token Estimation

| Function | Description |
|----------|-------------|
| `EstimateTokens(s string) int` | Estimate tokens (~4 chars/token) |
| `EstimateTokensForModel(s, model string) int` | Model-specific estimation |
| `FitsInBudget(content string, budget int) bool` | Check if content fits budget |
| `TruncateToBudget(content string, budget int) string` | Truncate at word boundaries |
| `CountWords(s string) int` | Count words in string |
| `TokensFromWords(wordCount int) int` | Estimate tokens from word count |

### Batch Graph Queries

| Function | Description |
|----------|-------------|
| `BatchQueryEntities(ctx, client, entityIDs)` | Batch lookup with defaults |
| `BatchQueryEntitiesWithOptions(ctx, client, entityIDs, opts)` | Configurable batch lookup |
| `ExpandWithNeighbors(ctx, client, entityIDs, depth)` | Expand to include N-hop neighbors |
| `CollectEntityIDs(relationships)` | Extract unique entity IDs |

### Context Formatting

| Function | Description |
|----------|-------------|
| `FormatEntitiesForContext(entities, opts)` | Format entities for LLM |
| `FormatRelationshipsForContext(relationships, opts)` | Format relationships for LLM |
| `FormatBatchResultForContext(result, opts)` | Format complete batch result |
| `BuildContextFromBatch(result, opts)` | Create ConstructedContext |

### Helper Functions

| Function | Description |
|----------|-------------|
| `NewConstructedContext(content, entities, sources)` | Create ConstructedContext |
| `EntitySource(entityID)` | Create entity source |
| `RelationshipSource(relationshipID)` | Create relationship source |
| `DocumentSource(docID)` | Create document source |
| `NewBudgetAllocation(totalBudget)` | Create budget tracker |

## Token Budget Management

The package provides tools for managing token budgets across context sections:

```go
// Allocate budget across sections
budget := context.NewBudgetAllocation(10000)
budget.Allocate("system_prompt", 500)
budget.Allocate("entities", 4000)
budget.Allocate("relationships", 2000)
remaining := budget.Remaining() // 3500 for conversation

// Or allocate proportionally
budget := context.NewBudgetAllocation(8000)
budget.Allocate("system_prompt", 500)
allocations := budget.AllocateProportionally(
    []string{"entities", "relationships", "history"},
    []float64{0.5, 0.2, 0.3},
)
```

## BatchQueryOptions

Configure batch queries with these options:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `IncludeRelationships` | bool | false | Fetch relationships for each entity |
| `Depth` | int | 0 | Relationship traversal depth |
| `MaxConcurrent` | int | 10 | Max concurrent relationship queries |

## FormatOptions

Configure formatting with these options:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `MaxTokens` | int | 4000 | Maximum tokens for output |
| `PrettyPrint` | bool | true | Pretty print JSON |
| `IncludeMetadata` | bool | false | Include entity metadata |
| `EntityOrder` | []string | nil | Explicit entity ordering |
| `SectionHeaders` | bool | true | Add section headers |

## Integration with Workflows

A Go application can attach a constructed result to `agentic.TaskMessage.Context` before dispatch.
The agent loop consumes nonempty `Context.Content` as supplied context. The application owns relevance,
query dependencies, and the choice to prepare fresh or shared context.

This package does not provide a workflow processor or step interpolation. In rule-driven applications,
rules trigger components; a component constructs context and uses the application's task-dispatch path.
`TaskMessage.Context` is not a `rule.Action` field.

Token counts from this package are estimates. They do not establish the exact size of a complete model request.
See [Context Construction](../../docs/concepts/22-context-construction.md) and
[Orchestration Layers](../../docs/concepts/14-orchestration-layers.md).

## Design Philosophy

This package follows the principle that "what's relevant" is domain knowledge:

- A code review system has different relevance criteria than a logistics system
- Rather than embedding domain-specific heuristics, SemStreams provides utilities
- The consumer (e.g., SemSpec) implements the relevance logic

**Pattern:**
```text
Consumer:
1. Analyze task to determine relevant entities (domain logic)
2. Use pkg/context to query and format entities (building blocks)
3. Embed ConstructedContext in TaskMessage (integration)

SemStreams:
4. Agent loop receives pre-built context
5. Further retrieval depends on the selected components and tools
6. Context size is estimated before dispatch
```

## Related Documentation

- [Agentic Systems](../../docs/concepts/13-agentic-systems.md) - Overview of agentic loop
- [Orchestration Layers](../../docs/concepts/14-orchestration-layers.md) - Rules and component composition
- [Context Construction](../../docs/concepts/22-context-construction.md) - Concept guide
