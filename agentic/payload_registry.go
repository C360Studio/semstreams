package agentic

import (
	"errors"
	"fmt"

	"github.com/c360studio/semstreams/payloadregistry"
)

// init populates the package-level global registry for transitional
// compat with code paths that still resolve through
// payloadregistry.Create at runtime (notably BaseMessage.UnmarshalJSON
// pre-C3). Both init() and the global are removed in C4 once every
// caller has been migrated to use a constructor-injected registry.
func init() {
	if err := RegisterPayloads(payloadregistry.Global()); err != nil {
		panic("agentic.init: " + err.Error())
	}
}

// RegisterPayloads registers all agentic payload types with the
// supplied registry. Called from payloadbuiltins.Register during
// process bootstrap. Returns aggregated errors via errors.Join so
// misconfigured deployments see every collision on a single boot.
//
// Builders are intentionally omitted — the PayloadRegistry's JSON
// fallback (Factory + json.Unmarshal) handles payload construction
// for workflow variable interpolation without requiring duplicate
// field-mapping code.
func RegisterPayloads(reg *payloadregistry.Registry) error {
	registrations := []*payloadregistry.Registration{
		{Domain: Domain, Category: CategoryTask, Version: SchemaVersion, Description: "Agent task request", Factory: func() any { return &TaskMessage{} }},
		{Domain: Domain, Category: CategoryUserMessage, Version: SchemaVersion, Description: "User message from any channel", Factory: func() any { return &UserMessage{} }},
		{Domain: Domain, Category: CategorySignal, Version: SchemaVersion, Description: "User control signal", Factory: func() any { return &UserSignal{} }},
		{Domain: Domain, Category: CategoryUserResponse, Version: SchemaVersion, Description: "User response to channel", Factory: func() any { return &UserResponse{} }},
		{Domain: Domain, Category: CategoryResponse, Version: SchemaVersion, Description: "Agent model response", Factory: func() any { return &AgentResponse{} }},
		{Domain: Domain, Category: CategoryToolResult, Version: SchemaVersion, Description: "Tool execution result", Factory: func() any { return &ToolResult{} }},
		{Domain: Domain, Category: CategoryRequest, Version: SchemaVersion, Description: "Agent model request", Factory: func() any { return &AgentRequest{} }},
		{Domain: Domain, Category: CategoryToolCall, Version: SchemaVersion, Description: "Tool call request", Factory: func() any { return &ToolCall{} }},
		{Domain: Domain, Category: CategoryLoopCreated, Version: SchemaVersion, Description: "Loop creation event", Factory: func() any { return &LoopCreatedEvent{} }},
		{Domain: Domain, Category: CategoryLoopCompleted, Version: SchemaVersion, Description: "Loop completion event", Factory: func() any { return &LoopCompletedEvent{} }},
		{Domain: Domain, Category: CategoryLoopFailed, Version: SchemaVersion, Description: "Loop failure event", Factory: func() any { return &LoopFailedEvent{} }},
		{Domain: Domain, Category: CategoryLoopCancelled, Version: SchemaVersion, Description: "Loop cancellation event", Factory: func() any { return &LoopCancelledEvent{} }},
		{Domain: Domain, Category: CategoryContextEvent, Version: SchemaVersion, Description: "Context management event", Factory: func() any { return &ContextEvent{} }},
	}

	var errs []error
	for _, r := range registrations {
		if err := reg.Register(r); err != nil {
			errs = append(errs, fmt.Errorf("register %s.%s.%s: %w", r.Domain, r.Category, r.Version, err))
		}
	}
	return errors.Join(errs...)
}
