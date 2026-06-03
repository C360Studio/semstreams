// Package rule — Typed single-token substitution for action.Object.
//
// gh#207 fix: `Action.Object` is typed `string`, but the destination
// `message.Triple.Object` is typed `any` and documented to carry
// `float64 / bool / string / int` literals (see message/triple.go:43).
// `ExecutionContext.SubstituteVariables` is `string → string` — it
// always returns a string, so a substituted Object loses its source
// type. The canonical reproducer is `update_triple` upserting
// `autoresearch.best.value` from a float-typed source triple: the
// destination lands as the stringified float, breaking both downstream
// consumers (typed Go reads) AND rule conditions (`toFloat64` in
// expression/evaluator.go has no string case, so numeric `lt`/`gt`
// against the stringified value falls to lexicographic compare and
// silently misorders across power-of-10 boundaries).
//
// `SubstituteVariablesTyped` is the typed counterpart, applied at the
// `add_triple` / `update_triple` call sites BEFORE the string
// substitution path. It resolves the template only when it consists
// of exactly one supported substitution token (no surrounding text,
// no concatenation) — for mixed templates and literal-only strings,
// the caller falls back to `SubstituteVariables` because string-concat
// semantics require string output.
//
// First-match semantic preserved for `$entity.triple.X` /
// `$related.triple.X`: if multiple triples on the entity carry the
// same predicate with DIFFERENT Object types (degenerate but legal),
// the FIRST stamped triple wins by both type AND value. Prior to this
// helper, all values were string-coerced; the new behavior is
// observable only in the degenerate mixed-type case. Cross-repo survey
// (semstreams + semteams rule packs, 19 sites) found zero production
// rules that stamp the same predicate with mixed Object types.
//
// Two known semantic divergences vs. the pre-fix string path. Both are
// degenerate (no production rule exercises either) but documented so
// the next reader doesn't have to re-derive them:
//
//  1. Predicates literally named `<X>.length` or `<X>.triples` —
//     pre-fix, the string path's pre-pass (applyTripleLengthSubstitutions
//     / applyTripleTriplesSubstitutions) interpreted the trailing token
//     as a count/enumeration suffix and resolved against an entity with
//     predicate `<X>`. Post-fix, the typed regex `[\w.-]+` captures the
//     full predicate INCLUDING the trailing `.length` / `.triples`, so
//     a triple genuinely named that wins. If a future rule pack needs
//     the pre-fix suffix-interpretation semantic, change the regex to
//     reject these suffixes (e.g. negative lookahead) and let the call
//     fall back to the string path that handles them.
//
//  2. `$message.<path>` resolving to a JSON `null` — ExtractMessageValue
//     in expression/message_path.go returns (nil, true) for a present-
//     but-null terminal. The typed path propagates that nil to the
//     destination Triple's Object as a typed nil, where the pre-fix
//     string path rendered it as the literal string "<nil>". The
//     post-fix behavior is more honest about the absence; consumers
//     that distinguished `Object == "<nil>"` as a sentinel will break
//     (no current consumer surveyed does this).
//
// NO reflection in the resolution path. Type passes through as `any`
// without inspection — the existing `triple.Object` is already `any`,
// so we propagate it directly. `reflect.TypeOf` is used only in tests
// to assert source/destination type equality.
package rule

import (
	"regexp"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/processor/rule/expression"
)

// typedEntityTripleRe matches `$entity.triple.<predicate>` when the
// ENTIRE template is exactly that token. Anchored on both ends so a
// mixed template (`"prefix-$entity.triple.X"`) or trailing suffix
// (`"$entity.triple.X.length"`, `"$entity.triple.X "`) fails the
// match and the caller falls back to string substitution.
//
// Predicate character class allows letters, digits, dot, hyphen,
// underscore — matching the predicate shapes used in production
// (e.g. `lineage.run-loop-entity-id` carries a hyphen).
var typedEntityTripleRe = regexp.MustCompile(`^\$entity\.triple\.([\w.-]+)$`)

// typedRelatedTripleRe — mirror of typedEntityTripleRe for the related
// entity. Same anchoring + character-class discipline.
var typedRelatedTripleRe = regexp.MustCompile(`^\$related\.triple\.([\w.-]+)$`)

