package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
)

// fakeEmitter is an in-memory graphEmitter the Manager unit tests
// drive against. It writes the merged entity state into the supplied
// fakeBucket so subsequent Manager reads see what was emitted —
// exercising the projection round-trip without NATS.
type fakeEmitter struct {
	mu       sync.Mutex
	bucket   *fakeBucket
	requests []*graph.UpdateEntityWithTriplesRequest
	deletes  []*graph.DeleteEntityRequest
	// deleteErr, when non-nil, makes delete() fail WITHOUT touching the
	// bucket — used to drive the DespawnWith partial-failure recovery path
	// (transition committed, delete failed, entity terminal-but-present).
	deleteErr error
	// createDegraded makes create() report a COMMITTED-but-unreadable write:
	// the mutation contract's Degraded shape. The bucket is still written, so
	// the state genuinely exists — only the response carries no Entity.
	createDegraded bool
	// createResponseMutator rewrites the Entity on the create RESPONSE without
	// touching what was stored, so a test can tell the causal response apart
	// from a later Get of the same entity.
	createResponseMutator func(*graph.EntityState)
	// createScript, when non-empty, drives create() one ATTEMPT AT A TIME:
	// each call consumes the next entry. It replaces the old createLosesReply
	// bool, which collapsed "committed" and "answered a failure" into a single
	// call and so could not represent — or falsify — a retry policy at all
	// (gh#861).
	createScript []createAttempt
	// createCalls counts every create() delivery, scripted or not, so a test
	// can pin that a NON-idempotent create was sent exactly once.
	createCalls int
	// forceRevisionMismatch makes update() fail CAS regardless of revision.
	forceRevisionMismatch bool
}

// createAttempt scripts ONE delivery of create(). The two fields are
// deliberately independent, because the failure this exists to model is
// precisely the one where they disagree: commit=true with a non-nil err is the
// LOST REPLY — the write landed durably and the answer never came back. A fake
// that can only return "committed and said so" or "did nothing and said so"
// cannot represent that state, and a test built on it proves nothing about how
// the caller handles it.
type createAttempt struct {
	// commit applies the write to the bucket before answering.
	commit bool
	// err is what this delivery answers. nil is a normal success.
	err error
	// onDeliver, when non-nil, runs when this attempt is consumed — used to
	// fail a test the moment a delivery happens that must never happen.
	onDeliver func()
}

// update mirrors graph-ingest's handleEntityUpdateWithTriples semantics:
// CAS-on-condition when ExpectedRevision > 0; otherwise must-exist
// (entity not found if absent); naive-append merge (NOT
// per-predicate-latest-wins — that's a read-time concern via
// extractTripleScalar). Per ADR-049 reviewer B1 the test substrate
// MUST match production or it masks bugs.
func (f *fakeEmitter) update(_ context.Context, req *graph.UpdateEntityWithTriplesRequest) (*graph.UpdateEntityWithTriplesResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
	currentRev := f.bucket.revOf(req.Entity.ID)
	if f.forceRevisionMismatch && f.bucket.exists(req.Entity.ID) {
		return nil, fmt.Errorf("%w: forced", errs.ErrRevisionMismatch)
	}
	if !f.bucket.exists(req.Entity.ID) {
		// ADR-060: production update() returns (nil, <error wrapping ErrEntityNotFound>).
		return nil, fmt.Errorf("%w: entity not found", ErrEntityNotFound)
	}
	if req.ExpectedRevision > 0 && req.ExpectedRevision != currentRev {
		// ADR-060: production update() now returns (nil, <classified error>)
		// matching errs.ErrRevisionMismatch on the CAS-conflict path.
		return nil, errs.ErrRevisionMismatch
	}
	current := f.bucket.get(req.Entity.ID)
	merged := current.Triples
	if len(req.RemoveTriples) > 0 {
		removeSet := map[string]struct{}{}
		for _, p := range req.RemoveTriples {
			removeSet[p] = struct{}{}
		}
		kept := make([]message.Triple, 0, len(merged))
		for _, t := range merged {
			if _, drop := removeSet[t.Predicate]; drop {
				continue
			}
			kept = append(kept, t)
		}
		merged = kept
	}
	// Replace-by-(subject,predicate) via MergeTriples — mirrors the
	// production graph-ingest update_with_triples add-leg (gh#244). Must
	// match the handler or this mock silently tests a different contract
	// than the wire it stands in for.
	merged = graph.MergeTriples(merged, req.AddTriples)
	state := *req.Entity
	state.Triples = merged
	f.bucket.put(req.Entity.ID, &state)
	return &graph.UpdateEntityWithTriplesResponse{
		MutationResponse: graph.MutationResponse{KVRevision: f.bucket.revOf(req.Entity.ID)},
		Entity:           &state,
	}, nil
}

// create mirrors graph-ingest's handleEntityCreateWithTriples — atomic
// create-or-fail. Returns ErrAlreadyExists when the entity is already
// present (the per-entity CAS race surface for fresh-create per
// ADR-049 reviewer B2).
func (f *fakeEmitter) create(_ context.Context, req *graph.CreateEntityWithTriplesRequest) (*graph.CreateEntityWithTriplesResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createCalls++
	// A scripted attempt outranks the default behaviour: it is how a test says
	// "this delivery committed and then lost its reply", which no combination of
	// the flags below can express.
	if len(f.createScript) > 0 {
		attempt := f.createScript[0]
		f.createScript = f.createScript[1:]
		if attempt.onDeliver != nil {
			attempt.onDeliver()
		}
		if attempt.commit && !f.bucket.exists(req.Entity.ID) {
			state := *req.Entity
			state.Triples = req.Triples
			f.bucket.put(req.Entity.ID, &state)
		}
		if attempt.err != nil {
			return nil, attempt.err
		}
		stored := f.bucket.get(req.Entity.ID)
		return &graph.CreateEntityWithTriplesResponse{
			MutationResponse: graph.MutationResponse{KVRevision: f.bucket.revOf(req.Entity.ID)},
			Entity:           stored,
			TriplesAdded:     len(req.Triples),
		}, nil
	}
	if f.bucket.exists(req.Entity.ID) {
		// ADR-060: production create() returns (nil, <error wrapping ErrAlreadyExists>).
		return nil, fmt.Errorf("%w: entity already exists", ErrAlreadyExists)
	}
	state := *req.Entity
	state.Triples = req.Triples
	f.bucket.put(req.Entity.ID, &state)
	if f.createDegraded {
		return &graph.CreateEntityWithTriplesResponse{
			MutationResponse: graph.MutationResponse{Degraded: true, DegradedReason: "read-back unavailable"},
			TriplesAdded:     len(req.Triples),
		}, nil
	}
	respState := state
	if f.createResponseMutator != nil {
		respState.Triples = append([]message.Triple(nil), state.Triples...)
		f.createResponseMutator(&respState)
	}
	return &graph.CreateEntityWithTriplesResponse{
		MutationResponse: graph.MutationResponse{KVRevision: f.bucket.revOf(req.Entity.ID)},
		Entity:           &respState,
		TriplesAdded:     len(req.Triples),
	}, nil
}

