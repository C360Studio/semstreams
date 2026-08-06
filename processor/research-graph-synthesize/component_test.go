package researchsynthesize

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semstreams/agentic/research"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/internal/graphmutation"
	"github.com/c360studio/semstreams/pkg/fusion"
)

func TestComponent_OutputPorts(t *testing.T) {
	c := newTestComponent(&fakeLoopStore{}, nil)
	outputs := c.OutputPorts()
	if len(outputs) != 1 {
		t.Fatalf("OutputPorts = %d, want canonical mutation port", len(outputs))
	}
	request, ok := outputs[0].Config.(component.NATSRequestPort)
	if !ok || !outputs[0].Required || request.Subject != graphmutation.SubjectFamily || request.Interface == nil ||
		request.Interface.Type != graphmutation.InterfaceType || request.Interface.Version != graphmutation.InterfaceVersion {
		t.Fatalf("graph mutation output drift: %#v", outputs[0])
	}
}

// fakeLoopStore replaces natsLoopStore. Records reads + writes so
// the test can assert on key ordering, envelope shape, and miss vs
// hit paths.
type fakeLoopStore struct {
	mu sync.Mutex

	intent    *research.Intent
	intentErr error

	exec    *research.ExecutionOutput
	execErr error

	route    *research.RouteDecision
	routeErr error

	getIntentCalls int
	getExecCalls   int
	getRouteCalls  int

	snapshotEnvelope []byte
	snapshotErr      error

	resultEnvelope []byte
	resultErr      error

	completionEnvelope []byte
	completionErr      error

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

func (s *fakeLoopStore) GetExecutionOutput(_ context.Context, _ string) (*research.ExecutionOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getExecCalls++
	if s.execErr != nil {
		return nil, s.execErr
	}
	return s.exec, nil
}

func (s *fakeLoopStore) GetRouteDecision(_ context.Context, _ string) (*research.RouteDecision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getRouteCalls++
	if s.routeErr != nil {
		return nil, s.routeErr
	}
	return s.route, nil
}

func (s *fakeLoopStore) PutSnapshot(_ context.Context, _ string, envelope []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshotEnvelope = append([]byte(nil), envelope...)
	s.writeOrder = append(s.writeOrder, "snapshot")
	return s.snapshotErr
}

func (s *fakeLoopStore) PutSearchResult(_ context.Context, _ string, envelope []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resultEnvelope = append([]byte(nil), envelope...)
	s.writeOrder = append(s.writeOrder, "search_result_complete")
	return s.resultErr
}

func (s *fakeLoopStore) PutLoopCompletion(_ context.Context, _ string, envelope []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.completionEnvelope = append([]byte(nil), envelope...)
	s.writeOrder = append(s.writeOrder, "loop_completion")
	return s.completionErr
}

func newTestComponent(loops LoopStore, synth Synthesizer) *Component {
	return &Component{
		config:      DefaultConfig(),
		synthesizer: synth,
		loops:       loops,
		logger:      quietLogger(),
	}
}

func TestComponent_HandleMessage_HappyPath(t *testing.T) {
	loops := &fakeLoopStore{
		intent: &research.Intent{Topic: "test.research.graph.synthesize.entity.drone-001 maintenance"},
		exec: &research.ExecutionOutput{
			Topic:    "test.research.graph.synthesize.entity.drone-001 maintenance",
			Action:   research.ActionWalkSeeds,
			Evidence: []fusion.Evidence{{EntityID: "test.research.graph.synthesize.entity.drone-001", Tier: "0", Source: "x", Score: 0.9}},
		},
		route: &research.RouteDecision{
			Action: research.ActionWalkSeeds,
			Args: map[string]any{
				"seeds": []map[string]string{{"ref": "test.research.graph.synthesize.entity.drone-001", "ref_type": "name"}},
			},
		},
	}
	s := &fakeSynthesizer{
		content: `{"synthesis":"Drone 001 has a maintenance window.","evidence_refs":["test.research.graph.synthesize.entity.drone-001"]}`,
	}
	c := newTestComponent(loops, s)
	c.handleMessage(context.Background(), "component.synthesize_answer.loop-9", nil)

	if atomic.LoadInt64(&c.messagesProcessed) != 1 {
		t.Errorf("messagesProcessed = %d, want 1", atomic.LoadInt64(&c.messagesProcessed))
	}
	if atomic.LoadInt64(&c.messagesEmitted) != 1 {
		t.Errorf("messagesEmitted = %d, want 1", atomic.LoadInt64(&c.messagesEmitted))
	}
	if loops.getIntentCalls != 1 || loops.getExecCalls != 1 || loops.getRouteCalls != 1 {
		t.Errorf("read calls: intent=%d exec=%d route=%d", loops.getIntentCalls, loops.getExecCalls, loops.getRouteCalls)
	}
	if len(loops.writeOrder) != 3 || loops.writeOrder[0] != "snapshot" || loops.writeOrder[1] != "search_result_complete" || loops.writeOrder[2] != "loop_completion" {
		t.Errorf("write order = %v, want [snapshot search_result_complete loop_completion]", loops.writeOrder)
	}
	if string(loops.completionEnvelope) != string(loops.resultEnvelope) {
		t.Errorf("loop_completion envelope must match search_result envelope (one source of truth for the SearchResult payload)")
	}
	if !strings.Contains(string(loops.resultEnvelope), `Drone 001`) {
		t.Error("envelope should carry synthesis prose")
	}
	if !strings.Contains(string(loops.resultEnvelope), `"router_action":"walk_seeds"`) {
		t.Error("envelope should carry DecompTrace.RouterAction")
	}
}

func TestComponent_HandleMessage_IgnoresBadSubject(t *testing.T) {
	loops := &fakeLoopStore{}
	s := &fakeSynthesizer{}
	c := newTestComponent(loops, s)
	c.handleMessage(context.Background(), "bad.subject", nil)

	if atomic.LoadInt64(&c.errors) != 1 {
		t.Errorf("errors = %d, want 1", atomic.LoadInt64(&c.errors))
	}
	if s.called {
		t.Error("synthesizer should not be called on bad subject")
	}
}

func TestComponent_HandleMessage_IntentMissingHardAborts(t *testing.T) {
	loops := &fakeLoopStore{intentErr: errIntentNotFound}
	s := &fakeSynthesizer{}
	c := newTestComponent(loops, s)
	c.handleMessage(context.Background(), "component.synthesize_answer.loop-x", nil)

	if atomic.LoadInt64(&c.errors) != 1 {
		t.Errorf("errors = %d, want 1", atomic.LoadInt64(&c.errors))
	}
	if len(loops.writeOrder) != 0 {
		t.Errorf("intent missing should NOT emit envelope; writeOrder = %v", loops.writeOrder)
	}
	if s.called {
		t.Error("synthesizer should not be called when intent missing")
	}
}

func TestComponent_HandleMessage_ExecMissingEmitsDegraded(t *testing.T) {
	loops := &fakeLoopStore{
		intent:  &research.Intent{Topic: "x"},
		execErr: errExecutionOutputNotFound,
	}
	s := &fakeSynthesizer{}
	c := newTestComponent(loops, s)
	c.handleMessage(context.Background(), "component.synthesize_answer.loop-x", nil)

	if atomic.LoadInt64(&c.messagesEmitted) != 1 {
		t.Errorf("messagesEmitted = %d, want 1 (degraded envelope still emitted)", atomic.LoadInt64(&c.messagesEmitted))
	}
	if !strings.Contains(string(loops.resultEnvelope), "could not be produced") {
		t.Error("envelope should carry the degraded-synthesis marker")
	}
	if s.called {
		t.Error("synthesizer should not be called when exec missing")
	}
}

func TestComponent_HandleMessage_RouteMissingProceeds(t *testing.T) {
	// Route nil + nil error → graceful pass to synthesizer.
	loops := &fakeLoopStore{
		intent: &research.Intent{Topic: "x"},
		exec: &research.ExecutionOutput{
			Topic:    "x",
			Action:   research.ActionSynthesizeDirectly,
			Evidence: []fusion.Evidence{{EntityID: "test.research.graph.synthesize.entity.e1", Tier: "0", Source: "x"}},
		},
		// route: nil
	}
	s := &fakeSynthesizer{
		content: `{"synthesis":"answer","evidence_refs":["test.research.graph.synthesize.entity.e1"]}`,
	}
	c := newTestComponent(loops, s)
	c.handleMessage(context.Background(), "component.synthesize_answer.loop-x", nil)

	if atomic.LoadInt64(&c.messagesEmitted) != 1 {
		t.Errorf("messagesEmitted = %d, want 1", atomic.LoadInt64(&c.messagesEmitted))
	}
	if !strings.Contains(string(loops.resultEnvelope), `"router_action":"synthesize_directly"`) {
		t.Error("DecompTrace should fall back to exec.Action when route missing")
	}
}

func TestComponent_HandleMessage_SynthesizerFailEmitsDegraded(t *testing.T) {
	loops := &fakeLoopStore{
		intent: &research.Intent{Topic: "x"},
		exec: &research.ExecutionOutput{
			Topic: "x", Action: research.ActionDecompose,
			Evidence: []fusion.Evidence{{EntityID: "test.research.graph.synthesize.entity.e1", Tier: "0", Source: "x"}},
		},
	}
	s := &fakeSynthesizer{content: `not JSON at all`}
	c := newTestComponent(loops, s)
	c.handleMessage(context.Background(), "component.synthesize_answer.loop-x", nil)

	if atomic.LoadInt64(&c.messagesEmitted) != 1 {
		t.Errorf("messagesEmitted = %d, want 1 (degraded envelope on synth failure)", atomic.LoadInt64(&c.messagesEmitted))
	}
	if !strings.Contains(string(loops.resultEnvelope), "could not be produced") {
		t.Error("envelope should carry the degraded-synthesis marker")
	}
}
