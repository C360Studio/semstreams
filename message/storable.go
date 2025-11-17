package message

// StorageReference points to where the full message data is stored.
// This enables lightweight message passing where components can reference
// stored data without transmitting the full payload.
//
// The StorageReference pattern supports the "store once, reference everywhere"
// architecture, reducing data duplication and enabling efficient processing
// of large messages.
//
// Example usage:
//   - ObjectStore stores full message and returns StorageReference
//   - GraphProcessor receives Storable with reference to full data
//   - Components can fetch full data only when needed
type StorageReference struct {
	// StorageInstance identifies which storage component holds the data.
	// This enables federation across multiple storage instances.
	// Examples: "message-store", "cache-1", "objectstore-primary"
	StorageInstance string `json:"storage_instance"`

	// Key is the storage-specific key to retrieve the data.
	// Format depends on the storage backend but typically includes
	// time-based partitioning for efficient retrieval.
	// Examples: "2025/01/13/14/msg_abc123", "robotics/drone/1/latest"
	Key string `json:"key"`

	// ContentType specifies the MIME type of the stored content.
	// This helps consumers understand how to process the data.
	// Examples: "application/json", "application/protobuf", "application/avro"
	ContentType string `json:"content_type"`

	// Size is an optional hint about the stored data size in bytes.
	// This helps consumers decide whether to fetch the full data.
	// A value of 0 indicates the size is unknown.
	Size int64 `json:"size,omitempty"`
}

// Storable extends Graphable with storage reference capability.
// Components that implement Storable can provide both semantic
// information (via Graphable) and a reference to their full data.
//
// This interface enables the lightweight message pattern where:
//  1. Domain processors create messages with semantic data
//  2. ObjectStore stores full message and adds StorageReference
//  3. GraphProcessor receives Storable with both semantics and reference
//  4. Consumers can access full data via StorageReference when needed
//
// Example implementation:
//
//	type StoredEntity struct {
//	    entityID string
//	    triples  []Triple
//	    storage  *StorageReference
//	}
//
//	func (s *StoredEntity) EntityID() string { return s.entityID }
//	func (s *StoredEntity) Triples() []Triple { return s.triples }
//	func (s *StoredEntity) StorageRef() *StorageReference { return s.storage }
type Storable interface {
	// Embeds Graphable for semantic capabilities
	Graphable

	// StorageRef returns reference to where full data is stored.
	// May return nil if data is not stored externally.
	StorageRef() *StorageReference
}
