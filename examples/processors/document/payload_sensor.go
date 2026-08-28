package document

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
)

// SensorDocument represents rich-text documentation for a sensor. It implements ContentStorable.
type SensorDocument struct {
	// Input fields
	ID          string   `json:"id"`          // e.g., "sensor-doc-001"
	Title       string   `json:"title"`       // e.g., "Temperature Sensor T-42"
	Description string   `json:"description"` // Sensor description
	Body        string   `json:"body"`        // Detailed documentation (stored in ObjectStore)
	Location    string   `json:"location"`    // Physical location description
	Reading     float64  `json:"reading"`     // Current/reference reading
	Unit        string   `json:"unit"`        // Unit of measurement
	Category    string   `json:"category"`    // temperature, pressure, humidity
	Tags        []string `json:"tags"`

	// Context fields (set by processor from config, preserved through JSON for NATS transport)
	OrgID    string `json:"org_id,omitempty"`
	Platform string `json:"platform,omitempty"`

	// Storage reference (set by processor)
	storageRef *message.StorageReference `json:"-"`
}

// EntityID returns a federated entity ID for the sensor document.
// Example: "acme.logistics.document.sensor.temperature.sensor-doc-001"
func (s *SensorDocument) EntityID() string {
	category := s.Category
	if category == "" {
		category = "general"
	}
	return fmt.Sprintf("%s.%s.document.sensor.%s.%s",
		s.OrgID,
		s.Platform,
		category,
		s.ID,
	)
}

// Triples returns METADATA ONLY facts about this sensor document.
// Body content is stored in ObjectStore, NOT in triples.
func (s *SensorDocument) Triples() []message.Triple {
	entityID := s.EntityID()
	now := time.Now()

	triples := []message.Triple{
		// Dublin Core: Title
		{
			Subject:    entityID,
			Predicate:  PredicateDCTitle,
			Object:     s.Title,
			Source:     tripleSourceName,
			Timestamp:  now,
			Confidence: defaultConfidence,
		},
		// Dublin Core: Type
		{
			Subject:    entityID,
			Predicate:  PredicateDCType,
			Object:     "sensor_doc",
			Source:     tripleSourceName,
			Timestamp:  now,
			Confidence: defaultConfidence,
		},
	}

	// Sensor location
	if s.Location != "" {
		triples = append(triples, message.Triple{
			Subject:    entityID,
			Predicate:  PredicateSensorLocation,
			Object:     s.Location,
			Source:     tripleSourceName,
			Timestamp:  now,
			Confidence: defaultConfidence,
		})
	}

	// Sensor reading
	if s.Reading != 0 {
		triples = append(triples, message.Triple{
			Subject:    entityID,
			Predicate:  PredicateSensorReading,
			Object:     s.Reading,
			Source:     tripleSourceName,
			Timestamp:  now,
			Confidence: defaultConfidence,
		})
	}

	// Sensor unit
	if s.Unit != "" {
		triples = append(triples, message.Triple{
			Subject:    entityID,
			Predicate:  PredicateSensorUnit,
			Object:     s.Unit,
			Source:     tripleSourceName,
			Timestamp:  now,
			Confidence: defaultConfidence,
		})
	}

	// Dublin Core: Subject (category)
	if s.Category != "" {
		triples = append(triples, message.Triple{
			Subject:    entityID,
			Predicate:  PredicateDCSubject,
			Object:     s.Category,
			Source:     tripleSourceName,
			Timestamp:  now,
			Confidence: defaultConfidence,
		})
	}

	// Tags
	for _, tag := range s.Tags {
		triples = append(triples, message.Triple{
			Subject:    entityID,
			Predicate:  PredicateContentTag,
			Object:     tag,
			Source:     tripleSourceName,
			Timestamp:  now,
			Confidence: defaultConfidence,
		})
	}

	// NOTE: Body and Description are NOT in triples - stored in ObjectStore

	return triples
}

// StorageRef implements message.Storable interface.
func (s *SensorDocument) StorageRef() *message.StorageReference {
	return s.storageRef
}

// SetStorageRef is called by processor after storing content.
func (s *SensorDocument) SetStorageRef(ref *message.StorageReference) {
	s.storageRef = ref
}

// ContentFields implements message.ContentStorable interface.
func (s *SensorDocument) ContentFields() map[string]string {
	fields := map[string]string{
		message.ContentRoleTitle: "title",
	}
	if s.Body != "" {
		fields[message.ContentRoleBody] = "body"
	}
	if s.Description != "" {
		fields[message.ContentRoleAbstract] = "description"
	}
	return fields
}

// RawContent implements message.ContentStorable interface.
func (s *SensorDocument) RawContent() map[string]string {
	content := map[string]string{
		"title": s.Title,
	}
	if s.Body != "" {
		content["body"] = s.Body
	}
	if s.Description != "" {
		content["description"] = s.Description
	}
	if s.Location != "" {
		content["location"] = s.Location
	}
	return content
}

// Schema returns the message type for sensor documents.
func (s *SensorDocument) Schema() message.Type {
	return message.Type{
		Domain:   "content",
		Category: "sensor_doc",
		Version:  "v1",
	}
}

// Validate checks required fields.
func (s *SensorDocument) Validate() error {
	if s.ID == "" {
		return fmt.Errorf("id is required")
	}
	if s.Title == "" {
		return fmt.Errorf("title is required")
	}
	if s.OrgID == "" {
		return fmt.Errorf("org_id is required (set by processor)")
	}
	if s.Platform == "" {
		return fmt.Errorf("platform is required (set by processor)")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (s *SensorDocument) MarshalJSON() ([]byte, error) {
	type Alias SensorDocument
	return json.Marshal((*Alias)(s))
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *SensorDocument) UnmarshalJSON(data []byte) error {
	type Alias SensorDocument
	return json.Unmarshal(data, (*Alias)(s))
}

// Compile-time interface checks
var (
	_ graph.Graphable         = (*SensorDocument)(nil)
	_ message.ContentStorable = (*SensorDocument)(nil)
	_ message.Payload         = (*SensorDocument)(nil)
)
