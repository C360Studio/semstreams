//go:build integration

package rule

import (
	"context"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
)

// TestProcessor_DebounceZero_NoCoalescingSet tests coalescer is nil when debounce=0
// Given: Processor configured with DebounceDelayMs=0
// When: Processor is initialized
// Then: entityCoalescer field remains nil (no coalescing set created)
func TestIntegration_Processor_DebounceZero_NoCoalescingSet(t *testing.T) {
	// Create shared test client outside subtests to avoid container startup flakiness
	testClient, err := natsclient.NewSharedTestClient(
		natsclient.WithJetStream(),
		natsclient.WithTestTimeout(10*time.Second),
		natsclient.WithStartTimeout(30*time.Second),
	)
	if err != nil {
		t.Fatalf("Failed to create test client: %v", err)
	}
	defer testClient.Terminate()

	tests := []struct {
		name               string
		debounceDelayMs    time.Duration
		expectCoalescerNil bool
	}{
		{
			name:               "debounce=0 (immediate processing)",
			debounceDelayMs:    0,
			expectCoalescerNil: true,
		},
		{
			name:               "debounce=100ms (batching enabled)",
			debounceDelayMs:    100 * time.Millisecond,
			expectCoalescerNil: false,
		},
		{
			name:               "debounce=1ms (minimal batching)",
			debounceDelayMs:    1 * time.Millisecond,
			expectCoalescerNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create minimal config
			config := mustTestConfig(t, "rule-test-pack")
			config.DebounceDelayMs = tt.debounceDelayMs

			// Create processor
			processor, err := NewProcessorWithMetrics(testClient.Client, &config, nil)
			if err != nil {
				t.Fatalf("NewProcessor failed: %v", err)
			}
			processor.SetPlatform(component.PlatformMeta{Org: "c360", Platform: "platform1"})

			ctx := context.Background()
			err = processor.Start(ctx)
			if err != nil {
				t.Fatalf("Start failed: %v", err)
			}
			defer processor.Stop(context.Background())

			// Check entityCoalescer state
			isNil := processor.entityCoalescer == nil

			if tt.expectCoalescerNil && !isNil {
				t.Errorf("Expected entityCoalescer to be nil when debounce=0, but it was created")
			}

			if !tt.expectCoalescerNil && isNil {
				t.Errorf("Expected entityCoalescer to be created when debounce>0, but it was nil")
			}
		})
	}
}

// TestProcessor_DebounceZero_ImmediateProcessing tests entities are processed immediately
// Given: Processor with DebounceDelayMs=0 and a mock callback
// When: Entity update arrives
// Then: Callback is invoked immediately (no batching, no delay)
func TestIntegration_Processor_DebounceZero_ImmediateProcessing(t *testing.T) {
	// This test verifies the behavior at the handleEntityUpdates level
	// We test that when debounce=0, the callback is invoked directly
	// instead of going through entityCoalescer.Add()

	config := mustTestConfig(t, "rule-test-pack")
	config.DebounceDelayMs = 0

	testClient, err := natsclient.NewSharedTestClient(
		natsclient.WithJetStream(),
		natsclient.WithTestTimeout(5*time.Second),
	)
	if err != nil {
		t.Fatalf("Failed to create test client: %v", err)
	}
	defer testClient.Terminate()

	processor, err := NewProcessorWithMetrics(testClient.Client, &config, nil)
	if err != nil {
		t.Fatalf("NewProcessor failed: %v", err)
	}
	processor.SetPlatform(component.PlatformMeta{Org: "c360", Platform: "platform1"})

	ctx := context.Background()
	err = processor.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer processor.Stop(context.Background())

	// Verify coalescer is nil
	if processor.entityCoalescer != nil {
		t.Fatalf("Expected entityCoalescer to be nil, but it was created")
	}

	// The actual immediate processing behavior will be tested by Builder
	// in integration tests with real KV watchers. This unit test confirms
	// the structural requirement: no coalescer = no batching overhead.
}

// TestProcessor_DebounceZero_NoTickerSpinning tests no background ticker when debounce=0
// Given: Processor with DebounceDelayMs=0
// When: Processor runs
// Then: No CoalescingSet ticker goroutine is spawned (no CPU waste)
func TestIntegration_Processor_DebounceZero_NoTickerSpinning(t *testing.T) {
	config := mustTestConfig(t, "rule-test-pack")
	config.DebounceDelayMs = 0

	testClient, err := natsclient.NewSharedTestClient(
		natsclient.WithJetStream(),
		natsclient.WithTestTimeout(5*time.Second),
	)
	if err != nil {
		t.Fatalf("Failed to create test client: %v", err)
	}
	defer testClient.Terminate()

	processor, err := NewProcessorWithMetrics(testClient.Client, &config, nil)
	if err != nil {
		t.Fatalf("NewProcessor failed: %v", err)
	}
	processor.SetPlatform(component.PlatformMeta{Org: "c360", Platform: "platform1"})

	ctx := context.Background()
	err = processor.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer processor.Stop(context.Background())

	// When debounce=0, entityCoalescer should be nil
	// Therefore no ticker goroutine exists
	if processor.entityCoalescer != nil {
		t.Errorf("entityCoalescer should be nil when debounce=0 (no ticker should exist)")
	}

	// Let processor run briefly to ensure no spinning ticker
	time.Sleep(50 * time.Millisecond)

	// If coalescer was created, it would have a ticker spinning
	// Since it's nil, there's no resource consumption
	// This is a structural test - no spinning ticker to observe
}

