package researchexecute

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semstreams/agentic/research"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/internal/graphmutation"
	"github.com/c360studio/semstreams/pkg/fusion"
)

// fakeLoopStore replaces natsLoopStore for handleMessage tests.
// Records reads + writes so the test can assert key ordering,
// envelope shape, and miss vs hit paths.
type fakeLoopStore struct {
	mu sync.Mutex

	intent           *research.Intent
	intentErr        error
	classifierOut    *research.ClassifierOutput
	classifierOutErr error
	decision         *research.RouteDecision
	decisionErr      error

	getIntentCalls     int
	getClassifierCalls int
	getDecisionCalls   int

	snapshotEnvelope []byte
	snapshotErr      error
	executeEnvelope  []byte
	executeErr       error

	writeOrder []string
}

func (s *fakeLoopStore) GetIntent(_ context.Context, _ string) (*research.Intent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getIntentCalls++
	if s.intentErr != nil {
		return nil, s.intentErr
	}
	return s.intent, nil
}

func (s *fakeLoopStore) GetClassifierOutput(_ context.Context, _ string) (*research.ClassifierOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getClassifierCalls++
	if s.classifierOutErr != nil {
		return nil, s.classifierOutErr
	}
	return s.classifierOut, nil
}

func (s *fakeLoopStore) GetRouteDecision(_ context.Context, _ string) (*research.RouteDecision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getDecisionCalls++
	if s.decisionErr != nil {
		return nil, s.decisionErr
	}
	return s.decision, nil
}

func (s *fakeLoopStore) PutSnapshot(_ context.Context, _ string, envelope []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshotEnvelope = append([]byte(nil), envelope...)
	s.writeOrder = append(s.writeOrder, "snapshot")
	return s.snapshotErr
}

func (s *fakeLoopStore) PutExecutionOutput(_ context.Context, _ string, envelope []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.executeEnvelope = append([]byte(nil), envelope...)
	s.writeOrder = append(s.writeOrder, "execute_complete")
	return s.executeErr
}

func newTestComponent(loops LoopStore, gq GraphQueryClient) *Component {
	config := DefaultConfig()
	inputs, outputs := mustResolveTestPorts(config.Ports)
	return &Component{
		config:  config,
		inputs:  inputs,
		outputs: outputs,
		loops:   loops,
		gq:      gq,
		logger:  quietLogger(),
	}
}

func mustResolveTestPorts(config *component.PortConfig) ([]component.Port, []component.Port) {
	inputs := make([]component.Port, len(config.Inputs))
	for index, definition := range config.Inputs {
		port, err := definition.Resolve(component.DirectionInput)
		if err != nil {
			panic(err)
		}
		inputs[index] = port
	}
	outputs := make([]component.Port, len(config.Outputs))
	for index, definition := range config.Outputs {
		port, err := definition.Resolve(component.DirectionOutput)
		if err != nil {
			panic(err)
		}
		outputs[index] = port
	}
	return inputs, outputs
}

// --- handleMessage happy paths ---

