# Agentic Components Reference

Detailed specifications for the 3 agentic processing components in SemStreams.

## Optional Components

These components are **optional** — deploy them only when you need LLM-powered autonomous task execution.
The core SemStreams system (ingestion, graph, indexing, queries, rules) operates independently without any
agentic components.

For conceptual background on when and why to use agentic systems, see
[Concepts: Agentic Systems](../concepts/13-agentic-systems.md).

## Overview

The SemStreams agentic subsystem provides LLM-powered autonomous task execution through three specialized
components communicating over NATS JetStream:

```text
┌─────────────────────────────────────────────────────────────────────┐
│                      Agentic Components                              │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│   agent.task.*                                                       │
│        │                                                             │
│        ▼                                                             │
│   ┌─────────────┐   agent.request.*   ┌──────────────┐              │
│   │             │ ─────────────────▶ │              │              │
│   │  agentic-   │                     │   agentic-   │   HTTP       │
│   │    loop     │ ◀───────────────── │    model     │ ◀────▶ LLM   │
│   │             │   agent.response.*  │              │              │
│   └──────┬──────┘                     └──────────────┘              │
│          │                                                           │
│          │ tool.execute.*                                            │
│          ▼                                                           │
│   ┌─────────────┐                                                   │
│   │  agentic-   │                                                   │
│   │   tools     │ ────▶ Tool Executors                              │
│   │             │                                                   │
│   └──────┬──────┘                                                   │
│          │ tool.result.*                                             │
│          ▼                                                           │
│   ┌─────────────┐                                                   │
│   │  agentic-   │                                                   │
│   │    loop     │ ────▶ agent.complete.*                            │
│   │             │                                                   │
│   └─────────────┘                                                   │
│          │                                                           │
│          ▼                                                           │
│   KV: AGENT_LOOPS, AGENT_TRAJECTORIES                               │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

Each component:

- Owns specific NATS subjects and KV buckets
- Communicates via JetStream for reliable delivery
- Implements `Discoverable` and `LifecycleComponent` interfaces
- Can be scaled independently

## Component Specifications

### 1. agentic-loop - Loop Orchestrator

**Purpose**: Manages the agentic loop state machine, coordinates between model and tools, tracks pending tool
calls, and captures execution trajectories.

**Interfaces**: `Discoverable`, `LifecycleComponent`

**Input Ports**:

| Port | Type | Subject | Description |
|------|------|---------|-------------|
| agent_task | jetstream | agent.task.* | Incoming task requests |
| agent_response | jetstream | agent.response.> | Model responses |
| tool_result | jetstream | tool.result.> | Tool execution results |

**Output Ports**:

| Port | Type | Subject | Description |
|------|------|---------|-------------|
| agent_request | jetstream | agent.request.* | Requests to model |
| tool_execute | jetstream | tool.execute.* | Tool execution requests |
| agent_complete | jetstream | agent.complete.* | Loop completion events |
| loops_bucket | kv-bucket | AGENT_LOOPS | Loop entity storage |
| trajectories_bucket | kv-bucket | AGENT_TRAJECTORIES | Trajectory storage |

**Configuration**:

```json
{
  "max_iterations": 20,
  "timeout": "120s",
  "stream_name": "AGENT",
  "loops_bucket": "AGENT_LOOPS",
  "trajectories_bucket": "AGENT_TRAJECTORIES",
  "consumer_name_suffix": "",
  "ports": {
    "inputs": [
      {"name": "agent_task", "type": "jetstream", "subject": "agent.task.*"},
      {"name": "agent_response", "type": "jetstream", "subject": "agent.response.>"},
      {"name": "tool_result", "type": "jetstream", "subject": "tool.result.>"}
    ],
    "outputs": [
      {"name": "agent_request", "type": "jetstream", "subject": "agent.request.*"},
      {"name": "tool_execute", "type": "jetstream", "subject": "tool.execute.*"},
      {"name": "agent_complete", "type": "jetstream", "subject": "agent.complete.*"},
      {"name": "loops_bucket", "type": "kv-bucket", "bucket": "AGENT_LOOPS"},
      {"name": "trajectories_bucket", "type": "kv-bucket", "bucket": "AGENT_TRAJECTORIES"}
    ]
  }
}
```

**Configuration Options**:

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `max_iterations` | int | 20 | Maximum loop iterations before forced failure |
| `timeout` | string | 120s | Maximum loop duration |
| `stream_name` | string | AGENT | JetStream stream name |
| `loops_bucket` | string | AGENT_LOOPS | KV bucket for loop entities |
| `trajectories_bucket` | string | AGENT_TRAJECTORIES | KV bucket for trajectories |
| `consumer_name_suffix` | string | "" | Suffix for unique consumer names |

**State Machine**:

```text
                    ┌─────────────────────────────────┐
                    │                                 │
                    ▼                                 │
