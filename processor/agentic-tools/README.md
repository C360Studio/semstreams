# agentic-tools

Tool execution component for the agentic processing system.

## Overview

The `agentic-tools` component executes tool calls from the agentic loop orchestrator. It receives `ToolCall` messages, dispatches them to registered tool executors, and publishes `ToolResult` messages back. Supports tool registration, allowlist filtering, per-execution timeouts, and concurrent execution.

## Architecture

```
┌───────────────┐     ┌────────────────┐     ┌──────────────────┐
│ agentic-loop  │────►│ agentic-tools  │────►│ Tool Executors   │
│               │     │                │     │ (your code)      │
│               │◄────│                │◄────│                  │
└───────────────┘     └────────────────┘     └──────────────────┘
  tool.execute.*        Execute()           read_file, query_db,
  tool.result.*                             call_api, etc.
```

## Features

- **Tool Registration**: Register custom tool executors at runtime
- **Allowlist Filtering**: Restrict which tools can execute
- **Timeout Handling**: Per-execution timeout with context cancellation
- **Concurrent Execution**: Multiple tools can run in parallel

## Configuration

```json
{
  "type": "processor",
  "name": "agentic-tools",
  "enabled": true,
  "config": {
    "stream_name": "AGENT",
    "timeout": "60s",
    "allowed_tools": null,
    "ports": {
      "inputs": [
        {"name":"tool_calls","config":{"kind":"jetstream","subjects":["tool.execute.>"],"stream_name":"AGENT"}}
      ],
      "outputs": [
        {"name":"tool_results","config":{"kind":"jetstream","subjects":["tool.result.*"],"stream_name":"AGENT"}}
      ]
    }
  }
}
```

### Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `allowed_tools` | []string | null | Tool allowlist (null/empty = allow all) |
| `timeout` | string | "60s" | Per-tool execution timeout |
| `stream_name` | string | "AGENT" | JetStream stream name |
| `consumer_name_suffix` | string | "" | Suffix for consumer names (for testing) |
| `ports` | object | (defaults) | Port configuration |

### JetStream Integration

All messaging uses JetStream for durability. Tool call subjects require the AGENT stream to exist with subjects matching `tool.execute.>` and `tool.result.>`.

Consumer naming: `agentic-tools-{subject-pattern}`

## Ports

### Inputs

| Name | Type | Subject | Description |
|------|------|---------|-------------|
| tool_calls | jetstream | tool.execute.> | Tool calls from agentic-loop |

### Outputs

| Name | Type | Subject | Description |
|------|------|---------|-------------|
| tool_results | jetstream | tool.result.* | Tool results to agentic-loop |

## Tool Registration

Tools implement the `ToolExecutor` interface:

```go
type ToolExecutor interface {
    Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error)
    ListTools() []agentic.ToolDefinition
}
```

The tool registry follows ADR-029 Pattern A (boot-registry): the embedding binary constructs an `*ExecutorRegistry`, registers builtins and any custom tools at startup, then plumbs the registry through `component.Dependencies.ToolRegistry`. There is no package-level singleton — every process owns its registry explicitly. This mirrors how `component.Registry` is wired in `cmd/semstreams/main.go`.

### Shared Registration (Preferred)

The shared registry is built once at boot and is what all components in the process resolve against. Embedders typically register builtins via `executors.RegisterBuiltins`, then layer custom tools on top:

```go
package main

import (
    "github.com/c360studio/semstreams/component"
    agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
    "github.com/c360studio/semstreams/processor/agentic-tools/executors"
    mytools "example.com/mytools"
)

func bootstrap(ctx context.Context /* ... */) error {
    reg := agentictools.NewExecutorRegistry()

    // Builtin tools (bash, http_request, github_*, rule CRUD, etc.).
    if err := executors.RegisterBuiltins(ctx, reg, executors.ToolDependencies{
        // ...nats client, platform, managers...
    }); err != nil {
        return err
    }

    // Custom tools.
    if err := reg.RegisterTool("read_file", &mytools.FileReader{}); err != nil {
        return err
    }
    if err := reg.RegisterTool("query_graph", &mytools.GraphQueryExecutor{}); err != nil {
        return err
    }

    // Plumb into the dependencies that components see.
    deps := component.Dependencies{
        ToolRegistry: reg,
        // ...nats client, etc...
    }
    _ = deps
    return nil
}
```

Duplicate registration returns an error — boot-time conflicts surface immediately rather than being silently swallowed.

### Per-Component Registration

For component-specific tools, register after creating the component:

