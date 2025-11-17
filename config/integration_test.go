package config

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestConfigSystem_Integration tests the entire config system for thread safety and panic prevention
func TestConfigSystem_Integration(t *testing.T) {
	// Create a valid config
	baseConfig := &Config{
		Platform: PlatformConfig{
			Org:  "c360",
			ID:   "integration-test",
			Type: "vessel",
		},
		Components: make(ComponentConfigs),
	}

	// Test SafeConfig in high-concurrency scenario
	safeConfig := NewSafeConfig(baseConfig)

	const numWorkers = 50
	const iterations = 100
	var wg sync.WaitGroup
	errors := make(chan error, numWorkers)

	// Start concurrent readers
	for i := 0; i < numWorkers/2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				cfg := safeConfig.Get()

				// Test safe accessors don't panic
				testComponentConfig := map[string]any{
					"port":     14550,
					"host":     "localhost",
					"subjects": []string{"test.subject"},
					"enabled":  true,
					"invalid":  map[string]string{"nested": "value"}, // Wrong type
				}

				// These should never panic regardless of data type mismatches
				_ = GetString(testComponentConfig, "host", "default")
				_ = GetInt(testComponentConfig, "port", 8080)
				_ = GetBool(testComponentConfig, "enabled", false)
				_ = GetStringSlice(testComponentConfig, "subjects", []string{"default"})

				// Test with completely invalid data
				invalidConfig := map[string]any{
					"string_as_int":   "not-a-number",
					"int_as_bool":     42,
					"array_as_string": []int{1, 2, 3},
					"null_value":      nil,
				}

				_ = GetString(invalidConfig, "string_as_int", "safe")
				_ = GetInt(invalidConfig, "int_as_bool", 0)
				_ = GetBool(invalidConfig, "array_as_string", false)

				// Verify config integrity
				if cfg.Platform.ID != "integration-test" && cfg.Platform.ID != "updated-test" {
					errors <- fmt.Errorf("Config corruption detected")
					return
				}
			}
		}()
	}

	// Start concurrent writers
	for i := 0; i < numWorkers/2; i++ {
		wg.Add(1)
		go func(_ int) {
			defer wg.Done()
			for j := 0; j < iterations/10; j++ {
				newConfig := &Config{
					Platform: PlatformConfig{
						Org:  "c360",
						ID:   "updated-test",
						Type: "vessel",
					},
					Components: make(ComponentConfigs),
				}

				if err := safeConfig.Update(newConfig); err != nil {
					errors <- err
					return
				}
			}
		}(i)
	}

	// Wait for completion with timeout
	done := make(chan bool)
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		close(errors)
		for err := range errors {
			t.Fatalf("Integration test failed: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Integration test timed out")
	}
}

// TestComponentConfigAccess_NoPanics tests that component config access is panic-safe
func TestComponentConfigAccess_NoPanics(t *testing.T) {
	// Simulate various component configurations that might cause panics
	testConfigs := []map[string]any{
		// Valid config
		{
			"components": map[string]any{
				"udp": map[string]any{
					"port": 14550,
					"host": "0.0.0.0",
				},
			},
		},
		// Invalid nested structure
		{
			"components": "not-a-map",
		},
		// Missing components
		{},
		// Nil values
		{
			"components": nil,
		},
		// Mixed types
		{
			"components": map[string]any{
				"udp": []string{"invalid", "config"},
			},
		},
	}

	for i, cfg := range testConfigs {
		t.Run(fmt.Sprintf("config_%d", i), func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Component config access panicked: %v", r)
				}
			}()

			// These should never panic
			_, _ = GetComponentConfig(cfg, "udp")
			_ = GetString(cfg, "components", "")
			_ = HasKey(cfg, "components")

			if components, err := GetComponentConfig(cfg, "udp"); err == nil {
				_ = GetInt(components, "port", 8080)
				_ = GetString(components, "host", "localhost")
			}
		})
	}
}

// TestNestedAccess_EdgeCases tests nested config access edge cases
func TestNestedAccess_EdgeCases(t *testing.T) {
	edgeCaseConfigs := []map[string]any{
		// Deeply nested
		{
			"level1": map[string]any{
				"level2": map[string]any{
					"level3": map[string]any{
						"value": "deep",
					},
				},
			},
		},
		// Broken nesting
		{
			"level1": "not-a-map",
		},
		// Empty maps
		{
			"level1": map[string]any{},
		},
		// Nil in chain
		{
			"level1": map[string]any{
				"level2": nil,
			},
		},
	}

	for i, cfg := range edgeCaseConfigs {
		t.Run(fmt.Sprintf("edge_case_%d", i), func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Nested access panicked: %v", r)
				}
			}()

			// These should never panic regardless of structure
			_ = GetNestedString(cfg, []string{"level1", "level2", "level3", "value"}, "default")
			_ = GetNestedInt(cfg, []string{"level1", "level2", "count"}, 0)
			_ = GetNestedBool(cfg, []string{"level1", "level2", "enabled"}, false)
			_ = HasNestedKey(cfg, []string{"level1", "level2", "level3"})
		})
	}
}
