// Package agenticdispatch provides message routing between users and agentic loops.
//
// The agentic-dispatch component handles command parsing, permission checking,
// current-loop reads, and message dispatch. It bridges input components (CLI, Slack,
// Discord, Web) with the agentic processing system.
// Explicit loop operations read validated durable state; listing and AutoContinue
// use one caught-up read-only view. Dispatch does not own intermediate loop state.
//
// # Architecture
//
//	┌─────────────┐                           ┌─────────────────┐
//	│  CLI/Slack/ │     user.message.*        │                 │
//	│  Discord/   │ ─────────────────────────▶│ agentic-dispatch│
//	│  Web Input  │                           │                 │
//	│             │◀───────────────────────── │  • Commands     │
//	└─────────────┘     user.response.*       │  • Perms        │
//	                                          │  • Loops        │
//	                                          └────────┬────────┘
//	                                                   │
//	                                                   │ agent.task.*
//	                                                   │ agent.signal.*
//	                                                   ▼
//	                                          ┌─────────────┐
//	                                          │ agentic-    │
//	                                          │ loop        │
//	                                          └─────────────┘
//
// # Command Registration
//
// Commands can be registered in two ways:
//
// 1. Global registration via init() - preferred for reusable commands:
//
//	package mycommands
//
//	import agenticdispatch "github.com/c360studio/semstreams/processor/agentic-dispatch"
//
//	func init() {
//	    agenticdispatch.RegisterCommand("mycommand", &MyCommandExecutor{})
//	}
//
// 2. Per-component registration - for component-specific commands:
//
//	registry := dispatchComponent.CommandRegistry()
//	registry.Register("local", config, handler)
//
// # CommandExecutor Interface
//
// External commands implement the CommandExecutor interface:
//
//	type CommandExecutor interface {
//	    Execute(ctx context.Context, cmdCtx *CommandContext, msg agentic.UserMessage, args []string, loopID string) (agentic.UserResponse, error)
//	    Config() CommandConfig
//	}
//
// The CommandContext provides access to dispatch services:
//
//	type CommandContext struct {
//	    NATSClient      *natsclient.Client     // For publishing messages
//	    LookupLoopOwner LoopOwnerLookup       // For exact loop ownership reads
//	    Logger          *slog.Logger           // For logging
//	    HasPermission   func(userID, permission string) bool // For permission checks
//	}
//
// LookupLoopOwner returns only LoopID and UserID. Invalid IDs, absent loops,
// missing ownership, invalid records, and unavailable storage are distinct
// classified failures, not interchangeable permission answers.
//
// # Built-in Commands
//
// The agentic-dispatch component provides these built-in commands:
//
//   - /cancel [loop_id] - Cancel current or specified loop
//   - /status [loop_id] - Show loop status
//   - /loops - List active loops
//   - /help - Show available commands
//
// # Permissions
//
// Commands can require permissions:
//
//   - view - View status, loops, history
//   - submit_task - Submit new tasks
//   - cancel_own - Cancel own loops
//   - cancel_any - Cancel any loop (admin)
//   - approve - Approve/reject results
//
// # NATS Subjects
//
// The agentic-dispatch component uses these subject patterns:
//
//   - user.message.{channel}.{id} - Incoming user messages
//   - user.response.{channel}.{id} - Outgoing responses
//   - agent.task.{task_id} - Task dispatch to agentic-loop
//   - agent.signal.{loop_id} - Signals to agentic-loop
//   - agent.complete.{loop_id} - Completion events from agentic-loop
//
// # JetStream Integration
//
// All messaging uses JetStream for durability:
//
//   - User messages consumed from USER stream
//   - Agent tasks published to AGENT stream
//   - Completion events consumed from AGENT stream
//
// Consumer naming follows the pattern: agentic-dispatch-{port-name}
//
// Default stream is USER (not AGENT) since this component bridges user
// input to the agentic system.
package agenticdispatch
