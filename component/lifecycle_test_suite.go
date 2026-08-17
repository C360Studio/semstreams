package component

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// LifecycleFactory creates a new instance of a LifecycleComponent for testing
type LifecycleFactory func() LifecycleComponent

// StandardLifecycleTests runs comprehensive lifecycle tests for any component that implements LifecycleComponent
// This ensures consistent testing standards across all components in the semstreams system
func StandardLifecycleTests(t *testing.T, factory LifecycleFactory) {
	t.Run("Compliance", func(t *testing.T) {
		testLifecycleCompliance(t, factory)
	})
	t.Run("ErrorPaths", func(t *testing.T) {
		testErrorPaths(t, factory)
	})
	t.Run("Concurrent", func(t *testing.T) {
		testConcurrentLifecycle(t, factory)
	})
	t.Run("NoLeaks", func(t *testing.T) {
		testNoResourceLeaks(t, factory)
	})
}

// testLifecycleCompliance tests standard lifecycle state transitions
func testLifecycleCompliance(t *testing.T, factory LifecycleFactory) {
	tests := []struct {
		name string
		test func(t *testing.T, comp LifecycleComponent)
	}{
		{"Initialize", testInitialize},
		{"Start", testStart},
		{"Stop", testStop},
		{"DoubleStart", testDoubleStart},
		{"DoubleStop", testDoubleStop},
		{"NilStartContext", testNilStartContext},
		{"NilStopContext", testNilStopContext},
		{"StartWithoutInit", testStartWithoutInit},
		{"StopWithoutStart", testStopWithoutStart},
		{"InitializeAfterStop", testInitializeAfterStop},
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
	err := comp.Initialize()
	assert.NoError(t, err, "Initialize should succeed on fresh component")
}

func testStart(t *testing.T, comp LifecycleComponent) {
	err := comp.Initialize()
	require.NoError(t, err, "Initialize must succeed before Start")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = comp.Start(ctx)
	assert.NoError(t, err, "Start should succeed after Initialize")

	// Clean shutdown
	err = comp.Stop(context.Background())
	assert.NoError(t, err, "Stop should succeed after Start")
}

func testStop(t *testing.T, comp LifecycleComponent) {
	// Stop without start should not error
	err := comp.Stop(context.Background())
	assert.NoError(t, err, "Stop should succeed even without Start")
}

func testDoubleStart(t *testing.T, comp LifecycleComponent) {
	err := comp.Initialize()
	require.NoError(t, err, "Initialize must succeed")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = comp.Start(ctx)
	require.NoError(t, err, "First Start should succeed")

	// Second start should be handled gracefully (either no-op or error)
	err = comp.Start(ctx)
	// We don't require this to error - implementation can choose to be idempotent

	err = comp.Stop(context.Background())
	assert.NoError(t, err, "Stop should succeed")
}

func testDoubleStop(t *testing.T, comp LifecycleComponent) {
	err := comp.Initialize()
	require.NoError(t, err, "Initialize must succeed")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = comp.Start(ctx)
	require.NoError(t, err, "Start must succeed")

	err = comp.Stop(context.Background())
	assert.NoError(t, err, "First Stop should succeed")

	err = comp.Stop(context.Background())
	assert.NoError(t, err, "Second Stop should be idempotent")
}

func testNilStartContext(t *testing.T, comp LifecycleComponent) {
	require.NoError(t, comp.Initialize())
	assert.Error(t, comp.Start(nil), "Start must reject a nil context")
}

func testNilStopContext(t *testing.T, comp LifecycleComponent) {
	assert.Error(t, comp.Stop(nil), "Stop must reject a nil context")
}

func testStartWithoutInit(t *testing.T, comp LifecycleComponent) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := comp.Start(ctx)
	// This should either succeed (if Start does implicit Initialize) or fail gracefully
	if err != nil {
		assert.Contains(t, err.Error(), "not initialized", "Error should indicate component not initialized")
	}
}

func testStopWithoutStart(t *testing.T, comp LifecycleComponent) {
	err := comp.Stop(context.Background())
	assert.NoError(t, err, "Stop should be safe to call without Start")
}

func testInitializeAfterStop(t *testing.T, comp LifecycleComponent) {
	err := comp.Initialize()
	require.NoError(t, err, "First Initialize should succeed")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = comp.Start(ctx)
	require.NoError(t, err, "Start should succeed")

	err = comp.Stop(context.Background())
	require.NoError(t, err, "Stop should succeed")

	// Re-initialize after stop
	err = comp.Initialize()
	assert.NoError(t, err, "Initialize should succeed after Stop")
}

