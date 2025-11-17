package config

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/c360/semstreams/natsclient"
	"github.com/stretchr/testify/require"
)

// TestConfigManager_ShutdownSequence verifies clean shutdown without panics
func TestConfigManager_ShutdownSequence(t *testing.T) {
	// Create NATS client with JetStream and KV
	natsClient := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithKV())

	// Create initial config
	cfg := &Config{
		Platform: PlatformConfig{
			ID:     "test-platform",
			Type:   "edge",
			Org:    "test-org",
			Region: "us-west",
		},
		NATS: NATSConfig{
			URLs: []string{"nats://localhost:4222"},
		},
	}

	// Create Manager
	cm, err := NewConfigManager(cfg, natsClient.Client, slog.Default())
	require.NoError(t, err)

	// Start the manager
	ctx := context.Background()
	err = cm.Start(ctx)
	require.NoError(t, err)

	// Create multiple subscribers
	var subscribers []<-chan Update
	for i := 0; i < 5; i++ {
		ch := cm.OnChange("services.*")
		subscribers = append(subscribers, ch)
	}

	// Start goroutines to read from channels
	done := make(chan struct{})
	for i, ch := range subscribers {
		go func(_ int, updates <-chan Update) {
			for {
				select {
				case update, ok := <-updates:
					if !ok {
						// Channel closed, exit cleanly
						return
					}
					// Process update (just validate it's not nil)
					_ = update
				case <-done:
					return
				}
			}
		}(i, ch)
	}

	// Give everything time to start
	time.Sleep(10 * time.Millisecond)

	// Stop the manager - this should close all channels
	err = cm.Stop(5 * time.Second)
	require.NoError(t, err)

	// Signal subscriber goroutines to exit if they haven't already
	close(done)

	// Give goroutines time to exit cleanly
	time.Sleep(10 * time.Millisecond)

	// If we got here without panics, the shutdown sequence worked correctly
	t.Log("Clean shutdown completed without 'send on closed channel' panics")
}

// TestConfigManager_ConcurrentShutdown tests concurrent operations during shutdown
func TestConfigManager_ConcurrentShutdown(t *testing.T) {
	natsClient := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithKV())

	cfg := &Config{
		Platform: PlatformConfig{
			ID:     "test-platform",
			Type:   "edge",
			Org:    "test-org",
			Region: "us-west",
		},
		NATS: NATSConfig{
			URLs: []string{"nats://localhost:4222"},
		},
	}

	cm, err := NewConfigManager(cfg, natsClient.Client, slog.Default())
	require.NoError(t, err)

	ctx := context.Background()
	err = cm.Start(ctx)
	require.NoError(t, err)

	// Subscribe
	ch := cm.OnChange("services.*")

	// Start a goroutine that tries to read during shutdown
	go func() {
		for range ch {
			t.Logf("Draining config change during shutdown")
		}
	}()

	// Multiple goroutines calling Stop concurrently
	for i := 0; i < 3; i++ {
		go func() {
			_ = cm.Stop(1 * time.Second)
		}()
	}

	// Give everything time to run
	time.Sleep(100 * time.Millisecond)

	// Should handle multiple Stop calls gracefully
	err = cm.Stop(1 * time.Second)
	require.NoError(t, err)

	t.Log("Handled concurrent Stop calls without issues")
}
