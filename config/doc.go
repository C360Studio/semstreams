// Package config provides configuration management for StreamKit applications.
//
// This package handles loading, validation, and dynamic updates of application
// configuration from JSON files, environment variables, and NATS KV store.
//
// # Core Components
//
// Config: Main configuration structure containing platform settings, NATS
// connection details, service configurations, and component definitions.
//
// SafeConfig: Thread-safe wrapper using RWMutex and deep cloning to prevent
// concurrent access issues and accidental mutations.
//
// Manager: Manages the complete lifecycle of configuration including
// initialization, NATS KV watching, change notifications via channels, and
// graceful shutdown with timeout handling.
//
// Loader: Loads configuration with layer merging (base + overrides) and
// environment variable substitution for flexible deployment scenarios.
//
// # Basic Usage
//
// Loading configuration from files with layer merging:
//
//	loader := config.NewLoader()
//	loader.AddLayer("config/base.json")
//	loader.AddLayer("config/production.json") // Overrides base
//	loader.EnableValidation(true)
//
//	cfg, err := loader.Load()
//	if err != nil {
//		log.Fatal(err)
//	}
//
// # Dynamic Configuration
//
// Using Manager for runtime updates via NATS KV:
//
//	cm, err := config.NewConfigManager(cfg, natsClient, logger)
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// Start watching for config changes
//	if err := cm.Start(ctx); err != nil {
//		log.Fatal(err)
//	}
//	defer cm.Stop(5 * time.Second)
//
//	// Subscribe to specific config changes
//	updates := cm.OnChange("services.*")
//	for update := range updates {
//		log.Printf("Service config changed: %s", update.Key)
//	}
//
// # Thread-Safe Access
//
// SafeConfig ensures thread-safe access to configuration:
//
//	safeConfig := cm.GetConfig()
//
//	// Read config (deep copy returned, safe to use)
//	cfg := safeConfig.Get()
//
//	// Update config atomically
//	safeConfig.Update(func(cfg *Config) {
//		cfg.Components["my-component"].Enabled = true
//	})
//
//	// Push updates to NATS KV
//	cm.PushToKV(ctx)
//
// # Environment Variable Overrides
//
// Configuration values can be overridden using environment variables:
//
//	# Override platform ID
//	export STREAMKIT_PLATFORM_ID="prod-cluster-01"
//
//	# Override NATS URLs (comma-separated)
//	export STREAMKIT_NATS_URLS="nats://server1:4222,nats://server2:4222"
//
// # Layer Merging
//
// Configuration layers are merged with last-wins semantics:
//
//	base.json:
//	  {"platform": {"id": "dev", "log_level": "debug"}}
//
//	production.json:
//	  {"platform": {"id": "prod"}}
//
//	Result:
//	  {"platform": {"id": "prod", "log_level": "debug"}}
//
// # Security
//
// The package includes security validation:
//   - File size limits (10MB max) to prevent memory exhaustion
//   - JSON depth validation (100 levels max) to prevent DoS attacks
//   - Path validation to prevent directory traversal
//   - Regular file checks (no symlinks or device files)
//
// # Configuration Structure
//
// The main Config struct contains:
//
//	type Config struct {
//	    Platform   PlatformConfig           // Platform metadata
//	    NATS       NATSConfig              // Message bus connection
//	    Services   map[string]any  // Service configurations
//	    Components map[string]ComponentConfig // Component definitions
//	}
//
// See the README.md file for detailed examples and configuration patterns.
package config
