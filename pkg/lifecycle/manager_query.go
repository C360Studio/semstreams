// Package lifecycle — query-ops surface (List, Watch, History,
// Children, References, LookupByEntityID, AssertRuleWritable,
// FieldType, GetWorkflowDefinition, ListWorkflows).
//
// All query ops read from ENTITY_STATES via the direct KV handle —
// graph-ingest remains the single writer; the harness only emits
// through it via the Manager state-change operations defined in
// manager.go.
package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sort"
	"strings"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/nats-io/nats.go/jetstream"
)

// List returns Participants of the given workflow type matching opts.
//
// Implementation: enumerates ENTITY_STATES keys, filters by the
// workflow's EntityIDPattern, loads + projects each match, applies
// the filter chain (Phase / Active / Match), and paginates with
// Limit + Offset.
//
// Complexity is O(N) per call where N is the bucket size; consumers
// hitting the cliff (~10K active instances) trigger the v2
// secondary-index work documented in ADR-049's deferred section.
// The API stays stable across that migration.
func (m *Manager) List(ctx context.Context, workflow string, opts ListOptions) ([]Participant, error) {
	reg, err := m.lookupByWorkflow(workflow)
	if err != nil {
		return nil, err
	}

	matchPlan, err := buildMatchPlan(reg.meta, opts.Match)
	if err != nil {
		return nil, fmt.Errorf("lifecycle: List Match resolution for workflow %q: %w", reg.workflow.Name, err)
	}

	bucket, err := m.ensureBucket(ctx)
	if err != nil {
		return nil, err
	}
	lister, err := bucket.ListKeys(ctx)
	if err != nil {
		return nil, fmt.Errorf("lifecycle: List ListKeys for workflow %q: %w", reg.workflow.Name, err)
	}
	defer lister.Stop()

	out := make([]Participant, 0)
	for key := range lister.Keys() {
		if !matchPattern(reg.workflow.EntityIDPattern, key) {
			continue
		}
		participant, _, getErr := m.getWithRevision(ctx, workflow, key)
		if getErr != nil {
			// Entity gone between list and get, or not yet
			// lifecycle-managed — skip quietly.
			if errors.Is(getErr, ErrEntityNotFound) || errors.Is(getErr, ErrEntityNotLifecycleManaged) {
				continue
			}
			return nil, fmt.Errorf("lifecycle: List Get %q: %w", key, getErr)
		}
		if !matchesPhaseFilter(participant, opts.Phase) {
			continue
		}
		if !matchesActiveFilter(participant, opts.Active) {
			continue
		}
		if !matchesMatchPlan(participant, matchPlan) {
			continue
		}
		out = append(out, participant)
	}

	if opts.Offset > 0 {
		if opts.Offset >= len(out) {
			return []Participant{}, nil
		}
		out = out[opts.Offset:]
	}
	if opts.Limit > 0 && len(out) > opts.Limit {
		out = out[:opts.Limit]
	}
	return out, nil
}

