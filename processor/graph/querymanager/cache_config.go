package querymanager

import "time"

// CacheConfig configures the multi-tier cache hierarchy
type CacheConfig struct {
	// L1 Cache (Hot - LRU)
	L1Hot L1CacheConfig

	// L2 Cache (Warm - TTL)
	L2Warm L2CacheConfig

	// L3 Cache (Query Results - Hybrid)
	L3Results L3CacheConfig

	// Cache warming settings
	Warming CacheWarmingConfig

	// Metrics configuration
	Metrics   bool   `default:"true"`
	Component string `default:"query_manager"`
}

// L1CacheConfig configures the L1 (hot) LRU cache
type L1CacheConfig struct {
	Type      string `default:"lru"`
	Size      int    `default:"1000"`
	Metrics   bool   `default:"true"`
	Component string `default:"query_l1"`
}

// L2CacheConfig configures the L2 (warm) TTL cache
type L2CacheConfig struct {
	Type            string        `default:"ttl"`
	Size            int           `default:"10000"`
	TTL             time.Duration `default:"5m"`
	CleanupInterval time.Duration `default:"1m"`
	Metrics         bool          `default:"true"`
	Component       string        `default:"query_l2"`
}

// L3CacheConfig configures the L3 (query results) Hybrid cache
type L3CacheConfig struct {
	Type            string        `default:"hybrid"`
	Size            int           `default:"100"`
	TTL             time.Duration `default:"1m"`
	CleanupInterval time.Duration `default:"30s"`
	Metrics         bool          `default:"true"`
	Component       string        `default:"query_l3"`
}

// CacheWarmingConfig configures cache warming behavior
type CacheWarmingConfig struct {
	Enabled           bool          `default:"true"`
	WarmupPatterns    []string      `default:"[\"c360.platform1.robotics.*.>\"]"`
	MaxWarmupEntities int           `default:"500"`
	WarmupTimeout     time.Duration `default:"30s"`
	WarmupInterval    time.Duration `default:"5m"`
}

// InvalidationConfig configures KV Watch for cache invalidation
type InvalidationConfig struct {
	// Watch patterns for different entity types
	Patterns []string `default:"[\"c360.platform1.robotics.*.>\", \"c360.platform1.sensors.*.>\"]"`

	// Batch invalidation settings
	BatchSize     int           `default:"100"`
	BatchInterval time.Duration `default:"10ms"`

	// Invalidation timeouts
	InvalidationTimeout time.Duration `default:"5s"`

	// Watch buffer settings
	WatchBuffer int `default:"10000"`

	// Metrics
	Metrics bool `default:"true"`
}