┌──────────┐   ┌──────────┐   ┌─────────────┐   ┌──────────┐   ┌──────────┐
│exploring │──▶│ planning │──▶│ architecting│──▶│executing │──▶│reviewing │
└──────────┘   └──────────┘   └─────────────┘   └──────────┘   └────┬─────┘
     ▲              ▲               ▲                ▲              │
     │              │               │                │              │
     └──────────────┴───────────────┴────────────────┘              │
                    (fluid backward transitions)                     │
                                                                     ▼
                                                           ┌──────────────────┐
                                                           │ complete │ failed │
                                                           └──────────────────┘
```

States are checkpoints, not gates. Agents can move backward when they need to rethink (except from terminal
states).

**Pending Tool Tracking**:

When a model requests multiple tool calls, the loop tracks each with its result:

```go
type LoopEntity struct {
    ID                 string
    State              LoopState
    PendingToolResults map[string]ToolResult  // Accumulated tool results by call ID
    Iterations         int
    MaxIterations      int
    StartedAt          time.Time
}
```

The loop only continues to the next model call when all pending tools have reported results.

---

### 2. agentic-model - Model Endpoint Caller

**Purpose**: Routes agent requests to OpenAI-compatible LLM endpoints, handles tool call marshaling, and
implements retry logic with configurable backoff.

**Interfaces**: `Discoverable`, `LifecycleComponent`

**Input Ports**:

| Port | Type | Subject | Description |
|------|------|---------|-------------|
| agent_request | jetstream | agent.request.> | Agent requests from loop |

**Output Ports**:

| Port | Type | Subject | Description |
|------|------|---------|-------------|
| agent_response | jetstream | agent.response.* | Model responses to loop |

**Configuration**:

```json
{
  "timeout": "120s",
  "retry": {
    "max_attempts": 3,
    "initial_delay": "1s",
    "max_delay": "60s",
    "rate_limit_delay": "5s"
  },
  "stream_name": "AGENT",
  "consumer_name_suffix": "",
  "ports": {
    "inputs": [
      {"name": "agent_request", "type": "jetstream", "subject": "agent.request.>"}
    ],
    "outputs": [
      {"name": "agent_response", "type": "jetstream", "subject": "agent.response.*"}
    ]
  }
}
```

Endpoints (including rate limits) are configured in the top-level `model_registry` block, not inline
in the component config.

**Configuration Options**:

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `timeout` | string | 120s | Component-level default LLM request timeout. See [Timeout Resolution](#timeout-resolution) for the full precedence chain. |
| `retry.max_attempts` | int | 3 | Maximum retry attempts |
| `retry.initial_delay` | string | 1s | Initial delay before first retry |
| `retry.max_delay` | string | 60s | Maximum delay between retries |
| `retry.rate_limit_delay` | string | 5s | Extra wait added before backoff on HTTP 429 |
| `stream_name` | string | AGENT | JetStream stream name |
| `consumer_name_suffix` | string | "" | Suffix for unique consumer names |

**Endpoint Configuration** (in `model_registry`):

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `url` | string | yes | Full URL to chat completions endpoint |
| `model` | string | yes | Model identifier for the API |
| `api_key_env` | string | no | Environment variable containing API key |
| `requests_per_minute` | int | no | Token bucket rate limit (0 = unlimited) |
| `max_concurrent` | int | no | Maximum simultaneous in-flight requests (0 = unlimited) |
| `request_timeout` | string | no | Per-endpoint LLM request timeout (e.g. `"45s"`). Overrides capability and component defaults. See [Timeout Resolution](#timeout-resolution). |

**Capability Configuration** (in `model_registry.capabilities`):

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `preferred` | []string | yes | Endpoint names in order of preference |
| `fallback` | []string | no | Backup endpoint names |
| `requires_tools` | bool | no | Filter the chain to tool-capable endpoints only |
| `timeout` | string | no | Per-capability LLM request timeout (e.g. `"30s"`). Overrides component default; overridden by endpoint and task timeouts. See [Timeout Resolution](#timeout-resolution). |

**Endpoint Resolution**:

1. Exact match on `model` field in request
2. Fall back to `default` endpoint if present
3. Return error if no match

#### Timeout Resolution

agentic-model wraps every LLM call in a `context.WithTimeout`. The timeout is
resolved at the call site from four layers, highest precedence first:

| Precedence | Source | Where it's set |
|------------|--------|----------------|
| 1 (highest) | `TaskMessage.timeout` → `AgentRequest.timeout` | Producer of the TaskMessage; persists across continuation iterations of the same loop |
| 2 | `endpoint.request_timeout` | `model_registry.endpoints.<name>.request_timeout` |
| 3 | `capability.timeout` | `model_registry.capabilities.<name>.timeout` |
| 4 (lowest) | `agentic-model.timeout` | Component config (default `120s`) |

The first layer with a non-empty, parseable duration wins. A malformed duration
at any layer logs a warning and falls through to the next. The selected source
is emitted on every request as a structured log field `timeout_source` with
values `task`, `endpoint`, `capability`, `component`, or `default`.

**Worked example**: with `TaskMessage.Timeout="30s"`, endpoint
`request_timeout="60s"`, capability `timeout="90s"`, and component `timeout="120s"`,
the effective request timeout is **30 seconds** (`timeout_source=task`). Omit
`TaskMessage.Timeout` and it becomes 60 seconds (`timeout_source=endpoint`), and so on.

**When to use which layer**:

- **Component** — sets the baseline for any request that doesn't match a more specific rule. Keep this as the safety ceiling.
- **Capability** — the natural place to express "fast classification calls should fail faster than heavy planning calls." Lets operators tune budgets without touching every TaskMessage producer.
- **Endpoint** — use when a specific endpoint has known latency characteristics (e.g. a local quantized model with predictable response times).
- **Task** — use sparingly for one-off overrides (e.g. a plan-reviewer task that should time out quickly even when routed to the default capability).

**Compatible Providers**:

| Provider | URL Pattern | Notes |
|----------|-------------|-------|
| OpenAI | `https://api.openai.com/v1/chat/completions` | Requires API key |
| Ollama | `http://localhost:11434/v1/chat/completions` | Local, no key needed |
| LiteLLM | `http://localhost:4000/v1/chat/completions` | Proxy for multiple providers |
| Azure OpenAI | `https://{deployment}.openai.azure.com/...` | Requires API key |
| vLLM | `http://localhost:8000/v1/chat/completions` | Local serving |
| Together AI | `https://api.together.xyz/v1/chat/completions` | Requires API key |
| Anthropic (via proxy) | Requires OpenAI-compatible proxy | Claude models |

