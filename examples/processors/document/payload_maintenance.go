package document

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/c360studio/semstreams/types"
)

// Maintenance represents a maintenance record entity. It implements ContentStorable.
type Maintenance struct {
	// Input fields
	ID             string   `json:"id"`              // e.g., "maint-001"
	Title          string   `json:"title"`           // e.g., "Pump Repair"
	Description    string   `json:"description"`     // Work description
	Body           string   `json:"body"`            // Detailed work log (stored in ObjectStore)
	Technician     string   `json:"technician"`      // Who performed the work
	Status         string   `json:"status"`          // completed, pending, in_progress
	CompletionDate string   `json:"completion_date"` // ISO timestamp
	Category       string   `json:"category"`        // equipment, facility, etc.
	Tags           []string `json:"tags"`

	// EntityIDValue is this record's own identity, minted once by the
	// processor under the composition root's platform.org / platform.id and
	// carried on the wire from there (ADR-102 d2).
	EntityIDValue string `json:"entity_id"`

	// Storage reference (set by processor)
	storageRef *message.StorageReference `json:"-"`
}

// EntityID returns the identity minted for this maintenance record.
func (m *Maintenance) EntityID() string {
	return m.EntityIDValue
}

// MintMaintenanceEntityID mints a maintenance record's federated entity ID under
// the deployment authority (ADR-102 d2): system = work, domain = maintenance.
// Example: "acme.dep1.work.maintenance.completed.maint-001"
func MintMaintenanceEntityID(authority types.PlatformMeta, status, id string) string {
	if status == "" {
		status = defaultMaintenanceStatus
	}
	return semtypes.EntityID{
		Org:      authority.Org,
		Platform: authority.Platform,
		System:   workSystem,
		Domain:   maintenanceDomain,
		Type:     status,
		Instance: id,
	}.Key()
}

// Triples returns METADATA ONLY facts about this maintenance record.
// Body content is stored in ObjectStore, NOT in triples.
func (m *Maintenance) Triples() []message.Triple {
	entityID := m.EntityID()
	now := time.Now()

	triples := []message.Triple{
		// Dublin Core: Title
		{
			Subject:    entityID,
			Predicate:  PredicateDCTitle,
			Object:     m.Title,
			Source:     tripleSourceName,
			Timestamp:  now,
			Confidence: defaultConfidence,
		},
		// Dublin Core: Type
		{
			Subject:    entityID,
			Predicate:  PredicateDCType,
			Object:     "maintenance",
			Source:     tripleSourceName,
			Timestamp:  now,
			Confidence: defaultConfidence,
		},
		// Maintenance status
		{
			Subject:    entityID,
			Predicate:  PredicateMaintenanceStatus,
			Object:     m.Status,
			Source:     tripleSourceName,
			Timestamp:  now,
			Confidence: defaultConfidence,
		},
	}

	// Dublin Core: Creator (technician)
	if m.Technician != "" {
		triples = append(triples, message.Triple{
			Subject:    entityID,
			Predicate:  PredicateDCCreator,
			Object:     m.Technician,
			Source:     tripleSourceName,
			Timestamp:  now,
			Confidence: defaultConfidence,
		})
	}

	// Dublin Core: Date (completion date)
	if m.CompletionDate != "" {
		ts, err := time.Parse(time.RFC3339, m.CompletionDate)
		if err != nil {
			slog.Warn("invalid completion_date timestamp",
				"entity_id", entityID,
				"value", m.CompletionDate,
				"error", err)
		} else {
			triples = append(triples, message.Triple{
				Subject:    entityID,
				Predicate:  PredicateDCDate,
				Object:     ts,
				Source:     tripleSourceName,
				Timestamp:  now,
				Confidence: defaultConfidence,
			})
		}
	}

	// Dublin Core: Subject (category)
	if m.Category != "" {
		triples = append(triples, message.Triple{
			Subject:    entityID,
			Predicate:  PredicateDCSubject,
			Object:     m.Category,
			Source:     tripleSourceName,
			Timestamp:  now,
			Confidence: defaultConfidence,
		})
	}

	// Tags
	for _, tag := range m.Tags {
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
func (m *Maintenance) StorageRef() *message.StorageReference {
	return m.storageRef
}

// SetStorageRef is called by processor after storing content.
func (m *Maintenance) SetStorageRef(ref *message.StorageReference) {
	m.storageRef = ref
}

// ContentFields implements message.ContentStorable interface.
func (m *Maintenance) ContentFields() map[string]string {
	fields := map[string]string{
		message.ContentRoleTitle: "title",
	}
	if m.Body != "" {
		fields[message.ContentRoleBody] = "body"
	}
	if m.Description != "" {
		fields[message.ContentRoleAbstract] = "description"
	}
	return fields
}

// RawContent implements message.ContentStorable interface.
func (m *Maintenance) RawContent() map[string]string {
	content := map[string]string{
		"title": m.Title,
	}
	if m.Body != "" {
		content["body"] = m.Body
	}
	if m.Description != "" {
		content["description"] = m.Description
	}
	return content
}

// Schema returns the message type for maintenance records.
func (m *Maintenance) Schema() message.Type {
	return message.Type{
		Domain:   "content",
		Category: "maintenance",
		Version:  "v1",
	}
}

// Validate checks required fields.
func (m *Maintenance) Validate() error {
	if m.ID == "" {
		return fmt.Errorf("id is required")
	}
	if m.Title == "" {
		return fmt.Errorf("title is required")
	}
	if m.EntityIDValue == "" {
		return fmt.Errorf("entity_id is required; mint it with MintMaintenanceEntityID under the deployment authority")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (m *Maintenance) MarshalJSON() ([]byte, error) {
	type Alias Maintenance
	return json.Marshal((*Alias)(m))
}

// UnmarshalJSON implements json.Unmarshaler.
func (m *Maintenance) UnmarshalJSON(data []byte) error {
	type Alias Maintenance
	return json.Unmarshal(data, (*Alias)(m))
}

// Compile-time interface checks
var (
	_ graph.Graphable         = (*Maintenance)(nil)
	_ message.ContentStorable = (*Maintenance)(nil)
	_ message.Payload         = (*Maintenance)(nil)
)
