// Package agentictools provides the tool execution processor for the SemStreams agentic system.
//
// # Overview
//
// The agentic-tools processor executes tool calls from the agentic loop orchestrator.
// It receives ToolCall messages, dispatches them to registered tool executors, and
// publishes ToolResult messages back. The processor supports tool registration,
// allowlist filtering, per-execution timeouts, correlated results, and durable
// completion.
//
// This processor enables agents to interact with external systems (files, APIs,
// databases, etc.) through a well-defined tool interface.
//
// # Architecture
//
// The tools processor sits between the loop orchestrator and tool implementations:
//
//	┌───────────────┐     ┌────────────────┐     ┌──────────────────┐
//	│ agentic-loop  │────▶│ agentic-tools  │────▶│ Tool Executors   │
//	│               │     │ (this pkg)     │     │ (your code)      │
//	│               │◀────│                │◀────│                  │
//	└───────────────┘     └────────────────┘     └──────────────────┘
//	  tool.execute.*        Execute()           read_file, query_db,
//	  tool.result.*                             call_api, etc.
//
// # ToolExecutor Interface
//
// Tools are implemented by satisfying the ToolExecutor interface:
//
//	type ToolExecutor interface {
//	    Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error)
//	    ListTools() []agentic.ToolDefinition
//	}
//
// Example implementation:
//
//	type FileReader struct{}
//
//	func (f *FileReader) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
//	    path, _ := call.Arguments["path"].(string)
//
//	    // Respect context cancellation
//	    select {
//	    case <-ctx.Done():
//	        return agentic.ToolResult{CallID: call.ID, Error: "cancelled"}, ctx.Err()
//	    default:
//	    }
//
//	    content, err := os.ReadFile(path)
//	    if err != nil {
//	        return agentic.ToolResult{
//	            CallID: call.ID,
//	            Error:  err.Error(),
//	        }, nil
//	    }
//
//	    return agentic.ToolResult{
//	        CallID:  call.ID,
//	        Content: string(content),
//	    }, nil
//	}
//
//	func (f *FileReader) ListTools() []agentic.ToolDefinition {
//	    return []agentic.ToolDefinition{
//	        {
//	            Name:        "read_file",
//	            Description: "Read the contents of a file",
//	            Parameters: map[string]any{
//	                "type": "object",
//	                "properties": map[string]any{
//	                    "path": map[string]any{
//	                        "type":        "string",
//	                        "description": "Path to the file",
//	                    },
//	                },
//	                "required": []string{"path"},
//	            },
//	        },
//	    }
//	}
//
// # Tool Registration
//
// The tool registry is a constructor-injected ADR-029 Pattern A registry,
// matching component.Registry. The embedding binary owns one
// *ExecutorRegistry per process and plumbs it through
// component.Dependencies.ToolRegistry. There is no package-level singleton.
//
// Tools can be registered in two ways:
//
// 1. Shared registration (preferred for tools used across components):
//
//		reg := agentictools.NewExecutorRegistry()
//		if err := reg.RegisterTool("my_tool", &MyToolExecutor{}); err != nil {
//		    return err
//		}
//		deps := component.Dependencies{ToolRegistry: reg}
//
//	 2. Per-component registration (for component-specific overrides or
//	    wrappers that should not be visible to other components):
//
//	    comp, _ := agentictools.NewComponent(rawConfig, deps)
//	    toolsComp := comp.(*agentictools.Component)
//	    err := toolsComp.RegisterToolExecutor(&FileReader{})
//
// Component-local registrations beat the shared registry for the same tool
// name — see wrapping_pattern_test.go for the precedence contract.
//
// The processor extracts tool names from ListTools() for routing and validation.
//
// # ExecutorRegistry
//
// The ExecutorRegistry provides thread-safe tool management:
//
//	registry := NewExecutorRegistry()
//
//	// Register tools
//	registry.RegisterTool("read_file", &FileReader{})
//	registry.RegisterTool("query_db", &DatabaseQuerier{})
//
//	// Get executor by name
//	executor := registry.GetTool("read_file")
//
//	// List all available tools
//	tools := registry.ListTools()
//
//	// Execute a tool call
//	result, err := registry.Execute(ctx, toolCall)
//
// The registry prevents duplicate registrations and returns descriptive errors
// for missing tools.
//
// # Tool Allowlist
//
// The processor supports allowlist filtering for security and control:
//
//	config := agentictools.Config{
//	    AllowedTools: []string{"read_file", "list_dir"},  // Only these allowed
//	    // ...
//	}
//
// Behavior:
//
//   - Empty/nil AllowedTools: All registered tools are allowed
//   - Populated AllowedTools: Only listed tools can execute
//   - Blocked tools return an error result (not a Go error)
//
// Example blocked response:
//
//	result := agentic.ToolResult{
//	    CallID: "call_001",
//	    Error:  "tool 'delete_file' is not allowed",
//	}
//
// # Timeout Handling
//
// Each tool execution runs with a configurable timeout:
//
//	config := agentictools.Config{
//	    Timeout: "60s",  // Per-tool execution timeout
//	    // ...
//	}
//
// The timeout is enforced via context cancellation. Tool implementations
// should respect ctx.Done() for proper cancellation:
//
//	func (t *SlowTool) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
//	    select {
//	    case <-ctx.Done():
//	        return agentic.ToolResult{CallID: call.ID, Error: "execution cancelled"}, ctx.Err()
//	    case result := <-t.doWork(call):
//	        return result, nil
//	    }
//	}
//
// Timeout errors are returned as ToolResult.Error, not as Go errors.
//
// # Quick Start
//
// Configure and start the processor:
//
//	config := agentictools.Config{
//	    StreamName:   "AGENT",
//	    AllowedTools: nil,  // Allow all
//	    Timeout:      "60s",
//	}
//
//	rawConfig, _ := json.Marshal(config)
//	comp, err := agentictools.NewComponent(rawConfig, deps)
//
//	// Register tools
//	toolsComp := comp.(*agentictools.Component)
//	toolsComp.RegisterToolExecutor(&FileReader{})
//	toolsComp.RegisterToolExecutor(&WebFetcher{})
//
//	// Start
//	lc := comp.(component.LifecycleComponent)
//	lc.Initialize()
//	lc.Start(ctx)
//
// # Configuration Reference
//
// Full configuration schema:
//
//	{
//	    "allowed_tools": ["string", ...],
//	    "timeout": "string (default: 60s)",
//	    "stream_name": "string (default: AGENT)",
//	    "consumer_name_suffix": "string (optional)",
//	    "ports": {
//	        "inputs": [...],
//	        "outputs": [...]
//	    }
//	}
//
// Configuration fields:
//
//   - allowed_tools: List of tool names to allow (nil/empty = allow all)
//   - timeout: Per-tool execution timeout as duration string (default: "60s")
//   - stream_name: JetStream stream name for agentic messages (default: "AGENT")
//   - consumer_name_suffix: Optional suffix for JetStream consumer names (for testing)
//   - ports: Port configuration for inputs and outputs
//
// # Ports
//
// Input ports (JetStream consumers):
//
//   - tool.execute: Tool execution requests from agentic-loop (subject: tool.execute.>)
//
// Output ports (JetStream publishers):
//
//   - tool.result: Tool execution results to agentic-loop (subject: tool.result.*)
//
// # Message Flow
//
// The processor handles each tool call through:
//
//  1. Receive ToolCall from tool.execute.>
//  2. Validate tool is in allowlist (if configured)
//  3. Look up executor in registry
//  4. Create timeout context
//  5. Execute tool with context
//  6. Publish ToolResult to tool.result.{call_id}
//  7. Acknowledge JetStream message
//
// # Error Handling
//
// Errors are categorized into two types:
//
// **Tool execution errors** (returned in ToolResult.Error):
//
//   - Tool not found in registry
//   - Tool not in allowlist
//   - Tool execution failed
//   - Timeout exceeded
//
// **System errors** (returned as Go error):
//
//   - JSON marshaling failures
//   - NATS publishing failures
//
// Tool execution errors don't fail the loop - the agent can handle them:
//
//	if result.Error != "" {
//	    // Agent sees: "Error: file not found"
//	    // Agent can try alternative approach
//	}
//
// # Wire Execution
//
// Every wire response remains correlated to its call ID. An initial
// approval_required response is a nonterminal pause: it creates no COMPLETED
// outcome and leaves the same CallID eligible for approved re-dispatch. Calls
// that reach execution or terminal policy rejection receive correlated durable
// terminal results, and COMPLETED redelivery replays that result without
// re-invoking the executor.
//
// The wire contract promises neither serialized execution nor execution
// overlap. The current implementation uses one native callback and joins
// outcome persistence, result publication, and delivery settlement before that
// callback returns. That is implementation detail rather than a stable
// serialization contract.
//
// MaxAckPending bounds delivered-but-unacknowledged admission at NATS. It does
// not promise executor overlap or thread safety. Every call receives its own
// cancellation context.
//
// # Built-in Tools
//
// The package does not include built-in tools - all tools must be registered
// by the application. This keeps the processor focused and allows full control
// over available capabilities.
//
// Common tools to implement:
//
//   - File operations: read_file, write_file, list_dir
//   - Web operations: fetch_url, call_api
//   - Database operations: query, insert, update
//   - Graph operations: graph_query (query knowledge graph)
//
// # Thread Safety
//
// ExecutorRegistry uses RWMutex for registry lookup and mutation. Tool
// registration should complete before Start. Direct application callers may
// invoke executors through the registry concurrently, so executor safety for
// those calls remains the application's responsibility; no such guarantee is
// inferred from the wire consumer.
//
// # Testing
//
// For testing, use mock executors and unique consumer names:
//
//	type MockExecutor struct {
//	    result agentic.ToolResult
//	}
//
//	func (m *MockExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
//	    m.result.CallID = call.ID
//	    return m.result, nil
//	}
//
//	func (m *MockExecutor) ListTools() []agentic.ToolDefinition {
//	    return []agentic.ToolDefinition{{Name: "mock_tool"}}
//	}
//
//	// In test
//	config := agentictools.Config{
//	    StreamName:         "AGENT",
//	    ConsumerNameSuffix: "test-" + t.Name(),
//	}
//
// # Limitations
//
// Current limitations:
//
//   - No tool versioning (single version per name)
//   - No tool dependencies or ordering
//   - No streaming tool output
//   - No built-in rate limiting per tool
//   - Timeout is global, not per-tool configurable
//
// # See Also
//
// Related packages:
//
//   - agentic: Shared types (ToolCall, ToolResult, ToolDefinition)
//   - processor/agentic-loop: Loop orchestration
//   - processor/agentic-model: LLM endpoint integration
//
// # JetStream Integration
//
// All messaging uses JetStream for durability. Tool call subjects require
// the AGENT stream to exist with subjects matching tool.execute.> and
// tool.result.>.
//
// Consumer naming follows the pattern: agentic-tools-{subject-pattern}
package agentictools