// Watch streams Participant snapshots for every write to
// ENTITY_STATES whose key matches the workflow's EntityIDPattern.
// Bootstrap-then-live: the first batch is the snapshot of current
// state, then live updates as KV writes land.
//
// Each delivered Participant is a fresh instance. Mutating it does
// NOT persist.
//
// CALLER MUST CANCEL ctx when done iterating — the watcher goroutine
// and the underlying jetstream subscription pin until ctx.Done().
func (m *Manager) Watch(ctx context.Context, workflow string) (<-chan Participant, error) {
	reg, err := m.lookupByWorkflow(workflow)
	if err != nil {
		return nil, err
	}
	bucket, err := m.ensureBucket(ctx)
	if err != nil {
		return nil, err
	}
	watcher, err := bucket.WatchAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("lifecycle: Watch for workflow %q: %w", reg.workflow.Name, err)
	}

	out := make(chan Participant, 16)
	go func() {
		defer close(out)
		defer watcher.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case entry, ok := <-watcher.Updates():
				if !ok {
					return
				}
				if entry == nil {
					// nil entry marks end-of-initial-values per
					// the NATS KV bootstrap contract; live events
					// follow.
					continue
				}
				if entry.Operation() == jetstream.KeyValueDelete ||
					entry.Operation() == jetstream.KeyValuePurge {
					continue
				}
				if !matchPattern(reg.workflow.EntityIDPattern, entry.Key()) {
					continue
				}
				var state graph.EntityState
				if err := json.Unmarshal(entry.Value(), &state); err != nil {
					m.logger.Warn("lifecycle: Watch unmarshal failed; skipping entry",
						slog.String("workflow", reg.workflow.Name),
						slog.String("key", entry.Key()),
						slog.Uint64("revision", entry.Revision()),
						slog.String("error", err.Error()),
					)
					continue
				}
				if !hasTriple(state.Triples, entry.Key(), reg.workflow.PhasePredicate) {
					continue
				}
				target := reflect.New(reg.meta.GoType).Interface().(Participant)
				if err := projectTriples(reg.meta, entry.Key(), state.Triples, target); err != nil {
					m.logger.Warn("lifecycle: Watch projection failed; skipping entry",
						slog.String("workflow", reg.workflow.Name),
						slog.String("key", entry.Key()),
						slog.String("error", err.Error()),
					)
					continue
				}
				select {
				case out <- target:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

// History returns the phase-transition history for the entity at
// entityID, derived from ENTITY_STATES revision replay. Each
// TransitionEvent represents one phase change.
//
// Reconstruction reads each historical revision, decodes its
// EntityState, and extracts the phase + audit-predicate values.
// Triggered + Note come from the audit triples Manager.Transition
// stamped at write time — no parallel audit store needed.
//
// The always-`framework` bug (the gap that ADR-049 closes) is
// structurally fixed: the real source is in the audit triple, so
// History reads it back rather than synthesizing a constant.
func (m *Manager) History(ctx context.Context, workflow, entityID string) ([]TransitionEvent, error) {
	reg, err := m.lookupByWorkflow(workflow)
	if err != nil {
		return nil, err
	}
	bucket, err := m.ensureBucket(ctx)
	if err != nil {
		return nil, err
	}
	entries, err := bucket.History(ctx, entityID)
	if err != nil {
		if errors.Is(err, jetstream.ErrKeyNotFound) {
			return nil, fmt.Errorf("%w: workflow=%q entity_id=%q",
				ErrEntityNotFound, reg.workflow.Name, entityID)
		}
		return nil, fmt.Errorf("lifecycle: History for %q: %w", entityID, err)
	}

	events := make([]TransitionEvent, 0, len(entries))
	previousPhase := ""
	for _, entry := range entries {
		if entry.Operation() == jetstream.KeyValueDelete ||
			entry.Operation() == jetstream.KeyValuePurge {
			events = append(events, TransitionEvent{
				From:      previousPhase,
				To:        "<deleted>",
				At:        entry.Created(),
				Triggered: TransitionSourceFramework,
			})
			previousPhase = "<deleted>"
			continue
		}
		var state graph.EntityState
		if err := json.Unmarshal(entry.Value(), &state); err != nil {
			m.logger.Warn("lifecycle: History unmarshal failed; skipping revision",
				slog.String("workflow", reg.workflow.Name),
				slog.String("entity_id", entityID),
				slog.Uint64("revision", entry.Revision()),
				slog.String("error", err.Error()),
			)
			continue
		}
		currentPhase := extractTripleScalar(state.Triples, entityID, reg.workflow.PhasePredicate)
		if currentPhase == "" || currentPhase == previousPhase {
			continue
		}
		event := TransitionEvent{
			From:      previousPhase,
			To:        currentPhase,
			At:        entry.Created(),
			Triggered: TransitionSourceFramework,
		}
		// Recover source attribution from audit triples stamped
		// at this revision (the ADR-049 mechanism that closes the
		// always-framework bug).
		if pred := reg.workflow.AuditPredicates.Source; pred != "" {
			if src := extractTripleScalar(state.Triples, entityID, pred); src != "" {
				event.Triggered = TransitionSource(src)
			}
		}
		if pred := reg.workflow.AuditPredicates.Note; pred != "" {
			event.Note = extractTripleScalar(state.Triples, entityID, pred)
		}
		if pred := reg.workflow.AuditPredicates.From; pred != "" {
			if from := extractTripleScalar(state.Triples, entityID, pred); from != "" {
				event.From = from
			}
		}
		events = append(events, event)
		previousPhase = currentPhase
	}
	return events, nil
}

// Children returns ChildResult entries for every child link
// declared in the parent workflow's ChildWorkflows, optionally
// narrowed by opts.Workflow + paginated by Limit/Offset.
//
// Cross-workflow: the parent and its children may be in different
// workflows. Manager.Children reads the parent's triples, walks
// each ChildSpec's LinkPredicate, and loads each linked entity via
// the child workflow's Get. Children whose child workflow isn't
// registered, or whose entity is gone, are skipped with a Warn log
// — one bad child shouldn't kill the whole response.
func (m *Manager) Children(ctx context.Context, parentEntityID string, opts ChildOptions) ([]ChildResult, error) {
	parentState, _, err := m.getEntity(ctx, parentEntityID)
	if err != nil {
		return nil, err
	}
	parentReg := m.findRegistrationForEntity(parentEntityID)
	if parentReg == nil {
		return nil, fmt.Errorf("lifecycle: Children — entity %q does not match any registered EntityIDPattern",
			parentEntityID)
	}

	type childRef struct{ workflow, entityID string }
	var refs []childRef
	for _, childSpec := range parentReg.workflow.ChildWorkflows {
		if opts.Workflow != "" && opts.Workflow != childSpec.Workflow {
			continue
		}
		for _, t := range parentState.Triples {
			if t.Predicate != childSpec.LinkPredicate {
				continue
			}
			childID, ok := t.Object.(string)
			if !ok {
				continue
			}
			refs = append(refs, childRef{childSpec.Workflow, childID})
		}
	}
	// Stable order for deterministic pagination across calls.
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].workflow != refs[j].workflow {
			return refs[i].workflow < refs[j].workflow
		}
		return refs[i].entityID < refs[j].entityID
	})

	if opts.Offset >= len(refs) {
		return nil, nil
	}
	end := len(refs)
	if opts.Limit > 0 && opts.Offset+opts.Limit < end {
		end = opts.Offset + opts.Limit
	}
	page := refs[opts.Offset:end]

	results := make([]ChildResult, 0, len(page))
	for _, r := range page {
		child, err := m.Get(ctx, r.workflow, r.entityID)
		if err != nil {
			m.logger.Warn("lifecycle: Children — child load failed; skipping",
				slog.String("parent", parentEntityID),
				slog.String("child", r.entityID),
				slog.String("child_workflow", r.workflow),
				slog.String("error", err.Error()),
			)
			continue
		}
		results = append(results, ChildResult{Workflow: r.workflow, State: child})
	}
	return results, nil
}

