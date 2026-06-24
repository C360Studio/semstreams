# agentic-memory

Graph-backed agent memory component for the agentic processing system.

## Overview

The `agentic-memory` component bridges the agentic loop system with the knowledge graph, providing persistent memory capabilities for agents. It responds to context events from agentic-loop, extracts facts for long-term storage in the graph, and hydrates context when needed for context recovery or pre-task preparation.

## Architecture

```
┌────────────────┐     ┌─────────────────┐     ┌──────────────┐
│  agentic-loop  │────►│ agentic-memory  │────►│   Graph      │
│                │     │                 │     │  Processor   │
│                │◀────│                 │     │              │
└────────────────┘     └─────────────────┘     └──────────────┘
  context.compaction.*   graph.mutation.*
  hydrate.request.*      context.injected.*
```

## Features

- **Context Hydration**: Retrieve relevant context from knowledge graph
- **Fact Extraction**: LLM-assisted extraction of facts from conversations
- **Memory Checkpointing**: Persist memory state for recovery
- **Event-Driven**: Responds to context events from agentic-loop

## Configuration

```json
{
  "type": "processor",
  "name": "agentic-memory",
  "enabled": true,
  "config": {
    "extraction": {
      "llm_assisted": {
        "enabled": false,
        "model": "fast",
        "trigger_iteration_interval": 5,
        "trigger_context_threshold": 0.8,
        "max_tokens": 1000
      }
    },
    "hydration": {
      "pre_task": {
        "enabled": true,
        "max_context_tokens": 2000,
        "include_decisions": true,
        "include_files": true
      },
      "post_compaction": {
        "enabled": true,
        "reconstruct_from_checkpoint": true,
        "max_recovery_tokens": 1500
      }
    },
    "checkpoint": {
      "enabled": true,
      "storage_bucket": "AGENT_MEMORY_CHECKPOINTS",
      "retention_days": 7
    },
    "stream_name": "AGENT"
  }
}
```

### Configuration Options

