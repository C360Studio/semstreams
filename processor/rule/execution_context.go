// Package rule - Execution context for rule actions
package rule

import (
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	gtypes "github.com/c360studio/semstreams/graph"
)

// unresolvedTemplateVarRe matches any $entity.*, $related.*, or $state.*
// token that survived substitution. These should never appear in a final
// string: if they do, either the predicate is missing from the entity at
// fire-time (common with race-y triple arrival), the ExecutionContext
// wasn't populated, or the author used a name that doesn't match any known
// substitution.
//
// Pattern rationale: first character after the dot must be a word char
// (letter/digit/underscore), then any run of word chars and dots lets us
// match deep predicate paths like $entity.triple.agent.loop.role. We stop
// at any other character so legitimate adjacent syntax (e.g. "$entity.id.")
// doesn't over-match.
var unresolvedTemplateVarRe = regexp.MustCompile(`\$(?:entity|related|state)\.\w[\w.]*`)

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
//
// If any template variable survives substitution (e.g. $entity.triple.X
// where X isn't on the entity at fire time — a common race with
// late-arriving triples), the literal stays in the output and a warning is
// logged so the author sees the silent-pass. Downstream callers that feed
// the result into an identifier (KV key, NATS subject) will then get a
// loud failure instead of mysteriously wrong behaviour.
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

	ec.warnUnresolvedTemplateVars(template, result)

	return result
}

// warnUnresolvedTemplateVars logs a warning when any $entity/$related/$state
// token survives substitution. Split out so callers or tests that need
// substitution without the warning side-effect can bypass it in the future,
// and so the warning message stays consistent regardless of which caller
// triggered it.
func (ec *ExecutionContext) warnUnresolvedTemplateVars(template, result string) {
	leftovers := unresolvedTemplateVarRe.FindAllString(result, -1)
	if len(leftovers) == 0 {
		return
	}
	slog.Default().Warn("Unresolved template variables in rule substitution — likely silent-pass bug",
		slog.Any("unresolved", leftovers),
		slog.String("template", template),
		slog.String("rule_id", ec.RuleID()),
		slog.String("entity_id", ec.EntityID))
}
