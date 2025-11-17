package config

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/c360/semstreams/types"
)

func TestSafeConfig_ThreadSafety(t *testing.T) {
	// Create a base config
	baseConfig := &Config{
		Platform: PlatformConfig{
			Org:  "c360",
			ID:   "test-platform",
			Type: "vessel",
		},
		Components: make(ComponentConfigs),
	}

	safeConfig := NewSafeConfig(baseConfig)

	const numGoroutines = 100
	const numOperations = 1000

	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)

	// Start multiple goroutines doing concurrent reads
	for i := 0; i < numGoroutines/2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				cfg := safeConfig.Get()
				if cfg == nil {
					errors <- fmt.Errorf("Got nil config")
					return
				}
				if cfg.Platform.ID != "test-platform" && cfg.Platform.ID != "updated-platform" {
					errors <- fmt.Errorf("Unexpected platform ID: %s", cfg.Platform.ID)
					return
				}
			}
		}()
	}

	// Start multiple goroutines doing concurrent updates
	for i := 0; i < numGoroutines/2; i++ {
		wg.Add(1)
		go func(_ int) {
			defer wg.Done()
			for j := 0; j < numOperations/10; j++ { // Fewer updates than reads
				newConfig := &Config{
					Platform: PlatformConfig{
						Org:  "c360",
						ID:   "updated-platform",
						Type: "vessel",
					},
					Components: make(ComponentConfigs),
				}
				if err := safeConfig.Update(newConfig); err != nil {
					errors <- fmt.Errorf("Update failed: %w", err)
					return
				}
			}
		}(i)
	}

	// Wait for all goroutines to complete
	done := make(chan bool)
	go func() {
		wg.Wait()
		close(done)
	}()

	// Wait for completion or timeout
	select {
	case <-done:
		// Check for errors
		close(errors)
		for err := range errors {
			t.Fatalf("Concurrent access error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Test timed out - possible deadlock")
	}
}

func TestSafeConfig_NilHandling(t *testing.T) {
	// Test with nil config
	safeConfig := NewSafeConfig(nil)

	cfg := safeConfig.Get()
	if cfg == nil {
		t.Error("SafeConfig.Get() should not return nil even with nil base config")
	}

	// Test updating with nil
	err := safeConfig.Update(nil)
	if err == nil {
		t.Error("SafeConfig.Update(nil) should return an error")
	}
}

func TestSafeConfig_ValidationDuringUpdate(t *testing.T) {
	safeConfig := NewSafeConfig(&Config{
		Platform: PlatformConfig{
			Org: "c360",
			ID:  "test",
		},
	})

	// Try to update with invalid config (missing required fields)
	invalidConfig := &Config{
		Platform: PlatformConfig{
			Org: "c360",
			// Missing ID
		},
		// Missing ObjectStore.BucketName
	}

	err := safeConfig.Update(invalidConfig)
	if err == nil {
		t.Error("Update with invalid config should fail validation")
	}

	// Original config should remain unchanged
	cfg := safeConfig.Get()
	if cfg.Platform.ID != "test" {
		t.Error("Original config was modified after failed update")
	}
}

func TestSafeConfig_DeepCopy(t *testing.T) {
	baseConfig := &Config{
		Platform: PlatformConfig{
			Org:          "c360",
			ID:           "test",
			Capabilities: []string{"radar", "ctd"},
		},
		Components: make(ComponentConfigs),
	}

	safeConfig := NewSafeConfig(baseConfig)

	// Get a copy
	cfg1 := safeConfig.Get()
	cfg2 := safeConfig.Get()

	// Modify cfg1
	cfg1.Platform.ID = "modified"
	cfg1.Platform.Capabilities = append(cfg1.Platform.Capabilities, "new-capability")
	// Components is now a map, add a test component
	cfg1.Components["test-component"] = types.ComponentConfig{}

	// cfg2 should be unchanged
	if cfg2.Platform.ID != "test" {
		t.Error("Deep copy failed - cfg2 was affected by cfg1 modification")
	}

	if len(cfg2.Platform.Capabilities) != 2 {
		t.Error("Deep copy failed - cfg2 capabilities were affected")
	}

	// Components is a map, check it wasn't modified
	if len(cfg2.Components) != 0 {
		t.Error("Deep copy failed - cfg2 components were affected")
	}

	// Original config should also be unchanged
	originalCfg := safeConfig.Get()
	if originalCfg.Platform.ID != "test" {
		t.Error("Original config was modified")
	}
}

func TestConfigClone(t *testing.T) {
	tests := []struct {
		name   string
		config *Config
	}{
		{
			name:   "nil config",
			config: nil,
		},
		{
			name:   "empty config",
			config: &Config{},
		},
		{
			name: "full config",
			config: &Config{
				Platform: PlatformConfig{
					Org:          "c360",
					ID:           "test",
					Type:         "vessel",
					Region:       "gulf_mexico",
					Capabilities: []string{"radar", "ctd"},
				},
				Components: make(ComponentConfigs),
				NATS: NATSConfig{
					URLs:          []string{"nats://localhost:4222"},
					ReconnectWait: 2 * time.Second,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clone := tt.config.Clone()

			if tt.config == nil {
				if clone == nil {
					t.Error("Clone of nil should return empty config, not nil")
				}
				return
			}

			// Verify deep copy by modifying original
			if tt.config.Platform.Capabilities != nil {
				originalLen := len(tt.config.Platform.Capabilities)
				tt.config.Platform.Capabilities = append(tt.config.Platform.Capabilities, "new-capability")

				if len(clone.Platform.Capabilities) != originalLen {
					t.Error("Clone was affected by original modification")
				}
			}

			// Components is now a map, test map modification
			if tt.config.Components != nil {
				originalLen := len(tt.config.Components)
				tt.config.Components["new-component"] = types.ComponentConfig{}

				if len(clone.Components) != originalLen {
					t.Error("Clone was affected by original modification")
				}
			}
		})
	}
}
