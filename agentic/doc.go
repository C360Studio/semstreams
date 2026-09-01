// Package agentic provides shared types for the SemStreams agentic processing system.
//
// # Overview
//
// The agentic package defines the foundational types used by the agentic component family:
// agentic-loop (orchestration), agentic-model (LLM integration), and agentic-tools (tool
// execution). These components work together to enable autonomous, tool-using AI agents
// that can execute complex multi-step tasks.
//
// This package provides:
//
//   - Request/Response types for agent communication (AgentRequest, AgentResponse)
//   - State machine types for loop lifecycle (LoopState, LoopEntity)
//   - Tool system types (ToolDefinition, ToolCall, ToolResult)
//   - Immutable trajectory facts/evidence plus transient active-loop helper types
//
// # Architecture Context
//
// The agentic system uses a three-component architecture communicating over NATS JetStream:
//
//	┌─────────────────┐
//	│  agentic-loop   │  Orchestrates the agent lifecycle
//	│  (state machine)│  Manages state, routes messages, records observations
//	└────────┬────────┘
//	         │
//	    ┌────┴────┐
//	    │         │
//	    ▼         ▼
//	┌────────┐  ┌────────────┐
//	│agentic-│  │agentic-    │
//	│model   │  │tools       │
//	│(LLM)   │  │(execution) │
//	└────────┘  └────────────┘
//
// All three components share the types defined in this package, ensuring consistent
// serialization and validation across the system.
//
// # Request/Response Types
//
// AgentRequest encapsulates a request to an LLM endpoint:
//
//	request := agentic.AgentRequest{
//	    RequestID: "req_001",
//	    LoopID:    "7c9e6679-7425-40de-944b-e07fc1f90ae7",
//	    Role:      "general",  // or "architect", "editor"
//	    Model:     "gpt-4",
//	    Messages: []agentic.ChatMessage{
//	        {Role: "system", Content: "You are a helpful assistant."},
//	        {Role: "user", Content: "Analyze this code for bugs."},
//	    },
//	    Tools: []agentic.ToolDefinition{
//	        {Name: "read_file", Description: "Read file contents", Parameters: schema},
//	    },
//	}
//
// AgentResponse captures the LLM's response:
//
//	response := agentic.AgentResponse{
//	    RequestID: "req_001",
//	    Status:    "complete",  // or "tool_call", "error"
//	    Message: agentic.ChatMessage{
//	        Role:    "assistant",
//	        Content: "I found 3 potential issues...",
//	    },
//	    TokenUsage: agentic.TokenUsage{
//	        PromptTokens:     150,
//	        CompletionTokens: 200,
//	    },
//	}
//
// # ChatMessage Roles
//
// The ChatMessage type supports four roles following the OpenAI convention:
//
//   - "system": System instructions that shape agent behavior
//   - "user": User input or task descriptions
//   - "assistant": Agent responses (from the LLM)
//   - "tool": Results from tool execution
//
// Messages with tool calls use the ToolCalls field instead of Content:
//
//	assistantMessage := agentic.ChatMessage{
//	    Role: "assistant",
//	    ToolCalls: []agentic.ToolCall{
//	        {ID: "call_001", Name: "read_file", Arguments: map[string]any{"path": "main.go"}},
//	    },
//	}
//
// # Agent Roles
//
// The agentic system supports three agent roles for different task patterns:
//
//   - "general": Standard single-agent execution for most tasks
//   - "architect": High-level planning and design (used with editor split)
//   - "editor": Implementation based on architect's plan
//
// The architect/editor split enables complex tasks where planning and execution
// benefit from separation. The loop orchestrator handles spawning editor loops
// when an architect completes.
//
// # State Machine
//
// LoopState represents the lifecycle of an agentic loop with seven states:
//
//	exploring → planning → architecting → executing → reviewing → complete
//	                                                           ↘ failed
//
// States are fluid checkpoints, not gates. The loop can move backward (e.g.,
// from executing back to exploring if the agent needs to rethink). Only the
// terminal states (complete, failed) prevent further transitions.
//
// Create and manage loop entities:
//
//	entity := agentic.NewLoopEntity("7c9e6679-7425-40de-944b-e07fc1f90ae7", "task_456", "general", "gpt-4")
//
//	// State transitions
//	entity.TransitionTo(agentic.LoopStatePlanning)
//	entity.TransitionTo(agentic.LoopStateExecuting)
//
//	// Iteration tracking (with guard)
//	if err := entity.IncrementIteration(); err != nil {
//	    // Max iterations reached
//	}
//
//	// Check terminal state
//	if entity.State.IsTerminal() {
//	    // Loop has finished
//	}
//
// # Tool System
//
// The tool system enables agents to interact with external systems through
// a well-defined interface.
//
// ToolDefinition describes an available tool:
//
//	toolDef := agentic.ToolDefinition{
//	    Name:        "read_file",
//	    Description: "Read the contents of a file",
//	    Parameters: map[string]any{
//	        "type": "object",
//	        "properties": map[string]any{
//	            "path": map[string]any{"type": "string", "description": "File path"},
//	        },
//	        "required": []string{"path"},
//	    },
//	}
//
// ToolCall represents a request from the LLM to execute a tool:
//
//	call := agentic.ToolCall{
//	    ID:        "call_001",
//	    Name:      "read_file",
//	    Arguments: map[string]any{"path": "/etc/hosts"},
//	}
//
// ToolResult returns the outcome of tool execution:
//
//	result := agentic.ToolResult{
//	    CallID:  "call_001",
//	    Content: "127.0.0.1 localhost\n...",
//	    // Or on error:
//	    // Error: "file not found",
//	}
//
// # Tool Validation
//
// The ValidateToolsAllowed function checks tool calls against an allowlist:
//
//	allowed := []string{"read_file", "write_file", "list_dir"}
//	calls := []agentic.ToolCall{{ID: "1", Name: "delete_file"}}
//
//	if err := agentic.ValidateToolsAllowed(calls, allowed); err != nil {
//	    // Error: "disallowed tools: delete_file"
//	}
//
// # Trajectory Tracking
//
// Durable trajectory history consists of immutable TrajectoryFactV1 observations.
// Each fact is bounded and body-free; full prompts, messages, tool arguments/results,
// URLs, and raw errors belong in content-addressed TrajectoryEvidenceV1 storage.
//
// Construct one observed fact:
//
//	fact := agentic.TrajectoryFactV1{
//	    SchemaVersion:   agentic.TrajectorySchemaV1,
//	    LoopDigest:      agentic.TrajectoryLoopDigest("7c9e6679-7425-40de-944b-e07fc1f90ae7"),
//	    AttemptID:       "attempt1",
//	    AttemptOrdinal:  1,
//	    Kind:            agentic.TrajectoryKindModelCompleted,
//	    CausalPhase:     agentic.TrajectoryPhaseModelResult,
//	    ObservedAt:      time.Now(),
//	    EvidenceCapture: agentic.TrajectoryEvidenceNone,
//	}
//	encoded, err := fact.CanonicalBytes()
//
// One or more terminal facts mean terminal outcomes were observed; they are not
// seals or completeness proofs. Aggregate Trajectory and TrajectoryStep remain
// transient execution helpers, not durable/public read authority.
//
// # Token Usage
//
// TokenUsage tracks token consumption for cost monitoring and rate limiting:
//
//	usage := agentic.TokenUsage{
//	    PromptTokens:     500,
//	    CompletionTokens: 250,
//	}
//	total := usage.Total() // 750
//
// # Model Configuration
//
// ModelConfig provides default values for LLM parameters:
//
//	config := agentic.ModelConfig{
//	    Temperature: 0.7,
//	    MaxTokens:   2048,
//	}
//
//	// Apply defaults (Temperature: 0.2, MaxTokens: 4096)
//	config = config.WithDefaults()
//
// # Validation
//
// All types include Validate() methods for input validation:
//
//	request := agentic.AgentRequest{...}
//	if err := request.Validate(); err != nil {
//	    // Handle validation error
//	}
//
// Validation rules:
//
//   - AgentRequest: requires request_id, at least one message, valid role
//   - AgentResponse: requires valid status (complete, tool_call, error)
//   - ChatMessage: requires valid role, either content, reasoning_content, or tool_calls
//   - ToolDefinition: requires name and parameters
//   - ToolCall: requires id and name
//   - ToolResult: requires call_id
//   - TrajectoryStep: requires valid step_type and timestamp
//   - LoopEntity: requires id, valid state, positive max_iterations
//
// # Integration with NATS
//
// The agentic components communicate via NATS JetStream using these subject patterns:
//
//	agent.task.*       - Task requests (external → loop)
//	agent.request.*    - Model requests (loop → model)
//	agent.response.*   - Model responses (model → loop)
//	tool.execute.*     - Tool execution (loop → tools)
//	tool.result.*      - Tool results (tools → loop)
//	agent.complete.*   - Completions (loop → external)
//
// All subjects use the AGENT stream for reliable delivery.
//
// # KV Storage
//
// Loop state and observed trajectory facts are persisted to NATS KV buckets:
//
//   - AGENT_LOOPS: LoopEntity per loop ID
//   - AGENT_TRAJECTORIES: TrajectoryFactV1 per immutable attempt key
//
// AGENT_TRAJECTORIES uses history 1 and no TTL. Full evidence is stored through a
// registered storage.Store and referenced by digest, exact size, content type, and
// logical storage instance.
//
// # Thread Safety
//
// Types in this package are not inherently thread-safe. When used concurrently,
// external synchronization is required. The agentic-loop component provides
// thread-safe loop/context managers and private active-loop mechanics that wrap these types.
//
// # Error Handling
//
// Validation errors are returned as standard Go errors with descriptive messages.
// Tool execution errors are captured in ToolResult.Error rather than returned
// as Go errors, allowing the agent to handle them gracefully.
//
// # Limitations
//
// Current limitations of the agentic type system:
//
//   - No streaming support (responses are complete documents)
//   - Tool parameters use map[string]any (no strong typing)
//   - Fact envelopes are strictly smaller than 8 KiB
//   - Visible facts are observed coverage, never a complete execution claim
//
// # See Also
//
// Related packages:
//
//   - processor/agentic-loop: Loop orchestration and state management
//   - processor/agentic-model: LLM endpoint integration
//   - processor/agentic-tools: Tool execution framework
package agentic
