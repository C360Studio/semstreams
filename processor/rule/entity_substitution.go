// Package rule - Entity-part substitution context.
//
// Rule templates frequently want a SEGMENT of an entity ID rather than
// the whole 6-part federated string. The canonical example is the
// `read_loop_result` tool: AGENT_LOOPS keys are `COMPLETE_<bare-uuid>`,
// where `<bare-uuid>` is the trailing `instance` segment of the full
// agent-loop entity ID `org.platform.agent.agentic-loop.execution.<uuid>`.
// Authors who reach for `$entity.id` in their rule prompt template hand
// the LLM the full string; the LLM then constructs `loop_id` arguments
// the tool can't resolve, and downstream agents wedge.
//
// This file exposes the six federated parts as discrete substitution
// tokens — `$entity.org`, `$entity.platform`, `$entity.domain`,
// `$entity.system`, `$entity.type`, `$entity.instance` — and the same
// for `$related.*`. Authors get clean per-segment access without writing
// any string-parsing in their templates.
//
// # Behaviour
//
// Resolution requires a valid 6-part entity ID. Anything else (empty
// string, fewer/more than 6 parts, parts containing invalid characters
// per IsValidEntityID) leaves the tokens unresolved so the existing
// unresolvedTemplateVarRe warning fires — surfaces author error or
// unexpected entity-ID shape rather than silently rendering empty.
//
// # Namespace boundary
//
// `$entity.<part>` and `$related.<part>` share the entity namespace
// with `$entity.id` and `$entity.triple.<predicate>`. The substitution
// pass runs after `$entity.id` and `$entity.triple.*` so a template
// containing both `$entity.id` and `$entity.instance` resolves both
// independently. Order does not matter for correctness because the
// part tokens have no overlap with the existing `$entity.id` /
// `$entity.triple.<predicate>` token shapes.
package rule

import (
	"strings"

	"github.com/c360studio/semstreams/message"
)

// entityPartNames maps the index of a 6-part entity ID's segment to the
// substitution-token suffix authors use. Order MUST match the canonical
// federated format (see message.IsValidEntityID and CLAUDE.md):
//
//	org.platform.domain.system.type.instance
//
// Adding a 7th federated part (or renaming any existing one) requires
// coordinated updates across this slice, message.IsValidEntityID's part
// count, the unresolvedTemplateVarRe in execution_context.go, and the
// CLAUDE.md documentation. The constant lives here rather than in
// message/ because it is substitution-token configuration, not entity-
// ID validation.
var entityPartNames = [6]string{
	"org", "platform", "domain", "system", "type", "instance",
}

// applyEntityPartsSubstitutions replaces `$<prefix>.<part>` tokens in
// template against entityID for each of the six federated parts.
// `prefix` is "entity" or "related"; entityID is the full 6-part
// federated string.
//
// An entityID that fails IsValidEntityID is a no-op — the tokens
// survive substitution and trip the unresolved-template warning in
// execution_context.go, surfacing the misuse loudly. This matches the
// late-arriving-triple precedent for `$entity.triple.X` where the
// predicate is missing on the entity at fire time.
func applyEntityPartsSubstitutions(template, prefix, entityID string) string {
	if !message.IsValidEntityID(entityID) {
		return template
	}
	parts := strings.Split(entityID, ".")
	// IsValidEntityID guarantees len(parts) == 6, so the loop bound is
	// statically aligned with entityPartNames. A future relaxation of
	// IsValidEntityID would break this assertion at boot via the
	// length check above; the bounds are not separately defended here.
	result := template
	for i, part := range parts {
		token := "$" + prefix + "." + entityPartNames[i]
		result = strings.ReplaceAll(result, token, part)
	}
	return result
}
