// Package rule — $entity.lifecycle.* substitution (ADR-047).
//
// Four resolvable paths read from the trigger entity's Participant
// state via the wired pkg/lifecycle.Manager:
//
//   - $entity.lifecycle.phase        → Participant.Phase()
//   - $entity.lifecycle.terminal     → "true" / "false"
//   - $entity.lifecycle.workflow     → Participant.Workflow()
//   - $entity.lifecycle.workflow_def → JSON of WorkflowDef (rare;
//     tooling-shaped, not condition-shaped)
//
// The Participant lookup is O(workflows) — a match over the registered
// EntityIDPattern set (Manager.findRegistrationForEntity) — plus one
// direct-key KV Get (the key IS the entity ID; no bucket scan), on the
// first call for an entity on an ExecutionContext; subsequent calls in
// the same ec hit the cache. Negative lookups (entity isn't
// lifecycle-managed) are also cached so that a multi-template rule pays
// that cost at most once per fire even when the trigger entity has no
// Participant.
//
// All paths leave tokens verbatim on lookup failure or missing-manager
// — the unresolved-template warning downstream surfaces the issue.
package rule

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"sort"
	"strings"

	"github.com/c360studio/semstreams/pkg/lifecycle"
	"github.com/c360studio/semstreams/processor/rule/expression"
)

// LifecycleLookup is the READ-ONLY slice of the Lifecycle harness that condition
// resolution needs — the two lookups that answer "what phase is this entity in"
// and "what does this workflow declare".
//
// It exists because resolution is a read and must not demand a write capability.
// The rule package's LifecycleManager (actions.go) also carries TransitionWith,
// Complete, Fail and AssertRuleWritable — powers a match check has no business
// holding, and which a caller would otherwise hand over just to ask a question.
// Taking the narrow set means a read-only caller can supply a read-only object,
// and a test double is two methods rather than eight.
//
// LifecycleManager and *lifecycle.Manager both satisfy it, so every existing
// caller passes unchanged.
type LifecycleLookup interface {
	LookupByEntityID(ctx context.Context, entityID string) (lifecycle.Participant, error)
	GetWorkflowDefinition(workflow string) (lifecycle.WorkflowDef, error)
}

// PopulateLifecycleStateFields resolves the Participant for entityID
// via manager.LookupByEntityID and writes the resulting
// $entity.lifecycle.* keys into fields. The keys live in the same
// shared stateFields map the expression evaluator consults for
// $state.*/$prev.*/$entity.lifecycle.* (see evaluator.go's
// evaluateConditionWithStateAndMessage). Callers populate this
// alongside their normal $state/$prev keys before invoking the
// evaluator.
//
// No-ops on a nil manager, empty entityID, nil fields, or a lookup
// failure — surviving $entity.lifecycle.* tokens then surface as
// missing-field errors at evaluation, which is the loud-fail signal
// the operator wants when a rule references the harness for an
// entity that isn't lifecycle-managed.
//
// O(workflows) plus one direct-key KV Get per call (no bucket scan);
// intended for the rule-fire path, not per-message-shaped. See
// LookupByEntityID's contract.
func PopulateLifecycleStateFields(ctx context.Context, manager LifecycleLookup, entityID string, fields expression.StateFields) {
	// Tolerant wrapper: the stateful path treats a lookup failure as "no
	// lifecycle state", which is correct for firing. populateLifecycleStateFields
	// carries the error for callers that must not answer without it.
	_ = populateLifecycleStateFields(ctx, manager, entityID, fields)
}

// populateLifecycleStateFields is PopulateLifecycleStateFields with the lookup
// error RETAINED.
//
// The split exists because "the caller supplied a Manager" and "lifecycle state
// resolved" are different facts, and conflating them reproduces this capability's
// defining defect. A supplied-but-failing lookup leaves `$entity.lifecycle.*`
// unresolved; the evaluator then returns `false, nil` for an optional field, and a
// stateless caller reads that confident no-match as "nothing owed".
//
// The stateful path keeps swallowing the error — a rule that cannot resolve
// lifecycle state must not fire, and false is the right answer for firing. The
// stateless path refuses instead, but only when a condition actually needs the
// state (see Matches). Same resolution, two dispositions, one implementation.
func populateLifecycleStateFields(ctx context.Context, manager LifecycleLookup, entityID string, fields expression.StateFields) error {
	if isNilLookup(manager) || entityID == "" || fields == nil {
		return nil
	}
	p, err := manager.LookupByEntityID(ctx, entityID)
	if err != nil {
		return err
	}
	fields["$entity.lifecycle.phase"] = p.Phase()
	fields["$entity.lifecycle.terminal"] = p.IsTerminal()
	fields["$entity.lifecycle.workflow"] = p.Workflow()
	return nil
}

