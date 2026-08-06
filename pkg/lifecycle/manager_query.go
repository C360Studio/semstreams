// Package lifecycle — query-ops surface (List, Watch, History,
// Children, References, LookupByEntityID, AssertRuleWritable,
// FieldType, GetWorkflowDefinition, ListWorkflows).
//
// List and Watch use the catalog reader for key enumeration and workflow-pattern
// subscriptions; exact projections use the admitted exact-read adapter.
// graph-ingest remains the single writer, and the harness emits only canonical
// mutations through the Manager state-change operations in manager.go.
package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sort"
	"strings"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/pkg/errs"
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

// EventOp is the kind of change an Event carries.
type EventOp int

const (
	// Upserted marks a create or a phase/field change; the event's
	// Participant holds the projected state.
	Upserted EventOp = iota
	// Deleted marks a reclaim (Manager.Despawn or a raw entity.delete).
	// The event's Participant is nil — only EntityID is meaningful.
	Deleted
)

// String renders the op for logs.
func (o EventOp) String() string {
	switch o {
	case Upserted:
		return "upserted"
	case Deleted:
		return "deleted"
	default:
		return "unknown"
	}
}

// Event is one observation delivered by Manager.WatchEvents.
//
// Consumers MUST treat a Deleted event as "ensure absent for EntityID", not
// "remove a row I previously saw": a delete can arrive for an entity the
// observer filtered out, or one deleted before the watch started. Participant
// is populated only for Upserted.
type Event struct {
	Op          EventOp
	EntityID    string
	Participant Participant
}

// Watch streams Participant snapshots for every write to
// ENTITY_STATES whose key matches the workflow's EntityIDPattern.
// Bootstrap-then-live: the first batch is the snapshot of current
// state, then live updates as KV writes land. Deletes are NOT
// delivered (upsert-only) — use WatchEvents to observe reclaims.
//
// Each delivered Participant is a fresh instance. Mutating it does
// NOT persist.
//
// CALLER MUST CANCEL ctx when done iterating — the watcher goroutine
// and the underlying jetstream subscription pin until ctx.Done().
func (m *Manager) Watch(ctx context.Context, workflow string) (<-chan Participant, error) {
	reg, watcher, err := m.startWatch(ctx, workflow, "Watch")
	if err != nil {
		return nil, err
	}
	out := make(chan Participant, 16)
	go func() {
		defer close(out)
		m.runWatchLoop(ctx, reg, watcher,
			func(_ string, p Participant) bool {
				select {
				case out <- p:
					return true
				case <-ctx.Done():
					return false
				}
			},
			nil, // upsert-only: reclaims are not delivered on this surface
		)
	}()
	return out, nil
}

// WatchEvents is the delete-visible sibling of Watch: it streams
// Events — Upserted (with projected Participant) for matching
// creates/phase-changes, and Deleted (Participant nil) for reclaims
// (KeyValueDelete/KeyValuePurge) whose key matches the workflow's
// EntityIDPattern. Bootstrap-then-live like Watch for Upserted; Deleted
// events are normally live, but the pattern-watch bootstrap can also replay a
// key whose latest revision is a tombstone — so a Deleted may arrive
// during initial values too. Either way treat Deleted as "ensure absent"
// (idempotent), never "remove a row I saw upserted." Lets an observer
// learn of reclaims without a parallel raw KV watch (gh#497).
//
// CALLER MUST CANCEL ctx when done — same pinning contract as Watch.
func (m *Manager) WatchEvents(ctx context.Context, workflow string) (<-chan Event, error) {
	reg, watcher, err := m.startWatch(ctx, workflow, "WatchEvents")
	if err != nil {
		return nil, err
	}
	out := make(chan Event, 16)
	send := func(ev Event) bool {
		select {
		case out <- ev:
			return true
		case <-ctx.Done():
			return false
		}
	}
	go func() {
		defer close(out)
		m.runWatchLoop(ctx, reg, watcher,
			func(entityID string, p Participant) bool {
				return send(Event{Op: Upserted, EntityID: entityID, Participant: p})
			},
			func(entityID string) bool {
				return send(Event{Op: Deleted, EntityID: entityID})
			},
		)
	}()
	return out, nil
}

