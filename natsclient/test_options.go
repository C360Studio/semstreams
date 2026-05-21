package natsclient

import "time"

// Additional helper options for specific use cases

// WithFastStartup configures NATS for fastest possible RUNTIME (short
// connection timeout, no extra features). Container startup itself still
// needs a realistic budget — Docker API responsiveness under parallel
// test pressure routinely exceeds 10s even when NATS itself is ready in
// under a second. Use the package default (30s) for the container
// startup wait; the "fast" part is the 2s connection timeout once the
// container is up.
func WithFastStartup() TestOption {
	return func(cfg *testConfig) {
		cfg.timeout = 2 * time.Second
		cfg.startTimeout = 30 * time.Second
	}
}

// WithIntegrationDefaults configures NATS with settings good for integration tests
func WithIntegrationDefaults() TestOption {
	return func(cfg *testConfig) {
		cfg.timeout = 5 * time.Second
		cfg.startTimeout = 30 * time.Second
		cfg.jetstream = true
	}
}

// WithE2EDefaults configures NATS with settings good for end-to-end tests
func WithE2EDefaults() TestOption {
	return func(cfg *testConfig) {
		cfg.timeout = 10 * time.Second
		cfg.startTimeout = 60 * time.Second
		cfg.jetstream = true
		cfg.kv = true
	}
}

// WithProductionLike configures NATS with settings that mimic production
func WithProductionLike() TestOption {
	return func(cfg *testConfig) {
		cfg.timeout = 30 * time.Second
		cfg.startTimeout = 60 * time.Second
		cfg.jetstream = true
		cfg.kv = true
		// Use latest stable version
		cfg.natsVersion = "2.12-alpine"
	}
}

// WithMinimalFeatures configures NATS with only basic pub/sub (fastest startup)
func WithMinimalFeatures() TestOption {
	return func(cfg *testConfig) {
		cfg.jetstream = false
		cfg.kv = false
		cfg.timeout = 1 * time.Second
		cfg.startTimeout = 5 * time.Second
	}
}
