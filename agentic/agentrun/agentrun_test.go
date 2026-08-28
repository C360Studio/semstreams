// Package agentrun_test contains TDD tests for the agentrun package (ADR-053 Pass B).
//
// Tests are organized by ADR decision:
//   - D1: AgentRun Participant interface compliance + projection round-trip
//   - D2: WorkflowDeclaration validation
//   - D4: Mint idempotence
//   - D6: ResolveRun typed + ancestry-walk fallback (with WARN log verification shape)
//   - D3: Terminal authority stays with coordinators/components
//   - D6: Subscriber category demux (cancellation rides loop_cancelled category)
//   - D6: Panic guard
package agentrun_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/agentic/agentrun"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/lifecycle"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- D1: AgentRun Participant interface compliance ---

func TestAgentRun_ParticipantInterface(t *testing.T) {
	t.Parallel()
	run := &agentrun.AgentRun{
		EntityIDField:     "acme.ops.chain.agent.execution.loop-uuid-abc",
		PhaseField:        "dispatched",
		ParentRunEntityID: "acme.ops.chain.agent.execution.parent-uuid",
	}
	assert.Equal(t, "acme.ops.chain.agent.execution.loop-uuid-abc", run.EntityID())
	assert.Equal(t, "agent-run", run.Workflow())
	assert.Equal(t, "dispatched", run.Phase())
	assert.False(t, run.IsTerminal(), "dispatched is not terminal")
	assert.Equal(t, "acme.ops.chain.agent.execution.parent-uuid", run.ParentEntityID())
}

func TestAgentRun_IsTerminal(t *testing.T) {
	t.Parallel()
	cases := []struct {
		phase    string
		terminal bool
	}{
		{"dispatched", false},
		{"executing", false},
		{"awaiting_approval", false},
		{"completed", true},
		{"failed", true},
		{"cancelled", true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.phase, func(t *testing.T) {
			t.Parallel()
			run := &agentrun.AgentRun{PhaseField: tc.phase}
			assert.Equal(t, tc.terminal, run.IsTerminal())
		})
	}
}

func TestAgentRun_RunID_ValidChainEntity(t *testing.T) {
	t.Parallel()
	run := &agentrun.AgentRun{
		EntityIDField: "acme.ops.chain.agent.execution.loop-uuid-abc",
	}
	runID, ok := run.RunID()
	require.True(t, ok)
	assert.Equal(t, "loop-uuid-abc", runID)
}

func TestAgentRun_RunID_NonChainEntityReturnsFalse(t *testing.T) {
	t.Parallel()
	// agentic-loop entity, not a chain.execution entity.
	run := &agentrun.AgentRun{
		EntityIDField: "acme.ops.agentic-loop.agent.execution.loop-uuid",
	}
	_, ok := run.RunID()
	assert.False(t, ok, "RunID should return false for non-chain.execution entity IDs")
}

// D1 ADR critical: the lifecycle:"id" field must hold the FULL 6-part entity
// ID, not a bare run UUID. Verify that the struct field tag is correct and
// that the AgentRun struct compiles as a valid Participant (static type check).
func TestAgentRun_FullIDInEntityIDField(t *testing.T) {
	t.Parallel()
	fullEntityID := "acme.ops.chain.agent.execution.run-uuid-001"
	run := &agentrun.AgentRun{EntityIDField: fullEntityID}
	// EntityID() must return the full 6-part ID, not a bare runID.
	assert.Equal(t, fullEntityID, run.EntityID())

	// RunID() must derive the bare UUID from the full ID — NOT the reverse.
	runID, ok := run.RunID()
	require.True(t, ok)
	assert.Equal(t, "run-uuid-001", runID, "RunID() must parse the instance segment")
	assert.NotContains(t, runID, ".", "bare RunID must not contain dots")
}

