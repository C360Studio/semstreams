// Package agenticloop provides the loop orchestrator for the SemStreams agentic system.
//
// # Overview
//
// The agentic-loop processor orchestrates autonomous agent execution by managing
// the lifecycle of agentic loops. It coordinates communication between the model
// processor (LLM calls) and tools processor (tool execution), tracks state through
// a 10-state machine, supports signal handling for user control, manages context
// memory with automatic compaction, and appends observed trajectory facts with
// separately stored full evidence.
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
//   - awaiting_approval: Waiting for user approval
//
// States are fluid checkpoints - the loop can transition backward (e.g., from
// executing back to exploring) to support agent rethinking. Only terminal states
// (complete, failed, cancelled) prevent further transitions.
//
// There is no paused state: the framework supports cancellation, durable human
// approval, safe retry/restart and operational quiescing, not arbitrary
// execution pause/resume (owner ruling, #1239, 2026-09-03). Exported
// transitions refuse it and a persisted "paused" record fails validation.
//
// State transitions are managed by the LoopManager and persisted to NATS KV.
//
// # Signal Handling
//
// The loop accepts control signals via the agent.signal.* input port:
//
//	signal := agentic.UserSignal{
//	    SignalID:    "sig_abc123",
//	    Type:        "cancel",  // the only handled verb
//	    LoopID:      "7c9e6679-7425-40de-944b-e07fc1f90ae7",
//	    UserID:      "user_789",
//	    ChannelType: "cli",
//	    ChannelID:   "session_001",
//	    Timestamp:   time.Now(),
//	}
//
// Signal types and their effects:
//
//   - cancel: Stop execution immediately, transition to cancelled state
//
// approve, reject, feedback and retry were advertised here and never handled;
// they were deleted alongside pause/resume (#1239). Approval and rejection are
// real on a different payload: ApprovalResponse over agent.approval_response.*
// (ADR-039).
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
// **Trajectory observations** - The component keeps transient execution detail
// internally while a loop is active. It exposes no aggregate manager or read API.
// Durable reads come from bounded TrajectoryFactV1 observations; full
// TrajectoryEvidenceV1 bodies live in the configured registered Store.
//
// **MessageHandler** - Routes and processes messages:
//
//	handler := NewMessageHandler(config)
//
//	// Handle incoming task. NOTE: this is BELOW the delivery seam, so the
//	// checks the component makes on a decoded delivery — including the
//	// TaskID/LoopID conflict refusal — do not run. An embedder calling the
//	// handler directly owns them.
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
//	    "approval_timeout": "12h",
//	    "trajectory_evidence_storage_instance": "objectstore",
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
//   - approval_timeout: Positive approval wait, at most 12h (default: "12h")
//   - trajectory_evidence_storage_instance: Registered Store instance for full evidence (default: "objectstore")
//   - consumer_name_suffix: Optional suffix for JetStream consumer names (for testing)
//   - context: Context management configuration (see ContextConfig)
//   - ports: Port configuration for inputs and outputs
//
// The loops KV-write output is the sole loop bucket declaration (default AGENT_LOOPS).
// The removed top-level loops_bucket key fails configuration admission. Startup observes
// History 10, TTL 24h and nonbinding MaxBytes before work. The 12h approval limit
// provides nominal grace, not a recovery guarantee.
//
// # Recovery across a process replacement
//
// Nothing in this process survives a replacement. A loop's durable facts are its
// AGENT_LOOPS record and the AgentRequest the stream retains for it, and four
// invariants make the pair decidable (#1330):
//
//   - I1. LoopEntity.PublishedRequestID names the request the loop has outstanding, and
//     while the record exists that exact AgentRequest is retained on agent.request.<loopID>.
//   - I2. The keys of PendingToolResults are the executions already applied against that
//     request - membership only, never rendered content.
//   - I3. Iterations moves only in the update that moves PublishedRequestID.
//   - I4. A PendingApproval names the request the record names.
//
// A replacement therefore classifies a redelivered model response or tool result by
// ORDERING its RequestID against PublishedRequestID, never by comparing conversation
// content: older is acknowledged without effect, newer is retried until the record names
// it, and one naming a request of another loop is quarantined. A delivery naming the
// current request rebuilds the loop from the record plus the retained request rather than
// refusing it; the replayed conversation lands in one region, so compaction attribution
// starts over (docs/concepts/13-agentic-systems.md).
//
// A SECOND delivery of the current request's answer is the one case ordering cannot
// decide, because both deliveries name the request the record names. The outstanding mark
// decides it instead: a response naming the current request while the loop holding it is
// waiting on no request is one this process already used, so it is acknowledged without
// effect and counted already_applied on model_responses_dropped_total. Tool execution
// would survive a second apply — the execution identity is deterministic and
// TOOL_CALL_OUTCOMES replays the outcome — but the loop's conversation would not: the
// assistant turn would be appended again and ride the next request the model is asked to
// answer.
//
// A redelivered TASK is the one lane the record cannot answer on its own. A record naming
// the loop's first request at iteration zero with an empty applied set is rebuilt from the
// task and that request republished - but only when the stream retains NO request for the
// loop, which is the crash window between the record write and the publish. Anything
// retained means the request went out, answered or not, so the task is acknowledged without
// effect, no loop is seated, and the loop is rebuilt by the lane that owns its outstanding
// work: the request's own response, or the first result of the batch that response
// dispatched.
//
// "[Iteration Budget]" and "[Working list" are RESERVED prefixes. A request is not the
// loop's conversation: the loop prepends that iteration's budget line, and when it has a
// working list that block, both Role "system" and both belonging to the one request. The
// rebuild drops the LEADING run of them, because seating them would pin one iteration's
// framing at the top of the rebuilt system prompt for the rest of the loop's life while
// every later request prepends a fresh one. A configured system prompt whose first message
// begins with either string is indistinguishable from that framing and is dropped with it,
// so do not start one with them. Only a leading run is dropped: a message further in is the
// conversation, whatever it says.
//
// A deferred turn is durable as a MARKER, not as the turn. A continuation admitted while
// a request is outstanding writes its text into the loop's context and sets
// PendingContinuation; only the marker reaches the record, so across a process
// replacement the text is not recovered. The rebuild CLEARS the marker with a warning
// rather than leave a loop that would spend an iteration re-asking the model with nothing
// new, and the turn must be re-sent. A turn already inside a retained request is a
// different case and is untouched: that request replays, so the marker still names its
// carrier and still stops the carrier's own completion from settling early.
//
// A turn arriving AFTER the replacement is the same limitation from the other side. A
// continuation reaches only a loop some process holds: a task whose id differs from the one
// the live record names is REFUSED — acknowledged without effect, with a warning naming both
// tasks and a continuation_unheld reason on task_intake_rejections_total — because the loop's
// conversation is in no process's memory and no redelivery of that turn could ever be applied
// here. The test is task identity, so the same refusal also answers a redelivered BIRTH task
// whose record a later continuation moved onto its own id; that one needs no re-send, because
// the turn it carries was applied when the loop was born. Re-send a turn that was never
// applied once a redelivered input has rebuilt the loop.
//
// The loop's TASK PROMPT is the same limitation one field over. taskPrompts is the one
// per-loop cache the WHOLESALE rebuild does not restore, because the record has no field to
// restore it from, so a loop rebuilt from its record and a retained request — the
// model-response and tool-result cold arms — publishes LoopCompletedEvent.Prompt and
// LoopFailedEvent.Prompt EMPTY and recoverEmptyContext falls back to its "Continue with the
// task." placeholder. The cold task arm is the exception: it runs the ordinary HandleTask,
// which caches the redelivered task's prompt, so its terminal events carry it. A consumer
// that reads Prompt off a completion must tolerate an empty one. The durable field
// for the turn and the prompt is https://github.com/C360Studio/semstreams/issues/1365.
//
// A rebuild is not a reprieve. TimeoutAt is written at birth and lives on the record, so a
// rebuilt loop keeps its ORIGINAL deadline; nothing refreshes it and downtime is not
// excluded from it. A replacement whose gap outran that deadline therefore rebuilds the
// loop and then fails it on the first delivery, publishing a terminal on
// agent.failed.<loopID> with the reason "loop timeout exceeded" - and the delivery is
// ACKNOWLEDGED, because the loop settled and nothing is owed. Size a loop's timeout above
// the replacement window you expect to operate under.
//
// An approval deadline is not recovered. PendingApproval is durable, but the timer is the
// snapshot in approval_sweeper.go over the loops this process holds, and no startup pass
// reads the bucket to restore one. A replacement holds a deadline again only for a loop
// some other redelivery rebuilt, and then it is the record's own RequestedAt plus Timeout,
// not a fresh wait. A parked loop otherwise stays in awaiting_approval until the approval
// is answered or the loop is cancelled. The cold approval-response branch - a replacement
// answering an approval for a loop it never started - is #1362.
//
// # Ports
//
// Input ports (JetStream consumers):
//
//   - agent.task: Task requests from external systems (subject: agent.task.*)
//   - agent.response: Model responses from agentic-model (subject: agent.response.>)
//   - tool.result: Tool results from agentic-tools (subject: tool.result.>)
//   - agent.signal: Control signals for loops (subject: agent.signal.*)
//   - trajectory_query: Observed fact queries (subject: agentic.query.trajectory)
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
//   - trajectories: Immutable TrajectoryFactV1 observations (bucket: AGENT_TRAJECTORIES)
//
// # KV Storage
//
// Loop state and observed trajectory facts are persisted to NATS KV:
//
// **AGENT_LOOPS bucket**: Stores LoopEntity as JSON, keyed by loop ID
//
//	{
//	    "id": "7c9e6679-7425-40de-944b-e07fc1f90ae7",
//	    "task_id": "task_456",
//	    "state": "executing",
//	    "role": "general",
//	    "model": "gpt-4",
//	    "iterations": 3,
//	    "max_iterations": 20,
//	    "parent_loop_id": "",
//	    "user_id": "user_789",
//	    "channel_type": "cli",
//	    "channel_id": "session_001"
//	}
//
// **COMPLETE_{loopID}**: Written when a loop completes, for rules engine consumption
//
//	{
//	    "loop_id": "7c9e6679-7425-40de-944b-e07fc1f90ae7",
//	    "task_id": "task_456",
//	    "outcome": "success",
//	    "role": "architect",
//	    "result": "Designed authentication system...",
//	    "model": "gpt-4",
//	    "iterations": 3,
//	    "parent_loop": ""
//	}
//
// **AGENT_TRAJECTORIES bucket**: Stores immutable TrajectoryFactV1 JSON at
// v1.<base32-sha256(loop-id)>.<attempt-id>. It uses history 1 and no TTL. Readers
// prefix-list visible facts, validate them, causally sort them, and report only
// coverage="observed" plus observed totals.
//
//	{
//	    "schema_version": "v1",
//	    "loop_digest": "<sha256>",
//	    "attempt_id": "01J...",
//	    "attempt_ordinal": 3,
//	    "kind": "model.completed",
//	    "causal_iteration": 2,
//	    "causal_phase": "model_result",
//	    "evidence_capture": "stored",
//	    "evidence": {"storage_instance":"objectstore", "key":"trajectory-evidence/v1/sha256/<sha256>"}
//	}
//
// Full prompts, messages, tool arguments/results, URLs, and raw errors live in
// content-addressed TrajectoryEvidenceV1 bodies borrowed through StoreRegistry.
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
//	config := agenticloop.DefaultConfig()
//
//	rawConfig, _ := json.Marshal(config)
//	comp, err := agenticloop.NewComponent(rawConfig, deps)
//
//	lc := comp.(component.LifecycleComponent)
//	lc.Initialize()
//	lc.Start(ctx)
//	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
//	defer shutdownCancel()
//	_ = lc.Stop(shutdownCtx)
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
// The LoopManager, internal active-loop detail, and ContextManager are thread-safe, using
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
//   - Append-only observed facts with full evidence stored by digest and returned by reference only
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
//   - Trajectory facts are internally bounded below 8 KiB; full evidence requires a registered Store
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
