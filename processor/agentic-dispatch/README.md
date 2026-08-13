# Agentic Dispatch Component

Message routing between users and agentic loops with command parsing, permissions, and loop tracking.

## Overview

The agentic-dispatch component is the central hub for user interaction with the agentic system. It:

- Parses commands from user messages
- Checks permissions before executing commands
- Tracks active loops per user and channel
- Routes tasks to the agentic-loop component
- Delivers responses back to users

## Configuration

```json
{
  "default_role": "general",
  "default_model": "qwen2.5-coder:32b",
  "auto_continue": true,
  "stream_name": "USER",
  "permissions": {
    "view": ["*"],
    "submit_task": ["*"],
    "cancel_own": true,
    "cancel_any": ["admin"],
    "approve": ["admin", "reviewer"]
  }
}
```

### Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `default_role` | string | "general" | Default role for tasks |
| `default_model` | string | "" | Default model for tasks |
| `auto_continue` | bool | true | Auto-continue conversations |
| `stream_name` | string | "USER" | JetStream stream for user messages |
| `permissions` | object | (see above) | Permission configuration |

### JetStream Integration

All messaging uses JetStream for durability:

- User messages consumed from USER stream
- Agent tasks published to AGENT stream
- Completion events consumed from AGENT stream

Terminal responses are projected from the registered production envelope, not
from the physical subject name. Success becomes a `result`, failure an `error`,
and cancellation a `status`. Dispatch merges `ChannelType`, `ChannelID`, and
optional `UserID` independently from the terminal event, the local tracker, and
`AGENT_LOOPS/<loopID>`. A response requires the channel type and channel ID;
`UserID` is optional metadata.

Dispatch ACKs a terminal only after any required `UserResponse` receives a
synchronous JetStream PubAck. Its response ID and `Nats-Msg-Id` are both
`terminal-user-response:<terminal BaseMessage ID>`, so a redelivery is stable
inside the USER stream duplicate window. Transient KV/publication failures are
delayed-NAKed, malformed terminals and routing collisions are Termed, and the
terminal consumers have unlimited attempts only while the source remains in
AGENT. See [Agent terminal settlement](../../docs/operations/38-agent-terminal-settlement.md).

Consumer naming: `agentic-dispatch-{port-name}`

## Built-in Commands

| Command | Permission | Description |
|---------|------------|-------------|
| `/cancel [id]` | `cancel_own` | Cancel current or specified loop |
| `/status [id]` | `view` | Show loop status |
| `/loops` | `view` | List your active loops |
| `/help` | (none) | Show available commands |

## Custom Command Registration

External packages can register custom commands using the global `init()` pattern:

```go
package semspec

import (
    "context"
    "github.com/c360/semstreams/agentic"
    agenticdispatch "github.com/c360/semstreams/processor/agentic-dispatch"
)

func init() {
    agenticdispatch.RegisterCommand("spec", &SpecCommand{})
}

type SpecCommand struct{}

func (c *SpecCommand) Config() agenticdispatch.CommandConfig {
    return agenticdispatch.CommandConfig{
        Pattern:     `^/spec\s*(.*)$`,
        Permission:  "submit_task",
        RequireLoop: false,
        Help:        "/spec [name] - Run spec-driven development",
    }
}

func (c *SpecCommand) Execute(
    ctx context.Context,
    cmdCtx *agenticdispatch.CommandContext,
    msg agentic.UserMessage,
    args []string,
    loopID string,
) (agentic.UserResponse, error) {
    // Use cmdCtx.NATSClient to publish messages
    // Use cmdCtx.LoopTracker to track loops
    // Use cmdCtx.HasPermission for permission checks
    // Use cmdCtx.Logger for logging

    return agentic.UserResponse{
        ResponseID:  "...",
        ChannelType: msg.ChannelType,
        ChannelID:   msg.ChannelID,
        UserID:      msg.UserID,
        Type:        agentic.ResponseTypeStatus,
        Content:     "Spec workflow started",
    }, nil
}
```

## CommandExecutor Interface

```go
type CommandExecutor interface {
    Execute(ctx context.Context, cmdCtx *CommandContext, msg agentic.UserMessage, args []string, loopID string) (agentic.UserResponse, error)
    Config() CommandConfig
}
```

## CommandContext

The `CommandContext` provides access to agentic-dispatch services:

```go
type CommandContext struct {
    NATSClient    *natsclient.Client                      // Publish NATS messages
    LoopTracker   *LoopTracker                            // Track active loops
    Logger        *slog.Logger                            // Structured logging
    HasPermission func(userID, permission string) bool    // Check permissions
}
```

## CommandConfig

```go
type CommandConfig struct {
    Pattern     string  // Regex pattern with capture groups for args
    Permission  string  // Required permission (empty = no permission required)
    RequireLoop bool    // Whether command requires an active loop
    Help        string  // Help text shown in /help output
}
```

## NATS Subjects

| Subject | Direction | Description |
|---------|-----------|-------------|
| `user.message.{channel}.{id}` | Subscribe | User input from channels |
| `user.response.{channel}.{id}` | Publish | Responses to users |
| `agent.task.{task_id}` | Publish | Task dispatch |
| `agent.signal.{loop_id}` | Publish | Signals (cancel, pause) |
| `agent.complete.{loop_id}` | Subscribe | Success and cancellation terminal events |
| `agent.failed.{loop_id}` | Subscribe | Failure terminal events |

## Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `router_messages_received_total` | counter | `channel_type` | Messages received |
| `router_commands_executed_total` | counter | `command` | Commands executed |
| `router_tasks_submitted_total` | counter | | Tasks submitted |
| `router_loops_active` | gauge | | Currently active loops |
| `router_routing_duration_seconds` | histogram | | Message routing latency |
| `router_terminal_settlement_total` | counter | `reason` | Terminal validation/routing/publication disposition |

## Integration Example

```go
// Register commands before creating agentic-dispatch component
func init() {
    agenticdispatch.RegisterCommand("mycommand", &MyCommand{})
}

// Commands are automatically loaded when component starts
comp, _ := agenticdispatch.NewComponent(config, deps)
comp.Start(ctx)
```

## See Also

- [CLI Input Component](../../input/cli/README.md) - Terminal interface
- [Agentic Components](../../docs/advanced/08-agentic-components.md) - Loop, model, tools
- [Input Router Spec](../../docs/architecture/specs/semstreams-input-router-spec.md) - Full specification
