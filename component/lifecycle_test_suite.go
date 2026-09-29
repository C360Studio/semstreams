package component

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// LifecycleFactory creates a new instance of a LifecycleComponent for testing.
// Factories must return independent instances and permit concurrent calls. A
// worker factory must report failures without calling Fatal or FailNow.
type LifecycleFactory func() LifecycleComponent

const lifecycleTestBudget = 5 * time.Second

// lifecycleTestOwner is the lexical owner of one returned test component. It
// stores only cancellation and terminal-attempt state; operation contexts stay
// with the invoking case or iteration.
type lifecycleTestOwner struct {
	component       LifecycleComponent
	cancelStart     context.CancelFunc
	attempted       bool
	concreteStopErr error
	stopBoundErr    error
}

func newLifecycleTestOwner(component LifecycleComponent) *lifecycleTestOwner {
	if component == nil {
		return nil
	}
	value := reflect.ValueOf(component)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if value.IsNil() {
			return nil
		}
	}
	return &lifecycleTestOwner{component: component}
}

func (o *lifecycleTestOwner) workContext(parent context.Context) context.Context {
	ctx, cancel := context.WithTimeout(parent, lifecycleTestBudget)
	o.cancelStart = cancel
	return ctx
}

func (o *lifecycleTestOwner) stop(workCtx context.Context, abort bool) error {
	stopCtx, cancelStop := context.WithTimeout(context.Background(), lifecycleTestBudget)
	defer cancelStop()
	return o.stopWithContext(stopCtx, workCtx, abort)
}

func (o *lifecycleTestOwner) stopWithContext(stopCtx, workCtx context.Context, abort bool) error {
	o.attempted = true // A returned error or panic does not authorize an implicit retry.
	o.concreteStopErr = o.component.Stop(stopCtx)
	o.stopBoundErr = stopCtx.Err()
	result := o.concreteStopErr
	if o.stopBoundErr != nil {
		result = errors.Join(result, fmt.Errorf("terminal context ended: %w", o.stopBoundErr))
	}
	if !abort && workCtx != nil && workCtx.Err() != nil {
		result = errors.Join(result, fmt.Errorf("accepted Start authority ended before controlled Stop: %w", workCtx.Err()))
	}
	return result
}

func (o *lifecycleTestOwner) abortContractError() error {
	if o.stopBoundErr != nil && !errors.Is(o.concreteStopErr, o.stopBoundErr) {
		return fmt.Errorf("component Stop returned %v without preserving caller-context cause %v", o.concreteStopErr, o.stopBoundErr)
	}
	return nil
}

func (o *lifecycleTestOwner) finish(workCtx context.Context, abort bool) error {
	if o.cancelStart != nil {
		defer o.cancelStart() // Stop completes before accepted Start authority ends.
	}
	if o.attempted {
		return nil
	}
	return o.stop(workCtx, abort)
}

// StandardLifecycleTests verifies the portable LifecycleComponent floor.
// Resource-specific drain ordering, blocked joins, and partial-acquisition
// rollback remain the responsibility of focused owner tests.
func StandardLifecycleTests(t *testing.T, factory LifecycleFactory) {
	t.Run("PortableFloor", func(t *testing.T) { testPortableLifecycleFloor(t, factory) })
	t.Run("ErrorPaths", func(t *testing.T) { testPortableErrorPaths(t, factory) })
	t.Run("ParallelFreshInstances", func(t *testing.T) { testParallelFreshInstances(t, factory) })
	t.Run("NoLeaks", func(t *testing.T) { testNoResourceLeaks(t, factory) })
}

func testPortableLifecycleFloor(t *testing.T, factory LifecycleFactory) {
	tests := []struct {
		name  string
		abort bool
		test  func(context.Context, *testing.T, *lifecycleTestOwner)
	}{
		{"Initialize", false, testInitialize},
		{"ControlledStopWithLiveStartAuthority", false, testControlledStopWithLiveStartAuthority},
		{"AcceptedStartParentCancellation", true, testAcceptedStartParentCancellation},
		{"CompletedRepeatedStop", false, testCompletedRepeatedStop},
		{"NilStartContext", false, testNilStartContext},
		{"NilStopContext", false, testNilStopContext},
		{"StopBeforeStart", false, testStopBeforeStart},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner := newLifecycleTestOwner(factory())
			if owner == nil {
				t.Fatal("component factory returned nil")
			}
			var workCtx context.Context
			defer func() {
				if err := owner.finish(workCtx, tt.abort); err != nil {
					t.Errorf("%s terminal cleanup: %v", tt.name, err)
				}
			}()
			workCtx = owner.workContext(t.Context())
			tt.test(workCtx, t, owner)
		})
	}
}

