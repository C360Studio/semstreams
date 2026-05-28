// Package rule — tests for the lifecycle_* action family (ADR-047).
//
// Coverage:
//   - lifecycle_transition: phase + atomic Set ops + Workflow
//     resolution (explicit + LookupByEntityID fallback) + nil-manager
//     loud-fail.
//   - lifecycle_complete: dispatches Manager.Complete.
//   - lifecycle_fail: dispatches Manager.Fail with non-empty Reason
//     (substitution-resolved); empty Reason rejected.
//   - applyLifecycleSet: literal / typed-set / increment / decrement /
//     unknown-field / type-mismatch / decrement-floor-on-uint.
//   - Action.Workflow / Action.Phase / Action.Set / Action.Reason
//     plumb through SubstituteVariables.
//
// Mock Manager keeps the test surface stable across pkg/lifecycle
// internal-storage churn — interface-defined surface, in-memory
// state, deterministic terminal-pick.
package rule

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/pkg/lifecycle"
)

// --- Mock Manager ---

// fakeParticipant is the test fixture's Participant implementation.
// Mirrors the ADR-047 worked example so test scenarios double as
// readable specs for downstream consumers.
type fakeParticipant struct {
	EntityIDF  string  `json:"entity_id"`
	WorkflowF  string  `json:"workflow"`
	PhaseF     string  `json:"phase"`
	TerminalF  bool    `json:"-"`
	OwnerField string  `json:"owner"`
	Counter    int     `json:"counter"`
	UCounter   uint32  `json:"u_counter"`
	Pct        float64 `json:"pct"`
}

func (p *fakeParticipant) EntityID() string             { return p.EntityIDF }
func (p *fakeParticipant) Workflow() string             { return p.WorkflowF }
func (p *fakeParticipant) Phase() string                { return p.PhaseF }
func (p *fakeParticipant) IsTerminal() bool             { return p.TerminalF }
func (p *fakeParticipant) KVBucket() string             { return "FAKE" }
func (p *fakeParticipant) KVKey(entityID string) string { return entityID }
func (p *fakeParticipant) ParentEntityID() string       { return "" }

// fakeLifecycleManager satisfies LifecycleManager. State lives in
// `entities` keyed by (workflow, entityID). Each test instance starts
// fresh; no cross-test pollution.
//
// ruleWritableFields per-workflow defines which JSON field names the
// fake reports as rule-writable from AssertRuleWritable. Mirrors the
// production gate (B1 — see manager_query.go AssertRuleWritable) so
// tests can lock both happy-path writes AND default-deny rejections
// against the same shape the production code exercises. Default per
// fixture below allow-lists owner / counter / u_counter / pct (the
// fields tests intentionally mutate); identity (entity_id), workflow,
// and phase deliberately stay out so the protection-bypass regression
// can't slip back in unnoticed.
type fakeLifecycleManager struct {
	entities           map[string]map[string]*fakeParticipant // workflow → entityID → state
	workflowDefs       map[string]lifecycle.WorkflowDef
	ruleWritableFields map[string]map[string]bool // workflow → fieldJSONName → allowed
	transitionCalls    []transitionCall
	completeCalls      []ctxKey
	failCalls          []failCall
}

type ctxKey struct{ workflow, entityID string }
type transitionCall struct {
	workflow, entityID, phase string
	source                    lifecycle.TransitionSource
	note                      string
	hadMutator                bool
}
type failCall struct {
	workflow, entityID, reason string
}

func newFakeManager() *fakeLifecycleManager {
	return &fakeLifecycleManager{
		entities:           make(map[string]map[string]*fakeParticipant),
		workflowDefs:       make(map[string]lifecycle.WorkflowDef),
		ruleWritableFields: make(map[string]map[string]bool),
	}
}

func (m *fakeLifecycleManager) seed(workflow string, p *fakeParticipant) {
	if m.entities[workflow] == nil {
		m.entities[workflow] = make(map[string]*fakeParticipant)
	}
	if m.ruleWritableFields[workflow] == nil {
		// Default-allow the fields tests intentionally mutate.
		// Identity / workflow / phase deliberately stay protected so
		// the AssertRuleWritable gate is exercised the same way it
		// fires in production.
		m.ruleWritableFields[workflow] = map[string]bool{
			"owner":     true,
			"counter":   true,
			"u_counter": true,
			"pct":       true,
		}
	}
	p.WorkflowF = workflow
	m.entities[workflow][p.EntityIDF] = p
}

