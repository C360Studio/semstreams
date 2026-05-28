package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sync"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// lifecycleMessageType identifies writes from the harness in graph-ingest
// telemetry. Stamped on every UpdateEntityWithTriplesRequest the Manager
// emits so operators can grep for lifecycle-driven entity mutations.
var lifecycleMessageType = message.Type{
	Domain:   "lifecycle",
	Category: "harness",
	Version:  "v1",
}

// Manager is the schema-and-discipline layer over ENTITY_STATES
// (ADR-049). Workflow types register at startup via Manager.Register;
// state changes route through graph-ingest via the standard
// UpdateEntityWithTriples wire (with CAS-on-condition via
// ExpectedRevision); reads project triples into the registered
// Schema struct.
//
// Concurrency model: registrations map is protected by RWMutex
// (read-heavy: Register at startup, every Get/Transition reads).
// Per-entity concurrency is handled by graph-ingest's CAS — the
// Manager.Transition loop re-reads on ErrKVRevisionMismatch and
// re-validates until either the write commits or the retry budget
// exhausts.
type Manager struct {
	natsClient *natsclient.Client
	logger     *slog.Logger
	emitter    graphEmitter

	// entityStatesBucket is the direct KV handle for ENTITY_STATES.
	// Used for reads (Get, List, History) and Watch — graph-ingest
	// is the single writer, but anyone can read. Lazily initialized
	// on first use; in the test path it's pre-populated.
	bucketMu           sync.Mutex
	entityStatesBucket jetstream.KeyValue

	mu            sync.RWMutex
	registrations map[string]*registration

	// driftSeen memoizes phase-drift Warn-log emissions so List
	// callers polling at dashboard frequencies don't generate
	// N×interval log lines per drifted entity.
	driftSeen sync.Map
}

// registration holds the per-workflow-type state Manager needs at
// every Get / Create / Transition call. Built once at Register time;
// read-only after.
type registration struct {
	workflow Workflow
	meta     *structMeta
}

// NewManager constructs a Manager that talks to NATS via the given
// client. Logger may be nil — falls back to slog.Default.
//
// Workflow registration happens via Manager.Register at app
// startup; this constructor itself does not touch NATS (the
// ENTITY_STATES bucket handle initializes lazily on first read).
func NewManager(client *natsclient.Client, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		natsClient:    client,
		logger:        logger,
		emitter:       newGraphEmitterNATS(client, 5*time.Second),
		registrations: make(map[string]*registration),
	}
}

// newManagerForTest constructs a Manager with an injected emitter
// and an injected ENTITY_STATES bucket. Test-only — production
// callers use NewManager.
func newManagerForTest(logger *slog.Logger, emitter graphEmitter, bucket jetstream.KeyValue) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		logger:             logger,
		emitter:            emitter,
		entityStatesBucket: bucket,
		registrations:      make(map[string]*registration),
	}
}

// Register declares a workflow to the Manager. Must be called at
// app startup, before any Get / Create / Transition calls land.
// Idempotent at the wiring level — re-registering the same workflow
// name returns ErrWorkflowAlreadyRegistered to surface a duplicate-init
// wiring bug.
//
// The Workflow.Schema reflect.Type is reflected over at Register
// time to build the projection metadata (predicate→FieldIndex map);
// the resulting structMeta is cached for the lifetime of the Manager.
//
// Returns ErrInvalidTransitionsTable if the table is internally
// inconsistent. Returns the wrapped tag-parsing error for unknown
// or contradictory lifecycle tags on Schema fields.
func (m *Manager) Register(workflow Workflow) error {
	if err := workflow.validate(); err != nil {
		return err
	}
	meta, err := parseSchemaType(workflow.Schema)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.registrations[workflow.Name]; exists {
		return fmt.Errorf("%w: workflow %q", ErrWorkflowAlreadyRegistered, workflow.Name)
	}

	m.registrations[workflow.Name] = &registration{
		workflow: workflow,
		meta:     meta,
	}
	m.logger.Info("lifecycle: registered workflow",
		slog.String("workflow", workflow.Name),
		slog.String("pattern", workflow.EntityIDPattern),
		slog.Int("phase_count", len(workflow.Transitions)),
		slog.Int("operator_writable_predicates", len(workflow.OperatorWritablePredicates)),
		slog.Int("child_workflows", len(workflow.ChildWorkflows)),
		slog.Int("reference_predicates", len(workflow.ReferencePredicates)),
	)
	return nil
}

