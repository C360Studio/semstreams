package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"sync"

	"github.com/c360studio/semstreams/natsclient"
)

// Manager is the framework-provided harness over Participant
// implementations. One Manager per process; workflow types register
// at startup via Manager.Register.
//
// Concurrency model: registrations map is protected by RWMutex
// (read-heavy: Register at startup, every Get/Update reads).
// Per-entity concurrency is delegated to the kvStore's revision-CAS
// semantics — Manager.Update's mutator closure runs under retry-
// on-conflict, so concurrent updates to the same entity serialize
// without an in-process per-entity lock.
type Manager struct {
	natsClient *natsclient.Client
	logger     *slog.Logger

	// storeFactory builds a per-bucket kvStore. The production
	// constructor (NewManager) wires this to a kvNATSStore-builder
	// closure over natsClient; tests inject a mock-store factory
	// via newManagerForTest.
	storeFactory func(bucket string) (kvStore, error)

	mu            sync.RWMutex
	registrations map[string]*registration
}

// registration holds the per-workflow-type state Manager needs at
// every Get / Create / Update call. Built once at Register time;
// read-only after — every field is immutable post-Register so the
// RWMutex only protects the map insert, not the registration body.
type registration struct {
	workflow    string
	factory     func() Participant
	transitions Transitions
	structMeta  *structMeta
	bucket      string
	store       kvStore

	// keySample is a cached factory instance held solely to
	// dispatch the KVKey(entityID) method without allocating a
	// fresh Participant per Get/Update. Per ADR-047's KVKey
	// contract, KVKey must be a pure function of entityID — no
	// per-instance struct state is consulted — so sharing one
	// sample across all calls is safe even under concurrency
	// (KVKey is read-only on the receiver).
	keySample Participant
}

// NewManager constructs a Manager that talks to NATS via the given
// client. Logger may be nil — falls back to slog.Default.
//
// Workflow registration happens via Manager.Register at app
// startup; this constructor itself does not touch NATS (per-bucket
// kvStore handles open lazily at Register time so deployments with
// no workflows registered pay no KV cost).
func NewManager(client *natsclient.Client, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	m := &Manager{
		natsClient:    client,
		logger:        logger,
		registrations: make(map[string]*registration),
	}
	m.storeFactory = m.defaultStoreFactory
	return m
}

// newManagerForTest constructs a Manager wired to the given store
// factory, bypassing NATS entirely. Test-only — production callers
// use NewManager.
func newManagerForTest(logger *slog.Logger, factory func(bucket string) (kvStore, error)) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		logger:        logger,
		storeFactory:  factory,
		registrations: make(map[string]*registration),
	}
}

// defaultStoreFactory is NewManager's wiring to kvNATSStore. Kept
// as a method so newManagerForTest can override storeFactory before
// any Register call lands.
func (m *Manager) defaultStoreFactory(bucket string) (kvStore, error) {
	return newKVNATSStore(m.natsClient, bucket)
}