func testInitialize(_ context.Context, t *testing.T, owner *lifecycleTestOwner) {
	require.NoError(t, owner.component.Initialize(), "Initialize should succeed on a fresh component")
}

func testControlledStopWithLiveStartAuthority(workCtx context.Context, t *testing.T, owner *lifecycleTestOwner) {
	require.NoError(t, owner.component.Initialize())
	require.NoError(t, owner.component.Start(workCtx))
	require.NoError(t, owner.stop(workCtx, false))
	require.NoError(t, workCtx.Err(), "accepted Start authority must remain live during controlled Stop")
}

func testAcceptedStartParentCancellation(workCtx context.Context, t *testing.T, owner *lifecycleTestOwner) {
	require.NoError(t, owner.component.Initialize())
	require.NoError(t, owner.component.Start(workCtx))
	owner.cancelStart()
	var stopErr error
	require.NotPanics(t, func() { stopErr = owner.stop(workCtx, true) },
		"abort Stop must remain a synchronous bounded lifecycle call")
	require.NoError(t, owner.abortContractError(), "abort Stop must preserve its exact caller-context cause")
	if stopErr != nil {
		t.Logf("abort Stop accurately reported terminal cleanup: %v", stopErr)
	}
}

func testCompletedRepeatedStop(workCtx context.Context, t *testing.T, owner *lifecycleTestOwner) {
	require.NoError(t, owner.component.Initialize())
	require.NoError(t, owner.component.Start(workCtx))
	require.NoError(t, owner.stop(workCtx, false))
	secondCtx, cancelSecond := context.WithTimeout(context.Background(), lifecycleTestBudget)
	defer cancelSecond()
	require.NoError(t, owner.component.Stop(secondCtx), "completed repeated Stop should be a no-op")
	require.NoError(t, secondCtx.Err(), "completed repeated Stop exceeded its caller bound")
}

func testNilStartContext(_ context.Context, t *testing.T, owner *lifecycleTestOwner) {
	require.NoError(t, owner.component.Initialize())
	assert.Error(t, owner.component.Start(nil), "Start must reject a nil context")
}

func testNilStopContext(_ context.Context, t *testing.T, owner *lifecycleTestOwner) {
	assert.Error(t, owner.component.Stop(nil), "Stop must reject a nil context")
}

func testStopBeforeStart(workCtx context.Context, t *testing.T, owner *lifecycleTestOwner) {
	require.NoError(t, owner.stop(workCtx, false), "Stop should be safe before Start")
}

func testPortableErrorPaths(t *testing.T, factory LifecycleFactory) {
	tests := []struct {
		name    string
		wantErr error
	}{
		{"PreCanceledStart", context.Canceled},
		{"PreExpiredStart", context.DeadlineExceeded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner := newLifecycleTestOwner(factory())
			if owner == nil {
				t.Fatal("component factory returned nil")
			}
			var workCtx context.Context
			defer func() {
				if err := owner.finish(workCtx, false); err != nil {
					t.Errorf("%s terminal cleanup: %v", tt.name, err)
				}
			}()
			workCtx = owner.workContext(t.Context())
			require.NoError(t, owner.component.Initialize())
			var startCtx context.Context
			var cancelInput context.CancelFunc
			switch tt.name {
			case "PreCanceledStart":
				startCtx, cancelInput = context.WithCancel(context.Background())
				cancelInput()
			case "PreExpiredStart":
				startCtx, cancelInput = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			default:
				t.Fatalf("unsupported rejected-Start case %q", tt.name)
			}
			defer cancelInput()
			require.ErrorIs(t, owner.component.Start(startCtx), tt.wantErr)
			require.NoError(t, owner.stop(workCtx, false), "pre-action Start rejection must leave Stop safe")
		})
	}
}
func runLifecycleCycle(parent context.Context, factory LifecycleFactory, label string) (resultErr error) {
	owner := newLifecycleTestOwner(factory())
	if owner == nil {
		return fmt.Errorf("%s: component factory returned nil", label)
	}
	var workCtx context.Context
	defer func() {
		if err := owner.finish(workCtx, false); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("%s cleanup: %w", label, err))
		}
	}()
	if err := owner.component.Initialize(); err != nil {
		return fmt.Errorf("%s Initialize: %w", label, err)
	}
	workCtx = owner.workContext(parent)
	if err := owner.component.Start(workCtx); err != nil {
		return fmt.Errorf("%s Start: %w", label, err)
	}
	if err := owner.stop(workCtx, false); err != nil {
		return fmt.Errorf("%s Stop: %w", label, err)
	}
	return nil
}

