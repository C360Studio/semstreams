package researchsynthesize

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph/llm"
	"github.com/c360studio/semstreams/natsclient"
	natsserver "github.com/nats-io/nats-server/v2/server"
)

type lifecycleBlockingPort struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (*lifecycleBlockingPort) ResourceID() string       { return "nats:invalid..subject" }
func (*lifecycleBlockingPort) IsExclusive() bool        { return false }
func (*lifecycleBlockingPort) Kind() component.PortKind { return component.PortKindNATS }
func (p *lifecycleBlockingPort) MarshalJSON() ([]byte, error) {
	p.once.Do(func() { close(p.entered) })
	<-p.release
	return nil, errors.New("blocked port failure")
}

type lifecycleObservedContext struct {
	context.Context
	observed chan struct{}
	once     sync.Once
}

func (c *lifecycleObservedContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.observed) })
	return c.Context.Done()
}

func TestComponentStopWaitsForStartFinalization(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	c := newTestComponent(&fakeLoopStore{}, &fakeSynthesizer{})
	c.inputs = []component.Port{{
		Name: "blocked", Direction: component.DirectionInput,
		Config: &lifecycleBlockingPort{entered: entered, release: release},
	}}
	startResult := make(chan error, 1)
	go func() { startResult <- c.Start(t.Context()) }()
	<-entered
	stopCtx := &lifecycleObservedContext{Context: t.Context(), observed: make(chan struct{})}
	stopResult := make(chan error, 1)
	go func() { stopResult <- c.Stop(stopCtx) }()
	<-stopCtx.observed
	select {
	case err := <-stopResult:
		t.Fatalf("Stop returned before Start finalized: %v", err)
	default:
	}
	close(release)
	if err := <-startResult; err == nil {
		t.Fatal("Start() returned nil for invalid blocked port")
	}
	if err := <-stopResult; err != nil {
		t.Fatalf("Stop() after failed Start cleanup = %v", err)
	}
}

func TestComponentFailedRunningStopIsTerminalAndNotReplayed(t *testing.T) {
	c := newTestComponent(&fakeLoopStore{}, &fakeSynthesizer{})
	c.inputs = nil
	if err := c.Start(t.Context()); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	stopCtx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := c.Stop(stopCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Stop(canceled) = %v, want context.Canceled", err)
	}
	if err := c.Stop(t.Context()); err != nil {
		t.Fatalf("repeated Stop() = %v, want nil", err)
	}
	if err := c.Start(t.Context()); err == nil {
		t.Fatal("Start() after failed terminal Stop returned nil")
	}
}

func TestComponentStopRejectsNilBeforeStateAndNoActionStopIsTerminal(t *testing.T) {
	c := newTestComponent(&fakeLoopStore{}, &fakeSynthesizer{})
	c.inputs = nil
	if err := c.Stop(nil); err == nil {
		t.Fatal("Stop(nil) returned nil")
	}
	if err := c.Start(t.Context()); err != nil {
		t.Fatalf("Start() after rejected Stop(nil) = %v", err)
	}
	if err := c.Stop(t.Context()); err != nil {
		t.Fatalf("Stop() = %v", err)
	}

	unused := newTestComponent(&fakeLoopStore{}, &fakeSynthesizer{})
	if err := unused.Stop(t.Context()); err != nil {
		t.Fatalf("Stop() before Start = %v", err)
	}
	if err := unused.Stop(t.Context()); err != nil {
		t.Fatalf("repeated no-action Stop() = %v", err)
	}
	if err := unused.Start(t.Context()); err == nil {
		t.Fatal("Start() after no-action terminal Stop returned nil")
	}
}

type lifecycleLLMClient struct {
	mu       sync.Mutex
	closed   chan struct{}
	once     sync.Once
	closeErr error
	closes   int
}

func (*lifecycleLLMClient) ChatCompletion(context.Context, llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{}, nil
}
func (*lifecycleLLMClient) Model() string { return "lifecycle-test" }
func (c *lifecycleLLMClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closes++
	if c.closeErr != nil {
		return c.closeErr
	}
	c.once.Do(func() { close(c.closed) })
	return nil
}

