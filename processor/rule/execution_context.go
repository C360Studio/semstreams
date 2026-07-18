// Package rule - Execution context for rule actions
package rule

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"time"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/lifecycle"
	"github.com/c360studio/semstreams/processor/rule/expression"
	"github.com/c360studio/semstreams/vocabulary"
)

// unresolvedTemplateVarRe matches any $entity.*, $related.*, $state.*,
// or $schedule.* token that survived substitution. These should never
// appear in a final string: if they do, either the predicate is missing
// from the entity at fire-time (common with race-y triple arrival), the
// ExecutionContext wasn't populated (e.g. a cron-only $schedule.* token
// reached an expression-rule path, or vice versa), or the author used a
// name that doesn't match any known substitution.
//
// Pattern rationale: first character after the dot must be a lower-case
// alphanumeric character, then dots and hyphens cover canonical predicate
// paths such as $entity.triple.agent.action.executed-by. Underscores remain
// accepted here because non-predicate namespaces (for example
// $schedule.last_fired_at) still use them. We stop at any other character so
// legitimate adjacent syntax (e.g. "$entity.id.") doesn't over-match.
var unresolvedTemplateVarRe = regexp.MustCompile(`\$(?:entity|related|state|schedule|caller|message)\.[a-z0-9][a-z0-9_.-]*`)

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
var tripleLengthRe = regexp.MustCompile(`\$(entity|related)\.triple\.([a-z0-9.-]+?)\.length\b`)

// tripleTriplesRe matches list-enumeration suffix references on
// entity or related triples (ADR-048 / PR 4 of the lifecycle bundle):
//
//	$entity.triple.<predicate>.triples
//	$related.triple.<predicate>.triples
//
// Mirrors tripleLengthRe's structure — capture group 1 is the
// namespace ("entity" or "related"); group 2 is the predicate name
// (which may itself contain dots — e.g. `gather.child.completed`).
// Trailing `\b` ensures we don't match a predicate genuinely ending
// in `.triples` as a substring of a longer token.
//
// Semantic: collects ALL element values across all matching triples,
// then renders as a JSON-encoded array string in string context. The
// JSON format is decided in ADR-048 (never CSV — commas-in-values is
// a latent production-bug class the framework refuses to introduce).
//
// Handles both multi-valued triple patterns:
//
//   - Pattern A — N triples with the same predicate, each Object is
//     scalar (e.g. gather.child.completed = N entries of distinct
//     TaskIDs). Each triple contributes one element.
//   - Pattern B — single triple with list-shaped Object (e.g.
//     coordinator.decision.subtopics = JSON-encoded []string).
//     The triple contributes all elements of its list.
//
// Mixed-shape (some triples scalar, some list) is tolerable: scalars
// contribute themselves, lists expand. Empty result (predicate not
// present) → "[]" (JSON empty array). Apps consuming the substituted
// value in a prompt or property MUST parse it as JSON per the
// canonical persona-prose template in ADR-048.
var tripleTriplesRe = regexp.MustCompile(`\$(entity|related)\.triple\.([a-z0-9.-]+?)\.triples\b`)

// tripleValueRe matches candidate scalar-value suffix references on
// entity or related triples (gh#519 / rule-evaluation-completeness):
//
//	$entity.triple.<predicate>.value
//	$related.triple.<predicate>.value
//
// Capture group 1 is the namespace ("entity" or "related"); group 2 is
// the CANDIDATE predicate — the text before the trailing `.value`.
// "Candidate" because matching this regex is necessary but not
// sufficient: applyTripleValueSubstitutions still runs arity
// disambiguation (vocabulary.ParsePredicate) on group 2 before treating
// the match as a `.value` token. See that function's doc comment for
// the full contract.
//
// The capture group's character class OVERLAPS the literal "value" it's
// anchored against (both contain a-z). Go's regexp (RE2) resolves this
// with Perl-style greedy-then-backtrack-to-fit semantics: a greedy `+`
// capture initially consumes the rest of the string, then gives back
// the minimum needed for the trailing literal `\.value\b` to match —
// which is exactly the LAST occurrence of `.value` in the token. That is
// precisely the "trailing suffix" semantic this feature needs: for
// `$entity.triple.a.value.b.value`, group 2 captures "a.value.b" (a
// legal 3-part predicate), not "a" or "a.value". Covered by
// TestTripleValueSubstitution_SuffixIsTheFinalDotValue.
var tripleValueRe = regexp.MustCompile(`\$(entity|related)\.triple\.([a-z0-9][a-z0-9.-]*)\.value\b`)

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

	// Lifecycle is the pkg/lifecycle.Manager wired by the rule
	// processor at Initialize time (ADR-047). The `$entity.lifecycle.*`
	// substitution layer reads from here; lifecycle_* action executors
	// reach the manager via ActionExecutor.lifecycle directly so this
	// field stays nil-safe — substitution tokens just survive
	// untouched (and trip the unresolved-template warning) when no
	// manager is wired.
	Lifecycle LifecycleManager

	// lifecycleParticipantCache memoizes the LookupByEntityID result
	// for the lifetime of this ExecutionContext, so multi-template
	// rules don't pay the linear-scan cost per substitution call.
	// Populated lazily inside applyLifecycleSubstitutions. Nil means
	// "not yet looked up"; a non-nil cache with a nil Participant
	// indicates a confirmed not-found (don't re-scan).
	lifecycleParticipantCache *lifecycleLookupResult
}