// testErrorPaths tests error scenarios and edge cases
func testErrorPaths(t *testing.T, factory LifecycleFactory) {
	tests := []struct {
		name      string
		setup     func(LifecycleComponent) error
		operation func(LifecycleComponent) error
		wantErr   bool
		errCheck  func(error) bool
	}{
		{
			name:  "cancelled_context_on_start",
			setup: func(comp LifecycleComponent) error { return comp.Initialize() },
			operation: func(comp LifecycleComponent) error {
				ctx, cancel := context.WithCancel(context.Background())
				cancel() // Cancel immediately
				return comp.Start(ctx)
			},
			wantErr: true,
			errCheck: func(err error) bool {
				return strings.Contains(err.Error(), "context") || strings.Contains(err.Error(), "cancel")
			},
		},
		{
			name:  "timeout_context_on_start",
			setup: func(comp LifecycleComponent) error { return comp.Initialize() },
			operation: func(comp LifecycleComponent) error {
				ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
				defer cancel()
				return comp.Start(ctx)
			},
			wantErr: true,
			errCheck: func(err error) bool {
				return strings.Contains(err.Error(), "context") || strings.Contains(err.Error(), "timeout")
			},
		},
		{
			name:      "start_without_initialize",
			setup:     func(_ LifecycleComponent) error { return nil },
			operation: func(comp LifecycleComponent) error { return comp.Start(context.Background()) },
			wantErr:   false, // Some components might handle this gracefully
			errCheck:  func(err error) bool { return err == nil || strings.Contains(err.Error(), "not initialized") },
		},
		{
			name:  "nil_context_on_start",
			setup: func(comp LifecycleComponent) error { return comp.Initialize() },
			operation: func(comp LifecycleComponent) error {
				// Pass nil context (should be handled gracefully or error)
				return comp.Start(nil)
			},
			wantErr:  true,
			errCheck: func(err error) bool { return err != nil },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			comp := factory()
			require.NotNil(t, comp, "Component factory returned nil")

			setupErr := tt.setup(comp)
			require.NoError(t, setupErr, "Test setup failed")

			err := tt.operation(comp)

			if tt.wantErr {
				assert.Error(t, err, "Operation should have failed")
				if err != nil && tt.errCheck != nil {
					assert.True(t, tt.errCheck(err), "Error should match expected pattern: %v", err)
				}
			} else {
				if err != nil && tt.errCheck != nil {
					assert.True(t, tt.errCheck(err), "Unexpected error pattern: %v", err)
				}
			}

			// Ensure component can still be stopped
			stopErr := comp.Stop(context.Background())
			assert.NoError(t, stopErr, "Component should be stoppable after error test")
		})
	}
}

// testConcurrentLifecycle tests concurrent operations on lifecycle methods
func testConcurrentLifecycle(t *testing.T, factory LifecycleFactory) {
	t.Run("ConcurrentInitialize", func(t *testing.T) {
		testConcurrentInitialize(t, factory)
	})
	t.Run("ConcurrentStopSharesGeneration", func(t *testing.T) {
		testConcurrentStopSharesGeneration(t, factory)
	})
	t.Run("CanceledStopRejoinsGeneration", func(t *testing.T) {
		testCanceledStopRejoinsGeneration(t, factory)
	})
	t.Run("ExpiredDeadlineStopRejoinsGeneration", func(t *testing.T) {
		testExpiredDeadlineStopRejoinsGeneration(t, factory)
	})
	t.Run("StressTest", func(t *testing.T) {
		testLifecycleStress(t, factory)
	})
}

func testCanceledStopRejoinsGeneration(t *testing.T, factory LifecycleFactory) {
	comp := factory()
	require.NotNil(t, comp, "Component factory returned nil")
	require.NoError(t, comp.Initialize())
	require.NoError(t, comp.Start(context.Background()))
	t.Cleanup(func() { _ = comp.Stop(context.Background()) })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, comp.Stop(ctx), context.Canceled,
		"an already-canceled Stop must signal shutdown and leave the generation available to rejoin")
	require.NoError(t, comp.Stop(context.Background()),
		"a later authorized Stop must rejoin and finish the same generation")
}

