package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/health"
	"github.com/c360studio/semstreams/internal/lifecyclejoin"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/stretchr/testify/require"
)

type failingMetricsServer struct {
	stopErr   error
	stopCalls atomic.Int32
}

func (*failingMetricsServer) Start() error { return nil }
func (s *failingMetricsServer) Stop() error {
	s.stopCalls.Add(1)
	return s.stopErr
}

type contextStopSpy struct {
	name      string
	status    Status
	stopErr   error
	stopCalls atomic.Int32
	contexts  chan context.Context
}

func (s *contextStopSpy) Name() string                                  { return s.name }
func (s *contextStopSpy) Start(context.Context) error                   { return nil }
func (s *contextStopSpy) Status() Status                                { return s.status }
func (s *contextStopSpy) IsHealthy() bool                               { return true }
func (s *contextStopSpy) GetStatus() Info                               { return Info{Name: s.name, Status: s.status} }
func (s *contextStopSpy) Health() health.Status                         { return health.NewHealthy(s.name, "test") }
func (s *contextStopSpy) RegisterMetrics(metric.MetricsRegistrar) error { return nil }
func (s *contextStopSpy) Stop(ctx context.Context) error {
	s.stopCalls.Add(1)
	if s.contexts != nil {
		s.contexts <- ctx
	}
	return s.stopErr
}

func TestBaseServiceRejectsNilLifecycleContextBeforeState(t *testing.T) {
	svc := NewBaseServiceWithOptions("nil-context", nil)

	err := svc.Start(nil)
	require.Error(t, err)
	require.True(t, errs.IsInvalid(err))
	require.Equal(t, StatusStopped, svc.Status())

	svc.status.Store(StatusRunning)
	err = svc.Stop(nil)
	require.Error(t, err)
	require.True(t, errs.IsInvalid(err))
	require.Equal(t, StatusRunning, svc.Status())
}

func TestMetricsStopRetainsAndReplaysFirstTeardownFailure(t *testing.T) {
	base := NewBaseServiceWithOptions("metrics", nil)
	runCtx, cancel := context.WithCancel(context.Background())
	require.NoError(t, base.Start(runCtx))
	serverErr := errors.New("injected metrics server stop failure")
	server := &failingMetricsServer{stopErr: serverErr}
	m := &Metrics{
		BaseService: base,
		server:      server,
		generation:  lifecyclejoin.NewGeneration(cancel, func() {}),
	}

	firstErr := m.Stop(context.Background())
	require.ErrorIs(t, firstErr, serverErr)
	secondErr := m.Stop(context.Background())
	require.EqualError(t, secondErr, firstErr.Error())
	require.Equal(t, int32(1), server.stopCalls.Load())
}

func TestBaseServiceStopSignalsLifetimeBeforeCanceledJoin(t *testing.T) {
	svc := NewBaseServiceWithOptions("canceled-stop", nil)
	runtimeCtx, runtimeCancel := context.WithCancel(context.Background())
	defer runtimeCancel()
	require.NoError(t, svc.Start(runtimeCtx))

	stopCtx, stopCancel := context.WithCancel(context.Background())
	stopCancel()
	err := svc.Stop(stopCtx)
	require.ErrorIs(t, err, context.Canceled)

	require.NotEqual(t, StatusRunning, svc.Status(), "Stop must signal the active generation before returning")
}

func TestBaseServiceStopJoinsInitialHealthCheckWithoutLateCallback(t *testing.T) {
	checkEntered := make(chan struct{})
	releaseCheck := make(chan struct{})
	callbackStarted := make(chan struct{}, 1)
	svc := NewBaseServiceWithOptions(
		"tracked-health",
		nil,
		WithHealthInterval(time.Hour),
		WithHealthCheck(func() error {
			close(checkEntered)
			<-releaseCheck
			return nil
		}),
	)
	svc.OnHealthChange(func(bool) { callbackStarted <- struct{}{} })
	require.NoError(t, svc.Start(context.Background()))

	select {
	case <-checkEntered:
	case <-time.After(time.Second):
		t.Fatal("initial health check did not start")
	}
	canceled, cancelStop := context.WithCancel(context.Background())
	cancelStop()
	require.ErrorIs(t, svc.Stop(canceled), context.Canceled)
	stopDone := make(chan error, 1)
	go func() { stopDone <- svc.Stop(context.Background()) }()
	close(releaseCheck)
	require.NoError(t, <-stopDone)
	require.False(t, svc.IsHealthy())
	require.Equal(t, StatusStopped, svc.Status())

	select {
	case <-callbackStarted:
		t.Fatal("health callback ran after terminal shutdown began")
	case <-time.After(100 * time.Millisecond):
		// Failure bound only: the check and Stop are synchronized by channels.
	}
}

