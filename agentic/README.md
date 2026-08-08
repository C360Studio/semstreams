# agentic

Shared types for the SemStreams agentic processing system.

## Overview

The `agentic` package defines foundational types used by the agentic component family:
- **agentic-loop**: Orchestrates agent lifecycle and state machine
- **agentic-model**: Integrates with LLM endpoints
- **agentic-tools**: Executes tool calls

These components communicate over NATS JetStream using the types defined here.

## Architecture

```
┌─────────────────┐
│  agentic-loop   │  Orchestrates the agent lifecycle
│  (state machine)│  Manages state, routes messages, captures trajectory
└────────┬────────┘
         │
    ┌────┴────┐
    │         │
    ▼         ▼
┌────────┐  ┌────────────┐
│agentic-│  │agentic-    │
│model   │  │tools       │
│(LLM)   │  │(execution) │
└────────┘  └────────────┘
```

## Type Categories

### Request/Response Types

| Type | Description |
|------|-------------|
| `AgentRequest` | Request to LLM endpoint with messages and tools |
| `AgentResponse` | LLM response with content, tool calls, or error |
| `ChatMessage` | Message in conversation (system/user/assistant/tool) |
| `TokenUsage` | Token consumption tracking |
| `ModelConfig` | LLM parameters (temperature, max_tokens) |

### State Machine Types

| Type | Description |
|------|-------------|
| `LoopState` | Loop lifecycle state (exploring, planning, executing, etc.) |
| `LoopEntity` | Complete loop instance with state, iterations, timeouts |

### Tool System Types

| Type | Description |
|------|-------------|
| `ToolDefinition` | Tool schema (name, description, parameters) |
| `ToolCall` | Request to execute a tool |
| `ToolResult` | Tool execution outcome (content, error, or metadata) |

### Trajectory Types

| Type | Description |
|------|-------------|
| `TrajectoryFactV1` | Bounded immutable observation of one loop attempt |
| `TrajectoryEvidenceV1` | Full evidence body stored by digest through a registered Store |
| `TrajectoryPage` | Cursor-paged, observed-only fact metadata and evidence references |
| `Trajectory` / `TrajectoryStep` | Transient active-loop execution helpers; never durable read authority |

### User Interaction Types

| Type | Description |
|------|-------------|
| `UserMessage` | Normalized input from any channel |
| `UserSignal` | Control signal (cancel, pause, resume, approve) |
| `UserResponse` | Response sent back to user |
| `TaskMessage` | Task to execute by agentic loop |
| `Attachment` | File or media attached to a message |
| `ResponseBlock` | Block of content in a rich response (text, code, diff) |
| `ResponseAction` | Interactive action in a response (button, reaction) |

## Usage

### Creating an Agent Request

```go
request := agentic.AgentRequest{
    RequestID: "req_001",
    LoopID:    "loop_123",
    Role:      "general",
    Model:     "gpt-4",
    Messages: []agentic.ChatMessage{
        {Role: "system", Content: "You are a helpful assistant."},
        {Role: "user", Content: "Analyze this code for bugs."},
    },
    Tools: []agentic.ToolDefinition{
        {Name: "read_file", Description: "Read file contents", Parameters: schema},
    },
}

if err := request.Validate(); err != nil {
    // Handle validation error
}
```

### Managing Loop State

```go
// Create with default max iterations (20)
entity := agentic.NewLoopEntity("loop_123", "task_456", "general", "gpt-4")

// Or with custom max iterations
entity := agentic.NewLoopEntity("loop_123", "task_456", "general", "gpt-4", 50)

// State transitions
entity.TransitionTo(agentic.LoopStatePlanning)
entity.TransitionTo(agentic.LoopStateExecuting)

// Iteration tracking
if err := entity.IncrementIteration(); err != nil {
    // Max iterations reached
}

// Check terminal state
if entity.State.IsTerminal() {
    // Loop has finished
}
```

### Reading trajectory observations

Agentic-loop records facts automatically. Public callers query the GraphQL trajectory field and receive strict,
cursor-paged metadata plus `StorageReference` values with `coverage: observed`. The read never hydrates evidence or
claims completeness. Authorized internal consumers retrieve a referenced body through the registered Store named by
that reference; they do not read an aggregate trajectory from KV or process memory.

### Validating Tool Calls Against Allowlist

```go
allowed := []string{"read_file", "write_file", "list_dir"}
calls := []agentic.ToolCall{{ID: "1", Name: "delete_file"}}

if err := agentic.ValidateToolsAllowed(calls, allowed); err != nil {
    // Error: "disallowed tools: delete_file"
}
```

## Loop States

The state machine supports these states:

| State | Description |
|-------|-------------|
| `exploring` | Initial discovery phase |
| `planning` | Planning approach |
| `architecting` | High-level design |
| `executing` | Active execution |
| `reviewing` | Reviewing results |
| `complete` | Successfully finished (terminal) |
| `failed` | Failed execution (terminal) |
| `cancelled` | Cancelled by user (terminal) |
| `paused` | Paused by user signal |
| `awaiting_approval` | Waiting for user approval |

States are fluid checkpoints. The loop can move backward except from terminal states.

## NATS Subject Patterns

Components communicate via these subjects:

| Subject | Direction | Description |
|---------|-----------|-------------|
| `agent.task.*` | external → loop | Task requests |
| `agent.request.*` | loop → model | Model requests |
| `agent.response.*` | model → loop | Model responses |
| `tool.execute.*` | loop → tools | Tool execution |
| `tool.result.*` | tools → loop | Tool results |
| `agent.complete.*` | loop → external | Completions |

## User Signals

Control signals for user interaction:

| Signal | Description |
|--------|-------------|
| `cancel` | Stop execution immediately |
| `pause` | Pause at next checkpoint |
| `resume` | Continue paused loop |
| `approve` | Approve pending result |
| `reject` | Reject with optional reason |
| `feedback` | Add feedback without decision |
| `retry` | Retry failed loop |

## Thread Safety

Types in this package are not inherently thread-safe. Agentic-loop owns synchronization for loop/context state and its
private active-loop mechanics.

## Limitations

- No streaming support (responses are complete documents)
- Tool parameters use `map[string]any` (no strong typing)
- Trajectory facts are immutable, observed-only, and internally bounded below 8 KiB
- Full evidence requires a configured registered Store and is returned by reference only

## Related Components

- [agentic-loop](../processor/agentic-loop/) - Loop orchestration
- [agentic-model](../processor/agentic-model/) - LLM endpoint integration
- [agentic-tools](../processor/agentic-tools/) - Tool execution framework
