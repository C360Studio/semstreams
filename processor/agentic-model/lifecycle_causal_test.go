package agenticmodel

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
)

type causalHandle struct {
	closed      chan struct{}
	closedCalls chan struct{}
	drains      atomic.Int32
}

func (*causalHandle) Stop()                     { panic("force Stop") }
func (h *causalHandle) Drain()                  { h.drains.Add(1) }
func (h *causalHandle) Closed() <-chan struct{} { h.closedCalls <- struct{}{}; return h.closed }

type observedContext struct {
	context.Context
	once sync.Once
	seen chan struct{}
}

func (c *observedContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.seen) })
	return c.Context.Done()
}
func TestLifecycleContextAndStopBeforeStartImmutability(t *testing.T) {
	c := &Component{}
	ended, cancel := context.WithCancel(t.Context())
	cancel()
	if c.Start(nil) == nil || c.Start(ended) == nil || c.lifecycleUsed {
		t.Fatal("invalid Start")
	}
	if c.Stop(nil) == nil || c.lifecycleUsed {
		t.Fatal("nil Stop")
	}
	if e := c.Stop(t.Context()); e != nil {
		t.Fatal(e)
	}
	if e := c.Start(t.Context()); !errors.Is(e, errs.ErrAlreadyStarted) {
		t.Fatalf("restart %v", e)
	}
	if e := c.Stop(t.Context()); e != nil {
		t.Fatal(e)
	}
}
func TestLifecycleCausalStartStopRollbackRetention(t *testing.T) {
	d, e := NewComponent([]byte(`{}`), component.Dependencies{NATSClient: &natsclient.Client{}, ModelRegistry: &model.Registry{}})
	if e != nil {
		t.Fatal(e)
	}
	c := d.(*Component)
	c.inputPorts = append(c.inputPorts, c.inputPorts[0])
	c.waitForStreamInput = func(context.Context, string) error { return nil }
	h := &causalHandle{closed: make(chan struct{}), closedCalls: make(chan struct{}, 3)}
	entered, release := make(chan struct{}), make(chan struct{})
	acquireErr, cleanupErr := errors.New("later acquisition"), errors.New("bounded cleanup")
	var calls, waits atomic.Int32
	var canceledBeforeClosedWait atomic.Bool
	var callbackCtx context.Context
	c.consumeStream = func(ctx context.Context, _ natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig, _ func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
		if calls.Add(1) == 1 {
			callbackCtx = ctx
			return h, nil
		}
		close(entered)
		<-release
		return nil, acquireErr
	}
	c.waitConsumerClosed = func(ctx context.Context, closed <-chan struct{}) error {
		if waits.Add(1) == 1 {
			canceledBeforeClosedWait.Store(callbackCtx.Err() != nil)
			return cleanupErr
		}
		select {
		case <-closed:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	start := make(chan error, 1)
	go func() { start <- c.Start(t.Context()) }()
	<-entered
	c.lifecycleMu.Lock()
	if len(c.consumers) != 1 || c.startDone == nil || !c.cleanupPending {
		t.Fatal("authority")
	}
	c.lifecycleMu.Unlock()
	observed := &observedContext{Context: t.Context(), seen: make(chan struct{})}
	stop := make(chan error, 1)
	go func() { stop <- c.Stop(observed) }()
	<-observed.seen
	c.lifecycleMu.Lock()
	c.lifecycleMu.Unlock()
	select {
	case e := <-stop:
		t.Fatalf("early %v", e)
	default:
	}
	close(release)
	if e := <-start; !errors.Is(e, acquireErr) || !errors.Is(e, cleanupErr) {
		t.Fatalf("Start %v", e)
	}
	<-h.closedCalls
	if h.drains.Load() != 1 || canceledBeforeClosedWait.Load() || callbackCtx.Err() == nil {
		t.Fatalf("rollback")
	}
	if e := c.Start(t.Context()); !errors.Is(e, errs.ErrAlreadyStarted) {
		t.Fatalf("restart %v", e)
	}
	<-h.closedCalls
	close(h.closed)
	if e := <-stop; e != nil {
		t.Fatal(e)
	}
	if h.drains.Load() != 1 {
		t.Fatal("replay")
	}
	if e := c.Stop(t.Context()); e != nil {
		t.Fatal(e)
	}
}
func TestLifecycleRunningDeadlineIsTerminalNoReplay(t *testing.T) {
	runCtx, cancelRun := context.WithCancel(t.Context())
	h := &causalHandle{closed: make(chan struct{}), closedCalls: make(chan struct{}, 2)}
	c := &Component{lifecycleUsed: true, running: true, cancel: cancelRun, consumers: []streamConsumerBinding{{handle: h}}, clientCache: map[string]*Client{}}
	stopCtx, expire := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() { result <- c.Stop(stopCtx) }()
	<-h.closedCalls
	if runCtx.Err() != nil {
		t.Fatal("early cancel")
	}
	expire()
	if e := <-result; !errors.Is(e, context.Canceled) {
		t.Fatalf("Stop %v", e)
	}
	if runCtx.Err() == nil || !c.terminal {
		t.Fatal("terminal")
	}
	if e := c.Stop(t.Context()); e != nil || h.drains.Load() != 1 {
		t.Fatalf("repeat %v", e)
	}
}

func TestLifecycleCleanupClosesModelClientsOutsideLockAndRetainsOnlyFailures(t *testing.T) {
	first := &Client{}
	second := &Client{}
	closeErr := errors.New("close model client")
	c := &Component{
		logger:      slog.Default(),
		clientCache: map[string]*Client{"first": first, "second": second},
	}
	var firstCalls, secondCalls atomic.Int32
	c.closeModelClient = func(client *Client) error {
		// Re-entry proves native Close is not invoked while clientMu is held.
		c.clientMu.Lock()
		c.clientMu.Unlock()
		switch client {
		case first:
			firstCalls.Add(1)
			return nil
		case second:
			if secondCalls.Add(1) == 1 {
				return closeErr
			}
			return nil
		default:
			t.Fatalf("unexpected client %p", client)
			return nil
		}
	}

	if err := c.cleanup(t.Context()); !errors.Is(err, closeErr) {
		t.Fatalf("first cleanup = %v", err)
	}
	c.clientMu.Lock()
	_, firstRetained := c.clientCache["first"]
	retainedSecond := c.clientCache["second"]
	c.clientMu.Unlock()
	if firstRetained || retainedSecond != second {
		t.Fatalf("partial clear: first=%v second=%p", firstRetained, retainedSecond)
	}

	if err := c.cleanup(t.Context()); err != nil {
		t.Fatalf("retry cleanup = %v", err)
	}
	c.clientMu.Lock()
	remaining := len(c.clientCache)
	c.clientMu.Unlock()
	if remaining != 0 || firstCalls.Load() != 1 || secondCalls.Load() != 2 {
		t.Fatalf("retry: remaining=%d first=%d second=%d", remaining, firstCalls.Load(), secondCalls.Load())
	}
}
