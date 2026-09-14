# Agentic Quickstart

Get started with SemStreams' agentic AI orchestration system.

## What is the Agentic System?

The agentic system enables LLM-powered autonomous task execution within SemStreams. Unlike simple
request-response LLM integrations, agents can:

- **Decide what actions to take** based on the current situation
- **Execute tools** to interact with the knowledge graph and external systems
- **Iterate** until the task is complete or a stopping condition is met

This transforms LLMs from passive responders into active problem solvers that can analyze sensor data,
investigate anomalies, and execute multi-step workflows.

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
    "approval_timeout": "5m",   // Auto-reject a gated call after this long; empty = wait forever
    "stream_name": "AGENT",     // NATS JetStream stream
    "loops_bucket": "AGENT_LOOPS",           // KV for loop state
    "trajectory_evidence_storage_instance": "objectstore"
  }
}
```

**agentic-model** - LLM caller. Note where the endpoints are *not*: `agentic-model`'s own config carries
`timeout`, `retry` and `ports` and nothing else
([`processor/agentic-model/config.go:13-18`](../../processor/agentic-model/config.go)).

```json
{
  "type": "processor",
  "name": "agentic-model",
  "config": {
    "timeout": "30s",
    "retry": {
      "max_attempts": 3,
      "backoff": "exponential"
    }
  }
}
```

Endpoints live in `model_registry`, a **top-level** key of the config file, beside `components` rather than
inside it ([`config/config.go:55`](../../config/config.go),
[`model/registry.go:384-388`](../../model/registry.go)). The component receives it as a dependency
([`processor/agentic-model/component.go:157,187`](../../processor/agentic-model/component.go)), which is why
a `model` name in a rule or a task resolves the same way for every component that calls a model:

```json
{
  "model_registry": {
    "endpoints": {
      "mock": {
        "provider": "openai",
        "url": "http://mock-llm:8080/v1",
        "model": "mock-model"
      }
    },
    "defaults": { "model": "mock" }
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
    "allowed_tools": ["query_entity", "query_by_type"],  // Empty array = allow all
    "approval_required": ["query_by_type"]               // These tools need a human first
  }
}
```

A tool named in `approval_required` is not executed when the model calls it.
The loop parks in `awaiting_approval` and publishes an approval-pending event;
a human answers over `POST /agentic-dispatch/loops/{id}/approval` with
`approve`, `reject`, or `modify`, and only then does the call run. The prefix is
the component's configured name, which the service manager mounts the routes
under. A `reject` does not fail the loop — the loop hands the model a synthesized
rejection result and carries on. Pair it with `agentic-loop`'s `approval_timeout`
above — with no timeout set, a gated call nobody answers waits forever. The
subjects, the state transitions, and what a product-layer approval UI has to
implement are in [Approval Flow](../concepts/17-approval-flow.md).

### Rule-Triggered Agents

Rules can spawn agents based on conditions. A `publish_agent` action needs four fields — `subject`, `role`,
`model` and `prompt`. All four are checked before the action does anything, and a missing one fails the action
with, for example, `subject is required for publish_agent action`
([`processor/rule/actions.go:1687-1698`](../../processor/rule/actions.go)).

```json
{
  "id": "temperature-anomaly-agent",
  "conditions": [
    { "field": "sensor.measurement.fahrenheit", "operator": "gte", "value": 45.0 }
  ],
  "on_enter": [
    {
      "type": "publish_agent",
      "subject": "agent.task.anomaly",
      "role": "general",
      "model": "mock",
      "prompt": "Temperature anomaly detected on sensor $entity.id. Its reading is $entity.triple.sensor.measurement.fahrenheit degrees Fahrenheit. Investigate and recommend an action."
    }
  ]
}
```

**Pick the subject to match the loop's task port.** `agentic-loop` subscribes its `agent.task` input to
`agent.task.*` on the `AGENT` stream
([`processor/agentic-loop/config.go:396`](../../processor/agentic-loop/config.go)). That wildcard is a single
NATS token, so `agent.task.anomaly` is delivered and `agent.task.anomaly.high` is not. Name the work in one
token, as the shipped rules do — `agent.task.research`, `agent.task.subtopic`, `agent.task.synthesis`
([`configs/rules/deep-research/`](../../configs/rules/deep-research/)).

**Substitution is `$`-prefixed, not Go templates.** There is no `text/template` engine on the rule's
substitution path; `{{.EntityID}}` is not substitution syntax and would reach the model as those literal
characters. The tokens a rule can use are `$entity.id`, the entity-ID segments (`$entity.org` …
`$entity.instance`), `$entity.triple.<predicate>`, `$message.<field>`, `$state.iteration` and `$now`, among
others — the full list is the doc comment on `SubstituteVariables`
([`processor/rule/execution_context.go:207-252`](../../processor/rule/execution_context.go)).

Get a `$` token wrong and the engine tells you: it survives into the prompt verbatim **and** logs an
unresolved-template warning, so the rule processor's logs will name it. Get the *syntax* wrong — `{{...}}` or
`${...}` — and nothing is logged at all, because those are not tokens the warning knows how to look for
([`processor/rule/execution_context.go:35`](../../processor/rule/execution_context.go)). A prompt full of
literal braces is the only symptom you will get, so read the prompt the agent actually received.

For a rule this quickstart's shape is derived from, read
[`configs/rules/deep-research/01-spawn-researcher.json`](../../configs/rules/deep-research/01-spawn-researcher.json).
The optional fields — `tools`, `properties`, `run_scope`, `workflow_slug`, `max_iterations` and the rest — are in
the [rules engine reference](../advanced/06-rules-engine.md#publish_agent).

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

**States are checkpoints, not gates.** Agents can move backward (e.g., from executing back to exploring) when
they need to rethink. Only terminal states (complete, failed, cancelled) are final.

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
nats kv get AGENT_LOOPS 7c9e6679-7425-40de-944b-e07fc1f90ae7

# Internal operators can watch append-only fact keys. Keys contain a loop digest,
# not the raw loop ID, and the bucket has history 1 with no TTL.
nats kv watch AGENT_TRAJECTORIES
```