func TestComponent_HandleMessage_WalkSeedsHappyPath(t *testing.T) {
	loops := &fakeLoopStore{
		intent: &research.Intent{Topic: "drone status"},
		classifierOut: &research.ClassifierOutput{
			Topic: "drone status", Tier: "0",
			Candidates: []research.Candidate{
				{EntityID: "acme.ops.robotics.gcs.drone.001", Label: "Drone One", Tier: "0", Source: "x"},
			},
		},
		decision: &research.RouteDecision{
			Action: research.ActionWalkSeeds,
			Args: map[string]any{
				"seeds": []any{
					map[string]any{"ref": "drone.001", "ref_type": research.SeedRefTypePartialID},
				},
			},
		},
	}
	gq := &fakeGraphQuery{
		entityStateOut: []fusion.Evidence{{EntityID: "acme.ops.robotics.gcs.drone.001"}},
		predicateWalkOut: []fusion.Evidence{
			{EntityID: "acme.ops.robotics.gcs.event.maint-001", Score: 0.7},
		},
		bm25Out: []fusion.Evidence{{EntityID: "acme.ops.robotics.gcs.alert.battery", Score: 0.5}},
	}
	c := newTestComponent(loops, gq)
	c.handleMessage(context.Background(), "component.execute_subqueries.loop-1", nil)

	if atomic.LoadInt64(&c.errors) != 0 {
		t.Errorf("errors = %d, want 0", atomic.LoadInt64(&c.errors))
	}
	if atomic.LoadInt64(&c.messagesEmitted) != 1 {
		t.Errorf("messagesEmitted = %d, want 1", atomic.LoadInt64(&c.messagesEmitted))
	}
	if len(loops.writeOrder) != 2 || loops.writeOrder[0] != "snapshot" || loops.writeOrder[1] != "execute_complete" {
		t.Errorf("write order = %v, want [snapshot execute_complete]", loops.writeOrder)
	}
}

func TestComponent_HandleMessage_DecomposeHappyPath(t *testing.T) {
	loops := &fakeLoopStore{
		intent: &research.Intent{Topic: "sensor anomalies"},
		classifierOut: &research.ClassifierOutput{
			Topic: "sensor anomalies", Tier: "1",
			Candidates: []research.Candidate{
				{EntityID: "test.research.graph.execute.entity.sensor-001", Tier: "0", Source: "x"},
			},
		},
		decision: &research.RouteDecision{
			Action: research.ActionDecompose,
			Args: map[string]any{
				"axes":  []any{"entity_type", "time"},
				"focus": "sensor maintenance",
				"scope": research.DecomposeScopeMedium,
			},
		},
	}
	gq := &fakeGraphQuery{
		entityStateOut:   []fusion.Evidence{{EntityID: "test.research.graph.execute.entity.sensor-001"}},
		temporalRangeOut: []fusion.Evidence{{EntityID: "test.research.graph.execute.entity.event-t1"}},
		bm25Out:          []fusion.Evidence{{EntityID: "test.research.graph.execute.entity.doc-abc", Score: 0.3}},
	}
	c := newTestComponent(loops, gq)
	c.handleMessage(context.Background(), "component.execute_subqueries.loop-2", nil)

	if atomic.LoadInt64(&c.errors) != 0 {
		t.Errorf("errors = %d, want 0", atomic.LoadInt64(&c.errors))
	}
	if atomic.LoadInt64(&c.messagesEmitted) != 1 {
		t.Errorf("messagesEmitted = %d, want 1", atomic.LoadInt64(&c.messagesEmitted))
	}
	// Should have hit all three executors (entity_state + temporal + bm25).
	if gq.entityStateCalls != 1 || gq.temporalRangeCalls != 1 || gq.bm25Calls != 1 {
		t.Errorf("call counts: es=%d temporal=%d bm=%d, want all 1", gq.entityStateCalls, gq.temporalRangeCalls, gq.bm25Calls)
	}
}

// --- handleMessage failure paths ---

func TestComponent_HandleMessage_BadSubjectShortCircuits(t *testing.T) {
	loops := &fakeLoopStore{}
	c := newTestComponent(loops, &fakeGraphQuery{})
	c.handleMessage(context.Background(), "wrong.prefix.loop-1", nil)
	if atomic.LoadInt64(&c.errors) != 1 {
		t.Errorf("errors = %d, want 1", atomic.LoadInt64(&c.errors))
	}
	if loops.getIntentCalls != 0 {
		t.Errorf("bad subject should short-circuit before any KV reads")
	}
}

func TestComponent_HandleMessage_MissingIntentIsRecorded(t *testing.T) {
	loops := &fakeLoopStore{intentErr: errIntentNotFound}
	c := newTestComponent(loops, &fakeGraphQuery{})
	c.handleMessage(context.Background(), "component.execute_subqueries.loop-1", nil)
	if atomic.LoadInt64(&c.errors) != 1 {
		t.Errorf("errors = %d, want 1", atomic.LoadInt64(&c.errors))
	}
	if loops.getClassifierCalls != 0 {
		t.Errorf("missing intent should short-circuit before classifier read")
	}
}

