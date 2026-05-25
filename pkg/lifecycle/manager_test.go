package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// missionState is the test fixture for Manager unit tests. Shape
// mirrors the ADR-047 worked example so the tests double as a
// readable spec for "how does a Participant implementation actually
// look in practice." Implements the Participant interface and uses
// the canonical struct-tag layout.
type missionState struct {
	EntityIDF      string `json:"entity_id" lifecycle:"id"`
	PhaseF         string `json:"phase" lifecycle:"phase,readonly"`
	OwnerOrgIDF    string `json:"owner_org_id" lifecycle:"operator_writable"`
	ParentMissionF string `json:"parent_mission,omitempty"`
}

func (m *missionState) EntityID() string             { return m.EntityIDF }
func (m *missionState) Workflow() string             { return "mission" }
func (m *missionState) Phase() string                { return m.PhaseF }
func (m *missionState) IsTerminal() bool             { return missionTransitions.IsTerminal(m.PhaseF) }
func (m *missionState) KVBucket() string             { return "MISSIONS" }
func (m *missionState) KVKey(entityID string) string { return "mission." + entityID }
func (m *missionState) ParentEntityID() string       { return m.ParentMissionF }

// valueMission is the deliberately-broken Participant shape for the
// "factory must return pointer" Register-time rejection test. Value-
// receiver methods + factory-returns-by-value. NOT a usable
// Participant in production — exists solely so the test fixture
// reaches the rv.Kind() != reflect.Pointer rejection path in
// Manager.Register.
type valueMission struct {
	EntityIDF string `json:"entity_id" lifecycle:"id"`
	PhaseF    string `json:"phase" lifecycle:"phase"`
}

func (v valueMission) EntityID() string             { return v.EntityIDF }
func (v valueMission) Workflow() string             { return "value-mission" }
func (v valueMission) Phase() string                { return v.PhaseF }
func (v valueMission) IsTerminal() bool             { return false }
func (v valueMission) KVBucket() string             { return "MISSIONS" }
func (v valueMission) KVKey(entityID string) string { return "v." + entityID }
func (v valueMission) ParentEntityID() string       { return "" }

var missionTransitions = Transitions{
	"planning":  {"flying", "aborted"},
	"flying":    {"capturing", "landing", "aborted"},
	"capturing": {"flying"},
	"landing":   {"completed", "failed"},
	"completed": {},
	"failed":    {},
	"aborted":   {},
}

func missionFactory() Participant { return &missionState{} }

// newTestManager builds a Manager wired to a fresh in-memory mock
// store, registers the mission workflow, and returns both for the
// test body. Centralizes the boilerplate so each test focuses on
// the behavior under test.
func newTestManager(t *testing.T) (*Manager, *kvMockStore) {
	t.Helper()
	store := newKVMockStore(nil)
	mgr := newManagerForTest(nil, func(bucket string) (kvStore, error) {
		if bucket != "MISSIONS" {
			t.Fatalf("test fixture only supports MISSIONS bucket, got %q", bucket)
		}
		return store, nil
	})
	if err := mgr.Register("mission", missionFactory, missionTransitions); err != nil {
		t.Fatalf("Register mission: %v", err)
	}
	return mgr, store
}

// --- Register ---

func TestManager_Register_HappyPath(t *testing.T) {
	mgr, _ := newTestManager(t)
	// Re-registering must reject as ErrWorkflowAlreadyRegistered;
	// idempotent-re-init is a wiring bug, not a benign no-op.
	err := mgr.Register("mission", missionFactory, missionTransitions)
	if !errors.Is(err, ErrWorkflowAlreadyRegistered) {
		t.Errorf("re-register should error with ErrWorkflowAlreadyRegistered, got %v", err)
	}
}

func TestManager_Register_RejectsInvalidTransitions(t *testing.T) {
	mgr := newManagerForTest(nil, func(string) (kvStore, error) {
		return newKVMockStore(nil), nil
	})
	bad := Transitions{
		"a": {"b"}, // "b" never declared as a key
	}
	err := mgr.Register("mission", missionFactory, bad)
	if !errors.Is(err, ErrInvalidTransitionsTable) {
		t.Fatalf("expected ErrInvalidTransitionsTable, got %v", err)
	}
}

