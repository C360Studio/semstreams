// Package rule — lifecycle_* action executors (ADR-047).
//
// Three actions bridge the rule engine to pkg/lifecycle.Manager:
//
//   - lifecycle_transition: move a Participant to a declared phase,
//     optionally applying field set-ops in the same atomic write.
//   - lifecycle_complete: dispatch to Manager.Complete (deterministic
//     terminal-phase pick from current phase's out-edges).
//   - lifecycle_fail: dispatch to Manager.Fail (transition to the
//     declared "failed" terminal, carrying Reason for audit).
//
// The Manager is injected via SetLifecycleManager — nil-valued
// manager surfaces an authoring-grade error message rather than a
// silent no-op. Wiring is the rule processor's responsibility at
// component-init time (see processor.go); tests construct an
// in-memory Manager via newManagerForTest.
package rule

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/c360studio/semstreams/pkg/lifecycle"
)

// executeLifecycleTransition runs a lifecycle_transition action,
// resolving the Workflow from action.Workflow (explicit) or by
// scanning the Manager's registrations for the trigger entityID
// (implicit). Phase value is substitution-resolved. Optional Set
// map applies field mutations in the same Manager.Update closure
// as the phase write.
func (e *ActionExecutor) executeLifecycleTransition(ctx context.Context, action Action, ec *ExecutionContext) error {
	mgr, err := e.requireLifecycleManager(ActionTypeLifecycleTransition)
	if err != nil {
		return err
	}
	entityID := ec.EntityID
	if entityID == "" {
		return errors.New("lifecycle_transition: ec.EntityID is empty; rule must fire with an entity context")
	}
	phase, err := resolveLifecycleString(ctx, ec, action.Phase, "phase")
	if err != nil {
		return fmt.Errorf("lifecycle_transition: %w", err)
	}
	if phase == "" {
		return errors.New("lifecycle_transition: phase is required (resolved to empty after substitution)")
	}
	workflow, err := e.resolveLifecycleWorkflow(ctx, ec, mgr, action, entityID)
	if err != nil {
		return fmt.Errorf("lifecycle_transition: %w", err)
	}

	var mutator func(lifecycle.Participant) error
	if len(action.Set) > 0 {
		// Pre-validate every Set key against the workflow's
		// structMeta BEFORE building the mutator closure — same
		// validate-then-mutate pattern UpdateFromOperator uses
		// (manager_query.go). Default-deny via AssertRuleWritable
		// catches: typo'd field names, identity (`lifecycle:"id"`)
		// writes, phase (`lifecycle:"phase"`) writes, and any
		// field that isn't `lifecycle:"operator_writable"`. The
		// rule path enforces the SAME protection convention as the
		// operator API so a rule definition can't accidentally
		// exceed an operator's authority (ADR-047).
		for jsonName := range action.Set {
			if err := mgr.AssertRuleWritable(workflow, jsonName); err != nil {
				return fmt.Errorf("lifecycle_transition: workflow=%q: %w", workflow, err)
			}
		}
		// Substitute string values inside Set up front so the
		// mutator closure captures already-resolved values. Capture
		// a snapshot to avoid CAS-retry seeing concurrently-mutated
		// callsite maps.
		resolved := substituteSetMap(ctx, action.Set, ec)
		mutator = func(p lifecycle.Participant) error {
			return applyLifecycleSet(p, resolved)
		}
	}

	if err := mgr.TransitionWith(ctx, workflow, entityID, phase, lifecycle.TransitionSourceRule, action.Reason, mutator); err != nil {
		return fmt.Errorf("lifecycle_transition: workflow=%q entity_id=%q phase=%q: %w",
			workflow, entityID, phase, err)
	}
	if e.logger != nil {
		e.logger.Debug("lifecycle_transition executed",
			"workflow", workflow,
			"entity_id", entityID,
			"phase", phase,
			"rule_id", ec.RuleID(),
			"set_keys", len(action.Set))
	}
	return nil
}

