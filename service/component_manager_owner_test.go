package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/stretchr/testify/require"
)

type componentManagerObservedContext struct {
	context.Context
	observed chan struct{}
	once     sync.Once
}

func (c *componentManagerObservedContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.observed) })
	return c.Context.Done()
}

type orderedManagerComponent struct {
	*mockDiscoverableComponent
	mu        *sync.Mutex
	stopOrder *[]string
	stopErr   error
}

func (*orderedManagerComponent) Initialize() error           { return nil }
func (*orderedManagerComponent) Start(context.Context) error { return nil }
func (c *orderedManagerComponent) Stop(context.Context) error {
	c.mu.Lock()
	*c.stopOrder = append(*c.stopOrder, c.Meta().Name)
	c.mu.Unlock()
	return c.stopErr
}

var _ component.LifecycleComponent = (*orderedManagerComponent)(nil)

type countingManagerHealthComponent struct {
	*mockDiscoverableComponent
	healthCalls   atomic.Int32
	dataFlowCalls atomic.Int32
}

type blockingManagerHealthComponent struct {
	*mockDiscoverableComponent
	manager         *ComponentManager
	healthEntered   chan struct{}
	healthReentered chan struct{}
	releaseHealth   chan struct{}
	healthReturned  chan struct{}
	stopEntered     chan struct{}
	healthCalls     atomic.Int32
	stopCalls       atomic.Int32
	healthOnce      sync.Once
	reenteredOnce   sync.Once
	returnedOnce    sync.Once
	stopOnce        sync.Once
}

func newBlockingManagerHealthComponent(name string) *blockingManagerHealthComponent {
	return &blockingManagerHealthComponent{
		mockDiscoverableComponent: &mockDiscoverableComponent{
			metadata: component.Metadata{Name: name, Type: "processor"},
		},
		healthEntered:   make(chan struct{}),
		healthReentered: make(chan struct{}),
		releaseHealth:   make(chan struct{}),
		healthReturned:  make(chan struct{}),
		stopEntered:     make(chan struct{}),
	}
}

func (*blockingManagerHealthComponent) Initialize() error             { return nil }
func (c *blockingManagerHealthComponent) Start(context.Context) error { return nil }

func (c *blockingManagerHealthComponent) Health() component.HealthStatus {
	c.healthCalls.Add(1)
	c.healthOnce.Do(func() { close(c.healthEntered) })
	c.manager.mu.Lock()
	c.reenteredOnce.Do(func() { close(c.healthReentered) })
	c.manager.mu.Unlock()
	<-c.releaseHealth
	c.returnedOnce.Do(func() { close(c.healthReturned) })
	return component.HealthStatus{Healthy: true}
}

func (c *blockingManagerHealthComponent) Stop(context.Context) error {
	c.stopCalls.Add(1)
	select {
	case <-c.healthReturned:
	default:
		return errors.New("component Stop entered before Health returned")
	}
	c.stopOnce.Do(func() { close(c.stopEntered) })
	return nil
}

var _ component.LifecycleComponent = (*blockingManagerHealthComponent)(nil)

func (c *countingManagerHealthComponent) Health() component.HealthStatus {
	c.healthCalls.Add(1)
	return component.HealthStatus{Healthy: true}
}

func (c *countingManagerHealthComponent) DataFlow() component.FlowMetrics {
	c.dataFlowCalls.Add(1)
	return component.FlowMetrics{}
}

func TestComponentManagerNilContextsPreserveLifecycleState(t *testing.T) {
	cm := &ComponentManager{
		BaseService: NewBaseServiceWithOptions("component-manager", nil),
		components:  make(map[string]*component.ManagedComponent),
		registry:    component.NewRegistry(),
	}
	require.Error(t, cm.Start(nil))
	require.Error(t, cm.Stop(nil))
	require.False(t, cm.lifecycleUsed)
	require.False(t, cm.cleanupPending)
	require.False(t, cm.lifecycleTerminal)
	require.Nil(t, cm.startDone)
	require.Nil(t, cm.cancel)

	require.NoError(t, cm.Stop(t.Context()))
	require.Error(t, cm.Stop(nil))
	require.True(t, cm.lifecycleUsed)
	require.True(t, cm.lifecycleTerminal)
}