// typedMessageRe matches `$message.<dotted.path>` when the entire
// template is exactly that token. Path segments are word-only (no
// hyphens) — mirrors messageTokenRe in message_substitution.go for
// the existing string path. Anchored.
var typedMessageRe = regexp.MustCompile(`^\$message\.(\w+(?:\.\w+)*)$`)

// SubstituteVariablesTyped attempts to resolve template to a typed
// value when it is EXACTLY one supported substitution token (no
// surrounding text, no concatenation, no recognized suffix like
// `.length` or `.triples`).
//
// Return contract:
//
//   - (value, true) — template matched a single supported token AND
//     resolution found a value. value is typed `any`, carrying the
//     source's Go type unchanged.
//   - (nil, false) — every other case: mixed template, literal-only
//     string (no `$`), unsupported namespace, single token whose
//     target doesn't exist (e.g. `$entity.triple.X` where X isn't on
//     the entity at fire time), or a recognized suffix like
//     `.length` / `.triples` (those carry typed semantics — int and
//     `[]string` — but are deferred to the string path which already
//     handles them via applyTripleLengthSubstitutions /
//     applyTripleTriplesSubstitutions).
//
// Callers that need a string result for concatenation or templating
// MUST fall back to SubstituteVariables when this returns false.
// Designed specifically for `action.Object` on `add_triple` /
// `update_triple` where preserving the source's numeric/bool type is
// required (gh#207).
//
// Supported single-token patterns:
//
//   - "$entity.triple.<predicate>" — first matching triple's Object
//     on the trigger entity (type `any`, source-preserving).
//   - "$related.triple.<predicate>" — same on the related entity.
//   - "$message.<dotted.path>" — typed value from MessageData via
//     expression.ExtractMessageValue. JSON-decoded numbers arrive as
//     `float64` by default unless the producer used `json.Number`.
//   - "$state.iteration" — `int` (MatchState.Iteration).
//   - "$state.max_iterations" — `int` (MatchState.MaxIterations).
//
// Inherently-string namespaces ($entity.id, $now, $caller.*,
// $schedule.*, entity-part segments) are NOT supported here — callers
// don't lose anything by falling through to the string path for
// those since the destination value is already a string. Returning
// (nil, false) for them keeps the caller dispatch logic simple
// (one boolean check).
//
// The empty template, the bare "$" string, and templates that don't
// start with "$" all short-circuit to (nil, false) without scanning.
func (ec *ExecutionContext) SubstituteVariablesTyped(template string) (any, bool) {
	if len(template) < 2 || template[0] != '$' {
		return nil, false
	}

	if m := typedEntityTripleRe.FindStringSubmatch(template); m != nil {
		return lookupTripleObjectTyped(ec.Entity, m[1])
	}
	if m := typedRelatedTripleRe.FindStringSubmatch(template); m != nil {
		return lookupTripleObjectTyped(ec.Related, m[1])
	}
	if m := typedMessageRe.FindStringSubmatch(template); m != nil {
		if ec.MessageData == nil {
			return nil, false
		}
		return expression.ExtractMessageValue(expression.MessageFields(ec.MessageData), m[1])
	}

	switch template {
	case "$state.iteration":
		if ec.State == nil {
			return nil, false
		}
		return ec.State.Iteration, true
	case "$state.max_iterations":
		if ec.State == nil {
			return nil, false
		}
		return ec.State.MaxIterations, true
	}

	return nil, false
}

// lookupTripleObjectTyped scans entity.Triples for the first triple
// matching predicate and returns its Object as-is (typed `any`).
// Returns (nil, false) when entity is nil OR no triple matches —
// caller treats as "single-token-but-unresolved" and falls back to
// the string path (which logs the unresolved-template warning).
//
// First-match: in the degenerate case where multiple triples carry
// the same predicate with different types, the FIRST stamped wins by
// both type AND value. Pre-fix behaviour string-coerced every
// candidate so the type-mismatch was invisible; cross-repo survey
// found zero production rules that exercise this corner.
//
// Linear scan matches the existing inline pattern in
// substituteVariablesWith — no reflection, no allocation beyond the
// returned `any` interface header.
func lookupTripleObjectTyped(entity *gtypes.EntityState, predicate string) (any, bool) {
	if entity == nil {
		return nil, false
	}
	for _, triple := range entity.Triples {
		if triple.Predicate == predicate {
			return triple.Object, true
		}
	}
	return nil, false
}