func (m *fakeLifecycleManager) TransitionWith(_ context.Context, workflow, entityID, newPhase string, source lifecycle.TransitionSource, note string, mutator func(lifecycle.Participant) error) error {
	m.transitionCalls = append(m.transitionCalls, transitionCall{
		workflow: workflow, entityID: entityID, phase: newPhase,
		source: source, note: note, hadMutator: mutator != nil,
	})
	p, ok := m.entities[workflow][entityID]
	if !ok {
		return fmt.Errorf("not found: %s/%s", workflow, entityID)
	}
	if mutator != nil {
		if err := mutator(p); err != nil {
			return err
		}
	}
	p.PhaseF = newPhase
	return nil
}

func (m *fakeLifecycleManager) Complete(_ context.Context, workflow, entityID string) error {
	m.completeCalls = append(m.completeCalls, ctxKey{workflow, entityID})
	p, ok := m.entities[workflow][entityID]
	if !ok {
		return fmt.Errorf("not found: %s/%s", workflow, entityID)
	}
	p.PhaseF = "completed"
	p.TerminalF = true
	return nil
}

func (m *fakeLifecycleManager) Fail(_ context.Context, workflow, entityID, reason string) error {
	if reason == "" {
		return errors.New("fail: empty reason")
	}
	m.failCalls = append(m.failCalls, failCall{workflow, entityID, reason})
	p, ok := m.entities[workflow][entityID]
	if !ok {
		return fmt.Errorf("not found: %s/%s", workflow, entityID)
	}
	p.PhaseF = "failed"
	p.TerminalF = true
	return nil
}

func (m *fakeLifecycleManager) LookupByEntityID(_ context.Context, entityID string) (lifecycle.Participant, error) {
	for _, byID := range m.entities {
		if p, ok := byID[entityID]; ok {
			return p, nil
		}
	}
	return nil, fmt.Errorf("not found: %s", entityID)
}

func (m *fakeLifecycleManager) GetWorkflowDefinition(workflow string) (lifecycle.WorkflowDef, error) {
	def, ok := m.workflowDefs[workflow]
	if !ok {
		return lifecycle.WorkflowDef{}, fmt.Errorf("workflow not registered: %s", workflow)
	}
	return def, nil
}

func (m *fakeLifecycleManager) AssertRuleWritable(workflow, fieldJSONName string) error {
	if _, registered := m.entities[workflow]; !registered {
		return fmt.Errorf("%w: workflow=%q not registered",
			lifecycle.ErrWorkflowNotRegistered, workflow)
	}
	allowed, ok := m.ruleWritableFields[workflow]
	if !ok || !allowed[fieldJSONName] {
		return fmt.Errorf("%w: workflow=%q field=%q",
			lifecycle.ErrFieldNotOperatorWritable, workflow, fieldJSONName)
	}
	return nil
}

// --- executeLifecycleTransition ---

