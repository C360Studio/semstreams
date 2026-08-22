package component

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// LifecycleFactory creates a new instance of a LifecycleComponent for testing
type LifecycleFactory func() LifecycleComponent

// StandardLifecycleTests verifies the portable LifecycleComponent floor.
// Resource-specific drain ordering, blocked joins, and partial-acquisition
// rollback remain the responsibility of focused owner tests.
func StandardLifecycleTests(t *testing.T, factory LifecycleFactory) {
	t.Run("PortableFloor", func(t *testing.T) {
		testPortableLifecycleFloor(t, factory)
	})
	t.Run("ErrorPaths", func(t *testing.T) {
		testPortableErrorPaths(t, factory)
	})
	t.Run("ParallelFreshInstances", func(t *testing.T) {
		testParallelFreshInstances(t, factory)
	})
	t.Run("NoLeaks", func(t *testing.T) {
		testNoResourceLeaks(t, factory)
	})
}

func testPortableLifecycleFloor(t *testing.T, factory LifecycleFactory) {
	tests := []struct {
		name string
		test func(t *testing.T, comp LifecycleComponent)
	}{
		{"Initialize", testInitialize},
		{"ControlledStopWithLiveStartAuthority", testControlledStopWithLiveStartAuthority},
		{"AcceptedStartParentCancellation", testAcceptedStartParentCancellation},
		{"CompletedRepeatedStop", testCompletedRepeatedStop},
		{"NilStartContext", testNilStartContext},
		{"NilStopContext", testNilStopContext},
		{"StopBeforeStart", testStopBeforeStart},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			comp := factory()
			require.NotNil(t, comp, "Component factory returned nil")
			tt.test(t, comp)
		})
	}
}

func testInitialize(t *testing.T, comp LifecycleComponent) {
	require.NoError(t, comp.Initialize(), "Initialize should succeed on a fresh component")
}

func testControlledStopWithLiveStartAuthority(t *testing.T, comp LifecycleComponent) {
	require.NoError(t, comp.Initialize())
	startCtx, cancelStart := context.WithCancel(context.Background())
	defer cancelStart()
	require.NoError(t, comp.Start(startCtx))

	stopCtx, cancelStop := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelStop()
	require.NoError(t, comp.Stop(stopCtx))
	require.NoError(t, startCtx.Err(), "the parent Start authority must remain live during controlled Stop")
}

func testAcceptedStartParentCancellation(t *testing.T, comp LifecycleComponent) {
	require.NoError(t, comp.Initialize())
	startCtx, cancelStart := context.WithCancel(context.Background())
	require.NoError(t, comp.Start(startCtx))
	cancelStart()

	stopCtx, cancelStop := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelStop()
	require.NoError(t, comp.Stop(stopCtx), "Stop should observe work ending with the accepted Start parent")
}

func testCompletedRepeatedStop(t *testing.T, comp LifecycleComponent) {
	require.NoError(t, comp.Initialize())
	startCtx, cancelStart := context.WithCancel(context.Background())
	defer cancelStart()
	require.NoError(t, comp.Start(startCtx))

	firstCtx, cancelFirst := context.WithTimeout(context.Background(), 5*time.Second)
	require.NoError(t, comp.Stop(firstCtx))
	cancelFirst()
	secondCtx, cancelSecond := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelSecond()
	require.NoError(t, comp.Stop(secondCtx), "completed repeated Stop should be a no-op")
}

func testNilStartContext(t *testing.T, comp LifecycleComponent) {
	require.NoError(t, comp.Initialize())
	assert.Error(t, comp.Start(nil), "Start must reject a nil context")
}

func testNilStopContext(t *testing.T, comp LifecycleComponent) {
	assert.Error(t, comp.Stop(nil), "Stop must reject a nil context")
}

func testStopBeforeStart(t *testing.T, comp LifecycleComponent) {
	stopCtx, cancelStop := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelStop()
	require.NoError(t, comp.Stop(stopCtx), "Stop should be safe before Start")
}