func TestComponentDrainKeepsCallbackContextAndLLMClientLive(t *testing.T) {
	server, err := natsserver.NewServer(&natsserver.Options{Port: -1, NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatalf("NewServer() = %v", err)
	}
	go server.Start()
	if !server.ReadyForConnections(5 * time.Second) {
		t.Fatal("NATS server did not become ready")
	}
	t.Cleanup(server.Shutdown)

	client, err := natsclient.NewClient(server.ClientURL())
	if err != nil {
		t.Fatalf("NewClient() = %v", err)
	}
	if err := client.Connect(t.Context()); err != nil {
		t.Fatalf("Connect() = %v", err)
	}
	t.Cleanup(func() { _ = client.Close(context.Background()) })

	handlerEntered := make(chan struct{})
	handlerRelease := make(chan struct{})
	handlerContext := make(chan context.Context, 1)
	loops := &fakeLoopStore{
		intentErr: context.Canceled, intentEntered: handlerEntered,
		intentRelease: handlerRelease, intentContext: handlerContext,
	}
	ownedClient := &lifecycleLLMClient{closed: make(chan struct{})}
	c := newTestComponent(loops, &fakeSynthesizer{})
	c.deps.NATSClient = client
	c.llmClient = ownedClient
	if err := c.Start(t.Context()); err != nil {
		t.Fatalf("Start() = %v", err)
	}

	facts, err := c.inputs[0].Facts()
	if err != nil {
		t.Fatalf("input Facts() = %v", err)
	}
	if err := client.Publish(t.Context(), facts.NATSSubjects()[0], nil); err != nil {
		t.Fatalf("Publish() = %v", err)
	}
	if err := client.GetConnection().Flush(); err != nil {
		t.Fatalf("Flush() = %v", err)
	}
	<-handlerEntered
	callbackCtx := <-handlerContext

	stopCtx := &lifecycleObservedContext{Context: t.Context(), observed: make(chan struct{})}
	stopResult := make(chan error, 1)
	go func() { stopResult <- c.Stop(stopCtx) }()
	<-stopCtx.observed
	if err := callbackCtx.Err(); err != nil {
		t.Fatalf("callback context canceled before native Drain completed: %v", err)
	}
	select {
	case <-ownedClient.closed:
		t.Fatal("LLM client closed while callback was still admitted")
	default:
	}
	close(handlerRelease)
	if err := <-stopResult; err != nil {
		t.Fatalf("Stop() = %v", err)
	}
	select {
	case <-ownedClient.closed:
	default:
		t.Fatal("LLM client was not closed after callback drain")
	}
}

func TestFailedStartRetainsExactLLMClientAfterCloseFailure(t *testing.T) {
	closeErr := errors.New("close failed")
	ownedClient := &lifecycleLLMClient{closed: make(chan struct{}), closeErr: closeErr}
	release := make(chan struct{})
	close(release)
	c := newTestComponent(&fakeLoopStore{}, &fakeSynthesizer{})
	c.llmClient = ownedClient
	c.inputs = []component.Port{{
		Name: "invalid", Direction: component.DirectionInput,
		Config: &lifecycleBlockingPort{entered: make(chan struct{}), release: release},
	}}

	if err := c.Start(t.Context()); !errors.Is(err, closeErr) {
		t.Fatalf("Start() = %v, want close failure", err)
	}
	if !c.cleanupPending {
		t.Fatal("failed Start did not retain cleanupPending")
	}
	if c.llmClient != ownedClient {
		t.Fatal("failed Start cleared exact LLM client after Close failure")
	}

	ownedClient.mu.Lock()
	ownedClient.closeErr = nil
	ownedClient.mu.Unlock()
	if err := c.Stop(t.Context()); err != nil {
		t.Fatalf("Stop() retry = %v", err)
	}
	if c.llmClient != nil || c.cleanupPending {
		t.Fatal("successful later Stop did not clear retained cleanup authority")
	}
	ownedClient.mu.Lock()
	closes := ownedClient.closes
	ownedClient.mu.Unlock()
	if closes != 2 {
		t.Fatalf("LLM Close calls = %d, want failed rollback plus later Stop", closes)
	}
}
