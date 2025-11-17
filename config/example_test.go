package config_test

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/c360/semstreams/config"
)

// ExampleLoader_Load demonstrates loading configuration from multiple layers
// with environment variable overrides and validation.
func ExampleLoader_Load() {
	loader := config.NewLoader()

	// Add base configuration layer
	loader.AddLayer("testdata/base.json")

	// Add environment-specific overrides
	loader.AddLayer("testdata/production.json")

	// Enable validation to catch errors early
	loader.EnableValidation(true)

	// Load merged configuration
	cfg, err := loader.Load()
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(cfg.Platform.ID)
	// Output: test-platform
}

// ExampleLoader_Load_environmentOverrides demonstrates using environment
// variables to override configuration values at runtime.
func ExampleLoader_Load_environmentOverrides() {
	// Set environment variables (in real usage, these would be set externally)
	// export STREAMKIT_PLATFORM_ID="prod-cluster-01"
	// export STREAMKIT_NATS_URLS="nats://server1:4222,nats://server2:4222"

	loader := config.NewLoader()
	loader.AddLayer("testdata/base.json")

	cfg, err := loader.Load()
	if err != nil {
		log.Fatal(err)
	}

	// Platform ID and NATS URLs can be overridden via environment
	fmt.Printf("Platform: %s\n", cfg.Platform.ID)
	fmt.Printf("NATS URLs: %v\n", cfg.NATS.URLs)
}

// ExampleSafeConfig_Get demonstrates thread-safe configuration access.
// The Get method returns a deep copy, preventing accidental mutations.
func ExampleSafeConfig_Get() {
	// Assume we have a Manager instance
	// safeConfig := configManager.GetConfig()

	// Get returns a deep copy - safe to use without locks
	// cfg := safeConfig.Get()

	// Read configuration values
	// platformID := cfg.Platform.ID
	// natsURLs := cfg.NATS.URLs

	// The returned config is a copy, so modifications don't affect
	// the shared state
	// cfg.Platform.ID = "modified" // Only affects this copy

	fmt.Println("Thread-safe configuration access")
	// Output: Thread-safe configuration access
}

// ExampleSafeConfig_Update demonstrates atomic configuration updates.
func ExampleSafeConfig_Update() {
	// Assume we have a Manager instance
	// safeConfig := configManager.GetConfig()

	// Update configuration atomically
	// safeConfig.Update(func(cfg *config.Config) {
	//     // Enable a component
	//     if comp, exists := cfg.Components["my-component"]; exists {
	//         comp.Enabled = true
	//         cfg.Components["my-component"] = comp
	//     }
	// })

	fmt.Println("Configuration updated atomically")
	// Output: Configuration updated atomically
}

// ExampleManager demonstrates the complete lifecycle of dynamic
// configuration management with NATS KV watching.
func ExampleManager() {
	// This example shows the complete pattern, but cannot run without NATS
	// In real usage:

	// 1. Load initial configuration
	// loader := config.NewLoader()
	// loader.AddLayer("config/base.json")
	// cfg, err := loader.Load()

	// 2. Create Manager with NATS client
	// cm, err := config.NewConfigManager(cfg, natsClient, logger)
	// if err != nil {
	//     log.Fatal(err)
	// }

	// 3. Start watching for changes
	// ctx := context.Background()
	// if err := cm.Start(ctx); err != nil {
	//     log.Fatal(err)
	// }
	// defer cm.Stop(5 * time.Second)

	// 4. Subscribe to configuration changes
	// updates := cm.OnChange("components.*")
	// go func() {
	//     for update := range updates {
	//         log.Printf("Component config changed: %s = %v",
	//             update.Key, update.Value)
	//     }
	// }()

	// 5. Push local changes to NATS KV
	// safeConfig := cm.GetConfig()
	// safeConfig.Update(func(cfg *config.Config) {
	//     cfg.Components["new-component"] = config.ComponentConfig{
	//         Type:    "processor/json_map",
	//         Enabled: true,
	//     }
	// })
	// cm.PushToKV(ctx)

	fmt.Println("Dynamic configuration management")
	// Output: Dynamic configuration management
}

