package agentic

import (
	"fmt"
	"strings"

	"github.com/c360studio/semstreams/message"
)

// ModelEndpointEntityID constructs a 6-part entity ID for a model registry endpoint.
// Format: {org}.{platform}.agent.model-registry.endpoint.{endpointName}
//
// Example: ModelEndpointEntityID("c360", "ops", "claude-sonnet")
// Returns: "c360.ops.agent.model-registry.endpoint.claude-sonnet"
//
// Panics if any input part is empty or contains a dot, as these represent
// programming errors — the caller is responsible for supplying well-formed identifiers.
func ModelEndpointEntityID(org, platform, endpointName string) string {
	if err := validatePart("org", org); err != nil {
		panic(fmt.Sprintf("ModelEndpointEntityID: %s", err))
	}
	if err := validatePart("platform", platform); err != nil {
		panic(fmt.Sprintf("ModelEndpointEntityID: %s", err))
	}
	if err := validatePart("endpointName", endpointName); err != nil {
		panic(fmt.Sprintf("ModelEndpointEntityID: %s", err))
	}

	id := fmt.Sprintf("%s.%s.agent.model-registry.endpoint.%s", org, platform, endpointName)

	if !message.IsValidEntityID(id) {
		panic(fmt.Sprintf("ModelEndpointEntityID: constructed id %q failed IsValidEntityID — check input values", id))
	}

	return id
}

// LoopExecutionEntityID constructs a 6-part entity ID for an agentic loop execution.
// Format: {org}.{platform}.agent.agentic-loop.execution.{loopID}
//
// Example: LoopExecutionEntityID("c360", "ops", "abc123")
// Returns: "c360.ops.agent.agentic-loop.execution.abc123"
//
// Panics if any input part is empty or contains a dot. Suitable for
// boot-time and post-completion paths where invalid input represents
// a programming error that should fail loud (operator config check at
// startup; framework bug in event-payload construction).
//
// Runtime tool executors (where a panic silently kills the dispatch
// goroutine and the agent fails opaquely) should use
// TryLoopExecutionEntityID and surface the error as ToolErrorInternal
// instead. ADR-036 Stage 3.8 documents the panic-class concern; the
// beta.36 read_loop_result wedge is the in-tree precedent.
func LoopExecutionEntityID(org, platform, loopID string) string {
	id, err := TryLoopExecutionEntityID(org, platform, loopID)
	if err != nil {
		panic(fmt.Sprintf("LoopExecutionEntityID: %s", err))
	}
	return id
}

// TryLoopExecutionEntityID is the error-returning variant of
// LoopExecutionEntityID. Use this from runtime hot paths (tool
// executors, per-iteration prompt assembly) where a panic would
// silently crash the dispatch goroutine and the agent would fail
// opaquely. Boot-time and post-completion callers can keep using the
// panicking LoopExecutionEntityID.
//
// Returns ("", error) when any input part is empty or contains a dot,
// or when the constructed ID fails IsValidEntityID.
func TryLoopExecutionEntityID(org, platform, loopID string) (string, error) {
	if err := validatePart("org", org); err != nil {
		return "", fmt.Errorf("LoopExecutionEntityID: %w", err)
	}
	if err := validatePart("platform", platform); err != nil {
		return "", fmt.Errorf("LoopExecutionEntityID: %w", err)
	}
	if err := validatePart("loopID", loopID); err != nil {
		return "", fmt.Errorf("LoopExecutionEntityID: %w", err)
	}

	id := fmt.Sprintf("%s.%s.agent.agentic-loop.execution.%s", org, platform, loopID)

	if !message.IsValidEntityID(id) {
		return "", fmt.Errorf("LoopExecutionEntityID: constructed id %q failed IsValidEntityID — check input values", id)
	}

	return id, nil
}

