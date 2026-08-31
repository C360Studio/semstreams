package agentic

import (
	"fmt"
	"strings"

	"github.com/c360studio/semstreams/message"
	semtypes "github.com/c360studio/semstreams/pkg/types"
)

// ModelEndpointEntityID constructs a 6-part entity ID for a model registry endpoint.
// Format: {org}.{platform}.model-registry.agent.endpoint.{endpointName}
//
// Example: ModelEndpointEntityID("c360", "ops", "claude-sonnet")
// Returns: "c360.ops.model-registry.agent.endpoint.claude-sonnet"
//
// Panics if any input part is empty or contains a dot, as these represent
// programming errors — the caller is responsible for supplying well-formed identifiers.
func ModelEndpointEntityID(org, platform, endpointName string) string {
	id, err := tryModelEndpointEntityID(org, platform, endpointName)
	if err != nil {
		panic(fmt.Sprintf("ModelEndpointEntityID: %s", err))
	}
	return id
}

// tryModelEndpointEntityID is the error-returning form of ModelEndpointEntityID;
// the decoded-payload path uses it so a malformed identity never panics.
func tryModelEndpointEntityID(org, platform, endpointName string) (string, error) {
	if err := validatePart("org", org); err != nil {
		return "", err
	}
	if err := validatePart("platform", platform); err != nil {
		return "", err
	}
	if err := validatePart("endpointName", endpointName); err != nil {
		return "", err
	}

	id := fmt.Sprintf("%s.%s.model-registry.agent.endpoint.%s", org, platform, endpointName)

	if !message.IsValidEntityID(id) {
		return "", fmt.Errorf("constructed id %q failed IsValidEntityID — check input values", id)
	}

	return id, nil
}

// LoopExecutionEntityID constructs a 6-part entity ID for an agentic loop execution.
// Format: {org}.{platform}.agentic-loop.agent.execution.{loopID}
//
// Example: LoopExecutionEntityID("c360", "ops", "abc123")
// Returns: "c360.ops.agentic-loop.agent.execution.abc123"
//
// This function owns the COMPOSITION of the ID, not the form of the loopID
// segment. That form — a framework-minted canonical UUID — is owned by
// internal/looptoken and decided at the seams that accept a token
// (TaskMessage.Validate, LoopManager.CreateLoopWithID, agentrun.Mint, dispatch
// intake). See ADR-105.
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

	id := fmt.Sprintf("%s.%s.agentic-loop.agent.execution.%s", org, platform, loopID)

	if !message.IsValidEntityID(id) {
		return "", fmt.Errorf("LoopExecutionEntityID: constructed id %q failed IsValidEntityID — check input values", id)
	}

	return id, nil
}

// ChainExecutionEntityID constructs a 6-part entity ID for a cross-arc agent chain.
// Format: {org}.{platform}.chain.agent.execution.{chainID}
//
// Example: ChainExecutionEntityID("c360", "ops", "abc123")
// Returns: "c360.ops.chain.agent.execution.abc123"
//
// A chain entity is the canonical anchor for cross-arc data flow: rules and
// product subscribers stamp milestone triples (chain.dispatched_at,
// chain.research_artifact_loop, chain.spec_artifact_loop, chain.paused.*,
// chain.decision.*, ...) on this entity. The chain_id is the dispatch loop's
// UUID — no new ID generation required at chain start. See semteams ADR-038
// for the chain-anchor pattern. `chain` is a sibling system (position 3) to
// `agentic-loop` within the framework-reserved `agent` domain (position 4).
//
// Panics if any input part is empty or contains a dot, as these represent
// programming errors — the caller is responsible for supplying well-formed identifiers.
//
// Runtime callers where a panic would silently crash a goroutine (event-
// construction in publish goroutines, tool executors) should use
// TryChainExecutionEntityID instead and surface the error explicitly.
func ChainExecutionEntityID(org, platform, chainID string) string {
	id, err := TryChainExecutionEntityID(org, platform, chainID)
	if err != nil {
		panic(fmt.Sprintf("ChainExecutionEntityID: %s", err))
	}
	return id
}

// TryChainExecutionEntityID is the error-returning variant of
// ChainExecutionEntityID. Use this from runtime hot paths (event-
// construction in publish goroutines, milestone subscribers) where a
// panic would silently crash the goroutine. Boot-time and post-completion
// callers that own their inputs can keep using the panicking form.
//
// Returns ("", error) when any input part is empty or contains a dot,
// or when the constructed ID fails IsValidEntityID.
func TryChainExecutionEntityID(org, platform, chainID string) (string, error) {
	if err := validatePart("org", org); err != nil {
		return "", fmt.Errorf("ChainExecutionEntityID: %w", err)
	}
	if err := validatePart("platform", platform); err != nil {
		return "", fmt.Errorf("ChainExecutionEntityID: %w", err)
	}
	if err := validatePart("chainID", chainID); err != nil {
		return "", fmt.Errorf("ChainExecutionEntityID: %w", err)
	}

	id := fmt.Sprintf("%s.%s.chain.agent.execution.%s", org, platform, chainID)

	if !message.IsValidEntityID(id) {
		return "", fmt.Errorf("ChainExecutionEntityID: constructed id %q failed IsValidEntityID — check input values", id)
	}

	return id, nil
}

// LoopIDFromExecutionEntityID extracts the loop_id segment from a 6-part
// entity ID matching the LoopExecutionEntityID shape:
// {org}.{platform}.agentic-loop.agent.execution.{loopID}
//
// Returns ("", false) when the input is not a valid 6-part entity ID, or
// when it doesn't match the agentic-loop.agent.execution.* shape (e.g.
// model-registry endpoints, trajectory steps, chain entities, or non-agent
// entity IDs). Used by the rule engine's publish_agent action to set
// task.ParentLoopID when a rule fires on a loop-execution-shaped trigger
// entity, so rule-fanned chains carry their parent linkage natively
// (semteams ADR-038 §D2).
//
// Reads positions by NAME through pkg/types.ParseEntityID (ADR-102): the
// system, domain, and type fields, never fixed indexes. Pure parser: no
// validation of the loop_id's content beyond the entity-ID grammar.
func LoopIDFromExecutionEntityID(entityID string) (string, bool) {
	parsed, err := semtypes.ParseEntityID(entityID)
	if err != nil {
		return "", false
	}
	if parsed.System != "agentic-loop" || parsed.Domain != "agent" || parsed.Type != "execution" {
		return "", false
	}
	return parsed.Instance, true
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
