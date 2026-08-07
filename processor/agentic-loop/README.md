# agentic-loop

Loop orchestrator component for the agentic processing system.

## Overview

The `agentic-loop` component orchestrates autonomous agent execution by managing the lifecycle of agentic loops. It coordinates communication between the model processor (LLM calls) and tools processor (tool execution), tracks state through a 10-state machine, supports signal handling for user control, manages context memory with automatic compaction, and captures complete execution trajectories for observability.

## Architecture

```
                         ┌─────────────────┐
    agent.task.*    ────►│                 │────► agent.request.*
                         │  agentic-loop   │
    agent.response.>◄────│                 │◄──── (from model)
                         │                 │
    tool.result.>   ────►│                 │────► tool.execute.*
                         │                 │
    agent.signal.*  ────►│                 │────► agent.complete.*
                         │                 │
                         │                 │────► agent.context.compaction.*
                         └────────┬────────┘
                                  │
                         ┌────────┴────────┐
                         │    NATS KV      │
                         │  AGENT_LOOPS    │
                         │  AGENT_TRAJ...  │
                         └─────────────────┘
```

## Features

- **State Machine**: 10-state lifecycle with signal-related states
- **Signal Handling**: Cancel, pause, resume, and approval signals
- **Context Management**: Automatic compaction and GC for long-running loops
- **Tool Coordination**: Tracks pending tool calls, aggregates results
- **Trajectory Capture**: Records complete execution paths for debugging
- **Iteration Guards**: Configurable max iterations to prevent runaway loops
- **Architect/Editor Split**: Automatic spawning of editor from architect
- **Rules Integration**: Enriched completion events for rules-based orchestration

## Configuration

```json
{
  "type": "processor",
  "name": "agentic-loop",
  "enabled": true,
  "config": {
    "max_iterations": 20,
    "timeout": "120s",
    "stream_name": "AGENT",
    "loops_bucket": "AGENT_LOOPS",
    "trajectories_bucket": "AGENT_TRAJECTORIES",
    "context": {
      "enabled": true,
      "compact_threshold": 0.60,
      "headroom_tokens": 6400,
      "model_limits": {
        "gpt-4o": 128000,
        "gpt-4o-mini": 128000,
        "claude-sonnet": 200000,
        "claude-opus": 200000,
        "default": 128000
      }
    },
    "ports": {
      "inputs": [
        {"name":"agent.task","config":{"kind":"jetstream","subjects":["agent.task.*"],"stream_name":"AGENT"}},
        {"name":"agent.response","config":{"kind":"jetstream","subjects":["agent.response.>"],"stream_name":"AGENT"}},
        {"name":"tool.result","config":{"kind":"jetstream","subjects":["tool.result.>"],"stream_name":"AGENT"}},
        {"name":"agent.signal","config":{"kind":"jetstream","subjects":["agent.signal.*"],"stream_name":"AGENT"}}
      ],
      "outputs": [
        {"name":"agent.request","config":{"kind":"jetstream","subjects":["agent.request.*"],"stream_name":"AGENT"}},
        {"name":"tool.execute","config":{"kind":"jetstream","subjects":["tool.execute.*"],"stream_name":"AGENT"}},
        {"name":"agent.complete","config":{"kind":"jetstream","subjects":["agent.complete.*"],"stream_name":"AGENT"}},
        {"name":"agent.context.compaction","config":{"kind":"jetstream","subjects":["agent.context.compaction.*"],"stream_name":"AGENT"}}
      ]
    }
  }
}
```

### Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `max_iterations` | int | 20 | Maximum loop iterations before failure (1-1000) |
| `timeout` | string | "120s" | Loop execution timeout |
| `stream_name` | string | "AGENT" | JetStream stream name |
| `consumer_name_suffix` | string | "" | Suffix for consumer names (for testing) |
| `loops_bucket` | string | "AGENT_LOOPS" | KV bucket for loop state |
| `trajectories_bucket` | string | "AGENT_TRAJECTORIES" | KV bucket for trajectories |
| `context` | object | (defaults) | Context management configuration |
| `ports` | object | (defaults) | Port configuration |

