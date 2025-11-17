package service_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/c360/semstreams/natsclient"
	"github.com/c360/semstreams/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServiceLifecycleRobustness tests that services handle lifecycle operations correctly
func TestServiceLifecycleRobustness(t *testing.T) {
	ctx := context.Background()

	t.Run("Metrics double-stop is safe", func(t *testing.T) {
		deps := &service.Dependencies{
			Logger: slog.Default(),
		}

		metricsConfig := json.RawMessage(`{"port": 9091}`)
		metrics, err := service.NewMetrics(metricsConfig, deps)
		require.NoError(t, err)

		// Start the service
		err = metrics.Start(ctx)
		require.NoError(t, err)

		// Stop once
		err = metrics.Stop(5 * time.Second)
		assert.NoError(t, err)

		// Stop again - should be safe
		err = metrics.Stop(5 * time.Second)
		assert.NoError(t, err, "Double stop should be safe")
	})

	t.Run("Metrics double-start returns error", func(t *testing.T) {
		deps := &service.Dependencies{
			Logger: slog.Default(),
		}

		metricsConfig := json.RawMessage(`{"port": 9092}`)
		metrics, err := service.NewMetrics(metricsConfig, deps)
		require.NoError(t, err)

		// Start the service
		err = metrics.Start(ctx)
		require.NoError(t, err)
		defer metrics.Stop(5 * time.Second)

		// Start again - should error
		err = metrics.Start(ctx)
		assert.Error(t, err, "Double start should return error")
		assert.Contains(t, err.Error(), "already started")
	})

	t.Run("MessageLogger double-stop is safe", func(t *testing.T) {
		// Create test NATS client
		testClient := natsclient.NewTestClient(t)
		defer testClient.Terminate()

		deps := &service.Dependencies{
			NATSClient: testClient.Client,
			Logger:     slog.Default(),
		}

		msgLoggerConfig := json.RawMessage(`{"max_messages": 100}`)
		msgLogger, err := service.NewMessageLoggerService(msgLoggerConfig, deps)
		require.NoError(t, err)

		// Start the service
		err = msgLogger.Start(ctx)
		require.NoError(t, err)

		// Stop once
		err = msgLogger.Stop(5 * time.Second)
		assert.NoError(t, err)

		// Stop again - should be safe
		err = msgLogger.Stop(5 * time.Second)
		assert.NoError(t, err, "Double stop should be safe")
	})

	t.Run("MessageLogger double-start returns error", func(t *testing.T) {
		// Create test NATS client
		testClient := natsclient.NewTestClient(t)
		defer testClient.Terminate()

		deps := &service.Dependencies{
			NATSClient: testClient.Client,
			Logger:     slog.Default(),
		}

		msgLoggerConfig := json.RawMessage(`{"max_messages": 100}`)
		msgLogger, err := service.NewMessageLoggerService(msgLoggerConfig, deps)
		require.NoError(t, err)

		// Start the service
		err = msgLogger.Start(ctx)
		require.NoError(t, err)
		defer msgLogger.Stop(5 * time.Second)

		// Start again - should error
		err = msgLogger.Start(ctx)
		assert.Error(t, err, "Double start should return error")
		assert.Contains(t, err.Error(), "already running")
	})

	t.Run("Concurrent start/stop stress test", func(t *testing.T) {
		deps := &service.Dependencies{
			Logger: slog.Default(),
		}

		// Test with multiple goroutines trying to start/stop simultaneously
		for i := 0; i < 10; i++ {
			metricsConfig := json.RawMessage(`{"port": 0}`) // Use port 0 for random port
			metrics, err := service.NewMetrics(metricsConfig, deps)
			require.NoError(t, err)

			// Start multiple goroutines trying to start
			startErrors := make(chan error, 5)
			for j := 0; j < 5; j++ {
				go func() {
					startErrors <- metrics.Start(ctx)
				}()
			}

			// Collect results - exactly one should succeed
			var successCount int
			for j := 0; j < 5; j++ {
				err := <-startErrors
				if err == nil {
					successCount++
				}
			}
			assert.Equal(t, 1, successCount, "Exactly one Start should succeed")

			// Now try multiple stops
			stopErrors := make(chan error, 5)
			for j := 0; j < 5; j++ {
				go func() {
					stopErrors <- metrics.Stop(1 * time.Second)
				}()
			}

			// All stops should succeed (idempotent)
			for j := 0; j < 5; j++ {
				err := <-stopErrors
				assert.NoError(t, err, "All Stop calls should succeed")
			}
		}
	})
}
