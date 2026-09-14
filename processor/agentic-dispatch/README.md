# Agentic Dispatch Component

An edge gateway between users and agentic loops, with command parsing and permission checks.

## Overview

The agentic-dispatch component is the central hub for user interaction with the agentic system. It:

- Parses commands from user messages
- Checks permissions before executing commands
- Reads current loop state from durable authority
- Routes tasks to the agentic-loop component
- Delivers responses back to users

Agentic-loop owns loop creation, approval waits, intermediate transitions, and terminal state. Dispatch does not
maintain another state machine or a `LoopTracker`. Explicit LoopID operations read the durable loop record;
listing, activity, debug, and AutoContinue share one caught-up read-only view. A restart rehydrates that view from
current state without replaying already-acknowledged creation or approval notifications.

## Configuration

```json
{
  "default_role": "general",
  "default_model": "qwen2.5-coder:32b",
  "auto_continue": false,
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
| `auto_continue` | bool | false | Opt into attachment to an active loop and implicit command targeting |
| `stream_name` | string | "USER" | JetStream stream for user messages |
| `permissions` | object | (see above) | Permission configuration |

### Chat turns and explicit attachment

By default, a message without `reply_to` starts an independent loop, even when another loop is active. For sequential
chat, send the displayed user/assistant transcript in optional `prior_messages` and the new turn in `content`.
Dispatch commits that history into the task; the loop can reconstruct its input after replacement. No loop ID is
needed for an independent turn. See [Agentic Systems](../../docs/concepts/13-agentic-systems.md#context-management)
for the HTTP request example and transcript ownership.

History is ordered, nonempty user/assistant text only. Omitted, null, and empty history are equivalent. Use delivered
`UserResponse.Content` for assistant entries, which may differ from raw model output. Invalid history returns an
error; it is not silently dropped.

Explicit `reply_to` and `auto_continue: true` retain their live-loop attachment behavior, but cannot be combined with
nonempty history. Commands do not accept history. Under defaults, commands that act on a loop require an explicit
loop ID, such as `/cancel <loop_id>`; enabling AutoContinue also opts into its implicit command target selection.

AutoContinue matches the exact `(UserID, ChannelType, ChannelID)` tuple: one nonterminal match continues, no match
starts work, and multiple matches refuse as ambiguous. An unavailable view is not an empty view. Between task
publication and the first durable loop record, a second route-only submission may create another loop. Echo the
returned LoopID when continuity is required; dispatch does not create a separate route claim to conceal that gap.

### JetStream Integration

All messaging uses JetStream for durability:

- User messages consumed from USER stream
- Agent tasks published to AGENT stream
- Completion events consumed from AGENT stream

Terminal responses are projected from the registered production envelope, not
from the physical subject name. Success becomes a `result`, failure an `error`,
and cancellation a `status`. Dispatch validates `ChannelType`, `ChannelID`, and
optional `UserID` against the terminal event and exact persisted loop record.
Conflicting nonempty route fields are refused. A response requires the channel type and channel ID;
`UserID` is optional metadata. The loops bucket comes from the declared
`agent_loops` KV read port (default `AGENT_LOOPS`), so a non-default bucket is
bound in configuration rather than assumed.

Dispatch consumes only user messages and terminal complete/failed work. The loop still publishes `agent.created`
and `agent.approval_pending` for external subscribers, but dispatch does not consume them to reconstruct state.

Which terminal is the USER's answer follows the typed decision, not route
ownership (ADR-101). A `decide` terminal whose action is `respond_direct` is
published as a `result` and `ask_user` as a `prompt`, each carrying the
decision's reason as content; any other decide action is a handoff to a rule
chain and publishes nothing, even when the deciding loop owns a route. A
terminal with no decision keeps the route-ownership behaviour above. When a
reply decision's own loop owns no route — the rule-spawned case — dispatch
resolves the origin from persisted ancestry (typed-first through `RunID`, then
the `ParentLoopID` chain, bounded at 32 hops), never from the process tracker.

Settlement reasons: `response_settled`, `route_less_settled` (no origin
existed), `handoff_settled` (a non-reply decision), `origin_unresolvable` (a
durable link pointed at an unobservable record; parent chain AND run anchors
exhausted), `terminal_route_unavailable`, `routing_read_transient`, `response_publish_transient`,
`routing_malformed`, `routing_collision_or_malformed`,
plus the decode-rejection reasons.

Dispatch ACKs a terminal only after any required `UserResponse` receives a
synchronous JetStream PubAck. Its response ID and `Nats-Msg-Id` are both
`terminal-user-response:<terminal BaseMessage ID>`, so a redelivery is stable
inside the USER stream duplicate window. Transient KV reads are delayed-NAKed.
An ambiguous response publication leaves the source unsettled and stops its exact delivery owner;
it is not assumed safe to retry immediately. Malformed terminals and routing collisions are Termed, and the
terminal consumers have unlimited attempts only while the source remains in
AGENT. See [Agent terminal settlement](../../docs/operations/38-agent-terminal-settlement.md).

Terminal response reconstruction requires both the retained source event and its exact routing records. Unreadable
authority remains retryable; a confirmed absent own loop record is reported as unavailable, never reconstructed
from memory.
A validated system-lane terminal with no user route settles without publishing a user response.

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
    "github.com/c360studio/semstreams/agentic"
    agenticdispatch "github.com/c360studio/semstreams/processor/agentic-dispatch"
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
    // Use cmdCtx.LookupLoopOwner(ctx, loopID) to check recorded ownership
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
    NATSClient      *natsclient.Client                   // Publish NATS messages
    LookupLoopOwner LoopOwnerLookup                      // Exact LoopID/owner lookup
    Logger          *slog.Logger                         // Structured logging
    HasPermission   func(userID, permission string) bool  // Check permissions
}
```

`LookupLoopOwner(ctx, loopID)` returns only the recorded LoopID and UserID. Invalid ID, absent loop, missing owner,
invalid record, and unavailable storage are distinct classified failures. Custom commands do not receive a tracker,
mutable entity, bucket handle, or general query interface.

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
| `agent.signal.{loop_id}` | Publish | Signals (cancel) |
| `agent.complete.{loop_id}` | Subscribe | Success and cancellation terminal events |
| `agent.failed.{loop_id}` | Subscribe | Failure terminal events |

## Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `router_messages_received_total` | counter | `channel_type` | Messages received |
| `router_commands_executed_total` | counter | `command` | Commands executed |
| `router_tasks_submitted_total` | counter | | Tasks submitted |
| `router_routing_duration_seconds` | histogram | | Message routing latency |
| `router_terminal_settlement_total` | counter | `reason` | Terminal validation/routing/publication disposition |

The former `semstreams_router_active_loops` gauge is removed. Use `/loops` while the shared view is caught up;
the separate loop execution gauge is process-local telemetry, not a durable loop count. `/loops` and `/debug/state`
return 503 when current-state truth is unavailable instead of reporting zero loops. Debug output includes readiness
and current poison diagnostics. See the [migration notes](../../docs/operations/migration-beta162-to-beta163.md).

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
