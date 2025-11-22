package querymanager

import (
	"fmt"
	"time"

	"github.com/c360/semstreams/errors"
)

// Config configures the query manager service
type Config struct {
	// Multi-tier cache configuration
	Cache CacheConfig

	// KV Watch configuration for cache invalidation
	Invalidation InvalidationConfig

	// Query execution configuration
	Query QueryConfig

	// Operation timeouts
	Timeouts TimeoutsConfig

	// Health check configuration
	HealthCheck HealthCheckConfig

	// KV bucket configuration
	Buckets BucketsConfig

	// Index manager integration (dependency)
	IndexManager RemoteIndexConfig
}

// QueryConfig configures query execution behavior
type QueryConfig struct {
	// Query planning
	MaxQueryDepth   int           `default:"10"`
	MaxQueryResults int           `default:"10000"`
	QueryTimeout    time.Duration `default:"30s"`

	// Path execution settings
	MaxPathLength int           `default:"20"`
	PathTimeout   time.Duration `default:"10s"`

	// Graph snapshot settings
	MaxSnapshotSize int           `default:"50000"`
	SnapshotTimeout time.Duration `default:"60s"`

	// Result caching
	CacheQueryResults bool          `default:"true"`
	ResultCacheTTL    time.Duration `default:"1m"`

	// Query optimization
	OptimizeQueries bool `default:"true"`
	ParallelQueries bool `default:"true"`
	MaxConcurrency  int  `default:"10"`
}

// TimeoutsConfig configures operation timeouts
type TimeoutsConfig struct {
	// Entity operations
	EntityGet   time.Duration `default:"2s"`
	EntityBatch time.Duration `default:"10s"`

	// Cache operations
	CacheGet        time.Duration `default:"100ms"`
	CacheSet        time.Duration `default:"100ms"`
	CacheInvalidate time.Duration `default:"1s"`

	// KV operations
	KVGet   time.Duration `default:"1s"`
	KVBatch time.Duration `default:"5s"`

	// Index operations
	IndexQuery time.Duration `default:"2s"`

	// Shutdown timeout
	Shutdown time.Duration `default:"10s"`
}

// HealthCheckConfig configures health monitoring
type HealthCheckConfig struct {
	// Health check intervals
	Interval         time.Duration `default:"30s"`
	CacheHealthCheck time.Duration `default:"10s"`

	// Health thresholds
	MaxLatency   time.Duration `default:"5s"`
	MinHitRatio  float64       `default:"0.5"`
	MaxErrorRate float64       `default:"0.05"`

	// Memory thresholds
	MaxMemoryMB float64 `default:"1000"`

	// Watch health thresholds
	MaxWatchLag    time.Duration `default:"5s"`
	MaxWatchErrors int64         `default:"10"`
}

// BucketsConfig specifies KV bucket names
type BucketsConfig struct {
	EntityStates string `default:"ENTITY_STATES"`
}

// RemoteIndexConfig configures remote index manager dependency
type RemoteIndexConfig struct {
	// Connection timeout for remote index manager
	ConnectionTimeout time.Duration `default:"5s"`

	// Query timeouts for remote index manager operations
	QueryTimeout time.Duration `default:"2s"`

	// Retry configuration
	MaxRetries int           `default:"3"`
	RetryDelay time.Duration `default:"100ms"`

	// Health check for remote index manager dependency
	HealthCheckInterval time.Duration `default:"30s"`
}

// Validate validates the configuration
func (c *Config) Validate() error {
	// Validate cache configuration
	if err := c.validateCache(); err != nil {
		return err
	}

	// Validate invalidation configuration
	if err := c.validateInvalidation(); err != nil {
		return err
	}

	// Validate query configuration
	if err := c.validateQuery(); err != nil {
		return err
	}

	// Validate timeouts
	if err := c.validateTimeouts(); err != nil {
		return err
	}

	// Validate health check configuration
	if err := c.validateHealthCheck(); err != nil {
		return err
	}

	// Validate buckets configuration
	if err := c.validateBuckets(); err != nil {
		return err
	}

	// Validate index manager configuration
	return c.validateIndexManager()
}

