// Package rule - Execution context for rule actions
package rule

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/processor/rule/expression"
)

// unresolvedTemplateVarRe matches any $entity.*, $related.*, $state.*,
// or $schedule.* token that survived substitution. These should never
// appear in a final string: if they do, either the predicate is missing
// from the entity at fire-time (common with race-y triple arrival), the
// ExecutionContext wasn't populated (e.g. a cron-only $schedule.* token
// reached an expression-rule path, or vice versa), or the author used a
// name that doesn't match any known substitution.
//
// Pattern rationale: first character after the dot must be a word char
// (letter/digit/underscore), then any run of word chars and dots lets us
// match deep predicate paths like $entity.triple.agent.loop.role. We stop
// at any other character so legitimate adjacent syntax (e.g. "$entity.id.")
// doesn't over-match.
var unresolvedTemplateVarRe = regexp.MustCompile(`\$(?:entity|related|state|schedule|caller|message)\.\w[\w.]*`)

// tripleLengthRe matches list-length suffix references on entity or
// related triples (#149 / ADR-046 Phase 1 follow-up):
//
//	$entity.triple.<predicate>.length
//	$related.triple.<predicate>.length
//
// Capture group 1 is the namespace ("entity" or "related"); group 2 is
// the predicate name (which may itself contain dots — e.g.
// `coordinator.decision.subtopics`). The trailing `\b` ensures we don't
// match a predicate that genuinely ends in `.length` as a substring of
// a longer token.
//
// Edge case: a predicate genuinely named `<foo>.length` would collide.
// The substitution prefers the suffix interpretation (count semantics)
// over the predicate-name interpretation because the count form is
// the documented use case and a literal `.length` predicate is rare;
// authors who need the literal can write the value into their template
// directly. Worth documenting if it ever becomes an issue in practice.
var tripleLengthRe = regexp.MustCompile(`\$(entity|related)\.triple\.([\w.]+?)\.length\b`)

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

	// Schedule carries cron-rule context (rule ID, spec, prior fire
	// timestamp). Populated by CronScheduler.fire on time-driven
	// dispatches; nil for message-path and KV-watch rules. The
	// `$schedule.*` substitution layer reads this; see
	// cron_substitution.go for the namespace contract.
	Schedule *ScheduleContext

	// Caller carries the identity of the caller whose request triggered
	// rule evaluation (user ID, role, org). Populated by the rule
	// processor when an authenticated caller is present in scope (e.g.
	// HTTP-originated messages in Tag 2+). Nil for cron-fired and pure
	// KV-watch rules that have no caller in scope. The `$caller.*`
	// substitution layer reads this; see caller_substitution.go for the
	// namespace contract.
	Caller *CallerContext

	// MessageData carries the payload of the inbound message that
	// triggered rule evaluation, as a generic map for dotted-path
	// access. Populated on the message-path (NATS-subscribed rules)
	// from `GenericJSONPayload.Data`. Nil for entity-state-driven and
	// cron-fired evaluations — those have no inbound message in scope.
	// The `$message.*` substitution layer reads this; see
	// message_substitution.go for the namespace contract.
	//
	// Required for ADR-039 tool-call governance: rule templates need
	// to splice fields from the proposed tool-call payload (`loop_id`,
	// `call_id`, `tool_name`, etc.) into verdict subjects and audit
	// reasons.
	MessageData map[string]any
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
//   - $now: Current wallclock as RFC3339 UTC (always available)
//   - $entity.id: The primary entity ID (full 6-part federated string)
//   - $entity.org / $entity.platform / $entity.domain / $entity.system / $entity.type / $entity.instance:
//     individual segments of a valid 6-part entity ID. Use $entity.instance
//     to pass the bare loop UUID into tools like read_loop_result without
//     handing the LLM the full federated string. Tokens survive
//     substitution (and trip the unresolved-template warning) when the
//     entity ID isn't a valid 6-part form — see entity_substitution.go.
//   - $related.id: The related entity ID (for pair rules)
//   - $related.org / $related.platform / $related.domain / $related.system / $related.type / $related.instance:
//     individual segments of a valid 6-part related entity ID, mirror of the entity set.
//   - $state.iteration: Current iteration count
//   - $state.max_iterations: Configured max iterations
//   - $schedule.id: Cron rule ID (cron rules only)
//   - $schedule.spec: Cron expression (cron rules only)
//   - $schedule.last_fired_at: Prior fire timestamp, RFC3339 UTC; empty on first fire
//   - $caller.id: Caller identity ID (caller-aware rules only)
//   - $caller.role: Caller role claim (caller-aware rules only)
//   - $caller.org: Caller organization claim (caller-aware rules only)
//   - $message.<field_path>: dotted-path access into the inbound
//     message data (message-path rules only). E.g. $message.loop_id
//     or $message.tool_args.command. Tokens survive substitution (and
//     trip the unresolved-template warning) when MessageData is nil
//     or the path doesn't resolve — see message_substitution.go.
//
// Entity triple values can be accessed via $entity.triple.<predicate> syntax.
//
// If any template variable survives substitution (e.g. $entity.triple.X
// where X isn't on the entity at fire time — a common race with
// late-arriving triples, or a cron-only $schedule.* token reaching an
// expression rule), the literal stays in the output and a warning is
// logged so the author sees the silent-pass. Downstream callers that
// feed the result into an identifier (KV key, NATS subject) will then
// get a loud failure instead of mysteriously wrong behaviour.
func (ec *ExecutionContext) SubstituteVariables(template string) string {
	return ec.substituteVariablesWith(template, nil)
}