// References returns ReferenceStub entries for every ReferencePredicate
// triple on the entity. Stubs are light — Workflow + Phase are
// populated only when the target itself is lifecycle-managed
// (matches some registered EntityIDPattern).
func (m *Manager) References(ctx context.Context, entityID string) ([]ReferenceStub, error) {
	state, _, err := m.getEntity(ctx, entityID)
	if err != nil {
		return nil, err
	}
	reg := m.findRegistrationForEntity(entityID)
	if reg == nil {
		return nil, fmt.Errorf("lifecycle: References — entity %q does not match any registered EntityIDPattern",
			entityID)
	}

	var stubs []ReferenceStub
	for _, refSpec := range reg.workflow.ReferencePredicates {
		for _, t := range state.Triples {
			if t.Predicate != refSpec.Predicate {
				continue
			}
			targetID, ok := t.Object.(string)
			if !ok {
				continue
			}
			stub := ReferenceStub{
				EntityID:  targetID,
				Predicate: refSpec.Predicate,
			}
			if targetReg := m.findRegistrationForEntity(targetID); targetReg != nil {
				stub.Workflow = targetReg.workflow.Name
				if target, _, err := m.getEntity(ctx, targetID); err == nil {
					stub.Phase = extractTripleScalar(target.Triples, targetID, targetReg.workflow.PhasePredicate)
				}
			}
			stubs = append(stubs, stub)
		}
	}
	return stubs, nil
}

