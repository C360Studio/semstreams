package document

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
)

// Document represents a generic document entity. It implements the ContentStorable
// interface with federated entity IDs, semantic predicates, and content storage.
//
// ContentStorable pattern:
//   - Triples() returns metadata ONLY (Dublin Core predicates, NO body)
//   - ContentFields() maps semantic roles to field names in stored content
//   - RawContent() returns content to store in ObjectStore
//   - StorageRef() returns reference to stored content (set by processor)
type Document struct {
	// Input fields (from incoming JSON)
	ID          string   `json:"id"`          // e.g., "doc-001"
	Title       string   `json:"title"`       // e.g., "Safety Manual"
	Description string   `json:"description"` // Primary text for semantic search
	Body        string   `json:"body"`        // Full text content (stored in ObjectStore, NOT in triples)
	Summary     string   `json:"summary"`     // Brief summary
	Category    string   `json:"category"`    // e.g., "safety", "operations"
	Tags        []string `json:"tags"`        // Classification tags
	CreatedAt   string   `json:"created_at"`  // ISO timestamp
	UpdatedAt   string   `json:"updated_at"`  // ISO timestamp

	// Context fields (set by processor from config, preserved through JSON for NATS transport)
	OrgID    string `json:"org_id,omitempty"`   // e.g., "acme"
	Platform string `json:"platform,omitempty"` // e.g., "logistics"

	// Storage reference (set by processor after storing content)
	storageRef *message.StorageReference `json:"-"`
}

// EntityID returns a deterministic 6-part federated entity ID following the pattern:
// {org}.{platform}.{system}.{domain}.{type}.{instance} — canonical order (ADR-102):
// system = document (the source), domain = content (this example's delegated taxonomy).
//
// Example: "acme.logistics.document.content.safety.doc-001"
func (d *Document) EntityID() string {
	category := d.Category
	if category == "" {
		category = "general"
	}
	return fmt.Sprintf("%s.%s.document.content.%s.%s",
		d.OrgID,
		d.Platform,
		category,
		d.ID,
	)
}

// Triples returns METADATA ONLY facts about this document using Dublin Core predicates.
// Large content fields (body, description) are stored in ObjectStore, NOT in triples.
// This prevents bloating entity state and enables efficient embedding extraction.
func (d *Document) Triples() []message.Triple {
	entityID := d.EntityID()
	now := time.Now()

	triples := []message.Triple{
		// Dublin Core: Title
		{
			Subject:    entityID,
			Predicate:  PredicateDCTitle,
			Object:     d.Title,
			Source:     tripleSourceName,
			Timestamp:  now,
			Confidence: defaultConfidence,
		},
		// Dublin Core: Type (document)
		{
			Subject:    entityID,
			Predicate:  PredicateDCType,
			Object:     "document",
			Source:     tripleSourceName,
			Timestamp:  now,
			Confidence: defaultConfidence,
		},
	}

	// Dublin Core: Subject (category)
	if d.Category != "" {
		triples = append(triples, message.Triple{
			Subject:    entityID,
			Predicate:  PredicateDCSubject,
			Object:     d.Category,
			Source:     tripleSourceName,
			Timestamp:  now,
			Confidence: defaultConfidence,
		})
	}

	// Tags as classification triples
	for _, tag := range d.Tags {
		triples = append(triples, message.Triple{
			Subject:    entityID,
			Predicate:  PredicateContentTag,
			Object:     tag,
			Source:     tripleSourceName,
			Timestamp:  now,
			Confidence: defaultConfidence,
		})
	}

	// Dublin Core: Date (created)
	if d.CreatedAt != "" {
		ts, err := time.Parse(time.RFC3339, d.CreatedAt)
		if err != nil {
			slog.Warn("invalid created_at timestamp",
				"entity_id", entityID,
				"value", d.CreatedAt,
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

	// NOTE: Body, Description, Summary are NOT in triples.
	// They are stored in ObjectStore and accessed via StorageRef + ContentFields.

	return triples
}

// StorageRef implements message.Storable interface.
// Returns reference to where content is stored in ObjectStore.
func (d *Document) StorageRef() *message.StorageReference {
	return d.storageRef
}

// SetStorageRef is called by processor after storing content in ObjectStore.
func (d *Document) SetStorageRef(ref *message.StorageReference) {
	d.storageRef = ref
}

// ContentFields implements message.ContentStorable interface.
// Returns semantic role → field name mapping for content stored in ObjectStore.
// Embedding workers use these roles to find text for embedding generation.
func (d *Document) ContentFields() map[string]string {
	fields := map[string]string{
		message.ContentRoleTitle: "title",
	}
	if d.Body != "" {
		fields[message.ContentRoleBody] = "body"
	}
	if d.Description != "" {
		fields[message.ContentRoleAbstract] = "description"
	}
	return fields
}

// RawContent implements message.ContentStorable interface.
// Returns content to store in ObjectStore.
// Field names here match values in ContentFields().
func (d *Document) RawContent() map[string]string {
	content := map[string]string{
		"title": d.Title,
	}
	if d.Body != "" {
		content["body"] = d.Body
	}
	if d.Description != "" {
		content["description"] = d.Description
	}
	if d.Summary != "" {
		content["summary"] = d.Summary
	}
	return content
}

// Schema returns the message type for documents.
func (d *Document) Schema() message.Type {
	return message.Type{
		Domain:   "content",
		Category: "document",
		Version:  "v1",
	}
}

// Validate checks that the document has all required fields.
func (d *Document) Validate() error {
	if d.ID == "" {
		return fmt.Errorf("id is required")
	}
	if d.Title == "" {
		return fmt.Errorf("title is required")
	}
	if d.OrgID == "" {
		return fmt.Errorf("org_id is required (set by processor)")
	}
	if d.Platform == "" {
		return fmt.Errorf("platform is required (set by processor)")
	}
	return nil
}

// MarshalJSON implements json.Marshaler for Document.
func (d *Document) MarshalJSON() ([]byte, error) {
	type Alias Document
	return json.Marshal((*Alias)(d))
}

// UnmarshalJSON implements json.Unmarshaler for Document.
func (d *Document) UnmarshalJSON(data []byte) error {
	type Alias Document
	return json.Unmarshal(data, (*Alias)(d))
}

// Compile-time interface checks
var (
	_ graph.Graphable         = (*Document)(nil)
	_ message.ContentStorable = (*Document)(nil)
	_ message.Payload         = (*Document)(nil)
)