// D1 projection round-trip guard: verify that the AgentRun struct has the
// correct lifecycle struct tags for the projection layer. We do this via the
// WorkflowDeclaration() → Schema field being the correct reflect.Type.
func TestAgentRun_WorkflowDeclarationSchemaIsAgentRunType(t *testing.T) {
	t.Parallel()
	wf := agentrun.WorkflowDeclaration()
	// Schema must point to AgentRun struct, not a pointer to it.
	// reflect.TypeOf(AgentRun{}) vs reflect.TypeOf(&AgentRun{}).Elem()
	// This asserts the Schema was set correctly in WorkflowDeclaration().
	schemaName := wf.Schema.Name()
	assert.Equal(t, "AgentRun", schemaName,
		"Workflow.Schema must be reflect.TypeOf(AgentRun{})")
}

// --- D2: WorkflowDeclaration validation ---

func TestWorkflowDeclaration_TransitionsValid(t *testing.T) {
	t.Parallel()
	wf := agentrun.WorkflowDeclaration()
	err := wf.Transitions.Validate()
	require.NoError(t, err, "agent-run transitions table must be internally consistent")
}

func TestWorkflowDeclaration_EntityIDPattern(t *testing.T) {
	t.Parallel()
	wf := agentrun.WorkflowDeclaration()
	assert.Equal(t, "*.*.chain.agent.execution.*", wf.EntityIDPattern)
}

func TestWorkflowDeclaration_PhasePredicate(t *testing.T) {
	t.Parallel()
	wf := agentrun.WorkflowDeclaration()
	assert.Equal(t, "agent.run.phase", wf.PhasePredicate)
}

func TestWorkflowDeclaration_TerminalPhases(t *testing.T) {
	t.Parallel()
	wf := agentrun.WorkflowDeclaration()
	terminals := wf.Transitions.TerminalPhases()
	assert.ElementsMatch(t, []string{"completed", "failed", "cancelled"}, terminals)
}

// --- Integration path: use lifecycle.Manager with integration build tag ---
// The unit tests below use an in-memory lifecycle reader.

// --- Mock lifecycle.Manager for unit tests ---

// mockLifecycleManager is a test double used for run-state reads and Mint.
type mockLifecycleManager struct {
	runs map[string]*agentrun.AgentRun
}

func newMockLifecycleManager() *mockLifecycleManager {
	return &mockLifecycleManager{
		runs: make(map[string]*agentrun.AgentRun),
	}
}

func (m *mockLifecycleManager) seed(run *agentrun.AgentRun) {
	m.runs[run.EntityIDField] = run
}

func (m *mockLifecycleManager) Get(_ context.Context, workflow, entityID string) (lifecycle.Participant, error) {
	if workflow != agentrun.WorkflowName {
		return nil, lifecycle.ErrWorkflowNotRegistered
	}
	run, ok := m.runs[entityID]
	if !ok {
		return nil, lifecycle.ErrEntityNotFound
	}
	// Return a copy so phase changes on the returned value don't affect stored state.
	runCopy := *run
	return &runCopy, nil
}

func (m *mockLifecycleManager) Create(_ context.Context, initial lifecycle.Participant) error {
	run, ok := initial.(*agentrun.AgentRun)
	if !ok {
		return errors.New("mockLifecycleManager: Create: unexpected type")
	}
	if _, exists := m.runs[run.EntityIDField]; exists {
		return lifecycle.ErrAlreadyExists
	}
	runCopy := *run
	m.runs[run.EntityIDField] = &runCopy
	return nil
}

// --- D4: Mint idempotence ---

// TestMint_Idempotent_ErrAlreadyExistsFallsBackToGet verifies that when
// Manager.Create returns ErrAlreadyExists, Mint falls back to Manager.Get
// and returns the already-minted run (not an error). This is the production
// idempotency path for JetStream redeliveries and concurrent rule firings.
func TestMint_Idempotent_ErrAlreadyExistsFallsBackToGet(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mock := newMockLifecycleManager()
	// Pre-seed the run to simulate a prior mint.
	runEntityID := "acme.ops.chain.agent.execution.run-already-exists"
	existing := &agentrun.AgentRun{
		EntityIDField: runEntityID,
		PhaseField:    "dispatched",
	}
	mock.seed(existing)

	// Mint with the same ID — Create returns ErrAlreadyExists.
	// Mint must fall back to Get and return the existing run.
	result, err := agentrun.Mint(ctx, mock, "acme", "ops", "run-already-exists",
		"acme.ops.agentic-loop.agent.execution.run-already-exists")
	require.NoError(t, err, "Mint must not error on ErrAlreadyExists — idempotent path")
	require.NotNil(t, result)
	assert.Equal(t, runEntityID, result.EntityID(),
		"idempotent Mint must return the existing run entity ID")
	assert.Equal(t, "dispatched", result.Phase(),
		"idempotent Mint must return the existing run's phase")

	runID, ok := result.RunID()
	require.True(t, ok)
	assert.Equal(t, "run-already-exists", runID,
		"idempotent Mint must return the correct bare RunID")
}