func TestManagerStopAllRejectsNilBeforeCallingServices(t *testing.T) {
	manager := createTestServiceManager(ManagerConfig{}, nil)
	spy := &contextStopSpy{name: "spy", status: StatusRunning}
	manager.services[spy.name] = spy
	manager.order = []string{spy.name}

	err := manager.StopAll(nil)
	require.Error(t, err)
	require.True(t, errs.IsInvalid(err))
	require.Zero(t, spy.stopCalls.Load())
}

func TestManagerStopAllPassesExactContextInReverseOrderAndAggregates(t *testing.T) {
	manager := createTestServiceManager(ManagerConfig{}, nil)
	order := make(chan string, 2)
	wantErr := errors.New("stop failed")
	a := &contextStopSpy{name: "a", status: StatusRunning, contexts: make(chan context.Context, 1)}
	b := &contextStopSpy{name: "b", status: StatusRunning, stopErr: wantErr, contexts: make(chan context.Context, 1)}
	aContexts, bContexts := a.contexts, b.contexts
	a.contexts = make(chan context.Context, 1)
	b.contexts = make(chan context.Context, 1)

	// Wrap observation without relying on wall-clock ordering.
	manager.services["a"] = serviceFunc{Service: a, stop: func(ctx context.Context) error {
		order <- "a"
		aContexts <- ctx
		return nil
	}}
	manager.services["b"] = serviceFunc{Service: b, stop: func(ctx context.Context) error {
		order <- "b"
		bContexts <- ctx
		return wantErr
	}}
	manager.order = []string{"a", "b"}

	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "caller")
	err := manager.StopAll(ctx)
	require.ErrorIs(t, err, wantErr)
	require.Equal(t, "b", <-order)
	require.Equal(t, "a", <-order)
	require.Same(t, ctx, <-bContexts)
	require.Same(t, ctx, <-aContexts)
}

type serviceFunc struct {
	Service
	stop func(context.Context) error
}

func (s serviceFunc) Stop(ctx context.Context) error { return s.stop(ctx) }

func TestBaseServiceConcurrentStopsShareCompletion(t *testing.T) {
	svc := NewBaseServiceWithOptions("concurrent-stop", nil)
	require.NoError(t, svc.Start(context.Background()))

	const callers = 8
	results := make(chan error, callers)
	var ready sync.WaitGroup
	ready.Add(callers)
	start := make(chan struct{})
	for range callers {
		go func() {
			ready.Done()
			<-start
			results <- svc.Stop(context.Background())
		}()
	}
	ready.Wait()
	close(start)
	for range callers {
		require.NoError(t, <-results)
	}
	require.Equal(t, StatusStopped, svc.Status())
}

type controlledGenerationComponent struct {
	*mockDiscoverableComponent
	startEntered    chan struct{}
	runtimeCanceled chan struct{}
	allowStartExit  chan struct{}
	startReturned   chan struct{}
	stopEntered     chan struct{}
	stopCalls       atomic.Int32
	startOnce       sync.Once
	cancelOnce      sync.Once
	returnOnce      sync.Once
	stopOnce        sync.Once
}

func newControlledGenerationComponent(name string) *controlledGenerationComponent {
	return &controlledGenerationComponent{
		mockDiscoverableComponent: &mockDiscoverableComponent{
			metadata: component.Metadata{Name: name, Type: "processor"},
		},
		startEntered:    make(chan struct{}),
		runtimeCanceled: make(chan struct{}),
		allowStartExit:  make(chan struct{}),
		startReturned:   make(chan struct{}),
		stopEntered:     make(chan struct{}),
	}
}

