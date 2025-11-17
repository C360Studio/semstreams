package indexmanager

import (
	"fmt"
	"time"

	"github.com/c360/semstreams/errors"
)

// Config configures the IndexManager
type Config struct {
	// Event buffer configuration
	EventBuffer EventBufferConfig `json:"event_buffer"`

	// Deduplication settings
	Deduplication DeduplicationConfig `json:"deduplication"`

	// Batch processing configuration
	BatchProcessing BatchProcessingConfig `json:"batch_processing"`

	// Worker pool configuration
	Workers int `json:"workers" default:"10"`

	// Index enable/disable flags
	Indexes IndexesConfig `json:"indexes"`

	// Cache configuration for each index
	Caches IndexCachesConfig `json:"caches"`

	// Semantic search configuration (optional)
	// Set Embedding.Enabled = false to disable semantic search features
	Embedding EmbeddingConfig `json:"embedding"`

	// Operation timeouts
	ProcessTimeout time.Duration `json:"process_timeout" default:"5s"`
	QueryTimeout   time.Duration `json:"query_timeout"   default:"2s"`

	// KV bucket names
	Buckets BucketsConfig `json:"buckets"`

	// Health check settings
	HealthCheck HealthCheckConfig `json:"health_check"`
}

// EventBufferConfig configures the event buffer behavior
type EventBufferConfig struct {
	// Buffer capacity for incoming KV change events
	Capacity int `json:"capacity" default:"50000"`

	// Overflow policy when buffer is full
	OverflowPolicy string `json:"overflow_policy" default:"drop_oldest"` // drop_oldest, drop_newest, block

	// Enable metrics collection
	Metrics bool `json:"metrics" default:"true"`
}

// DeduplicationConfig configures event deduplication
type DeduplicationConfig struct {
	// Enable deduplication
	Enabled bool `json:"enabled" default:"true"`

	// Deduplication window duration
	Window time.Duration `json:"window" default:"1s"`

	// Deduplication cache size
	CacheSize int `json:"cache_size" default:"10000"`

	// TTL for deduplication entries
	TTL time.Duration `json:"ttl" default:"5s"`
}

// BatchProcessingConfig configures batch processing behavior
type BatchProcessingConfig struct {
	// Maximum batch size before forcing processing
	Size int `json:"size" default:"500"`

	// Maximum time to wait before processing batch
	Interval time.Duration `json:"interval" default:"500ms"`

	// Enable parallel processing of different index types
	ParallelIndexes bool `json:"parallel_indexes" default:"true"`
}

// IndexesConfig controls which indexes are enabled
type IndexesConfig struct {
	Predicate bool `json:"predicate" default:"true"`
	Incoming  bool `json:"incoming"  default:"true"`
	Alias     bool `json:"alias"     default:"true"`
	Spatial   bool `json:"spatial"   default:"true"`
	Temporal  bool `json:"temporal"  default:"true"`
}

// IndexCachesConfig configures LRU caches for each index
type IndexCachesConfig struct {
	// Enable caching for index lookups
	Enabled bool `json:"enabled" default:"true"`

	// Cache sizes for each index type
	PredicateSize int `json:"predicate_size" default:"500"`
	SpatialSize   int `json:"spatial_size"   default:"200"`
	TemporalSize  int `json:"temporal_size"  default:"200"`
	IncomingSize  int `json:"incoming_size"  default:"1000"`
	AliasSize     int `json:"alias_size"     default:"100"`
}

// BucketsConfig specifies KV bucket names for each index type
type BucketsConfig struct {
	EntityStates string `json:"entity_states" default:"ENTITY_STATES"`
	Predicate    string `json:"predicate"     default:"PREDICATE_INDEX"`
	Incoming     string `json:"incoming"      default:"INCOMING_INDEX"`
	Alias        string `json:"alias"         default:"ALIAS_INDEX"`
	Spatial      string `json:"spatial"       default:"SPATIAL_INDEX"`
	Temporal     string `json:"temporal"      default:"TEMPORAL_INDEX"`
}

