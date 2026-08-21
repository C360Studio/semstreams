package websocket

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type causalCoreSubscription struct{ drains atomic.Int32 }

func (s *causalCoreSubscription) Drain(context.Context) error { s.drains.Add(1); return nil }

type causalConsumeHandle struct {
	closed chan struct{}
	drains atomic.Int32
}

type outputObservedContext struct {
	context.Context
	seen chan struct{}
	once atomic.Bool
}

func (c *outputObservedContext) Done() <-chan struct{} {
	if c.once.CompareAndSwap(false, true) {
		close(c.seen)
	}
	return c.Context.Done()
}

func (*causalConsumeHandle) Stop()                     { panic("force Stop") }
func (h *causalConsumeHandle) Drain()                  { h.drains.Add(1) }
func (h *causalConsumeHandle) Closed() <-chan struct{} { return h.closed }

func TestLifecycleStartReportsServerBindFailureSynchronously(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	w := mustNewOutput(t, port, "/ws", []string{"test.subject"}, nil)
	w.host = "127.0.0.1"
	if err := w.Start(t.Context()); err == nil {
		t.Fatal("Start succeeded despite occupied server port")
	}
	if w.running || w.server != nil {
		t.Fatal("bind failure published running HTTP authority")
	}
	if err := w.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestLifecycleContextAndStopBeforeStartAreImmutable(t *testing.T) {
	w := mustNewOutput(t, 28081, "/ws", []string{"test.subject"}, nil)
	ended, cancel := context.WithCancel(t.Context())
	cancel()
	if w.Start(nil) == nil || w.Start(ended) == nil || w.lifecycleUsed {
		t.Fatal("invalid Start changed authority")
	}
	if w.Stop(nil) == nil || w.lifecycleUsed {
		t.Fatal("nil Stop changed authority")
	}
	if err := w.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := w.Start(t.Context()); !errors.Is(err, errs.ErrAlreadyStarted) {
		t.Fatalf("Start after terminal Stop = %v", err)
	}
	if err := w.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if w.Stop(nil) == nil || !w.terminal {
		t.Fatal("nil Stop after terminal changed authority")
	}
}

func TestLifecycleServerStopFencesAndWaitsAdmittedUpgradeBeforeCancel(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	w := mustNewOutput(t, port, "/ws", []string{"test.subject"}, nil)
	w.host = "127.0.0.1"
	entered := make(chan context.Context, 1)
	release := make(chan struct{})
	w.requestHook = func(ctx context.Context) {
		entered <- ctx
		<-release
	}
	if err := w.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	shutdownStarted := make(chan struct{})
	w.server.RegisterOnShutdown(func() { close(shutdownStarted) })
	requestDone := make(chan error, 1)
	go func() {
		resp, err := http.Get("http://" + w.listener.Addr().String() + w.path)
		if resp != nil {
			_ = resp.Body.Close()
		}
		requestDone <- err
	}()
	handlerCtx := <-entered
	stopDone := make(chan error, 1)
	go func() { stopDone <- w.Stop(t.Context()) }()
	<-shutdownStarted
	if handlerCtx.Err() != nil {
		t.Fatal("handler context canceled before Shutdown drained admission")
	}
	close(release)
	if err := <-stopDone; err != nil {
		t.Fatal(err)
	}
	if err := <-requestDone; err != nil {
		t.Fatal(err)
	}
	if handlerCtx.Err() == nil {
		t.Fatal("handler context remained live after Stop")
	}
	if err := w.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestLifecycleServerDeadlineStopIsTerminalWithoutReplay(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	w := mustNewOutput(t, port, "/ws", []string{"test.subject"}, nil)
	w.host = "127.0.0.1"
	entered := make(chan struct{})
	release := make(chan struct{})
	w.requestHook = func(context.Context) {
		close(entered)
		<-release
	}
	if err := w.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	requestDone := make(chan struct{})
	go func() {
		resp, _ := http.Get("http://" + w.listener.Addr().String() + w.path)
		if resp != nil {
			_ = resp.Body.Close()
		}
		close(requestDone)
	}()
	<-entered
	stopCtx, cancel := context.WithCancel(t.Context())
	shutdownStarted := make(chan struct{})
	w.server.RegisterOnShutdown(func() { close(shutdownStarted) })
	stopDone := make(chan error, 1)
	go func() { stopDone <- w.Stop(stopCtx) }()
	<-shutdownStarted
	cancel()
	if err := <-stopDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("deadline Stop = %v", err)
	}
	close(release)
	<-requestDone
	if err := w.Stop(t.Context()); err != nil {
		t.Fatalf("terminal Stop replayed cleanup: %v", err)
	}
}

func TestLifecycleFailedSubscriptionStartRetainsExactCleanupForLaterStop(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	cfg := DefaultConstructorConfig()
	cfg.Name = "lifecycle-causal"
	cfg.Path = "/ws"
	cfg.InputPorts = []component.PortDefinition{
		{Name: "core", Config: component.NATSPort{Subject: "core.>"}},
		{Name: "js-one", Config: component.JetStreamPort{StreamName: "ONE", Subjects: []string{"one.>"}, DeliverPolicy: "new"}},
		{Name: "js-two", Config: component.JetStreamPort{StreamName: "TWO", Subjects: []string{"two.>"}, DeliverPolicy: "new"}},
	}
	cfg.OutputPorts = websocketOutputDefinitions(port)
	cfg.NATSClient = &natsclient.Client{}
	w := mustNewOutputFromConfig(t, cfg)
	w.host = "127.0.0.1"
	core := &causalCoreSubscription{}
	w.subscribeCore = func(context.Context, string, func(context.Context, *nats.Msg)) (coreSubscription, error) {
		return core, nil
	}
	w.waitForInput = func(context.Context, string) error { return nil }
	handle := &causalConsumeHandle{closed: make(chan struct{})}
	acquireErr := errors.New("later consume")
	cleanupErr := errors.New("bounded cleanup")
	var consumeCalls, waits atomic.Int32
	var callbackCtx context.Context
	laterEntered := make(chan struct{})
	releaseLater := make(chan struct{})
	w.consumeStream = func(ctx context.Context, _ natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig, _ func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
		if consumeCalls.Add(1) == 1 {
			callbackCtx = ctx
			return handle, nil
		}
		close(laterEntered)
		<-releaseLater
		return nil, acquireErr
	}
	retryWait := make(chan struct{})
	w.waitJSClosed = func(ctx context.Context, closed <-chan struct{}) error {
		if waits.Add(1) == 1 {
			if callbackCtx.Err() != nil {
				t.Fatal("callback authority canceled before Drain/Closed")
			}
			return cleanupErr
		}
		close(retryWait)
		select {
		case <-closed:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	startDone := make(chan error, 1)
	go func() { startDone <- w.Start(t.Context()) }()
	<-laterEntered
	w.lifecycleMu.Lock()
	if w.startDone == nil || !w.cleanupPending || len(w.consumers) != 1 {
		t.Fatal("Start did not incrementally publish cleanup authority")
	}
	w.lifecycleMu.Unlock()
	stopCtx := &outputObservedContext{Context: t.Context(), seen: make(chan struct{})}
	stopDone := make(chan error, 1)
	go func() { stopDone <- w.Stop(stopCtx) }()
	<-stopCtx.seen
	w.lifecycleMu.Lock()
	w.lifecycleMu.Unlock()
	select {
	case err := <-stopDone:
		t.Fatalf("Stop returned before Start rollback: %v", err)
	default:
	}
	close(releaseLater)
	startErr := <-startDone
	if !errors.Is(startErr, acquireErr) || !errors.Is(startErr, cleanupErr) {
		t.Fatalf("Start = %v", startErr)
	}
	w.lifecycleMu.Lock()
	if !w.cleanupPending || w.startDone != nil || len(w.consumers) != 1 || len(w.subscriptions) != 0 {
		t.Fatal("failed Start cleanup authority")
	}
	w.lifecycleMu.Unlock()
	if handle.drains.Load() != 1 || core.drains.Load() != 1 || callbackCtx.Err() == nil {
		t.Fatal("failed rollback order")
	}
	if err := w.Start(t.Context()); !errors.Is(err, errs.ErrAlreadyStarted) {
		t.Fatalf("restart = %v", err)
	}
	<-retryWait
	if handle.drains.Load() != 1 {
		t.Fatal("JS Drain replayed")
	}
	close(handle.closed)
	if err := <-stopDone; err != nil {
		t.Fatal(err)
	}
	if err := w.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
}