// delete mirrors graph-ingest's handleEntityDelete — idempotent
// reclaim. Removes the entity from the bucket and reports whether it
// existed (DeleteEntityResponse.Deleted); an already-absent entity is a
// no-op success, matching the production handler.
func (f *fakeEmitter) delete(_ context.Context, req *graph.DeleteEntityRequest) (*graph.DeleteEntityResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletes = append(f.deletes, req)
	if f.deleteErr != nil {
		return nil, f.deleteErr
	}
	existed := f.bucket.remove(req.EntityID)
	return &graph.DeleteEntityResponse{Deleted: existed}, nil
}

// fakeBucket is the minimal jetstream.KeyValue surface Manager.getEntity
// + manager_query.go exercise. We implement only the methods the
// Manager calls; the rest panic if invoked.
type fakeBucket struct {
	mu              sync.Mutex
	entries         map[string]*fakeBucketEntry
	raw             map[string][]byte
	history         map[string][]jetstream.KeyValueEntry
	nextRev         uint64
	watchFactory    func(string) (jetstream.KeyWatcher, error)
	watchAllFactory func() (jetstream.KeyWatcher, error)
}

type fakeBucketEntry struct {
	state     *graph.EntityState
	revision  uint64
	createdAt time.Time
}

func newFakeBucket() *fakeBucket {
	return &fakeBucket{
		entries: map[string]*fakeBucketEntry{},
		raw:     map[string][]byte{},
		history: map[string][]jetstream.KeyValueEntry{},
	}
}

func (b *fakeBucket) exists(id string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.entries[id]
	return ok
}

func (b *fakeBucket) get(id string) *graph.EntityState {
	b.mu.Lock()
	defer b.mu.Unlock()
	if e, ok := b.entries[id]; ok {
		clone := *e.state
		return &clone
	}
	return &graph.EntityState{ID: id}
}

func (b *fakeBucket) revOf(id string) uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	if e, ok := b.entries[id]; ok {
		return e.revision
	}
	return 0
}

func (b *fakeBucket) put(id string, state *graph.EntityState) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextRev++
	b.entries[id] = &fakeBucketEntry{state: state, revision: b.nextRev, createdAt: time.Now()}
}

// remove deletes an entry and reports whether it existed (mirrors the
// graph-ingest delete handler's Deleted flag).
func (b *fakeBucket) remove(id string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, existed := b.entries[id]
	delete(b.entries, id)
	return existed
}

// jetstream.KeyValue minimum surface — embedding via composition is
// painful, so we implement only what Manager uses.

func (b *fakeBucket) Get(_ context.Context, key string) (jetstream.KeyValueEntry, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if data, ok := b.raw[key]; ok {
		return &fakeKVEntry{key: key, value: data, revision: 1, created: time.Now()}, nil
	}
	e, ok := b.entries[key]
	if !ok {
		return nil, jetstream.ErrKeyNotFound
	}
	data, _ := json.Marshal(e.state)
	return &fakeKVEntry{key: key, value: data, revision: e.revision, created: e.createdAt}, nil
}

// fakeKVEntry is the bare-minimum jetstream.KeyValueEntry implementation.
type fakeKVEntry struct {
	key      string
	value    []byte
	revision uint64
	created  time.Time
}

func (e *fakeKVEntry) Bucket() string                  { return "ENTITY_STATES" }
func (e *fakeKVEntry) Key() string                     { return e.key }
func (e *fakeKVEntry) Value() []byte                   { return e.value }
func (e *fakeKVEntry) Revision() uint64                { return e.revision }
func (e *fakeKVEntry) Created() time.Time              { return e.created }
func (e *fakeKVEntry) Delta() uint64                   { return 0 }
func (e *fakeKVEntry) Operation() jetstream.KeyValueOp { return jetstream.KeyValuePut }

// jetstream.KeyValue methods Manager doesn't call in this test —
// panic so a future change that adds a call surfaces here.
func (b *fakeBucket) PutString(context.Context, string, string) (uint64, error) {
	panic("fakeBucket.PutString not implemented")
}
func (b *fakeBucket) Put(context.Context, string, []byte) (uint64, error) {
	panic("fakeBucket.Put not implemented")
}
func (b *fakeBucket) Create(context.Context, string, []byte, ...jetstream.KVCreateOpt) (uint64, error) {
	panic("fakeBucket.Create not implemented")
}
func (b *fakeBucket) Update(context.Context, string, []byte, uint64) (uint64, error) {
	panic("fakeBucket.Update not implemented")
}
func (b *fakeBucket) Delete(context.Context, string, ...jetstream.KVDeleteOpt) error {
	panic("fakeBucket.Delete not implemented")
}
func (b *fakeBucket) Purge(context.Context, string, ...jetstream.KVDeleteOpt) error {
	panic("fakeBucket.Purge not implemented")
}
func (b *fakeBucket) Watch(_ context.Context, pattern string, _ ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	if b.watchFactory == nil {
		panic("fakeBucket.Watch not implemented")
	}
	return b.watchFactory(pattern)
}
func (b *fakeBucket) WatchAll(context.Context, ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	if b.watchAllFactory == nil {
		panic("fakeBucket.WatchAll not implemented")
	}
	return b.watchAllFactory()
}
func (b *fakeBucket) WatchFiltered(context.Context, []string, ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	panic("fakeBucket.WatchFiltered not implemented")
}
func (b *fakeBucket) Keys(context.Context, ...jetstream.WatchOpt) ([]string, error) {
	panic("fakeBucket.Keys not implemented")
}
func (b *fakeBucket) ListKeys(context.Context, ...jetstream.WatchOpt) (jetstream.KeyLister, error) {
	panic("fakeBucket.ListKeys not implemented")
}
func (b *fakeBucket) ListKeysFiltered(context.Context, ...string) (jetstream.KeyLister, error) {
	panic("fakeBucket.ListKeysFiltered not implemented")
}
func (b *fakeBucket) History(_ context.Context, key string, _ ...jetstream.WatchOpt) ([]jetstream.KeyValueEntry, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	entries, ok := b.history[key]
	if !ok {
		return nil, jetstream.ErrKeyNotFound
	}
	return append([]jetstream.KeyValueEntry(nil), entries...), nil
}
func (b *fakeBucket) Bucket() string { return "ENTITY_STATES" }
func (b *fakeBucket) PurgeDeletes(context.Context, ...jetstream.KVPurgeOpt) error {
	panic("fakeBucket.PurgeDeletes not implemented")
}
func (b *fakeBucket) Status(context.Context) (jetstream.KeyValueStatus, error) {
	panic("fakeBucket.Status not implemented")
}
func (b *fakeBucket) GetRevision(_ context.Context, _ string, _ uint64) (jetstream.KeyValueEntry, error) {
	panic("fakeBucket.GetRevision not implemented")
}