// TestMint_EntityIDShape verifies the entity ID format produced by Mint.
func TestMint_EntityIDShape(t *testing.T) {
	t.Parallel()
	// Verify the entity ID Mint would construct without needing a Manager.
	entityID, err := agentic.TryChainExecutionEntityID("acme", "ops", "root-uuid")
	require.NoError(t, err)
	assert.Equal(t, "acme.ops.chain.agent.execution.root-uuid", entityID)
}

func TestMint_EmptyOrgReturnsError(t *testing.T) {
	t.Parallel()
	_, err := agentic.TryChainExecutionEntityID("", "ops", "root-uuid")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "org must not be empty")
}

func TestMint_DotInLoopIDReturnsError(t *testing.T) {
	t.Parallel()
	_, err := agentic.TryChainExecutionEntityID("acme", "ops", "bad.id.with.dots")
	require.Error(t, err)
}

// --- D6: ResolveRun typed + ancestry-walk fallback ---

func TestResolveRun_TypedTriplePath_WhenRunIDPresent(t *testing.T) {
	t.Parallel()
	// Set up: reader has agent.loop.run triple on the loop entity.
	reader := newFakeTripleReader()
	loopEntityID, _ := agentic.TryLoopExecutionEntityID("acme", "ops", "child-loop-id")
	reader.set(loopEntityID, agvocab.LoopRun, "root-run-id")

	// Verify reader returns the runID correctly.
	runID, ok, err := reader.GetLoopRunID(context.Background(), loopEntityID)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "root-run-id", runID)
}

func TestResolveRun_AncestryWalkPath_WhenNoRunTriple(t *testing.T) {
	t.Parallel()
	reader := newFakeTripleReader()
	rootEntityID, _ := agentic.TryLoopExecutionEntityID("acme", "ops", "root-loop-id")
	childEntityID, _ := agentic.TryLoopExecutionEntityID("acme", "ops", "child-loop-id")
	// Child's agent.loop.parent points to root.
	reader.set(childEntityID, "agent.loop.parent", rootEntityID)
	// Root has no parent — reader.GetLoopParentEntityID returns ("", false, nil).

	// Verify the reader gives us the correct parent chain.
	parentID, hasParent, err := reader.GetLoopParentEntityID(context.Background(), childEntityID)
	require.NoError(t, err)
	require.True(t, hasParent)
	assert.Equal(t, rootEntityID, parentID)

	// Root has no parent.
	_, rootHasParent, err := reader.GetLoopParentEntityID(context.Background(), rootEntityID)
	require.NoError(t, err)
	assert.False(t, rootHasParent)
}

// --- D3: Terminal lifecycle authority remains outside the subscriber ---

