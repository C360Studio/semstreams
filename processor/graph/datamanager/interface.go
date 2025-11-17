// Package datamanager consolidates entity and edge operations into a unified data management service.
// This package is the result of Phase 1 consolidation, merging EntityStore and EdgeManager
// to provide atomic entity+edge operations and simplified transaction management.
//
// The DataManager is the single writer to ENTITY_STATES KV bucket and handles all
// entity persistence, edge management, and maintains L1/L2 cache hierarchies.
package datamanager

import (
	"context"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	gtypes "github.com/c360/semstreams/graph"
	"github.com/c360/semstreams/metric"
)

// DataHandler provides unified entity and edge management.
// This is the ONLY service that writes to ENTITY_STATES KV bucket.
// All change propagation happens via NATS KV Watch, not event publishing.
type DataHandler interface {
	// Lifecycle management
	Run(ctx context.Context) error

	// Entity CRUD operations (from EntityStore)
	CreateEntity(ctx context.Context, entity *gtypes.EntityState) (*gtypes.EntityState, error)
	UpdateEntity(ctx context.Context, entity *gtypes.EntityState) (*gtypes.EntityState, error)
	DeleteEntity(ctx context.Context, id string) error
	GetEntity(ctx context.Context, id string) (*gtypes.EntityState, error)
	ExistsEntity(ctx context.Context, id string) (bool, error)

	// Atomic entity+edge operations (NEW)
	CreateEntityWithEdges(
		ctx context.Context,
		entity *gtypes.EntityState,
		edges []gtypes.Edge,
	) (*gtypes.EntityState, error)
	UpdateEntityWithEdges(
		ctx context.Context,
		entity *gtypes.EntityState,
		addEdges []gtypes.Edge,
		removeEdges []string,
	) (*gtypes.EntityState, error)

	// Edge operations (from EdgeManager)
	AddEdge(ctx context.Context, fromEntityID string, edge gtypes.Edge) error
	RemoveEdge(ctx context.Context, fromEntityID, toEntityID, edgeType string) error

	// Relationship management
	CreateRelationship(
		ctx context.Context,
		fromEntityID, toEntityID string,
		edgeType string,
		properties map[string]any,
	) error
	DeleteRelationship(ctx context.Context, fromEntityID, toEntityID string) error

	// Batch operations for efficiency
	BatchWrite(ctx context.Context, writes []EntityWrite) error
	BatchGet(ctx context.Context, ids []string) ([]*gtypes.EntityState, error)

	// List operations
	List(ctx context.Context, pattern string) ([]string, error)

	// Cleanup and maintenance
	CleanupIncomingReferences(ctx context.Context, deletedEntityID string, outgoingEdges []gtypes.Edge) error

	// Edge queries and consistency checks
	CheckOutgoingEdgesConsistency(
		ctx context.Context,
		entityID string,
		entity *gtypes.EntityState,
		status *EntityIndexStatus,
	)
	HasEdgeToEntity(entity *gtypes.EntityState, targetEntityID string) bool

	// Cache statistics
	GetCacheStats() CacheStats

	// Synchronization support for testing and graceful operations
	FlushPendingWrites(ctx context.Context) error
	GetPendingWriteCount() int
}

// Dependencies defines all dependencies needed by DataManager
type Dependencies struct {
	KVBucket        jetstream.KeyValue      // NATS KV bucket for persistence
	MetricsRegistry *metric.MetricsRegistry // Framework metrics registry
	Logger          *slog.Logger            // Structured logging
	Config          Config                  // Configuration
}

// No interfaces needed - we use concrete types from the framework

// EntityWrite represents a buffered write operation
type EntityWrite struct {
	Operation Operation           // create|update|delete
	Entity    *gtypes.EntityState // Entity data (nil for delete)
	Edges     []gtypes.Edge       // Edges to add (for create/update)
	Callback  func(error)         // Optional completion callback
	RequestID string              // Optional request ID for tracing
	Timestamp time.Time           // When request was created
}

// Operation represents the type of entity operation
type Operation string

const (
	// OperationCreate represents creating a new entity.
	OperationCreate Operation = "create"
	// OperationUpdate represents updating an existing entity.
	OperationUpdate Operation = "update"
	// OperationDelete represents deleting an entity.
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

// WriteResult represents the result of a write operation
type WriteResult struct {
	EntityID string              // ID of the entity written
	Version  int64               // Final version after write
	Created  bool                // Whether entity was created (vs updated)
	Entity   *gtypes.EntityState // Final entity state
	Error    error               // Error if operation failed
}

// BatchWriteResult represents the result of a batch write operation
type BatchWriteResult struct {
	Results   []WriteResult // Individual write results
	Succeeded int           // Number of successful writes
	Failed    int           // Number of failed writes
	Coalesced int           // Number of writes that were coalesced
	Duration  time.Duration // Total operation duration
}
