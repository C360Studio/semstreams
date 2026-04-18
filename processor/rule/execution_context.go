// Package rule - Execution context for rule actions
package rule

import (
	"fmt"
	"strings"
	"time"

	gtypes "github.com/c360studio/semstreams/graph"
)

// ExecutionContext carries typed data through the rule evaluation → action pipeline.
// It replaces the previous (entityID, relatedID string) action signature, providing
// actions with the full entity state and match state for richer execution logic.
type ExecutionContext struct {
	// EntityID is the primary entity identifier.
	EntityID string

	// RelatedID is the related entity identifier (empty for single-entity rules).
	RelatedID string

	// Entity is the full entity state with triples (nil for message-path rules).
	Entity *gtypes.EntityState

	// Related is the related entity state (nil for single-entity rules or message-path).
	Related *gtypes.EntityState

	// State is the current match state including iteration tracking.
	// May be nil for first evaluation before state is persisted.
	State *MatchState
}

// RuleID returns the originating rule's identifier, or an empty string if the
// execution context has no associated match state (e.g. message-path actions
// invoked outside a stateful evaluator). Callers use this to scope feedback
// loop prevention to the rule that caused the write.
func (ec *ExecutionContext) RuleID() string {
	if ec == nil || ec.State == nil {
		return ""
	}
	return ec.State.RuleID
}

// SubstituteVariables replaces template variables with values from the execution context.
// Supported variables:
//   - $entity.id: The primary entity ID
//   - $related.id: The related entity ID (for pair rules)
//   - $state.iteration: Current iteration count
//   - $state.max_iterations: Configured max iterations
//
// Entity triple values can be accessed via $entity.triple.<predicate> syntax.
func (ec *ExecutionContext) SubstituteVariables(template string) string {
	result := template

	// Time substitutions
	result = strings.ReplaceAll(result, "$now", time.Now().UTC().Format(time.RFC3339))

	// Core ID substitutions
	result = strings.ReplaceAll(result, "$entity.id", ec.EntityID)
	result = strings.ReplaceAll(result, "$related.id", ec.RelatedID)

	// State substitutions
	if ec.State != nil {
		result = strings.ReplaceAll(result, "$state.iteration", fmt.Sprintf("%d", ec.State.Iteration))
		result = strings.ReplaceAll(result, "$state.max_iterations", fmt.Sprintf("%d", ec.State.MaxIterations))
	}

	// Entity triple substitutions (e.g., $entity.triple.agent.role → triple value)
	if ec.Entity != nil {
		for _, triple := range ec.Entity.Triples {
			key := "$entity.triple." + triple.Predicate
			result = strings.ReplaceAll(result, key, fmt.Sprintf("%v", triple.Object))
		}
	}

	return result
}