```go
comp, _ := agentictools.NewComponent(rawConfig, deps)
toolsComp := comp.(*agentictools.Component)

// Register component-specific executors
toolsComp.RegisterToolExecutor(&CustomExecutor{})

// Start component
lc := comp.(component.LifecycleComponent)
lc.Initialize()
lc.Start(ctx)
```

The component-local registry is dispatched first; the shared registry from `deps.ToolRegistry` is the fallback. **Local beats shared** — register a thin `ToolExecutor` wrapper locally to override or post-process a builtin without disturbing the shared registry that other components see.

### Extending an existing tool (wrapping pattern)

To customise a builtin (e.g., transform `http_request` results before they reach the loop), wrap the inner executor and register the wrapper at the same name on the component-local registry:

```go
type loggedHTTP struct{ inner agentictools.ToolExecutor }

func (l loggedHTTP) ListTools() []agentic.ToolDefinition { return l.inner.ListTools() }

func (l loggedHTTP) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
    res, err := l.inner.Execute(ctx, call)
    // post-process / annotate / metricize / redact / ...
    return res, err
}

// In the binary that owns the component:
shared := /* the deps.ToolRegistry built at boot */
inner := shared.GetTool("http_request")
toolsComp.RegisterToolExecutor(loggedHTTP{inner: inner}) // local wins for this component
```

The shared registry is untouched, so other components in the process still resolve the original `http_request`. This is the recommended path for downstream consumers; see the test file `wrapping_pattern_test.go` for the contract this guarantees.

### Example Implementation

```go
type FileReader struct{}

func (f *FileReader) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
    var args struct {
        Path string `json:"path"`
    }
    if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
        return agentic.ToolResult{CallID: call.ID, Error: err.Error()}, nil
    }

    // Respect context cancellation
    select {
    case <-ctx.Done():
        return agentic.ToolResult{CallID: call.ID, Error: "cancelled"}, ctx.Err()
    default:
    }

    content, err := os.ReadFile(args.Path)
    if err != nil {
        return agentic.ToolResult{CallID: call.ID, Error: err.Error()}, nil
    }

    return agentic.ToolResult{CallID: call.ID, Content: string(content)}, nil
}

func (f *FileReader) ListTools() []agentic.ToolDefinition {
    return []agentic.ToolDefinition{{
        Name:        "read_file",
        Description: "Read the contents of a file",
        Parameters: map[string]any{
            "type": "object",
            "properties": map[string]any{
                "path": map[string]any{"type": "string", "description": "File path"},
            },
            "required": []string{"path"},
        },
    }}
}
```

## Tool Allowlist

Control which tools can execute:

```json
{
  "allowed_tools": ["read_file", "list_dir", "query_graph"]
}
```

| Config | Behavior |
|--------|----------|
| `null` or `[]` | All registered tools allowed |
| `["tool1", "tool2"]` | Only listed tools allowed |

Blocked tools return an error result (not a Go error):

```json
{
  "call_id": "call_001",
  "error": "tool 'delete_file' is not allowed"
}
```

## Message Formats

### ToolCall (Input)

```json
{
  "id": "call_001",
  "name": "read_file",
  "arguments": {
    "path": "/etc/hosts"
  }
}
```

### ToolResult (Output)

```json
{
  "call_id": "call_001",
  "content": "127.0.0.1 localhost\n...",
  "error": "",
  "metadata": {}
}
```

## Common Tools to Implement

| Tool | Description |
|------|-------------|
| `read_file` | Read file contents |
| `write_file` | Write content to file |
| `list_dir` | List directory contents |
| `fetch_url` | HTTP GET request |
| `call_api` | Generic HTTP request |
| `query_graph` | Query knowledge graph |
| `run_command` | Execute shell command |

## Troubleshooting

### Tool not found

- Verify tool executor is registered before Start()
- Check tool name matches exactly (case-sensitive)
- Ensure ListTools() returns the correct name

### Tool timeout

- Increase `timeout` for long-running operations
- Implement context cancellation in executor
- Check for blocking operations

### Tool blocked by allowlist

- Add tool name to `allowed_tools` array
- Set `allowed_tools: null` to allow all
- Verify tool name spelling

### Concurrent execution issues

- Ensure tool executor is thread-safe
- Don't share mutable state between calls
- Use proper synchronization if needed

## Related Components

- [agentic-loop](../agentic-loop/) - Loop orchestration
- [agentic-model](../agentic-model/) - LLM endpoint integration
- [agentic types](../../agentic/) - Shared type definitions
