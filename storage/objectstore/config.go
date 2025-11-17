// Package objectstore provides a NATS ObjectStore-based storage component
// for immutable message storage with time-bucketed keys and caching.
package objectstore

import (
	"github.com/c360/semstreams/component"
	"github.com/c360/semstreams/pkg/cache"
	"github.com/c360/semstreams/storage"
)

// Config holds configuration for ObjectStore storage component.
//
// Design Philosophy: Composition-Friendly
// - No hardcoded interface requirements (SemStreams can layer semantic interfaces)
// - Pluggable key generation (via storage.KeyGenerator interface)
// - Pluggable metadata extraction (via storage.MetadataExtractor interface)
// - Flexible port configuration
type Config struct {
	// Ports defines input/output port configuration
	Ports *component.PortConfig `json:"ports" schema:"type:ports,description:Port configuration for inputs and outputs,category:basic"`

	// BucketName is the NATS JetStream ObjectStore bucket name
	BucketName string `json:"bucket_name" schema:"type:string,description:NATS ObjectStore bucket name,default:MESSAGES,category:basic"`

	// DataCache configures the in-memory cache for retrieved objects
	DataCache cache.Config `json:"data_cache" schema:"type:object,description:Cache configuration for stored objects,category:performance"`

	// KeyGenerator optionally provides custom key generation strategy.
	// If nil, the default time-based key generator is used.
	// This allows SemStreams to provide entity-based keys while keeping
	// StreamKit generic.
	KeyGenerator storage.KeyGenerator `json:"-" schema:"-"`

	// MetadataExtractor optionally provides custom metadata extraction.
	// If nil, no metadata is stored with objects.
	// This allows SemStreams to add semantic metadata (entity IDs, triples)
	// while keeping StreamKit generic.
	MetadataExtractor storage.MetadataExtractor `json:"-" schema:"-"`
}

// DefaultConfig returns the default configuration for ObjectStore.
// Creates a simple key-value store with:
//   - Generic input/output ports (no interface requirements)
//   - Time-based key generation
//   - No metadata extraction
//   - Default caching settings
func DefaultConfig() Config {
	inputDefs := []component.PortDefinition{
		{
			Name:        "write",
			Type:        "nats",
			Subject:     "storage.objectstore.write",
			Interface:   "", // No interface requirement - accepts any message
			Required:    false,
			Description: "NATS subject for write operations (accepts any message)",
		},
		{
			Name:        "api",
			Type:        "nats-request",
			Subject:     "storage.objectstore.api",
			Interface:   "", // Request/Response operations
			Required:    false,
			Description: "Request/Response API for synchronous operations",
		},
	}

	outputDefs := []component.PortDefinition{
		{
			Name:        "events",
			Type:        "nats",
			Subject:     "storage.objectstore.events",
			Interface:   "", // Generic storage events
			Required:    false,
			Description: "Storage events (stored, retrieved)",
		},
	}

	return Config{
		Ports: &component.PortConfig{
			Inputs:  inputDefs,
			Outputs: outputDefs,
		},
		BucketName: "MESSAGES",
		DataCache: cache.Config{
			Enabled: true,
			MaxSize: 1000,
			TTL:     300, // 5 minutes
		},
		KeyGenerator:      nil, // Use default time-based generator
		MetadataExtractor: nil, // No metadata extraction
	}
}
