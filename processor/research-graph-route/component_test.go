package researchroute

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semstreams/agentic/research"
)

// fakeLoopStore replaces the natsLoopStore for handler integration
// tests. Records all reads + writes so the test can assert on key
// ordering, envelope shape, and miss vs hit paths.
type fakeLoopStore struct {
	mu sync.Mutex

	intent    *research.Intent
	intentErr error

	classifierOut    *research.ClassifierOutput
	classifierOutErr error

	getIntentCalls     int
	getClassifierCalls int

	snapshotEnvelope []byte
	snapshotErr      error

	routeEnvelope []byte
	routeErr      error

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

func (s *fakeLoopStore) PutSnapshot(_ context.Context, _ string, envelope []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshotEnvelope = append([]byte(nil), envelope...)
	s.writeOrder = append(s.writeOrder, "snapshot")
	return s.snapshotErr
}

func (s *fakeLoopStore) PutRouteDecision(_ context.Context, _ string, envelope []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.routeEnvelope = append([]byte(nil), envelope...)
	s.writeOrder = append(s.writeOrder, "route_complete")
	return s.routeErr
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newTestComponent constructs a Component with all injected
// dependencies stubbed. Skips Start — handleMessage doesn't touch
// deps once router/loops are set.
func newTestComponent(loops LoopStore, router Router) *Component {
	return &Component{
		config: DefaultConfig(),
		router: router,
		loops:  loops,
		logger: quietLogger(),
	}
}

func TestComponent_HandleMessage_HappyPath(t *testing.T) {
	loops := &fakeLoopStore{
		intent: &research.Intent{Topic: "drone-001 maintenance events"},
		classifierOut: &research.ClassifierOutput{
			Topic: "drone-001 maintenance events",
			Tier:  "0",
			Candidates: []research.Candidate{
				{EntityID: "drone-001", Label: "Drone 001", Relevance: 0.9, Tier: "0", Source: "x"},
			},
		},
	}
	router := &fakeRouter{
		content: `{"action":"walk_seeds","args":{"seeds":[{"ref":"drone-001","ref_type":"name"}]},"rationale":"seed found"}`,
	}
	c := newTestComponent(loops, router)
	c.handleMessage(context.Background(), "component.route_search.loop-123", nil)

	if atomic.LoadInt64(&c.messagesProcessed) != 1 {
		t.Errorf("messagesProcessed = %d, want 1", atomic.LoadInt64(&c.messagesProcessed))
	}
	if atomic.LoadInt64(&c.errors) != 0 {
		t.Errorf("errors = %d, want 0", atomic.LoadInt64(&c.errors))
	}
	if atomic.LoadInt64(&c.messagesEmitted) != 1 {
		t.Errorf("messagesEmitted = %d, want 1", atomic.LoadInt64(&c.messagesEmitted))
	}
	if loops.getIntentCalls != 1 || loops.getClassifierCalls != 1 {
		t.Errorf("read calls: intent=%d, classifier=%d", loops.getIntentCalls, loops.getClassifierCalls)
	}
	if len(loops.writeOrder) != 2 || loops.writeOrder[0] != "snapshot" || loops.writeOrder[1] != "route_complete" {
		t.Errorf("write order = %v, want [snapshot route_complete]", loops.writeOrder)
	}
	if len(loops.routeEnvelope) == 0 {
		t.Error("route envelope not written")
	}
}

func TestComponent_HandleMessage_IgnoresBadSubject(t *testing.T) {
	loops := &fakeLoopStore{}
	router := &fakeRouter{}
	c := newTestComponent(loops, router)
	c.handleMessage(context.Background(), "some.other.subject", nil)

	if atomic.LoadInt64(&c.errors) != 1 {
		t.Errorf("errors = %d, want 1 (bad-subject branch)", atomic.LoadInt64(&c.errors))
	}
	if loops.getIntentCalls != 0 {
		t.Errorf("getIntentCalls = %d, want 0 (bad subject should short-circuit)", loops.getIntentCalls)
	}
	if router.called {
		t.Error("router should not be called on bad subject")
	}
}

func TestComponent_HandleMessage_IntentNotFoundIsRecorded(t *testing.T) {
	loops := &fakeLoopStore{intentErr: errIntentNotFound}
	router := &fakeRouter{}
	c := newTestComponent(loops, router)
	c.handleMessage(context.Background(), "component.route_search.loop-1", nil)

	if atomic.LoadInt64(&c.errors) != 1 {
		t.Errorf("errors = %d, want 1", atomic.LoadInt64(&c.errors))
	}
	if router.called {
		t.Error("router called despite missing intent")
	}
}

func TestComponent_HandleMessage_ClassifierOutputNotFoundIsRecorded(t *testing.T) {
	loops := &fakeLoopStore{
		intent:           &research.Intent{Topic: "x"},
		classifierOutErr: errClassifierOutputNotFound,
	}
	router := &fakeRouter{}
	c := newTestComponent(loops, router)
	c.handleMessage(context.Background(), "component.route_search.loop-1", nil)

	if atomic.LoadInt64(&c.errors) != 1 {
		t.Errorf("errors = %d, want 1", atomic.LoadInt64(&c.errors))
	}
	if router.called {
		t.Error("router called despite missing classifier output")
	}
}

func TestComponent_HandleMessage_RouterErrorIsRecorded(t *testing.T) {
	loops := &fakeLoopStore{
		intent:        &research.Intent{Topic: "x"},
		classifierOut: &research.ClassifierOutput{Topic: "x", Tier: "0"},
	}
	router := &fakeRouter{err: errors.New("503 upstream")}
	c := newTestComponent(loops, router)
	c.handleMessage(context.Background(), "component.route_search.loop-1", nil)

	if atomic.LoadInt64(&c.errors) != 1 {
		t.Errorf("errors = %d, want 1", atomic.LoadInt64(&c.errors))
	}
	if atomic.LoadInt64(&c.messagesEmitted) != 0 {
		t.Errorf("messagesEmitted = %d, want 0 (router error short-circuits)", atomic.LoadInt64(&c.messagesEmitted))
	}
	if len(loops.writeOrder) != 0 {
		t.Errorf("write order = %v, want empty (router error short-circuits)", loops.writeOrder)
	}
}

func TestComponent_HandleMessage_SnapshotErrorIsTolerated(t *testing.T) {
	// Snapshot is best-effort; a failure should NOT block the
	// trigger write. Otherwise an operational glitch on the
	// snapshot key would silently drop loops mid-chain.
	loops := &fakeLoopStore{
		intent:        &research.Intent{Topic: "x"},
		classifierOut: &research.ClassifierOutput{Topic: "x", Tier: "0"},
		snapshotErr:   errors.New("write hiccup"),
	}
	router := &fakeRouter{content: `{"action":"synthesize_directly","args":{}}`}
	c := newTestComponent(loops, router)
	c.handleMessage(context.Background(), "component.route_search.loop-1", nil)

	if atomic.LoadInt64(&c.errors) != 0 {
		t.Errorf("errors = %d, want 0 (snapshot failure is best-effort)", atomic.LoadInt64(&c.errors))
	}
	if atomic.LoadInt64(&c.messagesEmitted) != 1 {
		t.Errorf("messagesEmitted = %d, want 1 (trigger should still fire)", atomic.LoadInt64(&c.messagesEmitted))
	}
	if len(loops.routeEnvelope) == 0 {
		t.Error("route envelope not written despite snapshot failure")
	}
}

func TestComponent_HandleMessage_TriggerWriteErrorIsRecorded(t *testing.T) {
	loops := &fakeLoopStore{
		intent:        &research.Intent{Topic: "x"},
		classifierOut: &research.ClassifierOutput{Topic: "x", Tier: "0"},
		routeErr:      errors.New("KV down"),
	}
	router := &fakeRouter{content: `{"action":"synthesize_directly","args":{}}`}
	c := newTestComponent(loops, router)
	c.handleMessage(context.Background(), "component.route_search.loop-1", nil)

	if atomic.LoadInt64(&c.errors) != 1 {
		t.Errorf("errors = %d, want 1 (trigger write failure is a real error)", atomic.LoadInt64(&c.errors))
	}
	if atomic.LoadInt64(&c.messagesEmitted) != 0 {
		t.Errorf("messagesEmitted = %d, want 0 (trigger failed)", atomic.LoadInt64(&c.messagesEmitted))
	}
}

// --- discoverable surface ---

func TestComponent_DiscoverableSurface(t *testing.T) {
	c := newTestComponent(&fakeLoopStore{}, &fakeRouter{})
	meta := c.Meta()
	if meta.Name != ComponentName {
		t.Errorf("Meta.Name = %q, want %q", meta.Name, ComponentName)
	}
	if meta.Type != "processor" {
		t.Errorf("Meta.Type = %q, want %q", meta.Type, "processor")
	}
	ports := c.InputPorts()
	if len(ports) != 1 {
		t.Errorf("InputPorts: got %d, want 1", len(ports))
	}
	if len(c.OutputPorts()) != 0 {
		t.Errorf("OutputPorts: got %d, want 0 (route_search writes via KV, not NATS)", len(c.OutputPorts()))
	}
}

// --- config validate / defaults ---

func TestConfig_ValidateRejectsNegativeCaps(t *testing.T) {
	c := DefaultConfig()
	c.MaxResponseTokens = -1
	if err := c.Validate(); err == nil {
		t.Error("Validate accepted negative max_response_tokens")
	}
	c = DefaultConfig()
	c.MaxCandidatesInPrompt = -1
	if err := c.Validate(); err == nil {
		t.Error("Validate accepted negative max_candidates_in_prompt")
	}
}

func TestConfig_ApplyDefaultsFillsZeros(t *testing.T) {
	c := Config{}
	c.ApplyDefaults()
	if c.LoopsBucket != "AGENT_LOOPS" {
		t.Errorf("LoopsBucket = %q, want AGENT_LOOPS", c.LoopsBucket)
	}
	if c.RouteTimeout != DefaultRouteTimeout {
		t.Errorf("RouteTimeout = %v, want %v", c.RouteTimeout, DefaultRouteTimeout)
	}
	if c.MaxResponseTokens != DefaultMaxResponseTokens {
		t.Errorf("MaxResponseTokens = %d, want %d", c.MaxResponseTokens, DefaultMaxResponseTokens)
	}
	if c.MaxCandidatesInPrompt != DefaultMaxCandidatesInPrompt {
		t.Errorf("MaxCandidatesInPrompt = %d, want %d", c.MaxCandidatesInPrompt, DefaultMaxCandidatesInPrompt)
	}
}