// SubstituteVariablesWithIterVar runs substitution with an extra
// single-variable overlay bound for the duration of the call. ADR-046
// Phase 1: for_each iteration in publish_agent passes the current
// item value as `varName`, which substitutes `$<varName>` in the
// template. Other namespaces ($entity, $message, $state, etc.) resolve
// as usual. Empty varName degenerates to SubstituteVariables — useful
// for callers that conditionally bind a var.
func (ec *ExecutionContext) SubstituteVariablesWithIterVar(template, varName, value string) string {
	if varName == "" {
		return ec.SubstituteVariables(template)
	}
	return ec.substituteVariablesWith(template, map[string]string{varName: value})
}

// substituteVariablesWith is the shared implementation under
// SubstituteVariables and SubstituteVariablesWithIterVar. The overlay
// map (nil when no iter-var is bound) carries names without the leading
// `$` — substitution writes `$<name>` → value as a plain string-replace.
// Overlay variables apply before the unresolved-template warning, so a
// matched iter-var doesn't trip the warning even if the name doesn't
// belong to a recognised framework namespace.
//
// Phase 1 (#134 / ADR-046) ships a single overlay key — the for_each
// iter-var. Phase 2 (gated-DAG dispatch) may introduce multi-key
// overlays (e.g. $item + $index); when it does, the iteration order
// here becomes load-bearing for prefix-shadowing cases ($i vs $item
// would collide). Phase 2 must either sort keys longest-first before
// substitution or namespace its overlay keys to avoid the foot.
func (ec *ExecutionContext) substituteVariablesWith(template string, overlay map[string]string) string {
	result := template

	// Time substitutions
	result = strings.ReplaceAll(result, "$now", time.Now().UTC().Format(time.RFC3339))

	// Core ID substitutions
	result = strings.ReplaceAll(result, "$entity.id", ec.EntityID)
	result = strings.ReplaceAll(result, "$related.id", ec.RelatedID)

	// Per-segment substitutions for valid 6-part entity IDs. No-op for
	// non-conforming IDs; the unresolved-template warning below surfaces
	// the misuse. See entity_substitution.go for the namespace contract.
	result = applyEntityPartsSubstitutions(result, "entity", ec.EntityID)
	result = applyEntityPartsSubstitutions(result, "related", ec.RelatedID)

	// State substitutions
	if ec.State != nil {
		result = strings.ReplaceAll(result, "$state.iteration", fmt.Sprintf("%d", ec.State.Iteration))
		result = strings.ReplaceAll(result, "$state.max_iterations", fmt.Sprintf("%d", ec.State.MaxIterations))
	}

	// List-length substitutions (#149). Resolved BEFORE the generic
	// triple substitution below, because the generic path would
	// stringify the list Object as `[a b c]` first, after which the
	// `.length` suffix would be orphaned (`[a b c].length`) and
	// trip the unresolved-template warning instead of resolving to
	// the count. Pre-passing keeps the suffix semantic correct.
	result = ec.applyTripleLengthSubstitutions(result)

	// Entity triple substitutions (e.g., $entity.triple.agent.role → triple value)
	if ec.Entity != nil {
		for _, triple := range ec.Entity.Triples {
			key := "$entity.triple." + triple.Predicate
			result = strings.ReplaceAll(result, key, fmt.Sprintf("%v", triple.Object))
		}
	}

	// Schedule substitutions (cron rules only). No-op when ec.Schedule
	// is nil; unknown $schedule.* tokens will then trip the
	// unresolved-template warning below.
	result = applyScheduleSubstitutions(result, ec.Schedule)

	// Caller substitutions. No-op when ec.Caller is nil; unknown
	// $caller.* tokens will then trip the unresolved-template warning
	// below.
	result = applyCallerSubstitutions(result, ec.Caller)

	// Message-data substitutions (message-path rules only). No-op when
	// ec.MessageData is nil; unknown $message.* tokens then trip the
	// unresolved-template warning below.
	result = applyMessageSubstitutions(result, ec.MessageData)

	// Iter-var overlay (ADR-046 Phase 1 for_each). Applied last so an
	// iter-var token like $subtopic resolves even if the operator
	// accidentally chose a name that overlaps a framework namespace —
	// last-write-wins is the predictable behaviour. Iter-vars are
	// always present-or-absent (the for_each loop never binds nil);
	// missing tokens behave the same as any other unresolved template
	// variable and trip the warning below.
	for name, value := range overlay {
		result = strings.ReplaceAll(result, "$"+name, value)
	}

	ec.warnUnresolvedTemplateVars(template, result)

	return result
}

