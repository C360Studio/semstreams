package graphgateway

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/c360studio/semstreams/graph/inference"
	"github.com/c360studio/semstreams/pkg/errs"
)

type observedContext struct {
	context.Context
	seen chan struct{}
	once sync.Once
}

func (c *observedContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.seen) })
	return c.Context.Done()
}

func TestLifecycleStartReportsStandaloneBindFailureSynchronously(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	c := createTestComponent(t)
	c.config.StandaloneServer = true
	c.config.BindAddress = listener.Addr().String()
	if err := c.Initialize(); err != nil {
		t.Fatal(err)
	}
	if err := c.Start(t.Context()); err == nil {
		t.Fatal("Start succeeded despite occupied standalone address")
	}
	if c.running || c.httpServer != nil {
		t.Fatal("bind failure published running HTTP authority")
	}
	if err := c.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestLifecycleContextAndStopBeforeStartAreImmutable(t *testing.T) {
	c := createTestComponent(t)
	if err := c.Initialize(); err != nil {
		t.Fatal(err)
	}
	ended, cancel := context.WithCancel(t.Context())
	cancel()
	if c.Start(nil) == nil || c.Start(ended) == nil || c.lifecycleUsed {
		t.Fatal("invalid Start changed authority")
	}
	if c.Stop(nil) == nil || c.lifecycleUsed {
		t.Fatal("nil Stop changed authority")
	}
	if err := c.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := c.Start(t.Context()); !errors.Is(err, errs.ErrAlreadyStarted) {
		t.Fatalf("Start after terminal Stop = %v", err)
	}
	if err := c.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if c.Stop(nil) == nil || !c.terminal {
		t.Fatal("nil Stop after terminal changed authority")
	}
}

func TestLifecycleStandaloneStopFencesAndWaitsAdmittedRequestBeforeReadinessCleanup(t *testing.T) {
	c := createTestComponent(t)
	c.config.StandaloneServer = true
	c.config.BindAddress = "127.0.0.1:0"
	c.config.ReadinessKeys = []string{"graph-ingest"}
	entered := make(chan context.Context, 1)
	release := make(chan struct{})
	c.requestHook = func(ctx context.Context) {
		entered <- ctx
		<-release
	}
	if err := c.Initialize(); err != nil {
		t.Fatal(err)
	}
	if err := c.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	shutdownStarted := make(chan struct{})
	c.httpServer.RegisterOnShutdown(func() { close(shutdownStarted) })
	requestDone := make(chan error, 1)
	go func() {
		resp, err := http.Get("http://" + c.listener.Addr().String() + "/graphql")
		if resp != nil {
			_ = resp.Body.Close()
		}
		requestDone <- err
	}()
	handlerCtx := <-entered
	stopDone := make(chan error, 1)
	go func() { stopDone <- c.Stop(t.Context()) }()
	<-shutdownStarted
	if handlerCtx.Err() != nil || c.readinessSet == nil {
		t.Fatal("request or readiness authority ended before admitted request")
	}
	close(release)
	if err := <-stopDone; err != nil {
		t.Fatal(err)
	}
	if err := <-requestDone; err != nil {
		t.Fatal(err)
	}
	if handlerCtx.Err() == nil || c.readinessSet != nil {
		t.Fatal("terminal authority was not cleared after admitted request")
	}
	if err := c.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestLifecycleSharedMuxStopFencesAndWaitsOutsideLifecycleLock(t *testing.T) {
	c := createTestComponent(t)
	if err := c.Initialize(); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	c.RegisterHTTPHandlers("", mux)
	entered := make(chan struct{})
	release := make(chan struct{})
	c.requestHook = func(context.Context) {
		close(entered)
		<-release
	}
	if err := c.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	requestDone := make(chan struct{})
	go func() {
		mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/graphql", nil))
		close(requestDone)
	}()
	<-entered
	stopCtx := &observedContext{Context: t.Context(), seen: make(chan struct{})}
	stopDone := make(chan error, 1)
	go func() { stopDone <- c.Stop(stopCtx) }()
	<-stopCtx.seen
	c.lifecycleMu.Lock()
	c.lifecycleMu.Unlock()
	select {
	case err := <-stopDone:
		t.Fatalf("Stop returned before admitted shared request: %v", err)
	default:
	}
	refused := httptest.NewRecorder()
	mux.ServeHTTP(refused, httptest.NewRequest(http.MethodPost, "/graphql", nil))
	if refused.Code != http.StatusServiceUnavailable {
		t.Fatalf("request after fence status = %d", refused.Code)
	}
	close(release)
	<-requestDone
	if err := <-stopDone; err != nil {
		t.Fatal(err)
	}
}

func TestLifecycleStandaloneDeadlineStopIsTerminalWithoutReplay(t *testing.T) {
	c := createTestComponent(t)
	c.config.StandaloneServer = true
	c.config.BindAddress = "127.0.0.1:0"
	entered := make(chan struct{})
	release := make(chan struct{})
	c.requestHook = func(context.Context) {
		close(entered)
		<-release
	}
	if err := c.Initialize(); err != nil {
		t.Fatal(err)
	}
	if err := c.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	requestDone := make(chan struct{})
	go func() {
		resp, _ := http.Get("http://" + c.listener.Addr().String() + "/graphql")
		if resp != nil {
			_ = resp.Body.Close()
		}
		close(requestDone)
	}()
	<-entered
	stopCtx, cancel := context.WithCancel(t.Context())
	shutdownStarted := make(chan struct{})
	c.httpServer.RegisterOnShutdown(func() { close(shutdownStarted) })
	stopDone := make(chan error, 1)
	go func() { stopDone <- c.Stop(stopCtx) }()
	<-shutdownStarted
	cancel()
	if err := <-stopDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("deadline Stop = %v", err)
	}
	close(release)
	<-requestDone
	if err := c.Stop(t.Context()); err != nil {
		t.Fatalf("terminal Stop replayed cleanup: %v", err)
	}
}

func TestLifecycleStandaloneRegistersPreparedInferenceBeforeServe(t *testing.T) {
	c := createTestComponent(t)
	c.config.StandaloneServer = true
	c.config.BindAddress = "127.0.0.1:0"
	c.config.EnableInferenceAPI = true
	c.prepareInference = func(context.Context) (*inference.HTTPHandler, error) {
		return inference.NewHTTPHandler(inference.NewNATSAnomalyStorage(nil, c.logger), nil, c.logger), nil
	}
	if err := c.Initialize(); err != nil {
		t.Fatal(err)
	}
	if err := c.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	response, err := http.Get("http://" + c.listener.Addr().String() + "/inference/stats")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		t.Fatal("prepared standalone inference route was not registered")
	}
	if err := c.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestLifecycleSharedInferenceRouteUsesAdmissionFence(t *testing.T) {
	c := createTestComponent(t)
	c.config.EnableInferenceAPI = true
	c.prepareInference = func(context.Context) (*inference.HTTPHandler, error) {
		return inference.NewHTTPHandler(inference.NewNATSAnomalyStorage(nil, c.logger), nil, c.logger), nil
	}
	if err := c.Initialize(); err != nil {
		t.Fatal(err)
	}
	if err := c.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	c.RegisterHTTPHandlers("/graph", mux)
	entered := make(chan struct{})
	release := make(chan struct{})
	cancelReached := make(chan struct{})
	c.beforeRuntimeCancel = func() { close(cancelReached) }
	c.requestHook = func(context.Context) {
		close(entered)
		<-release
	}
	requestDone := make(chan struct{})
	go func() {
		mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/graph/inference/stats", nil))
		close(requestDone)
	}()
	<-entered
	stopCtx := &observedContext{Context: t.Context(), seen: make(chan struct{})}
	stopDone := make(chan error, 1)
	go func() { stopDone <- c.Stop(stopCtx) }()
	<-stopCtx.seen
	c.lifecycleMu.Lock()
	c.lifecycleMu.Unlock()
	select {
	case err := <-stopDone:
		t.Fatalf("Stop returned before admitted inference request: %v", err)
	default:
	}
	select {
	case <-cancelReached:
		t.Fatal("runtime cancellation reached before admitted inference request returned")
	default:
	}
	late := httptest.NewRecorder()
	mux.ServeHTTP(late, httptest.NewRequest(http.MethodGet, "/graph/inference/stats", nil))
	if late.Code != http.StatusServiceUnavailable {
		t.Fatalf("late inference request status = %d", late.Code)
	}
	close(release)
	<-requestDone
	<-cancelReached
	if err := <-stopDone; err != nil {
		t.Fatal(err)
	}
}

func TestLifecycleSuccessfulStartPublishesRunningBeforeReleasingStop(t *testing.T) {
	c := createTestComponent(t)
	if err := c.Initialize(); err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{})
	release := make(chan struct{})
	c.beforeStartDoneClose = func() {
		close(ready)
		<-release
	}
	startDone := make(chan error, 1)
	go func() { startDone <- c.Start(t.Context()) }()
	<-ready
	c.mu.RLock()
	running := c.running
	startTime := c.startTime
	c.mu.RUnlock()
	c.lifecycleMu.Lock()
	startPending := c.startDone != nil
	c.lifecycleMu.Unlock()
	if !running || startTime.IsZero() || !startPending {
		t.Fatal("successful Start state was not published before completion")
	}
	stopCtx := &observedContext{Context: t.Context(), seen: make(chan struct{})}
	stopDone := make(chan error, 1)
	go func() { stopDone <- c.Stop(stopCtx) }()
	<-stopCtx.seen
	c.lifecycleMu.Lock()
	c.lifecycleMu.Unlock()
	c.mu.Lock()
	c.mu.Unlock()
	select {
	case err := <-stopDone:
		t.Fatalf("Stop returned before Start completed: %v", err)
	default:
	}
	close(release)
	if err := <-startDone; err != nil {
		t.Fatal(err)
	}
	if err := <-stopDone; err != nil {
		t.Fatal(err)
	}
	c.mu.RLock()
	running = c.running
	c.mu.RUnlock()
	if running || !c.terminal || c.Health().Healthy {
		t.Fatal("terminal Stop was overwritten by late Start publication")
	}
}