func testPortableErrorPaths(t *testing.T, factory LifecycleFactory) {
	t.Run("PreCanceledStart", func(t *testing.T) {
		comp := factory()
		require.NotNil(t, comp, "Component factory returned nil")
		require.NoError(t, comp.Initialize())
		startCtx, cancelStart := context.WithCancel(context.Background())
		cancelStart()
		require.ErrorIs(t, comp.Start(startCtx), context.Canceled)
		requireSafeStopAfterRejectedStart(t, comp)
	})

	t.Run("PreExpiredStart", func(t *testing.T) {
		comp := factory()
		require.NotNil(t, comp, "Component factory returned nil")
		require.NoError(t, comp.Initialize())
		startCtx, cancelStart := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		defer cancelStart()
		require.ErrorIs(t, comp.Start(startCtx), context.DeadlineExceeded)
		requireSafeStopAfterRejectedStart(t, comp)
	})
}

func requireSafeStopAfterRejectedStart(t *testing.T, comp LifecycleComponent) {
	t.Helper()
	stopCtx, cancelStop := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelStop()
	require.NoError(t, comp.Stop(stopCtx), "pre-action Start rejection must leave Stop safe")
}

func testParallelFreshInstances(t *testing.T, factory LifecycleFactory) {
	if testing.Short() {
		t.Skip("Skipping parallel fresh-instance test in short mode")
	}

	const iterations = 20
	const concurrency = 10

	var wg sync.WaitGroup
	results := make(chan error, iterations*concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				comp := factory()
				if comp == nil {
					results <- fmt.Errorf("component factory returned nil")
					continue
				}
				if err := comp.Initialize(); err != nil {
					results <- err
					continue
				}
				startCtx, cancelStart := context.WithCancel(context.Background())
				if err := comp.Start(startCtx); err != nil {
					cancelStart()
					results <- err
					continue
				}
				stopCtx, cancelStop := context.WithTimeout(context.Background(), 5*time.Second)
				err := comp.Stop(stopCtx)
				cancelStop()
				cancelStart()
				results <- err
			}
		}()
	}

	wg.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
}

// testNoResourceLeaks tests for memory and goroutine leaks
func testNoResourceLeaks(t *testing.T, factory LifecycleFactory) {
	if testing.Short() {
		t.Skip("Skipping resource leak test in short mode")
	}

	// Get baseline goroutine count
	runtime.GC()
	initialGoroutines := runtime.NumGoroutine()

	// Baseline memory
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)

	// Run lifecycle iterations - 50 is enough to detect leaks without being excessive
	const iterations = 50
	for i := 0; i < iterations; i++ {
		comp := factory()
		require.NotNil(t, comp, "Component factory returned nil")

		err := comp.Initialize()
		if err != nil {
			t.Logf("Initialize failed on iteration %d: %v", i, err)
			continue
		}

		startCtx, cancelStart := context.WithCancel(context.Background())
		err = comp.Start(startCtx)
		if err != nil {
			t.Logf("Start failed on iteration %d: %v", i, err)
		}

		stopCtx, cancelStop := context.WithTimeout(context.Background(), 5*time.Second)
		err = comp.Stop(stopCtx)
		cancelStop()
		if err != nil {
			t.Logf("Stop failed on iteration %d: %v", i, err)
		}

		cancelStart()

		// Periodic cleanup check
		if i%10 == 9 {
			runtime.GC()
		}
	}

	// Stop is a join boundary; no scheduler delay is needed before inspection.
	runtime.GC()

	// Check memory after
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)

	// Check goroutine count
	finalGoroutines := runtime.NumGoroutine()

	// Memory should not grow significantly (allow 50MB growth)
	growth := int64(m2.Alloc) - int64(m1.Alloc)
	if growth > 50*1024*1024 {
		t.Errorf("Memory grew by %d bytes (%.2f MB), expected < 50MB", growth, float64(growth)/(1024*1024))
	}

	// Goroutine count should be stable - allow some variance for NATS async cleanup
	// Each iteration should not leak goroutines, so 10 total growth is generous
	goroutineGrowth := finalGoroutines - initialGoroutines
	if goroutineGrowth > 10 {
		t.Errorf("Goroutine count grew by %d (initial: %d, final: %d), expected growth < 10",
			goroutineGrowth, initialGoroutines, finalGoroutines)
	}

	t.Logf("Resource leak test completed: %d iterations, memory growth: %d bytes, goroutine growth: %d",
		iterations, growth, goroutineGrowth)
}

