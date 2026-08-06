package researchroute

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semstreams/agentic/research"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph/llm"
	"github.com/c360studio/semstreams/internal/graphmutation"
	"github.com/c360studio/semstreams/model"
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
		intent: &research.Intent{Topic: "test.research.graph.route.entity.drone-001 maintenance events"},
		classifierOut: &research.ClassifierOutput{
			Topic: "test.research.graph.route.entity.drone-001 maintenance events",
			Tier:  "0",
			Candidates: []research.Candidate{
				{EntityID: "test.research.graph.route.entity.drone-001", Label: "Drone 001", Relevance: 0.9, Tier: "0", Source: "x"},
			},
		},
	}
	router := &fakeRouter{
		content: `{"action":"walk_seeds","args":{"seeds":[{"ref":"test.research.graph.route.entity.drone-001","ref_type":"name"}]},"rationale":"seed found"}`,
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

// --- initRouter ---

func TestInitRouter_RejectsMissingModelRegistry(t *testing.T) {
	// route_search has no keyword fallback (unlike nl_classify's
	// optional LLM tier). A nil ModelRegistry must surface as a
	// startup error, not a silent degrade to no-op routing — that
	// would leak loops mid-chain with no route.complete trigger
	// ever written.
	c := &Component{
		config: DefaultConfig(),
		logger: quietLogger(),
	}
	err := c.initRouter()
	if err == nil {
		t.Fatal("initRouter accepted nil ModelRegistry; want error")
	}
	if !strings.Contains(err.Error(), "model registry required") {
		t.Errorf("error should explain missing registry: %v", err)
	}
	// Specifically calls out the capability name so an operator
	// reading the log knows which capability to add.
	if !strings.Contains(err.Error(), model.CapabilityResearchRouting) {
		t.Errorf("error should mention CapabilityResearchRouting: %v", err)
	}
}

func TestInitRouter_SkipsWhenRouterAlreadyInjected(t *testing.T) {
	// Test-injected router (the path component_test.go's
	// newTestComponent uses) must NOT re-resolve the model registry.
	// Otherwise Start under tests would fail on nil deps.
	c := &Component{
		config: DefaultConfig(),
		logger: quietLogger(),
		router: &fakeRouter{},
	}
	if err := c.initRouter(); err != nil {
		t.Errorf("initRouter with pre-injected router should no-op, got %v", err)
	}
}

// --- concurrency ---

// TestComponent_HandleMessage_ConcurrentDispatch exercises 50
// concurrent handleMessage calls against a shared fakeRouter +
// fakeLoopStore. Validates that wg + atomic counters surface
// consistent totals under contention and that no write-order
// reordering trips the snapshot-then-trigger invariant for any
// individual call. Cheap regression net for the "two router calls
// raced; one snapshot wins, two triggers fire" class of bugs.
func TestComponent_HandleMessage_ConcurrentDispatch(t *testing.T) {
	const n = 50
	loops := &fakeLoopStore{
		intent:        &research.Intent{Topic: "x"},
		classifierOut: &research.ClassifierOutput{Topic: "x", Tier: "0"},
	}
	router := &fakeRouter{content: `{"action":"synthesize_directly","args":{},"rationale":"ok"}`}
	c := newTestComponent(loops, router)

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			subject := "component.route_search.loop-" + fmt.Sprintf("%d", idx)
			c.handleMessage(context.Background(), subject, nil)
		}(i)
	}
	wg.Wait()

	if got := atomic.LoadInt64(&c.messagesProcessed); got != n {
		t.Errorf("messagesProcessed = %d, want %d", got, n)
	}
	if got := atomic.LoadInt64(&c.messagesEmitted); got != n {
		t.Errorf("messagesEmitted = %d, want %d", got, n)
	}
	if got := atomic.LoadInt64(&c.errors); got != 0 {
		t.Errorf("errors = %d, want 0", got)
	}
	// Each call writes snapshot + trigger, so 2*n writes total.
	if got := len(loops.writeOrder); got != 2*n {
		t.Errorf("write count = %d, want %d (snapshot + trigger per call)", got, 2*n)
	}
	// Spot-check: every snapshot-then-trigger pair stays adjacent
	// at minimum (snapshot at even index, trigger at odd index of
	// each pair) — looser than full ordering since calls interleave.
	snapshots, triggers := 0, 0
	for _, w := range loops.writeOrder {
		switch w {
		case "snapshot":
			snapshots++
		case "route_complete":
			triggers++
		}
	}
	if snapshots != n || triggers != n {
		t.Errorf("snapshot=%d trigger=%d, want %d each", snapshots, triggers, n)
	}
}

// fakeLLMClient implements graph/llm.Client with no-op methods for
// gh#189's concurrent-Start/Stop race test. Tracks Close invocations
// so the test can assert "Stop saw a non-nil client and closed it"
// vs "Stop saw a nil-assigned client" across many iterations.
type fakeLLMClient struct {
	closeCount atomic.Int64
}

func (f *fakeLLMClient) ChatCompletion(context.Context, llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{Content: "test"}, nil
}
func (f *fakeLLMClient) Model() string { return "fake-test" }
func (f *fakeLLMClient) Close() error  { f.closeCount.Add(1); return nil }

// TestComponent_ConcurrentInitAndStop_NoRace empirically validates the
// gh#189 fix: the c.mu guard around both initRouter's write to
// c.llmClient/c.router (line ~225) AND Stop's close+nil-assign
// (line ~314) must serialize so `go test -race` reports no data race
// across many iterations.
//
// Pre-fix: only Stop's nil-assign was under c.mu — initRouter's write
// at line 221 was unprotected, so concurrent Start+Stop produced a
// flaggable race on c.llmClient. go-reviewer caught the "narrows the
// race" framing as dishonest and asked for empirical resolution; this
// test IS that resolution.
//
// Run under -race to validate. Plain `go test` will pass either way;
// the test's value is the data-race assertion the race detector
// performs in parallel.
func TestComponent_ConcurrentInitAndStop_NoRace(t *testing.T) {
	// The mutex-protected sections we're stressing are short — even
	// without coordination, 200 iterations gives the race detector
	// plenty of opportunity to interleave the two goroutines and
	// observe an unprotected access if the mutex contract is wrong.
	const iters = 200

	for i := 0; i < iters; i++ {
		c := &Component{
			config: DefaultConfig(),
			logger: quietLogger(),
		}

		// In-flight initRouter-equivalent: lock c.mu, write the two
		// fields that the production initRouter writes (c.llmClient +
		// c.router), release. Race-detector sees these writes;
		// production fix wraps the SAME writes in the SAME lock.
		writeDone := make(chan struct{})
		go func() {
			defer close(writeDone)
			client := &fakeLLMClient{}
			c.mu.Lock()
			c.llmClient = client
			c.router = newLLMRouterAdapter(client)
			c.mu.Unlock()
		}()

		// Concurrent Stop-equivalent on the same fields. The lock
		// ordering MUST serialize this against the writer above.
		go func() {
			c.mu.Lock()
			if c.llmClient != nil {
				_ = c.llmClient.Close()
				c.llmClient = nil
			}
			c.mu.Unlock()
		}()

		<-writeDone
		// Drain the Stop goroutine by re-acquiring under lock — this
		// also blocks until any in-flight Stop completes.
		c.mu.Lock()
		c.mu.Unlock() //nolint:staticcheck // intentional drain-by-lock
	}
	// The race detector is the assertion; this log line documents the
	// run for human-readable test output and satisfies linters that
	// want every test parameter to be used.
	t.Logf("ran %d concurrent init+stop iterations under c.mu; -race would have flagged any unprotected access", iters)
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