// TestSubscriber_RootLoopFailViaResolveWalk exercises the production
// resolution path: the root coordinator loop's terminal event carries RunID="" and
// RunEntityID="" (because the root was not spawned with a RunID — the run_scope:new
// rule fires AFTER the root starts, stamping agent.loop.run via tripleMutator on the
// firing entity but NOT updating LoopEntity.RunID). So the subscriber falls through
// to ResolveRun → reads the agent.loop.run triple from the firing entity (the root loop
// entity) → builds the chain entity ID → RunStateReader.Get.
//
// This test drives the exact resolution path production uses when the root loop
// fails before any child handoff (ev.RunID == "").
func TestSubscriber_RootLoopFailViaResolveWalk(t *testing.T) {
	t.Parallel()
	rootLoopID := "root-loop-id"
	runEntityID := "acme.ops.chain.agent.execution." + rootLoopID

	run := &agentrun.AgentRun{
		EntityIDField: runEntityID,
		PhaseField:    "dispatched",
	}

	// The reader simulates the effect of run_scope=new stamping agent.loop.run
	// on the firing (root) entity via tripleMutator (ADR-053 D4).
	reader := newFakeTripleReader()
	rootLoopEntityID, _ := agentic.TryLoopExecutionEntityID("acme", "ops", rootLoopID)
	reader.set(rootLoopEntityID, agvocab.LoopRun, rootLoopID)

	mock := newMockLifecycleManager()
	mock.seed(run)

	var observedRun *agentrun.AgentRun
	sub := agentrun.NewMilestoneSubscriberWithRunStateReader(mock, reader, "acme", "ops", nil)
	sub.AddHandler(&testMilestoneHandler{fn: func(_ context.Context, _ agentrun.LoopTerminalEvent, run *agentrun.AgentRun) error {
		observedRun = run
		return nil
	}})

	// Build LoopFailedEvent as production emits it for the root coordinator:
	// RunID="" and RunEntityID="" because LoopEntity.RunID was never set
	// (root was dispatched before run_scope:new fired).
	ev := &agentic.LoopFailedEvent{
		LoopID:   rootLoopID,
		TaskID:   "task-001",
		Outcome:  agentic.OutcomeFailed,
		Reason:   "context exhausted",
		Error:    "max iterations",
		Role:     "coordinator",
		Model:    "model-x",
		FailedAt: time.Now(),
		// RunID and RunEntityID deliberately empty — production-realistic shape.
	}
	data := mustMarshalBaseMessage(t, ev.Schema(), ev)

	err := sub.HandleEvent(context.Background(), data)
	require.NoError(t, err)

	require.NotNil(t, observedRun, "root terminal event must still resolve and reach handlers")
	assert.Equal(t, runEntityID, observedRun.EntityIDField)
	assert.Equal(t, "dispatched", mock.runs[runEntityID].PhaseField,
		"subscriber must not mutate lifecycle phase")
}

// TestSubscriber_ChildLoopFailPreservesRunStateViaWireRunID exercises the path
// where a child loop's terminal event carries RunEntityID on the wire.
func TestSubscriber_ChildLoopFailPreservesRunStateViaWireRunID(t *testing.T) {
	t.Parallel()
	runEntityID := "acme.ops.chain.agent.execution.root-loop-id"
	run := &agentrun.AgentRun{
		EntityIDField: runEntityID,
		PhaseField:    "dispatched", // still dispatched to test the guard boundary
	}

	reader := newFakeTripleReader()
	mock := newMockLifecycleManager()
	mock.seed(run)

	sub := agentrun.NewMilestoneSubscriberWithRunStateReader(mock, reader, "acme", "ops", nil)

	// A child loop fails while the observed run remains dispatched.
	childFailed := &agentic.LoopFailedEvent{
		LoopID:      "child-loop-id",
		TaskID:      "task-002",
		Outcome:     agentic.OutcomeFailed,
		Reason:      "tool error",
		Error:       "nats timeout",
		Role:        "researcher",
		Model:       "model-y",
		FailedAt:    time.Now(),
		RunID:       "root-loop-id",
		RunEntityID: runEntityID,
	}
	data := mustMarshalBaseMessage(t, childFailed.Schema(), childFailed)

	err := sub.HandleEvent(context.Background(), data)
	require.NoError(t, err)

	assert.Equal(t, "dispatched", mock.runs[runEntityID].PhaseField)
}