// HealthCheckConfig configures health monitoring
type HealthCheckConfig struct {
	// Interval for health checks
	Interval time.Duration `json:"interval" default:"30s"`

	// Maximum acceptable processing lag
	MaxLag time.Duration `json:"max_lag" default:"5s"`

	// Maximum backlog size before marking unhealthy
	MaxBacklog int `json:"max_backlog" default:"1000"`

	// Maximum error count before marking unhealthy
	MaxErrors int64 `json:"max_errors" default:"10"`
}

// EmbeddingConfig configures semantic search embedding generation.
//
// Semantic search is enabled by default with BM25 (pure Go, no external service).
// To disable, set Enabled = false or Provider = "disabled".
//
// The system automatically falls back from HTTP to BM25 if service is unavailable.
type EmbeddingConfig struct {
	// Enabled controls whether semantic search features are available
	// Default: true (using BM25 provider)
	Enabled bool `json:"enabled"`

	// Provider specifies the embedding provider (default: "bm25")
	// - "bm25": Pure Go lexical search (default, no external service)
	// - "http": HTTP API (semembed, LocalAI, OpenAI) with automatic BM25 fallback
	// - "disabled": Disable semantic search entirely
	// - Empty/unspecified: Defaults to "bm25"
	Provider string `json:"provider"`

	// HTTPEndpoint is the URL for HTTP embedding provider
	// Example: "http://localhost:8082"
	HTTPEndpoint string `json:"http_endpoint"`

	// HTTPModel is the model name for HTTP provider
	// Example: "all-MiniLM-L6-v2"
	HTTPModel string `json:"http_model"`

	// TextFields are the entity property fields to extract text from for embedding
	// Default: ["title", "content", "description", "summary", "text", "name"]
	TextFields []string `json:"text_fields"`

	// EnabledTypes is a list of message type patterns to embed (allow list)
	// Patterns support wildcards: "alerts.*.*", "events.incident.*", "notes.*.*"
	// Message types are in format: "domain.category.version" (e.g., "alerts.critical.v1")
	// If empty, all types are considered (subject to SkipTypes)
	EnabledTypes []string `json:"enabled_types,omitempty"`

	// SkipTypes is a list of message type patterns to NOT embed (deny list)
	// Evaluated before EnabledTypes. Common patterns:
	// - "telemetry.*.*" (skip raw sensor data)
	// - "sensors.*.*" (skip sensor readings)
	// - "metrics.*.*" (skip numeric metrics)
	SkipTypes []string `json:"skip_types,omitempty"`

	// RetentionWindow specifies how long to keep embeddings in memory before eviction
	// Prevents OOM from unbounded growth. Default: 24h
	// Set to 0 to disable TTL (not recommended for production)
	RetentionWindow time.Duration `json:"retention_window" default:"24h"`

	// CacheBucket is the NATS KV bucket name for embedding cache (optional)
	// If empty, caching is disabled. Recommended: "EMBEDDINGS_CACHE"
	CacheBucket string `json:"cache_bucket"`
}

// DefaultConfig returns a default Config with sensible defaults
func DefaultConfig() Config {
	return Config{
		EventBuffer: EventBufferConfig{
			Capacity:       50000,
			OverflowPolicy: "drop_oldest",
			Metrics:        true,
		},
		Deduplication: DeduplicationConfig{
			Enabled:   true,
			Window:    1 * time.Second,
			CacheSize: 10000,
			TTL:       5 * time.Second,
		},
		BatchProcessing: BatchProcessingConfig{
			Size:            500,
			Interval:        500 * time.Millisecond,
			ParallelIndexes: true,
		},
		Workers: 10,
		Indexes: IndexesConfig{
			Predicate: true,
			Incoming:  true,
			Alias:     true,
			Spatial:   true,
			Temporal:  true,
		},
		Embedding: EmbeddingConfig{
			Enabled:         true,   // Enable by default with BM25
			Provider:        "bm25", // Pure Go fallback (no external service needed)
			TextFields:      []string{"title", "content", "description", "summary", "text", "name"},
			RetentionWindow: 24 * time.Hour,
			CacheBucket:     "EMBEDDINGS_CACHE",
		},
		ProcessTimeout: 5 * time.Second,
		QueryTimeout:   2 * time.Second,
		Buckets: BucketsConfig{
			EntityStates: "ENTITY_STATES",
			Predicate:    "PREDICATE_INDEX",
			Incoming:     "INCOMING_INDEX",
			Alias:        "ALIAS_INDEX",
			Spatial:      "SPATIAL_INDEX",
			Temporal:     "TEMPORAL_INDEX",
		},
		HealthCheck: HealthCheckConfig{
			Interval:   30 * time.Second,
			MaxLag:     5 * time.Second,
			MaxBacklog: 1000,
			MaxErrors:  10,
		},
	}
}