### Context Configuration

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | bool | true | Enable context memory management |
| `compact_threshold` | float | 0.60 | Trigger compaction at this utilization (0.01-1.0) |
| `headroom_tokens` | int | 6400 | Reserve tokens for new content |
| `model_limits` | map | (defaults) | Token limits per model name |

## Ports

### Inputs

| Name | Type | Subject | Description |
|------|------|---------|-------------|
| agent.task | jetstream | agent.task.* | Task requests from external systems |
| agent.response | jetstream | agent.response.> | Model responses from agentic-model |
| tool.result | jetstream | tool.result.> | Tool results from agentic-tools |
| agent.signal | jetstream | agent.signal.* | Control signals (cancel, pause, resume) |

### Outputs

| Name | Type | Subject | Description |
|------|------|---------|-------------|
| agent.request | jetstream | agent.request.* | Model requests to agentic-model |
| tool.execute | jetstream | tool.execute.* | Tool calls to agentic-tools |
| agent.complete | jetstream | agent.complete.* | Loop completion events |
| agent.context.compaction | jetstream | agent.context.compaction.* | Context compaction events |

### KV Write

| Name | Bucket | Key Pattern | Description |
|------|--------|-------------|-------------|
| loops | AGENT_LOOPS | `{loop_id}` | Loop entity state |
| loops | AGENT_LOOPS | `COMPLETE_{loop_id}` | Completion state for rules engine |
| trajectories | AGENT_TRAJECTORIES | `{loop_id}` | Execution trajectories |

## State Machine

```
exploring → planning → architecting → executing → reviewing → complete
     ↑          ↑            ↑             ↑           ↑        ↘ failed
     └──────────┴────────────┴─────────────┴───────────┘         ↘ cancelled
                                                                  ↘ paused
                                                                   ↘ awaiting_approval
```

### States

| State | Terminal | Description |
|-------|----------|-------------|
| `exploring` | No | Initial state, gathering information |
| `planning` | No | Developing approach |
| `architecting` | No | Designing solution |
| `executing` | No | Implementing solution |
| `reviewing` | No | Validating results |
| `complete` | Yes | Successfully finished |
| `failed` | Yes | Failed due to error or max iterations |
| `cancelled` | Yes | Cancelled by user signal |
| `paused` | No | Paused by user signal, can resume |
| `awaiting_approval` | No | Waiting for user approval |

States are fluid checkpoints - loops can transition backward except from terminal states.

## Signal Handling

The loop accepts control signals via the `agent.signal.*` input port.

### Signal Message Format

```json
{
  "signal_id": "sig_abc123",
  "type": "cancel",
  "loop_id": "loop_456",
  "user_id": "user_789",
  "channel_type": "cli",
  "channel_id": "session_001",
  "payload": null,
  "timestamp": "2024-01-15T10:30:00Z"
}
```

### Signal Types

| Type | Description | Resulting State |
|------|-------------|-----------------|
| `cancel` | Stop execution immediately | `cancelled` |
| `pause` | Pause at next checkpoint | `paused` |
| `resume` | Continue paused loop | (previous state) |
| `approve` | Approve pending result | `complete` |
| `reject` | Reject with optional reason | `failed` |
| `feedback` | Add feedback without decision | (no change) |
| `retry` | Retry failed loop | `exploring` |

## Context Management

The loop includes automatic context memory management to handle long-running conversations.

### Context Regions

Messages are organized into priority regions (lower priority evicted first):

1. **tool_results** (priority 1) - Tool execution results, GC'd by age
2. **recent_history** (priority 2) - Recent conversation messages
3. **hydrated_context** (priority 3) - Retrieved context from memory
4. **compacted_history** (priority 4) - Summarized old conversation
5. **system_prompt** (priority 5) - Never evicted

### Context Events

Published to `agent.context.compaction.*`:

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

## KV Storage

### AGENT_LOOPS