// TrajectoryStepEntityID constructs a 6-part entity ID for a trajectory step.
// Format: {org}.{platform}.agent.agentic-loop.step.{loopID}-{stepIndex}
//
// Example: TrajectoryStepEntityID("c360", "ops", "abc123", 0)
// Returns: "c360.ops.agent.agentic-loop.step.abc123-0"
//
// Panics if any input part is empty or contains a dot, as these represent
// programming errors — the caller is responsible for supplying well-formed identifiers.
func TrajectoryStepEntityID(org, platform, loopID string, stepIndex int) string {
	if err := validatePart("org", org); err != nil {
		panic(fmt.Sprintf("TrajectoryStepEntityID: %s", err))
	}
	if err := validatePart("platform", platform); err != nil {
		panic(fmt.Sprintf("TrajectoryStepEntityID: %s", err))
	}
	if err := validatePart("loopID", loopID); err != nil {
		panic(fmt.Sprintf("TrajectoryStepEntityID: %s", err))
	}
	if stepIndex < 0 {
		panic(fmt.Sprintf("TrajectoryStepEntityID: stepIndex must be non-negative, got %d", stepIndex))
	}

	instance := fmt.Sprintf("%s-%d", loopID, stepIndex)
	id := fmt.Sprintf("%s.%s.agent.agentic-loop.step.%s", org, platform, instance)

	if !message.IsValidEntityID(id) {
		panic(fmt.Sprintf("TrajectoryStepEntityID: constructed id %q failed IsValidEntityID — check input values", id))
	}

	return id
}

// ChainExecutionEntityID constructs a 6-part entity ID for a cross-arc agent chain.
// Format: {org}.{platform}.agent.chain.execution.{chainID}
//
// Example: ChainExecutionEntityID("c360", "ops", "abc123")
// Returns: "c360.ops.agent.chain.execution.abc123"
//
// A chain entity is the canonical anchor for cross-arc data flow: rules and
// product subscribers stamp milestone triples (chain.dispatched_at,
// chain.research_artifact_loop, chain.spec_artifact_loop, chain.paused.*,
// chain.decision.*, ...) on this entity. The chain_id is the dispatch loop's
// UUID — no new ID generation required at chain start. See semteams ADR-038
// for the chain-anchor pattern. `chain` is a sibling component to
// `agentic-loop` within the `agent` domain.
//
// Panics if any input part is empty or contains a dot, as these represent
// programming errors — the caller is responsible for supplying well-formed identifiers.
func ChainExecutionEntityID(org, platform, chainID string) string {
	if err := validatePart("org", org); err != nil {
		panic(fmt.Sprintf("ChainExecutionEntityID: %s", err))
	}
	if err := validatePart("platform", platform); err != nil {
		panic(fmt.Sprintf("ChainExecutionEntityID: %s", err))
	}
	if err := validatePart("chainID", chainID); err != nil {
		panic(fmt.Sprintf("ChainExecutionEntityID: %s", err))
	}

	id := fmt.Sprintf("%s.%s.agent.chain.execution.%s", org, platform, chainID)

	if !message.IsValidEntityID(id) {
		panic(fmt.Sprintf("ChainExecutionEntityID: constructed id %q failed IsValidEntityID — check input values", id))
	}

	return id
}

// LoopIDFromExecutionEntityID extracts the loop_id segment from a 6-part
// entity ID matching the LoopExecutionEntityID shape:
// {org}.{platform}.agent.agentic-loop.execution.{loopID}
//
// Returns ("", false) when the input is not a valid 6-part entity ID, or
// when it doesn't match the agent.agentic-loop.execution.* shape (e.g.
// model-registry endpoints, trajectory steps, chain entities, or non-agent
// entity IDs). Used by the rule engine's publish_agent action to set
// task.ParentLoopID when a rule fires on a loop-execution-shaped trigger
// entity, so rule-fanned chains carry their parent linkage natively
// (semteams ADR-038 §D2).
//
// Pure parser: no validation of the loop_id's content beyond IsValidEntityID's
// alphanumeric/hyphen/underscore guard.
func LoopIDFromExecutionEntityID(entityID string) (string, bool) {
	if !message.IsValidEntityID(entityID) {
		return "", false
	}
	parts := strings.Split(entityID, ".")
	// IsValidEntityID guarantees exactly 6 parts; the indexed reads are safe.
	if parts[2] != "agent" || parts[3] != "agentic-loop" || parts[4] != "execution" {
		return "", false
	}
	return parts[5], true
}

// validatePart checks that a single entity ID component is non-empty and contains no dots.
// Dots are reserved as part separators in the 6-part entity ID format.
func validatePart(name, value string) error {
	if value == "" {
		return fmt.Errorf("%s must not be empty", name)
	}
	if strings.Contains(value, ".") {
		return fmt.Errorf("%s %q must not contain dots (dots are entity ID separators)", name, value)
	}
	return nil
}