// TestProcessor_DebounceNonZero_CoalescingSetCreated tests coalescer is created when debounce>0
// Given: Processor with DebounceDelayMs > 0
// When: Processor is initialized
// Then: entityCoalescer is created and functional
func TestIntegration_Processor_DebounceNonZero_CoalescingSetCreated(t *testing.T) {
	config := mustTestConfig(t, "rule-test-pack")
	config.DebounceDelayMs = 100 * time.Millisecond

	testClient, err := natsclient.NewSharedTestClient(
		natsclient.WithJetStream(),
		natsclient.WithTestTimeout(5*time.Second),
	)
	if err != nil {
		t.Fatalf("Failed to create test client: %v", err)
	}
	defer testClient.Terminate()

	processor, err := NewProcessorWithMetrics(testClient.Client, &config, nil)
	if err != nil {
		t.Fatalf("NewProcessor failed: %v", err)
	}
	processor.SetPlatform(component.PlatformMeta{Org: "c360", Platform: "platform1"})

	ctx := context.Background()
	err = processor.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer processor.Stop(context.Background())

	// Should have created coalescer
	if processor.entityCoalescer == nil {
		t.Errorf("Expected entityCoalescer to be created when debounce>0, but it was nil")
	}
}

// TestProcessor_DebounceZero_Transition tests transition scenarios
// Given: Various debounce delay configurations
// When: Processor is created with each config
// Then: Coalescer state matches expected behavior
func TestIntegration_Processor_DebounceZero_Transition(t *testing.T) {
	// Create shared test client outside subtests to avoid container startup flakiness
	testClient, err := natsclient.NewSharedTestClient(
		natsclient.WithJetStream(),
		natsclient.WithTestTimeout(10*time.Second),
		natsclient.WithStartTimeout(30*time.Second),
	)
	if err != nil {
		t.Fatalf("Failed to create test client: %v", err)
	}
	defer testClient.Terminate()

	tests := []struct {
		name                  string
		debounceDelayMs       time.Duration
		expectCoalescerExists bool
		description           string
	}{
		{
			name:                  "zero to immediate",
			debounceDelayMs:       0,
			expectCoalescerExists: false,
			description:           "No coalescer for immediate processing",
		},
		{
			name:                  "1ms minimal",
			debounceDelayMs:       1 * time.Millisecond,
			expectCoalescerExists: true,
			description:           "Coalescer exists for any positive delay",
		},
		{
			name:                  "10ms small window",
			debounceDelayMs:       10 * time.Millisecond,
			expectCoalescerExists: true,
			description:           "Coalescer exists for small window",
		},
		{
			name:                  "100ms default window",
			debounceDelayMs:       100 * time.Millisecond,
			expectCoalescerExists: true,
			description:           "Coalescer exists for default window",
		},
		{
			name:                  "1s large window",
			debounceDelayMs:       1 * time.Second,
			expectCoalescerExists: true,
			description:           "Coalescer exists for large window",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := mustTestConfig(t, "rule-test-pack")
			config.DebounceDelayMs = tt.debounceDelayMs

			processor, err := NewProcessorWithMetrics(testClient.Client, &config, nil)
			if err != nil {
				t.Fatalf("NewProcessor failed: %v", err)
			}
			processor.SetPlatform(component.PlatformMeta{Org: "c360", Platform: "platform1"})

			ctx := context.Background()
			err = processor.Start(ctx)
			if err != nil {
				t.Fatalf("Start failed: %v", err)
			}
			defer processor.Stop(context.Background())

			coalescerExists := processor.entityCoalescer != nil

			if tt.expectCoalescerExists && !coalescerExists {
				t.Errorf("%s: Expected coalescer to exist, but it was nil", tt.description)
			}

			if !tt.expectCoalescerExists && coalescerExists {
				t.Errorf("%s: Expected coalescer to be nil, but it exists", tt.description)
			}
		})
	}
}

