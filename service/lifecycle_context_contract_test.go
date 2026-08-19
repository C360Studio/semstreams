package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/health"
	"github.com/c360studio/semstreams/internal/lifecyclejoin"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/storage"
	"github.com/c360studio/semstreams/storage/storeregistry"
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

type liveAuthorityStopComponent struct {
	*mockDiscoverableComponent
	startCtx  context.Context
	stopped   chan struct{}
	stopCalls atomic.Int32
	stopOnce  sync.Once
}

func newLiveAuthorityStopComponent(name string) *liveAuthorityStopComponent {
	return &liveAuthorityStopComponent{
		mockDiscoverableComponent: &mockDiscoverableComponent{
			metadata: component.Metadata{Name: name, Type: "processor"},
		},
		stopped: make(chan struct{}),
	}
}

func (*liveAuthorityStopComponent) Initialize() error { return nil }

func (c *liveAuthorityStopComponent) Start(ctx context.Context) error {
	c.startCtx = ctx
	return nil
}

func (c *liveAuthorityStopComponent) Stop(context.Context) error {
	c.stopCalls.Add(1)
	select {
	case <-c.startCtx.Done():
		return errors.New("Start authority canceled before component Stop")
	default:
	}
	c.stopOnce.Do(func() { close(c.stopped) })
	return nil
}

var _ component.LifecycleComponent = (*liveAuthorityStopComponent)(nil)

type lateRegisteringStoreComponent struct {
	*mockDiscoverableComponent
	startEntered  chan struct{}
	startCanceled chan struct{}
	releaseStart  chan struct{}
	stopCalls     atomic.Int32
	store         storage.StreamableStore
}

func newLateRegisteringStoreComponent(name string) *lateRegisteringStoreComponent {
	return &lateRegisteringStoreComponent{
		mockDiscoverableComponent: &mockDiscoverableComponent{
			metadata: component.Metadata{Name: name, Type: "storage"},
		},
		startEntered:  make(chan struct{}),
		startCanceled: make(chan struct{}),
		releaseStart:  make(chan struct{}),
		store:         &fakeStreamable{id: name},
	}
}

func (*lateRegisteringStoreComponent) Initialize() error { return nil }

func (c *lateRegisteringStoreComponent) Start(ctx context.Context) error {
	close(c.startEntered)
	<-ctx.Done()
	close(c.startCanceled)
	<-c.releaseStart
	return nil
}

func (c *lateRegisteringStoreComponent) Stop(context.Context) error {
	c.stopCalls.Add(1)
	return nil
}

func (c *lateRegisteringStoreComponent) ProvidedStores() map[string]storage.StreamableStore {
	return map[string]storage.StreamableStore{c.Meta().Name: c.store}
}

var _ component.LifecycleComponent = (*lateRegisteringStoreComponent)(nil)
var _ component.StoreProvider = (*lateRegisteringStoreComponent)(nil)

type failedPartialStartComponent struct {
	*mockDiscoverableComponent
	startCtx        context.Context
	startErr        error
	stopCalls       atomic.Int32
	stopSawCanceled chan struct{}
	stopErr         error
	stopOnce        sync.Once
}

func (*failedPartialStartComponent) Initialize() error { return nil }

func (c *failedPartialStartComponent) Start(ctx context.Context) error {
	c.startCtx = ctx
	return c.startErr
}

func (c *failedPartialStartComponent) Stop(context.Context) error {
	call := c.stopCalls.Add(1)
	select {
	case <-c.startCtx.Done():
		c.stopOnce.Do(func() { close(c.stopSawCanceled) })
		if call == 1 {
			return c.stopErr
		}
		return nil
	default:
		return errors.New("failed partial Start authority remained live during Stop")
	}
}

var _ component.LifecycleComponent = (*failedPartialStartComponent)(nil)

type rollbackStoreComponent struct {
	*mockDiscoverableComponent
	stopCalls atomic.Int32
	store     storage.StreamableStore
}

