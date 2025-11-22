// Package datamanager consolidates entity and edge operations into a unified data management service.
package datamanager

import (
	"log/slog"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	gtypes "github.com/c360/semstreams/graph"
	"github.com/c360/semstreams/metric"
)

// Dependencies defines all dependencies needed by DataManager
type Dependencies struct {
	KVBucket        jetstream.KeyValue      // NATS KV bucket for persistence
	MetricsRegistry *metric.MetricsRegistry // Framework metrics registry
	Logger          *slog.Logger            // Structured logging
	Config          Config                  // Configuration
}

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