func TestComponentManagerTerminalStopFencesComponentBorrows(t *testing.T) {
	cm := &ComponentManager{
		BaseService: NewBaseServiceWithOptions("component-manager", nil),
		components:  make(map[string]*component.ManagedComponent),
		registry:    component.NewRegistry(),
	}
	cm.initialized.Store(true)
	require.NoError(t, cm.Stop(t.Context()))

	called := false
	err := cm.withComponents(func(map[string]*component.ManagedComponent) error {
		called = true
		return nil
	})
	require.Error(t, err)
	require.True(t, errs.IsTransient(err))
	require.False(t, called)
	require.ErrorContains(t, cm.Start(t.Context()), "already used")
	require.NoError(t, cm.Stop(t.Context()))
}

func TestComponentManagerTerminalHealthProjectionInvokesNoChild(t *testing.T) {
	comp := &countingManagerHealthComponent{
		mockDiscoverableComponent: &mockDiscoverableComponent{
			metadata: component.Metadata{Name: "observed", Type: "processor"},
		},
	}
	cm := &ComponentManager{
		BaseService: NewBaseServiceWithOptions("component-manager", nil),
		components: map[string]*component.ManagedComponent{
			"observed": {Component: comp, State: component.StateInitialized},
		},
		registry: component.NewRegistry(),
	}
	cm.initialized.Store(true)
	require.NoError(t, cm.Stop(t.Context()))

	require.Empty(t, cm.GetComponentHealth())
	require.Contains(t, cm.GetComponentStatus(), "observed")
	require.Equal(t, int32(0), comp.healthCalls.Load())
	require.Equal(t, int32(0), comp.dataFlowCalls.Load())
}

func TestComponentManagerStopFencesAndWaitsGetComponentHealthBorrow(t *testing.T) {
	comp := newBlockingManagerHealthComponent("observed")
	cm := &ComponentManager{
		BaseService: NewBaseServiceWithOptions("component-manager", nil),
		components: map[string]*component.ManagedComponent{
			"observed": {Component: comp, State: component.StateInitialized},
		},
		registry: component.NewRegistry(),
		runtimes: make(map[string]*componentRuntime),
	}
	comp.manager = cm
	cm.initialized.Store(true)
	require.NoError(t, cm.Start(t.Context()))

	healthResult := make(chan map[string]component.HealthStatus, 1)
	go func() { healthResult <- cm.GetComponentHealth() }()
	<-comp.healthEntered
	<-comp.healthReentered

	stopCtx := &componentManagerObservedContext{Context: t.Context(), observed: make(chan struct{})}
	stopResult := make(chan error, 1)
	go func() { stopResult <- cm.Stop(stopCtx) }()
	<-stopCtx.observed
	select {
	case err := <-stopResult:
		t.Fatalf("Stop returned before admitted Health completed: %v", err)
	default:
	}
	select {
	case <-comp.stopEntered:
		t.Fatal("child Stop entered before admitted Health completed")
	default:
	}

	close(comp.releaseHealth)
	require.True(t, (<-healthResult)["observed"].Healthy)
	require.NoError(t, <-stopResult)
	<-comp.stopEntered
	require.Equal(t, int32(1), comp.stopCalls.Load())

	healthCalls := comp.healthCalls.Load()
	require.Empty(t, cm.GetComponentHealth())
	require.Equal(t, healthCalls, comp.healthCalls.Load())
}

