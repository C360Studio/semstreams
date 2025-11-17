// Package messagemanager provides the MessageHandler interface and Manager implementation
package messagemanager

import (
	"context"

	gtypes "github.com/c360/semstreams/graph"
	"github.com/c360/semstreams/processor/graph/datamanager"
)

// MessageHandler handles all message processing business logic
type MessageHandler interface {
	// ProcessMessage processes any message type into entity states
	ProcessMessage(ctx context.Context, msg any) ([]*gtypes.EntityState, error)

	// ProcessWork processes raw message data from worker pool
	ProcessWork(ctx context.Context, data []byte) error

	// SetIndexManager sets the index manager dependency (for circular dependency resolution)
	SetIndexManager(indexManager IndexManager)
}

// Dependencies defines all dependencies needed by message manager
type Dependencies struct {
	DataHandler  datamanager.DataHandler
	IndexManager IndexManager
	Logger       Logger
}

// IndexManager interface for index operations
type IndexManager interface {
	ResolveAlias(ctx context.Context, aliasOrID string) (string, error)
}

// Logger interface for logging
type Logger interface {
	Debug(msg string, keysAndValues ...any)
	Error(msg string, keysAndValues ...any)
	Warn(msg string, keysAndValues ...any)
}

// Config holds message manager configuration
type Config struct {
	DefaultNamespace string
	DefaultPlatform  string
}

// DefaultConfig returns default configuration
func DefaultConfig() Config {
	return Config{
		DefaultNamespace: "default",
		DefaultPlatform:  "semstreams",
	}
}
