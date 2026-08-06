package lifecycle

import (
	"bytes"
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
	"github.com/c360studio/semstreams/pkg/errs"
	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/nats-io/nats.go/jetstream"
)

// lifecycleMessageType identifies writes from the harness in graph-ingest
// telemetry. Stamped on lifecycle mutations so operators can identify them.
var lifecycleMessageType = message.Type{
	Domain:   "lifecycle",
	Category: "harness",
	Version:  "v1",
}

type entityStatesReader interface {
	ListKeys(context.Context, ...jetstream.WatchOpt) (jetstream.KeyLister, error)
	Watch(context.Context, string, ...jetstream.WatchOpt) (jetstream.KeyWatcher, error)
}

// Manager is the schema-and-discipline layer over ENTITY_STATES
// (ADR-049). Workflow types register at startup via Manager.Register;
// state changes route through graph-ingest via revision-fenced reconcile;
// reads project triples into the registered
// Schema struct.
//
// Concurrency model: registrations map is protected by RWMutex
// (read-heavy: Register at startup, every Get/Transition reads).
// Per-entity concurrency is handled by graph-ingest's CAS — the
// Manager.Transition loop re-reads on ErrKVRevisionMismatch and
// re-validates until either the write commits or the retry budget
// exhausts.
type Manager struct {
	natsClient  *natsclient.Client
	logger      *slog.Logger
	emitter     graphEmitter
	exactReader graph.ExactEntityReader

	// entityStatesBucket is the catalog reader for ENTITY_STATES List and Watch
	// operations. Exact entity reads use exactReader. Graph-ingest remains the
	// single writer. Lazily initialized on first use; in tests it is pre-populated.
	bucketMu           sync.Mutex
	entityStatesBucket entityStatesReader

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
	manager := &Manager{
		natsClient:    client,
		logger:        logger,
		emitter:       newGraphEmitterNATS(client, 5*time.Second),
		registrations: make(map[string]*registration),
	}
	if client != nil {
		manager.exactReader = graph.NewExactEntityReader(client, 5*time.Second)
	}
	return manager
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
	// Every projection path allocates from Schema and converts to Participant
	// with an UNCHECKED assertion. Validating conformance here turns a
	// misconfiguration into a boot failure instead of a panic on whichever
	// request reaches a projection first.
	//
	// Reachability is why this is validated rather than trusted: the read paths
	// reach the conversion only for an entity that already exists, so on a fresh
	// deployment they return not-found long before it — while the operator
	// create lane reaches it with no precondition beyond a registered workflow
	// and a non-empty body. That lane also removed the compile-time backstop:
	// an app whose only creator is the HTTP route never writes a compile-checked
	// mgr.Create(ctx, &T{}).
	probe, ok := reflect.New(meta.GoType).Interface().(Participant)
	if !ok {
		return fmt.Errorf("%w: workflow %q Schema %s does not implement Participant on its pointer",
			ErrInvalidWorkflow, workflow.Name, meta.GoType)
	}
	_ = probe
	// Disjointness needs the parsed projection predicates, so it runs here
	// rather than inside validate() (gh#234).
	if err := workflow.validateDisjointness(meta); err != nil {
		return err
	}

	m.mu.RLock()
	_, dup := m.registrations[workflow.Name]
	m.mu.RUnlock()
	if dup {
		return fmt.Errorf("%w: workflow %q", ErrWorkflowAlreadyRegistered, workflow.Name)
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
func (m *Manager) ensureBucket(ctx context.Context) (entityStatesReader, error) {
	m.bucketMu.Lock()
	defer m.bucketMu.Unlock()
	if m.entityStatesBucket != nil {
		return m.entityStatesBucket, nil
	}
	if m.natsClient == nil {
		return nil, fmt.Errorf("lifecycle: NATS client unavailable (test-mode Manager constructed without an ENTITY_STATES bucket)")
	}
	bucket, err := graph.OpenCatalogReader(ctx, m.natsClient, graph.BucketEntityStates)
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
	if m.exactReader == nil {
		return nil, 0, errors.New("lifecycle: exact entity reader unavailable")
	}
	exact, err := m.exactReader.ReadExactEntity(ctx, entityID)
	if err != nil {
		var classified *errs.ClassifiedError
		if errors.As(err, &classified) && classified.Code == graph.ErrorCodeEntityNotFound {
			return nil, 0, fmt.Errorf("%w: entity_id=%q", ErrEntityNotFound, entityID)
		}
		return nil, 0, fmt.Errorf("lifecycle: exact read for %q: %w", entityID, err)
	}
	return exact.Entity, exact.KVRevision, nil
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
// processor stamping `mission.control.command` before any lifecycle action
// fires); Create coexists with those triples without clobbering.
//
// Returns ErrAlreadyExists if the entity already has a triple for
// the workflow's PhasePredicate (i.e. already lifecycle-managed in
// this workflow). The entity itself may exist with non-lifecycle
// triples and Create still succeeds.
//
// Stamps the initial phase and all non-zero projection-mapped fields in one
// strict create or revision-fenced reconcile chosen by the component.
func (m *Manager) Create(ctx context.Context, initial Participant) error {
	_, err := m.createWithSource(ctx, initial, TransitionSourceFramework)
	return err
}

type createOutcome struct {
	Entity *graph.EntityState
}

// createWithSource is Create with the transition attribution supplied by the
// caller that knows the provenance. A birth initiated through the operator API
// must not record itself as framework-authored.
//
// Unexported deliberately: CreateFromOperator is its only non-framework caller,
// and a new exported symbol needs a named caller at birth. Export it when a
// second provenance actually exists.
func (m *Manager) createWithSource(ctx context.Context, initial Participant, source TransitionSource) (createOutcome, error) {
	if initial == nil {
		return createOutcome{}, errors.New("lifecycle: Create requires non-nil Participant")
	}
	reg, err := m.lookupByWorkflow(initial.Workflow())
	if err != nil {
		return createOutcome{}, err
	}
	return m.createWithRegistration(ctx, reg, initial, source)
}

// createWithRegistration writes against a registration the CALLER selected.
//
// The distinction is load-bearing for any caller that chose a registration by
// something other than the Participant's own Workflow(). Register deliberately
// permits Name != Participant.Workflow() so a local registration alias does
// not brick, which makes the two selectors genuinely divergent:
// re-looking-up by initial.Workflow() would let a request routed and authorized
// as one workflow write with another's entity pattern, transition table, and
// audit predicates — and, where only the alias is registered, fail
// a valid advertised route with a false not-found.
//
// So the registration is threaded, not re-derived. Create re-derives it from the
// Participant (its only honest source); CreateFromOperator passes the one the
// route selected.
func (m *Manager) createWithRegistration(ctx context.Context, reg *registration, initial Participant, source TransitionSource) (createOutcome, error) {
	if initial == nil {
		return createOutcome{}, errors.New("lifecycle: Create requires non-nil Participant")
	}
	entityID := initial.EntityID()
	if entityID == "" {
		return createOutcome{}, errors.New("lifecycle: Create requires non-empty EntityID")
	}
	// An entity ID outside the workflow's declared pattern would COMMIT and then
	// be undiscoverable: List and Watch filter by this pattern, and Despawn
	// refuses to reclaim a non-matching ID — so the instance is readable only by
	// a direct Get that already knows the ID, and can never be removed. Refused
	// before any KV access, and refused for every caller rather than only the
	// operator lane: an unreclaimable orphan is not better because app code made
	// it.
	// semtypes.MatchEntityIDPattern, not the package-local matchPattern: the
	// local glob compares segment counts and wildcards and validates NOTHING
	// about the ID itself, so `..lifecycle.gcs.mission.` and IDs with spaces or
	// uppercase match it, reach graph-ingest, and come back as a classified
	// invalid — which surfaces to an operator as a canned 500. Rejecting the
	// literal here keeps a malformed ID a stable 400 and keeps it away from
	// every downstream side effect.
	matched, err := semtypes.MatchEntityIDPattern(reg.workflow.EntityIDPattern, entityID)
	if err != nil {
		return createOutcome{}, fmt.Errorf("%w: workflow=%q entity_id=%q pattern=%q: %s",
			ErrEntityIDPatternMismatch, reg.workflow.Name, entityID, reg.workflow.EntityIDPattern, err.Error())
	}
	if !matched {
		return createOutcome{}, fmt.Errorf("%w: workflow=%q entity_id=%q pattern=%q",
			ErrEntityIDPatternMismatch, reg.workflow.Name, entityID, reg.workflow.EntityIDPattern)
	}
	// Validate the initial Phase is declared in the transitions
	// table — typo at Create time is friendlier than typo discovered
	// at first Transition attempt.
	if _, declared := reg.workflow.Transitions[initial.Phase()]; !declared {
		return createOutcome{}, fmt.Errorf("%w: initial phase %q not declared in transitions table for workflow %q",
			ErrInvalidTransition, initial.Phase(), reg.workflow.Name)
	}

	// Read current state. Two distinct fresh-create paths:
	//   - entity absent → route through canonical entity.create
	//     (atomic create-or-fail; ErrAlreadyExists on race)
	//   - entity present without phase triple → attach lifecycle via
	//     entity.reconcile with CAS-on-condition (ExpectedRevision
	//     = current rev; concurrent attach fails with revision mismatch
	//     which we surface as ErrAlreadyExists)
	//
	// Per ADR-049 reviewer B2 this split closes the silent concurrent-
	// create race that ExpectedRevision=0 had on the prior code.
	now := time.Now()
	state, rev, err := m.getEntity(ctx, entityID)
	switch {
	case errors.Is(err, ErrEntityNotFound):
		delta := buildInitialTriples(reg, entityID, initial, now, source)
		createReq := &graph.CreateEntityRequest{
			Entity: &graph.EntityState{
				ID:          entityID,
				Version:     1,
				UpdatedAt:   now,
				MessageType: lifecycleMessageType,
			},
			Triples: delta,
		}
		resp, err := m.emitter.create(ctx, createReq)
		if err != nil {
			if errors.Is(err, ErrAlreadyExists) {
				return createOutcome{}, fmt.Errorf("%w: workflow=%q entity_id=%q",
					ErrAlreadyExists, reg.workflow.Name, entityID)
			}
			return createOutcome{}, err
		}
		return createOutcome{Entity: resp.Entity.Clone()}, nil
	case err != nil:
		return createOutcome{}, err
	}
	// Entity exists — must not already have phase triple.
	if hasTriple(state.Triples, entityID, reg.workflow.PhasePredicate) {
		return createOutcome{}, fmt.Errorf("%w: workflow=%q entity_id=%q",
			ErrAlreadyExists, reg.workflow.Name, entityID)
	}
	delta := buildInitialTriples(reg, entityID, initial, now, source)
	reconcileReq := &graph.ReconcilePredicatesRequest{
		EntityID: entityID, ExpectedRevision: rev,
		Predicates: predicatesOf(delta), Desired: delta,
	}
	reconcileResp, err := m.emitter.reconcile(ctx, reconcileReq)
	if err != nil {
		if errors.Is(err, errs.ErrRevisionMismatch) {
			// A revision mismatch means SOMETHING changed the entity — not
			// that a lifecycle birth happened. Any writer merging an unrelated
			// predicate produces one, and reporting that as "already
			// lifecycle-managed" is a false answer on a public route. Re-read
			// and let the phase triple decide.
			latest, _, reErr := m.getEntity(ctx, entityID)
			if reErr == nil && latest != nil && hasTriple(latest.Triples, entityID, reg.workflow.PhasePredicate) {
				return createOutcome{}, fmt.Errorf("%w: workflow=%q entity_id=%q (concurrent attach)",
					ErrAlreadyExists, reg.workflow.Name, entityID)
			}
			// No phase triple: the entity moved for an unrelated reason. Report
			// the contention as retryable rather than as a duplicate birth.
			return createOutcome{}, fmt.Errorf("%w: workflow=%q entity_id=%q: entity changed during lifecycle attach",
				ErrUpdateRetriesExhausted, reg.workflow.Name, entityID)
		}
		return createOutcome{}, err
	}
	return createOutcome{Entity: reconcileResp.Entity.Clone()}, nil
}

// buildInitialTriples constructs Manager.Create's phase, bounded birth record,
// optional latest-transition summary, and non-zero projection fields.
func buildInitialTriples(reg *registration, entityID string, initial Participant, now time.Time, source TransitionSource) []message.Triple {
	delta := []message.Triple{
		triple(entityID, reg.workflow.PhasePredicate, initial.Phase()),
	}
	delta = append(delta, transitionRecordsToTriples(entityID, []transitionRecord{
		newTransitionRecord("", initial.Phase(), now, source, "created"),
	})...)
	if reg.workflow.AuditPredicates.Source != "" {
		delta = append(delta, triple(entityID, reg.workflow.AuditPredicates.Source, string(source)))
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

func predicatesOf(triples []message.Triple) []string {
	predicates := make([]string, 0, len(triples))
	for _, item := range triples {
		predicates = append(predicates, item.Predicate)
	}
	return uniqueStrings(predicates)
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
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
// values and reconciled alongside the phase change in one selected group.
func (m *Manager) TransitionWith(ctx context.Context, workflow, entityID, newPhase string, source TransitionSource, note string, mutator func(Participant) error) error {
	_, err := m.transitionWithOutcome(ctx, workflow, entityID, newPhase, source, note, mutator)
	return err
}

type transitionOutcome struct {
	committedRevision uint64
}

// transitionWithOutcome is the internal transition primitive. Its committed
// revision lets a compound operation fence its next mutation to the exact
// transition it caused without re-reading authority.
func (m *Manager) transitionWithOutcome(
	ctx context.Context,
	workflow, entityID, newPhase string,
	source TransitionSource,
	note string,
	mutator func(Participant) error,
) (transitionOutcome, error) {
	reg, err := m.lookupByWorkflow(workflow)
	if err != nil {
		return transitionOutcome{}, err
	}
	if !isTransitionSource(source) {
		return transitionOutcome{}, fmt.Errorf("%w: unknown transition source %q", ErrInvalidTransition, source)
	}
	if _, declared := reg.workflow.Transitions[newPhase]; !declared {
		return transitionOutcome{}, fmt.Errorf("%w: target phase %q not declared in transitions table for workflow %q",
			ErrInvalidTransition, newPhase, reg.workflow.Name)
	}

	var lastErr error
	for retry := 0; retry < updateRetries; retry++ {
		state, currentRev, err := m.getEntity(ctx, entityID)
		if err != nil {
			return transitionOutcome{}, err
		}
		currentPhase := extractTripleScalar(state.Triples, entityID, reg.workflow.PhasePredicate)
		if currentPhase == "" {
			return transitionOutcome{}, fmt.Errorf("%w: workflow=%q entity_id=%q (no %s triple)",
				ErrEntityNotLifecycleManaged, reg.workflow.Name, entityID, reg.workflow.PhasePredicate)
		}
		if reg.workflow.Transitions.IsTerminal(currentPhase) {
			return transitionOutcome{}, fmt.Errorf("%w: workflow=%q entity_id=%q from=%q",
				ErrTerminalPhase, reg.workflow.Name, entityID, currentPhase)
		}
		if !reg.workflow.Transitions.IsValidTransition(currentPhase, newPhase) {
			return transitionOutcome{}, fmt.Errorf("%w: workflow=%q entity_id=%q from=%q to=%q (not a declared edge)",
				ErrInvalidTransition, reg.workflow.Name, entityID, currentPhase, newPhase)
		}
		records, err := decodeTransitionRecords(entityID, state.Triples)
		if err != nil {
			return transitionOutcome{}, err
		}
		if err := validateTransitionRecordChain(records, currentPhase); err != nil {
			return transitionOutcome{}, err
		}

		// Build the delta: phase + audit + (optional) mutator-changed
		// projection fields.
		now := nextTransitionTimestamp(records, time.Now())
		records = appendTransitionRecord(records,
			newTransitionRecord(currentPhase, newPhase, now, source, note))
		if err := validateTransitionRecordChain(records, newPhase); err != nil {
			return transitionOutcome{}, err
		}
		delta := []message.Triple{
			triple(entityID, reg.workflow.PhasePredicate, newPhase),
		}
		delta = append(delta, transitionRecordsToTriples(entityID, records)...)
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
				return transitionOutcome{}, fmt.Errorf("lifecycle: project entity %q for mutator: %w", entityID, err)
			}
			if err := mutator(projected); err != nil {
				return transitionOutcome{}, fmt.Errorf("lifecycle: TransitionWith mutator rejected change for %q: %w", entityID, err)
			}
			// Diff the mutated projection against the original
			// triples — any field whose projected value changed
			// emits a fresh triple at its declared predicate.
			delta = append(delta, diffProjectedTriples(reg.meta, entityID, state.Triples, projected)...)
		}

		// Replace, don't append: every field Transition writes (phase,
		// audit, mutator-changed scalars) is single-valued, so remove the
		// prior triple for each predicate before adding the new one. Without
		// this, transitions ACCUMULATE phase triples (e.g. [dispatched,
		// executing, completed]); extractTripleScalar reads last-match (so the
		// Manager stays correct) but the rule engine's GetFieldValue reads
		// first-match → it sees the stale initial phase and phase guards never
		// re-fire. Mirrors UpdateFromOperator's add+remove replace model.
		emitReq := &graph.ReconcilePredicatesRequest{
			EntityID: entityID, ExpectedRevision: currentRev,
			Predicates: uniqueStrings(append(predicatesOf(delta), transitionRecordPredicates...)), Desired: delta,
		}
		response, err := m.emitter.reconcile(ctx, emitReq)
		if err == nil {
			m.logger.Debug("lifecycle: transition",
				slog.String("workflow", reg.workflow.Name),
				slog.String("entity_id", entityID),
				slog.String("from", currentPhase),
				slog.String("to", newPhase),
				slog.String("source", string(source)),
				slog.String("note", note),
			)
			return transitionOutcome{committedRevision: response.KVRevision}, nil
		}
		if errors.Is(err, errs.ErrRevisionMismatch) {
			lastErr = err
			continue
		}
		return transitionOutcome{}, err
	}
	return transitionOutcome{}, fmt.Errorf("%w: workflow=%q entity_id=%q after %d retries (last: %v)",
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

		predicates := append([]string(nil), removes...)
		predicates = append(predicates, predicatesOf(adds)...)
		emitReq := &graph.ReconcilePredicatesRequest{
			EntityID: entityID, ExpectedRevision: currentRev,
			Predicates: uniqueStrings(predicates), Desired: adds,
		}
		_, err = m.emitter.reconcile(ctx, emitReq)
		if err == nil {
			return nil
		}
		if errors.Is(err, errs.ErrRevisionMismatch) {
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
	terminal, err := m.selectReachableTerminal(reg, entityID, from)
	if err != nil {
		return err
	}
	return m.Transition(ctx, workflow, entityID, terminal, TransitionSourceFramework, "")
}

// selectReachableTerminal returns the terminal phase deterministically
// reachable from `from` — the first declared terminal with a direct out-edge
// from `from`. This is the selection Complete and DespawnWith share. It logs a
// Warn when the choice is ambiguous (more than one terminal reachable) and
// returns ErrInvalidTransition when none is. Callers MUST have already
// confirmed `from` is non-terminal.
func (m *Manager) selectReachableTerminal(reg *registration, entityID, from string) (string, error) {
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
		return "", fmt.Errorf("%w: workflow=%q entity_id=%q phase=%q has no edge to any terminal phase (declared terminals: %v)",
			ErrInvalidTransition, reg.workflow.Name, entityID, from, terminals)
	}
	if len(reachable) > 1 {
		m.logger.Warn("lifecycle: terminal selection is ambiguous — multiple terminals reachable from current phase; consider Transition for explicit selection",
			slog.String("workflow", reg.workflow.Name),
			slog.String("entity_id", entityID),
			slog.String("from", from),
			slog.String("picked", reachable[0]),
			slog.Any("alternatives", reachable[1:]))
	}
	return reachable[0], nil
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

// Despawn reclaims a lifecycle entity by deleting it from ENTITY_STATES
// through the graph-ingest graph.mutation.entity.delete mutation, so no
// consumer hand-rolls the raw delete (gh#497). It reclaims ONLY — it does NOT
// transition the entity to a terminal phase first; a caller that wants a
// terminal audit trail should Complete/Fail beforehand, or use DespawnWith.
//
// workflow MUST be registered and its EntityIDPattern MUST match entityID; a
// mismatch returns ErrEntityIDPatternMismatch and emits no delete (scopes the
// reclaim to a known workflow and refuses a delete for a foreign entity).
//
// RECLAIM ≠ INDEX GC: Despawn removes the entity from ENTITY_STATES but does
// NOT clean derived indexes (PREDICATE/NAME/ALIAS/CONTEXT/spatial/embedding) —
// that is gh#433/ADR-068 work. Until it lands, a despawned entity may leave
// stale index rows. Despawn introduces no new leak (it centralizes the same
// entity.delete consumers already call); it does not fix the existing one.
func (m *Manager) Despawn(ctx context.Context, workflow, entityID string) error {
	reg, err := m.lookupByWorkflow(workflow)
	if err != nil {
		return err
	}
	if !matchPattern(reg.workflow.EntityIDPattern, entityID) {
		return fmt.Errorf("%w: workflow=%q entity_id=%q pattern=%q",
			ErrEntityIDPatternMismatch, reg.workflow.Name, entityID, reg.workflow.EntityIDPattern)
	}
	_, revision, err := m.getEntity(ctx, entityID)
	if err != nil {
		return fmt.Errorf("lifecycle: Despawn %q: %w", entityID, err)
	}
	if _, err := m.emitter.delete(ctx, &graph.DeleteEntityRequest{
		EntityID: entityID, ExpectedRevision: revision,
	}); err != nil {
		return fmt.Errorf("lifecycle: Despawn %q: %w", entityID, err)
	}
	m.logger.Debug("lifecycle: despawn",
		slog.String("workflow", reg.workflow.Name),
		slog.String("entity_id", entityID),
	)
	return nil
}

// DespawnWith is the common cull: it transitions the entity to its workflow's
// terminal phase (producing the phase write + audit TransitionEvent with the
// given source/note), then conditionally reclaims exactly the revision committed
// by that transition. The terminal is selected like Complete (first reachable
// terminal from the current phase).
//
// The two graph-ingest operations are NOT atomic. On partial failure the state
// is recoverable, never corrupt: if the terminal transition commits but the
// delete fails (or the process dies between them), the entity is left
// terminal-but-present and a subsequent Despawn reclaims it. Re-invoking
// DespawnWith is also safe — an already-terminal entity deletes at the revision
// it observed; an already-absent entity is a no-op success. It never re-reads
// after a committed transition, so newer state causes revision_mismatch rather
// than being silently reclaimed.
//
// Like Despawn, reclaim is NOT index GC (gh#433/ADR-068).
func (m *Manager) DespawnWith(ctx context.Context, workflow, entityID string, source TransitionSource, note string) error {
	reg, err := m.lookupByWorkflow(workflow)
	if err != nil {
		return err
	}
	if !matchPattern(reg.workflow.EntityIDPattern, entityID) {
		return fmt.Errorf("%w: workflow=%q entity_id=%q pattern=%q",
			ErrEntityIDPatternMismatch, reg.workflow.Name, entityID, reg.workflow.EntityIDPattern)
	}
	state, revision, err := m.getEntity(ctx, entityID)
	if errors.Is(err, ErrEntityNotFound) {
		return nil // already gone — nothing to transition or reclaim
	}
	if err != nil {
		return err
	}
	from := extractTripleScalar(state.Triples, entityID, reg.workflow.PhasePredicate)
	if from == "" {
		return fmt.Errorf("%w: workflow=%q entity_id=%q (no %s triple)",
			ErrEntityNotLifecycleManaged, reg.workflow.Name, entityID, reg.workflow.PhasePredicate)
	}
	// Skip the transition when already terminal — keeps DespawnWith idempotent
	// and lets it complete the recovery path a bare Despawn started.
	if !reg.workflow.Transitions.IsTerminal(from) {
		terminal, err := m.selectReachableTerminal(reg, entityID, from)
		if err != nil {
			return err
		}
		outcome, err := m.transitionWithOutcome(ctx, workflow, entityID, terminal, source, note, nil)
		if err != nil {
			return err
		}
		revision = outcome.committedRevision
	}
	if _, err := m.emitter.delete(ctx, &graph.DeleteEntityRequest{
		EntityID: entityID, ExpectedRevision: revision,
	}); err != nil {
		return fmt.Errorf("lifecycle: DespawnWith %q: %w", entityID, err)
	}
	m.logger.Debug("lifecycle: despawn with terminal transition",
		slog.String("workflow", reg.workflow.Name),
		slog.String("entity_id", entityID),
		slog.Uint64("expected_revision", revision),
	)
	return nil
}

// CreateResult is the authoritative instance returned by a successful
// operator-initiated create.
type CreateResult struct {
	Instance Participant
}

// CreateFromOperator decodes an operator-supplied initial state into the
// workflow's registered Schema and creates the instance, returning the
// authoritative committed state (gh#814).
//
// This is the BIRTH lane, and it is the only lane that carries a full initial
// state envelope. The must-exist lanes — UpdateFromOperator and Transition —
// stay envelope-free and continue to require an existing entity. Nothing here
// auto-vivifies: an operator who patches a non-existent instance still gets
// ErrEntityNotFound, because a patch that silently created state would make
// "the instance exists" unfalsifiable from the operator surface.
//
// CREATE-OR-FAIL. It delegates to Create, whose write is a CAS create; a
// duplicate ID returns ErrAlreadyExists rather than overwriting. There is no
// upsert lane on the operator surface, deliberately — upsert would make a
// retried request indistinguishable from a fresh one, and the operator API is
// exactly where that ambiguity is most expensive.
//
// Why the Manager owns the decode rather than handing callers a blank
// Participant to fill: the workflow's Go type is registration-private, the
// projection layer already owns Schema→instance allocation, and a two-step
// "allocate then remember to Create" contract puts a half-built Participant in
// a caller's hands. One call in, committed state out.
//
// The returned state is projected from the CAUSAL mutation response, not from
// a separate read issued afterwards. A post-hoc read answers a different
// question: it can fail after a durable commit (turning a committed birth into
// a 500) and it can observe another writer's later state.
//
// It does NOT check the decoded state's Workflow() against the argument. That
// guard existed briefly and was withdrawn: the target type is chosen BY the
// route, and every production Participant returns Workflow() from a package
// constant, so a request body cannot declare a workflow at all and the check
// could never fire. It passed its own test only because the test double had a
// JSON-decodable workflow field — it tested the double. The real route/body
// binding is the entity-ID pattern gate inside Create.
//
// The wiring invariant it was reaching for (Name == Participant.Workflow()) is
// NOT enforced anywhere, deliberately — Register permits the mismatch so a
// partial migration does not brick (see Register, ADR-056 Decision 5). What
// closes the hazard is that this lane writes against the registration the
// CALLER selected rather than re-deriving one from the Participant's own
// constant (TestCreateFromOperator_UsesTheRouteSelectedRegistration).
//
// Lost or malformed replies surface as commit-unknown errors and are never
// retried automatically.
func (m *Manager) CreateFromOperator(ctx context.Context, workflow string, initial json.RawMessage) (CreateResult, error) {
	reg, err := m.lookupByWorkflow(workflow)
	if err != nil {
		return CreateResult{}, err
	}
	if len(initial) == 0 {
		return CreateResult{}, fmt.Errorf("%w: initial state is required to create a %q instance",
			ErrInvalidInitialState, workflow)
	}

	target := reflect.New(reg.meta.GoType).Interface().(Participant)
	// DisallowUnknownFields: a permissive decode accepts keys this workflow
	// cannot persist, drops them in projectStructToTriples, and still answers
	// 201 — so an operator's submitted state is silently lost with a success.
	// Rejecting is the fail-closed direction and makes the create contract
	// falsifiable from the caller's side.
	dec := json.NewDecoder(bytes.NewReader(initial))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return CreateResult{}, fmt.Errorf("%w: decode initial state for workflow %q: %s",
			ErrInvalidInitialState, workflow, err.Error())
	}

	if target.EntityID() == "" {
		return CreateResult{}, fmt.Errorf("%w: initial state for workflow %q has no entity ID",
			ErrInvalidInitialState, workflow)
	}

	outcome, err := m.createWithRegistration(ctx, reg, target, TransitionSourceOperator)
	if err != nil {
		return CreateResult{}, err
	}
	if outcome.Entity == nil {
		return CreateResult{}, fmt.Errorf("%w: create response has no entity", ErrEmitFailed)
	}

	instance := reflect.New(reg.meta.GoType).Interface().(Participant)
	if err := projectTriples(reg.meta, target.EntityID(), outcome.Entity.Triples, instance); err != nil {
		return CreateResult{}, fmt.Errorf("%w: committed create response could not be projected: %w", ErrEmitFailed, err)
	}
	return CreateResult{Instance: instance}, nil
}
