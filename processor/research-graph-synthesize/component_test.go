package researchsynthesize

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic/research"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/internal/graphmutation"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/fusion"
	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/stretchr/testify/require"
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

func TestComponentLifecycleIsOneShot(t *testing.T) {
	c := newTestComponent(&fakeLoopStore{}, &fakeSynthesizer{})
	c.inputs = nil

	if err := c.Start(t.Context()); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	if err := c.Stop(t.Context()); err != nil {
		t.Fatalf("Stop() = %v", err)
	}
	if err := c.Stop(t.Context()); err != nil {
		t.Fatalf("repeated Stop() = %v, want nil", err)
	}
	if err := c.Start(t.Context()); err == nil {
		t.Fatal("Start() after completed Stop returned nil")
	}
}

// fakeLoopStore replaces natsLoopStore. Records reads + writes so
// the test can assert on key ordering, envelope shape, and miss vs
// hit paths.
type fakeLoopStore struct {
	mu                sync.Mutex
	intentEnteredOnce sync.Once
	intentEntered     chan struct{}
	intentRelease     chan struct{}
	intentContext     chan context.Context

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

func (s *fakeLoopStore) GetIntent(ctx context.Context, _ string) (*research.Intent, error) {
	if s.intentContext != nil {
		s.intentContext <- ctx
	}
	if s.intentEntered != nil {
		s.intentEnteredOnce.Do(func() { close(s.intentEntered) })
	}
	if s.intentRelease != nil {
		<-s.intentRelease
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getIntentCalls++
	if s.intentErr != nil {
		return nil, s.intentErr
	}
	return s.intent, nil
}

type blockingNATSPort struct {
	subject string
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (p *blockingNATSPort) ResourceID() string { return "nats:" + p.subject }
func (*blockingNATSPort) IsExclusive() bool    { return false }
func (*blockingNATSPort) Kind() component.PortKind {
	return component.PortKindNATS
}
func (p *blockingNATSPort) MarshalJSON() ([]byte, error) {
	p.once.Do(func() { close(p.entered) })
	<-p.release
	return json.Marshal(component.NATSPort{Subject: p.subject})
}

func TestStartRetainsFailedPartialCleanupForLaterStop(t *testing.T) {
	server, err := natsserver.NewServer(&natsserver.Options{
		Port: -1, NoLog: true, NoSigs: true,
	})
	require.NoError(t, err)
	go server.Start()
	require.True(t, server.ReadyForConnections(5*time.Second))
	t.Cleanup(server.Shutdown)

	client, err := natsclient.NewClient(server.ClientURL())
	require.NoError(t, err)
	require.NoError(t, client.Connect(t.Context()))
	t.Cleanup(func() { _ = client.Close(context.Background()) })

	first, err := (component.PortDefinition{
		Name: "first", Config: component.NATSPort{Subject: "component.synthesize_answer.loop-1"},
	}).Resolve(component.DirectionInput)
	require.NoError(t, err)
	portEntered := make(chan struct{})
	portRelease := make(chan struct{})
	second := component.Port{
		Name: "second", Direction: component.DirectionInput,
		Config: &blockingNATSPort{subject: "invalid..subject", entered: portEntered, release: portRelease},
	}
	handlerEntered := make(chan struct{})
	handlerRelease := make(chan struct{})
	loops := &fakeLoopStore{
		intentErr: context.Canceled, intentEntered: handlerEntered, intentRelease: handlerRelease,
	}
	c := newTestComponent(loops, &fakeSynthesizer{})
	c.deps.NATSClient = client
	c.inputs = []component.Port{first, second}

	startResult := make(chan error, 1)
	go func() { startResult <- c.Start(t.Context()) }()
	<-portEntered
	require.NoError(t, client.Publish(t.Context(), "component.synthesize_answer.loop-1", nil))
	require.NoError(t, client.GetConnection().Flush())
	<-handlerEntered
	close(portRelease)

	startErr := <-startResult
	require.Error(t, startErr)
	require.ErrorIs(t, startErr, context.DeadlineExceeded)
	require.True(t, c.cleanupPending, "failed rollback must retain cleanup authority")
	require.NotNil(t, c.cancel, "failed rollback must retain cancellation authority")
	require.Len(t, c.subscriptions, 1, "failed rollback must retain the allocated subscription")

	retryErr := c.Start(t.Context())
	require.Error(t, retryErr)
	require.ErrorContains(t, retryErr, "already used")
	require.True(t, c.cleanupPending, "a second Start must not overwrite retained cleanup authority")
	require.Len(t, c.subscriptions, 1)

	close(handlerRelease)
	require.NoError(t, c.Stop(t.Context()))
	require.Empty(t, c.subscriptions)
	require.False(t, c.cleanupPending)
	require.Nil(t, c.cancel)
	require.True(t, c.terminal)

	c.inputs = []component.Port{first}
	require.Error(t, c.Start(t.Context()), "same-instance restart must remain rejected after exact cleanup")
	require.NoError(t, c.Stop(t.Context()), "completed repeated Stop must be nil")
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
	config := DefaultConfig()
	inputs, outputs := mustResolveTestPorts(config.Ports)
	return &Component{
		config:      config,
		inputs:      inputs,
		outputs:     outputs,
		synthesizer: synth,
		loops:       loops,
		logger:      quietLogger(),
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
