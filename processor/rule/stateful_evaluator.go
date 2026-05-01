// Package rule - Stateful rule evaluation with state tracking
package rule

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/processor/rule/expression"
)

// Compile-time interface check
var _ ActionExecutorInterface = (*ActionExecutor)(nil)

// StatefulEvaluator handles rule evaluation with state tracking.
// It manages state transitions (entered/exited/none) and executes
// the appropriate actions based on the transition type.
type StatefulEvaluator struct {
	stateTracker   *StateTracker
	actionExecutor ActionExecutorInterface
	exprEvaluator  *expression.Evaluator
	logger         *slog.Logger
}

// ActionExecutorInterface defines the interface for action execution.
// Actions receive an ExecutionContext with the full entity state and match state,
// replacing the previous (entityID, relatedID string) signature.
type ActionExecutorInterface interface {
	Execute(ctx context.Context, action Action, ec *ExecutionContext) error
}

// NewStatefulEvaluator creates a new stateful evaluator with the given dependencies.
// If logger is nil, uses the default logger.
func NewStatefulEvaluator(stateTracker *StateTracker, actionExecutor ActionExecutorInterface, logger *slog.Logger) *StatefulEvaluator {
	if logger == nil {
		logger = slog.Default()
	}
	return &StatefulEvaluator{
		stateTracker:   stateTracker,
		actionExecutor: actionExecutor,
		exprEvaluator:  expression.NewExpressionEvaluator(),
		logger:         logger,
	}
}

// Evaluation groups the inputs to a single stateful rule evaluation.
// Zero values are meaningful: Revision=0 means "no KV revision available"
// (message-path rules), Bootstrap=false means live traffic (not startup replay),
// Entity/Related=nil means message-path rule, Caller=nil means no caller in
// scope (cron fires, KV-watch fires).
type Evaluation struct {
	Rule              Definition
	EntityID          string
	RelatedID         string // "" for single-entity rules
	CurrentlyMatching bool
	Entity            *gtypes.EntityState // nil for message-path rules
	Related           *gtypes.EntityState // nil for single-entity or message-path
	Revision          uint64              // KV revision that triggered this evaluation; 0 if unknown
	Bootstrap         bool                // true during watcher initial-state replay
	// Caller carries the authenticated identity that triggered this evaluation.
	// Populated by the message-path (HTTP-originated NATS messages carry
	// X-Caller-* headers extracted by natsclient.Subscribe). Nil for KV-watch
	// and cron fires which have no caller in scope.
	Caller *CallerContext
}

// entityKey returns the state-tracker key (single entity or canonical pair).
func (ev Evaluation) entityKey() string {
	if ev.RelatedID == "" {
		return ev.EntityID
	}
	return buildPairKey(ev.EntityID, ev.RelatedID)
}

