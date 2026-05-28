package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
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

func (f *fakeEmitter) emit(_ context.Context, req *graph.UpdateEntityWithTriplesRequest) (*graph.UpdateEntityWithTriplesResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
	currentRev := f.bucket.revOf(req.Entity.ID)
	if req.ExpectedRevision > 0 && req.ExpectedRevision != currentRev {
		return &graph.UpdateEntityWithTriplesResponse{
			MutationResponse: graph.MutationResponse{
				Success: false,
				Error:   "revision mismatch: expected " + tostr(req.ExpectedRevision) + ", current " + tostr(currentRev),
			},
		}, errEmitRevisionMismatch
	}
	current := f.bucket.get(req.Entity.ID)
	merged := current.Triples
	if len(req.RemoveTriples) > 0 {
		removeSet := map[string]struct{}{}
		for _, p := range req.RemoveTriples {
			removeSet[p] = struct{}{}
		}
		kept := merged[:0]
		for _, t := range merged {
			if _, drop := removeSet[t.Predicate]; drop {
				continue
			}
			kept = append(kept, t)
		}
		merged = kept
	}
	merged = applyPerPredicateLatestWins(merged, req.AddTriples)
	state := *req.Entity
	state.Triples = merged
	f.bucket.put(req.Entity.ID, &state)
	return &graph.UpdateEntityWithTriplesResponse{
		MutationResponse: graph.MutationResponse{Success: true, KVRevision: f.bucket.revOf(req.Entity.ID)},
		Entity:           &state,
	}, nil
}

// applyPerPredicateLatestWins mirrors graph-ingest's per-predicate
// latest-wins merge. Triples in adds replace prior triples with the
// same predicate on the same subject.
func applyPerPredicateLatestWins(existing, adds []message.Triple) []message.Triple {
	bySubjPred := map[string]int{}
	out := make([]message.Triple, 0, len(existing)+len(adds))
	for _, t := range existing {
		key := t.Subject + "|" + t.Predicate
		if idx, ok := bySubjPred[key]; ok {
			out[idx] = t
		} else {
			bySubjPred[key] = len(out)
			out = append(out, t)
		}
	}
	for _, t := range adds {
		key := t.Subject + "|" + t.Predicate
		if idx, ok := bySubjPred[key]; ok {
			out[idx] = t
		} else {
			bySubjPred[key] = len(out)
			out = append(out, t)
		}
	}
	return out
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

func tostr(u uint64) string {
	const digits = "0123456789"
	if u == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for u > 0 {
		i--
		buf[i] = digits[u%10]
		u /= 10
	}
	return string(buf[i:])
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
