package message

// Graphable enables messages to self-declare their domain entities and relationships.
// This interface eliminates the need for brittle string matching in entity extraction
// by allowing payloads to explicitly state what entities they contain and how those
// entities relate to each other.
//
// The Graphable pattern addresses a core architectural requirement: messages
// should contain domain expertise about the entities they represent. Rather than
// having infrastructure code guess what entities exist based on message types or
// field names, the message payload itself declares this information.
//
// Design Benefits:
//
//   - Domain Expertise: Payloads contain knowledge about their entities
//   - Type Safety: No string matching or reflection-based entity detection
//   - Extensibility: New domains simply implement the interface
//   - Relationships: Explicit declaration of entity relationships
//   - Performance: No need for complex entity detection algorithms
//   - Resolution Guidance: Provides hints for entity resolution and merging
//
// Triple-Based Design:
//
// The Graphable interface uses a Triple-based approach where payloads return
// RDF-like triples (subject, predicate, object) to describe entity properties
// and relationships. This provides maximum flexibility while remaining simple.
//
// Example Implementation:
//
//	type PositionPayload struct {
//	    SystemID  uint8   `json:"system_id"`
//	    Latitude  float64 `json:"latitude"`
//	    Longitude float64 `json:"longitude"`
//	    Altitude  float32 `json:"altitude"`
//	}
//
//	func (p *PositionPayload) EntityID() string {
//	    // Return deterministic 6-part ID
//	    return fmt.Sprintf("acme.telemetry.robotics.mavlink.drone.%d", p.SystemID)
//	}
//
//	func (p *PositionPayload) Triples() []Triple {
//	    entityID := p.EntityID()
//	    return []Triple{
//	        {Subject: entityID, Predicate: "rdf:type", Object: "robotics:Drone"},
//	        {Subject: entityID, Predicate: "geo:latitude", Object: p.Latitude},
//	        {Subject: entityID, Predicate: "geo:longitude", Object: p.Longitude},
//	        {Subject: entityID, Predicate: "geo:altitude", Object: p.Altitude},
//	    }
//	}
//
// Graphable provides entity identification and semantic triples
type Graphable interface {
	// EntityID returns deterministic 6-part ID: org.platform.domain.system.type.instance
	EntityID() string

	// Triples returns all facts about this entity
	Triples() []Triple
}