// lifecycleLookupResult caches the outcome of a Manager.LookupByEntityID
// call on an ExecutionContext. Memoizing in the negative case avoids
// re-scanning all registered workflows for every template substitution
// in a rule that references $entity.lifecycle.* but whose trigger
// entity isn't lifecycle-managed.
type lifecycleLookupResult struct {
	participant lifecycle.Participant
	err         error
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
// Three graceful suffix forms exist for the common "the predicate might
// not be present" cases — .length (count), .triples (JSON array of all
// values), and .value (scalar object, gh#519): all three resolve to a
// zero-ish value (0, "[]", "") on an absent predicate WITHOUT tripping
// the unresolved-template warning below. See applyTripleValueSubstitutions
// for the .value suffix's arity-disambiguation contract (it only applies
// when the text before ".value" parses as a canonical 3-part predicate;
// otherwise the whole sequence is the literal predicate, unchanged
// bare-form behavior).
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

	// List-enumeration substitutions (ADR-048). Same pre-pass
	// discipline as .length — runs before the generic triple
	// substitution so the suffix semantic is preserved against
	// list-Object stringification.
	result = ec.applyTripleTriplesSubstitutions(result)

	// Scalar-value substitutions (gh#519 / rule-evaluation-completeness).
	// Same pre-pass discipline as .length/.triples — runs before the
	// generic triple substitution so an arity-valid `.value` suffix
	// isn't orphaned by the generic loop stringifying a shorter/prefix
	// predicate match first.
	result = ec.applyTripleValueSubstitutions(result)

	// Entity triple substitutions (e.g., $entity.triple.agent.role → triple value).
	//
	// gh#160: iterate triples in descending predicate-length order so a
	// shorter predicate cannot swallow a longer one. Without the sort,
	// `$entity.triple.agent.lineage.researcher-plan` would substitute inside
	// the longer reference `$entity.triple.agent.lineage.researcher-plan-entity`
	// — leaving the `-entity` suffix dangling and producing a phantom
	// subject. Stable sort preserves the original order within equal-
	// length groups for deterministic test output.
	if ec.Entity != nil {
		sorted := make([]message.Triple, len(ec.Entity.Triples))
		copy(sorted, ec.Entity.Triples)
		sort.SliceStable(sorted, func(i, j int) bool {
			return len(sorted[i].Predicate) > len(sorted[j].Predicate)
		})
		for _, triple := range sorted {
			key := "$entity.triple." + triple.Predicate
			result = strings.ReplaceAll(result, key, fmt.Sprintf("%v", triple.Object))
		}
	}

	// Lifecycle harness substitutions (ADR-047). Resolves
	// $entity.lifecycle.{phase,terminal,workflow,workflow_def}. No-op
	// when ec.Lifecycle is nil or the entity isn't registered with
	// any workflow — surviving tokens trip the unresolved-template
	// warning below.
	result = ec.applyLifecycleSubstitutions(result)

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

// applyTripleTriplesSubstitutions replaces every
// `$entity.triple.<predicate>.triples` and
// `$related.triple.<predicate>.triples` token with a JSON-encoded
// array string of all element values across all matching triples.
// ADR-048 — the consumer-side enumeration primitive that closes
// the GH #151 fan-out join gap.
//
// Resolution semantics:
//
//   - Predicate found, N triples with scalar Objects (Pattern A,
//     e.g. gather.child.completed) → JSON array of N strings.
//   - Predicate found, single triple with list-shaped Object
//     (Pattern B, e.g. coordinator.decision.subtopics) → JSON array
//     of the list's elements.
//   - Predicate found, mixed shapes (some scalar, some list) →
//     scalars contribute themselves, lists expand. Tolerated; weird
//     in practice.
//   - Predicate NOT present on the entity → "[]" (JSON empty
//     array). Mirrors the .length substitution's "missing predicate
//     → 0" semantic — a join rule firing before any work has
//     accumulated is a legitimate authoring intent.
//   - Entity / Related nil for the referenced namespace → "[]" with
//     a Debug log (message-path rule shape, not an author error).
//   - Non-list scalar Object that fails coerceTripleObjectToStrings
//     gets stringified via fmt.Sprintf("%v", obj) — consistent with
//     coerceTripleObjectToStrings's handling of []any non-string
//     elements. Same value vs error trade-off as .length, but tipped
//     toward "best-effort string" here because the resulting list is
//     the value flowing into a downstream prompt — emitting [] would
//     silently drop data, while stringifying gives the consumer
//     something to parse.
//
// Idempotent: returns template unchanged when no `.triples` tokens
// match. Safe to call when ec.Entity / ec.Related are nil.
//
// Output is ALWAYS valid JSON. Consumers MUST parse it as JSON per
// the canonical persona-prose template documented in ADR-048
// (cheap-model substrate limitation noted there — small models may
// silently mis-parse; prefer static-N + AND-composition or upgrade
// the recipient role to general-class model).
func (ec *ExecutionContext) applyTripleTriplesSubstitutions(template string) string {
	return tripleTriplesRe.ReplaceAllStringFunc(template, func(match string) string {
		parts := tripleTriplesRe.FindStringSubmatch(match)
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
			slog.Default().Debug("triple-triples substitution: target entity nil; resolving to []",
				slog.String("token", match),
				slog.String("namespace", namespace),
				slog.String("rule_id", ec.RuleID()))
			return "[]"
		}
		values := collectPredicateValues(entity, predicate)
		// Empty list is a valid result — render as "[]" so a downstream
		// JSON parser sees a real empty array, not the Go zero value
		// "null" from json.Marshal on a nil slice.
		if len(values) == 0 {
			return "[]"
		}
		encoded, err := json.Marshal(values)
		if err != nil {
			// json.Marshal on []string can't fail under normal
			// conditions; defensive fallback to error sentinel that
			// won't pass the unresolved-template-var regex but will
			// surface in any downstream parser as malformed.
			slog.Default().Warn("triple-triples substitution: JSON encode failed (should be impossible)",
				slog.String("token", match),
				slog.String("predicate", predicate),
				slog.String("error", err.Error()),
				slog.String("rule_id", ec.RuleID()))
			return fmt.Sprintf("[ERROR_TRIPLES_ENCODE:%s]", predicate)
		}
		return string(encoded)
	})
}

