package researchexecute

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
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
	c := newTestComponent(&fakeLoopStore{}, &fakeGraphQuery{})
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
	c := newTestComponent(&fakeLoopStore{}, &fakeGraphQuery{})
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
	c := newTestComponent(&fakeLoopStore{}, &fakeGraphQuery{})
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

	unused := newTestComponent(&fakeLoopStore{}, &fakeGraphQuery{})
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

func newLifecycleNATSClient(t *testing.T) *natsclient.Client {
	t.Helper()
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
	return client
}

func TestComponentDrainKeepsCallbackContextLive(t *testing.T) {
	client := newLifecycleNATSClient(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	callbackContext := make(chan context.Context, 1)
	loops := &fakeLoopStore{
		intentErr: context.Canceled, intentEntered: entered,
		intentRelease: release, intentContext: callbackContext,
	}
	c := newTestComponent(loops, &fakeGraphQuery{})
	c.deps.NATSClient = client
	if err := c.Start(t.Context()); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	facts, err := c.inputs[0].Facts()
	if err != nil {
		t.Fatalf("Facts() = %v", err)
	}
	if err := client.Publish(t.Context(), facts.NATSSubjects()[0], nil); err != nil {
		t.Fatalf("Publish() = %v", err)
	}
	if err := client.GetConnection().Flush(); err != nil {
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
	close(release)
	if err := <-stopResult; err != nil {
		t.Fatalf("Stop() = %v", err)
	}
	if err := callbackCtx.Err(); !errors.Is(err, context.Canceled) {
		t.Fatalf("callback context after Stop = %v, want context.Canceled", err)
	}
}

func TestFailedStartRetainsPartialSubscription(t *testing.T) {
	client := newLifecycleNATSClient(t)
	portEntered := make(chan struct{})
	portRelease := make(chan struct{})
	handlerEntered := make(chan struct{})
	handlerRelease := make(chan struct{})
	loops := &fakeLoopStore{
		intentErr: context.Canceled, intentEntered: handlerEntered,
		intentRelease: handlerRelease,
	}
	c := newTestComponent(loops, &fakeGraphQuery{})
	c.deps.NATSClient = client
	c.inputs = append(c.inputs, component.Port{
		Name: "invalid", Direction: component.DirectionInput,
		Config: &lifecycleBlockingPort{entered: portEntered, release: portRelease},
	})

	startResult := make(chan error, 1)
	go func() { startResult <- c.Start(t.Context()) }()
	<-portEntered
	facts, err := c.inputs[0].Facts()
	if err != nil {
		t.Fatalf("Facts() = %v", err)
	}
	if err := client.Publish(t.Context(), facts.NATSSubjects()[0], nil); err != nil {
		t.Fatalf("Publish() = %v", err)
	}
	if err := client.GetConnection().Flush(); err != nil {
		t.Fatalf("Flush() = %v", err)
	}
	<-handlerEntered
	close(portRelease)

	startErr := <-startResult
	if !errors.Is(startErr, context.DeadlineExceeded) {
		t.Fatalf("Start() = %v, want rollback deadline", startErr)
	}
	if !c.cleanupPending || c.cancel == nil || len(c.subscriptions) != 1 {
		t.Fatal("failed Start did not retain exact partial subscription authority")
	}
	if err := c.Start(t.Context()); err == nil {
		t.Fatal("second Start overwrote retained cleanup authority")
	}
	close(handlerRelease)
	if err := c.Stop(t.Context()); err != nil {
		t.Fatalf("Stop() retry = %v", err)
	}
	if c.cleanupPending || c.cancel != nil || len(c.subscriptions) != 0 || !c.terminal {
		t.Fatal("later Stop did not clear completed cleanup authority")
	}
	if err := c.Stop(t.Context()); err != nil {
		t.Fatalf("repeated Stop() = %v, want nil", err)
	}
	if err := c.Start(t.Context()); err == nil {
		t.Fatal("Start() after exact cleanup returned nil")
	}
}