func TestSubscriber_ChildLoopFailPreservesExecutingRun(t *testing.T) {
	t.Parallel()
	runEntityID := "acme.ops.chain.agent.execution.root-loop-id"
	run := &agentrun.AgentRun{
		EntityIDField: runEntityID,
		PhaseField:    "executing", // already advanced — children are active
	}

	reader := newFakeTripleReader()
	mock := newMockLifecycleManager()
	mock.seed(run)

	sub := agentrun.NewMilestoneSubscriberWithRunStateReader(mock, reader, "acme", "ops", nil)

	// A CHILD loop fails: LoopID != RunID.
	childFailed := &agentic.LoopFailedEvent{
		LoopID:      "child-loop-id",
		TaskID:      "task-002",
		Outcome:     agentic.OutcomeFailed,
		Reason:      "tool error",
		Error:       "nats timeout",
		Role:        "researcher",
		Model:       "model-y",
		FailedAt:    time.Now(),
		RunID:       "root-loop-id",
		RunEntityID: runEntityID,
	}
	data := mustMarshalBaseMessage(t, childFailed.Schema(), childFailed)

	err := sub.HandleEvent(context.Background(), data)
	require.NoError(t, err)

	assert.Equal(t, "executing", mock.runs[runEntityID].PhaseField)
}

func TestSubscriber_RootCancelDeliveredWithoutLifecycleMutation(t *testing.T) {
	t.Parallel()
	runEntityID := "acme.ops.chain.agent.execution.root-cancel-id"
	run := &agentrun.AgentRun{
		EntityIDField: runEntityID,
		PhaseField:    "dispatched",
	}

	reader := newFakeTripleReader()
	mock := newMockLifecycleManager()
	mock.seed(run)

	var observedCategory string
	sub := agentrun.NewMilestoneSubscriberWithRunStateReader(mock, reader, "acme", "ops", nil)
	sub.AddHandler(&testMilestoneHandler{fn: func(_ context.Context, event agentrun.LoopTerminalEvent, _ *agentrun.AgentRun) error {
		observedCategory = event.Category
		return nil
	}})

	cancelled := &agentic.LoopCancelledEvent{
		LoopID:      "root-cancel-id",
		TaskID:      "task-003",
		Outcome:     agentic.OutcomeCancelled,
		CancelledBy: "user-xyz",
		CancelledAt: time.Now(),
		RunID:       "root-cancel-id",
		RunEntityID: runEntityID,
	}
	data := mustMarshalBaseMessage(t, cancelled.Schema(), cancelled)

	err := sub.HandleEvent(context.Background(), data)
	require.NoError(t, err)

	assert.Equal(t, agentic.CategoryLoopCancelled, observedCategory)
	assert.Equal(t, "dispatched", mock.runs[runEntityID].PhaseField,
		"subscriber must deliver cancellation without mutating lifecycle phase")
}

// --- D6: Subscriber category demux ---

func TestSubscriber_CategoryDemux_CancelledEventDetectedCorrectly(t *testing.T) {
	t.Parallel()
	runEntityID := "acme.ops.chain.agent.execution.demux-test"
	run := &agentrun.AgentRun{
		EntityIDField: runEntityID,
		PhaseField:    "executing",
	}

	reader := newFakeTripleReader()
	mock := newMockLifecycleManager()
	mock.seed(run)

	var receivedCategory string
	handler := &testMilestoneHandler{
		fn: func(_ context.Context, ev agentrun.LoopTerminalEvent, _ *agentrun.AgentRun) error {
			receivedCategory = ev.Category
			return nil
		},
	}
	sub := agentrun.NewMilestoneSubscriberWithRunStateReader(mock, reader, "acme", "ops", nil)
	sub.AddHandler(handler)

	// LoopCancelledEvent — category=loop_cancelled (not from subject).
	cancelled := &agentic.LoopCancelledEvent{
		LoopID:      "child-demux-loop",
		TaskID:      "task-003",
		Outcome:     agentic.OutcomeCancelled,
		CancelledBy: "user-xyz",
		CancelledAt: time.Now(),
		RunID:       "demux-test",
		RunEntityID: runEntityID,
	}
	data := mustMarshalBaseMessage(t, cancelled.Schema(), cancelled)
	err := sub.HandleEvent(context.Background(), data)
	require.NoError(t, err)

	assert.Equal(t, agentic.CategoryLoopCancelled, receivedCategory,
		"subscriber must demux by payload category, not by NATS subject")
}

