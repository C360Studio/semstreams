package researchclassify

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph/llm"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadregistry"
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

func newLifecycleTestComponent(t *testing.T) *Component {
	t.Helper()
	server, err := natsserver.NewServer(&natsserver.Options{
		Port: -1, NoLog: true, NoSigs: true, JetStream: true, StoreDir: t.TempDir(),
	})
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

	c := newTestComponent(&fakeLoopStore{}, &fakeClassifier{}, &fakeRetriever{})
	c.deps.NATSClient = client
	c.deps.PayloadRegistry = payloadregistry.New()
	return c
}

func TestComponentStopWaitsForStartFinalization(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	c := newLifecycleTestComponent(t)
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
	c := newLifecycleTestComponent(t)
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
	c := newLifecycleTestComponent(t)
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

	unused := newLifecycleTestComponent(t)
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
}

func (*lifecycleLLMClient) ChatCompletion(context.Context, llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{}, nil
}
func (*lifecycleLLMClient) Model() string { return "lifecycle-test" }
func (c *lifecycleLLMClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closeErr != nil {
		return c.closeErr
	}
	c.once.Do(func() { close(c.closed) })
	return nil
}

func TestComponentDrainKeepsCallbackContextAndLLMClientLive(t *testing.T) {
	c := newLifecycleTestComponent(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	callbackContext := make(chan context.Context, 1)
	loops := &fakeLoopStore{
		intentErr: context.Canceled, intentEntered: entered,
		intentRelease: release, intentContext: callbackContext,
	}
	ownedClient := &lifecycleLLMClient{closed: make(chan struct{})}
	c.llmClient = ownedClient
	if err := c.Start(t.Context()); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	// Start owns and replaces the production KV adapter. Swap in the behavior
	// fake only after startup and before publishing the callback-driving message.
	c.loops = loops
	facts, err := c.inputs[0].Facts()
	if err != nil {
		t.Fatalf("Facts() = %v", err)
	}
	if err := c.deps.NATSClient.Publish(t.Context(), facts.NATSSubjects()[0], nil); err != nil {
		t.Fatalf("Publish() = %v", err)
	}
	if err := c.deps.NATSClient.GetConnection().Flush(); err != nil {
		t.Fatalf("Flush() = %v", err)
	}
	<-entered
	callbackCtx := <-callbackContext
	stopCtx := &lifecycleObservedContext{Context: t.Context(), observed: make(chan struct{})}
	stopResult := make(chan error, 1)
	go func() { stopResult <- c.Stop(stopCtx) }()
	<-stopCtx.observed
	if err := callbackCtx.Err(); err != nil {
		t.Fatalf("callback context canceled before Drain: %v", err)
	}
	select {
	case <-ownedClient.closed:
		t.Fatal("LLM client closed before callback completed")
	default:
	}
	close(release)
	if err := <-stopResult; err != nil {
		t.Fatalf("Stop() = %v", err)
	}
	select {
	case <-ownedClient.closed:
	default:
		t.Fatal("LLM client not closed after callback drain")
	}
}

func TestFailedStartRetainsPartialSubscriptionAndLLMClient(t *testing.T) {
	c := newLifecycleTestComponent(t)
	closeErr := errors.New("close failed")
	ownedClient := &lifecycleLLMClient{closed: make(chan struct{}), closeErr: closeErr}
	c.llmClient = ownedClient
	release := make(chan struct{})
	close(release)
	c.inputs = append(c.inputs, component.Port{
		Name: "invalid", Direction: component.DirectionInput,
		Config: &lifecycleBlockingPort{entered: make(chan struct{}), release: release},
	})
	if err := c.Start(t.Context()); !errors.Is(err, closeErr) {
		t.Fatalf("Start() = %v, want close failure", err)
	}
	if !c.cleanupPending || len(c.subscriptions) != 1 || c.llmClient != ownedClient {
		t.Fatal("failed Start did not retain exact partial-acquisition authority")
	}
	ownedClient.mu.Lock()
	ownedClient.closeErr = nil
	ownedClient.mu.Unlock()
	if err := c.Stop(t.Context()); err != nil {
		t.Fatalf("Stop() retry = %v", err)
	}
	if c.cleanupPending || len(c.subscriptions) != 0 || c.llmClient != nil {
		t.Fatal("later Stop did not clear completed cleanup authority")
	}
}

func TestFailedStartRollbackExpiryRetainsPartialSubscription(t *testing.T) {
	c := newLifecycleTestComponent(t)
	portEntered := make(chan struct{})
	portRelease := make(chan struct{})
	handlerEntered := make(chan struct{})
	handlerRelease := make(chan struct{})
	var releasePort sync.Once
	var releaseHandler sync.Once
	t.Cleanup(func() {
		releasePort.Do(func() { close(portRelease) })
		releaseHandler.Do(func() { close(handlerRelease) })
	})
	loops := &fakeLoopStore{
		intentErr: context.Canceled, intentEntered: handlerEntered,
		intentRelease: handlerRelease,
	}
	c.inputs = append(c.inputs, component.Port{
		Name: "invalid", Direction: component.DirectionInput,
		Config: &lifecycleBlockingPort{entered: portEntered, release: portRelease},
	})

	startResult := make(chan error, 1)
	go func() { startResult <- c.Start(t.Context()) }()
	<-portEntered
	// Start owns and replaces the production KV adapter before subscribing.
	// Install the behavior fake only after that replacement and before publish.
	c.loops = loops
	facts, err := c.inputs[0].Facts()
	if err != nil {
		t.Fatalf("Facts() = %v", err)
	}
	if err := c.deps.NATSClient.Publish(t.Context(), facts.NATSSubjects()[0], nil); err != nil {
		t.Fatalf("Publish() = %v", err)
	}
	if err := c.deps.NATSClient.GetConnection().Flush(); err != nil {
		t.Fatalf("Flush() = %v", err)
	}
	<-handlerEntered
	releasePort.Do(func() { close(portRelease) })

	if err := <-startResult; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Start() = %v, want rollback deadline", err)
	}
	if !c.cleanupPending || c.cancel == nil || len(c.subscriptions) != 1 {
		t.Fatal("expired rollback did not retain exact partial subscription authority")
	}
	if err := c.Start(t.Context()); err == nil {
		t.Fatal("Start() replaced retained cleanup authority")
	}
	if !c.cleanupPending || len(c.subscriptions) != 1 {
		t.Fatal("rejected Start changed retained cleanup authority")
	}

	releaseHandler.Do(func() { close(handlerRelease) })
	if err := c.Stop(t.Context()); err != nil {
		t.Fatalf("Stop() retry = %v", err)
	}
	if c.cleanupPending || c.cancel != nil || len(c.subscriptions) != 0 || !c.terminal {
		t.Fatal("later Stop did not complete retained cleanup authority")
	}
}