// lookupByWorkflow finds the registration for the given workflow type.
// Returns ErrWorkflowNotRegistered when the type was never registered.
func (m *Manager) lookupByWorkflow(workflow string) (*registration, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	reg, ok := m.registrations[workflow]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrWorkflowNotRegistered, workflow)
	}
	return reg, nil
}

// ensureBucket lazy-initializes the ENTITY_STATES bucket handle.
// graph-ingest owns the bucket's lifecycle; the Manager just opens
// an existing handle.
func (m *Manager) ensureBucket(ctx context.Context) (jetstream.KeyValue, error) {
	m.bucketMu.Lock()
	defer m.bucketMu.Unlock()
	if m.entityStatesBucket != nil {
		return m.entityStatesBucket, nil
	}
	if m.natsClient == nil {
		return nil, fmt.Errorf("lifecycle: NATS client unavailable (test-mode Manager constructed without an ENTITY_STATES bucket)")
	}
	bucket, err := m.natsClient.GetKeyValueBucket(ctx, graph.BucketEntityStates)
	if err != nil {
		return nil, fmt.Errorf("lifecycle: open %s bucket: %w", graph.BucketEntityStates, err)
	}
	m.entityStatesBucket = bucket
	return bucket, nil
}

// getEntity reads the entity state from ENTITY_STATES + its current
// KV revision. Returns ErrEntityNotFound when the entity has no
// triples at all (never created).
func (m *Manager) getEntity(ctx context.Context, entityID string) (*graph.EntityState, uint64, error) {
	bucket, err := m.ensureBucket(ctx)
	if err != nil {
		return nil, 0, err
	}
	entry, err := bucket.Get(ctx, entityID)
	if err != nil {
		if errors.Is(err, jetstream.ErrKeyNotFound) {
			return nil, 0, fmt.Errorf("%w: entity_id=%q", ErrEntityNotFound, entityID)
		}
		return nil, 0, fmt.Errorf("lifecycle: KV get for %q: %w", entityID, err)
	}
	var state graph.EntityState
	if err := json.Unmarshal(entry.Value(), &state); err != nil {
		return nil, 0, fmt.Errorf("lifecycle: unmarshal entity %q: %w", entityID, err)
	}
	return &state, entry.Revision(), nil
}

// Get reads the entity at entityID for the given workflow and
// projects its triples into a fresh Participant of the registered
// Schema type. Returns ErrEntityNotFound when the entity doesn't
// exist; ErrEntityNotLifecycleManaged when it exists but has no
// phase triple.
//
// The returned Participant is a fresh instance — mutating it does
// NOT persist. Use Manager.Transition or Manager.UpdateFromOperator
// to commit changes.
func (m *Manager) Get(ctx context.Context, workflow, entityID string) (Participant, error) {
	p, _, err := m.getWithRevision(ctx, workflow, entityID)
	return p, err
}

// GetWithRevision is like Get but also returns the entity's current
// KV revision. Callers building their own CAS loops branch on the
// revision; the framework's own Transition / UpdateFromOperator
// uses it internally.
func (m *Manager) GetWithRevision(ctx context.Context, workflow, entityID string) (Participant, uint64, error) {
	return m.getWithRevision(ctx, workflow, entityID)
}