// Evaluate runs a stateful rule evaluation and fires the appropriate actions
// for the detected transition:
//
//   - TransitionEntered   → OnEnter (increments iteration)
//   - TransitionExited    → OnExit (preserves iteration)
//   - TransitionNone+match → WhileTrue
//
// Action-level When clauses are evaluated before each action runs. New state
// is persisted to the StateTracker on the way out.
//
// When Evaluation.Bootstrap is true, the watcher is replaying state after a
// restart. A persisted IsMatching=true state that still matches would
// otherwise produce TransitionNone — we promote it to a synthetic Entered
// (fires OnRecovery, or OnEnter under RerunOnRecovery) so state machines
// resume instead of hanging on restart.
func (e *StatefulEvaluator) Evaluate(ctx context.Context, ev Evaluation) (Transition, error) {
	entityKey := ev.entityKey()

	prevState, err := e.stateTracker.Get(ctx, ev.Rule.ID, entityKey)
	wasMatching := false
	hadPrevState := false
	if err != nil {
		if !errors.Is(err, ErrStateNotFound) {
			return TransitionNone, err
		}
	} else {
		wasMatching = prevState.IsMatching
		hadPrevState = true
	}

	// Durable stale-replay guard: if we've already recorded a transition at or
	// after this revision, the watcher is redelivering (NATS reconnect, consumer
	// resume) or delivering out of order. Skip without firing actions or
	// touching persisted state — the ephemeral ownRevisions map defends the
	// normal self-write path, this field defends across process restarts.
	// Revision==0 (message-path rules) and prev.SourceRevision==0 (old
	// persisted state with no recorded revision) both bypass the check.
	if ev.Revision > 0 && hadPrevState && prevState.SourceRevision >= ev.Revision {
		e.logger.Debug("Skipping stale revision replay",
			"rule_id", ev.Rule.ID,
			"entity_key", entityKey,
			"revision", ev.Revision,
			"prev_source_revision", prevState.SourceRevision)
		return TransitionNone, nil
	}

	// Re-evaluate conditions when the rule has transition operators. The initial
	// match evaluation runs without $prev.* state fields, so transition-operator
	// conditions always return false there; we re-check here with the previous
	// field values available.
	currentlyMatching := e.reEvaluateTransitions(ev.Rule, ev.EntityID, ev.Entity, prevState, ev.CurrentlyMatching)

	// Re-evaluate conditions that reference $caller.* pseudo-fields. The message-path
	// Rule.Evaluate(messages) call in the processor cannot see caller context (the Rule
	// interface carries no identity), so CurrentlyMatching is always false for pure
	// $caller.* conditions. We correct that here when the rule has caller conditions.
	//
	// SECURITY: the gate deliberately omits the "ev.Caller != nil" half. The previous
	// dual-gate (Caller!=nil && hasCallerConditions) allowed a body-spoof bypass:
	// with Caller==nil the gate was skipped, leaving a body-resolved currentlyMatching
	// value (from ExpressionRule.evaluateConditions) flowing through to action dispatch.
	// An attacker publishing {"$caller": {"role": "admin"}} with no X-Caller-* headers
	// could satisfy a positive-guard $caller.role==admin condition that way.
	//
	// Dropping the Caller!=nil half means re-eval always runs for caller-conditioned
	// rules. With Caller==nil, reEvaluateWithCallerFields injects no $caller.* keys
	// into stateFields, and the absent-field semantic in the expression evaluator
	// returns false for ALL operators — the correct "no identity" outcome for
	// cron and KV-watch fires.
	if hasCallerConditions(ev.Rule) {
		currentlyMatching = e.reEvaluateWithCallerFields(ev.Rule, ev.EntityID, ev.Entity, prevState, ev.Caller, currentlyMatching)
	}

	transition := DetectTransition(wasMatching, currentlyMatching)

	// Bootstrap recovery: a persisted matching state that still matches would
	// produce TransitionNone. For rules that opt in, promote to a synthetic
	// Entered so OnRecovery (or OnEnter under RerunOnRecovery) re-fires and the
	// state machine resumes instead of hanging.
	recovering := false
	if ev.Bootstrap && hadPrevState && transition == TransitionNone && currentlyMatching &&
		shouldFireOnRecovery(ev.Rule) {
		transition = TransitionEntered
		recovering = true
	}

	iteration := prevState.Iteration
	if transition == TransitionEntered {
		iteration++
	}

	matchState := &MatchState{
		RuleID:         ev.Rule.ID,
		EntityKey:      entityKey,
		IsMatching:     currentlyMatching,
		LastTransition: string(transition),
		TransitionAt:   time.Now(),
		LastChecked:    time.Now(),
		Iteration:      iteration,
		MaxIterations:  ev.Rule.MaxIterations,
		SourceRevision: ev.Revision,
	}

	ec := &ExecutionContext{
		EntityID:  ev.EntityID,
		RelatedID: ev.RelatedID,
		Entity:    ev.Entity,
		Related:   ev.Related,
		State:     matchState,
		Caller:    ev.Caller,
	}

	stateFields := expression.StateFields{
		"$state.iteration":       iteration,
		"$state.max_iterations":  ev.Rule.MaxIterations,
		"$state.last_transition": string(transition),
	}
	for field, prevValue := range prevState.FieldValues {
		stateFields["$prev."+field] = prevValue
	}
	// Populate $caller.* pseudo-fields from the caller identity when present.
	// Absent (nil) caller leaves stateFields without $caller.* keys, which
	// causes evaluateConditionWithState to return false for any $caller.*
	// condition — the correct "no identity" semantic for cron and KV-watch fires.
	if ev.Caller != nil {
		stateFields["$caller.id"] = ev.Caller.ID
		stateFields["$caller.role"] = ev.Caller.Role
		stateFields["$caller.org"] = ev.Caller.Org
	}

	actions := e.selectActions(ev.Rule, transition, currentlyMatching, recovering, ev.EntityID, ev.RelatedID, iteration)
	e.runActions(ctx, ev.Rule, ec, actions, ev.Entity, stateFields, ev.EntityID, ev.RelatedID)

	matchState.FieldValues = captureTransitionFields(ev.Rule, ev.Entity)

	if err := e.stateTracker.Set(ctx, *matchState); err != nil {
		e.logger.Warn("Failed to persist rule state",
			"rule_id", ev.Rule.ID,
			"entity_key", entityKey,
			"error", err)
		return transition, err
	}

	return transition, nil
}