// SubstituteConditionValues returns a copy of conds with each
// string-typed Value field run through ec.SubstituteVariables.
// Non-string Values pass through unchanged. Required so rule
// authors can write `value: "$entity.triple.foo.length"` and have
// it resolve before the operator sees it — without this, condition
// values flow from JSON straight to applyOperator, which then
// coerce-errors on the literal template string.
//
// Caught by the example-fan-out integration test on #149's first
// cut: rule 03's `value: "$entity.triple.coordinator.decision.subtopics.length"`
// was reaching length_eq's compareValue as the literal template,
// not as the substituted integer. Pre-existing latent gap surfaced
// the moment a reference config tried to use substitution in a
// condition value. The fix lives here rather than in the evaluator
// because substitution semantics belong with the rule package's
// ExecutionContext, not with the pure expression evaluator.
//
// Nil ec is a no-op pass-through (test harnesses that drive
// Evaluate directly without an EC still work).
//
// ConditionExpression.From is intentionally NOT substituted today.
// `from` is consumed only by OpTransition (which compares against
// $prev.* state fields outside the regular operator path), and no
// production rule today uses substituted templates in From. If a
// transition rule needs `from: "$entity.triple.X"` semantics in
// the future, extend this helper to also substitute string-typed
// From values — the change is local.
func SubstituteConditionValues(conds []expression.ConditionExpression, ec *ExecutionContext) []expression.ConditionExpression {
	if ec == nil || len(conds) == 0 {
		return conds
	}
	out := make([]expression.ConditionExpression, len(conds))
	for i, c := range conds {
		out[i] = c
		s, ok := c.Value.(string)
		if !ok || !strings.Contains(s, "$") {
			continue
		}
		out[i].Value = ec.SubstituteVariables(s)
	}
	return out
}

// applyTripleLengthSubstitutions replaces every
// `$entity.triple.<predicate>.length` and
// `$related.triple.<predicate>.length` token in template with the
// decimal-string integer count of triples whose Object is a list
// (using the same shape truth-table as coerceTripleObjectToStrings).
// #149 — closes the dynamic-counter join gap so `length_eq` rules can
// compare against the source-list size rather than a hardcoded N.
//
// Resolution semantics:
//   - Predicate found, Object is list-shaped → integer count.
//   - Predicate found, Object is non-list (scalar string, etc.) →
//     log Warn at substitution time + leave the token verbatim. The
//     existing unresolved-template warning then trips at the end of
//     substitution, surfacing the author error.
//   - Predicate not found on the entity → "0". Mirrors #148's array-
//     operator "missing predicate ⇒ empty array" semantic so a join
//     rule that fires "before any work spawned" is a legitimate
//     authoring intent.
//   - Entity / Related nil for the referenced namespace → "0" with
//     a Debug log (no entity in scope is a message-path rule shape,
//     not an authoring error).
//
// Idempotent: returns template unchanged when no `.length` tokens
// match. Safe to call when ec.Entity / ec.Related are nil.
func (ec *ExecutionContext) applyTripleLengthSubstitutions(template string) string {
	return tripleLengthRe.ReplaceAllStringFunc(template, func(match string) string {
		parts := tripleLengthRe.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match // defensive — shouldn't happen given the regex
		}
		namespace, predicate := parts[1], parts[2]

		var entity *gtypes.EntityState
		switch namespace {
		case "entity":
			entity = ec.Entity
		case "related":
			entity = ec.Related
		}
		if entity == nil {
			slog.Default().Debug("triple-length substitution: target entity nil; resolving to 0",
				slog.String("token", match),
				slog.String("namespace", namespace),
				slog.String("rule_id", ec.RuleID()))
			return "0"
		}
		for _, triple := range entity.Triples {
			if triple.Predicate != predicate {
				continue
			}
			items, ok := coerceTripleObjectToStrings(triple.Object)
			if !ok {
				// Loud Warn + self-describing sentinel. Cannot leave
				// the token verbatim because the generic triple-
				// substitution loop below would still match the
				// inner `$entity.triple.<predicate>` substring and
				// stringify the Object, leaving a nonsense
				// `<value>.length` in the output. The sentinel starts
				// with `[` (no `$` prefix) so it survives the generic
				// loop AND is visible in any prompt/subject the
				// operator inspects.
				slog.Default().Warn("triple-length substitution: Object is not list-shaped; replacing token with error sentinel",
					slog.String("token", match),
					slog.String("predicate", predicate),
					slog.String("object_type", fmt.Sprintf("%T", triple.Object)),
					slog.String("rule_id", ec.RuleID()),
					slog.String("hint", "the .length suffix requires the triple Object to be []string, []any, or a JSON-encoded list string — see coerceTripleObjectToStrings"))
				return fmt.Sprintf("[ERROR_LENGTH_NOT_LIST:%s]", predicate)
			}
			return fmt.Sprintf("%d", len(items))
		}
		// Predicate not present → 0 (mirrors #148 array-op semantics).
		return "0"
	})
}

