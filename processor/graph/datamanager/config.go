// Package datamanager consolidates entity and edge operations into a unified data management service.
package datamanager

import "time"

// Config configures the DataManager service
type Config struct {
	// Buffer configuration (from EntityStore)
	BufferConfig BufferConfig

	// KV bucket configuration
	BucketConfig BucketConfig

	// Cache configuration (from EntityStore)
	Cache CacheConfig

	// Edge configuration (from EdgeManager)
	Edge EdgeConfig

	// Worker pool configuration
	Workers int `default:"5"`

	// Operation timeouts
	WriteTimeout time.Duration `default:"5s"`
	ReadTimeout  time.Duration `default:"2s"`

	// Retry configuration
	MaxRetries int           `default:"10"`
	RetryDelay time.Duration `default:"100ms"`
}

// BufferConfig configures the write buffer behavior
type BufferConfig struct {
	// Buffer capacity
	Capacity int `default:"10000"`

	// Batching settings - groups writes for efficient processing
	BatchingEnabled bool          `default:"true"`
	FlushInterval   time.Duration `default:"50ms"`  // How often to flush the buffer
	MaxBatchSize    int           `default:"100"`   // Max writes per batch
	MaxBatchAge     time.Duration `default:"100ms"` // Max age before forced flush

	// Overflow policy when buffer is full
	OverflowPolicy string `default:"drop_oldest"` // drop_oldest, drop_newest, block
}

// BucketConfig configures the NATS KV bucket settings
type BucketConfig struct {
	Name     string        `default:"ENTITY_STATES"`
	TTL      time.Duration `default:"0s"` // 0 = no expiry
	History  int           `default:"5"`
	Replicas int           `default:"3"`
}

// EdgeConfig configures edge-specific settings
type EdgeConfig struct {
	// Edge validation settings
	ValidateEdgeTargets bool `default:"true"`
	AtomicOperations    bool `default:"true"`
}

// CacheConfig configures the L1/L2 cache hierarchy
type CacheConfig struct {
	// L1 Cache (Hot - LRU)
	L1Hot L1CacheConfig

	// L2 Cache (Warm - TTL)
	L2Warm L2CacheConfig

	// Metrics configuration
	Metrics   bool   `default:"true"`
	Component string `default:"data_manager"`
}

// DefaultConfig returns default configuration for DataManager
func DefaultConfig() Config {
	return Config{
		BufferConfig: BufferConfig{
			Capacity:        10000,
			BatchingEnabled: true,
			FlushInterval:   50 * time.Millisecond,
			MaxBatchSize:    100,
			MaxBatchAge:     100 * time.Millisecond,
			OverflowPolicy:  "drop_oldest",
		},
		BucketConfig: BucketConfig{
			Name:     "ENTITY_STATES",
			TTL:      0,
			History:  5,
			Replicas: 3,
		},
		Cache: CacheConfig{
			L1Hot: L1CacheConfig{
				Type:      "lru",
				Size:      1000,
				Metrics:   true,
				Component: "entity_l1",
			},
			L2Warm: L2CacheConfig{
				Type:            "ttl",
				Size:            10000,
				TTL:             5 * time.Minute,
				CleanupInterval: 1 * time.Minute,
				Metrics:         true,
				Component:       "entity_l2",
			},
			Metrics:   true,
			Component: "data_manager",
		},
		Edge: EdgeConfig{
			ValidateEdgeTargets: true,
			AtomicOperations:    true,
		},
		Workers:      5,
		WriteTimeout: 5 * time.Second,
		ReadTimeout:  2 * time.Second,
		MaxRetries:   10,
		RetryDelay:   100 * time.Millisecond,
	}
}