func TestComponentManagerStopWaitsForHealthPublisherBorrowBeforeChildStop(t *testing.T) {
	comp := newBlockingManagerHealthComponent("published")
	cm := &ComponentManager{
		BaseService: NewBaseServiceWithOptions("component-manager", nil),
		components: map[string]*component.ManagedComponent{
			"published": {Component: comp, State: component.StateInitialized},
		},
		registry:   component.NewRegistry(),
		runtimes:   make(map[string]*componentRuntime),
		natsClient: &natsclient.Client{},
	}
	comp.manager = cm
	cm.initialized.Store(true)
	require.NoError(t, cm.Start(t.Context()))
	<-comp.healthEntered
	<-comp.healthReentered

	stopCtx := &componentManagerObservedContext{Context: t.Context(), observed: make(chan struct{})}
	stopResult := make(chan error, 1)
	go func() { stopResult <- cm.Stop(stopCtx) }()
	<-stopCtx.observed
	select {
	case err := <-stopResult:
		t.Fatalf("Stop returned before publisher Health completed: %v", err)
	default:
	}
	select {
	case <-comp.stopEntered:
		t.Fatal("child Stop entered before publisher Health completed")
	default:
	}

	close(comp.releaseHealth)
	require.NoError(t, <-stopResult)
	<-comp.stopEntered
	require.Equal(t, int32(1), comp.stopCalls.Load())
}

func TestComponentManagerStopFencesAndWaitsAdmittedComponentBorrow(t *testing.T) {
	comp := newLiveAuthorityStopComponent("borrowed")
	cm := &ComponentManager{
		BaseService: NewBaseServiceWithOptions("component-manager", nil),
		components: map[string]*component.ManagedComponent{
			"borrowed": {Component: comp, State: component.StateInitialized},
		},
		registry: component.NewRegistry(),
		runtimes: make(map[string]*componentRuntime),
	}
	cm.initialized.Store(true)
	require.NoError(t, cm.Start(t.Context()))

	borrowEntered := make(chan struct{})
	releaseBorrow := make(chan struct{})
	borrowResult := make(chan error, 1)
	go func() {
		borrowResult <- cm.withComponents(func(map[string]*component.ManagedComponent) error {
			_ = cm.GetComponentStatus()
			close(borrowEntered)
			<-releaseBorrow
			return nil
		})
	}()
	<-borrowEntered

	stopCtx := &componentManagerObservedContext{Context: t.Context(), observed: make(chan struct{})}
	stopResult := make(chan error, 1)
	go func() { stopResult <- cm.Stop(stopCtx) }()
	<-stopCtx.observed
	called := false
	err := cm.withComponents(func(map[string]*component.ManagedComponent) error {
		called = true
		return nil
	})
	require.Error(t, err)
	require.True(t, errs.IsTransient(err))
	require.False(t, called)
	concurrentErr := cm.Stop(t.Context())
	require.Error(t, concurrentErr)
	require.True(t, errs.IsTransient(concurrentErr))

	close(releaseBorrow)
	require.NoError(t, <-borrowResult)
	require.NoError(t, <-stopResult)
	<-comp.stopped
	require.Equal(t, int32(1), comp.stopCalls.Load())
}

func TestComponentManagerStopUsesReverseStartOrderAndAggregates(t *testing.T) {
	var orderMu sync.Mutex
	var stopOrder []string
	errA := errors.New("stop a")
	errC := errors.New("stop c")
	components := make(map[string]*component.ManagedComponent)
	for name, stopErr := range map[string]error{"a": errA, "b": nil, "c": errC} {
		comp := &orderedManagerComponent{
			mockDiscoverableComponent: &mockDiscoverableComponent{
				metadata: component.Metadata{Name: name, Type: "processor"},
			},
			mu:        &orderMu,
			stopOrder: &stopOrder,
			stopErr:   stopErr,
		}
		components[name] = &component.ManagedComponent{Component: comp, State: component.StateInitialized}
	}
	cm := &ComponentManager{
		BaseService: NewBaseServiceWithOptions("component-manager", nil),
		components:  components,
		registry:    component.NewRegistry(),
		runtimes:    make(map[string]*componentRuntime),
	}
	cm.initialized.Store(true)
	require.NoError(t, cm.Start(t.Context()))
	startOrder := append([]string(nil), cm.startOrder...)

	err := cm.Stop(t.Context())
	require.ErrorIs(t, err, errA)
	require.ErrorIs(t, err, errC)
	require.Len(t, stopOrder, len(startOrder))
	for i, name := range startOrder {
		require.Equal(t, name, stopOrder[len(stopOrder)-1-i])
	}
	require.NoError(t, cm.Stop(t.Context()))
	require.Len(t, stopOrder, len(startOrder))
}