// BenchmarkLifecycleMethods provides benchmark tests for lifecycle operations
func BenchmarkLifecycleMethods(b *testing.B, factory LifecycleFactory) {
	b.Run("Initialize", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			comp := factory()
			_ = comp.Initialize()
			stopCtx, cancelStop := context.WithTimeout(context.Background(), time.Second)
			_ = comp.Stop(stopCtx)
			cancelStop()
		}
	})

	b.Run("Start", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			comp := factory()
			_ = comp.Initialize()
			startCtx, cancelStart := context.WithCancel(context.Background())
			_ = comp.Start(startCtx)
			stopCtx, cancelStop := context.WithTimeout(context.Background(), time.Second)
			_ = comp.Stop(stopCtx)
			cancelStop()
			cancelStart()
		}
	})

	b.Run("Stop", func(b *testing.B) {
		b.StopTimer()
		for i := 0; i < b.N; i++ {
			comp := factory()
			_ = comp.Initialize()
			startCtx, cancelStart := context.WithCancel(context.Background())
			_ = comp.Start(startCtx)
			stopCtx, cancelStop := context.WithTimeout(context.Background(), time.Second)
			b.StartTimer()
			_ = comp.Stop(stopCtx)
			b.StopTimer()
			cancelStop()
			cancelStart()
		}
	})

	b.Run("FullLifecycle", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			comp := factory()
			_ = comp.Initialize()
			startCtx, cancelStart := context.WithCancel(context.Background())
			_ = comp.Start(startCtx)
			stopCtx, cancelStop := context.WithTimeout(context.Background(), time.Second)
			_ = comp.Stop(stopCtx)
			cancelStop()
			cancelStart()
		}
	})
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

// TestErrorInjection tests components with injected errors
func TestErrorInjection(t *testing.T, factory LifecycleFactory) {
	tests := []struct {
		name        string
		setupError  func(*ErrorInjectingComponent)
		operation   string
		expectError bool
	}{
		{
			name: "inject_initialize_error",
			setupError: func(comp *ErrorInjectingComponent) {
				comp.InjectInitializeError(fmt.Errorf("injected initialize error"))
			},
			operation:   "initialize",
			expectError: true,
		},
		{
			name: "inject_start_error",
			setupError: func(comp *ErrorInjectingComponent) {
				comp.InjectStartError(fmt.Errorf("injected start error"))
			},
			operation:   "start",
			expectError: true,
		},
		{
			name: "inject_stop_error",
			setupError: func(comp *ErrorInjectingComponent) {
				comp.InjectStopError(fmt.Errorf("injected stop error"))
			},
			operation:   "stop",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseComp := factory()
			comp := NewErrorInjectingComponent(baseComp)
			tt.setupError(comp)

			var err error
			switch tt.operation {
			case "initialize":
				err = comp.Initialize()
			case "start":
				comp.Initialize() // Ensure component is initialized
				ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
				defer cancel()
				err = comp.Start(ctx)
			case "stop":
				comp.Initialize() // Ensure component is initialized
				ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
				comp.Start(ctx)
				cancel()
				err = comp.Stop(context.Background())
			}

			if tt.expectError {
				assert.Error(t, err, "Expected error for %s operation", tt.operation)
			} else {
				assert.NoError(t, err, "Expected no error for %s operation", tt.operation)
			}

			// Always try to clean up
			comp.Stop(context.Background())
		})
	}
}