// isNilLookup reports whether a LifecycleLookup is absent, including the TYPED-nil
// case that a plain `== nil` misses.
//
// `var m *lifecycle.Manager; Matches(ctx, def, state, m)` produces an interface
// value with a non-nil type and a nil pointer. `manager == nil` is false for it, so
// an untreated typed nil passes an "is a lookup available" guard and then panics
// inside LookupByEntityID. An exported API taking an interface has to handle this;
// the caller's `m` genuinely IS absent and deserves the absent-lookup behavior, not
// a crash.
func isNilLookup(manager LifecycleLookup) bool {
	if manager == nil {
		return true
	}
	v := reflect.ValueOf(manager)
	switch v.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return v.IsNil()
	default:
		return false
	}
}

const lifecycleSubstitutionPrefix = "$entity.lifecycle."

// lifecycleSubstitutionRule describes one resolvable suffix path
// under `$entity.lifecycle.`. Rules are sorted at package init by
// suffix length DESCENDING so the longest match always wins (S4
// reviewer rec — locks the prefix-superset invariant structurally,
// not via inline ordering tricks that future additions can break).
//
// `resolve` returns (value, ok). ok=false leaves the token verbatim
// so the unresolved-template warning downstream surfaces the misuse
// (loud-fail per [[feedback_no_clean_except_handwave]]).
type lifecycleSubstitutionRule struct {
	suffix  string
	resolve func(ec *ExecutionContext, p lifecycleParticipantSnapshot) (string, bool)
}

// lifecycleSubstitutionRules is the dispatch table for
// `$entity.lifecycle.*` tokens. Adding a new path is one struct entry
// — the package init sorts by descending suffix length so a longer
// path (e.g. `workflow_def`) is always tried before its prefix
// (`workflow`). Three anti-pattern classes addressed:
//
//  1. Naive ordering traps: a future `.phase_history` addition can't
//     accidentally be shredded by an earlier `.phase` ReplaceAll —
//     the table-sort enforces longest-first.
//  2. Prefix-superset corruption: when a longer-token resolve fails
//     (token survives), subsequent shorter-token substitutions would
//     mangle the survivor into `$entity.lifecycle.<value>_def`-style
//     garbage. The placeholder-swap inside applyLifecycleSubstitutions
//     handles this generically for every rule.
//  3. Empty-string silent-pass: ok=false means the operator's
//     intended token survives verbatim → unresolved-template warning
//     fires correctly.
var lifecycleSubstitutionRules = []lifecycleSubstitutionRule{
	{
		suffix: "phase",
		resolve: func(_ *ExecutionContext, p lifecycleParticipantSnapshot) (string, bool) {
			return p.Phase(), true
		},
	},
	{
		suffix: "terminal",
		resolve: func(_ *ExecutionContext, p lifecycleParticipantSnapshot) (string, bool) {
			return fmt.Sprintf("%t", p.IsTerminal()), true
		},
	},
	{
		suffix: "workflow",
		resolve: func(_ *ExecutionContext, p lifecycleParticipantSnapshot) (string, bool) {
			return p.Workflow(), true
		},
	},
	{
		suffix: "workflow_def",
		resolve: func(ec *ExecutionContext, p lifecycleParticipantSnapshot) (string, bool) {
			return ec.resolveWorkflowDefJSON(p.Workflow())
		},
	},
}

func init() {
	// Sort by suffix length descending so longest match wins on
	// every ReplaceAll pass. Stable order doesn't matter here because
	// distinct suffixes can't collide on length.
	sort.SliceStable(lifecycleSubstitutionRules, func(i, j int) bool {
		return len(lifecycleSubstitutionRules[i].suffix) > len(lifecycleSubstitutionRules[j].suffix)
	})
}