// ResolveListValue extracts a list-typed value from a substitution
// reference. ADR-046 Phase 1 for_each uses this — the rule author
// writes `for_each: "$entity.triple.coordinator.decision.subtopics"`
// and the framework needs the list shape preserved (not stringified
// via fmt.Sprintf as SubstituteVariables would do).
//
// Supported reference shapes:
//
//   - "$entity.triple.<predicate>" — triple object on the trigger
//     entity. The Object field is stored as `any`, so a list-shaped
//     value (e.g. set by a tool that stamped []string) round-trips as
//     []any after JSON unmarshal. JSON-encoded string lists (e.g.
//     `"[\"a\",\"b\"]"`) are parsed transparently.
//   - "$related.triple.<predicate>" — same on the related entity.
//
// Other namespaces ($message.*, $state.*, etc.) are not supported in
// Phase 1 — the use case is decomposer-emitted subtopics on the
// trigger entity. Adding namespaces is additive when needed.
//
// Returns (items, true) on successful list resolution. (nil, false)
// when the reference resolved but the value isn't list-shaped (e.g.
// the operator referenced a string predicate); the caller logs and
// treats this as a single-iteration degenerate case so author errors
// surface loudly rather than as a silent no-op. (nil, true) when the
// reference resolved to an empty list — a valid no-iteration case.
func (ec *ExecutionContext) ResolveListValue(reference string) ([]string, bool) {
	if !strings.HasPrefix(reference, "$entity.triple.") && !strings.HasPrefix(reference, "$related.triple.") {
		return nil, false
	}
	var entity *gtypes.EntityState
	var predicate string
	switch {
	case strings.HasPrefix(reference, "$entity.triple."):
		entity = ec.Entity
		predicate = strings.TrimPrefix(reference, "$entity.triple.")
	case strings.HasPrefix(reference, "$related.triple."):
		entity = ec.Related
		predicate = strings.TrimPrefix(reference, "$related.triple.")
	}
	if entity == nil {
		return nil, false
	}
	for _, triple := range entity.Triples {
		if triple.Predicate != predicate {
			continue
		}
		return coerceTripleObjectToStrings(triple.Object)
	}
	return nil, false
}

// coerceTripleObjectToStrings extracts a []string from a triple's
// Object field. Handles the three concrete shapes that round-trip
// through JSON: native []any (typical after JSON unmarshal of a list),
// []string (Go-constructed entity states in tests), and string-typed
// JSON-encoded list (the wire shape if a tool stamps the list as a
// JSON string for transport compactness — coordinator.decision.subtopics
// uses this so the triple Object stays primitive-typed on the wire).
// Returns (nil, false) on any other shape — the caller treats that as
// "author referenced a non-list predicate" and logs.
//
// Non-string items in []any / parsed JSON lists are stringified via
// fmt.Sprintf("%v", item) — intended use case is string lists (the
// decide tool's []string subtopics, future similar emissions).
// Numeric / bool lists work but cross a silent type boundary; if a
// rule author iterates `[1, 2, 3]` they get `["1", "2", "3"]` bound to
// the iter-var without warning. Acceptable for Phase 1 because no
// production caller emits non-string lists today; revisit if that
// changes.
func coerceTripleObjectToStrings(obj any) ([]string, bool) {
	switch v := obj.(type) {
	case []string:
		return v, true
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			out = append(out, fmt.Sprintf("%v", item))
		}
		return out, true
	case string:
		// JSON-encoded list fallback. Tools that stamp lists as a
		// triple via the canonical wire path (json.Marshal of
		// []string) produce this shape. Bail to "not a list" only on
		// a parse failure; trim whitespace first so leading/trailing
		// space doesn't fail the bracket-prefix sniff.
		trimmed := strings.TrimSpace(v)
		if !strings.HasPrefix(trimmed, "[") {
			return nil, false
		}
		var parsed []any
		if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
			return nil, false
		}
		out := make([]string, 0, len(parsed))
		for _, item := range parsed {
			out = append(out, fmt.Sprintf("%v", item))
		}
		return out, true
	}
	return nil, false
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