// executeLifecycleComplete dispatches Manager.Complete. Resolves
// workflow the same way as Transition.
func (e *ActionExecutor) executeLifecycleComplete(ctx context.Context, action Action, ec *ExecutionContext) error {
	mgr, err := e.requireLifecycleManager(ActionTypeLifecycleComplete)
	if err != nil {
		return err
	}
	entityID := ec.EntityID
	if entityID == "" {
		return errors.New("lifecycle_complete: ec.EntityID is empty; rule must fire with an entity context")
	}
	workflow, err := e.resolveLifecycleWorkflow(ctx, ec, mgr, action, entityID)
	if err != nil {
		return fmt.Errorf("lifecycle_complete: %w", err)
	}
	if err := mgr.Complete(ctx, workflow, entityID); err != nil {
		return fmt.Errorf("lifecycle_complete: workflow=%q entity_id=%q: %w",
			workflow, entityID, err)
	}
	if e.logger != nil {
		e.logger.Debug("lifecycle_complete executed",
			"workflow", workflow,
			"entity_id", entityID,
			"rule_id", ec.RuleID())
	}
	return nil
}

// executeLifecycleFail dispatches Manager.Fail. Reason is required
// (Manager.Fail rejects empty); substitution is applied.
func (e *ActionExecutor) executeLifecycleFail(ctx context.Context, action Action, ec *ExecutionContext) error {
	mgr, err := e.requireLifecycleManager(ActionTypeLifecycleFail)
	if err != nil {
		return err
	}
	entityID := ec.EntityID
	if entityID == "" {
		return errors.New("lifecycle_fail: ec.EntityID is empty; rule must fire with an entity context")
	}
	reason, err := resolveLifecycleString(ctx, ec, action.Reason, "reason")
	if err != nil {
		return fmt.Errorf("lifecycle_fail: %w", err)
	}
	if reason == "" {
		return errors.New("lifecycle_fail: reason is required (resolved to empty after substitution) — Manager.Fail rejects empty reasons since operators need the failure cause in the audit trail")
	}
	workflow, err := e.resolveLifecycleWorkflow(ctx, ec, mgr, action, entityID)
	if err != nil {
		return fmt.Errorf("lifecycle_fail: %w", err)
	}
	if err := mgr.Fail(ctx, workflow, entityID, reason); err != nil {
		return fmt.Errorf("lifecycle_fail: workflow=%q entity_id=%q: %w",
			workflow, entityID, err)
	}
	if e.logger != nil {
		e.logger.Debug("lifecycle_fail executed",
			"workflow", workflow,
			"entity_id", entityID,
			"reason", reason,
			"rule_id", ec.RuleID())
	}
	return nil
}

// requireLifecycleManager returns the wired manager or an error.
// Nil-manager surfaces as an authoring/wiring error rather than a
// silent no-op — apps that intentionally don't use the harness
// should not be using lifecycle_* actions.
func (e *ActionExecutor) requireLifecycleManager(actionType string) (LifecycleManager, error) {
	if e.lifecycle == nil {
		return nil, fmt.Errorf("%s: no lifecycle.Manager wired on the rule processor (call SetLifecycleManager during component init, or remove the action from the rule)", actionType)
	}
	return e.lifecycle, nil
}

// resolveLifecycleWorkflow picks the workflow name to use. Explicit
// action.Workflow takes priority (substitution-applied with
// loud-fail on surviving framework tokens — same discipline as
// resolveTripleSubject in actions.go for $entity.triple.* references).
// Falls back to Manager.LookupByEntityID when omitted.
func (e *ActionExecutor) resolveLifecycleWorkflow(ctx context.Context, ec *ExecutionContext, mgr LifecycleManager, action Action, entityID string) (string, error) {
	if action.Workflow != "" {
		workflow, err := resolveLifecycleString(ctx, ec, action.Workflow, "workflow")
		if err != nil {
			return "", err
		}
		if workflow == "" {
			return "", fmt.Errorf("workflow %q resolved to empty after substitution", action.Workflow)
		}
		return workflow, nil
	}
	p, err := mgr.LookupByEntityID(ctx, entityID)
	if err != nil {
		return "", fmt.Errorf("resolve workflow for entity_id=%q: %w (set action.Workflow explicitly when ambiguous)", entityID, err)
	}
	return p.Workflow(), nil
}

