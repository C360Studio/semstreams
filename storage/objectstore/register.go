package objectstore

import "github.com/c360/semstreams/component"

// Register registers the ObjectStore storage component with the given registry.
//
// The ObjectStore component provides:
//   - NATS JetStream ObjectStore-based immutable storage
//   - Time-bucketed key organization
//   - LRU/TTL/Hybrid caching support
//   - Pluggable key generation (composition-friendly)
//   - Pluggable metadata extraction (composition-friendly)
//   - Request/Response API for synchronous operations
//   - Fire-and-forget write operations
//   - Storage event publishing
//
// Composition-Friendly Design:
// The ObjectStore is designed to be easily extended by SemStreams or other
// semantic layers. The key generation and metadata extraction are pluggable,
// allowing semantic behavior to be layered on top of the core infrastructure.
func Register(registry *component.Registry) error {
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "objectstore",
		Factory:     NewComponent,
		Schema:      objectstoreSchema,
		Type:        "storage",
		Protocol:    "objectstore",
		Domain:      "storage",
		Description: "NATS ObjectStore component for immutable message storage",
		Version:     "1.0.0",
	})
}