func TestManager_Register_RejectsNilFactory(t *testing.T) {
	mgr := newManagerForTest(nil, func(string) (kvStore, error) {
		return newKVMockStore(nil), nil
	})
	if err := mgr.Register("mission", nil, missionTransitions); err == nil {
		t.Fatal("nil factory must be rejected")
	}
}

func TestManager_Register_RejectsFactoryReturningNil(t *testing.T) {
	mgr := newManagerForTest(nil, func(string) (kvStore, error) {
		return newKVMockStore(nil), nil
	})
	err := mgr.Register("mission", func() Participant { return nil }, missionTransitions)
	if err == nil || !strings.Contains(err.Error(), "nil") {
		t.Fatalf("nil-returning factory must be rejected with nil-mentioning error, got %v", err)
	}
}

func TestManager_Register_RejectsFactoryReturningValue(t *testing.T) {
	// Value-typed Participant returns from the factory are rejected
	// at Register time. json.Unmarshal can't populate value
	// receivers (no addressability), and reflect.Value.SetString on
	// the phase field would panic on a non-pointer. Catching this
	// at Register surfaces the wiring bug at startup, not at the
	// first Get/Transition attempt in prod.
	//
	// Uses the package-scoped valueMission fixture (value-receiver
	// methods, factory returns by value) so the test exercises the
	// REAL "non-pointer factory return" code path at manager.go's
	// rv.Kind() != reflect.Pointer check.
	mgr := newManagerForTest(nil, func(string) (kvStore, error) {
		return newKVMockStore(nil), nil
	})
	err := mgr.Register("value-mission", func() Participant {
		return valueMission{EntityIDF: "x", PhaseF: "planning"}
	}, missionTransitions)
	if err == nil {
		t.Fatal("value-typed factory return must be rejected")
	}
	if !strings.Contains(err.Error(), "pointer") {
		t.Errorf("rejection should mention 'pointer' for operator debugging, got %q", err)
	}
}

func TestManager_Register_RejectsMismatchedWorkflowName(t *testing.T) {
	mgr := newManagerForTest(nil, func(string) (kvStore, error) {
		return newKVMockStore(nil), nil
	})
	// Factory returns missionState which declares Workflow()=="mission",
	// but we register it under a different name. Mismatch should reject.
	err := mgr.Register("survey", missionFactory, missionTransitions)
	if err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("workflow-name mismatch should reject with mismatch-mentioning error, got %v", err)
	}
}

// --- Get ---

func TestManager_Get_ReturnsNotFound(t *testing.T) {
	mgr, _ := newTestManager(t)
	_, err := mgr.Get(context.Background(), "mission", "does-not-exist")
	if !errors.Is(err, ErrEntityNotFound) {
		t.Fatalf("Get on missing entity should error ErrEntityNotFound, got %v", err)
	}
}

func TestManager_Get_UnknownWorkflow(t *testing.T) {
	mgr, _ := newTestManager(t)
	_, err := mgr.Get(context.Background(), "never-registered", "irrelevant")
	if !errors.Is(err, ErrWorkflowNotRegistered) {
		t.Fatalf("Get on unregistered workflow should error ErrWorkflowNotRegistered, got %v", err)
	}
}

// --- Create + Get round-trip ---

func TestManager_Create_AndGet_RoundTrip(t *testing.T) {
	mgr, _ := newTestManager(t)
	initial := &missionState{
		EntityIDF:   "mission-001",
		PhaseF:      "planning",
		OwnerOrgIDF: "acme",
	}
	if err := mgr.Create(context.Background(), initial); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := mgr.Get(context.Background(), "mission", "mission-001")
	if err != nil {
		t.Fatalf("Get after Create: %v", err)
	}
	gotMission, ok := got.(*missionState)
	if !ok {
		t.Fatalf("Get returned %T, want *missionState", got)
	}
	if gotMission.PhaseF != "planning" || gotMission.OwnerOrgIDF != "acme" {
		t.Errorf("round-trip lost fields: %+v", gotMission)
	}
}