func (*controlledGenerationComponent) Initialize() error { return nil }

func (c *controlledGenerationComponent) Start(ctx context.Context) error {
	c.startOnce.Do(func() { close(c.startEntered) })
	<-ctx.Done()
	c.cancelOnce.Do(func() { close(c.runtimeCanceled) })
	<-c.allowStartExit
	c.returnOnce.Do(func() { close(c.startReturned) })
	return ctx.Err()
}

func (c *controlledGenerationComponent) Stop(ctx context.Context) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "controlledGenerationComponent", "Stop", "nil context")
	}
	select {
	case <-c.startReturned:
	default:
		return errors.New("Stop overlapped Start finalization")
	}
	c.stopCalls.Add(1)
	c.stopOnce.Do(func() { close(c.stopEntered) })
	return nil
}

var _ component.LifecycleComponent = (*controlledGenerationComponent)(nil)

func newStartedSupervisorManager(t *testing.T) (*ComponentManager, context.CancelFunc) {
	t.Helper()
	cm := &ComponentManager{
		BaseService: NewBaseServiceWithOptions("component-manager", nil),
		components:  make(map[string]*component.ManagedComponent),
		runtimes:    make(map[string]*componentRuntime),
		registry:    component.NewRegistry(),
	}
	cm.initialized.Store(true)
	runtimeCtx, runtimeCancel := context.WithCancel(context.Background())
	require.NoError(t, cm.Start(runtimeCtx))
	return cm, runtimeCancel
}

func TestComponentManagerDynamicRuntimeIgnoresRequestCancellationAfterAdmission(t *testing.T) {
	cm, runtimeCancel := newStartedSupervisorManager(t)
	defer runtimeCancel()
	comp := newControlledGenerationComponent("request-owned")
	cm.mu.Lock()
	cm.components["request-owned"] = &component.ManagedComponent{
		Component: comp,
		State:     component.StateInitialized,
	}
	cm.mu.Unlock()

	requestCtx, requestCancel := context.WithCancel(context.Background())
	require.NoError(t, cm.startSingleComponent(requestCtx, "request-owned"))
	waitForSignal(t, comp.startEntered, "dynamic Start admission")
	requestCancel()
	select {
	case <-comp.runtimeCanceled:
		t.Fatal("request cancellation canceled admitted runtime")
	default:
	}

	stopResult := make(chan error, 1)
	go func() { stopResult <- cm.Stop(context.Background()) }()
	waitForSignal(t, comp.runtimeCanceled, "manager lifetime cancellation")
	close(comp.allowStartExit)
	waitForSignal(t, comp.stopEntered, "same-generation Stop")
	require.NoError(t, <-stopResult)
}

func TestComponentManagerStopContextExpiryHandsOffSameGeneration(t *testing.T) {
	cm, runtimeCancel := newStartedSupervisorManager(t)
	defer runtimeCancel()
	comp := newControlledGenerationComponent("handoff")
	cm.mu.Lock()
	cm.components["handoff"] = &component.ManagedComponent{
		Component: comp,
		State:     component.StateInitialized,
	}
	cm.mu.Unlock()
	require.NoError(t, cm.startSingleComponent(context.Background(), "handoff"))
	waitForSignal(t, comp.startEntered, "dynamic Start entry")

	firstCtx, cancelFirst := context.WithCancel(context.Background())
	firstResult := make(chan error, 1)
	go func() { firstResult <- cm.Stop(firstCtx) }()
	waitForSignal(t, comp.runtimeCanceled, "generation cancellation")
	cancelFirst()
	require.ErrorIs(t, <-firstResult, context.Canceled)
	require.Zero(t, comp.stopCalls.Load(), "expired Stop must not overlap the in-flight Start")

	close(comp.allowStartExit)
	waitForSignal(t, comp.startReturned, "Start finalization")
	require.NoError(t, cm.Stop(context.Background()))
	waitForSignal(t, comp.stopEntered, "resumed same-generation Stop")
	require.Equal(t, int32(1), comp.stopCalls.Load())
}
