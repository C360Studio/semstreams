package agentic

import "github.com/c360studio/semstreams/payloadregistry"

// init registers all agentic payload types with the global PayloadRegistry.
// This enables BaseMessage.UnmarshalJSON to recreate typed payloads from JSON
// when the message type matches one of the agentic types.
//
// Builders are intentionally omitted - the PayloadRegistry's JSON fallback
// (Factory + json.Unmarshal) handles payload construction for workflow
// variable interpolation without requiring duplicate field-mapping code.
func init() {
	// Register TaskMessage payload factory
	err := payloadregistry.Register(&payloadregistry.Registration{
		Domain:      Domain,
		Category:    CategoryTask,
		Version:     SchemaVersion,
		Description: "Agent task request",
		Factory:     func() any { return &TaskMessage{} },
	})
	if err != nil {
		panic("failed to register TaskMessage payload: " + err.Error())
	}

	// Register UserMessage payload factory
	err = payloadregistry.Register(&payloadregistry.Registration{
		Domain:      Domain,
		Category:    CategoryUserMessage,
		Version:     SchemaVersion,
		Description: "User message from any channel",
		Factory:     func() any { return &UserMessage{} },
	})
	if err != nil {
		panic("failed to register UserMessage payload: " + err.Error())
	}

	// Register UserSignal payload factory
	err = payloadregistry.Register(&payloadregistry.Registration{
		Domain:      Domain,
		Category:    CategorySignal,
		Version:     SchemaVersion,
		Description: "User control signal",
		Factory:     func() any { return &UserSignal{} },
	})
	if err != nil {
		panic("failed to register UserSignal payload: " + err.Error())
	}

	// Register UserResponse payload factory
	err = payloadregistry.Register(&payloadregistry.Registration{
		Domain:      Domain,
		Category:    CategoryUserResponse,
		Version:     SchemaVersion,
		Description: "User response to channel",
		Factory:     func() any { return &UserResponse{} },
	})
	if err != nil {
		panic("failed to register UserResponse payload: " + err.Error())
	}

	// Register AgentResponse payload factory
	err = payloadregistry.Register(&payloadregistry.Registration{
		Domain:      Domain,
		Category:    CategoryResponse,
		Version:     SchemaVersion,
		Description: "Agent model response",
		Factory:     func() any { return &AgentResponse{} },
	})
	if err != nil {
		panic("failed to register AgentResponse payload: " + err.Error())
	}

	// Register ToolResult payload factory
	err = payloadregistry.Register(&payloadregistry.Registration{
		Domain:      Domain,
		Category:    CategoryToolResult,
		Version:     SchemaVersion,
		Description: "Tool execution result",
		Factory:     func() any { return &ToolResult{} },
	})
	if err != nil {
		panic("failed to register ToolResult payload: " + err.Error())
	}

	// Register AgentRequest payload factory
	err = payloadregistry.Register(&payloadregistry.Registration{
		Domain:      Domain,
		Category:    CategoryRequest,
		Version:     SchemaVersion,
		Description: "Agent model request",
		Factory:     func() any { return &AgentRequest{} },
	})
	if err != nil {
		panic("failed to register AgentRequest payload: " + err.Error())
	}

	// Register ToolCall payload factory
	err = payloadregistry.Register(&payloadregistry.Registration{
		Domain:      Domain,
		Category:    CategoryToolCall,
		Version:     SchemaVersion,
		Description: "Tool call request",
		Factory:     func() any { return &ToolCall{} },
	})
	if err != nil {
		panic("failed to register ToolCall payload: " + err.Error())
	}

	// Register LoopCreatedEvent payload factory
	err = payloadregistry.Register(&payloadregistry.Registration{
		Domain:      Domain,
		Category:    CategoryLoopCreated,
		Version:     SchemaVersion,
		Description: "Loop creation event",
		Factory:     func() any { return &LoopCreatedEvent{} },
	})
	if err != nil {
		panic("failed to register LoopCreatedEvent payload: " + err.Error())
	}

	// Register LoopCompletedEvent payload factory
	err = payloadregistry.Register(&payloadregistry.Registration{
		Domain:      Domain,
		Category:    CategoryLoopCompleted,
		Version:     SchemaVersion,
		Description: "Loop completion event",
		Factory:     func() any { return &LoopCompletedEvent{} },
	})
	if err != nil {
		panic("failed to register LoopCompletedEvent payload: " + err.Error())
	}

	// Register LoopFailedEvent payload factory
	err = payloadregistry.Register(&payloadregistry.Registration{
		Domain:      Domain,
		Category:    CategoryLoopFailed,
		Version:     SchemaVersion,
		Description: "Loop failure event",
		Factory:     func() any { return &LoopFailedEvent{} },
	})
	if err != nil {
		panic("failed to register LoopFailedEvent payload: " + err.Error())
	}

	// Register LoopCancelledEvent payload factory
	err = payloadregistry.Register(&payloadregistry.Registration{
		Domain:      Domain,
		Category:    CategoryLoopCancelled,
		Version:     SchemaVersion,
		Description: "Loop cancellation event",
		Factory:     func() any { return &LoopCancelledEvent{} },
	})
	if err != nil {
		panic("failed to register LoopCancelledEvent payload: " + err.Error())
	}

	// Register ContextEvent payload factory
	err = payloadregistry.Register(&payloadregistry.Registration{
		Domain:      Domain,
		Category:    CategoryContextEvent,
		Version:     SchemaVersion,
		Description: "Context management event",
		Factory:     func() any { return &ContextEvent{} },
	})
	if err != nil {
		panic("failed to register ContextEvent payload: " + err.Error())
	}
}