// --- tests ---

func newTestManager(t *testing.T) (*Manager, *fakeEmitter, *fakeBucket) {
	t.Helper()
	bucket := newFakeBucket()
	emitter := &fakeEmitter{bucket: bucket}
	mgr := newManagerForTest(nil, emitter, bucket)
	wf := lifecycle{}.fixtureWorkflow()
	if err := mgr.Register(wf); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return mgr, emitter, bucket
}

type lifecycle struct{}

func (lifecycle) fixtureWorkflow() Workflow {
	return Workflow{
		Name:            "fixture",
		EntityIDPattern: "*.*.lifecycle.gcs.mission.*",
		Phases:          []string{"planning", "flying", "completed", "aborted", "failed"},
		Transitions: Transitions{
			"planning":  {"flying", "aborted"},
			"flying":    {"completed", "aborted"},
			"completed": {},
			"aborted":   {},
			"failed":    {},
		},
		PhasePredicate: "mission.lifecycle.phase",
		Schema:         reflect.TypeOf(fixtureMission{}),
		OperatorWritablePredicates: []string{
			"mission.identity.owner-org-id",
			"mission.annotation.note",
		},
		AuditPredicates: AuditSpec{ // predicate-audit:unrelated {"column":20,"surface":"go-field:AuditPredicates","value":"","basis":"reviewed:predicate-container-values-audited"}
			Source: "mission.transition.source",
			At:     "mission.transition.at",
			From:   "mission.transition.from",
			Note:   "mission.transition.note",
		},
	}
}