// applyLifecycleSubstitutions resolves $entity.lifecycle.* tokens in
// template. Short-circuits when the template doesn't reference any of
// the supported paths.
func (ec *ExecutionContext) applyLifecycleSubstitutions(template string) string {
	if !strings.Contains(template, lifecycleSubstitutionPrefix) {
		return template
	}
	if ec.Lifecycle == nil || ec.EntityID == "" {
		// Manager not wired or no entity → surviving tokens trip
		// the unresolved-template warning; intentional loud-fail.
		return template
	}

	participant, err := ec.lookupLifecycleParticipant()
	if err != nil {
		// Negative lookup is cached on ec; no need to re-log per
		// substitution call. The unresolved-template warning will
		// surface the surviving tokens once per substitution call,
		// which is the operator-visible signal.
		return template
	}

	// Two-pass substitution to prevent the prefix-superset
	// corruption class: surviving longer-suffix tokens are protected
	// with NUL-sentinel placeholders during the shorter-suffix
	// substitution pass, then restored. Without this, a surviving
	// `$entity.lifecycle.workflow_def` would morph into
	// `$entity.lifecycle.<workflowName>_def` when the `.workflow`
	// rule fires, misleading operators hunting for a token they
	// never wrote.
	const placeholderPrefix = "\x00__LIFECYCLE_SURVIVED_"
	const placeholderSuffix = "__\x00"
	result := template
	survived := make(map[string]string) // suffix → placeholder used
	for _, rule := range lifecycleSubstitutionRules {
		token := lifecycleSubstitutionPrefix + rule.suffix
		if !strings.Contains(result, token) {
			continue
		}
		value, ok := rule.resolve(ec, participant)
		if ok {
			result = strings.ReplaceAll(result, token, value)
			continue
		}
		// Resolve failed → protect the surviving token with a
		// per-suffix placeholder so subsequent shorter-suffix rules
		// don't shred it. Restore at the end.
		placeholder := placeholderPrefix + rule.suffix + placeholderSuffix
		result = strings.ReplaceAll(result, token, placeholder)
		survived[rule.suffix] = placeholder
	}
	for suffix, placeholder := range survived {
		result = strings.ReplaceAll(result, placeholder, lifecycleSubstitutionPrefix+suffix)
	}
	return result
}

// lookupLifecycleParticipant memoizes Manager.LookupByEntityID on ec
// so multi-template rules don't pay the linear-scan cost more than
// once. Negative results are also cached.
func (ec *ExecutionContext) lookupLifecycleParticipant() (lifecycleParticipantSnapshot, error) {
	if ec.lifecycleParticipantCache != nil {
		c := ec.lifecycleParticipantCache
		if c.err != nil {
			return lifecycleParticipantSnapshot{}, c.err
		}
		return participantSnapshot(c.participant), nil
	}
	p, err := ec.Lifecycle.LookupByEntityID(context.Background(), ec.EntityID)
	ec.lifecycleParticipantCache = &lifecycleLookupResult{
		participant: p,
		err:         err,
	}
	if err != nil {
		return lifecycleParticipantSnapshot{}, err
	}
	return participantSnapshot(p), nil
}

// lifecycleParticipantSnapshot is the value returned from
// lookupLifecycleParticipant. Mirroring the underlying interface lets
// the substitution layer stay independent of pkg/lifecycle import
// gymnastics inside this file's helpers.
//
// IMPLICIT INVARIANT: workflow names registered with Manager.Register
// must not contain the substring `$entity.lifecycle.` — the
// sequential ReplaceAll pass in applyLifecycleSubstitutions could
// otherwise re-match a substituted workflow value against a later
// rule pass. Workflow names come from boot-time registrations, not
// from data, so this is a code-design invariant for callers, not a
// runtime check.
type lifecycleParticipantSnapshot struct {
	phase    string
	terminal bool
	workflow string
	entityID string
}

func (s lifecycleParticipantSnapshot) Phase() string    { return s.phase }
func (s lifecycleParticipantSnapshot) IsTerminal() bool { return s.terminal }
func (s lifecycleParticipantSnapshot) Workflow() string { return s.workflow }
func (s lifecycleParticipantSnapshot) EntityID() string { return s.entityID }

func participantSnapshot(p interface {
	Phase() string
	IsTerminal() bool
	Workflow() string
	EntityID() string
}) lifecycleParticipantSnapshot {
	return lifecycleParticipantSnapshot{
		phase:    p.Phase(),
		terminal: p.IsTerminal(),
		workflow: p.Workflow(),
		entityID: p.EntityID(),
	}
}

// resolveWorkflowDefJSON returns the JSON-encoded WorkflowDef for the
// named workflow paired with ok=true on success. ok=false on any
// lookup or marshal failure — caller leaves the surviving token
// verbatim so the unresolved-template warning downstream surfaces
// the problem (see [[feedback_no_clean_except_handwave]]: silent
// empty-string substitution would defeat the warning).
func (ec *ExecutionContext) resolveWorkflowDefJSON(workflow string) (string, bool) {
	if ec.Lifecycle == nil || workflow == "" {
		return "", false
	}
	def, err := ec.Lifecycle.GetWorkflowDefinition(workflow)
	if err != nil {
		slog.Default().Warn("lifecycle: $entity.lifecycle.workflow_def lookup failed",
			slog.String("workflow", workflow),
			slog.String("error", err.Error()))
		return "", false
	}
	// WorkflowDef carries a Transitions map; json.Marshal handles
	// it directly. Operators consuming this token in a tool-call
	// argument or rule-engine substitution string parse it back
	// themselves.
	out, err := json.Marshal(def)
	if err != nil {
		slog.Default().Warn("lifecycle: $entity.lifecycle.workflow_def JSON marshal failed",
			slog.String("workflow", workflow),
			slog.String("error", err.Error()))
		return "", false
	}
	return string(out), true
}