// applyTripleValueSubstitutions replaces every
// `$entity.triple.<predicate>.value` / `$related.triple.<predicate>.value`
// token with the scalar object value of the FIRST triple carrying that
// predicate — gh#519 / rule-evaluation-completeness. This is the
// documented scalar-graceful member completing the `.length`/`.triples`
// family: a warn-free way to read a scalar triple value into a condition
// or template without tripping the unresolved-template WARN when the
// entity legitimately doesn't carry the predicate.
//
// ARITY DISAMBIGUATION is the core design contract, made possible by PR
// #532's canonical predicate grammar (vocabulary.ParsePredicate): the
// trailing `.value` is a recognized suffix IFF the text before it parses
// as a canonical 3-part `domain.category.property` predicate. When it
// does not parse (e.g. "a.b" is only 2 segments), the ENTIRE captured
// sequence INCLUDING ".value" is a literal predicate name — this pass
// leaves the token untouched and falls through unchanged to the generic
// triple-substitution loop below, which already resolves literal
// predicates, including ones that happen to end in ".value" (see the
// "literal predicate ending in .value is not truncated" spec scenario).
// No presence heuristic, no ambiguity — purely syntactic, decided before
// any triple lookup happens.
//
// Must run BEFORE the generic triple-substitution loop for the same
// reason as .length/.triples (see applyTripleLengthSubstitutions):
// otherwise the generic loop could stringify a shorter/prefix predicate
// match first and orphan the ".value" suffix on the result.
//
// Resolution semantics once arity disambiguation selects the `.value`
// interpretation:
//
//   - Predicate present on entity → first matching triple's Object,
//     rendered with the same scalar formatting the generic loop uses
//     (fmt.Sprintf("%v", ...)). "First" = first occurrence in
//     entity.Triples order — the same canonical-ordering precedent as
//     lookupTripleObjectTyped and collectPredicateValues. Multi-valued
//     predicate selection beyond first-match is out of scope (see the
//     proposal's Non-goals).
//   - Predicate absent (no matching triple, OR Entity/Related is nil for
//     the referenced namespace) → empty string, SILENTLY. No Warn/Debug
//     log fires here — contrast .length/.triples, which do log on the
//     nil-entity case. The entire point of `.value` is a warn-free
//     scalar-graceful read: logging on the equally-common "entity not in
//     scope" case would defeat that purpose.
//
// Because a recognized `.value` token is ALWAYS replaced (with either a
// value or ""), it never survives to warnUnresolvedTemplateVars — no
// separate suppression list is needed there. The fallback-literal case
// (arity disambiguation says "not a suffix") is unaffected: if the
// literal predicate isn't present either, the token survives untouched
// and the existing unresolved-template warning fires exactly as it does
// today for any other unresolved literal predicate — the bare
// `$entity.triple.<predicate>` form's behavior is unchanged, per spec.
func (ec *ExecutionContext) applyTripleValueSubstitutions(template string) string {
	return tripleValueRe.ReplaceAllStringFunc(template, func(match string) string {
		parts := tripleValueRe.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match // defensive — shouldn't happen given the regex
		}
		namespace, candidate := parts[1], parts[2]

		if _, err := vocabulary.ParsePredicate(candidate); err != nil {
			// Arity disambiguation: candidate isn't a canonical 3-part
			// predicate, so ".value" is part of the literal predicate
			// name, not a recognized suffix. Leave untouched — the
			// generic triple-substitution loop resolves it (or, if
			// absent, it survives to trip the unchanged bare-form
			// unresolved-template warning).
			return match
		}

		var entity *gtypes.EntityState
		switch namespace {
		case "entity":
			entity = ec.Entity
		case "related":
			entity = ec.Related
		}
		if entity == nil {
			return ""
		}
		for _, triple := range entity.Triples {
			if triple.Predicate == candidate {
				return fmt.Sprintf("%v", triple.Object)
			}
		}
		return ""
	})
}