// resolveLifecycleString runs SubstituteVariables against an action
// string field and asserts no framework-namespace tokens
// ($entity|related|state|schedule|caller|message.*) survive. A
// surviving token means the trigger didn't carry the data the author
// expected — a typo or a wire-context mismatch (e.g. `$message.foo`
// on an entity-state-driven rule with no MessageData). Mirrors
// resolveTripleSubject's loudness discipline (actions.go) so authors
// see "unresolved template" rather than the downstream error
// (Manager.lookupByWorkflow's "not registered", or a transition
// rejected against a literal template phase).
//
// `fieldName` is the human-readable label inserted into the error
// message (e.g. "phase", "reason", "workflow"). Empty result is NOT
// rejected here — callers handle empty-string semantics for their
// own field (Manager.Fail rejects empty reason; lifecycle_transition
// requires non-empty phase).
func resolveLifecycleString(ctx context.Context, ec *ExecutionContext, raw string, fieldName string) (string, error) {
	resolved := strings.TrimSpace(ec.SubstituteVariables(ctx, raw))
	if leftovers := unresolvedTemplateVarRe.FindAllString(resolved, -1); len(leftovers) > 0 {
		return "", fmt.Errorf("%s %q has unresolved template variables %v after substitution (likely a typo or a reference that the trigger didn't carry at fire time)",
			fieldName, raw, leftovers)
	}
	return resolved, nil
}

// substituteSetMap walks a Set-shape map (literal-or-typed-op values),
// applying string substitution to literal-string values and to
// typed-op `value` fields. Non-string values are passed through
// untouched.
func substituteSetMap(ctx context.Context, set map[string]any, ec *ExecutionContext) map[string]any {
	out := make(map[string]any, len(set))
	for k, raw := range set {
		out[k] = substituteSetValue(ctx, raw, ec)
	}
	return out
}

func substituteSetValue(ctx context.Context, raw any, ec *ExecutionContext) any {
	switch v := raw.(type) {
	case string:
		return ec.SubstituteVariables(ctx, v)
	case map[string]any:
		// Typed op — substitute inside `value` only; op stays literal.
		out := make(map[string]any, len(v))
		for k, inner := range v {
			if k == "value" {
				if s, ok := inner.(string); ok {
					out[k] = ec.SubstituteVariables(ctx, s)
					continue
				}
			}
			out[k] = inner
		}
		return out
	default:
		return raw
	}
}

// applyLifecycleSet executes the Set map against a Participant
// instance. Field names are matched against JSON tags on the
// Participant struct (the same surface UpdateFromOperator uses for
// operator patches).
//
// Each entry is one of:
//
//   - literal string/number/bool: assigned via reflect (with assignability check).
//   - {"op":"set", "value": v}: equivalent to a bare literal v.
//   - {"op":"increment"}: numeric field += 1.
//   - {"op":"decrement"}: numeric field -= 1.
//
// Unknown field names, unsupported op, or type mismatches abort the
// transition with a wrapped error — no partial writes land.
func applyLifecycleSet(p lifecycle.Participant, set map[string]any) error {
	rv := reflect.ValueOf(p)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return fmt.Errorf("lifecycle set: participant must be a pointer-to-struct, got %T", p)
	}

	jsonIdx := buildJSONFieldIndex(rv.Type())
	for jsonName, raw := range set {
		fieldIdx, ok := jsonIdx[jsonName]
		if !ok {
			return fmt.Errorf("lifecycle set: field %q is not declared on participant type %s (json-tag lookup)", jsonName, rv.Type())
		}
		target := rv.FieldByIndex(fieldIdx)
		if !target.CanSet() {
			return fmt.Errorf("lifecycle set: field %q is not settable", jsonName)
		}
		if err := applySetEntry(target, jsonName, raw); err != nil {
			return err
		}
	}
	return nil
}