// Validate checks if the configuration is valid and returns validation errors
func (c *Config) Validate() error {
	// Validate event buffer
	if err := c.validateEventBuffer(); err != nil {
		return err
	}

	// Validate deduplication
	if err := c.validateDeduplication(); err != nil {
		return err
	}

	// Validate batch processing
	if err := c.validateBatchProcessing(); err != nil {
		return err
	}

	// Validate worker pool
	if err := c.validateWorkers(); err != nil {
		return err
	}

	// Validate timeouts
	if err := c.validateTimeouts(); err != nil {
		return err
	}

	// Validate at least one index is enabled
	if err := c.validateIndexes(); err != nil {
		return err
	}

	// Validate bucket names
	if err := c.validateBuckets(); err != nil {
		return err
	}

	// Validate health check
	return c.validateHealthCheck()
}

// validateEventBuffer validates event buffer configuration
func (c *Config) validateEventBuffer() error {
	if c.EventBuffer.Capacity <= 0 {
		msg := fmt.Sprintf("event_buffer.capacity must be positive, got %d", c.EventBuffer.Capacity)
		return errors.WrapInvalid(errors.ErrInvalidConfig, "IndexEngine", "Validate", msg)
	}

	validOverflowPolicies := []string{"drop_oldest", "drop_newest", "block"}
	validPolicy := false
	for _, policy := range validOverflowPolicies {
		if c.EventBuffer.OverflowPolicy == policy {
			validPolicy = true
			break
		}
	}
	if !validPolicy {
		msg := fmt.Sprintf(
			"event_buffer.overflow_policy must be one of %v, got %s",
			validOverflowPolicies,
			c.EventBuffer.OverflowPolicy,
		)
		return errors.WrapInvalid(errors.ErrInvalidConfig, "IndexEngine", "Validate", msg)
	}

	return nil
}

// validateDeduplication validates deduplication configuration
func (c *Config) validateDeduplication() error {
	if c.Deduplication.Enabled {
		if c.Deduplication.Window <= 0 {
			msg := fmt.Sprintf(
				"deduplication.window must be positive when enabled, got %v",
				c.Deduplication.Window,
			)
			return errors.WrapInvalid(errors.ErrInvalidConfig, "IndexEngine", "Validate", msg)
		}
		if c.Deduplication.CacheSize <= 0 {
			msg := fmt.Sprintf(
				"deduplication.cache_size must be positive when enabled, got %d",
				c.Deduplication.CacheSize,
			)
			return errors.WrapInvalid(errors.ErrInvalidConfig, "IndexEngine", "Validate", msg)
		}
		if c.Deduplication.TTL <= 0 {
			msg := fmt.Sprintf(
				"deduplication.ttl must be positive when enabled, got %v",
				c.Deduplication.TTL,
			)
			return errors.WrapInvalid(errors.ErrInvalidConfig, "IndexEngine", "Validate", msg)
		}
	}
	return nil
}

// validateBatchProcessing validates batch processing configuration
func (c *Config) validateBatchProcessing() error {
	if c.BatchProcessing.Size <= 0 {
		msg := fmt.Sprintf("batch_processing.size must be positive, got %d", c.BatchProcessing.Size)
		return errors.WrapInvalid(errors.ErrInvalidConfig, "IndexEngine", "Validate", msg)
	}
	if c.BatchProcessing.Interval <= 0 {
		msg := fmt.Sprintf(
			"batch_processing.interval must be positive, got %v",
			c.BatchProcessing.Interval,
		)
		return errors.WrapInvalid(errors.ErrInvalidConfig, "IndexEngine", "Validate", msg)
	}
	return nil
}