**Response Status Mapping**:

| LLM Response | AgentResponse Status | Action |
|--------------|---------------------|--------|
| Content only | `complete` | Loop may terminate |
| Tool calls present | `tool_call` | Loop dispatches tools |
| Error | `error` | Loop may retry or fail |

---

### 3. agentic-tools - Tool Dispatch

**Purpose**: Receives tool execution requests, validates against allowlist, dispatches to registered executors,
and returns results.

**Interfaces**: `Discoverable`, `LifecycleComponent`

**Input Ports**:

| Port | Type | Subject | Description |
|------|------|---------|-------------|
| tool_execute | jetstream | tool.execute.> | Tool execution requests |

**Output Ports**:

| Port | Type | Subject | Description |
|------|------|---------|-------------|
| tool_result | jetstream | tool.result.* | Tool execution results |

**Configuration**:

```json
{
  "allowed_tools": [],
  "timeout": "60s",
  "stream_name": "AGENT",
  "consumer_name_suffix": "",
  "ports": {
    "inputs": [
      {"name": "tool_execute", "type": "jetstream", "subject": "tool.execute.>"}
    ],
    "outputs": [
      {"name": "tool_result", "type": "jetstream", "subject": "tool.result.*"}
    ]
  }
}
```

**Configuration Options**:

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `allowed_tools` | []string | nil | Allowlist of tool names (empty = allow all) |
| `timeout` | string | 60s | Per-tool execution timeout |
| `stream_name` | string | AGENT | JetStream stream name |
| `consumer_name_suffix` | string | "" | Suffix for unique consumer names |