Application readers use graph-gateway GraphQL. The query returns one cursor-paged selection of observed fact metadata
and durable evidence references; it never returns evidence bodies:

```graphql
query {
  trajectory(loopId: "7c9e6679-7425-40de-944b-e07fc1f90ae7", limit: 64) {
    coverage
    terminal_observed
    observed_totals { facts tokens_in tokens_out }
    facts {
      attempt_id
      kind
      status
      evidence_digest
      evidence_capture
      evidence { storage_instance key content_type size }
    }
    next_cursor
  }
}
```

Treat `next_cursor` as opaque and pass it unchanged as the `cursor` argument to request the next page. Totals and
`terminal_observed` describe only the returned page. An authorized evidence reader separately resolves
`facts[].evidence.storage_instance` through the injected StoreRegistry and reads `facts[].evidence.key` from that
registered Store. Readers without that authority stop at the reference.

### Via HTTP API

When running with `service-manager` enabled:

```bash
# List active loops
curl http://localhost:8080/api/agent/loops

# Get loop details
curl http://localhost:8080/api/agent/loops/7c9e6679-7425-40de-944b-e07fc1f90ae7
```

### Via Metrics

Prometheus metrics at `:9090/metrics`. All metrics use the
`semstreams_` namespace. A few you'll reach for first:

```text
# Loop lifecycle
semstreams_agentic_loop_loops_created_total
semstreams_agentic_loop_loops_completed_total
semstreams_agentic_loop_loops_failed_total{reason="length_truncated"}
semstreams_agentic_loop_iterations_total

# Per-tool-call counts (the metric to watch for "did this tool run
# and how often"). Two complementary views:
#   - dispatched_total counts what the loop emitted to tool.execute
#   - executions_total counts what the executor actually ran (status
#     ∈ {success, error, timeout})
# Sum across statuses for "any execution"; filter on
# {status="success"} for the success rate.
semstreams_agentic_loop_tool_calls_dispatched_total{tool_name="query_entity"}
semstreams_agentic_tools_executions_total{tool_name="query_entity",status="success"}

# Token spend
semstreams_agentic_model_tokens_total{model="qwen-32b",type="prompt"}
semstreams_agentic_model_tokens_total{model="qwen-32b",type="completion"}
```

For the full metrics catalogue see
[`docs/advanced/08-agentic-components.md#metrics`](../advanced/08-agentic-components.md#metrics).

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

Replace the mock endpoint with a real provider, in the top-level `model_registry`. `api_key_env` names an
environment variable; the key itself never goes in the config file
([`model/registry.go:287`](../../model/registry.go)):

```json
{
  "model_registry": {
    "endpoints": {
      "default": {
        "provider": "openai",
        "url": "https://api.openai.com/v1",
        "model": "gpt-4-turbo-preview",
        "api_key_env": "OPENAI_API_KEY"
      },
      "local": {
        "provider": "openai",
        "url": "http://localhost:11434/v1",
        "model": "qwen2.5-coder:14b"
      }
    },
    "defaults": { "model": "default" }
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
- [Orchestration Layers](../concepts/14-orchestration-layers.md) - Rules, components and durable phase patterns
- [Troubleshooting](../operations/02-troubleshooting.md) - Common issues and solutions