func TestManager_Create_RejectsDuplicate(t *testing.T) {
	mgr, _ := newTestManager(t)
	initial := &missionState{EntityIDF: "dup", PhaseF: "planning"}
	if err := mgr.Create(context.Background(), initial); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	err := mgr.Create(context.Background(), initial)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate Create should error with already-exists, got %v", err)
	}
}

func TestManager_Create_RejectsUndeclaredInitialPhase(t *testing.T) {
	mgr, _ := newTestManager(t)
	bad := &missionState{EntityIDF: "x", PhaseF: "exploded"}
	err := mgr.Create(context.Background(), bad)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("Create with undeclared initial phase should error ErrInvalidTransition, got %v", err)
	}
}

// --- Update ---

func TestManager_Update_HappyPath(t *testing.T) {
	mgr, _ := newTestManager(t)
	mustCreate(t, mgr, "u-1", "planning")

	err := mgr.Update(context.Background(), "mission", "u-1", func(p Participant) error {
		ms := p.(*missionState)
		ms.OwnerOrgIDF = "newcorp"
		return nil
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, _ := mgr.Get(context.Background(), "mission", "u-1")
	if got.(*missionState).OwnerOrgIDF != "newcorp" {
		t.Errorf("mutation didn't persist: %+v", got)
	}
}

func TestManager_Update_MutatorErrorAborts(t *testing.T) {
	mgr, _ := newTestManager(t)
	mustCreate(t, mgr, "u-2", "planning")

	sentinel := errors.New("mutator says no")
	err := mgr.Update(context.Background(), "mission", "u-2", func(_ Participant) error {
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Errorf("mutator error must wrap and surface, got %v", err)
	}
	// State must be unchanged after mutator-reject (no KV write happened).
	got, _ := mgr.Get(context.Background(), "mission", "u-2")
	if got.(*missionState).PhaseF != "planning" {
		t.Errorf("mutator-reject should not have written; got phase %q", got.(*missionState).PhaseF)
	}
}

func TestManager_Update_RetriesOnCASConflict(t *testing.T) {
	// Use a mock store that injects a CAS conflict on the first Update,
	// then succeeds on retry. Verifies the retry loop actually runs.
	store := newKVMockStore(nil)
	conflictStore := &flakyOnceUpdateStore{kvMockStore: store}
	mgr := newManagerForTest(nil, func(_ string) (kvStore, error) {
		return conflictStore, nil
	})
	if err := mgr.Register("mission", missionFactory, missionTransitions); err != nil {
		t.Fatalf("Register: %v", err)
	}
	mustCreate(t, mgr, "u-3", "planning")

	err := mgr.Update(context.Background(), "mission", "u-3", func(p Participant) error {
		p.(*missionState).OwnerOrgIDF = "after-retry"
		return nil
	})
	if err != nil {
		t.Fatalf("Update should have succeeded on retry, got %v", err)
	}
	if !conflictStore.firstConflictInjected {
		t.Error("retry path didn't trigger — flakyOnceUpdateStore never injected its conflict")
	}
	got, _ := mgr.Get(context.Background(), "mission", "u-3")
	if got.(*missionState).OwnerOrgIDF != "after-retry" {
		t.Errorf("retry didn't apply final mutation: %+v", got)
	}
}

func TestManager_Update_ConcurrentMutationsSerialize(t *testing.T) {
	// N goroutines mutate the same entity concurrently. Each
	// appends a unique tag to the OwnerOrgIDF field. After all
	// goroutines return, the final field value must contain
	// every tag (none lost to silent CAS-conflict drops) and no
	// duplicates (the CAS retry loop must read-then-write
	// atomically — read-stale-then-write-stale would either
	// double-append or drop entries).
	//
	// N is bounded by updateRetries because of the convergence
	// arithmetic: with N writers, the worst-case loser needs
	// up to N-1 retries (each "round" of retries advances exactly
	// one writer). Using N == updateRetries-1 keeps the test
	// reliably non-flaky while still exercising the full CAS-
	// retry serialization path. The "what happens under
	// budget-exhaustion contention" case is covered separately
	// by TestManager_Update_ExhaustsRetriesUnderPersistentConflict.
	mgr, _ := newTestManager(t)
	mustCreate(t, mgr, "concurrent", "planning")

	const goroutines = updateRetries - 1
	tags := make([]string, goroutines)
	for i := range goroutines {
		tags[i] = fmt.Sprintf("tag-%02d", i)
	}

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func(tag string) {
			defer wg.Done()
			err := mgr.Update(context.Background(), "mission", "concurrent", func(p Participant) error {
				ms := p.(*missionState)
				if ms.OwnerOrgIDF == "" {
					ms.OwnerOrgIDF = tag
				} else {
					ms.OwnerOrgIDF = ms.OwnerOrgIDF + "," + tag
				}
				return nil
			})
			if err != nil {
				t.Errorf("concurrent Update %q: %v", tag, err)
			}
		}(tags[i])
	}
	wg.Wait()

	got, err := mgr.Get(context.Background(), "mission", "concurrent")
	if err != nil {
		t.Fatalf("Get after concurrent updates: %v", err)
	}
	final := got.(*missionState).OwnerOrgIDF
	parts := strings.Split(final, ",")
	if len(parts) != goroutines {
		t.Fatalf("expected %d tags in final value, got %d: %q", goroutines, len(parts), final)
	}
	seen := make(map[string]int, goroutines)
	for _, part := range parts {
		seen[part]++
	}
	if len(seen) != goroutines {
		t.Errorf("expected %d distinct tags, got %d (dup detection): %v", goroutines, len(seen), seen)
	}
	for _, tag := range tags {
		if seen[tag] != 1 {
			t.Errorf("tag %q appeared %d times, expected 1", tag, seen[tag])
		}
	}
}

func TestManager_Update_MutatorCanSetPhaseDirectly(t *testing.T) {
	// Current behavior: Manager.Update's mutator can mutate the
	// Phase field directly without going through Transition. This
	// bypasses the transitions-table validation that Transition
	// applies. This test LOCKS the current behavior — either it
	// continues to be allowed (apps that need it have an escape
	// hatch) OR a future change deliberately closes it (and
	// updates the test).
	//
	// Discipline call: apps SHOULD use Transition for phase
	// changes; the convenience of mutator-can-do-anything is
	// occasionally useful for state-recovery shims (e.g. clearing
	// drift on a known-corrupt instance). Documented here so the
	// behavior isn't ambiguous in either direction.
	mgr, _ := newTestManager(t)
	mustCreate(t, mgr, "direct", "planning")

	err := mgr.Update(context.Background(), "mission", "direct", func(p Participant) error {
		// Skip from planning to completed — not a declared edge
		// in the transitions table, but Update doesn't validate
		// edges (Transition does). The Update succeeds.
		ms := p.(*missionState)
		ms.PhaseF = "completed"
		return nil
	})
	if err != nil {
		t.Fatalf("mutator setting Phase directly should succeed (Update doesn't validate edges): %v", err)
	}
	got, _ := mgr.Get(context.Background(), "mission", "direct")
	if got.Phase() != "completed" {
		t.Errorf("direct Phase mutation didn't persist: %q", got.Phase())
	}
}

func TestManager_Update_ExhaustsRetriesUnderPersistentConflict(t *testing.T) {
	// Mock store that conflicts on EVERY Update — Manager must give
	// up after updateRetries attempts and surface the CAS error.
	store := newKVMockStore(nil)
	alwaysConflict := &alwaysConflictUpdateStore{kvMockStore: store}
	mgr := newManagerForTest(nil, func(_ string) (kvStore, error) {
		return alwaysConflict, nil
	})
	if err := mgr.Register("mission", missionFactory, missionTransitions); err != nil {
		t.Fatalf("Register: %v", err)
	}
	mustCreate(t, mgr, "u-4", "planning")

	err := mgr.Update(context.Background(), "mission", "u-4", func(_ Participant) error {
		return nil
	})
	if err == nil || !errors.Is(err, errKVRevisionMismatch) {
		t.Fatalf("persistent conflict should exhaust retries with CAS error, got %v", err)
	}
	if alwaysConflict.updateCalls != updateRetries {
		t.Errorf("expected exactly %d Update attempts, got %d", updateRetries, alwaysConflict.updateCalls)
	}
}

// --- Transition ---

func TestManager_Transition_HappyPath(t *testing.T) {
	mgr, _ := newTestManager(t)
	mustCreate(t, mgr, "t-1", "planning")

	err := mgr.Transition(context.Background(), "mission", "t-1", "flying", TransitionSourceRule, "")
	if err != nil {
		t.Fatalf("Transition planning→flying: %v", err)
	}
	got, _ := mgr.Get(context.Background(), "mission", "t-1")
	if got.Phase() != "flying" {
		t.Errorf("Phase didn't update, got %q", got.Phase())
	}
}

func TestManager_Transition_RejectsUndeclaredTarget(t *testing.T) {
	mgr, _ := newTestManager(t)
	mustCreate(t, mgr, "t-2", "planning")
	err := mgr.Transition(context.Background(), "mission", "t-2", "exploded", TransitionSourceRule, "")
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("target not in table should error ErrInvalidTransition, got %v", err)
	}
}

func TestManager_Transition_RejectsInvalidEdge(t *testing.T) {
	mgr, _ := newTestManager(t)
	mustCreate(t, mgr, "t-3", "planning")
	// planning → completed isn't a declared edge.
	err := mgr.Transition(context.Background(), "mission", "t-3", "completed", TransitionSourceRule, "")
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("non-edge transition should error ErrInvalidTransition, got %v", err)
	}
}

