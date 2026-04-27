# Agentic Quickstart

Get started with SemStreams' agentic AI orchestration system.

## What is the Agentic System?

The agentic system enables LLM-powered autonomous task execution within SemStreams. Unlike simple request-response LLM integrations, agents can:

- **Decide what actions to take** based on the current situation
- **Execute tools** to interact with the knowledge graph and external systems
- **Iterate** until the task is complete or a stopping condition is met

This transforms LLMs from passive responders into active problem solvers that can analyze sensor data, investigate anomalies, and execute multi-step workflows.

## Prerequisites

Before starting:

```bash
# Verify SemStreams builds
task build

# Verify Docker is running (required for E2E tests)
docker ps

# Check available ports
task e2e:check-ports
```

## Architecture Overview

The agentic system consists of 6 components communicating over NATS JetStream:

```text
                    ┌─────────────────────────────────────────┐
                    │           Agentic Components            │
                    ├─────────────────────────────────────────┤
User Message ───────► agentic-dispatch ─────► agentic-loop   │
                    │       │                      │          │
                    │       │              ┌───────┴───────┐  │
                    │       │              ▼               ▼  │
                    │       │        agentic-model   agentic-tools
                    │       │              │               │  │
                    │       │              ▼               │  │
                    │       │           LLM API    ◄───────┘  │
                    │       │                                 │
                    │       ◄─────── agent.complete.* ────────│
                    └─────────────────────────────────────────┘
```

| Component | Purpose |
|-----------|---------|
| **agentic-dispatch** | Routes user messages, handles commands, manages permissions |
| **agentic-loop** | State machine, orchestrates model calls and tool execution |
| **agentic-model** | Calls OpenAI-compatible LLM endpoints with retry logic |
| **agentic-tools** | Dispatches tool calls to registered executors |
| **agentic-memory** | Graph-backed persistent memory (optional) |
| **agentic-governance** | PII filtering, rate limiting, content governance (optional) |

## Running Your First Agent

The fastest way to see agents in action is the E2E test suite:

```bash
# Run the agentic E2E tests (~30 seconds)
task e2e:agentic
```

This starts a Docker environment with:
- NATS JetStream
- SemStreams with agentic components
- A mock LLM server for testing

When a sensor reading exceeds the temperature threshold, a rule triggers an agent that:
1. Receives the task to investigate the anomaly
2. Uses the `query_entity` tool to get sensor details from the knowledge graph
3. Analyzes the data and provides recommendations
4. Completes with an assessment

## Understanding the Configuration

The agentic configuration (`configs/agentic.json`) defines the component pipeline.

### Core Components

**agentic-loop** - The orchestrator:

```json
{
  "type": "processor",
  "name": "agentic-loop",
  "config": {
    "max_iterations": 5,        // Maximum tool call rounds
    "timeout": "30s",           // Overall loop timeout
    "stream_name": "AGENT",     // NATS JetStream stream
    "loops_bucket": "AGENT_LOOPS",           // KV for loop state
    "trajectories_bucket": "AGENT_TRAJECTORIES"  // KV for trajectories
  }
}
```

**agentic-model** - LLM caller:

```json
{
  "type": "processor",
  "name": "agentic-model",
  "config": {
    "endpoints": {
      "mock": {
        "url": "http://mock-llm:8080/v1",
        "model": "mock-model"
      },
      "openai": {
        "url": "https://api.openai.com/v1",
        "model": "gpt-4-turbo-preview",
        "api_key_env": "OPENAI_API_KEY"  // Read from environment
      }
    },
    "retry": {
      "max_attempts": 3,
      "backoff": "exponential"
    }
  }
}
```

