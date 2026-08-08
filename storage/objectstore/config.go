// Package objectstore provides a NATS ObjectStore-based storage component
// for immutable message storage with time-bucketed keys and caching.
package objectstore

import (
	"log/slog"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/pkg/cache"
	"github.com/c360studio/semstreams/storage"
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

	// InstanceName is the storage COMPONENT instance name stamped into every
	// StorageReference.StorageInstance this store produces (gh#400). It is the
	// canonical handle a resolver (e.g. the fusion hydration deref helper,
	// ADR-062 #399) maps back to this store. Set by the objectstore Component
	// from its instance name; when empty (standalone Store callers) it defaults
	// to BucketName so the stamped instance is never empty. Internal wiring, not
	// operator-configured — hence schema-excluded like the pluggable generators.
	InstanceName string `json:"-" schema:"-"`

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

	// Logger, when set, receives the boot-time retention-reconcile WARN emitted by
	// the D2 content-store guard (ADR-068; #600) when it strips a binding MaxAge/
	// MaxBytes from the backing stream. Internal wiring — components thread their
	// own logger; standalone callers leave it nil and the guard falls back to
	// slog.Default(). Schema-excluded like the other injected dependencies.
	Logger *slog.Logger `json:"-" schema:"-"`
}

// Validate checks if the configuration is valid.
func (c Config) Validate() error {
	// BucketName is optional - defaults to MESSAGES
	// DataCache validation is handled by cache.Config.Validate() if called
	// KeyGenerator and MetadataExtractor are optional pluggable interfaces
	// Ports validation is handled by component.PortConfig if present
	return nil
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
			Name: "write", Config: component.NATSPort{Subject: "storage.objectstore.write"}, Required: false,
			Description: "NATS subject for write operations (accepts any message)",
		},
	}

	outputDefs := []component.PortDefinition{
		{
			Name: "events", Config: component.NATSPort{Subject: "storage.objectstore.events"}, Required: false,
			Description: "Storage events (stored, retrieved)",
		},
		{
			Name: "stored", Config: component.NATSPort{Subject: "storage.objectstore.stored", Interface: &component.InterfaceContract{
				// StoredMessage with StorageRef
				Type: "storage.stored.v1"}}, Required: false,
			Description: "StoredMessage output for ContentStorable pattern",
		},
	}

	return Config{
		Ports: &component.PortConfig{
			Inputs:  inputDefs,
			Outputs: outputDefs,
		},
		BucketName: "MESSAGES",
		DataCache: cache.Config{
			Enabled:         true,
			Strategy:        cache.StrategyLRU,
			MaxSize:         1000,
			TTL:             5 * time.Minute,
			CleanupInterval: 1 * time.Minute,
		},
		KeyGenerator:      nil, // Use default time-based generator
		MetadataExtractor: nil, // No metadata extraction
	}
}