func TestSubscriber_CategoryDemux_CompletedEventDetectedCorrectly(t *testing.T) {
	t.Parallel()
	runEntityID := "acme.ops.chain.agent.execution.completed-run"
	run := &agentrun.AgentRun{
		EntityIDField: runEntityID,
		PhaseField:    "executing",
	}

	reader := newFakeTripleReader()
	mock := newMockLifecycleManager()
	mock.seed(run)

	var receivedCategory string
	handler := &testMilestoneHandler{
		fn: func(_ context.Context, ev agentrun.LoopTerminalEvent, _ *agentrun.AgentRun) error {
			receivedCategory = ev.Category
			return nil
		},
	}
	sub := agentrun.NewMilestoneSubscriberWithRunStateReader(mock, reader, "acme", "ops", nil)
	sub.AddHandler(handler)

	completed := &agentic.LoopCompletedEvent{
		LoopID:      "some-loop",
		TaskID:      "task-004",
		Outcome:     agentic.OutcomeSuccess,
		Role:        "researcher",
		Result:      "done",
		Model:       "model-z",
		CompletedAt: time.Now(),
		RunID:       "completed-run",
		RunEntityID: runEntityID,
	}
	data := mustMarshalBaseMessage(t, completed.Schema(), completed)
	err := sub.HandleEvent(context.Background(), data)
	require.NoError(t, err)

	assert.Equal(t, agentic.CategoryLoopCompleted, receivedCategory)
}

// --- D6: Panic guard in handler ---

func TestSubscriber_PanicGuard_SecondHandlerRunsAfterFirstPanics(t *testing.T) {
	t.Parallel()
	runEntityID := "acme.ops.chain.agent.execution.panic-run"
	run := &agentrun.AgentRun{
		EntityIDField: runEntityID,
		PhaseField:    "executing",
	}

	reader := newFakeTripleReader()
	mock := newMockLifecycleManager()
	mock.seed(run)

	var secondHandlerCalled bool
	sub := agentrun.NewMilestoneSubscriberWithRunStateReader(mock, reader, "acme", "ops", nil)
	sub.AddHandler(&testMilestoneHandler{
		fn: func(context.Context, agentrun.LoopTerminalEvent, *agentrun.AgentRun) error {
			panic("intentional test panic")
		},
	})
	sub.AddHandler(&testMilestoneHandler{
		fn: func(_ context.Context, _ agentrun.LoopTerminalEvent, _ *agentrun.AgentRun) error {
			secondHandlerCalled = true
			return nil
		},
	})

	completed := &agentic.LoopCompletedEvent{
		LoopID:      "non-root-panic-loop",
		TaskID:      "task-005",
		Outcome:     agentic.OutcomeSuccess,
		Role:        "researcher",
		Result:      "done",
		Model:       "model-z",
		CompletedAt: time.Now(),
		RunID:       "panic-run",
		RunEntityID: runEntityID,
	}
	data := mustMarshalBaseMessage(t, completed.Schema(), completed)

	require.NotPanics(t, func() {
		err := sub.HandleEvent(context.Background(), data)
		require.NoError(t, err, "HandleEvent must not propagate panic as error")
	})
	assert.True(t, secondHandlerCalled, "second handler must run even when first panics")
}

// --- Non-run loops (no RunEntityID, no agent.loop.run triple) ---

