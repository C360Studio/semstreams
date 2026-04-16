package agentic

import (
	"encoding/json"
	"time"

	"github.com/c360studio/semstreams/message"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

// TrajectoryStepEntity wraps a TrajectoryStep with the context needed to
// produce a graph entity. It implements message.ContentStorable so that
// large content (tool results, model responses) is stored in ObjectStore
// while metadata-only triples go into the graph.
type TrajectoryStepEntity struct {
	Step      TrajectoryStep
	Org       string
	Platform  string
	LoopID    string
	StepIndex int

	storageRef *message.StorageReference
}

// EntityID returns the 6-part entity ID for this trajectory step.
func (e *TrajectoryStepEntity) EntityID() string {
	return TrajectoryStepEntityID(e.Org, e.Platform, e.LoopID, e.StepIndex)
}

// Triples returns metadata-only triples for this step.
// Large content (tool args/results, model responses) is NOT included —
// that goes to ObjectStore via RawContent/ContentFields.
func (e *TrajectoryStepEntity) Triples() []message.Triple {
	entityID := e.EntityID()
	loopEntityID := LoopExecutionEntityID(e.Org, e.Platform, e.LoopID)
	now := time.Now()

	triple := func(predicate string, object any) message.Triple {
		return message.Triple{
			Subject:    entityID,
			Predicate:  predicate,
			Object:     object,
			Source:     "agentic-loop",
			Timestamp:  now,
			Confidence: 1.0,
		}
	}

	triples := []message.Triple{
		triple(agvocab.StepType, e.Step.StepType),
		triple(agvocab.StepIndex, e.StepIndex),
		triple(agvocab.StepLoop, loopEntityID),
		triple(agvocab.StepTimestamp, e.Step.Timestamp.Format(time.RFC3339)),
		triple(agvocab.StepDuration, e.Step.Duration),
	}

	switch e.Step.StepType {
	case "tool_call":
		if e.Step.ToolName != "" {
			triples = append(triples, triple(agvocab.StepToolName, e.Step.ToolName))
		}
		if e.Step.ToolStatus != "" {
			triples = append(triples, triple(agvocab.StepToolStatus, e.Step.ToolStatus))
		}
		if e.Step.ErrorMessage != "" {
			triples = append(triples, triple(agvocab.StepErrorMessage, e.Step.ErrorMessage))
		}
		if e.Step.ErrorCategory != "" {
			triples = append(triples, triple(agvocab.StepErrorCategory, e.Step.ErrorCategory))
		}
	case "model_call":
		if e.Step.Model != "" {
			triples = append(triples, triple(agvocab.StepModel, e.Step.Model))
		}
		if e.Step.TokensIn > 0 {
			triples = append(triples, triple(agvocab.StepTokensIn, e.Step.TokensIn))
		}
		if e.Step.TokensOut > 0 {
			triples = append(triples, triple(agvocab.StepTokensOut, e.Step.TokensOut))
		}
	case "context_compaction":
		if e.Step.TokensIn > 0 {
			triples = append(triples, triple(agvocab.StepTokensEvicted, e.Step.TokensIn))
		}
		if e.Step.TokensOut > 0 {
			triples = append(triples, triple(agvocab.StepTokensSummarized, e.Step.TokensOut))
		}
		if e.Step.Model != "" {
			triples = append(triples, triple(agvocab.StepModel, e.Step.Model))
		}
		if e.Step.Utilization > 0 {
			triples = append(triples, triple(agvocab.StepUtilization, e.Step.Utilization))
		}
	}

	// Common optional predicates (apply to all step types)
	if e.Step.Capability != "" {
		triples = append(triples, triple(agvocab.StepCapability, e.Step.Capability))
	}
	if e.Step.Provider != "" {
		triples = append(triples, triple(agvocab.StepProvider, e.Step.Provider))
	}
	if e.Step.RetryCount > 0 {
		triples = append(triples, triple(agvocab.StepRetries, e.Step.RetryCount))
	}

	return triples
}

// StorageRef returns the reference to stored content in ObjectStore.
func (e *TrajectoryStepEntity) StorageRef() *message.StorageReference {
	return e.storageRef
}

// SetStorageRef sets the ObjectStore reference after content is stored.
func (e *TrajectoryStepEntity) SetStorageRef(ref *message.StorageReference) {
	e.storageRef = ref
}

// ContentFields returns the semantic role to field name mapping.
// This tells embedding workers which fields to use for text extraction.
func (e *TrajectoryStepEntity) ContentFields() map[string]string {
	switch e.Step.StepType {
	case "tool_call":
		fields := map[string]string{
			message.ContentRoleTitle: "tool_name",
		}
		if e.Step.ErrorMessage != "" {
			fields[message.ContentRoleBody] = "error"
		} else if e.Step.ToolResult != "" {
			fields[message.ContentRoleBody] = "tool_result"
		}
		return fields
	case "model_call":
		fields := map[string]string{
			message.ContentRoleTitle: "model",
		}
		if e.Step.Response != "" {
			fields[message.ContentRoleBody] = "response"
		}
		return fields
	case "context_compaction":
		if e.Step.Response != "" {
			return map[string]string{
				message.ContentRoleBody: "summary",
			}
		}
		return nil
	default:
		return nil
	}
}

// RawContent returns the content to store in ObjectStore.
// Field names here match the values in ContentFields().
func (e *TrajectoryStepEntity) RawContent() map[string]string {
	switch e.Step.StepType {
	case "tool_call":
		content := map[string]string{
			"tool_name": e.Step.ToolName,
		}
		if e.Step.ToolResult != "" {
			content["tool_result"] = e.Step.ToolResult
		}
		if e.Step.ErrorMessage != "" {
			content["error"] = e.Step.ErrorMessage
		}
		if len(e.Step.ToolArguments) > 0 {
			argsJSON, err := json.Marshal(e.Step.ToolArguments)
			if err == nil {
				content["tool_arguments"] = string(argsJSON)
			}
		}
		return content
	case "model_call":
		content := map[string]string{
			"model": e.Step.Model,
		}
		if e.Step.Response != "" {
			content["response"] = e.Step.Response
		}
		return content
	case "context_compaction":
		if e.Step.Response != "" {
			return map[string]string{
				"summary": e.Step.Response,
			}
		}
		return nil
	default:
		return nil
	}
}