> Need different LLM timeouts for fast vs heavy workloads? You can set
> `request_timeout` per endpoint and `timeout` per capability in the model
> registry — see
> [agentic-components → Timeout Resolution](../advanced/08-agentic-components.md#timeout-resolution).

**agentic-tools** - Tool executor:

```json
{
  "type": "processor",
  "name": "agentic-tools",
  "config": {
    "timeout": "10s",
    "allowed_tools": ["query_entity"]  // Empty array = allow all
  }
}
```

### Rule-Triggered Agents

Rules can spawn agents based on conditions:

```json
{
  "id": "temperature-anomaly-agent",
  "conditions": [
    { "field": "sensor.measurement.fahrenheit", "operator": "gte", "value": 45.0 }
  ],
  "on_enter": [
    {
      "type": "publish_agent",
      "role": "general",
      "model": "mock",
      "prompt": "Temperature anomaly detected. Analyze sensor {{.EntityID}}..."
    }
  ]
}
```

## State Machine

The agentic loop uses a fluid state machine:

```text
┌───────────┐   ┌──────────┐   ┌─────────────┐   ┌───────────┐   ┌───────────┐
│ exploring │──►│ planning │──►│ architecting│──►│ executing │──►│ reviewing │
└───────────┘   └──────────┘   └─────────────┘   └───────────┘   └─────┬─────┘
      ▲               ▲               ▲                ▲               │
      │               │               │                │               │
      └───────────────┴───────────────┴────────────────┘               │
                   (fluid backward transitions)                         │
                                                                        ▼
                                                    ┌───────────────────────────┐
                                                    │ complete │ failed │cancelled│
                                                    └───────────────────────────┘
```

**States are checkpoints, not gates.** Agents can move backward (e.g., from executing back to exploring) when they need to rethink. Only terminal states (complete, failed, cancelled) are final.

| State | Description |
|-------|-------------|
| `exploring` | Initial state, gathering information |
| `planning` | Developing approach |
| `architecting` | Designing solution |
| `executing` | Implementing solution |
| `reviewing` | Validating results |
| `complete` | Successfully finished |
| `failed` | Failed due to error or max iterations |
| `cancelled` | Cancelled by user signal |

## Observing Agent Execution

### Via NATS KV

```bash
# Watch loop state changes
nats kv watch AGENT_LOOPS

# Get a specific loop
nats kv get AGENT_LOOPS loop_abc123

# View trajectories after completion
nats kv get AGENT_TRAJECTORIES loop_abc123
```

### Via HTTP API

When running with `service-manager` enabled:

```bash
# List active loops
curl http://localhost:8080/api/agent/loops

# Get loop details
curl http://localhost:8080/api/agent/loops/loop_abc123
```

### Via Metrics

Prometheus metrics at `:9090/metrics`:

```
agentic_loop_started_total{role="general"}
agentic_loop_completed_total{role="general",status="complete"}
agentic_loop_iterations_total{loop_id="abc123"}
agentic_model_tokens_in_total{endpoint="openai"}
agentic_tools_executed_total{tool_name="query_entity"}
```

## Writing Custom Tools

Tools extend what agents can do. Implement the `ToolExecutor` interface:

```go
type MyToolExecutor struct{}

func (e *MyToolExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
    var args struct {
        Query string `json:"query"`
    }
    json.Unmarshal([]byte(call.Arguments), &args)

    // Do the work
    result := doSomethingWith(args.Query)

    return agentic.ToolResult{
        CallID:  call.ID,
        Content: result,
    }, nil
}

func (e *MyToolExecutor) ListTools() []agentic.ToolDefinition {
    return []agentic.ToolDefinition{
        {
            Name:        "my_tool",
            Description: "Does something useful",
            Parameters: map[string]any{
                "type": "object",
                "properties": map[string]any{
                    "query": map[string]any{"type": "string"},
                },
                "required": []string{"query"},
            },
        },
    }
}
```

Register on the shared tool registry at binary boot, then plumb it through
component dependencies (see `docs/operations/migration-beta16.md`):

```go
reg := agentictools.NewExecutorRegistry()
_ = executors.RegisterBuiltins(ctx, reg, executors.ToolDependencies{ /* ... */ })
_ = reg.RegisterTool("my_tool", &MyToolExecutor{})

deps := component.Dependencies{ToolRegistry: reg /* , ... */}
```

## Production Configuration

### Using Real LLMs

Replace the mock endpoint with a real provider:

```json
{
  "endpoints": {
    "default": {
      "url": "https://api.openai.com/v1/chat/completions",
      "model": "gpt-4-turbo-preview",
      "api_key_env": "OPENAI_API_KEY"
    },
    "local": {
      "url": "http://localhost:11434/v1/chat/completions",
      "model": "qwen2.5-coder:14b"
    }
  }
}
```

### Timeout Tuning

| Workload | Loop Timeout | Model Timeout | Tool Timeout |
|----------|--------------|---------------|--------------|
| Simple Q&A | 30s | 30s | 10s |
| Code review | 120s | 60s | 30s |
| Research tasks | 300s | 120s | 60s |

### Security

Always use tool allowlists in production:

```json
{
  "allowed_tools": ["query_entity", "read_file", "list_dir"]
}
```

## Next Steps

- [Agentic Components Reference](../advanced/08-agentic-components.md) - Detailed component specifications
- [Agentic Systems Concepts](../concepts/13-agentic-systems.md) - Foundational concepts
- [Orchestration Layers](../concepts/14-orchestration-layers.md) - When to use rules vs. workflows
- [Workflow Quickstart](08-workflow-quickstart.md) - Multi-step workflow orchestration
- [Troubleshooting](../operations/02-troubleshooting.md) - Common issues and solutions