#### Extraction Configuration

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `llm_assisted.enabled` | bool | false | Enable LLM-assisted fact extraction. **Dormant until wired (gh#317 / ADR-059)** — no LLM client is injected yet, so setting this `true` fails component construction rather than silently producing zero triples. |
| `llm_assisted.model` | string | "fast" | Model alias for extraction |
| `llm_assisted.trigger_iteration_interval` | int | 5 | Extract every N iterations |
| `llm_assisted.trigger_context_threshold` | float | 0.8 | Extract at N% utilization |
| `llm_assisted.max_tokens` | int | 1000 | Max tokens for extraction |

#### Hydration Configuration

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `pre_task.enabled` | bool | true | Enable pre-task hydration |
| `pre_task.max_context_tokens` | int | 2000 | Max tokens for pre-task context |
| `pre_task.include_decisions` | bool | true | Include past decisions |
| `pre_task.include_files` | bool | true | Include file context |
| `post_compaction.enabled` | bool | true | Enable post-compaction recovery |
| `post_compaction.reconstruct_from_checkpoint` | bool | true | Use checkpoints |
| `post_compaction.max_recovery_tokens` | int | 1500 | Max tokens for recovery |

#### Checkpoint Configuration

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | bool | true | Enable checkpointing |
| `storage_bucket` | string | "AGENT_MEMORY_CHECKPOINTS" | KV bucket name |
| `retention_days` | int | 7 | Days to retain checkpoints |

## Ports

### Inputs

| Name | Type | Subject/Bucket | Description |
|------|------|----------------|-------------|
| compaction_events | jetstream | agent.context.compaction.> | Context events from agentic-loop |
| hydrate_requests | jetstream | memory.hydrate.request.* | Explicit hydration requests |
| entity_states | kv-watch | ENTITY_STATES | Entity state changes |

### Outputs

| Name | Type | Subject | Description |
|------|------|---------|-------------|
| injected_context | jetstream | agent.context.injected.* | Hydrated context |
| graph_mutations | nats | graph.mutation.* | Fact triples for graph |
| checkpoint_events | nats | memory.checkpoint.created.* | Checkpoint notifications |

## Memory Operations

### Context Hydration

Retrieves relevant context from the knowledge graph:

**Pre-Task Hydration**: Injects context before a task starts

```json
{
  "loop_id": "loop_123",
  "type": "pre_task",
  "task_description": "Implement user authentication"
}
```

**Post-Compaction Hydration**: Recovers context after compaction

```json
{
  "loop_id": "loop_123",
  "type": "post_compaction"
}
```

### Fact Extraction

Extracts structured facts from conversation content using LLM:

- Triggered by `compaction_starting` events
- Extracts subject-predicate-object triples
- Publishes to graph for persistent storage

### Memory Checkpointing

Creates snapshots of memory state:

- Triggered on significant events
- Stored in NATS KV with configurable TTL
- Enables recovery of context state

## Message Formats

### Context Event (Input)

From agentic-loop context management:

```json
{
  "type": "compaction_starting",
  "loop_id": "loop_123",
  "iteration": 5,
  "utilization": 0.65
}
```

```json
{
  "type": "compaction_complete",
  "loop_id": "loop_123",
  "iteration": 5,
  "tokens_saved": 2500,
  "summary": "Discussed authentication implementation..."
}
```

### Hydrate Request (Input)

Explicit hydration request:

```json
{
  "loop_id": "loop_123",
  "type": "pre_task",
  "task_description": "Implement user authentication"
}
```

### Injected Context (Output)

Published to agent.context.injected.*:

```json
{
  "loop_id": "loop_123",
  "type": "post_compaction",
  "context": "## Previous Decisions\n- Use JWT for auth...",
  "token_count": 850
}
```

### Graph Mutation (Output)

Published to graph.mutation.*:

```json
{
  "loop_id": "loop_123",
  "operation": "add_triples",
  "triples": [
    {
      "subject": "loop:loop_123",
      "predicate": "discussed",
      "object": "jwt_authentication"
    }
  ]
}
```

## Memory Lifecycle

A typical memory lifecycle for an agentic loop:

```
1. Loop starts
   └─► Pre-task hydration injects relevant context from graph

2. Loop executes iterations
   └─► Context grows with each model/tool interaction

3. Context approaches token limit
   └─► agentic-loop publishes compaction_starting
   └─► agentic-memory extracts key facts

4. agentic-loop compresses context
   └─► Publishes compaction_complete with summary
   └─► agentic-memory hydrates recovered context

5. Loop continues with refreshed context
   └─► Repeat steps 2-4 as needed

6. Loop completes
   └─► Final checkpoint created for future reference
```

## Integration with agentic-loop

agentic-memory integrates with agentic-loop through context events:

1. agentic-loop publishes to `agent.context.compaction.*`
2. agentic-memory consumes events, extracts facts, hydrates context
3. agentic-memory publishes to `agent.context.injected.*`
4. agentic-loop can consume injected context for enhancement

## Troubleshooting

### No context hydrated

- Check that graph processor is running
- Verify graph contains relevant data
- Check `max_context_tokens` / `max_recovery_tokens` limits

### Extraction not triggering

- **LLM-assisted extraction is dormant** (gh#317 / ADR-059): no LLM client is wired
  yet, so `extraction.llm_assisted.enabled=true` fails component construction rather
  than producing triples. It will be activated via the claim surface in ADR-059.
- Check `trigger_iteration_interval` and `trigger_context_threshold`
- Ensure agentic-model is running with the configured model alias

### Missing compaction events

- Verify agentic-loop is publishing to `agent.context.compaction.*`
- Check AGENT stream exists and is accessible
- Verify consumer is subscribed correctly

### Checkpoint failures

- Check `storage_bucket` exists
- Verify retention policy allows writes
- Check KV bucket size limits

## Related Components

- [agentic-loop](../agentic-loop/) - Loop orchestration (publishes context events)
- [agentic-model](../agentic-model/) - LLM endpoint integration (for extraction)
- [agentic-dispatch](../agentic-dispatch/) - User message routing
- [agentic types](../../agentic/) - Shared type definitions
