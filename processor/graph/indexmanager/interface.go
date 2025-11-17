// Package indexmanager provides the Indexer interface and IndexManager implementation.
// This service is responsible for maintaining secondary indexes by watching ENTITY_STATES KV
// for changes and updating indexes asynchronously with event buffering and deduplication.
//
// The IndexManager watches KV changes instead of consuming events directly,
// enabling eventual consistency and decoupled processing.
package indexmanager

import (
	"context"
	"time"

	gtypes "github.com/c360/semstreams/graph"
)

// Indexer watches KV for entity changes and maintains indexes.
// It provides both update and query operations for all secondary indexes.
type Indexer interface {
	// Lifecycle - Run blocks until error or context done
	Run(ctx context.Context) error

	// Index update operations (write operations - kept for special operations like bulk imports)
	UpdatePredicateIndex(ctx context.Context, entityID string, entityState interface{}) error
	UpdateSpatialIndex(ctx context.Context, entityID string, position interface{}) error
	UpdateTemporalIndex(ctx context.Context, entityID string, entityState *gtypes.EntityState) error
	UpdateIncomingIndex(ctx context.Context, targetEntityID, sourceEntityID string) error
	RemoveFromIncomingIndex(ctx context.Context, targetEntityID, sourceEntityID string) error
	UpdateAliasIndex(ctx context.Context, alias, entityID string) error

	// Index deletion operations
	DeleteFromIndexes(ctx context.Context, entityID string) error
	DeleteFromPredicateIndex(ctx context.Context, entityID string) error
	DeleteFromSpatialIndex(ctx context.Context, entityID string) error
	DeleteFromTemporalIndex(ctx context.Context, entityID string) error
	DeleteFromIncomingIndex(ctx context.Context, entityID string) error
	DeleteFromAliasIndex(ctx context.Context, alias string) error

	// Simple index Gets (single KV lookup operations)
	GetPredicateIndex(ctx context.Context, predicate string) ([]string, error)
	GetIncomingRelationships(ctx context.Context, targetEntityID string) ([]Relationship, error)
	ResolveAlias(ctx context.Context, alias string) (string, error)

	// Complex queries (multiple KV lookups)
	QuerySpatial(ctx context.Context, bounds Bounds) ([]string, error)
	QueryTemporal(ctx context.Context, start, end time.Time) ([]string, error)

	// Batch operations for efficiency
	GetPredicateIndexes(ctx context.Context, predicates []string) (map[string][]string, error)
	ResolveAliases(ctx context.Context, aliases []string) (map[string]string, error)

	// Semantic search operations (requires embedding configuration)
	SearchSemantic(ctx context.Context, query string, opts *SemanticSearchOptions) (*SearchResults, error)
	SearchHybrid(ctx context.Context, query *HybridQuery) (*SearchResults, error)

	// Metrics
	GetBacklog() int
	GetDeduplicationStats() DeduplicationStats
}

// Bounds represents spatial query bounds
type Bounds struct {
	North float64 `json:"north"`
	South float64 `json:"south"`
	East  float64 `json:"east"`
	West  float64 `json:"west"`
}

// Relationship represents an incoming relationship
type Relationship struct {
	FromEntityID string                 `json:"from_entity_id"`
	EdgeType     string                 `json:"edge_type"`
	Weight       float64                `json:"weight"`
	Properties   map[string]interface{} `json:"properties"`
	CreatedAt    time.Time              `json:"created_at"`
}

// DeduplicationStats provides metrics on event deduplication
type DeduplicationStats struct {
	TotalEvents       int64   `json:"total_events"`
	DuplicateEvents   int64   `json:"duplicate_events"`
	ProcessedEvents   int64   `json:"processed_events"`
	DeduplicationRate float64 `json:"deduplication_rate"`
	CacheSize         int     `json:"cache_size"`
	CacheHitRate      float64 `json:"cache_hit_rate"`
}

// EntityChange represents a KV change event to be processed
type EntityChange struct {
	Key       string    `json:"key"`       // Entity ID (KV key)
	Operation Operation `json:"operation"` // create|update|delete
	Value     []byte    `json:"value"`     // Entity state JSON (nil for delete)
	Revision  uint64    `json:"revision"`  // KV revision
	Timestamp time.Time `json:"timestamp"` // When change was detected
}

// Operation represents the type of KV operation detected
type Operation string

const (
	// OperationCreate represents a KV Put with revision=1.
	OperationCreate Operation = "create"
	// OperationUpdate represents a KV Put with revision>1.
	OperationUpdate Operation = "update"
	// OperationDelete represents a KV Delete operation.
	OperationDelete Operation = "delete"
)

// String returns the string representation of the operation
func (o Operation) String() string {
	return string(o)
}

// IsValid checks if the operation is valid
func (o Operation) IsValid() bool {
	switch o {
	case OperationCreate, OperationUpdate, OperationDelete:
		return true
	default:
		return false
	}
}
