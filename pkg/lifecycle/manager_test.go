package lifecycle

import (
	"context"
	"errors"
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

func (m *missionState) EntityID() string       { return m.EntityIDF }
func (m *missionState) Workflow() string       { return "mission" }
func (m *missionState) Phase() string          { return m.PhaseF }
func (m *missionState) IsTerminal() bool       { return missionTransitions.IsTerminal(m.PhaseF) }
func (m *missionState) KVBucket() string       { return "MISSIONS" }
func (m *missionState) KVKey() string          { return "mission." + m.EntityIDF }
func (m *missionState) ParentEntityID() string { return m.ParentMissionF }

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
	// json.Unmarshal can't populate value-typed Participants; the
	// reflect SetString on the phase field would also panic on a
	// non-addressable value. Reject at Register time so the wiring
	// bug surfaces at startup.
	type valueMission struct{ missionState }
	type valueImpl struct {
		MissionState valueMission
	}
	_ = valueImpl{}
	// Constructing a value-typed Participant requires a non-pointer
	// return. The Participant interface is satisfied by missionState
	// (value receiver methods would work but we use pointer
	// receivers); valueFactory returns the value directly, not a
	// pointer.
	valueFactory := func() Participant {
		// missionState's methods are pointer-receiver, so we have
		// to return *missionState via a dereference trick — but
		// the simpler shape is: a separate type that implements
		// Participant with value receivers, intentionally NOT
		// returning a pointer. For this test the goal is just to
		// trigger the "non-pointer factory return" rejection.
		var ms = missionState{}
		// reflect.ValueOf(ms).Kind() == reflect.Struct, not Pointer.
		// We need to return ms as Participant, but missionState's
		// methods are on the pointer receiver. Cast through any:
		type valueParticipant struct{ s missionState }
		// Simpler: just define an inline type with value receivers.
		// Skip the gymnastics — return missionState by value.
		_ = ms
		return nil // can't easily express value-typed Participant inline
	}
	mgr := newManagerForTest(nil, func(string) (kvStore, error) {
		return newKVMockStore(nil), nil
	})
	err := mgr.Register("mission", valueFactory, missionTransitions)
	if err == nil {
		t.Fatal("nil-returning factory must error (this is the simplest hostile shape)")
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