func TestManager_Transition_RejectsFromTerminal(t *testing.T) {
	mgr, _ := newTestManager(t)
	mustCreate(t, mgr, "t-4", "planning")
	// Walk to a terminal first.
	must(t, mgr.Transition(context.Background(), "mission", "t-4", "aborted", TransitionSourceOperator, ""))
	// Now try to transition out of terminal — should error specifically
	// ErrTerminalPhase (distinguished from ErrInvalidTransition so
	// dashboards can show the right hint).
	err := mgr.Transition(context.Background(), "mission", "t-4", "planning", TransitionSourceRule, "")
	if !errors.Is(err, ErrTerminalPhase) {
		t.Fatalf("transition from terminal should error ErrTerminalPhase, got %v", err)
	}
}

// --- Complete ---

func TestManager_Complete_PicksFirstReachableTerminal(t *testing.T) {
	mgr, _ := newTestManager(t)
	mustCreate(t, mgr, "c-1", "planning")
	// Walk to landing so a terminal transition is possible.
	must(t, mgr.Transition(context.Background(), "mission", "c-1", "flying", TransitionSourceRule, ""))
	must(t, mgr.Transition(context.Background(), "mission", "c-1", "landing", TransitionSourceRule, ""))

	if err := mgr.Complete(context.Background(), "mission", "c-1"); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	got, _ := mgr.Get(context.Background(), "mission", "c-1")
	// landing → {completed, failed}; sorted declared terminals are
	// {aborted, completed, failed}; first sorted terminal that is
	// ALSO reachable from landing is "completed". Deterministic.
	if got.Phase() != "completed" {
		t.Errorf("Complete from landing should pick first sorted reachable terminal 'completed', got %q",
			got.Phase())
	}
	if !got.IsTerminal() {
		t.Errorf("entity should be terminal after Complete, got %q", got.Phase())
	}
}