// validateCache validates cache configuration
func (c *Config) validateCache() error {
	if c.Cache.L1Hot.Size <= 0 {
		msg := fmt.Sprintf("L1 cache size must be positive, got %d", c.Cache.L1Hot.Size)
		return errors.WrapInvalid(errors.ErrInvalidConfig, "query manager", "Validate", msg)
	}
	if c.Cache.L2Warm.Size <= 0 {
		msg := fmt.Sprintf("L2 cache size must be positive, got %d", c.Cache.L2Warm.Size)
		return errors.WrapInvalid(errors.ErrInvalidConfig, "query manager", "Validate", msg)
	}
	if c.Cache.L3Results.Size <= 0 {
		msg := fmt.Sprintf("L3 cache size must be positive, got %d", c.Cache.L3Results.Size)
		return errors.WrapInvalid(errors.ErrInvalidConfig, "query manager", "Validate", msg)
	}
	if c.Cache.L2Warm.TTL <= 0 {
		msg := fmt.Sprintf("L2 TTL must be positive, got %v", c.Cache.L2Warm.TTL)
		return errors.WrapInvalid(errors.ErrInvalidConfig, "query manager", "Validate", msg)
	}
	if c.Cache.L3Results.TTL <= 0 {
		msg := fmt.Sprintf("L3 TTL must be positive, got %v", c.Cache.L3Results.TTL)
		return errors.WrapInvalid(errors.ErrInvalidConfig, "query manager", "Validate", msg)
	}
	return nil
}

// validateInvalidation validates invalidation configuration
func (c *Config) validateInvalidation() error {
	if len(c.Invalidation.Patterns) == 0 {
		msg := "at least one invalidation pattern must be configured"
		return errors.WrapInvalid(errors.ErrInvalidConfig, "query manager", "Validate", msg)
	}
	if c.Invalidation.BatchSize <= 0 {
		msg := fmt.Sprintf("invalidation batch size must be positive, got %d", c.Invalidation.BatchSize)
		return errors.WrapInvalid(errors.ErrInvalidConfig, "query manager", "Validate", msg)
	}
	return nil
}

// validateQuery validates query configuration
func (c *Config) validateQuery() error {
	if c.Query.MaxQueryDepth <= 0 {
		msg := fmt.Sprintf("max query depth must be positive, got %d", c.Query.MaxQueryDepth)
		return errors.WrapInvalid(errors.ErrInvalidConfig, "query manager", "Validate", msg)
	}
	if c.Query.MaxQueryResults <= 0 {
		msg := fmt.Sprintf("max query results must be positive, got %d", c.Query.MaxQueryResults)
		return errors.WrapInvalid(errors.ErrInvalidConfig, "query manager", "Validate", msg)
	}
	if c.Query.MaxConcurrency <= 0 {
		msg := fmt.Sprintf("max concurrency must be positive, got %d", c.Query.MaxConcurrency)
		return errors.WrapInvalid(errors.ErrInvalidConfig, "query manager", "Validate", msg)
	}
	return nil
}

// validateTimeouts validates timeout configuration
func (c *Config) validateTimeouts() error {
	if c.Timeouts.EntityGet <= 0 {
		msg := fmt.Sprintf("entity get timeout must be positive, got %v", c.Timeouts.EntityGet)
		return errors.WrapInvalid(errors.ErrInvalidConfig, "query manager", "Validate", msg)
	}
	if c.Timeouts.KVGet <= 0 {
		msg := fmt.Sprintf("KV get timeout must be positive, got %v", c.Timeouts.KVGet)
		return errors.WrapInvalid(errors.ErrInvalidConfig, "query manager", "Validate", msg)
	}
	if c.Timeouts.Shutdown <= 0 {
		msg := fmt.Sprintf("shutdown timeout must be positive, got %v", c.Timeouts.Shutdown)
		return errors.WrapInvalid(errors.ErrInvalidConfig, "query manager", "Validate", msg)
	}
	return nil
}

// validateHealthCheck validates health check configuration
func (c *Config) validateHealthCheck() error {
	if c.HealthCheck.Interval <= 0 {
		msg := fmt.Sprintf("health check interval must be positive, got %v", c.HealthCheck.Interval)
		return errors.WrapInvalid(errors.ErrInvalidConfig, "query manager", "Validate", msg)
	}
	if c.HealthCheck.MinHitRatio < 0 || c.HealthCheck.MinHitRatio > 1 {
		msg := fmt.Sprintf("min hit ratio must be between 0 and 1, got %f", c.HealthCheck.MinHitRatio)
		return errors.WrapInvalid(errors.ErrInvalidConfig, "query manager", "Validate", msg)
	}
	if c.HealthCheck.MaxErrorRate < 0 || c.HealthCheck.MaxErrorRate > 1 {
		msg := fmt.Sprintf("max error rate must be between 0 and 1, got %f", c.HealthCheck.MaxErrorRate)
		return errors.WrapInvalid(errors.ErrInvalidConfig, "query manager", "Validate", msg)
	}
	return nil
}

// validateBuckets validates bucket configuration
func (c *Config) validateBuckets() error {
	if c.Buckets.EntityStates == "" {
		msg := "entity states bucket name cannot be empty"
		return errors.WrapInvalid(errors.ErrInvalidConfig, "query manager", "Validate", msg)
	}
	return nil
}