// reEvaluateTransitions re-evaluates conditions when the rule has transition operators.
// EvaluateEntityState runs without $prev.* state fields, so transition conditions
// always return false there. This method re-evaluates the full condition set with
// previous field values from the state tracker.
func (e *StatefulEvaluator) reEvaluateTransitions(
	ruleDef Definition,
	entityID string,
	entity *gtypes.EntityState,
	prevState MatchState,
	currentlyMatching bool,
) bool {
	if !hasTransitionConditions(ruleDef) || entity == nil {
		return currentlyMatching
	}

	prevFields := expression.StateFields{}
	for field, prevValue := range prevState.FieldValues {
		prevFields["$prev."+field] = prevValue
	}

	expr := expression.LogicalExpression{
		Conditions: ruleDef.Conditions,
		Logic:      ruleDef.Logic,
	}
	if expr.Logic == "" {
		expr.Logic = expression.LogicAnd
	}
	match, err := e.exprEvaluator.EvaluateWithStateFields(entity, prevFields, expr)
	if err != nil {
		e.logger.Warn("Failed to re-evaluate transition conditions",
			"rule_id", ruleDef.ID,
			"entity_id", entityID,
			"error", err)
		return currentlyMatching
	}
	return match
}

// selectActions picks the action list to run for a transition and logs the
// decision. It also handles the bootstrap-recovery fork: when recovering is
// true, OnRecovery wins if defined, otherwise OnEnter runs under a recovery
// log message so operators can tell restart replays apart from live entries.
func (e *StatefulEvaluator) selectActions(
	ruleDef Definition,
	transition Transition,
	currentlyMatching bool,
	recovering bool,
	entityID, relatedID string,
	iteration int,
) []Action {
	var actions []Action
	switch transition {
	case TransitionEntered:
		msg := "Rule entered"
		if recovering && len(ruleDef.OnRecovery) > 0 {
			actions = ruleDef.OnRecovery
			msg = "Rule recovered (on_recovery)"
		} else {
			actions = ruleDef.OnEnter
			if recovering {
				msg = "Rule recovered (on_enter)"
			}
		}
		e.logger.Debug(msg,
			"rule_id", ruleDef.ID,
			"entity_id", entityID,
			"related_id", relatedID,
			"iteration", iteration,
			"action_count", len(actions))
	case TransitionExited:
		actions = ruleDef.OnExit
		e.logger.Debug("Rule exited",
			"rule_id", ruleDef.ID,
			"entity_id", entityID,
			"related_id", relatedID,
			"action_count", len(actions))
	case TransitionNone:
		if currentlyMatching {
			actions = ruleDef.WhileTrue
			e.logger.Debug("Rule while true",
				"rule_id", ruleDef.ID,
				"entity_id", entityID,
				"related_id", relatedID,
				"action_count", len(actions))
		}
	}
	return actions
}

// runActions executes each action in the list, skipping those whose When
// clause does not match. Action errors are logged and do not stop subsequent
// actions — the rule engine prefers best-effort execution so one failing
// side effect does not block the rest of a transition.
func (e *StatefulEvaluator) runActions(
	ctx context.Context,
	ruleDef Definition,
	ec *ExecutionContext,
	actions []Action,
	entity *gtypes.EntityState,
	stateFields expression.StateFields,
	entityID, relatedID string,
) {
	for _, action := range actions {
		if len(action.When) > 0 {
			match, whenErr := e.evaluateWhen(action.When, entity, stateFields)
			if whenErr != nil {
				e.logger.Warn("When clause evaluation failed, skipping action",
					"rule_id", ruleDef.ID,
					"entity_id", entityID,
					"action_type", action.Type,
					"error", whenErr)
				continue
			}
			if !match {
				e.logger.Debug("Action skipped by When clause",
					"rule_id", ruleDef.ID,
					"entity_id", entityID,
					"action_type", action.Type)
				continue
			}
		}

		if err := e.actionExecutor.Execute(ctx, action, ec); err != nil {
			if errors.Is(err, ErrDenyVerdict) {
				// Deny is terminal: subsequent actions in this evaluation cycle do not
				// run. Non-deny errors continue best-effort dispatch (see else branch).
				e.logger.Info("rule action denied; short-circuiting remaining actions",
					"rule_id", ruleDef.ID,
					"entity_id", entityID,
					"action_type", action.Type,
					"error", err)
				break
			}
			e.logger.Error("Failed to execute action",
				"rule_id", ruleDef.ID,
				"entity_id", entityID,
				"related_id", relatedID,
				"action_type", action.Type,
				"error", err)
		}
	}
}

// shouldFireOnRecovery reports whether a rule should re-fire its entry actions
// during bootstrap replay when the rule was previously matching.
//
// A rule opts in by either declaring OnRecovery actions or setting
// RerunOnRecovery=true. Rules without either are assumed to have side effects
// that must not re-run on restart (e.g. publishing an alert).
func shouldFireOnRecovery(ruleDef Definition) bool {
	if len(ruleDef.OnRecovery) > 0 {
		return true
	}
	return ruleDef.RerunOnRecovery && len(ruleDef.OnEnter) > 0
}