**Tool Executor Interface**:

```go
type ToolExecutor interface {
    Execute(ctx context.Context, call ToolCall) (ToolResult, error)
    ListTools() []ToolDefinition
}
```

**Tool Registration**:

Tools can be registered in two ways:

1. **Global registration via init()** (preferred for reusable tools):

```go
// In your tool package
func init() {
    agentictools.RegisterTool("file_reader", &FileReaderExecutor{})
    agentictools.RegisterTool("web_search", &WebSearchExecutor{})
}
```

1. **Per-component registration** (for component-specific tools):

```go
comp, _ := agentictools.NewComponent(rawConfig, deps)
toolsComp := comp.(*agentictools.Component)
toolsComp.RegisterToolExecutor(&CustomExecutor{})
```

The global registration pattern matches how components and rules are registered in SemStreams.

**Note**: The router component uses the same `init()` pattern for command registration. See the
[Input Router Specification](../architecture/specs/semstreams-input-router-spec.md#part-6-command-registry)
for details on registering custom commands.

**Listing Available Tools**:

```go
// Get all registered tools (global + local)
tools := toolsComp.ListTools()
```

**Allowlist Behavior**:

| `allowed_tools` Value | Behavior |
|----------------------|----------|
| `nil` or `[]` | All registered tools allowed |
| `["tool_a", "tool_b"]` | Only listed tools allowed |

When a tool is blocked, the result contains an error message that the model can reason about.

---

## KV Bucket Ownership Table

| Bucket | Writer | Readers | Purpose |
|--------|--------|---------|---------|
| `AGENT_LOOPS` | agentic-loop | (optional) rule, graph-query | Loop entity state |
| `AGENT_TRAJECTORIES` | agentic-loop | (optional) graph-query | Execution traces |

Note: The rule processor and graph-query are optional readers. The agentic system operates independently
without them.

---

## Message Formats

### AgentRequest

Sent from agentic-loop to agentic-model:

```json
{
  "id": "req_abc123",
  "loop_id": "loop_xyz789",
  "model": "gpt-4",
  "messages": [
    {"role": "system", "content": "You are a helpful assistant."},
    {"role": "user", "content": "Review main.go for security issues."}
  ],
  "tools": [
    {
      "name": "read_file",
      "description": "Read file contents",
      "parameters": {
        "type": "object",
        "properties": {
          "path": {"type": "string"}
        },
        "required": ["path"]
      }
    }
  ],
  "temperature": 0.7,
  "max_tokens": 4096
}
```

### AgentResponse

Sent from agentic-model to agentic-loop:

```json
{
  "request_id": "req_abc123",
  "status": "tool_call",
  "message": {
    "role": "assistant",
    "content": "",
    "tool_calls": [
      {
        "id": "call_001",
        "name": "read_file",
        "arguments": {"path": "main.go"}
      }
    ]
  },
  "token_usage": {
    "prompt_tokens": 150,
    "completion_tokens": 45
  }
}
```

### ToolCall

Sent from agentic-loop to agentic-tools:

```json
{
  "id": "call_001",
  "loop_id": "loop_xyz789",
  "name": "read_file",
  "arguments": "{\"path\": \"main.go\"}"
}
```

### ToolResult

Sent from agentic-tools to agentic-loop:

```json
{
  "call_id": "call_001",
  "loop_id": "loop_xyz789",
  "content": "package main\n\nimport \"fmt\"\n\nfunc main() {\n    fmt.Println(\"Hello\")\n}",
  "error": ""
}
```

---

## Production Configuration

### Timeout Tuning

Different workloads require different timeouts. The agentic-model component
resolves each LLM call's timeout from four layers (see [Timeout Resolution](#timeout-resolution)):
task → endpoint → capability → component default. The **capability** layer is
usually the right place to express workload-shape tuning, because capabilities
already partition model traffic by intent (fast classification, heavy planning,
summarization, etc.) and a single registry block covers all callers.

**Capability-level recommendations** (`model_registry.capabilities.<name>.timeout`):

| Capability | Typical Timeout | Workload |
|------------|----------------|----------|
| `fast` | 15–30s | Classification, routing, short answers |
| `general` | 60s | Default Q&A, summaries, simple tool use |
| `heavy` | 120–180s | Planning, multi-step reasoning, long context |
| `summarization` | 180–300s | Full-context compaction across large histories |

**Endpoint-level** (`model_registry.endpoints.<name>.request_timeout`): set
when a specific endpoint has known latency quirks (e.g. a local quantized model
with slower TTFT) independent of what capability is routing to it.

**Task-level** (`TaskMessage.timeout`): reserve for one-off overrides, e.g. a
short-lived plan-reviewer task that should time out quickly even when routed
to a `heavy`-capability endpoint.

**Component default** (`agentic-model.timeout`): keep this as the safety
ceiling — the last-resort cap when no more specific rule applies.

**Loop and tool timeouts** remain separate concerns (see agentic-loop and
agentic-tools configs above). Rule of thumb: loop timeout should be >
(`max_iterations` × longest expected model timeout) + tool overhead.

### Max Iterations

Tune based on task complexity:

| Task Type | Recommended max_iterations |
|-----------|---------------------------|
| Single-step (Q&A) | 3-5 |
| Multi-step (code review) | 10-15 |
| Research/exploration | 20-30 |
| Complex multi-file changes | 30-50 |

**Warning**: Higher iteration limits increase cost and risk of loops. Always combine with timeouts.

### Stream Retention Settings

Configure the AGENT stream for your deployment:

```json
{
  "name": "AGENT",
  "subjects": ["agent.>", "tool.>"],
  "retention": "limits",
  "max_age": "1h",
  "max_msgs": 100000,
  "max_bytes": 104857600,
  "storage": "memory",
  "replicas": 1
}
```

| Setting | Production | Development |
|---------|------------|-------------|
| `storage` | file | memory |
| `replicas` | 3 | 1 |
| `max_age` | 24h | 1h |

### Multiple Endpoints Strategy

Configure fallback endpoints for reliability:

```json
{
  "endpoints": {
    "primary": {
      "url": "https://api.openai.com/v1/chat/completions",
      "model": "gpt-4-turbo-preview",
      "api_key_env": "OPENAI_API_KEY"
    },
    "fallback": {
      "url": "http://localhost:11434/v1/chat/completions",
      "model": "qwen2.5-coder:14b"
    },
    "default": {
      "url": "http://localhost:11434/v1/chat/completions",
      "model": "qwen2.5-coder:7b"
    }
  }
}
```

Use the `model` field in requests to route to specific endpoints.

---

## Observability

### Metrics

Each component exposes Prometheus metrics:

**agentic-loop**:

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `agentic_loop_started_total` | counter | role | Loops started |
| `agentic_loop_completed_total` | counter | role, status | Loops completed |
| `agentic_loop_iterations_total` | counter | loop_id | Iterations per loop |
| `agentic_loop_duration_seconds` | histogram | role | Loop duration |
| `agentic_loop_pending_tools` | gauge | loop_id | Currently pending tool calls |

**agentic-model**:

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `agentic_model_requests_total` | counter | endpoint, status | Requests to LLM |
| `agentic_model_tokens_in_total` | counter | endpoint | Input tokens |
| `agentic_model_tokens_out_total` | counter | endpoint | Output tokens |
| `agentic_model_latency_seconds` | histogram | endpoint | Request latency |
| `agentic_model_retries_total` | counter | endpoint | Retry attempts |
| `semstreams_agentic_model_rate_limit_hits_total` | counter | model | HTTP 429 responses received |

**agentic-tools**:

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `agentic_tools_executed_total` | counter | tool_name, status | Tool executions |
| `agentic_tools_duration_seconds` | histogram | tool_name | Execution duration |
| `agentic_tools_blocked_total` | counter | tool_name | Blocked by allowlist |

### Trajectory Analysis

Query trajectories for debugging and analytics:

```bash
# Get trajectory for a specific loop
nats kv get AGENT_TRAJECTORIES loop_xyz789

# Watch for new trajectories
nats kv watch AGENT_TRAJECTORIES
```

**Trajectory fields for analysis**:

| Field | Use Case |
|-------|----------|
| `total_tokens_in` | Cost tracking |
| `total_tokens_out` | Cost tracking |
| `steps[].duration` | Performance analysis |
| `steps[].tool_name` | Tool usage patterns |
| `outcome` | Success/failure rates |

### Debugging Failed Loops

When a loop fails:

1. **Check loop entity state**:

   ```bash
   nats kv get AGENT_LOOPS loop_xyz789
   ```

2. **Review trajectory**:

   ```bash
   nats kv get AGENT_TRAJECTORIES loop_xyz789
   ```

3. **Check for pending tools**:
   Look at `pending_tool_results` in the loop entity — tools that never returned results indicate
   execution failures in agentic-tools.

4. **Review model responses**:
   Trajectory steps include model outputs — check for error messages or unexpected behavior.

---

## Advanced Patterns

### Custom Tool Executors

Implement the `ToolExecutor` interface for custom tools:

```go
type DatabaseQueryExecutor struct {
    db *sql.DB
}

func (e *DatabaseQueryExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
    var args struct {
        Query string `json:"query"`
    }
    if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
        return agentic.ToolResult{
            CallID: call.ID,
            Error:  "invalid arguments: " + err.Error(),
        }, nil
    }
    
    // Validate query (prevent SQL injection)
    if !isReadOnlyQuery(args.Query) {
        return agentic.ToolResult{
            CallID: call.ID,
            Error:  "only SELECT queries are allowed",
        }, nil
    }
    
    rows, err := e.db.QueryContext(ctx, args.Query)
    if err != nil {
        return agentic.ToolResult{
            CallID: call.ID,
            Error:  err.Error(),
        }, nil
    }
    defer rows.Close()
    
    result := formatRows(rows)
    return agentic.ToolResult{
        CallID:  call.ID,
        Content: result,
    }, nil
}

func (e *DatabaseQueryExecutor) ListTools() []agentic.ToolDefinition {
    return []agentic.ToolDefinition{
        {
            Name:        "database_query",
            Description: "Execute a read-only SQL query against the database",
            Parameters: map[string]interface{}{
                "type": "object",
                "properties": map[string]interface{}{
                    "query": map[string]interface{}{
                        "type":        "string",
                        "description": "SQL SELECT query to execute",
                    },
                },
                "required": []string{"query"},
            },
        },
    }
}
```

### Multi-Model Routing

Route different tasks to appropriate models:

```go
// In your task submission code
func submitTask(task Task) {
    model := "default"
    
    switch task.Type {
    case "code_review":
        model = "gpt-4"  // Best reasoning
    case "translation":
        model = "gpt-3.5-turbo"  // Fast, good enough
    case "local_only":
        model = "ollama"  // Privacy-sensitive
    }
    
    request := agentic.AgentRequest{
        Model:    model,
        Messages: task.Messages,
        Tools:    task.Tools,
    }
    
    publish("agent.task."+task.ID, request)
}
```

### Architect/Editor Workflows

The agentic-loop supports automatic architect/editor handoff:

```go
// Task with architect role spawns editor automatically
task := agentic.TaskMessage{
    LoopID: uuid.New().String(),
    Role:   "architect",  // Will spawn editor on completion
    Prompt: "Design a solution for user authentication",
}
```

**Flow**:

1. Architect loop completes with a plan
2. agentic-loop automatically creates editor loop
3. Editor receives architect's output as context
4. Editor implements the plan
5. Final result published to `agent.complete.*`

### Rule-Triggered Agents (Optional Integration)

The rule processor can trigger agents by publishing to `agent.task.*`. This is an **optional integration** —
the agentic system works without any rules configured.

**What rules can do:**

- **Observe agents**: Watch `AGENT_LOOPS` KV bucket for state changes, fire alerts on thresholds
- **Trigger agents**: Publish tasks to `agent.task.*` based on graph events
- **Chain agents**: Spawn follow-up agents when previous agents complete

**What rules cannot do:**

- Force agent state transitions (agents manage their own state machine)
- Interrupt running agents (agents are autonomous once started)
- Modify agent behavior mid-execution

#### Example: Trigger agent on graph event

A rule can watch for entity changes and spawn an agent to investigate. The rule uses the `publish` action
to send a TaskMessage to the agentic-loop:

- Rule watches entity pattern (e.g., `security_alert.>`)
- When condition matches, rule publishes to `agent.task.{task_id}`
- Message payload includes: task_id, role, model, prompt
- agentic-loop receives task and begins autonomous execution

See [Rules Engine](06-rules-engine.md) for rule configuration details.

### Graph Query Tool Integration

Enable agents to query the knowledge graph:

```go
type GraphQueryExecutor struct {
    client *natsclient.Client
}

func (e *GraphQueryExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
    var args struct {
        EntityID string `json:"entity_id,omitempty"`
        Query    string `json:"query,omitempty"`
    }
    json.Unmarshal([]byte(call.Arguments), &args)
    
    var result []byte
    var err error
    
    if args.EntityID != "" {
        result, err = e.client.Request(ctx, "graph.query.entity", 
            []byte(`{"entity_id": "`+args.EntityID+`"}`))
    } else if args.Query != "" {
        result, err = e.client.Request(ctx, "graph.query.semantic",
            []byte(`{"query": "`+args.Query+`"}`))
    }
    
    if err != nil {
        return agentic.ToolResult{CallID: call.ID, Error: err.Error()}, nil
    }
    
    return agentic.ToolResult{CallID: call.ID, Content: string(result)}, nil
}

func (e *GraphQueryExecutor) ListTools() []agentic.ToolDefinition {
    return []agentic.ToolDefinition{
        {
            Name:        "graph_query",
            Description: "Query the knowledge graph for entity information or semantic search",
            Parameters: map[string]interface{}{
                "type": "object",
                "properties": map[string]interface{}{
                    "entity_id": map[string]interface{}{
                        "type":        "string",
                        "description": "Specific entity ID to retrieve",
                    },
                    "query": map[string]interface{}{
                        "type":        "string",
                        "description": "Natural language query for semantic search",
                    },
                },
            },
        },
    }
}
```

---

## Security Considerations

### Tool Allowlists

Always use allowlists in production:

```json
{
  "allowed_tools": ["read_file", "list_dir", "graph_query"]
}
```

**Never allow in production without careful consideration**:

- `execute_command` or `bash` tools
- `write_file` without path restrictions
- `http_request` to arbitrary URLs
- Database write operations

### API Key Management

Store API keys in environment variables, never in config files:

```json
{
  "endpoints": {
    "openai": {
      "url": "https://api.openai.com/v1/chat/completions",
      "api_key_env": "OPENAI_API_KEY"
    }
  }
}
```

Use secret management systems in production:

- Kubernetes Secrets
- HashiCorp Vault
- AWS Secrets Manager
- Environment variable injection

### Rate Limiting

Protect against runaway loops and costs:

1. **Iteration limits**: Always set `max_iterations`
2. **Layered timeout guards**: Set `timeout` at loop and tool levels, and tune
   LLM call timeouts per capability or endpoint in the model registry
   (see [Timeout Resolution](#timeout-resolution)). The component-level
   `agentic-model.timeout` remains the safety ceiling.
3. **Endpoint throttling**: Configure `requests_per_minute` and `max_concurrent` on each endpoint in
   the model registry. The throttle is shared across all agents targeting that endpoint, preventing
   agent teams from collectively saturating a provider's rate limit.
4. **Budget alerts**: Monitor token usage metrics

Configure endpoint throttling in the model registry:

```json
{
  "model_registry": {
    "endpoints": {
      "gpt-4": {
        "url": "https://api.openai.com/v1/chat/completions",
        "model": "gpt-4-turbo-preview",
        "api_key_env": "OPENAI_API_KEY",
        "requests_per_minute": 60,
        "max_concurrent": 5
      }
    }
  }
}
```

The `semstreams_agentic_model_rate_limit_hits_total` metric tracks HTTP 429 responses per model. A
rising count indicates the configured limit is too high for the provider tier and should be reduced.

### Input Validation

Tool executors must validate all inputs:

```go
func (e *FileReaderExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
    var args struct {
        Path string `json:"path"`
    }
    json.Unmarshal([]byte(call.Arguments), &args)
    
    // Validate path - prevent directory traversal
    cleanPath := filepath.Clean(args.Path)
    if strings.HasPrefix(cleanPath, "..") || filepath.IsAbs(cleanPath) {
        return agentic.ToolResult{
            CallID: call.ID,
            Error:  "path must be relative and within workspace",
        }, nil
    }
    
    // Check against allowed directories
    if !isInAllowedDir(cleanPath, e.allowedDirs) {
        return agentic.ToolResult{
            CallID: call.ID,
            Error:  "path outside allowed directories",
        }, nil
    }
    
    // ... proceed with read
}
```

### Audit Logging

Trajectories provide audit trails, but consider additional logging:

```go
func (e *SensitiveToolExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
    // Log before execution
    slog.Info("sensitive tool invoked",
        "tool", call.Name,
        "loop_id", call.LoopID,
        "arguments", call.Arguments,
    )
    
    result := e.doExecution(ctx, call)
    
    // Log after execution (without sensitive content)
    slog.Info("sensitive tool completed",
        "tool", call.Name,
        "loop_id", call.LoopID,
        "has_error", result.Error != "",
    )
    
    return result, nil
}
```

---

## Troubleshooting

### Loop Stuck in State

**Symptoms**: Loop entity shows same state for extended period.

**Diagnosis**:

```bash
nats kv get AGENT_LOOPS <loop_id>
```

Check `pending_tool_results` — if non-empty, tools haven't returned results.

**Common causes**:

1. agentic-tools not running or not subscribed
2. Tool executor threw panic (check logs)
3. Tool timeout exceeded

**Resolution**: Restart agentic-tools, check tool executor logs.

### Model Returns Empty Response

**Symptoms**: AgentResponse has empty `content` and no `tool_calls`.

**Diagnosis**: Check agentic-model logs for HTTP errors.

**Common causes**:

1. Model endpoint unreachable
2. Invalid API key
3. Rate limited
4. Request too large (context length exceeded)

**Resolution**: Verify endpoint configuration, check API key, review request size.

### Tool Not Found

**Symptoms**: Tool result contains "tool not found" error.

**Diagnosis**: Check registered tools:

```go
registry.ListAllTools()
```

**Common causes**:

1. Tool executor not registered
2. Tool name mismatch (case sensitive)
3. Tool blocked by allowlist

**Resolution**: Register executor before starting component, verify tool names match.

### High Token Usage

**Symptoms**: Metrics show unexpectedly high token counts.

**Diagnosis**: Review trajectories for patterns:

```bash
nats kv get AGENT_TRAJECTORIES <loop_id>
```

**Common causes**:

1. Large tool results included in every message
2. Long system prompts repeated each turn
3. Loop iterations accumulating context

**Resolution**: Summarize tool results, optimize prompts, reduce max_iterations.

---

## Related Documentation

- [Agentic Systems Concepts](../concepts/13-agentic-systems.md) - Foundational concepts
- [Graph Components Reference](07-graph-components.md) - Knowledge graph integration
- [Configuration Guide](../basics/06-configuration.md) - Component configuration
- [Architecture Overview](../basics/02-architecture.md) - System design