// validateWorkers validates worker pool configuration
func (c *Config) validateWorkers() error {
	if c.Workers <= 0 {
		msg := fmt.Sprintf("workers must be positive, got %d", c.Workers)
		return errors.WrapInvalid(errors.ErrInvalidConfig, "IndexEngine", "Validate", msg)
	}
	if c.Workers > 100 {
		msg := fmt.Sprintf(
			"workers should not exceed 100 for performance reasons, got %d",
			c.Workers,
		)
		return errors.WrapInvalid(errors.ErrInvalidConfig, "IndexEngine", "Validate", msg)
	}
	return nil
}

// validateTimeouts validates timeout configuration
func (c *Config) validateTimeouts() error {
	if c.ProcessTimeout <= 0 {
		msg := fmt.Sprintf("process_timeout must be positive, got %v", c.ProcessTimeout)
		return errors.WrapInvalid(errors.ErrInvalidConfig, "IndexEngine", "Validate", msg)
	}
	if c.QueryTimeout <= 0 {
		msg := fmt.Sprintf("query_timeout must be positive, got %v", c.QueryTimeout)
		return errors.WrapInvalid(errors.ErrInvalidConfig, "IndexEngine", "Validate", msg)
	}
	return nil
}

// validateIndexes validates that at least one index is enabled
func (c *Config) validateIndexes() error {
	if !c.Indexes.Predicate && !c.Indexes.Incoming && !c.Indexes.Alias &&
		!c.Indexes.Spatial && !c.Indexes.Temporal {
		msg := "at least one index type must be enabled"
		return errors.WrapInvalid(errors.ErrInvalidConfig, "IndexEngine", "Validate", msg)
	}
	return nil
}

// validateHealthCheck validates health check configuration
func (c *Config) validateHealthCheck() error {
	if c.HealthCheck.Interval <= 0 {
		msg := fmt.Sprintf("health_check.interval must be positive, got %v", c.HealthCheck.Interval)
		return errors.WrapInvalid(errors.ErrInvalidConfig, "IndexEngine", "Validate", msg)
	}
	if c.HealthCheck.MaxLag <= 0 {
		msg := fmt.Sprintf("health_check.max_lag must be positive, got %v", c.HealthCheck.MaxLag)
		return errors.WrapInvalid(errors.ErrInvalidConfig, "IndexEngine", "Validate", msg)
	}
	if c.HealthCheck.MaxBacklog <= 0 {
		msg := fmt.Sprintf(
			"health_check.max_backlog must be positive, got %d",
			c.HealthCheck.MaxBacklog,
		)
		return errors.WrapInvalid(errors.ErrInvalidConfig, "IndexEngine", "Validate", msg)
	}
	if c.HealthCheck.MaxErrors <= 0 {
		msg := fmt.Sprintf("health_check.max_errors must be positive, got %d", c.HealthCheck.MaxErrors)
		return errors.WrapInvalid(errors.ErrInvalidConfig, "IndexEngine", "Validate", msg)
	}
	return nil
}

// validateBuckets checks that all bucket names are valid
func (c *Config) validateBuckets() error {
	buckets := map[string]string{
		"entity_states": c.Buckets.EntityStates,
		"predicate":     c.Buckets.Predicate,
		"incoming":      c.Buckets.Incoming,
		"alias":         c.Buckets.Alias,
		"spatial":       c.Buckets.Spatial,
		"temporal":      c.Buckets.Temporal,
	}

	for name, bucket := range buckets {
		if bucket == "" {
			msg := fmt.Sprintf("bucket name for %s cannot be empty", name)
			return errors.WrapInvalid(errors.ErrInvalidConfig, "IndexEngine", "validateBucketConfig", msg)
		}
		if len(bucket) > 64 {
			msg := fmt.Sprintf(
				"bucket name for %s is too long (max 64 chars): %s",
				name,
				bucket,
			)
			return errors.WrapInvalid(errors.ErrInvalidConfig, "IndexEngine", "validateBucketConfig", msg)
		}
	}

	// Check for duplicate bucket names
	seen := make(map[string]string)
	for name, bucket := range buckets {
		if existing, exists := seen[bucket]; exists {
			msg := fmt.Sprintf(
				"duplicate bucket name '%s' used for both %s and %s",
				bucket,
				name,
				existing,
			)
			return errors.WrapInvalid(errors.ErrInvalidConfig, "IndexEngine", "validateBucketConfig", msg)
		}
		seen[bucket] = name
	}

	return nil
}