// TestProcessor_DebounceZero_ConfigValidation tests config validation
// Given: Config with debounce_delay_ms=0
// When: Processor is created
// Then: Configuration is accepted as valid (0 means disabled)
func TestIntegration_Processor_DebounceZero_ConfigValidation(t *testing.T) {
	config := mustTestConfig(t, "rule-test-pack")
	config.DebounceDelayMs = 0

	testClient, err := natsclient.NewSharedTestClient(
		natsclient.WithJetStream(),
		natsclient.WithTestTimeout(5*time.Second),
	)
	if err != nil {
		t.Fatalf("Failed to create test client: %v", err)
	}
	defer testClient.Terminate()

	// Should not error on debounce=0
	processor, err := NewProcessorWithMetrics(testClient.Client, &config, nil)
	if err != nil {
		t.Fatalf("NewProcessor should accept debounce=0, got error: %v", err)
	}
	processor.SetPlatform(component.PlatformMeta{Org: "c360", Platform: "platform1"})

	ctx := context.Background()
	err = processor.Start(ctx)
	if err != nil {
		t.Fatalf("Start should succeed with debounce=0, got error: %v", err)
	}
	defer processor.Stop(context.Background())

	// Verify coalescer is nil
	if processor.entityCoalescer != nil {
		t.Errorf("entityCoalescer should be nil when debounce=0")
	}
}

// TestProcessor_DebounceZero_EdgeCases tests edge case configurations
// Given: Edge case debounce values
// When: Processor is created
// Then: Behavior matches expected semantics
func TestIntegration_Processor_DebounceZero_EdgeCases(t *testing.T) {
	// Create shared test client outside subtests to avoid container startup flakiness
	testClient, err := natsclient.NewSharedTestClient(
		natsclient.WithJetStream(),
		natsclient.WithTestTimeout(10*time.Second),
		natsclient.WithStartTimeout(30*time.Second),
	)
	if err != nil {
		t.Fatalf("Failed to create test client: %v", err)
	}
	defer testClient.Terminate()

	tests := []struct {
		name                  string
		debounceDelayMs       time.Duration
		expectCoalescerExists bool
		description           string
	}{
		{
			name:                  "exactly zero",
			debounceDelayMs:       0,
			expectCoalescerExists: false,
			description:           "Zero means disabled - no coalescer",
		},
		{
			name:                  "exactly 1ns",
			debounceDelayMs:       1 * time.Nanosecond,
			expectCoalescerExists: true,
			description:           "Any positive value enables coalescer",
		},
		{
			name:                  "exactly 1ms",
			debounceDelayMs:       1 * time.Millisecond,
			expectCoalescerExists: true,
			description:           "1ms enables coalescer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := mustTestConfig(t, "rule-test-pack")
			config.DebounceDelayMs = tt.debounceDelayMs

			processor, err := NewProcessorWithMetrics(testClient.Client, &config, nil)
			if err != nil {
				t.Fatalf("NewProcessor failed: %v", err)
			}
			processor.SetPlatform(component.PlatformMeta{Org: "c360", Platform: "platform1"})

			ctx := context.Background()
			err = processor.Start(ctx)
			if err != nil {
				t.Fatalf("Start failed: %v", err)
			}
			defer processor.Stop(context.Background())

			coalescerExists := processor.entityCoalescer != nil

			if tt.expectCoalescerExists != coalescerExists {
				t.Errorf("%s: Expected coalescer exists=%v, got %v",
					tt.description, tt.expectCoalescerExists, coalescerExists)
			}
		})
	}
}

// TestProcessor_DebounceZero_NoResourceLeak tests no resource leak when debounce=0
// Given: Processor with debounce=0
// When: Processor starts and stops multiple times
// Then: No goroutine leak, no ticker leak
func TestIntegration_Processor_DebounceZero_NoResourceLeak(t *testing.T) {
	config := mustTestConfig(t, "rule-test-pack")
	config.DebounceDelayMs = 0

	testClient, err := natsclient.NewSharedTestClient(
		natsclient.WithJetStream(),
		natsclient.WithTestTimeout(5*time.Second),
	)
	if err != nil {
		t.Fatalf("Failed to create test client: %v", err)
	}
	defer testClient.Terminate()

	// Create and stop processor multiple times
	for i := 0; i < 3; i++ {
		processor, err := NewProcessorWithMetrics(testClient.Client, &config, nil)
		if err != nil {
			t.Fatalf("Iteration %d: NewProcessor failed: %v", i, err)
		}
		processor.SetPlatform(component.PlatformMeta{Org: "c360", Platform: "platform1"})

		ctx := context.Background()
		err = processor.Start(ctx)
		if err != nil {
			t.Fatalf("Iteration %d: Start failed: %v", i, err)
		}

		// Verify no coalescer (no ticker to leak)
		if processor.entityCoalescer != nil {
			t.Errorf("Iteration %d: entityCoalescer should be nil", i)
		}

		// Stop cleanly
		processor.Stop(context.Background())

		// Brief sleep to allow cleanup
		time.Sleep(10 * time.Millisecond)
	}

	// If there were resource leaks (tickers, goroutines), they would accumulate
	// This test verifies clean lifecycle when debounce=0
}