func (m *Manager) getWithRevision(ctx context.Context, workflow, entityID string) (Participant, uint64, error) {
	reg, err := m.lookupByWorkflow(workflow)
	if err != nil {
		return nil, 0, err
	}
	state, revision, err := m.getEntity(ctx, entityID)
	if err != nil {
		return nil, 0, err
	}
	if !hasTriple(state.Triples, entityID, reg.workflow.PhasePredicate) {
		return nil, 0, fmt.Errorf("%w: workflow=%q entity_id=%q (no %s triple)",
			ErrEntityNotLifecycleManaged, reg.workflow.Name, entityID, reg.workflow.PhasePredicate)
	}

	target := reflect.New(reg.meta.GoType).Interface().(Participant)
	if err := projectTriples(reg.meta, entityID, state.Triples, target); err != nil {
		return nil, 0, fmt.Errorf("lifecycle: project entity %q (workflow %q): %w",
			entityID, reg.workflow.Name, err)
	}

	// Drift detection — entity's projected phase isn't declared in
	// the transitions table. Logged once per (workflow, entity, phase)
	// tuple so polling callers don't flood logs.
	if _, declared := reg.workflow.Transitions[target.Phase()]; !declared {
		driftKey := reg.workflow.Name + "|" + entityID + "|" + target.Phase()
		if _, alreadyLogged := m.driftSeen.LoadOrStore(driftKey, struct{}{}); !alreadyLogged {
			m.logger.Warn("lifecycle: entity phase not declared in transitions table — silent drift detected (logged once per process per drift state)",
				slog.String("workflow", reg.workflow.Name),
				slog.String("entity_id", entityID),
				slog.String("phase", target.Phase()),
				slog.Any("declared_phases", reg.workflow.Transitions.Phases()),
			)
		}
	}
	return target, revision, nil
}

// GetRaw is the debug escape hatch (ADR-049 P1): returns the full
// graph.EntityState for the given entityID without projection or
// workflow-scoping. Operator dashboards use this to render
// arbitrary triples on a lifecycle entity for debug purposes.
//
// Bypasses the projection layer so it has no schema requirements
// — works on any entity in ENTITY_STATES regardless of workflow
// registration. Returns ErrEntityNotFound for unknown entities.
func (m *Manager) GetRaw(ctx context.Context, entityID string) (*graph.EntityState, error) {
	state, _, err := m.getEntity(ctx, entityID)
	return state, err
}

// Create attaches lifecycle to the entity at initial.EntityID() —
// the "add lifecycle dimension" semantics per ADR-049 Q5. The
// entity MAY already exist with non-lifecycle triples (e.g. a
// processor stamping `mission.command` before any lifecycle action
// fires); Create coexists with those triples without clobbering.
//
// Returns ErrAlreadyExists if the entity already has a triple for
// the workflow's PhasePredicate (i.e. already lifecycle-managed in
// this workflow). The entity itself may exist with non-lifecycle
// triples and Create still succeeds.
//
// Stamps the initial phase + all non-zero projection-mapped fields
// as triples in one atomic AddTriplesBatch via graph-ingest.
func (m *Manager) Create(ctx context.Context, initial Participant) error {
	if initial == nil {
		return errors.New("lifecycle: Create requires non-nil Participant")
	}
	reg, err := m.lookupByWorkflow(initial.Workflow())
	if err != nil {
		return err
	}
	entityID := initial.EntityID()
	if entityID == "" {
		return errors.New("lifecycle: Create requires non-empty EntityID")
	}
	// Validate the initial Phase is declared in the transitions
	// table — typo at Create time is friendlier than typo discovered
	// at first Transition attempt.
	if _, declared := reg.workflow.Transitions[initial.Phase()]; !declared {
		return fmt.Errorf("%w: initial phase %q not declared in transitions table for workflow %q",
			ErrInvalidTransition, initial.Phase(), reg.workflow.Name)
	}

	// Read current state. Two distinct fresh-create paths:
	//   - entity absent → route through CreateEntityWithTriples
	//     (atomic create-or-fail; ErrAlreadyExists on race)
	//   - entity present without phase triple → attach lifecycle via
	//     UpdateEntityWithTriples with CAS-on-condition (ExpectedRevision
	//     = current rev; concurrent attach fails with revision mismatch
	//     which we surface as ErrAlreadyExists)
	//
	// Per ADR-049 reviewer B2 this split closes the silent concurrent-
	// create race that ExpectedRevision=0 had on the prior code.
	now := time.Now()
	state, rev, err := m.getEntity(ctx, entityID)
	switch {
	case errors.Is(err, ErrEntityNotFound):
		delta := buildInitialTriples(reg, entityID, initial, now)
		createReq := &graph.CreateEntityWithTriplesRequest{
			Entity: &graph.EntityState{
				ID:          entityID,
				Version:     1,
				UpdatedAt:   now,
				MessageType: lifecycleMessageType,
			},
			Triples: delta,
		}
		if _, err := m.emitter.create(ctx, createReq); err != nil {
			if errors.Is(err, ErrAlreadyExists) {
				return fmt.Errorf("%w: workflow=%q entity_id=%q",
					ErrAlreadyExists, reg.workflow.Name, entityID)
			}
			return err
		}
		return nil
	case err != nil:
		return err
	}
	// Entity exists — must not already have phase triple.
	if hasTriple(state.Triples, entityID, reg.workflow.PhasePredicate) {
		return fmt.Errorf("%w: workflow=%q entity_id=%q",
			ErrAlreadyExists, reg.workflow.Name, entityID)
	}
	delta := buildInitialTriples(reg, entityID, initial, now)
	updateReq := &graph.UpdateEntityWithTriplesRequest{
		Entity: &graph.EntityState{
			ID:          entityID,
			Version:     state.Version + 1,
			UpdatedAt:   now,
			MessageType: lifecycleMessageType,
		},
		AddTriples:       delta,
		ExpectedRevision: rev,
	}
	if _, err := m.emitter.update(ctx, updateReq); err != nil {
		if errors.Is(err, errEmitRevisionMismatch) {
			// Concurrent attach from a sibling — surface as
			// ErrAlreadyExists since the most likely cause is
			// a parallel lifecycle attach.
			return fmt.Errorf("%w: workflow=%q entity_id=%q (concurrent attach)",
				ErrAlreadyExists, reg.workflow.Name, entityID)
		}
		return err
	}
	return nil
}

