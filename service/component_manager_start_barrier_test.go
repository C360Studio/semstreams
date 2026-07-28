package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/component"
)

// barrierTestComponent is a minimal LifecycleComponent whose Start behavior the
// barrier tests script: it can fail with a fixed error, or block until the test
// releases a gate. startReturned is closed as the LAST action of Start, so a
// test that observes it can assert Start really returned.
type barrierTestComponent struct {
	name     string
	startErr error

	// entered/release gate a blocking Start. Nil means Start returns
	// immediately. entered is closed when Start begins; Start then blocks
	// until release is closed.
	entered chan struct{}
	release chan struct{}

	startReturned chan struct{}
}

func newBarrierTestComponent(name string) *barrierTestComponent {
	return &barrierTestComponent{name: name, startReturned: make(chan struct{})}
}

func (c *barrierTestComponent) Meta() component.Metadata {
	return component.Metadata{Name: c.name, Type: "processor", Version: "1.0.0"}
}
func (c *barrierTestComponent) InputPorts() []component.Port         { return nil }
func (c *barrierTestComponent) OutputPorts() []component.Port        { return nil }
func (c *barrierTestComponent) ConfigSchema() component.ConfigSchema { return component.ConfigSchema{} }
func (c *barrierTestComponent) Health() component.HealthStatus {
	return component.HealthStatus{Healthy: true}
}
func (c *barrierTestComponent) DataFlow() component.FlowMetrics { return component.FlowMetrics{} }
func (c *barrierTestComponent) Initialize() error               { return nil }
func (c *barrierTestComponent) Stop(_ time.Duration) error      { return nil }

func (c *barrierTestComponent) Start(_ context.Context) error {
	defer close(c.startReturned)
	if c.entered != nil {
		close(c.entered)
		<-c.release
	}
	return c.startErr
}

var _ component.LifecycleComponent = (*barrierTestComponent)(nil)

// newBarrierTestManager builds a ComponentManager pre-loaded with the given
// components in StateInitialized, ready for Start. It bypasses config loading
// (unit scope) but drives the real startAllComponents launch path.
func newBarrierTestManager(t *testing.T, comps ...*barrierTestComponent) *ComponentManager {
	t.Helper()
	cm := &ComponentManager{
		BaseService: NewBaseServiceWithOptions("component-manager", nil),
		components:  make(map[string]*component.ManagedComponent),
		registry:    component.NewRegistry(),
	}
	for _, c := range comps {
		cm.components[c.name] = &component.ManagedComponent{
			Component: c,
			State:     component.StateInitialized,
		}
	}
	cm.initialized.Store(true)
	return cm
}

// TestComponentManagerStart_FailsClosedOnComponentStartError locks the
// framework-composition fail-closed requirement: a component whose Start
// returns an error must fail ComponentManager.Start with an error naming the
// component — not be absorbed by a fire-and-forget launch.
func TestComponentManagerStart_FailsClosedOnComponentStartError(t *testing.T) {
	boom := errors.New("bucket carries a binding retention")
	failing := newBarrierTestComponent("failing-comp")
	failing.startErr = boom
	ok := newBarrierTestComponent("healthy-comp")

	cm := newBarrierTestManager(t, failing, ok)
	defer func() { _ = cm.Stop(2 * time.Second) }()

	err := cm.Start(context.Background())
	require.Error(t, err, "a component Start failure must fail ComponentManager.Start")
	assert.Contains(t, err.Error(), "failing-comp", "the error must name the failed component")
	assert.ErrorIs(t, err, boom, "the component's own error must be wrapped, not replaced")

	// The failed component is recorded as failed with its error; the healthy
	// one still started.
	status := cm.GetComponentStatus()
	require.Contains(t, status, "failing-comp")
	assert.Equal(t, component.StateFailed, status["failing-comp"].State)
	assert.ErrorIs(t, status["failing-comp"].LastError, boom)
	require.Contains(t, status, "healthy-comp")
	assert.Equal(t, component.StateStarted, status["healthy-comp"].State)
}

