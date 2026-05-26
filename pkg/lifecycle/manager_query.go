// Package lifecycle — query-ops surface (List, Watch, History).
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
)

// List returns instances of the given workflow type matching opts.
// v1 implementation is linear-scan over the workflow's bucket: every
// key is fetched, deserialized, and run through the filter chain.
// Complexity is O(N) per call where N is the bucket size.
//
// Scaling cliff: ~10K active instances is fine for v1 linear scan;
// beyond that the secondary-index v2 work (ADR-047 § KV indexing)
// is the upgrade path. Operators hitting the cliff demonstrate the
// bottleneck and trigger v2 work; the ListOptions surface stays
// stable across the migration so callers don't need to change.
//
// Filter ordering (cheap → expensive) so Match's reflect-based
// comparisons only run on candidates that already passed the
// cheap Phase / Active method-call filters. This is the
// reviewer-recommended optimization to keep reflect cost bounded.
//
// Returned slice ownership: each Participant is a fresh instance
// (factory-produced + json-deserialized). Mutating returned
// pointers does NOT persist — use Update with a mutator closure
// to commit changes. Order is the bucket's ListKeys order (sorted
// for the mock; jetstream returns key insertion order plus
// pagination — operator dashboards should sort caller-side if
// needed).
func (m *Manager) List(ctx context.Context, workflow string, opts ListOptions) ([]Participant, error) {
	reg, err := m.lookupByWorkflow(workflow)
	if err != nil {
		return nil, err
	}

	// Resolve Match field indices ONCE per call (cached lookup by
	// JSON field name → reflect FieldIndex slice). The inner-loop
	// per-candidate reflect cost is then FieldByIndex (constant-
	// time) not FieldByName (linear walk). For a Match with K keys
	// scanned over N entities, this is K+N reflect ops, not K*N.
	matchPlan, err := buildMatchPlan(reg.structMeta, opts.Match)
	if err != nil {
		return nil, fmt.Errorf("lifecycle: List Match resolution for workflow %q: %w", reg.workflow, err)
	}

	keys, err := reg.store.ListKeys(ctx)
	if err != nil {
		return nil, fmt.Errorf("lifecycle: List ListKeys for workflow %q: %w", reg.workflow, err)
	}

	out := make([]Participant, 0, len(keys))
	skipped := 0
	// Iterate via the existing per-entityID resolution path so
	// drift-detection-when-added (TODO(manager) on Participant.Phase)
	// flows through one code path, not two. Apps that need raw KV
	// scan without Participant decoding can drop to natsclient
	// directly — out of scope for the harness.
	for _, key := range keys {
		// Derive the entityID from the key. v1 uses the convention
		// that KVKey(entityID) produces a key from which entityID
		// can be recovered. We extract by inverse: compute the
		// keySample-generated key for a sentinel ID, find the
		// position of the sentinel, then for each scanned key,
		// slice at that position.
		//
		// TODO(list-perf): cache the key→entityID transform at
		// Register time too (mirror of keyForEntity). Today this
		// is invoked per candidate. Land alongside the v2
		// secondary index — scan-path optimization belongs there.
		entityID, ok := entityIDFromKey(key, reg)
		if !ok {
			// Key doesn't match our workflow's KVKey shape — skip
			// rather than error. A bucket may hold keys from
			// multiple sources if operators share buckets across
			// workflows (uncommon but supported).
			skipped++
			continue
		}
		participant, _, getErr := m.getWithRevision(ctx, reg, entityID)
		if getErr != nil {
			// Entity gone between ListKeys and Get — race;
			// quietly skip rather than fail the whole List call.
			if errors.Is(getErr, ErrEntityNotFound) {
				continue
			}
			return nil, fmt.Errorf("lifecycle: List Get %q: %w", entityID, getErr)
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

	if skipped > 0 {
		m.logger.Debug("lifecycle: List skipped keys not matching workflow shape",
			slog.String("workflow", reg.workflow),
			slog.Int("skipped", skipped))
	}

	// Apply Offset / Limit on the filtered set.
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

// Watch streams Participant snapshots for every write to the
// workflow's bucket. Bootstrap-then-live: the first batch is the
// snapshot of current state, then live updates as KV writes
// land. The returned channel closes when ctx is canceled OR the
// underlying watcher errors (caller treats both as "stop
// iterating").
//
// Each delivered Participant is a fresh instance. Mutating it
// does NOT persist (same contract as Get).
//
// Delete-marker events are NOT delivered as Participants —
// they're skipped (no instance to render). Apps that need to
// react to deletions should use the kvStore primitive directly
// via a follow-up tool — Watch's contract is "current Participants
// you can read."
//
// CALLER MUST CANCEL ctx when done iterating. The watcher
// goroutine and its underlying jetstream subscription pin until
// ctx.Done(); a caller that simply stops reading the channel
// without canceling leaks the goroutine and the NATS subscription
// until the next write-then-block triggers buffer back-pressure.
// Pattern:
//
//	ctx, cancel := context.WithCancel(parentCtx)
//	defer cancel()
//	ch, err := mgr.Watch(ctx, workflow)
//	// ...drain ch...
func (m *Manager) Watch(ctx context.Context, workflow string) (<-chan Participant, error) {
	reg, err := m.lookupByWorkflow(workflow)
	if err != nil {
		return nil, err
	}
	src, err := reg.store.Watch(ctx)
	if err != nil {
		return nil, fmt.Errorf("lifecycle: Watch for workflow %q: %w", reg.workflow, err)
	}
	out := make(chan Participant, 16)
	go func() {
		defer close(out)
		for entry := range src {
			if entry.IsDelete {
				// Skip tombstones — Watch contract is current
				// Participants. Apps needing deletion signals
				// hook the kvStore.Watch directly via a future
				// API; out of scope for the harness today.
				continue
			}
			target := reg.factory()
			if err := json.Unmarshal(entry.Value, target); err != nil {
				m.logger.Warn("lifecycle: Watch unmarshal failed; skipping entry",
					slog.String("workflow", reg.workflow),
					slog.String("key", entry.Key),
					slog.Uint64("revision", entry.Revision),
					slog.String("error", err.Error()))
				continue
			}
			select {
			case out <- target:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

// UpdateFromOperator applies a patch map to the entity, validating
// that every patched field is tagged `lifecycle:"operator_writable"`
// in the registered state struct. Default-deny: fields without the
// tag are rejected with ErrFieldNotOperatorWritable. The patch is
// applied atomically under Manager.Update's CAS-retry contract.
//
// Patch values are STRICT-typed against the target field's concrete
// type (reflect.Type.AssignableTo); a Go-typed mismatch (e.g. int
// against a uint32 field) is rejected. HTTP gateway components
// (PR 3 of the ADR-047 bundle) handle query-string-to-Go-type
// coercion at the wire boundary — that is the right place for
// permissive parsing of operator-supplied values, NOT inside
// UpdateFromOperator.
//
// Returns ErrEntityNotFound if the entity doesn't exist (same as
// Update). Returns ErrFieldNotOperatorWritable for the first
// disallowed key encountered. Returns a wrapped type-mismatch
// error for the first incompatible patch value.
func (m *Manager) UpdateFromOperator(ctx context.Context, workflow, entityID string, patch map[string]any) error {
	if len(patch) == 0 {
		return nil // no-op patch is success — operators sometimes
		// hit endpoints with empty bodies and shouldn't see errors
	}
	reg, err := m.lookupByWorkflow(workflow)
	if err != nil {
		return err
	}

	// Pre-validate the entire patch against operator_writable +
	// type compatibility BEFORE the Update mutator runs. Rejecting
	// late inside the mutator would cost a KV round-trip for a
	// patch that was never going to apply.
	plan := make([]operatorPatchSpec, 0, len(patch))
	for key, value := range patch {
		field, ok := reg.structMeta.FieldsByJSONName[key]
		if !ok {
			return fmt.Errorf("lifecycle: UpdateFromOperator key %q does not match any field on workflow %q",
				key, reg.workflow)
		}
		if !field.OperatorWritable {
			return fmt.Errorf("%w: workflow=%q field=%q",
				ErrFieldNotOperatorWritable, reg.workflow, key)
		}
		plan = append(plan, operatorPatchSpec{
			field:    field,
			wantVal:  value,
			jsonName: key,
		})
	}

	return m.Update(ctx, workflow, entityID, func(p Participant) error {
		rv := reflect.ValueOf(p)
		if rv.Kind() == reflect.Pointer {
			rv = rv.Elem()
		}
		for _, spec := range plan {
			target := rv.FieldByIndex(spec.field.FieldIndex)
			patchVal := reflect.ValueOf(spec.wantVal)
			// nil-valued patches set the field to its zero
			// value — matches operator intent "clear this field."
			if !patchVal.IsValid() {
				target.SetZero()
				continue
			}
			if !patchVal.Type().AssignableTo(target.Type()) {
				return fmt.Errorf("lifecycle: UpdateFromOperator field %q: cannot assign %v to %v",
					spec.jsonName, patchVal.Type(), target.Type())
			}
			target.Set(patchVal)
		}
		return nil
	})
}

// operatorPatchSpec is the pre-validated, per-patch-key wiring
// resolved at the top of UpdateFromOperator. Cached locally rather
// than computed inside the mutator so CAS-retry doesn't re-validate
// on every iteration.
type operatorPatchSpec struct {
	field    *fieldMeta
	wantVal  any
	jsonName string
}

// Children returns Participants whose ParentEntityID() equals the
// given parentEntityID, across ALL registered workflows. v1
// implementation is a linear scan over every registered workflow's
// bucket; complexity is O(sum-of-bucket-sizes) per call.
//
// Cross-workflow semantics: a parent in workflow A can have
// children in workflow B (e.g. semspec's Plan-owns-Requirements
// pattern — Plan and Requirement are distinct workflows). Children
// returns all of them merged into one slice; sort caller-side if
// needed.
//
// Scaling consideration: high parent-child fan-out apps should
// prefer List(workflow, Match{"parent_field": parentID}) where
// parent_field is the JSON name of the ParentEntityID-backing
// struct field. That stays within one workflow's bucket and works
// with the v2 secondary-index path when it lands. Children is the
// generic helper for the cross-workflow case; List + Match is the
// scaling-friendly path for the common intra-workflow case.
func (m *Manager) Children(ctx context.Context, parentEntityID string) ([]Participant, error) {
	m.mu.RLock()
	regs := make([]*registration, 0, len(m.registrations))
	for _, reg := range m.registrations {
		regs = append(regs, reg)
	}
	m.mu.RUnlock()

	var out []Participant
	for _, reg := range regs {
		participants, err := m.List(ctx, reg.workflow, ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("lifecycle: Children scan workflow %q: %w", reg.workflow, err)
		}
		for _, p := range participants {
			if p.ParentEntityID() == parentEntityID {
				out = append(out, p)
			}
		}
	}
	return out, nil
}

// Ancestors returns Participants walking from the given entityID
// up the parent chain, ROOT-FIRST (the entity itself is NOT
// included; ancestors[0] is the immediate parent, ancestors[last]
// is the root). Stops when ParentEntityID() returns empty string
// or when a parent can't be resolved (logged + walk truncated).
//
// Cross-workflow: each ancestor may be in a different workflow
// than the starting entity. v1 implementation resolves each
// ancestor by scanning every registered workflow's bucket for the
// entityID — O(workflows × bucket-size) per ancestor lookup. Apps
// with deep parent chains in high-cardinality workflows feel this;
// the v2 secondary-index work (ADR-047 § KV indexing) makes the
// per-ancestor lookup constant-time.
//
// Returns empty slice (not error) for entities with no parent
// (already at root). Returns ErrEntityNotFound if the starting
// entityID itself can't be resolved.
func (m *Manager) Ancestors(ctx context.Context, entityID string) ([]Participant, error) {
	current, err := m.findByEntityID(ctx, entityID)
	if err != nil {
		return nil, err
	}
	var out []Participant
	parentID := current.ParentEntityID()
	for parentID != "" {
		parent, err := m.findByEntityID(ctx, parentID)
		if err != nil {
			if errors.Is(err, ErrEntityNotFound) {
				// Truncate the walk loudly — the chain is
				// broken (parent referenced but deleted or
				// never created). Log so operators see the
				// dangling ref; return what we have.
				m.logger.Warn("lifecycle: Ancestors walk truncated — parent not found",
					slog.String("starting_entity", entityID),
					slog.String("missing_parent", parentID))
				return out, nil
			}
			return nil, err
		}
		out = append(out, parent)
		parentID = parent.ParentEntityID()
	}
	return out, nil
}

// findByEntityID resolves an entityID to a Participant by scanning
// every registered workflow's bucket. Returns ErrEntityNotFound if
// the ID isn't present in any bucket.
//
// Internal helper for Ancestors; not exposed publicly because the
// linear-scan cost across all workflows is bounded only by the
// total instance count. Apps wanting "which workflow does this ID
// belong to" should track that mapping themselves.
func (m *Manager) findByEntityID(ctx context.Context, entityID string) (Participant, error) {
	m.mu.RLock()
	regs := make([]*registration, 0, len(m.registrations))
	for _, reg := range m.registrations {
		regs = append(regs, reg)
	}
	m.mu.RUnlock()

	for _, reg := range regs {
		// Use getFromRegistration directly — m.Get would re-take
		// the registrations RLock once per workflow to re-resolve
		// reg by name. The snapshot above is sufficient.
		p, err := m.getFromRegistration(ctx, reg, entityID)
		if err == nil {
			return p, nil
		}
		if !errors.Is(err, ErrEntityNotFound) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("%w: entity_id=%q (scanned %d workflows)",
		ErrEntityNotFound, entityID, len(regs))
}

// GetWorkflowDefinition returns the WorkflowDef for the given
// workflow type, suitable for operator-dashboard introspection
// (state-machine diagram rendering, patchable-field listing, etc.).
//
// Returns ErrWorkflowNotRegistered if the workflow isn't registered.
func (m *Manager) GetWorkflowDefinition(workflow string) (WorkflowDef, error) {
	reg, err := m.lookupByWorkflow(workflow)
	if err != nil {
		return WorkflowDef{}, err
	}
	return WorkflowDef{
		Workflow:               reg.workflow,
		Transitions:            reg.transitions,
		KVBucket:               reg.bucket,
		OperatorWritableFields: reg.structMeta.OperatorWritableJSONNames(),
	}, nil
}

// ListWorkflows returns the WorkflowDef for every registered
// workflow type, sorted by workflow name for deterministic
// operator-dashboard output across restarts.
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
		reg := m.registrations[name]
		out = append(out, WorkflowDef{
			Workflow:               reg.workflow,
			Transitions:            reg.transitions,
			KVBucket:               reg.bucket,
			OperatorWritableFields: reg.structMeta.OperatorWritableJSONNames(),
		})
	}
	return out
}

// History returns the phase-transition history for the entity at
// entityID, derived from KV revision replay. Each TransitionEvent
// represents one write to the entity's KV key.
//
// Reconstruction semantics (v1 — best-effort from KV alone):
//   - Each revision is decoded as a Participant snapshot
//   - From = previous revision's Phase (empty for the first revision)
//   - To = this revision's Phase
//   - At = revision's CreatedAt timestamp
//   - Triggered = TransitionSourceFramework (cannot recover the
//     original source from KV alone — source/note persistence is a
//     future enhancement that would write an audit triple alongside
//     the state update)
//   - Note = "" (same limitation as Triggered)
//
// Returns ErrEntityNotFound if the entity has no KV history (never
// existed). An entity that existed and was deleted returns its
// history including the final delete-marker as a synthetic
// transition with To="<deleted>" so audit trails can show the
// terminal event.
//
// Revisions where the Phase did NOT change (Update calls that
// mutated other fields but not Phase) are skipped — History
// surfaces phase-transition events specifically, not every
// state mutation. Apps wanting the full revision stream should
// drop to the kvStore primitive directly.
func (m *Manager) History(ctx context.Context, workflow, entityID string) ([]TransitionEvent, error) {
	reg, err := m.lookupByWorkflow(workflow)
	if err != nil {
		return nil, err
	}
	key := keyForEntity(entityID, reg)
	entries, err := reg.store.History(ctx, key)
	if err != nil {
		if errors.Is(err, errKVKeyNotFound) {
			return nil, fmt.Errorf("%w: workflow=%q entity_id=%q",
				ErrEntityNotFound, reg.workflow, entityID)
		}
		return nil, fmt.Errorf("lifecycle: History for %q: %w", entityID, err)
	}

	events := make([]TransitionEvent, 0, len(entries))
	previousPhase := ""
	for _, entry := range entries {
		if entry.IsDelete {
			// Surface the deletion as a synthetic terminal event
			// so audit trails are complete.
			events = append(events, TransitionEvent{
				From:      previousPhase,
				To:        "<deleted>",
				At:        entry.CreatedAt,
				Triggered: TransitionSourceFramework,
			})
			previousPhase = "<deleted>"
			continue
		}
		snapshot := reg.factory()
		if err := json.Unmarshal(entry.Value, snapshot); err != nil {
			// Don't fail the whole History call on a single bad
			// revision — log and skip. A corrupt revision in the
			// stream shouldn't black out the entity's audit trail.
			m.logger.Warn("lifecycle: History unmarshal failed; skipping revision",
				slog.String("workflow", reg.workflow),
				slog.String("entity_id", entityID),
				slog.Uint64("revision", entry.Revision),
				slog.String("error", err.Error()))
			continue
		}
		thisPhase := snapshot.Phase()
		if thisPhase == previousPhase {
			// Update that didn't change phase — not a transition.
			continue
		}
		events = append(events, TransitionEvent{
			From:      previousPhase,
			To:        thisPhase,
			At:        entry.CreatedAt,
			Triggered: TransitionSourceFramework,
		})
		previousPhase = thisPhase
	}
	return events, nil
}

// ---- internal helpers ----

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
			return nil, fmt.Errorf("lifecycle: Match key %q does not match any field on the registered state struct (check json: tags)",
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

// entityIDFromKey extracts the entityID from a KV key by inverse
// of the registration's keySample.KVKey(entityID) transform. v1
// uses a sentinel-substitution approach: compute the prefix/suffix
// once per call from a known sentinel ID, then slice the input key.
//
// Returns (entityID, true) when the key matches the workflow's
// KVKey shape; ("", false) when it doesn't (key from a different
// workflow sharing the bucket, or operator-injected debug keys).
//
// TODO(list-perf): the sentinel-detection is currently per-call.
// Cache the prefix/suffix on registration at Register time so List
// only pays the detection cost once per Register, not once per
// List call. Lands alongside the v2 secondary index work.
func entityIDFromKey(key string, reg *registration) (string, bool) {
	const sentinel = "___LIFECYCLE_ENTITY_ID_SENTINEL___"
	sample := reg.keySample.KVKey(sentinel)
	idx := strings.Index(sample, sentinel)
	if idx < 0 {
		// KVKey didn't incorporate the entityID — apps using
		// constant-key shapes don't roundtrip through this helper.
		// Treat as "doesn't match" so List quietly skips.
		return "", false
	}
	prefix := sample[:idx]
	suffix := sample[idx+len(sentinel):]
	if !strings.HasPrefix(key, prefix) || !strings.HasSuffix(key, suffix) {
		return "", false
	}
	return key[len(prefix) : len(key)-len(suffix)], true
}