// LookupByEntityID resolves an entityID to a Participant by matching
// the entity against every registered EntityIDPattern. Returns the
// first matching workflow's projected Participant.
//
// O(workflows) per call — typically a handful of registrations.
// Suitable for the rule engine's `lifecycle_*` action path and
// `$entity.lifecycle.*` substitution path.
func (m *Manager) LookupByEntityID(ctx context.Context, entityID string) (Participant, error) {
	reg := m.findRegistrationForEntity(entityID)
	if reg == nil {
		return nil, fmt.Errorf("%w: entity_id=%q (no registered EntityIDPattern matches)",
			ErrEntityNotFound, entityID)
	}
	return m.Get(ctx, reg.workflow.Name, entityID)
}

// findRegistrationForEntity returns the registration whose
// EntityIDPattern matches the given entityID. Returns nil when no
// registration matches.
//
// Pattern matching: '*' wildcards per dot-separated segment. The
// 6-part EntityIDPattern matches the 6-part entity_id one segment
// at a time.
func (m *Manager) findRegistrationForEntity(entityID string) *registration {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, reg := range m.registrations {
		if matchPattern(reg.workflow.EntityIDPattern, entityID) {
			return reg
		}
	}
	return nil
}

// matchPattern compares a 6-part dotted glob pattern against a
// concrete entity_id. '*' in a segment matches any value for that
// segment; non-'*' segments require exact match.
func matchPattern(pattern, id string) bool {
	if pattern == "" {
		return false
	}
	if pattern == id || pattern == "*" {
		return true
	}
	pParts := strings.Split(pattern, ".")
	iParts := strings.Split(id, ".")
	if len(pParts) != len(iParts) {
		return false
	}
	for i := range pParts {
		if pParts[i] == "*" {
			continue
		}
		if pParts[i] != iParts[i] {
			return false
		}
	}
	return true
}

// AssertRuleWritable returns nil when fieldJSONName is a field on
// the registered workflow that the rule layer's lifecycle_transition
// `set` clause is allowed to mutate. Returns ErrFieldNotOperatorWritable
// otherwise — same default-deny convention as UpdateFromOperator so
// rule definitions can't accidentally exceed operator authority.
func (m *Manager) AssertRuleWritable(workflow, fieldJSONName string) error {
	reg, err := m.lookupByWorkflow(workflow)
	if err != nil {
		return err
	}
	field, ok := reg.meta.FieldsByJSONName[fieldJSONName]
	if !ok {
		return fmt.Errorf("%w: workflow=%q field=%q (no such field on the registered schema)",
			ErrFieldNotOperatorWritable, reg.workflow.Name, fieldJSONName)
	}
	if field.IsID {
		return fmt.Errorf("%w: workflow=%q field=%q (entity_id is immutable — set on Create only, never via rule transition)",
			ErrFieldNotOperatorWritable, reg.workflow.Name, fieldJSONName)
	}
	if field.IsPhase {
		return fmt.Errorf("%w: workflow=%q field=%q (phase is owned by Manager.Transition; use lifecycle_transition's phase field, not set)",
			ErrFieldNotOperatorWritable, reg.workflow.Name, fieldJSONName)
	}
	if !field.OperatorWritable {
		return fmt.Errorf("%w: workflow=%q field=%q (default-deny — tag the struct field `lifecycle:\"operator_writable,predicate=...\"` if rules + operators should be able to mutate it)",
			ErrFieldNotOperatorWritable, reg.workflow.Name, fieldJSONName)
	}
	return nil
}

// FieldType returns the reflect.Type of the named field on the
// registered Schema, for callers (the rule executor) that need to
// apply typed numeric ops (increment/decrement) without
// reimplementing field-name resolution.
func (m *Manager) FieldType(workflow, fieldJSONName string) (reflect.Type, error) {
	if err := m.AssertRuleWritable(workflow, fieldJSONName); err != nil {
		return nil, err
	}
	reg, _ := m.lookupByWorkflow(workflow)
	field := reg.meta.FieldsByJSONName[fieldJSONName]
	t := reg.meta.GoType
	return t.FieldByIndex(field.FieldIndex).Type, nil
}

