// Package config provides configuration management for SemStreams applications.
//
// This package handles loading, validation, boot selection, and durable desired
// configuration from JSON files, environment variables, and NATS KV.
//
// # Core Components
//
// Config: Main configuration structure containing platform settings, NATS
// connection details, service configurations, and component definitions.
//
// SafeConfig: Thread-safe wrapper using RWMutex and deep cloning to prevent
// concurrent access issues and accidental mutations.
//
// Manager: Arbitrates file and KV configuration at boot, keeps an in-memory
// authoring view synchronized with durable desired state, and owns its watchers.
// Post-boot writes do not mutate the process's sealed component composition.
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
// # Durable Desired Configuration
//
// Using Manager to observe and author next-boot configuration via NATS KV:
//
//	cm, err := config.NewConfigManager(ctx, cfg, natsClient, logger)
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// Start synchronizing the desired-state authoring view.
//	if err := cm.Start(ctx); err != nil {
//		log.Fatal(err)
//	}
//	defer cm.Stop(5 * time.Second)
//
//	// Read the synchronized desired configuration. Composition roots select
//	// their immutable boot snapshot before constructing components.
//	desired := cm.GetConfig().Get()
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
//	// Read-modify-write atomically: Mutate holds the write lock across the whole
//	// clone → mutate → swap so concurrent mutations cannot lose one another's
//	// change (gh#515). Do NOT do Get() → mutate → Update() — the lock is released
//	// between the read and the swap, so a concurrent writer can clobber you.
//	safeConfig.Mutate(func(cfg *Config) error {
//		c := cfg.Components["my-component"]
//		c.Enabled = true
//		cfg.Components["my-component"] = c
//		return nil
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