func (*rollbackStoreComponent) Initialize() error           { return nil }
func (*rollbackStoreComponent) Start(context.Context) error { return nil }
func (c *rollbackStoreComponent) Stop(context.Context) error {
	c.stopCalls.Add(1)
	return nil
}
func (c *rollbackStoreComponent) ProvidedStores() map[string]storage.StreamableStore {
	return map[string]storage.StreamableStore{"shared": c.store}
}

var _ component.LifecycleComponent = (*rollbackStoreComponent)(nil)
var _ component.StoreProvider = (*rollbackStoreComponent)(nil)

type gracefulArbitrationStoreComponent struct {
	*mockDiscoverableComponent
	runtimeCtx    context.Context
	storeRegistry *storeregistry.Registry
	store         storage.StreamableStore
	stopCalls     atomic.Int32
}

func (*gracefulArbitrationStoreComponent) Initialize() error { return nil }
func (c *gracefulArbitrationStoreComponent) Start(ctx context.Context) error {
	c.runtimeCtx = ctx
	return nil
}
func (c *gracefulArbitrationStoreComponent) Stop(context.Context) error {
	c.stopCalls.Add(1)
	if err := c.runtimeCtx.Err(); err != nil {
		return errors.Join(errors.New("runtime canceled before graceful Stop"), err)
	}
	if _, registered := c.storeRegistry.Streamable(c.Meta().Name); registered {
		return errors.New("store fence did not run before graceful Stop")
	}
	return nil
}
func (c *gracefulArbitrationStoreComponent) ProvidedStores() map[string]storage.StreamableStore {
	return map[string]storage.StreamableStore{c.Meta().Name: c.store}
}

var _ component.LifecycleComponent = (*gracefulArbitrationStoreComponent)(nil)
var _ component.StoreProvider = (*gracefulArbitrationStoreComponent)(nil)

func TestComponentManagerCallsComponentStopBeforeRuntimeCancellation(t *testing.T) {
	cm := &ComponentManager{
		BaseService: NewBaseServiceWithOptions("component-manager", nil),
		components:  make(map[string]*component.ManagedComponent),
		runtimes:    make(map[string]*componentRuntime),
		registry:    component.NewRegistry(),
	}
	comp := newLiveAuthorityStopComponent("live-stop")
	cm.components["live-stop"] = &component.ManagedComponent{
		Component: comp,
		State:     component.StateInitialized,
	}
	cm.initialized.Store(true)
	require.NoError(t, cm.Start(t.Context()))

	require.NoError(t, cm.Stop(t.Context()))
	waitForSignal(t, comp.stopped, "component Stop with live Start authority")
	require.ErrorIs(t, comp.startCtx.Err(), context.Canceled)
	require.Equal(t, int32(1), comp.stopCalls.Load())
	require.NoError(t, cm.Stop(t.Context()))
	require.Equal(t, int32(1), comp.stopCalls.Load())
	require.ErrorContains(t, cm.Start(t.Context()), "already used")
}

func TestComponentManagerStopDuringStartCancelsAndPreventsLateStoreCommit(t *testing.T) {
	cm := &ComponentManager{
		BaseService:   NewBaseServiceWithOptions("component-manager", nil),
		components:    make(map[string]*component.ManagedComponent),
		runtimes:      make(map[string]*componentRuntime),
		registry:      component.NewRegistry(),
		storeRegistry: storeregistry.New(),
		storeProvided: make(map[string][]string),
	}
	comp := newLateRegisteringStoreComponent("late-store")
	cm.components["late-store"] = &component.ManagedComponent{
		Component: comp,
		State:     component.StateInitialized,
	}
	cm.initialized.Store(true)

	startResult := make(chan error, 1)
	go func() { startResult <- cm.Start(t.Context()) }()
	<-comp.startEntered

	stopResult := make(chan error, 1)
	go func() { stopResult <- cm.Stop(t.Context()) }()
	<-comp.startCanceled
	close(comp.releaseStart)
	require.Error(t, <-startResult)
	require.NoError(t, <-stopResult)
	_, registered := cm.storeRegistry.Streamable("late-store")
	require.False(t, registered, "a canceled late Start must not commit its store")
	require.Equal(t, int32(1), comp.stopCalls.Load())
	require.NoError(t, cm.Stop(t.Context()))
	require.Equal(t, int32(1), comp.stopCalls.Load())
}

