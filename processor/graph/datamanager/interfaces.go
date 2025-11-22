// Package datamanager consolidates entity and edge operations into a unified data management service.
// This package is the result of Phase 1 consolidation, merging EntityStore and EdgeManager
// to provide atomic entity+edge operations and simplified transaction management.
//
// The DataManager is the single writer to ENTITY_STATES KV bucket and handles all
// entity persistence, edge management, and maintains L1/L2 cache hierarchies.
//
// The package now follows Interface Segregation Principle with 6 focused interfaces:
// - EntityReader: Read-only entity access with caching
// - EntityWriter: Basic entity mutation operations
// - EntityManager: Complete entity lifecycle management
// - EdgeManager: Graph relationship operations
// - DataConsistency: Graph integrity checking and maintenance
// - DataLifecycle: Component lifecycle and observability
package datamanager

import (
	"context"

	gtypes "github.com/c360/semstreams/graph"
)

// EntityReader provides read-only entity access with caching.
// Used by components that only need to query entities (e.g., QueryManager).
type EntityReader interface {
	GetEntity(ctx context.Context, id string) (*gtypes.EntityState, error)
	ExistsEntity(ctx context.Context, id string) (bool, error)
	BatchGet(ctx context.Context, ids []string) ([]*gtypes.EntityState, error)
}

// EntityWriter provides basic entity mutation operations.
// Used by components that need simple CRUD operations without edge management.
type EntityWriter interface {
	CreateEntity(ctx context.Context, entity *gtypes.EntityState) (*gtypes.EntityState, error)
	UpdateEntity(ctx context.Context, entity *gtypes.EntityState) (*gtypes.EntityState, error)
	DeleteEntity(ctx context.Context, id string) error
}

// EntityManager provides complete entity lifecycle management.
// Combines reader, writer, and advanced operations like atomic entity+edge writes.
type EntityManager interface {
	EntityReader
	EntityWriter

	CreateEntityWithEdges(ctx context.Context, entity *gtypes.EntityState, edges []gtypes.Edge) (*gtypes.EntityState, error)
	UpdateEntityWithEdges(ctx context.Context, entity *gtypes.EntityState, addEdges []gtypes.Edge, removeEdges []string) (*gtypes.EntityState, error)
	BatchWrite(ctx context.Context, writes []EntityWrite) error
	List(ctx context.Context, pattern string) ([]string, error)
}

// EdgeManager provides graph relationship operations.
// Used by components that manage entity relationships and graph structure.
type EdgeManager interface {
	AddEdge(ctx context.Context, fromEntityID string, edge gtypes.Edge) error
	RemoveEdge(ctx context.Context, fromEntityID, toEntityID, edgeType string) error
	CreateRelationship(ctx context.Context, fromEntityID, toEntityID string, edgeType string, properties map[string]any) error
	DeleteRelationship(ctx context.Context, fromEntityID, toEntityID string) error
}

// DataConsistency provides graph integrity checking and maintenance.
// Used by components that need to verify or repair graph consistency.
type DataConsistency interface {
	CleanupIncomingReferences(ctx context.Context, deletedEntityID string, outgoingEdges []gtypes.Edge) error
	CheckOutgoingEdgesConsistency(ctx context.Context, entityID string, entity *gtypes.EntityState, status *EntityIndexStatus)
	HasEdgeToEntity(entity *gtypes.EntityState, targetEntityID string) bool
}

// DataLifecycle manages component lifecycle and observability.
// Used by the main processor to start/stop the manager and monitor health.
type DataLifecycle interface {
	Run(ctx context.Context) error
	FlushPendingWrites(ctx context.Context) error
	GetPendingWriteCount() int
	GetCacheStats() CacheStats
}

// Compile-time verification that Manager implements all interfaces
var (
	_ EntityReader    = (*Manager)(nil)
	_ EntityWriter    = (*Manager)(nil)
	_ EntityManager   = (*Manager)(nil)
	_ EdgeManager     = (*Manager)(nil)
	_ DataConsistency = (*Manager)(nil)
	_ DataLifecycle   = (*Manager)(nil)
)