// TestComponentManagerStart_JoinsMultipleFailures pins the "multiple boot-time
// failures are all reported" scenario: the joined error names every failed
// component, not only the first.
func TestComponentManagerStart_JoinsMultipleFailures(t *testing.T) {
	errA := errors.New("failure A")
	errB := errors.New("failure B")
	failA := newBarrierTestComponent("fail-a")
	failA.startErr = errA
	failB := newBarrierTestComponent("fail-b")
	failB.startErr = errB

	cm := newBarrierTestManager(t, failA, failB)
	defer func() { _ = cm.Stop(2 * time.Second) }()

	err := cm.Start(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fail-a", "first failed component must be named")
	assert.Contains(t, err.Error(), "fail-b", "second failed component must be named")
	assert.ErrorIs(t, err, errA)
	assert.ErrorIs(t, err, errB)
}

// TestComponentManagerStart_WaitsForAllComponentStarts locks the barrier
// itself: ComponentManager.Start must not return until every launched
// component Start has returned. A gated component blocks its Start until the
// test releases it; Start returning while the gate is held is the
// fire-and-forget bug.
func TestComponentManagerStart_WaitsForAllComponentStarts(t *testing.T) {
	gated := newBarrierTestComponent("gated-comp")
	gated.entered = make(chan struct{})
	gated.release = make(chan struct{})

	cm := newBarrierTestManager(t, gated)
	defer func() { _ = cm.Stop(2 * time.Second) }()

	done := make(chan error, 1)
	go func() { done <- cm.Start(context.Background()) }()

	// The component is now provably inside Start and blocked on the gate.
	<-gated.entered

	// Absence assertion: prove Start has NOT returned while a component Start
	// is still in flight. A bounded wait is unavoidable here (we are asserting
	// a negative); 500ms is orders of magnitude beyond the fire-and-forget
	// return path, which returns in microseconds once launch goroutines are
	// spawned.
	select {
	case err := <-done:
		t.Fatalf("ComponentManager.Start returned (err=%v) while a component Start was still in flight (fire-and-forget)", err)
	case <-time.After(500 * time.Millisecond):
	}

	// Release the gate; Start must now complete and the component must have
	// finished its Start before Start returned.
	close(gated.release)
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("ComponentManager.Start did not return after the gated component was released")
	}

	select {
	case <-gated.startReturned:
	default:
		t.Fatal("ComponentManager.Start returned before the component's Start had returned")
	}
	status := cm.GetComponentStatus()
	assert.Equal(t, component.StateStarted, status["gated-comp"].State)
}

// TestPerformDetailedHealthCheck_ReportsFailedComponent locks the health-truth
// requirement: a component in StateFailed must surface from the detailed health
// check as an error naming the component and its last error, and recovery to
// StateStarted must clear it. (The TryRLock never-block posture is covered by
// TestPerformDetailedHealthCheckDoesNotLeakReader, gh#508.)
func TestPerformDetailedHealthCheck_ReportsFailedComponent(t *testing.T) {
	lastErr := fmt.Errorf("start failed: %w", errors.New("no responders"))
	cm := &ComponentManager{
		BaseService: NewBaseServiceWithOptions("component-manager", nil),
		components:  make(map[string]*component.ManagedComponent),
		registry:    component.NewRegistry(),
	}
	comp := newBarrierTestComponent("broken-comp")
	cm.components["broken-comp"] = &component.ManagedComponent{
		Component: comp,
		State:     component.StateFailed,
		LastError: lastErr,
	}

	err := cm.performDetailedHealthCheck()
	require.Error(t, err, "a StateFailed component must make the detailed health check fail")
	assert.Contains(t, err.Error(), "broken-comp", "the health error must name the failed component")
	assert.ErrorIs(t, err, lastErr, "the health error must carry the component's LastError")

	// Recovery: a restart back to StateStarted clears the failure.
	cm.updateComponentState("broken-comp", component.StateStarted, nil)
	assert.NoError(t, cm.performDetailedHealthCheck(), "recovery to StateStarted must clear the health failure")

	// A wrapped mutex-guard sync.Mutex sanity: concurrent health checks while a
	// writer flips state must not race (exercised under -race).
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = cm.performDetailedHealthCheck()
		}()
	}
	cm.updateComponentState("broken-comp", component.StateFailed, lastErr)
	wg.Wait()
}
