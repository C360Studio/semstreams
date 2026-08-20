package agenticgovernance

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
)

type causalHandle struct {
	closed      chan struct{}
	closedCalls chan struct{}
	drains      atomic.Int32
}

func (*causalHandle) Stop()                     { panic("unexpected force Stop") }
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
	d, err := NewComponent([]byte(`{}`), component.Dependencies{NATSClient: &natsclient.Client{}})
	if err != nil {
		t.Fatal(err)
	}
	c := d.(*Component)
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
	startResult := make(chan error, 1)
	go func() { startResult <- c.Start(t.Context()) }()
	<-entered
	c.lifecycleMu.Lock()
	if len(c.consumers) != 1 || c.startDone == nil || !c.cleanupPending {
		t.Fatal("first authority not published")
	}
	c.lifecycleMu.Unlock()
	observed := &observedContext{Context: t.Context(), seen: make(chan struct{})}
	stopResult := make(chan error, 1)
	go func() { stopResult <- c.Stop(observed) }()
	<-observed.seen
	c.lifecycleMu.Lock()
	c.lifecycleMu.Unlock()
	select {
	case e := <-stopResult:
		t.Fatalf("early Stop: %v", e)
	default:
	}
	close(release)
	if e := <-startResult; !errors.Is(e, acquireErr) || !errors.Is(e, cleanupErr) {
		t.Fatalf("Start: %v", e)
	}
	<-h.closedCalls
	if h.drains.Load() != 1 || canceledBeforeClosedWait.Load() || callbackCtx.Err() == nil {
		t.Fatalf("rollback drains=%d early_cancel=%v ctx=%v", h.drains.Load(), canceledBeforeClosedWait.Load(), callbackCtx.Err())
	}
	if e := c.Start(t.Context()); !errors.Is(e, errs.ErrAlreadyStarted) {
		t.Fatalf("restart: %v", e)
	}
	<-h.closedCalls
	close(h.closed)
	if e := <-stopResult; e != nil {
		t.Fatal(e)
	}
	if h.drains.Load() != 1 {
		t.Fatal("Drain replay")
	}
	if e := c.Stop(t.Context()); e != nil {
		t.Fatal(e)
	}
}
func TestLifecycleRunningDeadlineIsTerminalNoReplay(t *testing.T) {
	runCtx, cancelRun := context.WithCancel(t.Context())
	h := &causalHandle{closed: make(chan struct{}), closedCalls: make(chan struct{}, 2)}
	c := &Component{lifecycleUsed: true, running: true, cancel: cancelRun, consumers: []streamConsumerBinding{{handle: h}}}
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
