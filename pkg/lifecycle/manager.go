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
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/ownership"
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

	// Ownership wiring (ADR-056 Decision 5 embed). Set via AttachOwnership in
	// the framework boot path, AFTER ownership.EnsureBuckets and BEFORE the
	// first Register. All three are written once at attach time and read under
	// m.mu. A nil ownerRegistry — the default, and every nil-client / test /
	// unmigrated-deploy path — makes the ownership axis a pure no-op, so
	// Register behaves exactly as it did pre-ADR-056.
	//
	// Runtime posture for the first consumer is OBSERVE-ONLY: a cross-owner
	// overlap is logged, not bricked (Decision 5 — a partial migration must not
	// fail boot). The hard-fail flip and the write-time owner-lease check are a
	// later increment (see pkg/ownership/doc.go).
	ownerRegistry *ownership.Registry
	ownerCtx      context.Context // app-root ctx; its cancellation stops the heartbeat (no Close needed)
	heartbeater   *ownership.Heartbeater

	// driftSeen memoizes phase-drift Warn-log emissions so List
	// callers polling at dashboard frequencies don't generate
	// N×interval log lines per drifted entity.
	driftSeen sync.Map

	// ownershipWG tracks the heartbeat goroutine spawned by AttachOwnership
	// so callers can join it via WaitOwnership (gh#279). The goroutine is
	// started exactly once — in AttachOwnership — and runs until the ctx
	// passed there is cancelled.
	ownershipWG sync.WaitGroup
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
	// Disjointness needs the parsed projection predicates, so it runs here
	// rather than inside validate() (gh#234).
	if err := workflow.validateDisjointness(meta); err != nil {
		return err
	}

	// Snapshot the ownership wiring + check for a duplicate name under the lock,
	// then RELEASE it before any NATS I/O. RegisterOwner is a CAS loop with
	// network round-trips; holding the registrations RWMutex across it would
	// serialize every concurrent Get/Transition (all take RLock) behind it.
	m.mu.RLock()
	reg := m.ownerRegistry
	ownerCtx := m.ownerCtx
	hb := m.heartbeater
	_, dup := m.registrations[workflow.Name]
	m.mu.RUnlock()
	if dup {
		return fmt.Errorf("%w: workflow %q", ErrWorkflowAlreadyRegistered, workflow.Name)
	}

	// Ownership registration (Decision 5 embed) — outside the lock. Only on
	// success is the local registration committed below, so a registry-rejected
	// owner never half-lands; an observe-only overlap still commits (the workflow
	// must keep working through a partial migration).
	if reg != nil {
		if ownerCtx == nil {
			ownerCtx = context.Background()
		}
		regn := deriveOwnerRegistration(workflow, meta)
		switch err := reg.RegisterOwner(ownerCtx, regn); {
		case err == nil:
			hb.Add(workflow.Name)
		case errors.Is(err, ownership.ErrOwnershipOverlap):
			// Cross-owner collision. OBSERVE-ONLY for the first consumer: the
			// claim is not recorded in the epoch, but the workflow still
			// registers locally and functions (Decision 5 — do not brick a
			// partial migration). Logged loud so the collision is visible now;
			// the hard-fail flip is the enforcement increment.
			m.logger.Warn("lifecycle: ownership overlap registering workflow — observe-only (claim NOT recorded; resolve before the enforcement increment)",
				slog.String("workflow", workflow.Name),
				slog.String("pattern", workflow.EntityIDPattern),
				slog.Any("error", err))
		case errors.Is(err, ownership.ErrInvalidClaim):
			// A malformed claim is a bug in THIS workflow's own declaration
			// (e.g. a Name that is not a subject-safe owner id) — not a
			// partial-migration condition. Always fatal.
			return fmt.Errorf("lifecycle: register %q ownership: %w", workflow.Name, err)
		default:
			// Transient NATS/registry error. Don't brick the workflow in
			// observe-only mode; the claim simply isn't recorded this boot and
			// is re-asserted on the next registrant pass or redeploy.
			m.logger.Warn("lifecycle: ownership registration failed — continuing without claim record",
				slog.String("workflow", workflow.Name),
				slog.Any("error", err))
		}
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
		slog.Bool("ownership_attached", reg != nil),
	)
	return nil
}