Stores `LoopEntity` as JSON:

```json
{
  "id": "loop_123",
  "task_id": "task_456",
  "state": "executing",
  "role": "general",
  "model": "gpt-4",
  "iterations": 3,
  "max_iterations": 20,
  "started_at": "2024-01-15T10:30:00Z",
  "timeout_at": "2024-01-15T10:32:00Z",
  "parent_loop_id": "",
  "pause_requested": false,
  "pause_requested_by": "",
  "state_before_pause": "",
  "cancelled_by": "",
  "cancelled_at": null,
  "user_id": "user_789",
  "channel_type": "cli",
  "channel_id": "session_001"
}
```

### COMPLETE_{loopID}

Written when a loop completes, for rules engine consumption:

```json
{
  "loop_id": "loop_123",
  "task_id": "task_456",
  "outcome": "success",
  "role": "architect",
  "result": "Designed authentication system with JWT...",
  "model": "gpt-4",
  "iterations": 3,
  "parent_loop": ""
}
```

### AGENT_TRAJECTORIES

Stores `Trajectory` as JSON:

```json
{
  "loop_id": "loop_123",
  "start_time": "2024-01-15T10:30:00Z",
  "end_time": "2024-01-15T10:31:45Z",
  "steps": [...],
  "outcome": "complete",
  "total_tokens_in": 1500,
  "total_tokens_out": 800,
  "duration": 105000
}
```

## Message Formats

### TaskMessage (Input)

```json
{
  "loop_id": "optional-custom-id",
  "task_id": "task_123",
  "role": "general",
  "model": "gpt-4",
  "prompt": "Analyze this code for bugs"
}
```

### Completion Event (Output)

```json
{
  "loop_id": "loop_123",
  "task_id": "task_456",
  "outcome": "success",
  "role": "architect",
  "result": "Designed authentication system...",
  "model": "gpt-4",
  "iterations": 3,
  "parent_loop": ""
}
```

## Rules/Workflow Integration

The loop integrates with the rules engine for orchestration:

1. On completion, writes `COMPLETE_{loopID}` key to KV
2. Rules engine watches `COMPLETE_*` keys
3. Rules can trigger follow-up actions (e.g., spawn editor when architect completes)

### Architect/Editor Pattern

```
1. Task arrives with role="architect"
2. Architect loop executes and produces a plan
3. On completion, COMPLETE_{loopID} written with role="architect"
4. Rule matches COMPLETE_* where role="architect"
5. Rule spawns new loop with role="editor", parent_loop={loopID}
6. Editor receives architect's output as context
```

## Context Event Consumers

The loop publishes context (compaction lifecycle) events onto the AGENT stream
for observability. The OTel span collector (`output/otel`) consumes them via its
`agent.>` subscription and records each as a span event on the active loop span:

- `compaction_starting` - compaction about to run
- `compaction_complete` - compaction finished (tokens saved recorded)

## Troubleshooting

### Loop stuck waiting for response

- Check that agentic-model is running and subscribed
- Verify AGENT stream exists with correct subjects
- Check model endpoint is accessible

### Max iterations reached

- Increase `max_iterations` for complex tasks
- Check if agent is stuck in tool call loop
- Review trajectory for repeated patterns

### Missing tool results

- Verify agentic-tools is running
- Check tool executor is registered
- Ensure tool name matches exactly

### Context compaction issues

- Check `compact_threshold` is appropriate for workload
- Verify model registry has a summarization-capable endpoint or a large-context model
- Review `model_limits` for your model

### Signal not processed

- Verify signal published to correct subject: `agent.signal.{loop_id}`
- Check loop is not in terminal state (complete/failed/cancelled)
- Ensure signal message format is correct

## Related Components

- [agentic-model](../agentic-model/) - LLM endpoint integration
- [agentic-tools](../agentic-tools/) - Tool execution
- [agentic-dispatch](../agentic-dispatch/) - User message routing
- [workflow](../workflow/) - Multi-step orchestration
- [agentic types](../../agentic/) - Shared type definitions
