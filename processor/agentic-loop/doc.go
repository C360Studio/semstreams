// Package agenticloop provides the loop orchestrator for the SemStreams agentic system.
//
// # Overview
//
// The agentic-loop processor orchestrates autonomous agent execution by managing
// the lifecycle of agentic loops. It coordinates communication between the model
// processor (LLM calls) and tools processor (tool execution), tracks state through
// a 10-state machine, supports signal handling for user control, manages context
// memory with automatic compaction, and captures complete execution trajectories
// for observability.
//
// This is the central component of the agentic system - it receives task requests,
// routes messages between model and tools, handles iteration limits, processes
// control signals, manages context memory, and publishes completion events.
//
// # Architecture
//
// The loop orchestrator sits at the center of the agentic component family:
//
//	                  ┌─────────────────┐
//	agent.task.*  ──▶ │                 │ ──▶ agent.request.*
//	                  │  agentic-loop   │
//	agent.response.>◀─│   (this pkg)    │◀── agent.response.*
//	                  │                 │
//	tool.result.>  ──▶│                 │ ──▶ tool.execute.*
//	                  │                 │
//	agent.signal.* ──▶│                 │ ──▶ agent.complete.*
//	                  │                 │
//	                  │                 │ ──▶ agent.context.compaction.*
//	                  └────────┬────────┘
//	                           │
//	                  ┌────────┴────────┐
//	                  │   NATS KV       │
//	                  │  AGENT_LOOPS    │
//	                  │  AGENT_TRAJ...  │
//	                  └─────────────────┘
//
// # Message Flow
//
// A typical loop execution follows this pattern:
//
//  1. External system publishes TaskMessage to agent.task.*
//  2. Loop creates LoopEntity, starts Trajectory, publishes AgentRequest to agent.request.*
//  3. agentic-model processes request, publishes AgentResponse to agent.response.*
//  4. Loop receives response:
//     - If status="tool_call": publishes ToolCall to tool.execute.* for each tool
//     - If status="complete": publishes completion to agent.complete.*
//     - If status="error": marks loop as failed
//  5. agentic-tools executes tools, publishes ToolResult to tool.result.*
//  6. Loop receives tool results, when all complete: increments iteration, sends next request
//  7. Cycle repeats until complete, failed, or max iterations reached
//
// # State Machine
//
// Loops progress through ten states defined in the agentic package:
//
//	exploring → planning → architecting → executing → reviewing → complete
//	     ↑          ↑            ↑             ↑           ↑        ↘ failed
//	     └──────────┴────────────┴─────────────┴───────────┘         ↘ cancelled
//	                                                                  ↘ paused
//	                                                                   ↘ awaiting_approval
//
// States:
//
//   - exploring: Initial state, gathering information
//   - planning: Developing approach
//   - architecting: Designing solution
//   - executing: Implementing solution
//   - reviewing: Validating results
//   - complete: Successfully finished (terminal)
//   - failed: Failed due to error or max iterations (terminal)
//   - cancelled: Cancelled by user signal (terminal)
//   - paused: Paused by user signal, can resume
//   - awaiting_approval: Waiting for user approval
//
// States are fluid checkpoints - the loop can transition backward (e.g., from
// executing back to exploring) to support agent rethinking. Only terminal states
// (complete, failed, cancelled) prevent further transitions.
//
// State transitions are managed by the LoopManager and persisted to NATS KV.
//
// # Signal Handling
//
// The loop accepts control signals via the agent.signal.* input port:
//
//	signal := agentic.UserSignal{
//	    SignalID:    "sig_abc123",
//	    Type:        "cancel",  // cancel, pause, resume, approve, reject, feedback, retry
//	    LoopID:      "loop_456",
//	    UserID:      "user_789",
//	    ChannelType: "cli",
//	    ChannelID:   "session_001",
//	    Timestamp:   time.Now(),
//	}
//
// Signal types and their effects:
//
//   - cancel: Stop execution immediately, transition to cancelled state
//   - pause: Pause at next checkpoint, transition to paused state
//   - resume: Continue paused loop, restore previous state
//   - approve: Approve pending result, transition to complete
//   - reject: Reject with optional reason, transition to failed
//   - feedback: Add feedback without decision, no state change
//   - retry: Retry failed loop, transition to exploring
//
// # Context Management
//
// The loop includes automatic context memory management to handle long-running
// conversations that approach model token limits.
//
// Context is organized into priority regions (lower priority evicted first):
//
//  1. tool_results (priority 1) - Tool execution results, GC'd by age
//  2. recent_history (priority 2) - Recent conversation messages
//  3. hydrated_context (priority 3) - Retrieved context from memory
//  4. compacted_history (priority 4) - Summarized old conversation
//  5. system_prompt (priority 5) - Never evicted
//
// Configuration:
//
//	context := agenticloop.ContextConfig{
//	    Enabled:            true,
//	    CompactThreshold:   0.60,  // Trigger compaction at 60% utilization
//	    HeadroomTokens:     6400,  // Reserve tokens for new content
//	}
//
// Model context limits are resolved from the unified model registry
// (component.Dependencies.ModelRegistry). If a model is not found in
// the registry, DefaultContextLimit (128000) is used as fallback.
//
// Context events are published to agent.context.compaction.*:
//
//   - compaction_starting: Context approaching limit, compaction starting
//   - compaction_complete: Compaction finished, includes tokens saved
//
// # Component Architecture
//
// The package is organized into three main components:
//
// **LoopManager** - Manages loop entity lifecycle:
//
//	manager := NewLoopManager()
//
//	// Create a loop
//	loopID, err := manager.CreateLoop("task_123", "general", "gpt-4", 20)
//
//	// State transitions
//	err = manager.TransitionLoop(loopID, agentic.LoopStateExecuting)
//
//	// Iteration tracking
//	err = manager.IncrementIteration(loopID)
//
//	// Pending tool management
//	manager.AddPendingTool(loopID, "call_001")
//	manager.RemovePendingTool(loopID, "call_001")
//	allDone := manager.AllToolsComplete(loopID)
//
//	// Context management
//	cm := manager.GetContextManager(loopID)
//
// **TrajectoryManager** - Captures execution traces:
//
//	trajManager := NewTrajectoryManager()
//
//	// Start trajectory for a loop
//	trajectory, err := trajManager.StartTrajectory(loopID)
//
//	// Add steps (model calls, tool calls)
//	trajManager.AddStep(loopID, agentic.TrajectoryStep{
//	    Timestamp: time.Now(),
//	    StepType:  "model_call",
//	    TokensIn:  150,
//	    TokensOut: 200,
//	})
//
//	// Complete trajectory
//	trajectory, err = trajManager.CompleteTrajectory(loopID, "complete")
//
// **MessageHandler** - Routes and processes messages:
//
//	handler := NewMessageHandler(config)
//
//	// Handle incoming task
//	result, err := handler.HandleTask(ctx, TaskMessage{
//	    TaskID: "task_123",
//	    Role:   "general",
//	    Model:  "gpt-4",
//	    Prompt: "Analyze this code for bugs",
//	})
//
//	// Handle model response
//	result, err = handler.HandleModelResponse(ctx, loopID, response)
//
//	// Handle tool result
//	result, err = handler.HandleToolResult(ctx, loopID, toolResult)
//
// # Configuration
//
// The processor is configured via JSON:
//
//	{
//	    "max_iterations": 20,
//	    "timeout": "120s",
//	    "stream_name": "AGENT",
//	    "loops_bucket": "AGENT_LOOPS",
//	    "trajectories_bucket": "AGENT_TRAJECTORIES",
//	    "context": {
//	        "enabled": true,
//	        "compact_threshold": 0.60,
//	        "headroom_tokens": 6400,
//	    },
//	    "ports": {
//	        "inputs": [...],
//	        "outputs": [...]
//	    }
//	}
//
// Configuration fields:
//
//   - max_iterations: Maximum loop iterations before failure (default: 20, range: 1-1000)
//   - timeout: Loop execution timeout as duration string (default: "120s")
//   - stream_name: JetStream stream name for agentic messages (default: "AGENT")
//   - loops_bucket: NATS KV bucket for loop state (default: "AGENT_LOOPS")
//   - trajectories_bucket: NATS KV bucket for trajectories (default: "AGENT_TRAJECTORIES")
//   - consumer_name_suffix: Optional suffix for JetStream consumer names (for testing)
//   - context: Context management configuration (see ContextConfig)
//   - ports: Port configuration for inputs and outputs
//
// # Ports
//
// Input ports (JetStream consumers):
//
//   - agent.task: Task requests from external systems (subject: agent.task.*)
//   - agent.response: Model responses from agentic-model (subject: agent.response.>)
//   - tool.result: Tool results from agentic-tools (subject: tool.result.>)
//   - agent.signal: Control signals for loops (subject: agent.signal.*)
//
// Output ports (JetStream publishers):
//
//   - agent.request: Model requests to agentic-model (subject: agent.request.*)
//   - tool.execute: Tool execution requests to agentic-tools (subject: tool.execute.*)
//   - agent.complete: Loop completion events (subject: agent.complete.*)
//   - agent.context.compaction: Context compaction events (subject: agent.context.compaction.*)
//
// KV write ports:
//
//   - loops: Loop entity state (bucket: AGENT_LOOPS)
//   - trajectories: Trajectory data (bucket: AGENT_TRAJECTORIES)
//
// # KV Storage
//
// Loop state and trajectories are persisted to NATS KV for durability and queryability:
//
// **AGENT_LOOPS bucket**: Stores LoopEntity as JSON, keyed by loop ID
//
//	{
//	    "id": "loop_123",
//	    "task_id": "task_456",
//	    "state": "executing",
//	    "role": "general",
//	    "model": "gpt-4",
//	    "iterations": 3,
//	    "max_iterations": 20,
//	    "parent_loop_id": "",
//	    "pause_requested": false,
//	    "user_id": "user_789",
//	    "channel_type": "cli",
//	    "channel_id": "session_001"
//	}
//
// **COMPLETE_{loopID}**: Written when a loop completes, for rules engine consumption
//
//	{
//	    "loop_id": "loop_123",
//	    "task_id": "task_456",
//	    "outcome": "success",
//	    "role": "architect",
//	    "result": "Designed authentication system...",
//	    "model": "gpt-4",
//	    "iterations": 3,
//	    "parent_loop": ""
//	}
//
// **AGENT_TRAJECTORIES bucket**: Stores Trajectory as JSON, keyed by loop ID
//
//	{
//	    "loop_id": "loop_123",
//	    "start_time": "2024-01-15T10:30:00Z",
//	    "end_time": "2024-01-15T10:31:45Z",
//	    "steps": [...],
//	    "outcome": "complete",
//	    "total_tokens_in": 1500,
//	    "total_tokens_out": 800,
//	    "duration": 105000
//	}
//
// KV buckets are created automatically if they don't exist during component startup.
//
// # Rules/Workflow Integration
//
// The loop integrates with the rules engine for orchestration:
//
//  1. On completion, writes COMPLETE_{loopID} key to KV
//  2. Rules engine watches COMPLETE_* keys
//  3. Rules can trigger follow-up actions (e.g., spawn editor when architect completes)
//
// Architect/Editor pattern:
//
//  1. Task arrives with role="architect"
//  2. Architect loop executes and produces a plan
//  3. On completion, COMPLETE_{loopID} written with role="architect"
//  4. Rule matches COMPLETE_* where role="architect"
//  5. Rule spawns new loop with role="editor", parent_loop={loopID}
//  6. Editor receives architect's output as context
//
// # Quick Start
//
// Create and start the component:
//
//	config := agenticloop.Config{
//	    MaxIterations:      20,
//	    Timeout:            "120s",
//	    StreamName:         "AGENT",
//	    LoopsBucket: "AGENT_LOOPS",
//	    Context:    agenticloop.DefaultContextConfig(),
//	}
//
//	rawConfig, _ := json.Marshal(config)
//	comp, err := agenticloop.NewComponent(rawConfig, deps)
//
//	lc := comp.(component.LifecycleComponent)
//	lc.Initialize()
//	lc.Start(ctx)
//	defer lc.Stop(5 * time.Second)
//
// Publish a task:
//
//	task := agenticloop.TaskMessage{
//	    TaskID: "analyze_code",
//	    Role:   "general",
//	    Model:  "gpt-4",
//	    Prompt: "Review main.go for security issues",
//	}
//	taskData, _ := json.Marshal(task)
//	natsClient.PublishToStream(ctx, "agent.task.review", taskData)
//
// # Thread Safety
//
// The LoopManager, TrajectoryManager, and ContextManager are thread-safe, using
// RWMutex for concurrent access. Multiple goroutines can safely:
//
//   - Create and manage different loops concurrently
//   - Read loop state while other loops are being modified
//   - Track pending tools across concurrent tool executions
//   - Add messages to context regions
//
// The Component itself is not designed for concurrent Start/Stop calls.
//
// # Error Handling
//
// Errors are handled at multiple levels:
//
//   - Validation errors: Returned immediately from handlers
//   - State transition errors: Logged, loop may continue or fail depending on severity
//   - Max iterations: Loop transitions to failed state, not returned as error
//   - KV persistence errors: Logged but don't block message processing
//   - Context cancellation: Propagated up, handlers check ctx.Err() early
//
// # Observability
//
// The component provides observability through:
//
//   - Structured logging (slog) for all significant events
//   - Trajectory capture for complete execution audit trails
//   - Context events for memory management visibility
//   - Health status via Health() method
//   - Flow metrics via DataFlow() method
//
// # Testing
//
// For testing, use the ConsumerNameSuffix config option to create unique
// JetStream consumer names per test:
//
//	config := agenticloop.Config{
//	    StreamName:         "AGENT",
//	    ConsumerNameSuffix: "test-" + t.Name(),
//	    // ...
//	}
//
// This prevents consumer name conflicts when running tests in parallel.
//
// # Limitations
//
// Current limitations:
//
//   - No streaming support for partial responses
//   - Trajectory size limited by NATS KV (1MB default)
//   - No built-in retry for failed tool executions
//   - Context summarization requires LLM call (cost consideration)
//
// # See Also
//
// Related packages:
//
//   - agentic: Shared types (LoopEntity, AgentRequest, UserSignal, etc.)
//   - processor/agentic-model: LLM endpoint integration
//   - processor/agentic-tools: Tool execution framework
//   - processor/agentic-dispatch: User message routing
//   - processor/workflow: Multi-step orchestration
package agenticloop