// validateIndexManager validates index manager configuration
func (c *Config) validateIndexManager() error {
	if c.IndexManager.ConnectionTimeout <= 0 {
		msg := fmt.Sprintf(
			"index manager connection timeout must be positive, got %v",
			c.IndexManager.ConnectionTimeout,
		)
		return errors.WrapInvalid(errors.ErrInvalidConfig, "query manager", "Validate", msg)
	}
	if c.IndexManager.MaxRetries < 0 {
		msg := fmt.Sprintf(
			"index manager max retries must be non-negative, got %d",
			c.IndexManager.MaxRetries,
		)
		return errors.WrapInvalid(errors.ErrInvalidConfig, "query manager", "Validate", msg)
	}
	return nil
}

// setCacheDefaults sets default values for cache configuration
func (c *Config) setCacheDefaults() {
	if c.Cache.L1Hot.Size == 0 {
		c.Cache.L1Hot.Size = 1000
	}
	if c.Cache.L1Hot.Component == "" {
		c.Cache.L1Hot.Component = "query_l1"
	}

	if c.Cache.L2Warm.Size == 0 {
		c.Cache.L2Warm.Size = 10000
	}
	if c.Cache.L2Warm.TTL == 0 {
		c.Cache.L2Warm.TTL = 5 * time.Minute
	}
	if c.Cache.L2Warm.CleanupInterval == 0 {
		c.Cache.L2Warm.CleanupInterval = 1 * time.Minute
	}
	if c.Cache.L2Warm.Component == "" {
		c.Cache.L2Warm.Component = "query_l2"
	}

	if c.Cache.L3Results.Size == 0 {
		c.Cache.L3Results.Size = 100
	}
	if c.Cache.L3Results.TTL == 0 {
		c.Cache.L3Results.TTL = 1 * time.Minute
	}
	if c.Cache.L3Results.CleanupInterval == 0 {
		c.Cache.L3Results.CleanupInterval = 30 * time.Second
	}
	if c.Cache.L3Results.Component == "" {
		c.Cache.L3Results.Component = "query_l3"
	}
}

// setInvalidationDefaults sets default values for invalidation configuration
func (c *Config) setInvalidationDefaults() {
	if len(c.Invalidation.Patterns) == 0 {
		c.Invalidation.Patterns = []string{"c360.platform1.robotics.*.>", "c360.platform1.sensors.*.>"}
	}
	if c.Invalidation.BatchSize == 0 {
		c.Invalidation.BatchSize = 100
	}
	if c.Invalidation.BatchInterval == 0 {
		c.Invalidation.BatchInterval = 10 * time.Millisecond
	}
}

// setQueryDefaults sets default values for query configuration
func (c *Config) setQueryDefaults() {
	if c.Query.MaxQueryDepth == 0 {
		c.Query.MaxQueryDepth = 10
	}
	if c.Query.MaxQueryResults == 0 {
		c.Query.MaxQueryResults = 10000
	}
	if c.Query.MaxConcurrency == 0 {
		c.Query.MaxConcurrency = 10
	}
}

// setTimeoutDefaults sets default values for timeout configuration
func (c *Config) setTimeoutDefaults() {
	if c.Timeouts.EntityGet == 0 {
		c.Timeouts.EntityGet = 5 * time.Second
	}
	if c.Timeouts.KVGet == 0 {
		c.Timeouts.KVGet = 3 * time.Second
	}
	if c.Timeouts.Shutdown == 0 {
		c.Timeouts.Shutdown = 5 * time.Second
	}
}

// setHealthCheckDefaults sets default values for health check configuration
func (c *Config) setHealthCheckDefaults() {
	if c.HealthCheck.Interval == 0 {
		c.HealthCheck.Interval = 30 * time.Second
	}
	if c.HealthCheck.MinHitRatio == 0 {
		c.HealthCheck.MinHitRatio = 0.8
	}
	if c.HealthCheck.MaxErrorRate == 0 {
		c.HealthCheck.MaxErrorRate = 0.1
	}
}

// setIndexManagerDefaults sets default values for index manager configuration
func (c *Config) setIndexManagerDefaults() {
	if c.IndexManager.ConnectionTimeout == 0 {
		c.IndexManager.ConnectionTimeout = 5 * time.Second
	}
	if c.IndexManager.MaxRetries == 0 {
		c.IndexManager.MaxRetries = 3
	}
}

// setBucketDefaults sets default values for bucket configuration
func (c *Config) setBucketDefaults() {
	if c.Buckets.EntityStates == "" {
		c.Buckets.EntityStates = "ENTITY_STATES"
	}
}

// SetDefaults sets default values for the configuration
func (c *Config) SetDefaults() {
	c.setCacheDefaults()
	c.setInvalidationDefaults()
	c.setQueryDefaults()
	c.setTimeoutDefaults()
	c.setHealthCheckDefaults()
	c.setIndexManagerDefaults()
	c.setBucketDefaults()
}
