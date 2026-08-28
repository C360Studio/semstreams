package document

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
)

// Observation represents an observation or inspection record. It implements ContentStorable.
type Observation struct {
	// Input fields
	ID          string   `json:"id"`          // e.g., "obs-001"
	Title       string   `json:"title"`       // e.g., "Safety Hazard Report"
	Description string   `json:"description"` // What was observed
	Body        string   `json:"body"`        // Detailed notes (stored in ObjectStore)
	Observer    string   `json:"observer"`    // Who made the observation
	Severity    string   `json:"severity"`    // low, medium, high, critical
	ObservedAt  string   `json:"observed_at"` // ISO timestamp
	Category    string   `json:"category"`    // safety, quality, environment
	Tags        []string `json:"tags"`

	// Context fields (set by processor from config, preserved through JSON for NATS transport)
	OrgID    string `json:"org_id,omitempty"`
	Platform string `json:"platform,omitempty"`

	// Storage reference (set by processor)
	storageRef *message.StorageReference `json:"-"`
}

// EntityID returns a federated entity ID for the observation.
// Example: "acme.logistics.record.observation.high.obs-001"
func (o *Observation) EntityID() string {
	severity := o.Severity
	if severity == "" {
		severity = "medium"
	}
	return fmt.Sprintf("%s.%s.record.observation.%s.%s",
		o.OrgID,
		o.Platform,
		severity,
		o.ID,
	)
}

// Triples returns METADATA ONLY facts about this observation.
// Body content is stored in ObjectStore, NOT in triples.
func (o *Observation) Triples() []message.Triple {
	entityID := o.EntityID()
	now := time.Now()

	triples := []message.Triple{
		// Dublin Core: Title
		{
			Subject:    entityID,
			Predicate:  PredicateDCTitle,
			Object:     o.Title,
			Source:     tripleSourceName,
			Timestamp:  now,
			Confidence: defaultConfidence,
		},
		// Dublin Core: Type
		{
			Subject:    entityID,
			Predicate:  PredicateDCType,
			Object:     "observation",
			Source:     tripleSourceName,
			Timestamp:  now,
			Confidence: defaultConfidence,
		},
		// Observation severity
		{
			Subject:    entityID,
			Predicate:  PredicateObservationSeverity,
			Object:     o.Severity,
			Source:     tripleSourceName,
			Timestamp:  now,
			Confidence: defaultConfidence,
		},
	}

	// Dublin Core: Creator (observer)
	if o.Observer != "" {
		triples = append(triples, message.Triple{
			Subject:    entityID,
			Predicate:  PredicateDCCreator,
			Object:     o.Observer,
			Source:     tripleSourceName,
			Timestamp:  now,
			Confidence: defaultConfidence,
		})
	}

	// Dublin Core: Date (observed at)
	if o.ObservedAt != "" {
		ts, err := time.Parse(time.RFC3339, o.ObservedAt)
		if err != nil {
			slog.Warn("invalid observed_at timestamp",
				"entity_id", entityID,
				"value", o.ObservedAt,
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
	if o.Category != "" {
		triples = append(triples, message.Triple{
			Subject:    entityID,
			Predicate:  PredicateDCSubject,
			Object:     o.Category,
			Source:     tripleSourceName,
			Timestamp:  now,
			Confidence: defaultConfidence,
		})
	}

	// Tags
	for _, tag := range o.Tags {
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
func (o *Observation) StorageRef() *message.StorageReference {
	return o.storageRef
}

// SetStorageRef is called by processor after storing content.
func (o *Observation) SetStorageRef(ref *message.StorageReference) {
	o.storageRef = ref
}

// ContentFields implements message.ContentStorable interface.
func (o *Observation) ContentFields() map[string]string {
	fields := map[string]string{
		message.ContentRoleTitle: "title",
	}
	if o.Body != "" {
		fields[message.ContentRoleBody] = "body"
	}
	if o.Description != "" {
		fields[message.ContentRoleAbstract] = "description"
	}
	return fields
}

// RawContent implements message.ContentStorable interface.
func (o *Observation) RawContent() map[string]string {
	content := map[string]string{
		"title": o.Title,
	}
	if o.Body != "" {
		content["body"] = o.Body
	}
	if o.Description != "" {
		content["description"] = o.Description
	}
	return content
}

// Schema returns the message type for observations.
func (o *Observation) Schema() message.Type {
	return message.Type{
		Domain:   "content",
		Category: "observation",
		Version:  "v1",
	}
}

// Validate checks required fields.
func (o *Observation) Validate() error {
	if o.ID == "" {
		return fmt.Errorf("id is required")
	}
	if o.Title == "" {
		return fmt.Errorf("title is required")
	}
	if o.OrgID == "" {
		return fmt.Errorf("org_id is required (set by processor)")
	}
	if o.Platform == "" {
		return fmt.Errorf("platform is required (set by processor)")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (o *Observation) MarshalJSON() ([]byte, error) {
	type Alias Observation
	return json.Marshal((*Alias)(o))
}

// UnmarshalJSON implements json.Unmarshaler.
func (o *Observation) UnmarshalJSON(data []byte) error {
	type Alias Observation
	return json.Unmarshal(data, (*Alias)(o))
}

// Compile-time interface checks
var (
	_ graph.Graphable         = (*Observation)(nil)
	_ message.ContentStorable = (*Observation)(nil)
	_ message.Payload         = (*Observation)(nil)
)