func TestLifecycleTransition_HappyPath(t *testing.T) {
	t.Parallel()
	mgr := newFakeManager()
	mgr.seed("mission", &fakeParticipant{EntityIDF: "m-1", PhaseF: "planning"})

	exec := NewActionExecutor(nil)
	exec.SetLifecycleManager(mgr)

	action := Action{
		Type:     ActionTypeLifecycleTransition,
		Workflow: "mission",
		Phase:    "flying",
	}
	err := exec.Execute(context.Background(), action, &ExecutionContext{EntityID: "m-1"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if mgr.entities["mission"]["m-1"].PhaseF != "flying" {
		t.Errorf("phase did not move, got %q", mgr.entities["mission"]["m-1"].PhaseF)
	}
	if len(mgr.transitionCalls) != 1 {
		t.Fatalf("expected one TransitionWith call, got %d", len(mgr.transitionCalls))
	}
	if mgr.transitionCalls[0].source != lifecycle.TransitionSourceRule {
		t.Errorf("expected rule-source transition, got %q", mgr.transitionCalls[0].source)
	}
}

func TestLifecycleTransition_AtomicSetOps(t *testing.T) {
	t.Parallel()
	mgr := newFakeManager()
	mgr.seed("mission", &fakeParticipant{EntityIDF: "m-2", PhaseF: "planning", Counter: 5, UCounter: 3})

	exec := NewActionExecutor(nil)
	exec.SetLifecycleManager(mgr)

	action := Action{
		Type:     ActionTypeLifecycleTransition,
		Workflow: "mission",
		Phase:    "flying",
		Set: map[string]any{
			"owner":     "alice",
			"counter":   map[string]any{"op": "increment"},
			"u_counter": map[string]any{"op": "decrement"},
			"pct":       map[string]any{"op": "set", "value": 0.75},
		},
	}
	err := exec.Execute(context.Background(), action, &ExecutionContext{EntityID: "m-2"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	p := mgr.entities["mission"]["m-2"]
	if p.PhaseF != "flying" {
		t.Errorf("phase: got %q", p.PhaseF)
	}
	if p.OwnerField != "alice" {
		t.Errorf("owner literal: got %q", p.OwnerField)
	}
	if p.Counter != 6 {
		t.Errorf("counter increment: got %d, want 6", p.Counter)
	}
	if p.UCounter != 2 {
		t.Errorf("u_counter decrement: got %d, want 2", p.UCounter)
	}
	if p.Pct != 0.75 {
		t.Errorf("pct typed set: got %v, want 0.75", p.Pct)
	}
	if !mgr.transitionCalls[0].hadMutator {
		t.Errorf("expected TransitionWith mutator non-nil for Set ops")
	}
}

func TestLifecycleTransition_DecrementUintClampsAtZero(t *testing.T) {
	t.Parallel()
	mgr := newFakeManager()
	mgr.seed("mission", &fakeParticipant{EntityIDF: "m-3", PhaseF: "planning", UCounter: 0})

	exec := NewActionExecutor(nil)
	exec.SetLifecycleManager(mgr)

	action := Action{
		Type:     ActionTypeLifecycleTransition,
		Workflow: "mission",
		Phase:    "flying",
		Set:      map[string]any{"u_counter": map[string]any{"op": "decrement"}},
	}
	err := exec.Execute(context.Background(), action, &ExecutionContext{EntityID: "m-3"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if mgr.entities["mission"]["m-3"].UCounter != 0 {
		t.Errorf("uint decrement should clamp at 0, got %d", mgr.entities["mission"]["m-3"].UCounter)
	}
}

// --- B1 protection: lifecycle_transition `set` honors operator_writable + id + phase ---

func TestLifecycleTransition_SetRejectsIdentityField(t *testing.T) {
	t.Parallel()
	// Regression guard: rule action MUST NOT be able to rewrite the
	// identity field (lifecycle:"id"). A typo or attacker-influenced
	// rule definition shouldn't be more privileged than the operator
	// API. Mirrors UpdateFromOperator's default-deny.
	mgr := newFakeManager()
	mgr.seed("mission", &fakeParticipant{EntityIDF: "b1-id", PhaseF: "planning"})

	exec := NewActionExecutor(nil)
	exec.SetLifecycleManager(mgr)

	action := Action{
		Type:     ActionTypeLifecycleTransition,
		Workflow: "mission",
		Phase:    "flying",
		Set:      map[string]any{"entity_id": "hijacked-id"},
	}
	err := exec.Execute(context.Background(), action, &ExecutionContext{EntityID: "b1-id"})
	if err == nil {
		t.Fatalf("expected rejection for entity_id write via Set")
	}
	if !errors.Is(err, lifecycle.ErrFieldNotOperatorWritable) {
		t.Errorf("expected ErrFieldNotOperatorWritable, got %v", err)
	}
	// Phase MUST NOT have moved either (validation precedes mutator).
	if mgr.entities["mission"]["b1-id"].PhaseF != "planning" {
		t.Errorf("phase moved despite Set rejection, got %q", mgr.entities["mission"]["b1-id"].PhaseF)
	}
}

func TestLifecycleTransition_SetRejectsPhaseField(t *testing.T) {
	t.Parallel()
	// Phase is managed exclusively via lifecycle_transition's `phase`
	// argument under transitions-table validation. Direct Set on phase
	// would bypass the table — must be rejected at the validate-then-
	// mutate gate.
	mgr := newFakeManager()
	mgr.seed("mission", &fakeParticipant{EntityIDF: "b1-phase", PhaseF: "planning"})

	exec := NewActionExecutor(nil)
	exec.SetLifecycleManager(mgr)

	action := Action{
		Type:     ActionTypeLifecycleTransition,
		Workflow: "mission",
		Phase:    "flying",
		Set:      map[string]any{"phase": "completed"},
	}
	err := exec.Execute(context.Background(), action, &ExecutionContext{EntityID: "b1-phase"})
	if err == nil {
		t.Fatalf("expected rejection for phase write via Set")
	}
	if !errors.Is(err, lifecycle.ErrFieldNotOperatorWritable) {
		t.Errorf("expected ErrFieldNotOperatorWritable, got %v", err)
	}
}

func TestLifecycleTransition_SetRejectsNonOperatorWritableField(t *testing.T) {
	t.Parallel()
	// Default-deny: a field that exists on the participant struct but
	// isn't tagged lifecycle:"operator_writable" cannot be written via
	// Set. Convergence with UpdateFromOperator's protection.
	mgr := newFakeManager()
	mgr.seed("mission", &fakeParticipant{EntityIDF: "b1-secret", PhaseF: "planning"})
	// Explicitly do NOT add "secret" to ruleWritableFields — it's a
	// non-operator-writable field that the rule path must reject.

	exec := NewActionExecutor(nil)
	exec.SetLifecycleManager(mgr)

	action := Action{
		Type:     ActionTypeLifecycleTransition,
		Workflow: "mission",
		Phase:    "flying",
		Set:      map[string]any{"secret": "leaked"},
	}
	err := exec.Execute(context.Background(), action, &ExecutionContext{EntityID: "b1-secret"})
	if err == nil {
		t.Fatalf("expected rejection for non-operator_writable field write")
	}
	if !errors.Is(err, lifecycle.ErrFieldNotOperatorWritable) {
		t.Errorf("expected ErrFieldNotOperatorWritable, got %v", err)
	}
	// No transition call should have been issued — gate runs BEFORE
	// mutator construction.
	if len(mgr.transitionCalls) != 0 {
		t.Errorf("Manager should not see a TransitionWith call when Set is rejected, got %v", mgr.transitionCalls)
	}
}

func TestLifecycleTransition_UnknownFieldAborts(t *testing.T) {
	t.Parallel()
	mgr := newFakeManager()
	mgr.seed("mission", &fakeParticipant{EntityIDF: "m-4", PhaseF: "planning"})

	exec := NewActionExecutor(nil)
	exec.SetLifecycleManager(mgr)

	action := Action{
		Type:     ActionTypeLifecycleTransition,
		Workflow: "mission",
		Phase:    "flying",
		Set:      map[string]any{"not_a_field": "value"},
	}
	err := exec.Execute(context.Background(), action, &ExecutionContext{EntityID: "m-4"})
	if err == nil {
		t.Fatalf("expected error for unknown field")
	}
	if mgr.entities["mission"]["m-4"].PhaseF != "planning" {
		t.Errorf("phase moved despite mutator error, got %q", mgr.entities["mission"]["m-4"].PhaseF)
	}
}

func TestLifecycleTransition_ResolvesWorkflowViaLookup(t *testing.T) {
	t.Parallel()
	mgr := newFakeManager()
	mgr.seed("mission", &fakeParticipant{EntityIDF: "m-5", PhaseF: "planning"})

	exec := NewActionExecutor(nil)
	exec.SetLifecycleManager(mgr)

	// No Workflow on the action → LookupByEntityID fallback.
	action := Action{
		Type:  ActionTypeLifecycleTransition,
		Phase: "flying",
	}
	err := exec.Execute(context.Background(), action, &ExecutionContext{EntityID: "m-5"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if mgr.transitionCalls[0].workflow != "mission" {
		t.Errorf("expected resolved workflow=mission, got %q", mgr.transitionCalls[0].workflow)
	}
}

func TestLifecycleTransition_NilManagerLoudFail(t *testing.T) {
	t.Parallel()
	exec := NewActionExecutor(nil)
	// No SetLifecycleManager — lifecycle is nil.
	action := Action{
		Type:  ActionTypeLifecycleTransition,
		Phase: "flying",
	}
	err := exec.Execute(context.Background(), action, &ExecutionContext{EntityID: "m-x"})
	if err == nil {
		t.Fatalf("expected wiring error when no Manager is set")
	}
}

func TestLifecycleTransition_EmptyEntityIDRejected(t *testing.T) {
	t.Parallel()
	mgr := newFakeManager()
	exec := NewActionExecutor(nil)
	exec.SetLifecycleManager(mgr)
	action := Action{
		Type:  ActionTypeLifecycleTransition,
		Phase: "flying",
	}
	err := exec.Execute(context.Background(), action, &ExecutionContext{EntityID: ""})
	if err == nil {
		t.Fatalf("expected rejection for empty EntityID")
	}
}

func TestLifecycleTransition_PhaseSubstitution(t *testing.T) {
	t.Parallel()
	mgr := newFakeManager()
	mgr.seed("mission", &fakeParticipant{EntityIDF: "m-6", PhaseF: "planning"})

	exec := NewActionExecutor(nil)
	exec.SetLifecycleManager(mgr)

	action := Action{
		Type:     ActionTypeLifecycleTransition,
		Workflow: "mission",
		Phase:    "$message.next_phase",
	}
	ec := &ExecutionContext{
		EntityID:    "m-6",
		MessageData: map[string]any{"next_phase": "flying"},
	}
	err := exec.Execute(context.Background(), action, ec)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if mgr.entities["mission"]["m-6"].PhaseF != "flying" {
		t.Errorf("phase substitution failed, got %q", mgr.entities["mission"]["m-6"].PhaseF)
	}
}

// --- executeLifecycleComplete ---

func TestLifecycleComplete_DispatchesManager(t *testing.T) {
	t.Parallel()
	mgr := newFakeManager()
	mgr.seed("mission", &fakeParticipant{EntityIDF: "c-1", PhaseF: "landing"})

	exec := NewActionExecutor(nil)
	exec.SetLifecycleManager(mgr)

	action := Action{Type: ActionTypeLifecycleComplete, Workflow: "mission"}
	err := exec.Execute(context.Background(), action, &ExecutionContext{EntityID: "c-1"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(mgr.completeCalls) != 1 || mgr.completeCalls[0].entityID != "c-1" {
		t.Errorf("Complete not dispatched, got %v", mgr.completeCalls)
	}
}

// --- executeLifecycleFail ---

func TestLifecycleFail_DispatchesManagerWithReason(t *testing.T) {
	t.Parallel()
	mgr := newFakeManager()
	mgr.seed("mission", &fakeParticipant{EntityIDF: "f-1", PhaseF: "flying"})

	exec := NewActionExecutor(nil)
	exec.SetLifecycleManager(mgr)

	action := Action{
		Type:     ActionTypeLifecycleFail,
		Workflow: "mission",
		Reason:   "battery critical",
	}
	err := exec.Execute(context.Background(), action, &ExecutionContext{EntityID: "f-1"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(mgr.failCalls) != 1 || mgr.failCalls[0].reason != "battery critical" {
		t.Errorf("Fail dispatch incorrect, got %v", mgr.failCalls)
	}
}

func TestLifecycleFail_EmptyReasonRejected(t *testing.T) {
	t.Parallel()
	mgr := newFakeManager()
	mgr.seed("mission", &fakeParticipant{EntityIDF: "f-2", PhaseF: "flying"})

	exec := NewActionExecutor(nil)
	exec.SetLifecycleManager(mgr)

	action := Action{
		Type:     ActionTypeLifecycleFail,
		Workflow: "mission",
	}
	err := exec.Execute(context.Background(), action, &ExecutionContext{EntityID: "f-2"})
	if err == nil {
		t.Fatalf("expected rejection for empty Reason")
	}
	if len(mgr.failCalls) != 0 {
		t.Errorf("Manager.Fail should not have been called, got %v", mgr.failCalls)
	}
}

// --- S2 + S3 loud-fail: unresolved template tokens in Phase / Reason / Workflow ---

func TestLifecycleTransition_UnresolvedPhaseTokenRejected(t *testing.T) {
	t.Parallel()
	mgr := newFakeManager()
	mgr.seed("mission", &fakeParticipant{EntityIDF: "s2-phase", PhaseF: "planning"})

	exec := NewActionExecutor(nil)
	exec.SetLifecycleManager(mgr)

	// $message.next_phase is referenced but MessageData lacks the key
	// → SubstituteVariables leaves the token verbatim and we must
	// surface the authoring error here (mirror resolveTripleSubject).
	action := Action{
		Type:     ActionTypeLifecycleTransition,
		Workflow: "mission",
		Phase:    "$message.next_phase",
	}
	err := exec.Execute(context.Background(), action, &ExecutionContext{EntityID: "s2-phase"})
	if err == nil {
		t.Fatalf("expected unresolved-template error for phase")
	}
	if !strings.Contains(err.Error(), "unresolved template variables") {
		t.Errorf("error should mention 'unresolved template variables', got %q", err.Error())
	}
	if mgr.entities["mission"]["s2-phase"].PhaseF != "planning" {
		t.Errorf("phase moved despite rejection, got %q", mgr.entities["mission"]["s2-phase"].PhaseF)
	}
}

func TestLifecycleFail_UnresolvedReasonTokenRejected(t *testing.T) {
	t.Parallel()
	mgr := newFakeManager()
	mgr.seed("mission", &fakeParticipant{EntityIDF: "s2-reason", PhaseF: "flying"})

	exec := NewActionExecutor(nil)
	exec.SetLifecycleManager(mgr)

	action := Action{
		Type:     ActionTypeLifecycleFail,
		Workflow: "mission",
		Reason:   "denied by $caller.id", // ec.Caller is nil → token survives
	}
	err := exec.Execute(context.Background(), action, &ExecutionContext{EntityID: "s2-reason"})
	if err == nil {
		t.Fatalf("expected unresolved-template error for reason")
	}
	if !strings.Contains(err.Error(), "unresolved template variables") {
		t.Errorf("error should mention 'unresolved template variables', got %q", err.Error())
	}
	if len(mgr.failCalls) != 0 {
		t.Errorf("Manager.Fail should not be called when Reason has unresolved tokens, got %v", mgr.failCalls)
	}
}

func TestLifecycleTransition_WorkflowSubstitutionResolved(t *testing.T) {
	t.Parallel()
	// S3: Action.Workflow now flows through substitution. Author
	// can data-drive the workflow name via $message.workflow_type.
	mgr := newFakeManager()
	mgr.seed("mission", &fakeParticipant{EntityIDF: "s3-wf-resolved", PhaseF: "planning"})

	exec := NewActionExecutor(nil)
	exec.SetLifecycleManager(mgr)

	action := Action{
		Type:     ActionTypeLifecycleTransition,
		Workflow: "$message.workflow_type",
		Phase:    "flying",
	}
	ec := &ExecutionContext{
		EntityID:    "s3-wf-resolved",
		MessageData: map[string]any{"workflow_type": "mission"},
	}
	err := exec.Execute(context.Background(), action, ec)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if mgr.transitionCalls[0].workflow != "mission" {
		t.Errorf("workflow substitution didn't reach Manager, got %q", mgr.transitionCalls[0].workflow)
	}
}

func TestLifecycleTransition_UnresolvedWorkflowTokenRejected(t *testing.T) {
	t.Parallel()
	mgr := newFakeManager()
	mgr.seed("mission", &fakeParticipant{EntityIDF: "s3-wf-unresolved", PhaseF: "planning"})

	exec := NewActionExecutor(nil)
	exec.SetLifecycleManager(mgr)

	// $message.workflow_type referenced but MessageData lacks the key.
	action := Action{
		Type:     ActionTypeLifecycleTransition,
		Workflow: "$message.workflow_type",
		Phase:    "flying",
	}
	err := exec.Execute(context.Background(), action, &ExecutionContext{EntityID: "s3-wf-unresolved"})
	if err == nil {
		t.Fatalf("expected unresolved-template error for workflow")
	}
	if !strings.Contains(err.Error(), "unresolved template variables") {
		t.Errorf("error should mention 'unresolved template variables', got %q", err.Error())
	}
	if len(mgr.transitionCalls) != 0 {
		t.Errorf("Manager should not see a TransitionWith call when Workflow has unresolved tokens, got %v", mgr.transitionCalls)
	}
}

func TestLifecycleFail_ReasonSubstitution(t *testing.T) {
	t.Parallel()
	mgr := newFakeManager()
	mgr.seed("mission", &fakeParticipant{EntityIDF: "f-3", PhaseF: "flying"})

	exec := NewActionExecutor(nil)
	exec.SetLifecycleManager(mgr)

	action := Action{
		Type:     ActionTypeLifecycleFail,
		Workflow: "mission",
		Reason:   "denied by $caller.id",
	}
	ec := &ExecutionContext{
		EntityID: "f-3",
		Caller:   &CallerContext{ID: "operator-alice"},
	}
	err := exec.Execute(context.Background(), action, ec)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if mgr.failCalls[0].reason != "denied by operator-alice" {
		t.Errorf("reason substitution failed, got %q", mgr.failCalls[0].reason)
	}
}