func TestManager_Complete_RejectsFromTerminal(t *testing.T) {
	mgr, _ := newTestManager(t)
	mustCreate(t, mgr, "c-2", "planning")
	must(t, mgr.Transition(context.Background(), "mission", "c-2", "aborted", TransitionSourceOperator, ""))

	err := mgr.Complete(context.Background(), "mission", "c-2")
	if !errors.Is(err, ErrTerminalPhase) {
		t.Fatalf("Complete on already-terminal entity should error ErrTerminalPhase, got %v", err)
	}
}

func TestManager_Complete_NoReachableTerminalErrors(t *testing.T) {
	// Custom transitions table where the non-terminal phase has no
	// edge to any terminal — Complete should surface the wiring bug.
	noReachableTerminal := Transitions{
		"start":     {"middle"},
		"middle":    {"start"}, // cycle, no terminal reachable
		"completed": {},
	}
	store := newKVMockStore(nil)
	mgr := newManagerForTest(nil, func(_ string) (kvStore, error) {
		return store, nil
	})
	if err := mgr.Register("mission", missionFactory, noReachableTerminal); err != nil {
		t.Fatalf("Register: %v", err)
	}
	must(t, mgr.Create(context.Background(), &missionState{EntityIDF: "stuck", PhaseF: "start"}))

	err := mgr.Complete(context.Background(), "mission", "stuck")
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("Complete with no reachable terminal should error ErrInvalidTransition, got %v", err)
	}
	if !strings.Contains(err.Error(), "no edge to any terminal") {
		t.Errorf("error should mention 'no edge to any terminal' for operator debugging, got %q", err)
	}
}