// startWatch resolves the workflow and opens one pattern subscription for that
// workflow. Validation rides that subscription's matching entries.
func (m *Manager) startWatch(ctx context.Context, workflow, caller string) (*registration, jetstream.KeyWatcher, error) {
	reg, err := m.lookupByWorkflow(workflow)
	if err != nil {
		return nil, nil, err
	}
	bucket, err := m.ensureBucket(ctx)
	if err != nil {
		return nil, nil, err
	}
	watcher, err := bucket.Watch(ctx, reg.workflow.EntityIDPattern)
	if err != nil {
		return nil, nil, errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeIndexNotReady,
			fmt.Errorf("lifecycle: %s pattern watch for workflow %q: %w", caller, reg.workflow.Name, err))
	}
	return reg, watcher, nil
}

// runWatchLoop drives one workflow-pattern watch over ENTITY_STATES, invoking
// onUpsert for each matching projected write and onDelete for each matching reclaim. A
// callback returning false stops the loop (callers use this to honor ctx
// cancellation on a blocked send); the loop also returns on ctx.Done or watcher
// close. onDelete may be nil (Watch's upsert-only surface). Shared by Watch and
// WatchEvents so the projection/dispatch logic is not duplicated.
func (m *Manager) runWatchLoop(
	ctx context.Context,
	reg *registration,
	watcher jetstream.KeyWatcher,
	onUpsert func(entityID string, p Participant) bool,
	onDelete func(entityID string) bool,
) {
	defer watcher.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case entry, ok := <-watcher.Updates():
			if !ok {
				if ctx.Err() != nil {
					return
				}
				m.logger.Warn("lifecycle workflow watcher degraded",
					slog.String("code", graph.ErrorCodeIndexNotReady),
					slog.String("workflow", reg.workflow.Name),
					slog.String("error", "ENTITY_STATES pattern watcher closed unexpectedly"))
				return
			}
			if entry == nil {
				continue
			}

			delivery, keep, err := m.prepareWatchEntry(reg, entry)
			if err != nil {
				var contractErr *graph.StateContractError
				if errors.As(err, &contractErr) {
					m.logger.Warn("lifecycle workflow watch encountered poisoned graph state; closing subscription",
						slog.String("workflow", reg.workflow.Name),
						slog.String("entity", entry.Key()),
						slog.Uint64("revision", entry.Revision()),
						slog.String("code", graph.ErrorCodeGraphStateResetRequired),
						slog.String("reason", string(contractErr.Reason)))
				}
				return
			}
			if !keep {
				continue
			}
			if !m.deliverWatchEntry(delivery, onUpsert, onDelete) {
				return
			}
		}
	}
}

type lifecycleWatchDelivery struct {
	entityID    string
	participant Participant
	deleted     bool
}

// prepareWatchEntry decodes and projects one entry from a workflow-pattern
// watch. Validation belongs to this subscription's matching read path.
func (m *Manager) prepareWatchEntry(
	reg *registration,
	entry jetstream.KeyValueEntry,
) (lifecycleWatchDelivery, bool, error) {
	if entry.Operation() == jetstream.KeyValueDelete || entry.Operation() == jetstream.KeyValuePurge {
		if !matchPattern(reg.workflow.EntityIDPattern, entry.Key()) {
			return lifecycleWatchDelivery{}, false, nil
		}
		return lifecycleWatchDelivery{entityID: entry.Key(), deleted: true}, true, nil
	}

	var state graph.EntityState
	if err := graph.UnmarshalEntityState(entry.Value(), &state); err != nil {
		return lifecycleWatchDelivery{}, false, err
	}
	if !matchPattern(reg.workflow.EntityIDPattern, entry.Key()) {
		return lifecycleWatchDelivery{}, false, nil
	}
	participant, ok := m.projectWatchState(reg, entry.Key(), &state)
	if !ok {
		return lifecycleWatchDelivery{}, false, nil
	}
	return lifecycleWatchDelivery{entityID: entry.Key(), participant: participant}, true, nil
}