// buildInitialTriples constructs the triple slice for Manager.Create:
// phase + audit (source=framework, at=now, note="created") + non-zero
// projection fields.
func buildInitialTriples(reg *registration, entityID string, initial Participant, now time.Time) []message.Triple {
	delta := []message.Triple{
		triple(entityID, reg.workflow.PhasePredicate, initial.Phase()),
	}
	if reg.workflow.AuditPredicates.Source != "" {
		delta = append(delta, triple(entityID, reg.workflow.AuditPredicates.Source, string(TransitionSourceFramework)))
	}
	if reg.workflow.AuditPredicates.At != "" {
		delta = append(delta, triple(entityID, reg.workflow.AuditPredicates.At, now.Format(time.RFC3339Nano)))
	}
	if reg.workflow.AuditPredicates.Note != "" {
		delta = append(delta, triple(entityID, reg.workflow.AuditPredicates.Note, "created"))
	}
	delta = append(delta, projectStructToTriples(reg.meta, entityID, initial)...)
	return delta
}

// updateRetries bounds the CAS-conflict retry budget per
// Transition / UpdateFromOperator call. A higher number defends
// against tight loops of concurrent updates; too high invites
// unbounded latency under contention.
const updateRetries = 5

// Transition moves the entity at entityID from its current phase to
// newPhase. Validates against the registered Transitions table and
// emits the phase change + audit triples atomically through
// graph-ingest with CAS-on-condition via ExpectedRevision. On CAS
// conflict, re-reads + re-validates + re-emits up to updateRetries.
func (m *Manager) Transition(ctx context.Context, workflow, entityID, newPhase string, source TransitionSource, note string) error {
	return m.TransitionWith(ctx, workflow, entityID, newPhase, source, note, nil)
}