func applySetEntry(target reflect.Value, jsonName string, raw any) error {
	op, isOp := parseSetOp(raw)
	if !isOp {
		// Bare literal — assign as-is.
		return assignLiteral(target, jsonName, raw)
	}
	switch op.kind {
	case "set":
		return assignLiteral(target, jsonName, op.value)
	case "increment":
		return addToNumeric(target, jsonName, 1)
	case "decrement":
		return addToNumeric(target, jsonName, -1)
	default:
		return fmt.Errorf("lifecycle set: field %q: unsupported op %q (supported: set, increment, decrement)", jsonName, op.kind)
	}
}

// setOp is the parsed shape of a {"op": "...", "value": ...} entry.
// `value` is meaningful only for op=="set".
type setOp struct {
	kind  string
	value any
}

// parseSetOp recognises map[string]any{"op": <string>, ...} as a
// typed op. Anything else returns isOp=false.
func parseSetOp(raw any) (setOp, bool) {
	m, ok := raw.(map[string]any)
	if !ok {
		return setOp{}, false
	}
	opRaw, ok := m["op"]
	if !ok {
		return setOp{}, false
	}
	opStr, ok := opRaw.(string)
	if !ok {
		return setOp{}, false
	}
	return setOp{kind: opStr, value: m["value"]}, true
}

func assignLiteral(target reflect.Value, jsonName string, raw any) error {
	if raw == nil {
		target.SetZero()
		return nil
	}
	src := reflect.ValueOf(raw)
	if src.Type().AssignableTo(target.Type()) {
		target.Set(src)
		return nil
	}
	if src.Type().ConvertibleTo(target.Type()) && numericKind(target) && numericKind(src) {
		target.Set(src.Convert(target.Type()))
		return nil
	}
	return fmt.Errorf("lifecycle set: field %q: cannot assign %v to %v", jsonName, src.Type(), target.Type())
}

func addToNumeric(target reflect.Value, jsonName string, delta int64) error {
	switch target.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		target.SetInt(target.Int() + delta)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		// Clamp at zero on decrement to avoid wrap.
		cur := target.Uint()
		if delta < 0 {
			d := uint64(-delta)
			if d > cur {
				target.SetUint(0)
				return nil
			}
			target.SetUint(cur - d)
			return nil
		}
		target.SetUint(cur + uint64(delta))
	case reflect.Float32, reflect.Float64:
		target.SetFloat(target.Float() + float64(delta))
	default:
		return fmt.Errorf("lifecycle set: field %q: increment/decrement requires a numeric field, got %v", jsonName, target.Kind())
	}
	return nil
}

func numericKind(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	}
	return false
}

// buildJSONFieldIndex maps json-tag names (or lower-cased field
// names for tag-less fields) to FieldByIndex slices. Mirrors the
// shape pkg/lifecycle/tags.go uses for operator patches, kept
// local here to avoid widening pkg/lifecycle's exported surface.
func buildJSONFieldIndex(t reflect.Type) map[string][]int {
	idx := make(map[string][]int)
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		name := jsonNameForField(field)
		if name == "-" {
			continue
		}
		idx[name] = field.Index
	}
	return idx
}

func jsonNameForField(f reflect.StructField) string {
	tag := f.Tag.Get("json")
	if tag == "" {
		return f.Name
	}
	if i := strings.IndexByte(tag, ','); i >= 0 {
		tag = tag[:i]
	}
	if tag == "" {
		return f.Name
	}
	return tag
}