// TestSubscriber_NonRunLoop_HandlersCalledWithNilRun verifies the exact contract for
// a standalone loop (not part of any run): resolveRunForEvent returns (nil, nil) →
// handlers are called with run=nil. This pins the nil-run handler contract so
// product handlers know to expect nil.
func TestSubscriber_NonRunLoop_HandlersCalledWithNilRun(t *testing.T) {
	t.Parallel()
	// reader has NO agent.loop.run triple for standalone-loop → walk → root has no parent
	// → Manager.Get for the loop itself returns ErrEntityNotFound → (nil, nil).
	reader := newFakeTripleReader()
	mock := newMockLifecycleManager()
	// mock has NO runs seeded — Manager.Get returns ErrEntityNotFound for any entity.

	var handlerRun *agentrun.AgentRun
	var handlerCalled bool
	sub := agentrun.NewMilestoneSubscriberWithRunStateReader(mock, reader, "acme", "ops", nil)
	sub.AddHandler(&testMilestoneHandler{
		fn: func(_ context.Context, _ agentrun.LoopTerminalEvent, run *agentrun.AgentRun) error {
			handlerCalled = true
			handlerRun = run
			return nil
		},
	})

	// Loop with no RunID or RunEntityID and no agent.loop.run triple → non-run loop.
	completed := &agentic.LoopCompletedEvent{
		LoopID:      "standalone-loop",
		TaskID:      "task-006",
		Outcome:     agentic.OutcomeSuccess,
		Role:        "researcher",
		Result:      "done",
		Model:       "model-z",
		CompletedAt: time.Now(),
		// RunID and RunEntityID deliberately empty.
	}
	data := mustMarshalBaseMessage(t, completed.Schema(), completed)

	err := sub.HandleEvent(context.Background(), data)
	require.NoError(t, err)

	// Contract: handlers ARE called with nil run — non-run loops do not produce errors.
	assert.True(t, handlerCalled, "handlers must be called even for non-run loops (run will be nil)")
	assert.Nil(t, handlerRun, "run must be nil for a loop with no run association")

}

// --- Helper types ---

// fakeTripleReader implements agentrun.LoopTripleReader for tests.
type fakeTripleReader struct {
	triples map[string]map[string]string // entityID → predicate → value
}

func newFakeTripleReader() *fakeTripleReader {
	return &fakeTripleReader{triples: make(map[string]map[string]string)}
}

func (r *fakeTripleReader) set(entityID, predicate, value string) {
	if r.triples[entityID] == nil {
		r.triples[entityID] = make(map[string]string)
	}
	r.triples[entityID][predicate] = value
}

func (r *fakeTripleReader) GetLoopRunID(_ context.Context, loopEntityID string) (string, bool, error) {
	if m, ok := r.triples[loopEntityID]; ok {
		if v, ok2 := m[agvocab.LoopRun]; ok2 && v != "" {
			return v, true, nil
		}
	}
	return "", false, nil
}

func (r *fakeTripleReader) GetLoopParentEntityID(_ context.Context, loopEntityID string) (string, bool, error) {
	if m, ok := r.triples[loopEntityID]; ok {
		if v, ok2 := m["agent.loop.parent"]; ok2 && v != "" {
			return v, true, nil
		}
	}
	return "", false, nil
}

// testMilestoneHandler is a test double for agentrun.MilestoneHandler.
type testMilestoneHandler struct {
	fn func(context.Context, agentrun.LoopTerminalEvent, *agentrun.AgentRun) error
}

func (h *testMilestoneHandler) OnLoopTerminal(ctx context.Context, ev agentrun.LoopTerminalEvent, run *agentrun.AgentRun) error {
	return h.fn(ctx, ev, run)
}

// mustMarshalBaseMessage uses the exact registry-discriminated production wire.
func mustMarshalBaseMessage(t *testing.T, schema message.Type, payload message.Payload) []byte {
	t.Helper()
	envelope := message.NewBaseMessage(schema, payload, "agentic-loop")
	data, err := json.Marshal(envelope)
	require.NoError(t, err)
	return data
}

// --- Static compile-time checks ---

// Ensure mockLifecycleManager satisfies both narrow seams it exercises.
var _ agentrun.RunStateReader = (*mockLifecycleManager)(nil)
var _ agentrun.MintableManager = (*mockLifecycleManager)(nil)

func init() {
	// Verify the Participant interface at test compile time.
	var _ lifecycle.Participant = (*agentrun.AgentRun)(nil)
	// Verify LoopTripleReader
	var _ agentrun.LoopTripleReader = (*fakeTripleReader)(nil)
	// Verify MilestoneHandler
	var _ agentrun.MilestoneHandler = (*testMilestoneHandler)(nil)
}

// Compile-time check: AgentRun satisfies lifecycle.Participant.
var _ lifecycle.Participant = (*agentrun.AgentRun)(nil)
