package researchassess

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semstreams/agentic/research"
	"github.com/c360studio/semstreams/pkg/fusion"
)

// fakeLoopStore replaces the natsLoopStore for handler integration
// tests. Records all reads + writes so the test can assert on key
// ordering, envelope shape, and miss vs hit paths.
type fakeLoopStore struct {
	mu sync.Mutex

	intent    *research.Intent
	intentErr error

	exec    *research.ExecutionOutput
	execErr error

	getIntentCalls int
	getExecCalls   int

	snapshotEnvelope []byte
	snapshotErr      error

	assessEnvelope []byte
	assessErr      error

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

func (s *fakeLoopStore) PutSnapshot(_ context.Context, _ string, envelope []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshotEnvelope = append([]byte(nil), envelope...)
	s.writeOrder = append(s.writeOrder, "snapshot")
	return s.snapshotErr
}

func (s *fakeLoopStore) PutAssessmentOutput(_ context.Context, _ string, envelope []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.assessEnvelope = append([]byte(nil), envelope...)
	s.writeOrder = append(s.writeOrder, "assess_complete")
	return s.assessErr
}

func newTestComponent(loops LoopStore, assessor Assessor) *Component {
	return &Component{
		config:   DefaultConfig(),
		assessor: assessor,
		loops:    loops,
		logger:   quietLogger(),
	}
}

func TestComponent_HandleMessage_SufficientHappyPath(t *testing.T) {
	loops := &fakeLoopStore{
		intent: &research.Intent{Topic: "test.research.graph.assess.entity.drone-001 maintenance events"},
		exec: &research.ExecutionOutput{
			Topic:    "test.research.graph.assess.entity.drone-001 maintenance events",
			Action:   research.ActionWalkSeeds,
			Evidence: []fusion.Evidence{{EntityID: "test.research.graph.assess.entity.drone-001", Tier: "0", Source: "x", Score: 0.9}},
		},
	}
	a := &fakeAssessor{
		content: `{"sufficient":true,"rationale":"covered","confidence":0.8}`,
	}
	c := newTestComponent(loops, a)
	c.handleMessage(context.Background(), "component.assess_sufficiency.loop-xyz", nil)

	if atomic.LoadInt64(&c.messagesProcessed) != 1 {
		t.Errorf("messagesProcessed = %d, want 1", atomic.LoadInt64(&c.messagesProcessed))
	}
	if atomic.LoadInt64(&c.errors) != 0 {
		t.Errorf("errors = %d, want 0", atomic.LoadInt64(&c.errors))
	}
	if atomic.LoadInt64(&c.messagesEmitted) != 1 {
		t.Errorf("messagesEmitted = %d, want 1", atomic.LoadInt64(&c.messagesEmitted))
	}
	if loops.getIntentCalls != 1 || loops.getExecCalls != 1 {
		t.Errorf("read calls: intent=%d, exec=%d", loops.getIntentCalls, loops.getExecCalls)
	}
	if len(loops.writeOrder) != 2 || loops.writeOrder[0] != "snapshot" || loops.writeOrder[1] != "assess_complete" {
		t.Errorf("write order = %v, want [snapshot assess_complete]", loops.writeOrder)
	}
	// Surface-level envelope assertions — payload-level decoding is
	// covered in the round-trip tests in agentic/research. Here we
	// just verify the wire bytes carry the expected fields.
	if !strings.Contains(string(loops.assessEnvelope), `"sufficient":true`) {
		t.Error("envelope should carry sufficient:true")
	}
	if !strings.Contains(string(loops.assessEnvelope), `"evidence_count":1`) {
		t.Error("envelope should carry evidence_count:1 (echoes upstream Evidence len)")
	}
	if !strings.Contains(string(loops.assessEnvelope), `"topic":"test.research.graph.assess.entity.drone-001 maintenance events"`) {
		t.Error("envelope should carry topic echoed from intent")
	}
}

func TestComponent_HandleMessage_RefinePath(t *testing.T) {
	loops := &fakeLoopStore{
		intent: &research.Intent{Topic: "fleet status"},
		exec: &research.ExecutionOutput{
			Topic:    "fleet status",
			Action:   research.ActionDecompose,
			Evidence: []fusion.Evidence{{EntityID: "test.research.graph.assess.entity.drone-001", Tier: "0", Source: "x"}},
		},
	}
	a := &fakeAssessor{
		content: `{"sufficient":false,"refined_queries":["all drones","alerts last 24h"],"confidence":0.4}`,
	}
	c := newTestComponent(loops, a)
	c.handleMessage(context.Background(), "component.assess_sufficiency.loop-1", nil)
	if atomic.LoadInt64(&c.messagesEmitted) != 1 {
		t.Errorf("emitted = %d, want 1", atomic.LoadInt64(&c.messagesEmitted))
	}
	if !strings.Contains(string(loops.assessEnvelope), `"sufficient":false`) {
		t.Error("envelope should carry sufficient:false")
	}
	if !strings.Contains(string(loops.assessEnvelope), "all drones") {
		t.Error("envelope should carry refined_queries")
	}
}

func TestComponent_HandleMessage_IgnoresBadSubject(t *testing.T) {
	loops := &fakeLoopStore{}
	a := &fakeAssessor{}
	c := newTestComponent(loops, a)
	c.handleMessage(context.Background(), "some.other.subject", nil)

	if atomic.LoadInt64(&c.errors) != 1 {
		t.Errorf("errors = %d, want 1 (bad-subject branch)", atomic.LoadInt64(&c.errors))
	}
	if loops.getIntentCalls != 0 {
		t.Errorf("getIntentCalls = %d, want 0 (bad subject should short-circuit)", loops.getIntentCalls)
	}
	if a.called {
		t.Error("assessor should not be called on bad subject")
	}
}

func TestComponent_HandleMessage_IntentMissingHardAborts(t *testing.T) {
	loops := &fakeLoopStore{intentErr: errIntentNotFound}
	a := &fakeAssessor{}
	c := newTestComponent(loops, a)
	c.handleMessage(context.Background(), "component.assess_sufficiency.loop-1", nil)

	if atomic.LoadInt64(&c.errors) != 1 {
		t.Errorf("errors = %d, want 1", atomic.LoadInt64(&c.errors))
	}
	if len(loops.writeOrder) != 0 {
		t.Errorf("intent missing should NOT emit envelope; writeOrder = %v", loops.writeOrder)
	}
	if a.called {
		t.Error("assessor should not be called when intent missing")
	}
}

func TestComponent_HandleMessage_ExecMissingEmitsDegraded(t *testing.T) {
	loops := &fakeLoopStore{
		intent:  &research.Intent{Topic: "x"},
		execErr: errExecutionOutputNotFound,
	}
	a := &fakeAssessor{}
	c := newTestComponent(loops, a)
	c.handleMessage(context.Background(), "component.assess_sufficiency.loop-1", nil)

	if atomic.LoadInt64(&c.messagesEmitted) != 1 {
		t.Errorf("messagesEmitted = %d, want 1 (degraded envelope still emitted)", atomic.LoadInt64(&c.messagesEmitted))
	}
	if !strings.Contains(string(loops.assessEnvelope), `"degraded":true`) {
		t.Error("envelope should carry degraded:true")
	}
	if !strings.Contains(string(loops.assessEnvelope), `"sufficient":false`) {
		t.Error("degraded envelope should default sufficient:false (refine safety net)")
	}
	if a.called {
		t.Error("assessor should not be called when exec missing")
	}
}

func TestComponent_HandleMessage_AssessorErrorEmitsDegraded(t *testing.T) {
	loops := &fakeLoopStore{
		intent: &research.Intent{Topic: "x"},
		exec:   &research.ExecutionOutput{Topic: "x", Action: research.ActionDecompose, Evidence: []fusion.Evidence{{EntityID: "test.research.graph.assess.entity.e", Tier: "0", Source: "s"}}},
	}
	a := &fakeAssessor{content: `not a JSON object at all`}
	c := newTestComponent(loops, a)
	c.handleMessage(context.Background(), "component.assess_sufficiency.loop-1", nil)

	if atomic.LoadInt64(&c.messagesEmitted) != 1 {
		t.Errorf("messagesEmitted = %d, want 1 (degraded envelope on assessor failure)", atomic.LoadInt64(&c.messagesEmitted))
	}
	if !strings.Contains(string(loops.assessEnvelope), `"degraded":true`) {
		t.Error("assessor-failure envelope should carry degraded:true")
	}
}