// AttachOwnership wires the Manager to the ADR-056 owner registry (Decision 5
// embed). Call it in the framework boot path AFTER ownership.EnsureBuckets and
// BEFORE the first Register, passing a context that is CANCELLED ON SHUTDOWN:
// AttachOwnership spawns a heartbeat goroutine bound to it, and ctx cancellation
// (not a separate Close) is what stops that goroutine. context.Background() will
// LEAK the goroutine until process exit — derive a cancellable context and
// cancel it on the way down (see cmd/semstreams/main.go). A Manager with no
// registry attached (the default, and every nil-client / test / unmigrated-deploy
// path) treats ownership as a pure no-op.
//
// Each subsequently-registered workflow derives an owner claim (deriveOwnerRegistration),
// registers it through the shared epoch (cross-process overlap surfaced —
// Decision 2, observe-only for the first consumer), and is enrolled for liveness
// heartbeating so a later-booting registrant never falsely compacts it.
//
// A nil registry is a no-op (so callers can pass the result of EnsureBuckets
// unconditionally — a resourceless deploy that skipped EnsureBuckets passes nil).
// Idempotent only in the trivial sense; call it once at boot.
func (m *Manager) AttachOwnership(ctx context.Context, reg *ownership.Registry) {
	if reg == nil {
		return
	}
	m.mu.Lock()
	m.ownerRegistry = reg
	m.ownerCtx = ctx
	m.heartbeater = reg.NewHeartbeater(ownership.HeartbeatInterval)
	hb := m.heartbeater
	m.mu.Unlock()

	// One heartbeat goroutine ticks every enrolled owner's presence key until
	// ctx is cancelled. Started here with no owners yet; Register enrolls each.
	// Tracked via ownershipWG so callers can join via WaitOwnership (gh#279).
	m.ownershipWG.Add(1)
	go func() {
		defer m.ownershipWG.Done()
		hb.Run(ctx)
	}()
}