// GetWorkflowDefinition returns the WorkflowDef for the given
// workflow type. Returns ErrWorkflowNotRegistered when the workflow
// isn't registered.
func (m *Manager) GetWorkflowDefinition(workflow string) (WorkflowDef, error) {
	reg, err := m.lookupByWorkflow(workflow)
	if err != nil {
		return WorkflowDef{}, err
	}
	return workflowDef(reg), nil
}

// ListWorkflows returns the WorkflowDef for every registered
// workflow type, sorted by workflow name for deterministic
// operator-dashboard output.
func (m *Manager) ListWorkflows() []WorkflowDef {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.registrations))
	for name := range m.registrations {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]WorkflowDef, 0, len(names))
	for _, name := range names {
		out = append(out, workflowDef(m.registrations[name]))
	}
	return out
}

func workflowDef(reg *registration) WorkflowDef {
	return WorkflowDef{
		Workflow:                   reg.workflow.Name,
		Transitions:                reg.workflow.Transitions,
		EntityIDPattern:            reg.workflow.EntityIDPattern,
		PhasePredicate:             reg.workflow.PhasePredicate,
		OperatorWritableFields:     reg.meta.OperatorWritableJSONNames(),
		OperatorWritablePredicates: reg.meta.OperatorWritablePredicates(),
	}
}

// ---- internal: match plan + filter chain ----

// matchPlan caches the structMeta resolution for ListOptions.Match
// keys — one FieldIndex slice per match key, so the per-candidate
// loop uses constant-time FieldByIndex instead of linear FieldByName.
type matchPlan struct {
	specs []matchSpec
}

type matchSpec struct {
	jsonName string
	field    *fieldMeta
	wantVal  any
}

// buildMatchPlan resolves Match map keys against the structMeta's
// FieldsByJSONName at List-call time. Errors if a Match key doesn't
// correspond to a known field — apps that pass a typo'd key need
// the loud failure, not silent zero matches.
func buildMatchPlan(sm *structMeta, match map[string]any) (*matchPlan, error) {
	if len(match) == 0 {
		return &matchPlan{}, nil
	}
	plan := &matchPlan{specs: make([]matchSpec, 0, len(match))}
	for key, want := range match {
		field, ok := sm.FieldsByJSONName[key]
		if !ok {
			return nil, fmt.Errorf("lifecycle: Match key %q does not match any field on the registered Schema (check json: tags)",
				key)
		}
		plan.specs = append(plan.specs, matchSpec{
			jsonName: key,
			field:    field,
			wantVal:  want,
		})
	}
	return plan, nil
}

// matchesMatchPlan runs the cached plan against a Participant.
// Pure FieldByIndex reads + reflect.DeepEqual per spec; no map
// lookups in the inner loop.
func matchesMatchPlan(p Participant, plan *matchPlan) bool {
	if plan == nil || len(plan.specs) == 0 {
		return true
	}
	rv := reflect.ValueOf(p)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	for _, spec := range plan.specs {
		got := rv.FieldByIndex(spec.field.FieldIndex).Interface()
		if !reflect.DeepEqual(got, spec.wantVal) {
			return false
		}
	}
	return true
}

// matchesPhaseFilter returns true when Phase filter is empty OR
// matches the participant's current phase.
func matchesPhaseFilter(p Participant, phase string) bool {
	if phase == "" {
		return true
	}
	return p.Phase() == phase
}

// matchesActiveFilter returns true when Active filter is false
// (no filter) OR when the participant is not terminal.
func matchesActiveFilter(p Participant, active bool) bool {
	if !active {
		return true
	}
	return !p.IsTerminal()
}

// triplesEntity is the placeholder shape that signals projection
// callers don't need the full EntityState. Kept here to make the
// unused-import elimination cleaner — not referenced.
var _ message.Triple