// TransitionWith is Transition + a caller-supplied mutator that runs
// in the same projected-Participant context as the transition,
// allowing additional fields to be patched atomically with the
// phase change. The mutator runs AFTER transitions-table validation
// but BEFORE the phase delta is built; failures from the mutator
// abort the whole transition.
//
// The mutator's mutations are diffed against the projection-extracted
// values and emitted as triple deltas alongside the phase change.
// Same atomic AddTriplesBatch write.
func (m *Manager) TransitionWith(ctx context.Context, workflow, entityID, newPhase string, source TransitionSource, note string, mutator func(Participant) error) error {
	reg, err := m.lookupByWorkflow(workflow)
	if err != nil {
		return err
	}
	if _, declared := reg.workflow.Transitions[newPhase]; !declared {
		return fmt.Errorf("%w: target phase %q not declared in transitions table for workflow %q",
			ErrInvalidTransition, newPhase, reg.workflow.Name)
	}

	var lastErr error
	for retry := 0; retry < updateRetries; retry++ {
		state, currentRev, err := m.getEntity(ctx, entityID)
		if err != nil {
			return err
		}
		currentPhase := extractTripleScalar(state.Triples, entityID, reg.workflow.PhasePredicate)
		if currentPhase == "" {
			return fmt.Errorf("%w: workflow=%q entity_id=%q (no %s triple)",
				ErrEntityNotLifecycleManaged, reg.workflow.Name, entityID, reg.workflow.PhasePredicate)
		}
		if reg.workflow.Transitions.IsTerminal(currentPhase) {
			return fmt.Errorf("%w: workflow=%q entity_id=%q from=%q",
				ErrTerminalPhase, reg.workflow.Name, entityID, currentPhase)
		}
		if !reg.workflow.Transitions.IsValidTransition(currentPhase, newPhase) {
			return fmt.Errorf("%w: workflow=%q entity_id=%q from=%q to=%q (not a declared edge)",
				ErrInvalidTransition, reg.workflow.Name, entityID, currentPhase, newPhase)
		}

		// Build the delta: phase + audit + (optional) mutator-changed
		// projection fields.
		now := time.Now()
		delta := []message.Triple{
			triple(entityID, reg.workflow.PhasePredicate, newPhase),
		}
		if reg.workflow.AuditPredicates.Source != "" {
			delta = append(delta, triple(entityID, reg.workflow.AuditPredicates.Source, string(source)))
		}
		if reg.workflow.AuditPredicates.At != "" {
			delta = append(delta, triple(entityID, reg.workflow.AuditPredicates.At, now.Format(time.RFC3339Nano)))
		}
		if reg.workflow.AuditPredicates.From != "" {
			delta = append(delta, triple(entityID, reg.workflow.AuditPredicates.From, currentPhase))
		}
		if note != "" && reg.workflow.AuditPredicates.Note != "" {
			delta = append(delta, triple(entityID, reg.workflow.AuditPredicates.Note, note))
		}

		// Run mutator under projection if present.
		if mutator != nil {
			projected := reflect.New(reg.meta.GoType).Interface().(Participant)
			if err := projectTriples(reg.meta, entityID, state.Triples, projected); err != nil {
				return fmt.Errorf("lifecycle: project entity %q for mutator: %w", entityID, err)
			}
			if err := mutator(projected); err != nil {
				return fmt.Errorf("lifecycle: TransitionWith mutator rejected change for %q: %w", entityID, err)
			}
			// Diff the mutated projection against the original
			// triples — any field whose projected value changed
			// emits a fresh triple at its declared predicate.
			delta = append(delta, diffProjectedTriples(reg.meta, entityID, state.Triples, projected)...)
		}

		emitReq := &graph.UpdateEntityWithTriplesRequest{
			Entity: &graph.EntityState{
				ID:          entityID,
				Version:     state.Version + 1,
				UpdatedAt:   now,
				MessageType: lifecycleMessageType,
			},
			AddTriples:       delta,
			ExpectedRevision: currentRev,
		}
		_, err = m.emitter.update(ctx, emitReq)
		if err == nil {
			m.logger.Debug("lifecycle: transition",
				slog.String("workflow", reg.workflow.Name),
				slog.String("entity_id", entityID),
				slog.String("from", currentPhase),
				slog.String("to", newPhase),
				slog.String("source", string(source)),
				slog.String("note", note),
			)
			return nil
		}
		if errors.Is(err, errEmitRevisionMismatch) {
			lastErr = err
			continue
		}
		return err
	}
	return fmt.Errorf("%w: workflow=%q entity_id=%q after %d retries (last: %v)",
		ErrUpdateRetriesExhausted, reg.workflow.Name, entityID, updateRetries, lastErr)
}