func TestComponent_HandleMessage_MissingClassifierEmitsDegradedSoR3Fires(t *testing.T) {
	// Per the I3 fix from PR 4 review: missing classifier output
	// is a chain-race condition (R1 fired before classify.complete
	// was written). Hard-aborting here would strand the loop —
	// instead we emit a degraded envelope so R3 still fires and
	// assess_sufficiency can route to refine OR low-confidence
	// synthesize. Verify: errors++ AND degraded envelope written.
	loops := &fakeLoopStore{
		intent:           &research.Intent{Topic: "x"},
		classifierOutErr: errClassifierOutputMissing,
	}
	c := newTestComponent(loops, &fakeGraphQuery{})
	c.handleMessage(context.Background(), "component.execute_subqueries.loop-1", nil)
	if atomic.LoadInt64(&c.errors) != 1 {
		t.Errorf("errors = %d, want 1", atomic.LoadInt64(&c.errors))
	}
	if loops.getDecisionCalls != 0 {
		t.Errorf("missing classifier should short-circuit before decision read")
	}
	if len(loops.executeEnvelope) == 0 {
		t.Error("degraded trigger envelope not written; R3 would strand")
	}
}

func TestComponent_HandleMessage_MissingDecisionEmitsDegradedSoR3Fires(t *testing.T) {
	loops := &fakeLoopStore{
		intent:        &research.Intent{Topic: "x"},
		classifierOut: &research.ClassifierOutput{Topic: "x", Tier: "0"},
		decisionErr:   errRouteDecisionMissing,
	}
	c := newTestComponent(loops, &fakeGraphQuery{})
	c.handleMessage(context.Background(), "component.execute_subqueries.loop-1", nil)
	if atomic.LoadInt64(&c.errors) != 1 {
		t.Errorf("errors = %d, want 1", atomic.LoadInt64(&c.errors))
	}
	if len(loops.executeEnvelope) == 0 {
		t.Error("degraded trigger envelope not written; R3 would strand")
	}
}

func TestComponent_HandleMessage_UnexpectedActionEmitsDegradedOutput(t *testing.T) {
	// retighten + synthesize_directly shouldn't reach execute_subqueries
	// per R2's `when` gating, but if they do (rule-config bug) the
	// component must NOT silently strand the loop — emits a degraded
	// envelope so R3 still fires.
	loops := &fakeLoopStore{
		intent:        &research.Intent{Topic: "x"},
		classifierOut: &research.ClassifierOutput{Topic: "x", Tier: "0"},
		decision:      &research.RouteDecision{Action: research.ActionRetighten, Args: map[string]any{"topic": "x"}},
	}
	c := newTestComponent(loops, &fakeGraphQuery{})
	c.handleMessage(context.Background(), "component.execute_subqueries.loop-1", nil)
	if atomic.LoadInt64(&c.errors) == 0 {
		t.Error("unexpected action should record an error")
	}
	// But the trigger key should still have been written.
	if len(loops.executeEnvelope) == 0 {
		t.Error("degraded trigger envelope not written; downstream chain would strand")
	}
}

func TestComponent_HandleMessage_BadWalkSeedsArgsEmitsDegraded(t *testing.T) {
	loops := &fakeLoopStore{
		intent:        &research.Intent{Topic: "x"},
		classifierOut: &research.ClassifierOutput{Topic: "x", Tier: "0"},
		decision: &research.RouteDecision{
			Action: research.ActionWalkSeeds,
			Args:   map[string]any{"seeds": []any{}}, // empty seeds — Parse will reject
		},
	}
	c := newTestComponent(loops, &fakeGraphQuery{})
	c.handleMessage(context.Background(), "component.execute_subqueries.loop-1", nil)
	if atomic.LoadInt64(&c.errors) == 0 {
		t.Error("bad walk_seeds args should record an error")
	}
	if len(loops.executeEnvelope) == 0 {
		t.Error("degraded trigger envelope not written despite bad args")
	}
}