func testExpiredDeadlineStopRejoinsGeneration(t *testing.T, factory LifecycleFactory) {
	comp := factory()
	require.NotNil(t, comp, "Component factory returned nil")
	require.NoError(t, comp.Initialize())
	require.NoError(t, comp.Start(context.Background()))
	t.Cleanup(func() { _ = comp.Stop(context.Background()) })

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	require.ErrorIs(t, comp.Stop(ctx), context.DeadlineExceeded,
		"an expired-deadline Stop must signal shutdown and leave the generation available to rejoin")
	require.NoError(t, comp.Stop(context.Background()),
		"a later authorized Stop must rejoin and finish the same generation")
}

func testConcurrentStopSharesGeneration(t *testing.T, factory LifecycleFactory) {
	comp := factory()
	require.NotNil(t, comp, "Component factory returned nil")
	require.NoError(t, comp.Initialize())
	require.NoError(t, comp.Start(context.Background()))

	const callers = 8
	results := make(chan error, callers)
	var callersDone sync.WaitGroup
	for range callers {
		callersDone.Add(1)
		go func() {
			defer callersDone.Done()
			results <- comp.Stop(context.Background())
		}()
	}
	callersDone.Wait()
	close(results)
	var first error
	firstSet := false
	for err := range results {
		if !firstSet {
			first = err
			firstSet = true
			continue
		}
		require.Equal(t, fmt.Sprint(first), fmt.Sprint(err), "concurrent Stop callers must observe one generation result")
	}
	require.Equal(t, fmt.Sprint(first), fmt.Sprint(comp.Stop(context.Background())),
		"repeated Stop must replay the completed generation result")
}

func testConcurrentInitialize(t *testing.T, factory LifecycleFactory) {
	comp := factory()
	require.NotNil(t, comp, "Component factory returned nil")

	var wg sync.WaitGroup
	errors := make([]error, 20)

	// 20 goroutines trying to initialize concurrently
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errors[idx] = comp.Initialize()
		}(i)
	}

	wg.Wait()

	// At least one initialize should succeed
	successCount := 0
	for _, err := range errors {
		if err == nil {
			successCount++
		}
	}

	assert.GreaterOrEqual(t, successCount, 1, "At least one Initialize should succeed")

	// Component should be in a valid state
	err := comp.Stop(context.Background())
	assert.NoError(t, err, "Component should be stoppable after concurrent initialize")
}

func testLifecycleStress(t *testing.T, factory LifecycleFactory) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	const iterations = 50
	const concurrency = 10

	var wg sync.WaitGroup

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(_ int) {
			defer wg.Done()

			for j := 0; j < iterations; j++ {
				comp := factory()
				require.NotNil(t, comp, "Component factory returned nil")

				// Random lifecycle operations
				switch j % 4 {
				case 0:
					// Full lifecycle
					_ = comp.Initialize()
					ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
					_ = comp.Start(ctx)
					cancel()
					_ = comp.Stop(context.Background())
				case 1:
					// Initialize only
					_ = comp.Initialize()
					_ = comp.Stop(context.Background())
				case 2:
					// Start without init
					ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
					_ = comp.Start(ctx)
					cancel()
					_ = comp.Stop(context.Background())
				case 3:
					// Stop only
					_ = comp.Stop(context.Background())
				}
			}
		}(i)
	}

	wg.Wait()
	t.Logf(
		"Stress test completed: %d workers × %d iterations = %d total operations",
		concurrency,
		iterations,
		concurrency*iterations,
	)
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

		// Use 5 second timeout - NATS components need time for subscription setup
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err = comp.Start(ctx)
		if err != nil {
			t.Logf("Start failed on iteration %d: %v", i, err)
		}

		err = comp.Stop(context.Background())
		if err != nil {
			t.Logf("Stop failed on iteration %d: %v", i, err)
		}

		cancel()

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
			comp.Initialize()
			comp.Stop(context.Background())
		}
	})

	b.Run("Start", func(b *testing.B) {
		comp := factory()
		comp.Initialize()
		defer comp.Stop(context.Background())

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			comp.Start(ctx)
			cancel()
		}
	})

	b.Run("Stop", func(b *testing.B) {
		comp := factory()
		comp.Initialize()
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		comp.Start(ctx)
		cancel()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			comp.Stop(context.Background())
		}
	})

	b.Run("FullLifecycle", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			comp := factory()
			comp.Initialize()
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			comp.Start(ctx)
			cancel()
			comp.Stop(context.Background())
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