// Register declares a workflow type to the Manager. Must be called
// at app startup, before any Get / Create / Update calls land.
// Idempotent at the wiring level — re-registering the same workflow
// returns ErrWorkflowAlreadyRegistered to surface a duplicate-init
// wiring bug.
//
// The factory returns a fresh zero-value Participant; Manager.Get
// uses it as the json.Unmarshal target so the codec can populate
// the app's typed state struct. Factory must return a POINTER
// receiver (json.Unmarshal can't populate value-typed Participant
// implementations).
//
// transitions is validated against Transitions.Validate before
// registration commits; an invalid table is rejected with the
// validation error wrapped as ErrInvalidTransitionsTable.
func (m *Manager) Register(workflow string, factory func() Participant, transitions Transitions) error {
	if workflow == "" {
		return fmt.Errorf("%w: workflow name required", ErrWorkflowNotRegistered)
	}
	if factory == nil {
		return fmt.Errorf("%w: factory required for workflow %q", ErrWorkflowNotRegistered, workflow)
	}

	if err := transitions.Validate(); err != nil {
		return err
	}

	sample := factory()
	if sample == nil {
		return fmt.Errorf("%w: factory for workflow %q returned nil", ErrWorkflowNotRegistered, workflow)
	}

	// Validate factory return shape — must be a non-nil pointer to a
	// settable struct (json.Unmarshal target + reflect.Value field
	// writes both require addressability).
	rv := reflect.ValueOf(sample)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("%w: factory for workflow %q must return non-nil pointer (got %v)",
			ErrWorkflowNotRegistered, workflow, rv.Kind())
	}

	structMeta, err := parseStructTags(rv)
	if err != nil {
		return err
	}

	bucket := sample.KVBucket()
	if bucket == "" {
		// Defensive: an empty KVBucket would route every workflow's
		// instances into bucket "" — almost certainly a wiring bug.
		return fmt.Errorf("%w: factory for workflow %q returned empty KVBucket",
			ErrWorkflowNotRegistered, workflow)
	}

	if sample.Workflow() == "" {
		return fmt.Errorf("%w: factory for workflow %q returned empty Workflow() — must match the registered name",
			ErrWorkflowNotRegistered, workflow)
	}
	if sample.Workflow() != workflow {
		return fmt.Errorf("%w: factory for workflow %q returns Workflow()=%q (mismatch — factory's Workflow() must match the registered name)",
			ErrWorkflowNotRegistered, workflow, sample.Workflow())
	}

	store, err := m.storeFactory(bucket)
	if err != nil {
		return fmt.Errorf("lifecycle: open KV bucket %q for workflow %q: %w", bucket, workflow, err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.registrations[workflow]; exists {
		return fmt.Errorf("%w: workflow %q", ErrWorkflowAlreadyRegistered, workflow)
	}

	m.registrations[workflow] = &registration{
		workflow:    workflow,
		factory:     factory,
		transitions: transitions,
		structMeta:  structMeta,
		bucket:      bucket,
		store:       store,
		keySample:   sample,
	}
	m.logger.Info("lifecycle: registered workflow",
		slog.String("workflow", workflow),
		slog.String("bucket", bucket),
		slog.Int("phase_count", len(transitions)))
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

// Get loads the instance at entityID for the given workflow type.
// Returns ErrEntityNotFound when no instance exists at the key,
// ErrWorkflowNotRegistered when the workflow type isn't registered.
//
// The returned Participant is a fresh instance — mutating it does
// NOT persist. Use Manager.Update with a mutator closure to commit
// changes.
//
// Drift validation: TODO(manager) — see Participant.Phase doc-comment.
// Currently this just returns whatever Phase() the deserialized struct
// reports, even if it's not in the registered Transitions table.
// PR 1b's query-ops slice adds the drift check.
func (m *Manager) Get(ctx context.Context, workflow, entityID string) (Participant, error) {
	reg, err := m.lookupByWorkflow(workflow)
	if err != nil {
		return nil, err
	}
	return m.getFromRegistration(ctx, reg, entityID)
}

// getFromRegistration is the shared Get-by-resolved-registration
// path. Reused by Update so it doesn't re-take the registrations
// RLock per retry.
func (m *Manager) getFromRegistration(ctx context.Context, reg *registration, entityID string) (Participant, error) {
	p, _, err := m.getWithRevision(ctx, reg, entityID)
	return p, err
}

// getWithRevision returns the Participant plus the KV revision at
// which it was loaded — required for Update's CAS write. The Get
// public method discards the revision; Update uses it.
func (m *Manager) getWithRevision(ctx context.Context, reg *registration, entityID string) (Participant, uint64, error) {
	key := keyForEntity(entityID, reg)
	value, revision, err := reg.store.Get(ctx, key)
	if err != nil {
		if errors.Is(err, errKVKeyNotFound) {
			return nil, 0, fmt.Errorf("%w: workflow=%q entity_id=%q",
				ErrEntityNotFound, reg.workflow, entityID)
		}
		return nil, 0, fmt.Errorf("lifecycle: KV get for %q: %w", entityID, err)
	}
	target := reg.factory()
	if err := json.Unmarshal(value, target); err != nil {
		return nil, 0, fmt.Errorf("lifecycle: unmarshal entity %q (workflow %q): %w",
			entityID, reg.workflow, err)
	}
	return target, revision, nil
}

// Create commits a fresh Participant to KV. Returns an error if
// an instance already exists at the same EntityID (create-only
// semantics — apps that want to overwrite must Delete first OR
// use Update with a fresh Get).
//
// The initial.EntityID() value is used as both the JSON identity
// and the KV key derivation source (via Participant.KVKey()).
func (m *Manager) Create(ctx context.Context, initial Participant) error {
	if initial == nil {
		return errors.New("lifecycle: Create requires non-nil Participant")
	}
	reg, err := m.lookupByWorkflow(initial.Workflow())
	if err != nil {
		return err
	}
	// Validate the initial Phase is declared in the transitions
	// table — typo at Create time is friendlier than typo discovered
	// at first Transition attempt.
	if _, declared := reg.transitions[initial.Phase()]; !declared {
		return fmt.Errorf("%w: initial phase %q not declared in transitions table for workflow %q",
			ErrInvalidTransition, initial.Phase(), reg.workflow)
	}
	value, err := json.Marshal(initial)
	if err != nil {
		return fmt.Errorf("lifecycle: marshal Create payload: %w", err)
	}
	key := keyForEntity(initial.EntityID(), reg)
	if _, err := reg.store.Create(ctx, key, value); err != nil {
		if errors.Is(err, errKVKeyExists) {
			return fmt.Errorf("lifecycle: Create — entity %q already exists in workflow %q",
				initial.EntityID(), reg.workflow)
		}
		return fmt.Errorf("lifecycle: KV create for %q: %w", initial.EntityID(), err)
	}
	return nil
}

// updateRetries bounds the CAS-conflict retry budget per Update call.
// A higher number defends against tight loops of concurrent updates;
// too high invites unbounded latency under contention. 5 is enough
// to absorb typical bursts (a few rules racing on the same entity)
// without becoming a hidden bottleneck if a rule is stuck rewriting
// the same entity in a tight loop.
const updateRetries = 5

// Update mutates the entity at entityID via the given mutator
// closure, under CAS retry semantics:
//
//  1. Get current state + revision
//  2. Call mutator(participant) — mutator mutates the pointer in place
//  3. Marshal + KV.Update with the revision
//  4. On ErrKVRevisionMismatch, retry from step 1 (up to updateRetries)
//
// mutator errors abort the update immediately with the wrapped error
// — no retry, no KV write. The mutator must NOT call Manager methods
// (re-entry would deadlock the registrations RWMutex and risk
// CAS-loop tail-chases).
//
// Returns ErrEntityNotFound if the entity doesn't exist at any
// point during the retry budget. Returns the wrapped mutator error
// if the closure rejected the proposed change. Returns
// ErrKVRevisionMismatch if the retry budget exhausted under
// persistent contention.
func (m *Manager) Update(ctx context.Context, workflow, entityID string, mutator func(Participant) error) error {
	if mutator == nil {
		return errors.New("lifecycle: Update requires non-nil mutator")
	}
	reg, err := m.lookupByWorkflow(workflow)
	if err != nil {
		return err
	}
	for range updateRetries {
		current, revision, err := m.getWithRevision(ctx, reg, entityID)
		if err != nil {
			return err
		}
		if err := mutator(current); err != nil {
			return fmt.Errorf("lifecycle: Update mutator rejected change for %q: %w", entityID, err)
		}
		value, err := json.Marshal(current)
		if err != nil {
			return fmt.Errorf("lifecycle: marshal Update payload for %q: %w", entityID, err)
		}
		key := keyForEntity(entityID, reg)
		if _, err := reg.store.Update(ctx, key, value, revision); err != nil {
			if errors.Is(err, errKVRevisionMismatch) {
				// CAS conflict — concurrent writer beat us. Retry.
				continue
			}
			if errors.Is(err, errKVKeyNotFound) {
				return fmt.Errorf("%w: workflow=%q entity_id=%q (deleted during Update)",
					ErrEntityNotFound, reg.workflow, entityID)
			}
			return fmt.Errorf("lifecycle: KV update for %q: %w", entityID, err)
		}
		return nil
	}
	return fmt.Errorf("%w: workflow=%q entity_id=%q after %d retries",
		errKVRevisionMismatch, reg.workflow, entityID, updateRetries)
}

// Transition moves the entity at entityID from its current phase to
// newPhase, atomically with any CAS retries. Validates the transition
// against the registered Transitions table:
//
//   - newPhase must be declared in the table (typo catcher)
//   - (current → newPhase) must be a declared edge
//   - current phase must NOT be terminal (terminal entities cannot
//     transition further — surfaces ErrTerminalPhase distinctly so
//     dashboards can show "cannot abort an already-completed mission")
//
// source identifies what triggered the transition (rule action,
// operator API, component direct call, framework auto). Persisted
// in the History stream for audit (the History query op lands in
// the next commit).
//
// Note is an optional free-text annotation; Manager.Fail uses it
// to carry the failure reason. Pass "" when not relevant.
func (m *Manager) Transition(ctx context.Context, workflow, entityID, newPhase string, source TransitionSource, note string) error {
	reg, err := m.lookupByWorkflow(workflow)
	if err != nil {
		return err
	}
	if _, declared := reg.transitions[newPhase]; !declared {
		return fmt.Errorf("%w: target phase %q not declared in transitions table for workflow %q",
			ErrInvalidTransition, newPhase, reg.workflow)
	}
	return m.Update(ctx, workflow, entityID, func(p Participant) error {
		from := p.Phase()
		if reg.transitions.IsTerminal(from) {
			return fmt.Errorf("%w: workflow=%q entity_id=%q from=%q",
				ErrTerminalPhase, reg.workflow, entityID, from)
		}
		if !reg.transitions.IsValidTransition(from, newPhase) {
			return fmt.Errorf("%w: workflow=%q entity_id=%q from=%q to=%q (not a declared edge)",
				ErrInvalidTransition, reg.workflow, entityID, from, newPhase)
		}
		// Mutate the Phase field in place via reflection (the
		// alternative — requiring Participant.SetPhase(string) on
		// the interface — puts boilerplate on every app's state
		// struct; the cached structMeta.PhaseField makes this a
		// single field-by-index reflect write).
		if err := setPhaseField(p, reg.structMeta, newPhase); err != nil {
			return err
		}
		// Source + note are observable today via slog; the History
		// query op in the next commit reconstructs full TransitionEvent
		// records from KV revision metadata + a per-update audit
		// triple. For now the log line is the audit trail.
		// TODO(history): emit a TransitionEvent{From, To, At,
		// Triggered: source, Note: note} stamped onto the entity (or
		// a side KV history bucket) so History can return it.
		m.logger.Debug("lifecycle: transition",
			slog.String("workflow", reg.workflow),
			slog.String("entity_id", entityID),
			slog.String("from", from),
			slog.String("to", newPhase),
			slog.String("source", string(source)),
			slog.String("note", note))
		return nil
	})
}

// Complete transitions the entity to the first terminal phase
// REACHABLE from its current phase (sorted iteration of declared
// terminals + first one with an in-edge from the current phase).
// Deterministic across processes — picks the same target every time
// given the same transitions table.
//
// Errors with ErrTerminalPhase if the entity is already terminal,
// or ErrInvalidTransition if no terminal phase is reachable from
// the current phase (e.g. transitions table declares terminals but
// the current phase has no edge to any of them — wiring bug worth
// surfacing).
//
// Apps with semantic terminal-name distinctions (failed vs aborted
// vs cancelled vs completed) should call Transition directly with
// the specific terminal phase rather than relying on Complete's
// selection. Complete is the convenience for "mark this done" in
// workflows where the natural success-terminal is unambiguous from
// the current phase's edge set.
//
// There is a small race window between the internal Get-for-current-
// phase and the subsequent Transition: if a concurrent writer
// transitions the entity to terminal between our two operations,
// the Transition call surfaces ErrTerminalPhase. The benign-race
// outcome (entity is terminal as desired) reads as an error to
// the caller — acceptable trade-off for the simpler API.
func (m *Manager) Complete(ctx context.Context, workflow, entityID string) error {
	reg, err := m.lookupByWorkflow(workflow)
	if err != nil {
		return err
	}
	current, err := m.Get(ctx, workflow, entityID)
	if err != nil {
		return err
	}
	from := current.Phase()
	if reg.transitions.IsTerminal(from) {
		return fmt.Errorf("%w: workflow=%q entity_id=%q from=%q",
			ErrTerminalPhase, reg.workflow, entityID, from)
	}
	outEdges := reg.transitions[from]
	// Sorted terminals filtered to those reachable from current
	// phase. Deterministic + sorted means apps see the same
	// target across processes given the same table.
	terminals := reg.transitions.TerminalPhases()
	var reachable []string
	for _, terminal := range terminals {
		if slices.Contains(outEdges, terminal) {
			reachable = append(reachable, terminal)
		}
	}
	if len(reachable) == 0 {
		return fmt.Errorf("%w: workflow=%q entity_id=%q phase=%q has no edge to any terminal phase (declared terminals: %v)",
			ErrInvalidTransition, reg.workflow, entityID, from, terminals)
	}
	// When MORE than one terminal is reachable from the current
	// phase, Complete's sorted-pick is ambiguous-looking — apps
	// probably wanted semantic distinction (success vs failure vs
	// abort) and grabbed the convenience helper. Loud-warn so the
	// footgun is visible in operator logs without breaking the
	// caller. Single-reachable terminals are unambiguous and don't
	// warn.
	if len(reachable) > 1 {
		m.logger.Warn("lifecycle: Complete selection is ambiguous — multiple terminals reachable from current phase; consider Transition for explicit selection",
			slog.String("workflow", reg.workflow),
			slog.String("entity_id", entityID),
			slog.String("from", from),
			slog.String("picked", reachable[0]),
			slog.Any("alternatives", reachable[1:]))
	}
	return m.Transition(ctx, workflow, entityID, reachable[0], TransitionSourceFramework, "")
}

// Fail transitions the entity to a terminal phase, carrying the
// reason in the TransitionEvent.Note for audit. Selects the
// terminal phase by exact-name match against the reserved name
// "failed" if present; otherwise errors (apps that don't declare
// a "failed" phase must call Transition directly with their
// preferred error-state phase).
//
// The reason argument is required (empty string rejected) — a Fail
// with no reason defeats the audit purpose.
func (m *Manager) Fail(ctx context.Context, workflow, entityID, reason string) error {
	if reason == "" {
		return errors.New("lifecycle: Fail requires non-empty reason (operators need the failure cause in the audit trail)")
	}
	reg, err := m.lookupByWorkflow(workflow)
	if err != nil {
		return err
	}
	const failedPhase = "failed"
	if _, declared := reg.transitions[failedPhase]; !declared {
		return fmt.Errorf("%w: workflow %q does not declare a %q phase; call Transition with your preferred error-state phase instead",
			ErrInvalidTransition, reg.workflow, failedPhase)
	}
	// reason flows to Transition.note below; the slog audit inside
	// Transition picks it up. Full TransitionEvent persistence
	// (History) lands in the next commit on this branch.
	return m.Transition(ctx, workflow, entityID, failedPhase, TransitionSourceFramework, reason)
}

// setPhaseField writes newPhase into the Phase field on p via the
// cached structMeta's PhaseField metadata. Returns an error if the
// field index can't be resolved (shouldn't happen at runtime since
// parseStructTags validated PhaseField presence at Register time).
//
// Uses reflect.Value.FieldByIndex with the cached FieldIndex slice
// from parseStructTags — constant-time indexed access, not the
// linear FieldByName walk. The reflect cost per Transition is one
// indirect SetString, no map lookup.
func setPhaseField(p Participant, sm *structMeta, newPhase string) error {
	rv := reflect.ValueOf(p)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	field := rv.FieldByIndex(sm.PhaseField.FieldIndex)
	if !field.CanSet() {
		return fmt.Errorf("lifecycle: phase field %q is not settable (Participant must be a pointer to a struct with exported phase field)",
			sm.PhaseField.FieldName)
	}
	if field.Kind() != reflect.String {
		return fmt.Errorf("lifecycle: phase field %q has type %v, expected string",
			sm.PhaseField.FieldName, field.Kind())
	}
	field.SetString(newPhase)
	return nil
}

// keyForEntity returns the KV key for the given entity ID in the
// workflow's bucket. Pure dispatch — no allocation, no reflect.
//
// ADR-047's KVKey(entityID string) signature makes the key a pure
// function of entityID, so the cached keySample (one instance per
// registration, populated at Register time) can serve every call.
// The receiver state isn't consulted; only the entityID arg.
func keyForEntity(entityID string, reg *registration) string {
	return reg.keySample.KVKey(entityID)
}