func testParallelFreshInstances(t *testing.T, factory LifecycleFactory) {
	if testing.Short() {
		t.Skip("Skipping parallel fresh-instance test in short mode")
	}
	runParallelFreshInstances(t.Context(), factory, func(err error) { t.Error(err) })
}

func runParallelFreshInstances(parent context.Context, factory LifecycleFactory, report func(error)) {
	const iterations = 20
	const concurrency = 10
	var failed atomic.Bool
	results := make(chan error, concurrency)
	var workers sync.WaitGroup
	for worker := 0; worker < concurrency; worker++ {
		workers.Add(1)
		go func(worker int) {
			defer workers.Done()
			for iteration := 0; iteration < iterations; iteration++ {
				// This check is admission; an instance acquired before another worker
				// reports failure remains owned until its finalizer returns.
				if failed.Load() {
					return
				}
				err := runLifecycleCycle(parent, factory, fmt.Sprintf("worker %d iteration %d", worker, iteration))
				if err != nil {
					failed.Store(true)
				}
				results <- err
				if err != nil {
					return
				}
			}
		}(worker)
	}
	go func() {
		workers.Wait()
		close(results)
	}()
	for err := range results {
		if err != nil {
			report(err) // Caller reports while workers finish already-owned instances.
		}
	}
}

func testNoResourceLeaks(t *testing.T, factory LifecycleFactory) {
	if testing.Short() {
		t.Skip("Skipping resource leak test in short mode")
	}
	runtime.GC()
	initialGoroutines := runtime.NumGoroutine()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	const iterations = 50
	for iteration := 0; iteration < iterations; iteration++ {
		if err := runLifecycleCycle(t.Context(), factory, fmt.Sprintf("NoLeaks iteration %d", iteration)); err != nil {
			t.Error(err)
			return // The current lexical owner has already finalized.
		}
		if iteration%10 == 9 {
			runtime.GC()
		}
	}
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	finalGoroutines := runtime.NumGoroutine()
	growth := int64(after.Alloc) - int64(before.Alloc)
	if growth > 50*1024*1024 {
		t.Errorf("Memory grew by %d bytes (%.2f MB), expected < 50MB", growth, float64(growth)/(1024*1024))
	}
	goroutineGrowth := finalGoroutines - initialGoroutines
	if goroutineGrowth > 10 {
		t.Errorf("Goroutine count grew by %d (initial: %d, final: %d), expected growth < 10",
			goroutineGrowth, initialGoroutines, finalGoroutines)
	}
	t.Logf("Resource leak test completed: %d iterations, memory growth: %d bytes, goroutine growth: %d",
		iterations, growth, goroutineGrowth)
}

// BenchmarkLifecycleMethods measures individual operations and the complete
// lifecycle while each iteration retains its own checked terminal owner.
func BenchmarkLifecycleMethods(b *testing.B, factory LifecycleFactory) {
	for _, mode := range []string{"Initialize", "Start", "Stop", "FullLifecycle"} {
		b.Run(mode, func(b *testing.B) {
			b.StopTimer()
			for iteration := 0; iteration < b.N; iteration++ {
				if err := benchmarkLifecycleIteration(b, factory, mode); err != nil {
					b.Fatalf("%s iteration %d: %v", mode, iteration, err)
				}
			}
		})
	}
}

