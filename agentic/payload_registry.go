package agentic

import (
	"errors"
	"fmt"

	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/c360studio/semstreams/pkg/projection/contract"
	"github.com/c360studio/semstreams/vocabulary"
)

// RegisterPayloads registers all agentic payload types with the
// supplied registry. Called from payloadbuiltins.Register during
// process bootstrap. Returns aggregated errors via errors.Join so
// misconfigured deployments see every collision on a single boot.
//
// Each registration carries its ADR-054 indexing-profile floor (the profile
// graph-ingest stamps on an entity born with the type when the producer
// declares none) and, for the two types that hold one, the projection
// contract bound to the type (ADR-103): the registry is the one table.
//
// Builders are intentionally omitted — the PayloadRegistry's JSON
// fallback (Factory + json.Unmarshal) handles payload construction
// for workflow variable interpolation without requiring duplicate
// field-mapping code.
func RegisterPayloads(reg *payloadregistry.Registry) error {
	const (
		content = vocabulary.IndexingProfileContent
		control = vocabulary.IndexingProfileControl
		signal  = vocabulary.IndexingProfileSignal
		trace   = vocabulary.IndexingProfileTrace
	)
	registrations := []*payloadregistry.Registration{
		{Domain: Domain, Category: CategoryTask, Version: SchemaVersion, Description: "Agent task request", Factory: func() any { return &TaskMessage{} }, IndexingProfile: control},
		{Domain: Domain, Category: CategoryUserMessage, Version: SchemaVersion, Description: "User message from any channel", Factory: func() any { return &UserMessage{} }, IndexingProfile: content},
		{Domain: Domain, Category: CategorySignal, Version: SchemaVersion, Description: "User control signal", Factory: func() any { return &UserSignal{} }, IndexingProfile: signal},
		{Domain: Domain, Category: CategoryUserResponse, Version: SchemaVersion, Description: "User response to channel", Factory: func() any { return &UserResponse{} }, IndexingProfile: content},
		{Domain: Domain, Category: CategoryResponse, Version: SchemaVersion, Description: "Agent model response", Factory: func() any { return &AgentResponse{} }, IndexingProfile: trace},
		{Domain: Domain, Category: CategoryToolResult, Version: SchemaVersion, Description: "Tool execution result", Factory: func() any { return &ToolResult{} }, IndexingProfile: trace},
		{Domain: Domain, Category: CategoryRequest, Version: SchemaVersion, Description: "Agent model request", Factory: func() any { return &AgentRequest{} }, IndexingProfile: trace},
		{Domain: Domain, Category: CategoryToolCall, Version: SchemaVersion, Description: "Tool call request", Factory: func() any { return &ToolCall{} }, IndexingProfile: trace},
		{Domain: Domain, Category: CategoryLoopCreated, Version: SchemaVersion, Description: "Loop creation event", Factory: func() any { return &LoopCreatedEvent{} }, IndexingProfile: control},
		{Domain: Domain, Category: CategoryLoopCompleted, Version: SchemaVersion, Description: "Loop completion event", Factory: func() any { return &LoopCompletedEvent{} }, IndexingProfile: control},
		{Domain: Domain, Category: CategoryLoopFailed, Version: SchemaVersion, Description: "Loop failure event", Factory: func() any { return &LoopFailedEvent{} }, IndexingProfile: control},
		{Domain: Domain, Category: CategoryLoopCancelled, Version: SchemaVersion, Description: "Loop cancellation event", Factory: func() any { return &LoopCancelledEvent{} }, IndexingProfile: control},
		{Domain: Domain, Category: CategoryContextEvent, Version: SchemaVersion, Description: "Context management event", Factory: func() any { return &ContextEvent{} }, IndexingProfile: trace},
		{Domain: Domain, Category: CategoryApprovalPending, Version: SchemaVersion, Description: "Approval-pending event for human-in-the-loop tool gating", Factory: func() any { return &ApprovalPendingEvent{} }, IndexingProfile: control},
		{Domain: Domain, Category: CategoryApprovalResponse, Version: SchemaVersion, Description: "Approval response from human-in-the-loop UI", Factory: func() any { return &ApprovalResponse{} }, IndexingProfile: control},

		// Entity types born on the mutation lane (ADR-103): each is a Graphable
		// payload with a factory, so it decodes on the fact lane as itself.
		{Domain: Domain, Category: CategoryLoopExecution, Version: SchemaVersion, Description: "Agentic loop execution entity (spawn identity)", Factory: func() any { return &LoopExecutionEntity{} }, IndexingProfile: control, Contracts: []contract.Contract{LoopExecutionContract()}},
		{Domain: Domain, Category: CategoryAgentLesson, Version: SchemaVersion, Description: "Agent lesson record entity", Factory: func() any { return &AgentLessonEntity{} }, IndexingProfile: content, Contracts: []contract.Contract{LessonContract()}},
		{Domain: Domain, Category: CategoryOpsDiagnosis, Version: SchemaVersion, Description: "Ops diagnosis finding entity", Factory: func() any { return &OpsDiagnosisEntity{} }, IndexingProfile: content},
		{Domain: Domain, Category: CategoryModelEndpoint, Version: SchemaVersion, Description: "Model registry endpoint entity", Factory: func() any { return &ModelEndpointEntity{} }, IndexingProfile: control},
		{Domain: Domain, Category: CategoryWebObservation, Version: SchemaVersion, Description: "Web observation entity (one canonical URL observed by agent tools)", Factory: func() any { return &WebObservationEntity{} }, IndexingProfile: content},
	}

	var errs []error
	for _, r := range registrations {
		if err := reg.Register(r); err != nil {
			errs = append(errs, fmt.Errorf("register %s.%s.%s: %w", r.Domain, r.Category, r.Version, err))
		}
	}
	return errors.Join(errs...)
}
