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
	if f.bucket.exists(req.Entity.ID) {
		// ADR-060: production create() returns (nil, <error wrapping ErrAlreadyExists>).
		return nil, fmt.Errorf("%w: entity already exists", ErrAlreadyExists)
	}
	state := *req.Entity
	state.Triples = req.Triples
	f.bucket.put(req.Entity.ID, &state)
	return &graph.CreateEntityWithTriplesResponse{
		MutationResponse: graph.MutationResponse{KVRevision: f.bucket.revOf(req.Entity.ID)},
		Entity:           &state,
		TriplesAdded:     len(req.Triples),
	}, nil
}

// fakeBucket is the minimal jetstream.KeyValue surface Manager.getEntity
// + manager_query.go exercise. We implement only the methods the
// Manager calls; the rest panic if invoked.
type fakeBucket struct {
	mu      sync.Mutex
	entries map[string]*fakeBucketEntry
	nextRev uint64
}

type fakeBucketEntry struct {
	state     *graph.EntityState
	revision  uint64
	createdAt time.Time
}

func newFakeBucket() *fakeBucket {
	return &fakeBucket{entries: map[string]*fakeBucketEntry{}}
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

// jetstream.KeyValue minimum surface — embedding via composition is
// painful, so we implement only what Manager uses.

func (b *fakeBucket) Get(_ context.Context, key string) (jetstream.KeyValueEntry, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
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
func (b *fakeBucket) Watch(context.Context, string, ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	panic("fakeBucket.Watch not implemented")
}
func (b *fakeBucket) WatchAll(context.Context, ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	panic("fakeBucket.WatchAll not implemented")
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
func (b *fakeBucket) History(context.Context, string, ...jetstream.WatchOpt) ([]jetstream.KeyValueEntry, error) {
	panic("fakeBucket.History not implemented")
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
		PhasePredicate: "mission.phase",
		Schema:         reflect.TypeOf(fixtureMission{}),
		OperatorWritablePredicates: []string{
			"mission.owner_org_id",
			"mission.note",
		},
		AuditPredicates: AuditSpec{
			Source: "mission.last_transition_source",
			At:     "mission.last_transition_at",
			From:   "mission.last_transition_from",
			Note:   "mission.last_transition_note",
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
	// simulates a processor stamping `mission.command` before any
	// lifecycle action fires.
	bucket.put(id, &graph.EntityState{
		ID: id,
		Triples: []message.Triple{
			{Subject: id, Predicate: "mission.command", Object: "launch"},
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
		if tr.Predicate == "mission.phase" {
			phaseTriples = append(phaseTriples, tr)
		}
	}
	if len(phaseTriples) != 1 {
		t.Fatalf("expected exactly 1 mission.phase triple after Create+2 Transitions (replace, not append), got %d: %+v", len(phaseTriples), phaseTriples)
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
		EntityIDPattern: "*.lifecycle.gcs.mission.*", // 5 segments
		Transitions:     Transitions{"planning": {}},
		PhasePredicate:  "x.phase",
		Schema:          reflect.TypeOf(fixtureMission{}),
	}
	err := bad.validate()
	if err == nil {
		t.Fatal("expected validate to reject 5-segment EntityIDPattern, got nil")
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
			w.ChildWorkflows = []ChildSpec{{Workflow: "child", LinkPredicate: w.PhasePredicate}}
		}, true},
		{"child-link collides with an audit predicate", func(w *Workflow) {
			w.ChildWorkflows = []ChildSpec{{Workflow: "child", LinkPredicate: w.AuditPredicates.At}}
		}, true},
		{"reference collides with a scalar projection field", func(w *Workflow) {
			w.ReferencePredicates = []ReferenceSpec{{Predicate: "mission.owner_org_id"}}
		}, true},
		{"reference field matching its own ReferenceSpec is valid", func(w *Workflow) {
			w.ReferencePredicates = []ReferenceSpec{{Predicate: "mission.assigned_drone"}}
		}, false},
		{"distinct child-link is fine", func(w *Workflow) {
			w.ChildWorkflows = []ChildSpec{{Workflow: "child", LinkPredicate: "mission.subtask"}}
		}, false},
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
	if _, ok := gotPreds["mission.owner_org_id"]; ok {
		t.Error("zero-value OwnerOrgID should not emit a delta against missing predicate baseline")
	}
}