func benchmarkLifecycleIteration(b *testing.B, factory LifecycleFactory, mode string) (resultErr error) {
	if mode == "FullLifecycle" {
		b.StartTimer()
	}
	owner := newLifecycleTestOwner(factory())
	if owner == nil {
		b.StopTimer()
		return errors.New("component factory returned nil")
	}
	var workCtx context.Context
	defer func() {
		b.StopTimer()
		if err := owner.finish(workCtx, false); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("terminal cleanup: %w", err))
		}
	}()
	if mode == "Initialize" {
		b.StartTimer()
	}
	if err := owner.component.Initialize(); err != nil {
		return fmt.Errorf("Initialize: %w", err)
	}
	if mode == "Initialize" {
		b.StopTimer()
	}
	if mode == "Initialize" {
		return nil
	}
	workCtx = owner.workContext(b.Context())
	if mode == "Start" {
		b.StartTimer()
	}
	if err := owner.component.Start(workCtx); err != nil {
		return fmt.Errorf("Start: %w", err)
	}
	if mode == "Start" {
		b.StopTimer()
		return nil
	}
	if mode == "Stop" {
		b.StartTimer()
	}
	if err := owner.stop(workCtx, false); err != nil {
		return fmt.Errorf("Stop: %w", err)
	}
	return nil
}

// ErrorInjectingComponent wraps a component to inject errors for testing
type ErrorInjectingComponent struct {
	LifecycleComponent
	injectInitError  bool
	injectStartError bool
	injectStopError  bool
	initError        error
	startError       error
	stopError        error
}

// NewErrorInjectingComponent creates a component wrapper that can inject errors for testing
func NewErrorInjectingComponent(comp LifecycleComponent) *ErrorInjectingComponent {
	return &ErrorInjectingComponent{LifecycleComponent: comp}
}

// InjectInitializeError configures the component to return an error on Initialize
func (e *ErrorInjectingComponent) InjectInitializeError(err error) {
	e.injectInitError = true
	e.initError = err
}

// InjectStartError configures the component to return an error on Start
func (e *ErrorInjectingComponent) InjectStartError(err error) {
	e.injectStartError = true
	e.startError = err
}

// InjectStopError configures the component to return an error on Stop
func (e *ErrorInjectingComponent) InjectStopError(err error) {
	e.injectStopError = true
	e.stopError = err
}

// Initialize initializes the component, returning injected error if configured
func (e *ErrorInjectingComponent) Initialize() error {
	if e.injectInitError {
		return e.initError
	}
	return e.LifecycleComponent.Initialize()
}

// Start starts the component, returning injected error if configured
func (e *ErrorInjectingComponent) Start(ctx context.Context) error {
	if e.injectStartError {
		return e.startError
	}
	return e.LifecycleComponent.Start(ctx)
}

// Stop stops the component, returning injected error if configured
func (e *ErrorInjectingComponent) Stop(ctx context.Context) error {
	if e.injectStopError {
		return e.stopError
	}
	return e.LifecycleComponent.Stop(ctx)
}

// TestErrorInjection tests components with injected errors while the underlying
// component remains independently owned for terminal cleanup.
func TestErrorInjection(t *testing.T, factory LifecycleFactory) {
	tests := []struct {
		name      string
		operation string
		inject    func(*ErrorInjectingComponent, error)
	}{
		{"inject_initialize_error", "initialize", (*ErrorInjectingComponent).InjectInitializeError},
		{"inject_start_error", "start", (*ErrorInjectingComponent).InjectStartError},
		{"inject_stop_error", "stop", (*ErrorInjectingComponent).InjectStopError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner := newLifecycleTestOwner(factory())
			if owner == nil {
				t.Fatal("component factory returned nil")
			}
			var workCtx context.Context
			defer func() {
				if err := owner.finish(workCtx, false); err != nil {
					t.Errorf("%s base terminal cleanup: %v", tt.name, err)
				}
			}()
			wrapped := NewErrorInjectingComponent(owner.component)
			injected := errors.New("injected " + tt.operation + " error")
			tt.inject(wrapped, injected)
			var operationErr error
			switch tt.operation {
			case "initialize":
				operationErr = wrapped.Initialize()
			case "start":
				require.NoError(t, owner.component.Initialize(), "base Initialize prerequisite")
				workCtx = owner.workContext(t.Context())
				operationErr = wrapped.Start(workCtx)
			case "stop":
				require.NoError(t, owner.component.Initialize(), "base Initialize prerequisite")
				workCtx = owner.workContext(t.Context())
				require.NoError(t, owner.component.Start(workCtx), "base Start prerequisite")
				operationCtx, cancelOperation := context.WithTimeout(context.Background(), lifecycleTestBudget)
				operationErr = wrapped.Stop(operationCtx)
				if err := operationCtx.Err(); err != nil {
					t.Errorf("injected Stop operation exceeded caller bound: %v", err)
				}
				cancelOperation()
			}
			require.ErrorIs(t, operationErr, injected, "expected %s wrapper operation error", tt.operation)
		})
	}
}