// diffProjectedTriples compares the mutated projected struct against
// the original triple slice and returns triple deltas for fields
// whose projected value changed. Skips the phase field (Transition
// emits it explicitly) and read-only fields (ID, audit, reference).
//
// Zero-value semantics (per ADR-049 reviewer B4): when a predicate is
// MISSING from the original triples AND the mutator leaves the field
// at its zero value, no delta is emitted — emitting `field=0` to fill
// a missing predicate is noise, not a meaningful state change. When
// the original triple IS present with zero-value content (the operator
// genuinely set it to zero earlier), and the mutator leaves it at
// zero, also no delta. The diff emits only when the in-memory value
// differs from the projected-string form of the original triple.
//
// Limited to scalars + time.Time per the projection layer's documented
// scope. Slice / map / pointer field types on the Schema are NOT
// supported by projection today; if added later, this diff needs to
// extend its comparison logic.
func diffProjectedTriples(sm *structMeta, entityID string, original []message.Triple, mutated Participant) []message.Triple {
	rv := reflect.ValueOf(mutated)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	var delta []message.Triple
	for predicate, meta := range sm.FieldsByPredicate {
		if meta.IsPhase || meta.ReadOnly {
			continue
		}
		fieldVal := rv.FieldByIndex(meta.FieldIndex)
		if !fieldVal.IsValid() {
			continue
		}
		// Skip when both sides are zero-value-missing: predicate not
		// in original AND mutator left field at zero. Prevents
		// spurious deltas on first Transition of an entity whose
		// mutator does not touch this field.
		hasOriginal := hasTriple(original, entityID, predicate)
		if !hasOriginal && fieldVal.IsZero() {
			continue
		}
		newVal := fieldVal.Interface()
		oldStr := extractTripleScalar(original, entityID, predicate)
		newStr := fmt.Sprintf("%v", newVal)
		if t, ok := newVal.(time.Time); ok {
			newStr = t.Format(time.RFC3339Nano)
		}
		if hasOriginal && oldStr == newStr {
			continue
		}
		delta = append(delta, triple(entityID, predicate, newVal))
	}
	return delta
}

// UpdateFromOperator applies a JSON-keyed patch to the entity,
// validating that every patched field is operator_writable. The
// patch is applied atomically — phase is NOT touched here (use
// Transition for phase changes). CAS retries on revision mismatch.
//
// Patch values are wired to triples via the field's declared
// predicate. nil values map to RemoveTriples for that predicate.
//
// Returns ErrEntityNotFound when the entity doesn't exist;
// ErrEntityNotLifecycleManaged when it exists but has no phase
// triple; ErrFieldNotOperatorWritable for the first protected key
// in the patch.
func (m *Manager) UpdateFromOperator(ctx context.Context, workflow, entityID string, patch map[string]any) error {
	if len(patch) == 0 {
		return nil
	}
	reg, err := m.lookupByWorkflow(workflow)
	if err != nil {
		return err
	}

	// Pre-validate the patch against operator_writable BEFORE the
	// CAS loop — invalid patches shouldn't burn round-trips.
	adds, removes, err := projectPatchToTriples(reg.meta, entityID, patch)
	if err != nil {
		return fmt.Errorf("lifecycle: UpdateFromOperator workflow=%q: %w", reg.workflow.Name, err)
	}

	var lastErr error
	for retry := 0; retry < updateRetries; retry++ {
		state, currentRev, err := m.getEntity(ctx, entityID)
		if err != nil {
			return err
		}
		if !hasTriple(state.Triples, entityID, reg.workflow.PhasePredicate) {
			return fmt.Errorf("%w: workflow=%q entity_id=%q (no %s triple)",
				ErrEntityNotLifecycleManaged, reg.workflow.Name, entityID, reg.workflow.PhasePredicate)
		}

		emitReq := &graph.UpdateEntityWithTriplesRequest{
			Entity: &graph.EntityState{
				ID:          entityID,
				Version:     state.Version + 1,
				UpdatedAt:   time.Now(),
				MessageType: lifecycleMessageType,
			},
			AddTriples:       adds,
			RemoveTriples:    removes,
			ExpectedRevision: currentRev,
		}
		_, err = m.emitter.update(ctx, emitReq)
		if err == nil {
			return nil
		}
		if errors.Is(err, errEmitRevisionMismatch) {
			lastErr = err
			continue
		}
		return err
	}
	return fmt.Errorf("%w: workflow=%q entity_id=%q after %d retries (last: %v)",
		ErrUpdateRetriesExhausted, reg.workflow.Name, entityID, updateRetries, lastErr)
}

