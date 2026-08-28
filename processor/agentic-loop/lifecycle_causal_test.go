package agenticloop

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
	closed       chan struct{}
	closedCalls  chan struct{}
	drains       atomic.Int32
	closeOnDrain bool
	once         sync.Once
}

func (*causalHandle) Stop() { panic("force") }
func (h *causalHandle) Drain() {
	h.drains.Add(1)
	if h.closeOnDrain {
		h.once.Do(func() { close(h.closed) })
	}
}
func (h *causalHandle) Closed() <-chan struct{} { h.closedCalls <- struct{}{}; return h.closed }

type causalRequestSub struct {
	drains       atomic.Int32
	nativeDrains atomic.Int32
	firstErr     error
	retryEntered chan struct{}
	retryRelease chan struct{}
	retryOnce    sync.Once
}

func (s *causalRequestSub) Drain(ctx context.Context) error {
	if s.drains.Add(1) == 1 {
		s.nativeDrains.Add(1)
		return s.firstErr
	}
	if s.retryEntered == nil {
		return nil
	}
	s.retryOnce.Do(func() { close(s.retryEntered) })
	select {
	case <-s.retryRelease:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

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
	d, e := NewComponent([]byte(`{}`), component.Dependencies{NATSClient: &natsclient.Client{}})
	if e != nil {
		t.Fatal(e)
	}
	c := d.(*Component)
	c.initializeKVBucketsInput = func(context.Context) error { return nil }
	c.waitForStreamInput = func(context.Context, string) error { return nil }
	tracked := &causalHandle{closed: make(chan struct{}), closedCalls: make(chan struct{}, 3)}
	var consumeCalls atomic.Int32
	var callbackCtx context.Context
	c.consumeStream = func(_ context.Context, handlerCtx context.Context, _ natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig, _ func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
		if consumeCalls.Add(1) == 1 {
			callbackCtx = handlerCtx
			return tracked, nil
		}
		h := &causalHandle{closed: make(chan struct{}), closedCalls: make(chan struct{}, 2), closeOnDrain: true}
		return h, nil
	}
	coreErr := errors.New("core drain wait")
	core := &causalRequestSub{
		firstErr:     coreErr,
		retryEntered: make(chan struct{}),
		retryRelease: make(chan struct{}),
	}
	entered, release := make(chan struct{}), make(chan struct{})
	acquireErr, cleanupErr := errors.New("later acquisition"), errors.New("bounded cleanup")
	var subscribeCalls, waits atomic.Int32
	var orderViolation atomic.Bool
	c.subscribeRequests = func(context.Context, string, func(context.Context, []byte) ([]byte, error)) (requestSubscription, error) {
		if subscribeCalls.Add(1) == 1 {
			return core, nil
		}
		close(entered)
		<-release
		return nil, acquireErr
	}
	c.waitConsumerClosed = func(ctx context.Context, closed <-chan struct{}) error {
		if waits.Add(1) == 1 {
			orderViolation.Store(core.drains.Load() != 1 || callbackCtx.Err() != nil)
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
	if len(c.consumers) == 0 || c.trajectorySub != core || c.startDone == nil || !c.cleanupPending {
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
	if e := <-start; !errors.Is(e, acquireErr) || !errors.Is(e, cleanupErr) || !errors.Is(e, coreErr) {
		t.Fatalf("Start %v", e)
	}
	<-tracked.closedCalls
	if tracked.drains.Load() != 1 || orderViolation.Load() || callbackCtx.Err() == nil {
		t.Fatal("rollback")
	}
	select {
	case <-core.retryEntered:
	case <-tracked.closedCalls:
		t.Fatal("cleanup retry skipped the retained core subscription")
	}
	assertFailedCleanupAuthority(t, c, core)
	if e := c.Start(t.Context()); !errors.Is(e, errs.ErrAlreadyStarted) {
		t.Fatalf("restart %v", e)
	}
	close(core.retryRelease)
	<-tracked.closedCalls
	c.lifecycleMu.Lock()
	if c.trajectorySub != nil {
		t.Fatal("successfully rejoined core subscription was not cleared")
	}
	c.lifecycleMu.Unlock()
	close(tracked.closed)
	if e := <-stop; e != nil {
		t.Fatal(e)
	}
	if tracked.drains.Load() != 1 || core.drains.Load() != 2 || core.nativeDrains.Load() != 1 {
		t.Fatalf("replay: js=%d core_calls=%d core_native=%d", tracked.drains.Load(), core.drains.Load(), core.nativeDrains.Load())
	}
	if e := c.Stop(t.Context()); e != nil {
		t.Fatal(e)
	}
}

func assertFailedCleanupAuthority(t *testing.T, c *Component, core requestSubscription) {
	t.Helper()
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	if !c.cleanupPending || c.startDone != nil || c.trajectorySub != core {
		t.Fatal("failed Start did not retain exact unresolved cleanup authority")
	}
}

func TestLifecycleRunningDeadlineIsTerminalNoReplay(t *testing.T) {
	runCtx, cancelRun := context.WithCancel(t.Context())
	h := &causalHandle{closed: make(chan struct{}), closedCalls: make(chan struct{}, 2)}
	c := &Component{lifecycleUsed: true, started: true, cancel: cancelRun, consumers: []streamConsumerBinding{{handle: h}}}
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