func TestComponentManagerFailedStartRollsBackSynchronouslyAndDoesNotReplay(t *testing.T) {
	wantErr := errors.New("partial start failed")
	comp := &failedPartialStartComponent{
		mockDiscoverableComponent: &mockDiscoverableComponent{
			metadata: component.Metadata{Name: "partial", Type: "processor"},
		},
		startErr:        wantErr,
		stopSawCanceled: make(chan struct{}),
	}
	cm := &ComponentManager{
		BaseService: NewBaseServiceWithOptions("component-manager", nil),
		components: map[string]*component.ManagedComponent{
			"partial": {Component: comp, State: component.StateInitialized},
		},
		runtimes: make(map[string]*componentRuntime),
		registry: component.NewRegistry(),
	}
	cm.initialized.Store(true)
	require.ErrorIs(t, cm.Start(t.Context()), wantErr)
	<-comp.stopSawCanceled
	require.Equal(t, int32(1), comp.stopCalls.Load())
	require.NoError(t, cm.Stop(context.Background()))
	require.Equal(t, int32(1), comp.stopCalls.Load())
}

func TestComponentManagerFailedStartCleanupFailureRetainsAuthorityForLaterStop(t *testing.T) {
	startErr := errors.New("partial start failed")
	cleanupErr := errors.New("partial cleanup failed")
	comp := &failedPartialStartComponent{
		mockDiscoverableComponent: &mockDiscoverableComponent{
			metadata: component.Metadata{Name: "partial", Type: "processor"},
		},
		startErr:        startErr,
		stopErr:         cleanupErr,
		stopSawCanceled: make(chan struct{}),
	}
	cm := &ComponentManager{
		BaseService: NewBaseServiceWithOptions("component-manager", nil),
		components: map[string]*component.ManagedComponent{
			"partial": {Component: comp, State: component.StateInitialized},
		},
		runtimes: make(map[string]*componentRuntime),
		registry: component.NewRegistry(),
	}
	cm.initialized.Store(true)

	err := cm.Start(t.Context())
	require.ErrorIs(t, err, startErr)
	require.ErrorIs(t, err, cleanupErr)
	require.True(t, cm.cleanupPending)
	require.False(t, cm.lifecycleTerminal)
	require.ErrorContains(t, cm.Start(t.Context()), "already used")
	require.NoError(t, cm.Stop(t.Context()))
	require.Equal(t, int32(2), comp.stopCalls.Load())
	require.True(t, cm.lifecycleTerminal)
	require.NoError(t, cm.Stop(t.Context()))
	require.Equal(t, int32(2), comp.stopCalls.Load())
}

func TestComponentManagerRunningStopDeregistersStoreBeforeLiveChildStop(t *testing.T) {
	storeRegistry := storeregistry.New()
	comp := &gracefulArbitrationStoreComponent{
		mockDiscoverableComponent: &mockDiscoverableComponent{
			metadata: component.Metadata{Name: "arbitrated", Type: "storage"},
		},
		storeRegistry: storeRegistry,
		store:         &fakeStreamable{id: "arbitrated"},
	}
	cm := &ComponentManager{
		BaseService: NewBaseServiceWithOptions("component-manager", nil),
		components: map[string]*component.ManagedComponent{
			"arbitrated": {Component: comp, State: component.StateStarted},
		},
		runtimes:      make(map[string]*componentRuntime),
		registry:      component.NewRegistry(),
		storeRegistry: storeRegistry,
		storeProvided: make(map[string][]string),
	}
	cm.initialized.Store(true)
	require.NoError(t, cm.Start(t.Context()))
	_, registered := storeRegistry.Streamable("arbitrated")
	require.True(t, registered)

	require.NoError(t, cm.Stop(t.Context()))
	require.ErrorIs(t, comp.runtimeCtx.Err(), context.Canceled)
	_, registered = storeRegistry.Streamable("arbitrated")
	require.False(t, registered)
	require.Equal(t, int32(1), comp.stopCalls.Load())
	require.NoError(t, cm.Stop(t.Context()))
	require.Equal(t, int32(1), comp.stopCalls.Load())
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