// Complete transitions the entity to the first terminal phase
// REACHABLE from its current phase, deterministically. Errors with
// ErrTerminalPhase if the entity is already terminal, or
// ErrInvalidTransition if no terminal phase is reachable.
func (m *Manager) Complete(ctx context.Context, workflow, entityID string) error {
	reg, err := m.lookupByWorkflow(workflow)
	if err != nil {
		return err
	}
	state, _, err := m.getEntity(ctx, entityID)
	if err != nil {
		return err
	}
	from := extractTripleScalar(state.Triples, entityID, reg.workflow.PhasePredicate)
	if from == "" {
		return fmt.Errorf("%w: workflow=%q entity_id=%q (no %s triple)",
			ErrEntityNotLifecycleManaged, reg.workflow.Name, entityID, reg.workflow.PhasePredicate)
	}
	if reg.workflow.Transitions.IsTerminal(from) {
		return fmt.Errorf("%w: workflow=%q entity_id=%q from=%q",
			ErrTerminalPhase, reg.workflow.Name, entityID, from)
	}
	outEdges := reg.workflow.Transitions[from]
	terminals := reg.workflow.Transitions.TerminalPhases()
	var reachable []string
	for _, terminal := range terminals {
		for _, e := range outEdges {
			if e == terminal {
				reachable = append(reachable, terminal)
				break
			}
		}
	}
	if len(reachable) == 0 {
		return fmt.Errorf("%w: workflow=%q entity_id=%q phase=%q has no edge to any terminal phase (declared terminals: %v)",
			ErrInvalidTransition, reg.workflow.Name, entityID, from, terminals)
	}
	if len(reachable) > 1 {
		m.logger.Warn("lifecycle: Complete selection is ambiguous — multiple terminals reachable from current phase; consider Transition for explicit selection",
			slog.String("workflow", reg.workflow.Name),
			slog.String("entity_id", entityID),
			slog.String("from", from),
			slog.String("picked", reachable[0]),
			slog.Any("alternatives", reachable[1:]))
	}
	return m.Transition(ctx, workflow, entityID, reachable[0], TransitionSourceFramework, "")
}

// Fail transitions the entity to the "failed" terminal phase,
// carrying the reason in the audit Note predicate. Errors if no
// "failed" phase is declared on the workflow (apps must call
// Transition explicitly with their preferred error-state phase
// otherwise).
//
// reason must be non-empty — a Fail with no reason defeats the
// audit purpose.
func (m *Manager) Fail(ctx context.Context, workflow, entityID, reason string) error {
	if reason == "" {
		return errors.New("lifecycle: Fail requires non-empty reason (operators need the failure cause in the audit trail)")
	}
	reg, err := m.lookupByWorkflow(workflow)
	if err != nil {
		return err
	}
	const failedPhase = "failed"
	if _, declared := reg.workflow.Transitions[failedPhase]; !declared {
		return fmt.Errorf("%w: workflow %q does not declare a %q phase; call Transition with your preferred error-state phase instead",
			ErrInvalidTransition, reg.workflow.Name, failedPhase)
	}
	return m.Transition(ctx, workflow, entityID, failedPhase, TransitionSourceFramework, reason)
}
