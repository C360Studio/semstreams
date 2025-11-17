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
		{"StartWithoutInit", testStartWithoutInit},
		{"StopWithoutStart", testStopWithoutStart},
		{"InitializeAfterStop", testInitializeAfterStop},
		{"RestartAfterStop", testRestartAfterStop},
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
	err = comp.Stop(5 * time.Second)
	assert.NoError(t, err, "Stop should succeed after Start")
}

func testStop(t *testing.T, comp LifecycleComponent) {
	// Stop without start should not error
	err := comp.Stop(5 * time.Second)
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

	err = comp.Stop(5 * time.Second)
	assert.NoError(t, err, "Stop should succeed")
}

func testDoubleStop(t *testing.T, comp LifecycleComponent) {
	err := comp.Initialize()
	require.NoError(t, err, "Initialize must succeed")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = comp.Start(ctx)
	require.NoError(t, err, "Start must succeed")

	err = comp.Stop(5 * time.Second)
	assert.NoError(t, err, "First Stop should succeed")

	err = comp.Stop(5 * time.Second)
	assert.NoError(t, err, "Second Stop should be idempotent")
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
	err := comp.Stop(5 * time.Second)
	assert.NoError(t, err, "Stop should be safe to call without Start")
}

func testInitializeAfterStop(t *testing.T, comp LifecycleComponent) {
	err := comp.Initialize()
	require.NoError(t, err, "First Initialize should succeed")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = comp.Start(ctx)
	require.NoError(t, err, "Start should succeed")

	err = comp.Stop(5 * time.Second)
	require.NoError(t, err, "Stop should succeed")

	// Re-initialize after stop
	err = comp.Initialize()
	assert.NoError(t, err, "Initialize should succeed after Stop")
}

func testRestartAfterStop(t *testing.T, comp LifecycleComponent) {
	err := comp.Initialize()
	require.NoError(t, err, "Initialize should succeed")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = comp.Start(ctx)
	require.NoError(t, err, "First Start should succeed")

	err = comp.Stop(5 * time.Second)
	require.NoError(t, err, "Stop should succeed")

	// Restart
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()

	err = comp.Start(ctx2)
	if err != nil {
		// Some components might require re-initialization after stop
		err = comp.Initialize()
		require.NoError(t, err, "Re-initialize should succeed if Start fails after Stop")

		err = comp.Start(ctx2)
		assert.NoError(t, err, "Start should succeed after re-initialization")
	}

	err = comp.Stop(5 * time.Second)
	assert.NoError(t, err, "Final Stop should succeed")
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
				ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
				defer cancel()
				time.Sleep(10 * time.Millisecond) // Ensure timeout
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
			wantErr:  false,                              // Implementation dependent
			errCheck: func(_ error) bool { return true }, // Any response is acceptable
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
			stopErr := comp.Stop(5 * time.Second)
			assert.NoError(t, stopErr, "Component should be stoppable after error test")
		})
	}
}

// testConcurrentLifecycle tests concurrent operations on lifecycle methods
func testConcurrentLifecycle(t *testing.T, factory LifecycleFactory) {
	t.Run("ConcurrentStartStop", func(t *testing.T) {
		testConcurrentStartStop(t, factory)
	})
	t.Run("ConcurrentInitialize", func(t *testing.T) {
		testConcurrentInitialize(t, factory)
	})
	t.Run("StressTest", func(t *testing.T) {
		testLifecycleStress(t, factory)
	})
}

func testConcurrentStartStop(t *testing.T, factory LifecycleFactory) {
	comp := factory()
	require.NotNil(t, comp, "Component factory returned nil")

	err := comp.Initialize()
	require.NoError(t, err, "Initialize must succeed")

	var wg sync.WaitGroup
	errors := make([]error, 100)

	// 50 goroutines trying to start
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			errors[idx] = comp.Start(ctx)
		}(i)
	}

	// 50 goroutines trying to stop
	for i := 50; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			time.Sleep(10 * time.Millisecond) // Give starts a chance
			errors[idx] = comp.Stop(5 * time.Second)
		}(i)
	}

	wg.Wait()

	// Verify no panics occurred and at least one operation succeeded
	successfulStarts := 0
	successfulStops := 0

	for i, err := range errors {
		if i < 50 && err == nil {
			successfulStarts++
		} else if i >= 50 && err == nil {
			successfulStops++
		}
	}

	assert.GreaterOrEqual(t, successfulStarts, 1, "At least one Start should succeed")
	assert.GreaterOrEqual(t, successfulStops, 1, "At least one Stop should succeed")

	// Final cleanup
	_ = comp.Stop(5 * time.Second)
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
	err := comp.Stop(5 * time.Second)
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
					_ = comp.Stop(5 * time.Second)
				case 1:
					// Initialize only
					_ = comp.Initialize()
					_ = comp.Stop(5 * time.Second)
				case 2:
					// Start without init
					ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
					_ = comp.Start(ctx)
					cancel()
					_ = comp.Stop(5 * time.Second)
				case 3:
					// Stop only
					_ = comp.Stop(5 * time.Second)
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
	time.Sleep(100 * time.Millisecond) // Allow GC to settle
	initialGoroutines := runtime.NumGoroutine()

	// Baseline memory
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)

	// Run many lifecycle iterations
	const iterations = 1000
	for i := 0; i < iterations; i++ {
		comp := factory()
		require.NotNil(t, comp, "Component factory returned nil")

		err := comp.Initialize()
		if err != nil {
			t.Logf("Initialize failed on iteration %d: %v", i, err)
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		err = comp.Start(ctx)
		if err != nil {
			t.Logf("Start failed on iteration %d: %v", i, err)
		}

		err = comp.Stop(5 * time.Second)
		if err != nil {
			t.Logf("Stop failed on iteration %d: %v", i, err)
		}

		cancel()

		// Periodic cleanup check
		if i%100 == 99 {
			runtime.GC()
			time.Sleep(10 * time.Millisecond)
		}
	}

	// Final cleanup
	runtime.GC()
	time.Sleep(200 * time.Millisecond) // Allow goroutines to cleanup

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

	// Goroutine count should be stable (allow 5 goroutine variance)
	goroutineGrowth := finalGoroutines - initialGoroutines
	if goroutineGrowth > 5 {
		t.Errorf("Goroutine count grew by %d (initial: %d, final: %d), expected growth < 5",
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
			comp.Stop(5 * time.Second)
		}
	})

	b.Run("Start", func(b *testing.B) {
		comp := factory()
		comp.Initialize()
		defer comp.Stop(5 * time.Second)

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
			comp.Stop(5 * time.Second)
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
			comp.Stop(5 * time.Second)
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
func (e *ErrorInjectingComponent) Stop(timeout time.Duration) error {
	if e.injectStopError {
		return e.stopError
	}
	return e.LifecycleComponent.Stop(timeout)
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
				err = comp.Stop(5 * time.Second)
			}

			if tt.expectError {
				assert.Error(t, err, "Expected error for %s operation", tt.operation)
			} else {
				assert.NoError(t, err, "Expected no error for %s operation", tt.operation)
			}

			// Always try to clean up
			comp.Stop(5 * time.Second)
		})
	}
}