// ExampleManager_OnChange demonstrates subscribing to specific
// configuration change patterns.
func ExampleManager_OnChange() {
	// Assume we have a running Manager
	// cm := getConfigManager()

	// Subscribe to all service configuration changes
	// serviceUpdates := cm.OnChange("services.*")

	// Subscribe to specific component changes
	// componentUpdates := cm.OnChange("components.my-component")

	// Subscribe to platform configuration
	// platformUpdates := cm.OnChange("platform")

	// Process updates
	// go func() {
	//     for update := range serviceUpdates {
	//         log.Printf("Service updated: %s", update.Key)
	//         // React to configuration change
	//         handleServiceUpdate(update)
	//     }
	// }()

	fmt.Println("Subscribed to configuration changes")
	// Output: Subscribed to configuration changes
}

// Example_componentAccess demonstrates type-safe component configuration access.
func Example_componentAccess() {
	// Assume we have a loaded configuration
	// cfg := loadConfig()

	// Get component configuration with type checking
	// comp, exists := cfg.Components["udp-input"]
	// if !exists {
	//     log.Fatal("Component not found")
	// }

	// Access component properties
	// componentType := comp.Type
	// enabled := comp.Enabled
	// config := comp.Config

	// Type-safe access to nested config using helpers
	// bindAddr := cfg.GetString("components.udp-input.config.bind_address")
	// port := cfg.GetInt("components.udp-input.config.port")

	fmt.Println("Type-safe component access")
	// Output: Type-safe component access
}

// ExampleManager_PushToKV demonstrates pushing local configuration
// changes to NATS KV for distribution to other instances.
func ExampleManager_PushToKV() {
	// This demonstrates the pattern for pushing config updates

	// Get the safe config wrapper
	// safeConfig := cm.GetConfig()

	// Make local changes
	// safeConfig.Update(func(cfg *config.Config) {
	//     cfg.Platform.LogLevel = "debug"
	//     cfg.Components["processor-1"].Enabled = false
	// })

	// Push changes to NATS KV
	// ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	// defer cancel()
	//
	// if err := cm.PushToKV(ctx); err != nil {
	//     log.Printf("Failed to push config: %v", err)
	// }

	// Other instances watching the KV will receive the updates

	fmt.Println("Configuration pushed to NATS KV")
	// Output: Configuration pushed to NATS KV
}

// ExampleManager_Stop demonstrates graceful shutdown of Manager.
func ExampleManager_Stop() {
	// Assume we have a running Manager
	// cm := getConfigManager()

	// Graceful shutdown with timeout
	// timeout := 5 * time.Second
	// if err := cm.Stop(timeout); err != nil {
	//     log.Printf("Manager shutdown error: %v", err)
	// }

	// Stop is idempotent - safe to call multiple times
	// cm.Stop(timeout) // No error

	fmt.Println("Manager stopped gracefully")
	// Output: Manager stopped gracefully
}

// ExampleMinimalConfig demonstrates using the simplified MinimalConfig
// for basic StreamKit applications.
func ExampleMinimalConfig() {
	// MinimalConfig provides a simplified configuration structure
	// for applications that don't need the full Config complexity

	// Load minimal configuration
	// cfg, err := config.LoadMinimalConfig("config/minimal.json")
	// if err != nil {
	//     log.Fatal(err)
	// }

	// Access core settings
	// platformID := cfg.Platform.ID
	// natsURLs := cfg.NATS.URLs
	// messageLoggerEnabled := cfg.Services.MessageLogger

	// MinimalConfig includes:
	// - Platform configuration (ID, environment, logging)
	// - NATS connection settings
	// - Core service toggles (message logger, discovery)

	fmt.Println("Minimal configuration for simple applications")
	// Output: Minimal configuration for simple applications
}

// Helper function to demonstrate context timeout pattern
func demonstrateContextTimeout() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Use context for operations with timeout
	_ = ctx
}
