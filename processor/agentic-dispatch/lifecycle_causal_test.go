package agenticdispatch

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
)

type causalConsumeHandle struct {
	closed      chan struct{}
	closedCalls chan struct{}
	drains      atomic.Int32
}

func (*causalConsumeHandle) Stop()                     { panic("unexpected force Stop") }
func (h *causalConsumeHandle) Drain()                  { h.drains.Add(1) }
func (h *causalConsumeHandle) Closed() <-chan struct{} { h.closedCalls <- struct{}{}; return h.closed }

type causalObservedContext struct {
	context.Context
	once sync.Once
	seen chan struct{}
}

func (c *causalObservedContext) Done() <-chan struct{} {
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
	discoverable, err := NewComponent([]byte(`{}`), componentDependenciesForCausalTest())
	if err != nil {
		t.Fatal(err)
	}
	c := discoverable.(*Component)
	c.waitForStreamInput = func(context.Context, string) error { return nil }
	handle := &causalConsumeHandle{closed: make(chan struct{}), closedCalls: make(chan struct{}, 3)}
	secondEntered, releaseSecond := make(chan struct{}), make(chan struct{})
	acquireErr, cleanupErr := errors.New("later acquisition"), errors.New("bounded cleanup")
	var calls atomic.Int32
	var callbackCtx context.Context
	c.consumeStream = func(ctx context.Context, _ natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig, _ func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
		if calls.Add(1) == 1 {
			callbackCtx = ctx
			return handle, nil
		}
		close(secondEntered)
		<-releaseSecond
		return nil, acquireErr
	}
	var waits atomic.Int32
	var canceledBeforeClosedWait atomic.Bool
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
	<-secondEntered
	c.lifecycleMu.Lock()
	if len(c.consumers) != 1 || c.startDone == nil || !c.cleanupPending {
		t.Fatal("first handle/start authority not incrementally published")
	}
	c.lifecycleMu.Unlock()
	observed := &causalObservedContext{Context: t.Context(), seen: make(chan struct{})}
	stopResult := make(chan error, 1)
	go func() { stopResult <- c.Stop(observed) }()
	<-observed.seen
	c.lifecycleMu.Lock()
	c.lifecycleMu.Unlock()
	select {
	case err := <-stopResult:
		t.Fatalf("Stop returned before startDone: %v", err)
	default:
	}
	close(releaseSecond)
	if err := <-startResult; !errors.Is(err, acquireErr) || !errors.Is(err, cleanupErr) {
		t.Fatalf("Start error = %v", err)
	}
	<-handle.closedCalls
	if handle.drains.Load() != 1 || canceledBeforeClosedWait.Load() || callbackCtx.Err() == nil {
		t.Fatalf("failed rollback authority: drains=%d early_cancel=%v ctx=%v", handle.drains.Load(), canceledBeforeClosedWait.Load(), callbackCtx.Err())
	}
	c.lifecycleMu.Lock()
	activityRetained := c.activityDone != nil || c.activityCommands != nil
	c.lifecycleMu.Unlock()
	if activityRetained {
		t.Fatal("GraphView controller was not joined during rollback")
	}
	if err := c.Start(t.Context()); !errors.Is(err, errs.ErrAlreadyStarted) {
		t.Fatalf("Start while cleanup pending = %v", err)
	}
	<-handle.closedCalls
	select {
	case err := <-stopResult:
		t.Fatalf("Stop returned before Closed: %v", err)
	default:
	}
	close(handle.closed)
	if err := <-stopResult; err != nil {
		t.Fatalf("retry Stop: %v", err)
	}
	if handle.drains.Load() != 1 {
		t.Fatalf("Drain replayed: %d", handle.drains.Load())
	}
	if err := c.Stop(t.Context()); err != nil {
		t.Fatalf("repeat Stop: %v", err)
	}
}
func TestLifecycleRunningDeadlineIsTerminalNoReplay(t *testing.T) {
	runCtx, cancelRun := context.WithCancel(t.Context())
	h := &causalConsumeHandle{closed: make(chan struct{}), closedCalls: make(chan struct{}, 2)}
	c := &Component{lifecycleUsed: true, started: true, cancel: cancelRun, consumers: []streamConsumerBinding{{handle: h}}}
	stopCtx, expire := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() { result <- c.Stop(stopCtx) }()
	<-h.closedCalls
	if runCtx.Err() != nil {
		t.Fatal("callback canceled before Closed")
	}
	expire()
	if e := <-result; !errors.Is(e, context.Canceled) {
		t.Fatalf("Stop %v", e)
	}
	if runCtx.Err() == nil || !c.terminal {
		t.Fatal("deadline did not terminalize")
	}
	if e := c.Stop(t.Context()); e != nil || h.drains.Load() != 1 {
		t.Fatalf("repeat %v drains=%d", e, h.drains.Load())
	}
}

func componentDependenciesForCausalTest() component.Dependencies {
	return component.Dependencies{NATSClient: &natsclient.Client{}, ModelRegistry: &model.Registry{}}
}