// collectPredicateValues iterates every triple on entity matching
// predicate and produces a flat []string of element values. List-
// shaped Objects (Pattern B) contribute all their elements; scalar
// Objects (Pattern A) contribute themselves via fmt.Sprintf("%v",
// obj).
//
// Returns nil for "no matching triples"; callers distinguish nil
// from empty as needed (the JSON-encoding path renders both as "[]"
// per consumer-facing contract).
func collectPredicateValues(entity *gtypes.EntityState, predicate string) []string {
	if entity == nil {
		return nil
	}
	var out []string
	for _, triple := range entity.Triples {
		if triple.Predicate != predicate {
			continue
		}
		if items, ok := coerceTripleObjectToStrings(triple.Object); ok {
			// Pattern B (one triple, list Object) OR Pattern A
			// triple whose Object happens to be a list shape.
			out = append(out, items...)
			continue
		}
		// Pattern A: scalar Object. Stringify and contribute as
		// a single element. fmt.Sprintf("%v") matches the
		// coerceTripleObjectToStrings []any path's handling of
		// non-string elements, so the formatting is consistent
		// across the two paths.
		out = append(out, fmt.Sprintf("%v", triple.Object))
	}
	return out
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
//
// A recognized `.length` / `.triples` / `.value` suffix token never
// reaches this function still `$`-prefixed: each pre-pass (see
// applyTripleLengthSubstitutions / applyTripleTriplesSubstitutions /
// applyTripleValueSubstitutions) unconditionally replaces its matched
// tokens — with a real value, or with the documented absent-predicate
// sentinel (0 / "[]" / "") — so no separate suppression list is needed
// here. Only the arity-disambiguation fallback case for `.value` (the
// text before it isn't a valid 3-part predicate, so the whole sequence
// is a literal predicate) can still surface here unresolved, and that is
// intentional: it is exactly the unchanged bare-form behavior.
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