// WaitOwnership blocks until the heartbeat goroutine spawned by AttachOwnership
// exits. Callers should cancel the ctx passed to AttachOwnership first (to signal
// the goroutine), then call WaitOwnership to join it. This closes the gh#279 join
// gap — without this, the goroutine may still be running when the NATS client
// closes, causing a benign-but-noisy "connection closed" error.
//
// No-op when AttachOwnership was never called or was called with a nil registry.
func (m *Manager) WaitOwnership() {
	m.ownershipWG.Wait()
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

// ownerToken returns the wire form of the OwnerToken for a lifecycle write
// (ADR-056 PR-3.5). The token is minted by the attached ownership Registry
// (Registry.OwnerToken) — the Manager never composes the
// "<workflowName>#<incarnation>" format itself. When no Registry is attached
// (nil, test paths, unmigrated deploys) the token is the empty string —
// graph-ingest skips the lease check on an empty token, so the Manager's
// behaviour is unchanged for those paths.
func (m *Manager) ownerToken(workflowName string) string {
	m.mu.RLock()
	reg := m.ownerRegistry
	m.mu.RUnlock()
	if reg == nil {
		return ""
	}
	return reg.OwnerToken(workflowName).Wire()
}

// checkQuiesced returns ErrOwnerQuiesced when the attached ownership Registry has
// QUIESCED workflowName (ADR-056 PR-4): WatchRevival detected another process
// re-registered the same owner id with a different incarnation, so this process
// is the stale writer and must not clobber the live owner's authoritative state.
// A nil Registry (ownership disabled this boot) or an un-quiesced owner returns
// nil — the common case, a single field read on the hot path.
func (m *Manager) checkQuiesced(workflowName string) error {
	m.mu.RLock()
	reg := m.ownerRegistry
	m.mu.RUnlock()
	if reg != nil && reg.IsQuiesced(workflowName) {
		return fmt.Errorf("%w: workflow=%q", ErrOwnerQuiesced, workflowName)
	}
	return nil
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
	// ADR-056 PR-4: refuse the write if this owner was superseded by another
	// process (WatchRevival quiesce) — we are the stale writer, do not clobber.
	if err := m.checkQuiesced(reg.workflow.Name); err != nil {
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
	// ownerTok is the OwnerToken for all writes in this Create call (ADR-056
	// PR-1). Empty when the Registry is not wired — graph-ingest skips the
	// lease check for empty tokens.
	ownerTok := m.ownerToken(reg.workflow.Name)
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
			Triples:    delta,
			OwnerToken: ownerTok,
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
		OwnerToken:       ownerTok,
	}
	if _, err := m.emitter.update(ctx, updateReq); err != nil {
		if errors.Is(err, errs.ErrRevisionMismatch) {
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
//
// AuditPredicates.From is deliberately NOT stamped here — initial
// creation has no prior phase. History reconstruction handles the
// absent-From case by falling back to previousPhase="" for the first
// revision (see Manager.History at manager_query.go:209).
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
	// ADR-056 PR-4: refuse the transition if this owner was superseded (quiesce).
	if err := m.checkQuiesced(reg.workflow.Name); err != nil {
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

		// Replace, don't append: every field Transition writes (phase,
		// audit, mutator-changed scalars) is single-valued, so remove the
		// prior triple for each predicate before adding the new one. Without
		// this, transitions ACCUMULATE phase triples (e.g. [dispatched,
		// executing, completed]); extractTripleScalar reads last-match (so the
		// Manager stays correct) but the rule engine's GetFieldValue reads
		// first-match → it sees the stale initial phase and phase guards never
		// re-fire. Mirrors UpdateFromOperator's add+remove replace model.
		removePreds := make([]string, 0, len(delta))
		for _, t := range delta {
			removePreds = append(removePreds, t.Predicate)
		}
		emitReq := &graph.UpdateEntityWithTriplesRequest{
			Entity: &graph.EntityState{
				ID:          entityID,
				Version:     state.Version + 1,
				UpdatedAt:   now,
				MessageType: lifecycleMessageType,
			},
			AddTriples:       delta,
			RemoveTriples:    removePreds,
			ExpectedRevision: currentRev,
			OwnerToken:       m.ownerToken(reg.workflow.Name),
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
		if errors.Is(err, errs.ErrRevisionMismatch) {
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
	// ADR-056 PR-4: refuse the operator patch if this owner was superseded (quiesce).
	if err := m.checkQuiesced(reg.workflow.Name); err != nil {
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
			OwnerToken:       m.ownerToken(reg.workflow.Name),
		}
		_, err = m.emitter.update(ctx, emitReq)
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
// Idempotent: reclaiming an already-absent entity succeeds (the delete handler
// reports Deleted:false with no error).
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
	if _, err := m.emitter.delete(ctx, &graph.DeleteEntityRequest{EntityID: entityID}); err != nil {
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
// given source/note), then reclaims it via Despawn. The terminal is selected
// like Complete (first reachable terminal from the current phase).
//
// The two graph-ingest operations are NOT atomic. On partial failure the state
// is recoverable, never corrupt: if the terminal transition commits but the
// delete fails (or the process dies between them), the entity is left
// terminal-but-present and a subsequent Despawn reclaims it. Re-invoking
// DespawnWith is also safe — an already-terminal entity skips the transition
// and only reclaims; an already-absent entity is a no-op success.
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
	state, _, err := m.getEntity(ctx, entityID)
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
		if err := m.Transition(ctx, workflow, entityID, terminal, source, note); err != nil {
			return err
		}
	}
	return m.Despawn(ctx, workflow, entityID)
}