func TestComponent_HandleMessage_SnapshotErrorIsTolerated(t *testing.T) {
	loops := &fakeLoopStore{
		intent:        &research.Intent{Topic: "x"},
		classifierOut: &research.ClassifierOutput{Topic: "x", Tier: "0"},
		decision: &research.RouteDecision{
			Action: research.ActionDecompose,
			Args:   map[string]any{"axes": []any{"entity_type"}, "focus": "f"},
		},
		snapshotErr: errors.New("snapshot KV down"),
	}
	c := newTestComponent(loops, &fakeGraphQuery{})
	c.handleMessage(context.Background(), "component.execute_subqueries.loop-1", nil)
	if atomic.LoadInt64(&c.errors) != 0 {
		t.Errorf("snapshot error should be best-effort, errors = %d", atomic.LoadInt64(&c.errors))
	}
	if atomic.LoadInt64(&c.messagesEmitted) != 1 {
		t.Errorf("trigger should still fire despite snapshot failure")
	}
}

func TestComponent_HandleMessage_TriggerErrorIsRecorded(t *testing.T) {
	loops := &fakeLoopStore{
		intent:        &research.Intent{Topic: "x"},
		classifierOut: &research.ClassifierOutput{Topic: "x", Tier: "0"},
		decision: &research.RouteDecision{
			Action: research.ActionDecompose,
			Args:   map[string]any{"axes": []any{"entity_type"}, "focus": "f"},
		},
		executeErr: errors.New("execute KV down"),
	}
	c := newTestComponent(loops, &fakeGraphQuery{})
	c.handleMessage(context.Background(), "component.execute_subqueries.loop-1", nil)
	if atomic.LoadInt64(&c.errors) != 1 {
		t.Errorf("trigger write error should record, errors = %d", atomic.LoadInt64(&c.errors))
	}
}

// --- discoverable surface ---

func TestComponent_DiscoverableSurface(t *testing.T) {
	c := newTestComponent(&fakeLoopStore{}, &fakeGraphQuery{})
	if c.Meta().Name != ComponentName {
		t.Errorf("Meta.Name drift")
	}
	if len(c.InputPorts()) != 1 {
		t.Errorf("InputPorts: got %d, want 1", len(c.InputPorts()))
	}
	outputs := c.OutputPorts()
	if len(outputs) != 1 {
		t.Fatalf("OutputPorts: got %d, want canonical mutation port", len(outputs))
	}
	request, ok := outputs[0].Config.(component.NATSRequestPort)
	if !ok || !outputs[0].Required || request.Subject != graphmutation.SubjectFamily || request.Interface == nil ||
		request.Interface.Type != graphmutation.InterfaceType || request.Interface.Version != graphmutation.InterfaceVersion {
		t.Errorf("graph mutation output drift: %#v", outputs[0])
	}
}

// --- config validate / defaults ---

func TestConfig_ValidateRejectsNegativeCaps(t *testing.T) {
	c := DefaultConfig()
	c.MaxParallelism = -1
	if err := c.Validate(); err == nil {
		t.Error("Validate accepted negative max_parallelism")
	}
	c = DefaultConfig()
	c.MaxResultsPerSubquery = -1
	if err := c.Validate(); err == nil {
		t.Error("Validate accepted negative max_results_per_subquery")
	}
}

func TestConfig_ApplyDefaultsFillsZeros(t *testing.T) {
	c := Config{}
	c.ApplyDefaults()
	if c.LoopsBucket != "AGENT_LOOPS" || c.ExecuteTimeout != DefaultExecuteTimeout || c.MaxParallelism != DefaultMaxParallelism || c.MaxResultsPerSubquery != DefaultMaxResultsPerSubquery {
		t.Errorf("defaults not applied: %+v", c)
	}
}