func (m *Manager) deliverWatchEntry(
	delivery lifecycleWatchDelivery,
	onUpsert func(entityID string, p Participant) bool,
	onDelete func(entityID string) bool,
) bool {
	if delivery.deleted {
		return onDelete == nil || onDelete(delivery.entityID)
	}
	return onUpsert(delivery.entityID, delivery.participant)
}

// projectWatchState phase-gates and projects a decoded upsert KV entry into a
// fresh Participant of the workflow's Schema type. Returns
// (participant, true) on a matching lifecycle-managed write, or (nil, false)
// when the entry should be skipped — an unmarshal failure, a missing phase
// triple (not yet lifecycle-managed), or a projection failure (the two error
// cases are logged). The caller has already pattern-matched the key and
// confirmed the op is an upsert.
func (m *Manager) projectWatchState(reg *registration, entityID string, state *graph.EntityState) (Participant, bool) {
	if !hasTriple(state.Triples, entityID, reg.workflow.PhasePredicate) {
		return nil, false
	}
	target := reflect.New(reg.meta.GoType).Interface().(Participant)
	if err := projectTriples(reg.meta, entityID, state.Triples, target); err != nil {
		m.logger.Warn("lifecycle: watch projection failed; skipping entry",
			slog.String("workflow", reg.workflow.Name),
			slog.String("key", entityID),
			slog.String("error", err.Error()),
		)
		return nil, false
	}
	return target, true
}

// History returns the bounded operator transition window recorded in the
// participant's current ENTITY_STATES value. The records survive restart with
// bucket History=1 because each phase mutation atomically carries the retained
// occurrence-discriminated records forward. This is not an unbounded audit log.
func (m *Manager) History(ctx context.Context, workflow, entityID string) ([]TransitionEvent, error) {
	reg, err := m.lookupByWorkflow(workflow)
	if err != nil {
		return nil, err
	}
	state, _, err := m.getEntity(ctx, entityID)
	if err != nil {
		return nil, err
	}
	if !hasTriple(state.Triples, entityID, reg.workflow.PhasePredicate) {
		return nil, fmt.Errorf("%w: workflow=%q entity_id=%q",
			ErrEntityNotLifecycleManaged, reg.workflow.Name, entityID)
	}
	records, err := decodeTransitionRecords(entityID, state.Triples)
	if err != nil {
		return nil, fmt.Errorf("lifecycle: History for %q: %w", entityID, err)
	}
	currentPhase := extractTripleScalar(state.Triples, entityID, reg.workflow.PhasePredicate)
	if err := validateTransitionRecordChain(records, currentPhase); err != nil {
		return nil, fmt.Errorf("lifecycle: History for %q: %w", entityID, err)
	}
	events := make([]TransitionEvent, len(records))
	for i := range records {
		events[i] = records[i].event
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

// References returns source-derived relationship facts for every declared
// ReferencePredicate on the entity. It performs exactly one authority read for
// the source. Targets are not hydrated, classified, or checked for existence;
// an unresolved object ID is valid eventual graph state.
func (m *Manager) References(ctx context.Context, entityID string) ([]RelationshipReference, error) {
	state, _, err := m.getEntity(ctx, entityID)
	if err != nil {
		return nil, err
	}
	reg := m.findRegistrationForEntity(entityID)
	if reg == nil {
		return nil, fmt.Errorf("lifecycle: References — entity %q does not match any registered EntityIDPattern",
			entityID)
	}

	var references []RelationshipReference
	for _, refSpec := range reg.workflow.ReferencePredicates {
		for _, t := range state.Triples {
			if t.Predicate != refSpec.Predicate {
				continue
			}
			targetID, ok := t.Object.(string)
			if !ok {
				continue
			}
			reference := RelationshipReference{
				EntityID:  targetID,
				Predicate: refSpec.Predicate,
			}
			references = append(references, reference)
		}
	}
	return references, nil
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