func TestManager_RoundTripCreateGetTransition(t *testing.T) {
	t.Parallel()
	mgr, _, _ := newTestManager(t)
	ctx := context.Background()

	id := "c360.platform1.lifecycle.gcs.mission.001"
	if err := mgr.Create(ctx, &fixtureMission{ID: id, PhaseF: "planning", OwnerOrgID: "acme"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := mgr.Get(ctx, "fixture", id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	gotMission := got.(*fixtureMission)
	if gotMission.PhaseF != "planning" {
		t.Errorf("Phase=%q, want planning", gotMission.PhaseF)
	}
	if gotMission.OwnerOrgID != "acme" {
		t.Errorf("OwnerOrgID=%q, want acme", gotMission.OwnerOrgID)
	}

	if err := mgr.Transition(ctx, "fixture", id, "flying", TransitionSourceRule, "launched"); err != nil {
		t.Fatalf("Transition: %v", err)
	}
	got2, err := mgr.Get(ctx, "fixture", id)
	if err != nil {
		t.Fatalf("Get after transition: %v", err)
	}
	if got2.Phase() != "flying" {
		t.Errorf("after Transition: phase=%q, want flying", got2.Phase())
	}
}

func TestManager_TransitionRejectsInvalidEdge(t *testing.T) {
	t.Parallel()
	mgr, _, _ := newTestManager(t)
	ctx := context.Background()
	id := "c360.platform1.lifecycle.gcs.mission.t1"
	if err := mgr.Create(ctx, &fixtureMission{ID: id, PhaseF: "planning"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	err := mgr.Transition(ctx, "fixture", id, "completed", TransitionSourceRule, "")
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition, got %v", err)
	}
}

func TestManager_CreateOnExistingPhaseTripleErrAlreadyExists(t *testing.T) {
	t.Parallel()
	mgr, _, _ := newTestManager(t)
	ctx := context.Background()
	id := "c360.platform1.lifecycle.gcs.mission.dup"
	if err := mgr.Create(ctx, &fixtureMission{ID: id, PhaseF: "planning"}); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	err := mgr.Create(ctx, &fixtureMission{ID: id, PhaseF: "planning"})
	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists, got %v", err)
	}
}

func TestManager_UpdateFromOperatorPatchesPredicate(t *testing.T) {
	t.Parallel()
	mgr, _, _ := newTestManager(t)
	ctx := context.Background()
	id := "c360.platform1.lifecycle.gcs.mission.op"
	if err := mgr.Create(ctx, &fixtureMission{ID: id, PhaseF: "planning"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := mgr.UpdateFromOperator(ctx, "fixture", id, map[string]any{
		"note": "operator-set",
	}); err != nil {
		t.Fatalf("UpdateFromOperator: %v", err)
	}
	got, err := mgr.Get(ctx, "fixture", id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.(*fixtureMission).Note != "operator-set" {
		t.Errorf("Note=%q, want operator-set", got.(*fixtureMission).Note)
	}
}

func TestManager_UpdateFromOperatorRejectsProtectedField(t *testing.T) {
	t.Parallel()
	mgr, _, _ := newTestManager(t)
	ctx := context.Background()
	id := "c360.platform1.lifecycle.gcs.mission.prot"
	if err := mgr.Create(ctx, &fixtureMission{ID: id, PhaseF: "planning"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	err := mgr.UpdateFromOperator(ctx, "fixture", id, map[string]any{"phase": "flying"})
	if !errors.Is(err, ErrFieldNotOperatorWritable) {
		t.Fatalf("expected ErrFieldNotOperatorWritable, got %v", err)
	}
}

func TestManager_GetReturnsNotLifecycleManagedForRawEntity(t *testing.T) {
	t.Parallel()
	mgr, _, bucket := newTestManager(t)
	ctx := context.Background()
	id := "c360.platform1.lifecycle.gcs.mission.raw"
	// Seed entity directly into the bucket WITHOUT a phase triple —
	// simulates a processor stamping `mission.control.command` before any
	// lifecycle action fires.
	bucket.put(id, &graph.EntityState{
		ID: id,
		Triples: []message.Triple{
			{Subject: id, Predicate: "mission.control.command", Object: "launch"},
		},
	})
	_, err := mgr.Get(ctx, "fixture", id)
	if !errors.Is(err, ErrEntityNotLifecycleManaged) {
		t.Fatalf("expected ErrEntityNotLifecycleManaged, got %v", err)
	}
	// After Create the entity coexists with its prior triple.
	if err := mgr.Create(ctx, &fixtureMission{ID: id, PhaseF: "planning"}); err != nil {
		t.Fatalf("Create after raw seed: %v", err)
	}
	got, err := mgr.Get(ctx, "fixture", id)
	if err != nil {
		t.Fatalf("Get after Create: %v", err)
	}
	if got.Phase() != "planning" {
		t.Errorf("phase wrong: %v", got.Phase())
	}
}

func TestManagerGetRejectsPredicatePoisonWithoutProjection(t *testing.T) {
	t.Parallel()

	mgr, _, bucket := newTestManager(t)
	entityID := "c360.platform1.lifecycle.gcs.mission.poisoned"
	bucket.put(entityID, &graph.EntityState{
		ID: entityID,
		Triples: []message.Triple{
			{Subject: entityID, Predicate: "mission.lifecycle.phase", Object: "planning"},
			{Subject: entityID, Predicate: "legacy.predicate", Object: "old"}, // predicate-audit:invalid {"kind":"stored-predicate","value":"legacy.predicate","reason":"arity"}
		},
	})

	participant, err := mgr.Get(context.Background(), "fixture", entityID)
	if err == nil {
		t.Fatal("Get error = nil, want graph-state poison")
	}
	if participant != nil {
		t.Fatalf("Get returned projected participant %#v from poisoned state", participant)
	}
	var contractErr *graph.StateContractError
	if !errors.As(err, &contractErr) {
		t.Fatalf("Get error = %T %v, want StateContractError", err, err)
	}
	if contractErr.Reason != graph.GraphStateReasonNoncanonicalPredicate {
		t.Fatalf("contract reason = %q, want %q", contractErr.Reason, graph.GraphStateReasonNoncanonicalPredicate)
	}
}

func TestManager_LookupByEntityIDMatchesPattern(t *testing.T) {
	t.Parallel()
	mgr, _, _ := newTestManager(t)
	ctx := context.Background()
	id := "c360.platform1.lifecycle.gcs.mission.lkup"
	if err := mgr.Create(ctx, &fixtureMission{ID: id, PhaseF: "planning"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := mgr.LookupByEntityID(ctx, id)
	if err != nil {
		t.Fatalf("LookupByEntityID: %v", err)
	}
	if got.Workflow() != "fixture" {
		t.Errorf("workflow=%q, want fixture", got.Workflow())
	}
}

func TestManager_GetRawReturnsAllTriples(t *testing.T) {
	t.Parallel()
	mgr, _, bucket := newTestManager(t)
	ctx := context.Background()
	id := "c360.platform1.lifecycle.gcs.mission.raw2"
	bucket.put(id, &graph.EntityState{
		ID: id,
		Triples: []message.Triple{
			{Subject: id, Predicate: "x.y.z", Object: "literal"},
		},
	})
	state, err := mgr.GetRaw(ctx, id)
	if err != nil {
		t.Fatalf("GetRaw: %v", err)
	}
	if len(state.Triples) != 1 || state.Triples[0].Predicate != "x.y.z" {
		t.Errorf("unexpected raw triples: %+v", state.Triples)
	}
}

// TestManager_TransitionReplacesPhaseTripleNotAppend guards the
// replace-not-append invariant: Transition must REMOVE the prior phase
// triple, leaving exactly one. The earlier naive-append behavior left
// [planning, flying, completed]; extractTripleScalar (last-match) kept
// Manager.Get correct, but the rule engine's GetFieldValue reads
// FIRST-match → it saw the stale "planning" and phase guards never
// re-fired (semteams autoresearch 4a). With the fix, both read paths
// see the same single value. Single-valued for every predicate
// Transition writes (phase + audit), not just phase.
func TestManager_TransitionReplacesPhaseTripleNotAppend(t *testing.T) {
	t.Parallel()
	mgr, _, bucket := newTestManager(t)
	ctx := context.Background()
	id := "c360.platform1.lifecycle.gcs.mission.accum"

	if err := mgr.Create(ctx, &fixtureMission{ID: id, PhaseF: "planning"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := mgr.Transition(ctx, "fixture", id, "flying", TransitionSourceRule, "go"); err != nil {
		t.Fatalf("Transition 1: %v", err)
	}
	if err := mgr.Transition(ctx, "fixture", id, "completed", TransitionSourceRule, "ok"); err != nil {
		t.Fatalf("Transition 2: %v", err)
	}

	stored := bucket.get(id)
	var phaseTriples []message.Triple
	for _, tr := range stored.Triples {
		if tr.Predicate == "mission.lifecycle.phase" {
			phaseTriples = append(phaseTriples, tr)
		}
	}
	if len(phaseTriples) != 1 {
		t.Fatalf("expected exactly 1 mission.lifecycle.phase triple after Create+2 Transitions (replace, not append), got %d: %+v", len(phaseTriples), phaseTriples)
	}
	// The single triple must be the latest phase — so first-match
	// (rule engine) and last-match (Manager) now agree.
	if got, _ := phaseTriples[0].Object.(string); got != "completed" {
		t.Errorf("phase triple object = %q, want completed (both read paths must agree)", got)
	}
	got, err := mgr.Get(ctx, "fixture", id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Phase() != "completed" {
		t.Errorf("projected Phase=%q, want completed", got.Phase())
	}
}

// TestManager_ConcurrentCreateOnlyOneWins exercises B2 — atomic
// create-or-fail. With ExpectedRevision=0's prior broken semantics
// two concurrent Creates would silently both succeed and double-stamp
// the phase triple.
func TestManager_ConcurrentCreateOnlyOneWins(t *testing.T) {
	t.Parallel()
	mgr, _, _ := newTestManager(t)
	ctx := context.Background()
	id := "c360.platform1.lifecycle.gcs.mission.race"

	const N = 8
	var wg sync.WaitGroup
	wg.Add(N)
	errs := make(chan error, N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			errs <- mgr.Create(ctx, &fixtureMission{ID: id, PhaseF: "planning"})
		}()
	}
	wg.Wait()
	close(errs)
	successes := 0
	alreadyExists := 0
	other := 0
	for err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrAlreadyExists):
			alreadyExists++
		default:
			other++
		}
	}
	if successes != 1 {
		t.Errorf("expected exactly 1 successful concurrent Create, got %d (already_exists=%d, other=%d)",
			successes, alreadyExists, other)
	}
	if other != 0 {
		t.Errorf("expected losers to surface ErrAlreadyExists, got %d unclassified errors", other)
	}
}

// TestWorkflowValidateRejectsNon6SegmentPattern pins B5 — workflows
// declaring a 5-segment EntityIDPattern fail at Register time, not at
// first List call with no matches.
func TestWorkflowValidateRejectsNon6SegmentPattern(t *testing.T) {
	t.Parallel()
	bad := Workflow{
		Name:            "bad",
		EntityIDPattern: "*.lifecycle.gcs.mission.*", // entity-id-audit:classify intentional-malformed "*.lifecycle.gcs.mission.*" line=699 column=20 surface=go-field:Workflow.EntityIDPattern entity_id_pattern_invalid:arity five segment rejection fixture
		Transitions:     Transitions{"planning": {}},
		PhasePredicate:  "workflow.lifecycle.phase",
		Schema:          reflect.TypeOf(fixtureMission{}),
	}
	err := bad.validate()
	if err == nil {
		t.Fatal("expected validate to reject 5-segment EntityIDPattern, got nil")
	}
}

func TestWorkflowValidateRejectsNoncanonicalDeclaredPredicate(t *testing.T) {
	t.Parallel()
	bad := lifecycle{}.fixtureWorkflow()
	bad.ReferencePredicates = []ReferenceSpec{{Predicate: "mission.assigned_drone"}} // predicate-audit:unrelated {"column":28,"surface":"go-assignment:ReferencePredicates","value":"","basis":"reviewed:predicate-container-values-audited"}
	// predicate-audit:invalid {"location":"line:638:column:56","kind":"stored-predicate","value":"mission.assigned_drone","reason":"arity"}

	err := bad.validate()
	if err == nil {
		t.Fatal("expected validate to reject noncanonical reference predicate, got nil")
	}
	if !errors.Is(err, ErrInvalidWorkflow) {
		t.Fatalf("expected ErrInvalidWorkflow, got %v", err)
	}
}

// TestWorkflow_ValidateDisjointness pins gh#234 — a single-valued predicate
// (phase, audit, or scalar projection field) that collides with a
// cardinality-many predicate (child-link or reference) is rejected at Register,
// since Manager.Transition's RemoveTriples would otherwise delete the
// many-valued triples. The reference-field-matching-its-own-ReferenceSpec case
// guards against a false positive.
func TestWorkflow_ValidateDisjointness(t *testing.T) {
	t.Parallel()
	base := lifecycle{}.fixtureWorkflow()
	meta, err := parseSchemaType(base.Schema)
	if err != nil {
		t.Fatalf("parseSchemaType: %v", err)
	}

	tests := []struct {
		name    string
		mutate  func(w *Workflow)
		wantErr bool
	}{
		{"valid fixture (no many-valued predicates)", func(_ *Workflow) {}, false},
		{"child-link collides with phase predicate", func(w *Workflow) {
			w.ChildWorkflows = []ChildSpec{{Workflow: "child", LinkPredicate: "mission.lifecycle.phase"}}
		}, true},
		{"child-link collides with an audit predicate", func(w *Workflow) {
			w.ChildWorkflows = []ChildSpec{{Workflow: "child", LinkPredicate: "mission.transition.at"}}
		}, true},
		{"reference collides with a scalar projection field", func(w *Workflow) {
			w.ReferencePredicates = []ReferenceSpec{{Predicate: "mission.identity.owner-org-id"}} // predicate-audit:unrelated {"column":28,"surface":"go-assignment:ReferencePredicates","value":"","basis":"reviewed:predicate-container-values-audited"}
		}, true},
		{"reference field matching its own ReferenceSpec is valid", func(w *Workflow) {
			w.ReferencePredicates = []ReferenceSpec{{Predicate: "mission.assignment.drone"}} // predicate-audit:unrelated {"column":28,"surface":"go-assignment:ReferencePredicates","value":"","basis":"reviewed:predicate-container-values-audited"}
		}, false},
		{"distinct child-link is fine", func(w *Workflow) {
			w.ChildWorkflows = []ChildSpec{{Workflow: "child", LinkPredicate: "mission.child.subtask"}}
		}, false},
		{"predicate declared as both child-link and reference", func(w *Workflow) {
			w.ChildWorkflows = []ChildSpec{{Workflow: "child", LinkPredicate: "mission.child.subtask"}}
			w.ReferencePredicates = []ReferenceSpec{{Predicate: "mission.child.subtask"}} // predicate-audit:unrelated {"column":28,"surface":"go-assignment:ReferencePredicates","value":"","basis":"reviewed:predicate-container-values-audited"}
		}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := base
			tt.mutate(&w)
			err := w.validateDisjointness(meta)
			switch {
			case tt.wantErr && err == nil:
				t.Fatal("expected a disjointness error, got nil")
			case tt.wantErr && !errors.Is(err, ErrInvalidWorkflow):
				t.Fatalf("expected ErrInvalidWorkflow, got %v", err)
			case !tt.wantErr && err != nil:
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}

// TestManager_DiffSkipsZeroValueOnMissingPredicate pins B4 — a
// TransitionWith mutator that touches a zero-value field should not
// emit a spurious delta against a missing-predicate baseline.
func TestManager_DiffSkipsZeroValueOnMissingPredicate(t *testing.T) {
	t.Parallel()
	mgr, emitter, _ := newTestManager(t)
	ctx := context.Background()
	id := "c360.platform1.lifecycle.gcs.mission.zerodiff"

	if err := mgr.Create(ctx, &fixtureMission{ID: id, PhaseF: "planning"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	preCount := len(emitter.requests)
	// Mutator that touches no fields — should not emit non-phase deltas.
	if err := mgr.TransitionWith(ctx, "fixture", id, "flying", TransitionSourceRule, "", func(_ Participant) error {
		return nil
	}); err != nil {
		t.Fatalf("TransitionWith: %v", err)
	}
	if len(emitter.requests) != preCount+1 {
		t.Fatalf("expected exactly 1 emit, got %d", len(emitter.requests)-preCount)
	}
	last := emitter.requests[len(emitter.requests)-1]
	// Count predicate emissions: phase + 4 audit fields = 5 expected.
	// If diff is spurious on zero-value Note field, we'd see > 5.
	gotPreds := map[string]bool{}
	for _, tr := range last.AddTriples {
		gotPreds[tr.Predicate] = true
	}
	if _, ok := gotPreds["mission.identity.owner-org-id"]; ok {
		t.Error("zero-value OwnerOrgID should not emit a delta against missing predicate baseline")
	}
}

// --- Despawn / DespawnWith (gh#497) ---

func TestManager_Despawn_ReclaimsAndIsIdempotent(t *testing.T) {
	t.Parallel()
	mgr, emitter, bucket := newTestManager(t)
	ctx := context.Background()
	id := "c360.platform1.lifecycle.gcs.mission.dsp1"
	if err := mgr.Create(ctx, &fixtureMission{ID: id, PhaseF: "planning"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !bucket.exists(id) {
		t.Fatalf("precondition: entity should exist after Create")
	}
	if err := mgr.Despawn(ctx, "fixture", id); err != nil {
		t.Fatalf("Despawn: %v", err)
	}
	if bucket.exists(id) {
		t.Errorf("entity should be gone from ENTITY_STATES after Despawn")
	}
	if len(emitter.deletes) != 1 || emitter.deletes[0].EntityID != id {
		t.Errorf("expected exactly 1 delete for %q, got %+v", id, emitter.deletes)
	}
	// Idempotent: despawning an already-absent entity succeeds.
	if err := mgr.Despawn(ctx, "fixture", id); err != nil {
		t.Errorf("Despawn on absent entity should be idempotent success, got %v", err)
	}
}

func TestManager_Despawn_RejectsUnregisteredWorkflow(t *testing.T) {
	t.Parallel()
	mgr, emitter, _ := newTestManager(t)
	err := mgr.Despawn(context.Background(), "nope", "c360.platform1.lifecycle.gcs.mission.x")
	if !errors.Is(err, ErrWorkflowNotRegistered) {
		t.Errorf("want ErrWorkflowNotRegistered, got %v", err)
	}
	if len(emitter.deletes) != 0 {
		t.Errorf("no delete should be emitted for an unregistered workflow, got %d", len(emitter.deletes))
	}
}

func TestManager_Despawn_RejectsPatternMismatch(t *testing.T) {
	t.Parallel()
	mgr, emitter, _ := newTestManager(t)
	// Registered workflow, but the id does not match its EntityIDPattern.
	err := mgr.Despawn(context.Background(), "fixture", "c360.platform1.other.gcs.sensor.9")
	if !errors.Is(err, ErrEntityIDPatternMismatch) {
		t.Errorf("want ErrEntityIDPatternMismatch, got %v", err)
	}
	if len(emitter.deletes) != 0 {
		t.Errorf("no delete should be emitted on pattern mismatch, got %d", len(emitter.deletes))
	}
}

func TestManager_DespawnWith_TransitionsThenReclaims(t *testing.T) {
	t.Parallel()
	mgr, emitter, bucket := newTestManager(t)
	ctx := context.Background()
	id := "c360.platform1.lifecycle.gcs.mission.dsp2"
	if err := mgr.Create(ctx, &fixtureMission{ID: id, PhaseF: "planning"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// planning's only reachable terminal is "aborted" (planning→aborted edge).
	if err := mgr.DespawnWith(ctx, "fixture", id, TransitionSourceRule, "predator-cull"); err != nil {
		t.Fatalf("DespawnWith: %v", err)
	}
	if bucket.exists(id) {
		t.Errorf("entity should be gone after DespawnWith")
	}
	sawTerminal := false
	for _, r := range emitter.requests {
		for _, tr := range r.AddTriples {
			if tr.Predicate == "mission.lifecycle.phase" && tr.Object == "aborted" {
				sawTerminal = true
			}
		}
	}
	if !sawTerminal {
		t.Errorf("DespawnWith should transition to terminal 'aborted' before delete; emits=%+v", emitter.requests)
	}
	if len(emitter.deletes) != 1 || emitter.deletes[0].EntityID != id {
		t.Errorf("expected exactly 1 delete for %q, got %+v", id, emitter.deletes)
	}
}

func TestManager_DespawnWith_PartialFailureRecoverableViaDespawn(t *testing.T) {
	t.Parallel()
	mgr, emitter, bucket := newTestManager(t)
	ctx := context.Background()
	id := "c360.platform1.lifecycle.gcs.mission.dsp3"
	if err := mgr.Create(ctx, &fixtureMission{ID: id, PhaseF: "planning"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// The delete leg fails: the terminal transition commits but the entity is
	// left terminal-but-present (the documented non-atomic partial failure).
	emitter.deleteErr = errors.New("simulated delete failure")
	if err := mgr.DespawnWith(ctx, "fixture", id, TransitionSourceRule, "cull"); err == nil {
		t.Fatal("DespawnWith should surface the delete failure")
	}
	if !bucket.exists(id) {
		t.Fatalf("after a failed delete, entity should still be present (terminal-but-present)")
	}
	// Recovery: a subsequent Despawn (delete now succeeds) reclaims it.
	emitter.deleteErr = nil
	if err := mgr.Despawn(ctx, "fixture", id); err != nil {
		t.Fatalf("recovery Despawn: %v", err)
	}
	if bucket.exists(id) {
		t.Errorf("entity should be reclaimed after recovery Despawn")
	}
}

// TestCreate_RefusesEntityIDOutsideTheWorkflowPattern pins gh#814's first
// blocking finding, measured against real NATS during review: an out-of-pattern
// create COMMITS, returns success, is readable by Get — and is invisible to
// List and Watch (both filter by the pattern) and unreclaimable by Despawn
// (which refuses a non-matching ID). The surface reports a birth it cannot then
// discover or remove.
//
// Owner-lease enforcement does not cover this: an out-of-pattern write is
// UNCLAIMED rather than stale, so the lease check passes it through.
func TestCreate_RefusesEntityIDOutsideTheWorkflowPattern(t *testing.T) {
	mgr, emitter, _ := newTestManager(t)
	ctx := context.Background()

	const outside = "evil.corp.other.system.type.pwned"
	err := mgr.Create(ctx, &fixtureMission{ID: outside, PhaseF: "planning"})
	if !errors.Is(err, ErrEntityIDPatternMismatch) {
		t.Fatalf("Create out-of-pattern err = %v, want ErrEntityIDPatternMismatch", err)
	}
	if n := len(emitter.requests); n != 0 {
		t.Errorf("emitter saw %d write requests — the refusal must land before any write", n)
	}
	if _, getErr := mgr.Get(ctx, "fixture", outside); !errors.Is(getErr, ErrEntityNotFound) {
		t.Errorf("Get after refused create = %v, want ErrEntityNotFound — a refused create must leave nothing behind", getErr)
	}
}

// TestCreate_AcceptsEntityIDInsideThePattern is the other direction: the gate
// must not refuse a legitimate ID. Without this the previous test is satisfied
// by a Create that refuses everything.
func TestCreate_AcceptsEntityIDInsideThePattern(t *testing.T) {
	mgr, _, _ := newTestManager(t)
	ctx := context.Background()

	const inside = "c360.platform1.lifecycle.gcs.mission.ok"
	if err := mgr.Create(ctx, &fixtureMission{ID: inside, PhaseF: "planning"}); err != nil {
		t.Fatalf("Create in-pattern: %v", err)
	}
	if _, err := mgr.Get(ctx, "fixture", inside); err != nil {
		t.Fatalf("Get after in-pattern create: %v", err)
	}
}

// TestMustExistLanes_DoNotAutoVivify pins the no-auto-vivify contract at the
// PRODUCTION seam. The gateway test that carried this claim exercised a
// hand-written fake and could not observe the real Manager at all.
//
// MEASURED, and stated precisely because the obvious claim would be wrong:
// disabling BOTH guards in Manager.UpdateFromOperator does NOT flip this test.
// That is not a hole — the emit layer enforces must-exist independently
// (graph-ingest's update handler, mirrored by the fake emitter), so the
// invariant survives the Manager's guards being removed. Review reported the
// surviving mutation as "nothing guards the invariant"; the sharper reading is
// that the Manager's checks are a second line that buys a clearer error, and the
// contract itself is held one layer down.
//
// So this test deliberately pins the OBSERVABLE contract — an absent entity
// yields ErrEntityNotFound and nothing is created — rather than any one layer's
// check. Verified falsifiable: relaxing the Manager guards AND the emit layer's
// must-exist arm together does flip the state-patch subtest.
//
// Two scope caveats, because the reasoning above is narrower than it looks. The
// layer that holds it here is the FAKE emitter, which is documented to mirror
// graph-ingest's handler — the claim generalizes only as far as that mirror
// holds. And the TRANSITION subtest is not covered by this reasoning at all:
// TransitionWith carries its own independent guards, so it survives both
// relaxations for a different reason.
func TestMustExistLanes_DoNotAutoVivify(t *testing.T) {
	ctx := context.Background()
	const ghost = "c360.platform1.lifecycle.gcs.mission.ghost"

	t.Run("state patch", func(t *testing.T) {
		mgr, _, _ := newTestManager(t)
		err := mgr.UpdateFromOperator(ctx, "fixture", ghost, map[string]any{"owner_org_id": "acme"})
		if !errors.Is(err, ErrEntityNotFound) {
			t.Fatalf("UpdateFromOperator on an absent entity = %v, want ErrEntityNotFound", err)
		}
		if _, getErr := mgr.Get(ctx, "fixture", ghost); !errors.Is(getErr, ErrEntityNotFound) {
			t.Error("the patch created the instance — only the create lane may")
		}
	})

	t.Run("transition", func(t *testing.T) {
		mgr, _, _ := newTestManager(t)
		err := mgr.Transition(ctx, "fixture", ghost, "flying", TransitionSourceOperator, "")
		if !errors.Is(err, ErrEntityNotFound) {
			t.Fatalf("Transition on an absent entity = %v, want ErrEntityNotFound", err)
		}
		if _, getErr := mgr.Get(ctx, "fixture", ghost); !errors.Is(getErr, ErrEntityNotFound) {
			t.Error("the transition created the instance — only the create lane may")
		}
	})
}

// TestRegister_RejectsSchemaThatIsNotAParticipant closes an unchecked assertion
// that panics on the FIRST request to the create lane. Every projection path
// does reflect.New(Schema).(Participant); the read paths reach it only for an
// entity that already exists, so on a fresh volume they return not-found first.
// The create lane reaches it with no precondition at all.
func TestRegister_RejectsSchemaThatIsNotAParticipant(t *testing.T) {
	mgr := newManagerForTest(nil, &fakeEmitter{bucket: newFakeBucket()}, newFakeBucket())
	wf := lifecycle{}.fixtureWorkflow()
	wf.Name = "notparticipant"
	wf.Schema = reflect.TypeOf(notAParticipant{})

	err := mgr.Register(wf)
	if err == nil {
		t.Fatal("Register accepted a Schema that does not implement Participant — the assertion panics at first request instead")
	}
	if !errors.Is(err, ErrInvalidWorkflow) {
		t.Errorf("err = %v, want ErrInvalidWorkflow", err)
	}
}

// notAParticipant has the lifecycle tags parseSchemaType requires but no
// Participant methods — the exact shape Register previously accepted.
type notAParticipant struct {
	ID     string `json:"entity_id" lifecycle:"id"`
	PhaseF string `json:"phase" lifecycle:"phase,predicate=mission.lifecycle.phase"`
}

// TestCreateFromOperator_ProjectsTheCausalResponseNotALaterRead pins the remedy
// for the "a committed birth can report as 500" blocker.
//
// The distinguishing fixture matters: the create RESPONSE carries a value the
// stored entity does not, so a post-hoc Get cannot produce it. Without this, the
// causal projection and the read it replaced are indistinguishable — review
// mutation-proved exactly that by swapping the projection back to a Get and
// keeping the whole suite green.
func TestCreateFromOperator_ProjectsTheCausalResponseNotALaterRead(t *testing.T) {
	mgr, emitter, _ := newTestManager(t)
	ctx := context.Background()

	emitter.createResponseMutator = func(e *graph.EntityState) {
		for i := range e.Triples {
			if e.Triples[i].Predicate == "mission.identity.owner-org-id" {
				e.Triples[i].Object = "from-the-causal-response"
			}
		}
	}

	const id = "c360.platform1.lifecycle.gcs.mission.causal"
	result, err := mgr.CreateFromOperator(ctx, "fixture",
		[]byte(`{"entity_id":"`+id+`","phase":"planning","owner_org_id":"from-the-request"}`))
	if err != nil {
		t.Fatalf("CreateFromOperator: %v", err)
	}
	if result.Degraded {
		t.Fatalf("unexpected degraded commit: %s", result.DegradedReason)
	}
	got := result.Instance.(*fixtureMission).OwnerOrgID
	if got != "from-the-causal-response" {
		t.Errorf("OwnerOrgID = %q, want %q — the result must be projected from the mutation response for THIS request, not from a later read",
			got, "from-the-causal-response")
	}
}

// TestCreateFromOperator_DegradedCommitIsSuccess pins the other half: a write
// that committed durably but could not be read back is a SUCCESS with a signal.
// Reporting it as an error makes the operator retry a birth that already
// happened, and the mutation contract forbids that retry.
func TestCreateFromOperator_DegradedCommitIsSuccess(t *testing.T) {
	mgr, emitter, _ := newTestManager(t)
	ctx := context.Background()
	emitter.createDegraded = true

	const id = "c360.platform1.lifecycle.gcs.mission.degraded"
	result, err := mgr.CreateFromOperator(ctx, "fixture",
		[]byte(`{"entity_id":"`+id+`","phase":"planning"}`))
	if err != nil {
		t.Fatalf("a degraded commit must not be an error: %v", err)
	}
	if !result.Degraded {
		t.Fatal("degraded commit was not signalled — the caller cannot tell it from a fully-read-back create")
	}
	if result.DegradedReason == "" {
		t.Error("degraded commit carries no reason — an operator has nothing to act on")
	}
	if result.Instance != nil {
		t.Error("degraded commit returned an Instance; there was nothing projectable")
	}
	// The write really did land — that is what makes retrying wrong.
	if _, getErr := mgr.Get(ctx, "fixture", id); getErr != nil {
		t.Errorf("degraded create did not commit: %v", getErr)
	}
}

// TestCreateFromOperator_UsesTheRouteSelectedRegistration pins that the write
// happens against the registration the CALLER chose, not one re-derived from
// the Participant's own constant.
//
// The two selectors genuinely diverge, because Register deliberately permits
// Name != Participant.Workflow() so a partial migration's cross-owner overlap
// does not brick. Re-deriving let a request routed as one workflow write with
// another's pattern, transitions, owner token, and audit predicates — and, with
// only the alias registered, fail a valid advertised route with a false
// not-found. That second case is what this test drives.
func TestCreateFromOperator_UsesTheRouteSelectedRegistration(t *testing.T) {
	ctx := context.Background()
	// One shared bucket: the emitter writes to it and the manager reads from it.
	bucket := newFakeBucket()
	mgr := newManagerForTest(nil, &fakeEmitter{bucket: bucket}, bucket)

	// Register ONLY the alias. fixtureMission.Workflow() returns "fixture",
	// which is deliberately not registered here.
	alias := lifecycle{}.fixtureWorkflow()
	alias.Name = "fixture-alias"
	if err := mgr.Register(alias); err != nil {
		t.Fatalf("Register alias: %v", err)
	}

	const id = "c360.platform1.lifecycle.gcs.mission.aliased"
	result, err := mgr.CreateFromOperator(ctx, "fixture-alias",
		[]byte(`{"entity_id":"`+id+`","phase":"planning"}`))
	if err != nil {
		t.Fatalf("create against the alias route: %v — the route's registration was discarded and re-looked-up by the Participant's own constant", err)
	}
	if result.Degraded {
		t.Fatalf("unexpected degraded commit: %s", result.DegradedReason)
	}
	if _, getErr := mgr.Get(ctx, "fixture-alias", id); getErr != nil {
		t.Errorf("instance not readable under the route's workflow: %v", getErr)
	}
}

// TestCreateFromOperator_RejectsUnknownFields pins the fail-closed decode. A
// permissive decode accepts keys the workflow cannot persist, drops them, and
// still answers 201 — losing an operator's submitted state behind a success.
func TestCreateFromOperator_RejectsUnknownFields(t *testing.T) {
	mgr, _, _ := newTestManager(t)
	ctx := context.Background()

	_, err := mgr.CreateFromOperator(ctx, "fixture",
		[]byte(`{"entity_id":"c360.platform1.lifecycle.gcs.mission.unk","phase":"planning","not_a_field":"dropped"}`))
	if !errors.Is(err, ErrInvalidInitialState) {
		t.Fatalf("err = %v, want ErrInvalidInitialState — an unpersistable key must not be silently dropped behind a 201", err)
	}
}

// TestCreate_LostReplyIsNeitherADuplicateNorASuccess pins the gh#861 answer on
// the lost-reply path: an outcome this request cannot prove is reported as
// UNKNOWN — never as a success it cannot show it made, and never reconstructed
// from stored state.
//
// The predecessor of this test asserted the OPPOSITE — that the manager RECOVER
// a lost reply as a degraded success — by re-reading the entity and matching the
// RFC3339Nano audit stamp this call was about to write. Wall-clock granularity
// is coarser than that format's precision, so two concurrent creates build
// byte-identical deltas and the LOSER reads the WINNER's write as proof of its
// own birth. openspec/specs/lifecycle/spec.md already required the answer be
// derived from the causal mutation response rather than a separate read, and
// named that exact failure mode.
//
// The fake scripts ATTEMPTS rather than one collapsed call, because a policy
// about re-sending cannot be tested by a fake that cannot represent a second
// delivery.
func TestCreate_LostReplyIsNeitherADuplicateNorASuccess(t *testing.T) {
	lostReplyTransport := fmt.Errorf("%w: NATS request to %s: context deadline exceeded",
		ErrEmitFailed, graphSubjectCreateWithTriples)
	alreadyExists := fmt.Errorf("%w: entity already exists", ErrAlreadyExists)

	cases := []struct {
		name string
		// attemptErr is what the ONE delivery answers after committing.
		attemptErr error
		wantErrIs  error
		why        string
	}{
		{
			// The regression gate for the DELETED reconciliation. The entity in
			// the bucket carries this very request's audit stamp — exactly the
			// "proof of ownership" the removed re-read accepted — and the answer
			// must still be a conflict, because a stamp two writers can share
			// proves nothing about who wrote it.
			name:       "already-exists for a write carrying this request's own stamp is still a conflict",
			attemptErr: alreadyExists,
			wantErrIs:  ErrAlreadyExists,
			why:        "ownership was reconstructed from stored state and a conflict became a success",
		},
		{
			// The emitter's new contract: a create whose reply is lost is a
			// transport failure with an UNKNOWN outcome. It is not dressed up as
			// a conflict (which blames the caller for something that may not
			// exist) and not as a success.
			name:       "a transport failure after a durable commit stays an unknown outcome",
			attemptErr: lostReplyTransport,
			wantErrIs:  ErrEmitFailed,
			why:        "an unprovable outcome was converted into a definite answer",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mgr, emitter, _ := newTestManager(t)
			ctx := context.Background()
			emitter.createScript = []createAttempt{
				{commit: true, err: tc.attemptErr},
				{commit: true, onDeliver: func() {
					t.Error("the create was re-sent after an ambiguous failure — a " +
						"non-idempotent create must not be delivered twice; the second " +
						"delivery is what answers already-exists for this request's own birth")
				}},
			}

			const id = "c360.platform1.lifecycle.gcs.mission.lostreply"
			result, err := mgr.CreateFromOperator(ctx, "fixture",
				[]byte(`{"entity_id":"`+id+`","phase":"planning"}`))

			if err == nil {
				t.Fatalf("reported SUCCESS (degraded=%v, reason=%q): %s",
					result.Degraded, result.DegradedReason, tc.why)
			}
			if !errors.Is(err, tc.wantErrIs) {
				t.Errorf("err = %v, want errors.Is(err, %v): %s", err, tc.wantErrIs, tc.why)
			}
			if emitter.createCalls != 1 {
				t.Errorf("create was delivered %d times, want exactly 1", emitter.createCalls)
			}
			// The write DID land in both cases: this is the genuinely-ambiguous
			// state, not a no-op failure. Resolving it is the caller's move
			// against authoritative state — which is exactly what the manager
			// must not do on the caller's behalf.
			if _, getErr := mgr.Get(ctx, "fixture", id); getErr != nil {
				t.Errorf("fixture is wrong: the scripted attempt should have committed: %v", getErr)
			}
		})
	}
}

// TestCreate_UnrelatedConcurrentUpdateIsNotADuplicateBirth pins the attach-path
// correction: a CAS revision mismatch means something changed the entity, not
// that a lifecycle birth happened. Any writer merging an unrelated predicate
// produces one, and "already lifecycle-managed" would be a false answer.
func TestCreate_UnrelatedConcurrentUpdateIsNotADuplicateBirth(t *testing.T) {
	mgr, emitter, bucket := newTestManager(t)
	ctx := context.Background()

	// Entity exists with a NON-lifecycle triple, and the CAS will miss.
	const id = "c360.platform1.lifecycle.gcs.mission.contended"
	bucket.put(id, &graph.EntityState{
		ID:      id,
		Version: 1,
		// Canonical predicate, unrelated to this workflow's lifecycle: the
		// point is an entity that moved for a reason that is not a birth.
		Triples: []message.Triple{{Subject: id, Predicate: "mission.identity.owner-org-id", Object: "acme"}},
	})
	emitter.forceRevisionMismatch = true

	err := mgr.Create(ctx, &fixtureMission{ID: id, PhaseF: "planning"})
	if errors.Is(err, ErrAlreadyExists) {
		t.Fatal("an unrelated concurrent update was reported as a duplicate lifecycle birth")
	}
	if !errors.Is(err, ErrUpdateRetriesExhausted) {
		t.Errorf("err = %v, want a retryable contention error", err)
	}
}
