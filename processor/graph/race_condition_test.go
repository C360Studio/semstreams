package graph

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestAliasCacheRaceCondition tests for race conditions in processor initialization and IsReady calls
func TestAliasCacheRaceCondition(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	processor, err := NewProcessor(ProcessorDeps{
		Config:          DefaultConfig(),
		NATSClient:      natsClient,
		MetricsRegistry: nil,
		Logger:          slog.Default(),
	})
	require.NoError(t, err)

	err = processor.Initialize()
	require.NoError(t, err)

	// Use context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	const numGoroutines = 100
	const numIterations = 50

	// Start multiple goroutines that call IsReady() concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numIterations; j++ {
				// This should not race with initialization
				_ = processor.IsReady()
				time.Sleep(time.Microsecond) // Small delay to increase chance of race
			}
		}()
	}

	// Start processor in background (this initializes modules)
	startErr := make(chan error, 1)
	go func() {
		startErr <- processor.Start(ctx)
	}()

	// Wait for processor to be ready
	err = processor.WaitForReady(5 * time.Second)
	require.NoError(t, err)

	// Verify processor is actually ready
	require.True(t, processor.IsReady())

	// Cancel context to trigger shutdown
	cancel()

	// Wait for all goroutines to finish
	wg.Wait()

	// Wait for processor to shutdown
	select {
	case err := <-startErr:
		// Processor shut down cleanly
		if err != nil {
			t.Logf("Start returned error (expected during shutdown): %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Processor did not shutdown within timeout")
	}
}

// TestConcurrentAccess tests concurrent access to processor methods
func TestConcurrentAccess(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	processor, err := NewProcessor(ProcessorDeps{
		Config:          DefaultConfig(),
		NATSClient:      natsClient,
		MetricsRegistry: nil,
		Logger:          slog.Default(),
	})
	require.NoError(t, err)

	err = processor.Initialize()
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start processor
	startErr := make(chan error, 1)
	go func() {
		startErr <- processor.Start(ctx)
	}()

	// Wait for ready
	err = processor.WaitForReady(5 * time.Second)
	require.NoError(t, err)

	var wg sync.WaitGroup
	const numGoroutines = 50
	const numIterations = 20

	// Test concurrent IsReady calls
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numIterations; j++ {
				require.True(t, processor.IsReady())
			}
		}()
	}

	// Test concurrent Health calls
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numIterations; j++ {
				health := processor.Health()
				require.True(t, health.Healthy)
			}
		}()
	}

	// Test concurrent GetReadinessDetails calls
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numIterations; j++ {
				details := processor.GetReadinessDetails()
				require.Contains(t, details, "DataManager: initialized")
			}
		}()
	}

	// Wait for all concurrent operations to complete
	wg.Wait()

	// Shutdown
	cancel()

	select {
	case err := <-startErr:
		if err != nil {
			t.Logf("Start returned error (expected during shutdown): %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Processor did not shutdown within timeout")
	}
}