// --- Fail ---

func TestManager_Fail_RequiresReason(t *testing.T) {
	mgr, _ := newTestManager(t)
	err := mgr.Fail(context.Background(), "mission", "anything", "")
	if err == nil {
		t.Fatal("Fail with empty reason must error (audit-trail discipline)")
	}
}

func TestManager_Fail_TransitionsToFailedPhase(t *testing.T) {
	mgr, _ := newTestManager(t)
	mustCreate(t, mgr, "f-1", "planning")
	must(t, mgr.Transition(context.Background(), "mission", "f-1", "flying", TransitionSourceRule, ""))
	must(t, mgr.Transition(context.Background(), "mission", "f-1", "landing", TransitionSourceRule, ""))

	if err := mgr.Fail(context.Background(), "mission", "f-1", "engine failure"); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	got, _ := mgr.Get(context.Background(), "mission", "f-1")
	if got.Phase() != "failed" {
		t.Errorf("Fail should transition to 'failed', got %q", got.Phase())
	}
}

// --- helpers ---

func mustCreate(t *testing.T, mgr *Manager, id, phase string) {
	t.Helper()
	err := mgr.Create(context.Background(), &missionState{EntityIDF: id, PhaseF: phase})
	if err != nil {
		t.Fatalf("mustCreate %q@%q: %v", id, phase, err)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// flakyOnceUpdateStore wraps kvMockStore to inject one CAS conflict
// on the FIRST Update call. Used by the retry-on-conflict test.
type flakyOnceUpdateStore struct {
	*kvMockStore
	mu                    sync.Mutex
	firstConflictInjected bool
}

func (f *flakyOnceUpdateStore) Update(ctx context.Context, key string, value []byte, expectedRevision uint64) (uint64, error) {
	f.mu.Lock()
	if !f.firstConflictInjected {
		f.firstConflictInjected = true
		f.mu.Unlock()
		return 0, errKVRevisionMismatch
	}
	f.mu.Unlock()
	return f.kvMockStore.Update(ctx, key, value, expectedRevision)
}

// alwaysConflictUpdateStore makes EVERY Update conflict, used to
// verify Manager exhausts updateRetries and surfaces the CAS error.
type alwaysConflictUpdateStore struct {
	*kvMockStore
	mu          sync.Mutex
	updateCalls int
}

func (a *alwaysConflictUpdateStore) Update(_ context.Context, _ string, _ []byte, _ uint64) (uint64, error) {
	a.mu.Lock()
	a.updateCalls++
	a.mu.Unlock()
	return 0, errKVRevisionMismatch
}