// hasTransitionConditions returns true if any condition in the rule uses the transition operator.
func hasTransitionConditions(ruleDef Definition) bool {
	for _, cond := range ruleDef.Conditions {
		if cond.Operator == expression.OpTransition {
			return true
		}
	}
	return false
}

// hasCallerConditions returns true if any top-level condition in the rule references
// a $caller.* pseudo-field. Used to gate the re-evaluation pass in Evaluate so that
// rules without caller conditions skip the extra expression.EvaluateWithStateFields call.
func hasCallerConditions(ruleDef Definition) bool {
	for _, cond := range ruleDef.Conditions {
		if strings.HasPrefix(cond.Field, "$caller.") {
			return true
		}
	}
	return false
}

// reEvaluateWithCallerFields re-evaluates all rule conditions with $caller.* and $prev.*
// fields populated. This corrects the match result for rules whose top-level conditions
// reference $caller.* pseudo-fields: the upstream Rule.Evaluate(messages) call cannot
// inject caller context (the Rule interface is identity-agnostic), so those conditions
// always return false from the initial evaluation. We fix the result here, inside the
// stateful evaluator that owns the caller-aware Evaluation struct.
func (e *StatefulEvaluator) reEvaluateWithCallerFields(
	ruleDef Definition,
	entityID string,
	entity *gtypes.EntityState,
	prevState MatchState,
	caller *CallerContext,
	currentlyMatching bool,
) bool {
	// Populate $caller.* keys only when a caller is present. A nil caller
	// (cron, KV-watch, any anonymous fire) leaves these keys absent from
	// callerFields, which causes the expression evaluator's absent-field
	// branch to return false for every $caller.* condition — the correct
	// "no identity" semantic regardless of which operator is used (eq, ne, in, ...).
	//
	// Do NOT populate empty-string values for nil caller: that would change
	// the semantics for "ne" guards ($caller.role != "admin" with nil caller
	// would match "" != "admin" = true, incorrectly admitting anonymous fires).
	callerFields := expression.StateFields{}
	if caller != nil {
		callerFields["$caller.id"] = caller.ID
		callerFields["$caller.role"] = caller.Role
		callerFields["$caller.org"] = caller.Org
	}
	for field, prevValue := range prevState.FieldValues {
		callerFields["$prev."+field] = prevValue
	}

	expr := expression.LogicalExpression{
		Conditions: ruleDef.Conditions,
		Logic:      ruleDef.Logic,
	}
	if expr.Logic == "" {
		expr.Logic = expression.LogicAnd
	}

	match, err := e.exprEvaluator.EvaluateWithStateFields(entity, callerFields, expr)
	if err != nil {
		e.logger.Warn("Failed to re-evaluate caller conditions",
			"rule_id", ruleDef.ID,
			"entity_id", entityID,
			"error", err)
		return currentlyMatching
	}
	return match
}

// captureTransitionFields scans a rule definition for transition conditions and
// snapshots the current entity triple values for those fields. This snapshot is
// stored in MatchState.FieldValues so the next evaluation can detect transitions.
func captureTransitionFields(ruleDef Definition, entity *gtypes.EntityState) map[string]string {
	if entity == nil {
		return nil
	}

	// Collect field names used in transition conditions
	fields := make(map[string]struct{})
	for _, cond := range ruleDef.Conditions {
		if cond.Operator == expression.OpTransition {
			fields[cond.Field] = struct{}{}
		}
	}
	// Also check When clauses inside actions
	for _, actions := range [][]Action{ruleDef.OnEnter, ruleDef.OnExit, ruleDef.WhileTrue} {
		for _, action := range actions {
			for _, cond := range action.When {
				if cond.Operator == expression.OpTransition {
					fields[cond.Field] = struct{}{}
				}
			}
		}
	}

	if len(fields) == 0 {
		return nil
	}

	// Snapshot current values for tracked fields
	values := make(map[string]string, len(fields))
	for _, triple := range entity.Triples {
		if _, tracked := fields[triple.Predicate]; tracked {
			values[triple.Predicate] = fmt.Sprintf("%v", triple.Object)
		}
	}
	return values
}

// evaluateWhen evaluates a When clause (action-level guard conditions).
// When clauses use AND logic by default — all conditions must match for the action to execute.
// If entity is nil (message-path rules), the When clause is skipped and returns true.
func (e *StatefulEvaluator) evaluateWhen(
	conditions []expression.ConditionExpression,
	entity *gtypes.EntityState,
	stateFields expression.StateFields,
) (bool, error) {
	// For message-path rules without entity state, skip When evaluation
	// unless all conditions reference $state.* fields
	expr := expression.LogicalExpression{
		Conditions: conditions,
		Logic:      expression.LogicAnd,
	}
	return e.exprEvaluator.EvaluateWithStateFields(entity, stateFields, expr)
}
