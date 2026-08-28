// Package rule - Entity-part substitution context.
//
// Rule templates frequently want a SEGMENT of an entity ID rather than
// the whole 6-part federated string. The canonical example is the
// `read_loop_result` tool: AGENT_LOOPS keys are `COMPLETE_<bare-uuid>`,
// where `<bare-uuid>` is the trailing `instance` segment of the full
// agent-loop entity ID `org.platform.agentic-loop.agent.execution.<uuid>`.
// Authors who reach for `$entity.id` in their rule prompt template hand
// the LLM the full string; the LLM then constructs `loop_id` arguments
// the tool can't resolve, and downstream agents wedge.
//
// This file exposes the six federated parts as discrete substitution
// tokens — `$entity.org`, `$entity.platform`, `$entity.system`,
// `$entity.domain`, `$entity.type`, `$entity.instance` — and the same
// for `$related.*`. Authors get clean per-segment access without writing
// any string-parsing in their templates.
//
// # Behaviour
//
// Resolution requires a canonical 6-part entity ID. Anything else (empty
// string, fewer/more than 6 parts, parts containing invalid characters
// per pkg/types.ParseEntityID) leaves the tokens unresolved so the existing
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

	semtypes "github.com/c360studio/semstreams/pkg/types"
)

// entityPartNames are the substitution-token suffixes authors use for the six
// canonical positions, in canonical order org.platform.system.domain.type.instance.
// Every token resolves from the NAMED field of pkg/types.ParseEntityID (see
// entityPartValues), never from a raw index into the dotted string, so a token
// keeps its meaning across any canonical-order change (rule-engine
// requirement "Entity-segment substitution tokens are named by position
// meaning"). The unresolvedTemplateVarRe in execution_context.go and the
// CLAUDE.md documentation list the same six names.
var entityPartNames = [6]string{
	"org", "platform", "system", "domain", "type", "instance",
}

// entityPartValues maps each token name to the named field it resolves from.
func entityPartValues(parsed semtypes.EntityID) [6]string {
	return [6]string{
		parsed.Org,      // $<prefix>.org
		parsed.Platform, // $<prefix>.platform
		parsed.System,   // $<prefix>.system
		parsed.Domain,   // $<prefix>.domain
		parsed.Type,     // $<prefix>.type
		parsed.Instance, // $<prefix>.instance
	}
}

// applyEntityPartsSubstitutions replaces `$<prefix>.<part>` tokens in
// template against entityID for each of the six canonical positions.
// `prefix` is "entity" or "related"; entityID is the full 6-part
// federated string.
//
// An entityID that fails canonical validation is a no-op — the tokens
// survive substitution and trip the unresolved-template warning in
// execution_context.go, surfacing the misuse loudly. This matches the
// late-arriving-triple precedent for `$entity.triple.X` where the
// predicate is missing on the entity at fire time.
func applyEntityPartsSubstitutions(template, prefix, entityID string) string {
	parsed, err := semtypes.ParseEntityID(entityID)
	if err != nil {
		return template
	}
	values := entityPartValues(parsed)
	result := template
	for i, name := range entityPartNames {
		result = strings.ReplaceAll(result, "$"+prefix+"."+name, values[i])
	}
	return result
}