// ApplyDefaults applies default values to unset fields
func (c *Config) ApplyDefaults() {
	defaults := DefaultConfig()

	// Apply buffer defaults
	if c.EventBuffer.Capacity == 0 {
		c.EventBuffer.Capacity = defaults.EventBuffer.Capacity
	}
	if c.EventBuffer.OverflowPolicy == "" {
		c.EventBuffer.OverflowPolicy = defaults.EventBuffer.OverflowPolicy
	}

	// Apply deduplication defaults
	if c.Deduplication.Window == 0 {
		c.Deduplication.Window = defaults.Deduplication.Window
	}
	if c.Deduplication.CacheSize == 0 {
		c.Deduplication.CacheSize = defaults.Deduplication.CacheSize
	}
	if c.Deduplication.TTL == 0 {
		c.Deduplication.TTL = defaults.Deduplication.TTL
	}

	// Apply batch processing defaults
	if c.BatchProcessing.Size == 0 {
		c.BatchProcessing.Size = defaults.BatchProcessing.Size
	}
	if c.BatchProcessing.Interval == 0 {
		c.BatchProcessing.Interval = defaults.BatchProcessing.Interval
	}

	// Apply worker defaults
	if c.Workers == 0 {
		c.Workers = defaults.Workers
	}

	// Apply timeout defaults
	if c.ProcessTimeout == 0 {
		c.ProcessTimeout = defaults.ProcessTimeout
	}
	if c.QueryTimeout == 0 {
		c.QueryTimeout = defaults.QueryTimeout
	}

	// Apply bucket name defaults
	if c.Buckets.EntityStates == "" {
		c.Buckets.EntityStates = defaults.Buckets.EntityStates
	}
	if c.Buckets.Predicate == "" {
		c.Buckets.Predicate = defaults.Buckets.Predicate
	}
	if c.Buckets.Incoming == "" {
		c.Buckets.Incoming = defaults.Buckets.Incoming
	}
	if c.Buckets.Alias == "" {
		c.Buckets.Alias = defaults.Buckets.Alias
	}
	if c.Buckets.Spatial == "" {
		c.Buckets.Spatial = defaults.Buckets.Spatial
	}
	if c.Buckets.Temporal == "" {
		c.Buckets.Temporal = defaults.Buckets.Temporal
	}

	// Apply embedding defaults
	if c.Embedding.RetentionWindow == 0 {
		c.Embedding.RetentionWindow = defaults.Embedding.RetentionWindow
	}

	// Apply health check defaults
	if c.HealthCheck.Interval == 0 {
		c.HealthCheck.Interval = defaults.HealthCheck.Interval
	}
	if c.HealthCheck.MaxLag == 0 {
		c.HealthCheck.MaxLag = defaults.HealthCheck.MaxLag
	}
	if c.HealthCheck.MaxBacklog == 0 {
		c.HealthCheck.MaxBacklog = defaults.HealthCheck.MaxBacklog
	}
	if c.HealthCheck.MaxErrors == 0 {
		c.HealthCheck.MaxErrors = defaults.HealthCheck.MaxErrors
	}
}

// GetEnabledIndexes returns a list of enabled index types
func (c *Config) GetEnabledIndexes() []string {
	var enabled []string
	if c.Indexes.Predicate {
		enabled = append(enabled, "predicate")
	}
	if c.Indexes.Incoming {
		enabled = append(enabled, "incoming")
	}
	if c.Indexes.Alias {
		enabled = append(enabled, "alias")
	}
	if c.Indexes.Spatial {
		enabled = append(enabled, "spatial")
	}
	if c.Indexes.Temporal {
		enabled = append(enabled, "temporal")
	}
	return enabled
}

// IsIndexEnabled checks if a specific index type is enabled
func (c *Config) IsIndexEnabled(indexType string) bool {
	switch indexType {
	case "predicate":
		return c.Indexes.Predicate
	case "incoming":
		return c.Indexes.Incoming
	case "alias":
		return c.Indexes.Alias
	case "spatial":
		return c.Indexes.Spatial
	case "temporal":
		return c.Indexes.Temporal
	default:
		return false
	}
}
