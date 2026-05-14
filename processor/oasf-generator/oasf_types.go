// Package oasfgenerator provides an OASF (Open Agent Specification Framework) record generator
// that serializes SemStreams agent capabilities to the AGNTCY standard format.
package oasfgenerator

import (
	"encoding/json"
	"fmt"
	"time"
)

// OASFRecord represents an OASF (Open Agent Specification Framework) record.
// This is the standard format for describing agent capabilities in the AGNTCY ecosystem.
// See: https://docs.agntcy.org/pages/syntaxes/oasf
type OASFRecord struct {
	// Name is the human-readable name of the agent.
	Name string `json:"name"`

	// Version is the semantic version of the agent (e.g., "1.0.0").
	Version string `json:"version"`

	// SchemaVersion is the OASF schema version this record conforms to.
	// Currently "1.0.0" is the supported version.
	SchemaVersion string `json:"schema_version"`

	// Authors lists the creators/maintainers of the agent.
	Authors []string `json:"authors"`

	// CreatedAt is when this OASF record was generated (RFC-3339 format).
	CreatedAt string `json:"created_at"`

	// Description provides a human-readable description of the agent's purpose.
	Description string `json:"description"`

	// Skills lists the capabilities the agent possesses.
	Skills []OASFSkill `json:"skills"`

	// Domains lists the domains this agent operates in.
	Domains []OASFDomain `json:"domains,omitempty"`

	// Extensions holds provider-specific metadata.
	Extensions map[string]any `json:"extensions,omitempty"`
}

// OASFSkill represents a single capability/skill in an OASF record.
type OASFSkill struct {
	// ID is the AGNTCY OASF taxonomy class ID. Canonical values are
	// drawn from the published taxonomy (see vocabulary/oasf); IDs in
	// the extension range (>= oasf.ExtensionBase) identify SemStreams
	// custom skill classes that don't have a canonical taxonomy entry.
	// Zero is intentionally not a valid OASF class ID.
	ID uint32 `json:"id"`

	// Name is the OASF hierarchical name for the skill class
	// (path-segment form, e.g. "natural_language_processing" or
	// "semstreams/code_review" for extensions). Pairs with ID — the
	// numeric class is authoritative, the name is the human-readable
	// hierarchy mirror.
	Name string `json:"name"`

	// Description provides details about what this skill does.
	Description string `json:"description,omitempty"`

	// Confidence is the agent's self-assessed confidence in this skill (0.0-1.0).
	Confidence float64 `json:"confidence,omitempty"`

	// Permissions lists the permissions required to execute this skill.
	Permissions []string `json:"permissions,omitempty"`

	// Tags provide categorical labels for skill discovery.
	Tags []string `json:"tags,omitempty"`

	// InputSchema describes the expected input format (JSON Schema).
	InputSchema map[string]any `json:"input_schema,omitempty"`

	// OutputSchema describes the expected output format (JSON Schema).
	OutputSchema map[string]any `json:"output_schema,omitempty"`
}

// OASFDomain represents a domain the agent operates in.
type OASFDomain struct {
	// Name is the domain identifier.
	Name string `json:"name"`

	// Description provides details about the domain.
	Description string `json:"description,omitempty"`

	// Priority indicates the agent's focus on this domain (higher = more focused).
	Priority int `json:"priority,omitempty"`
}

// CurrentSchemaVersion is the OASF schema version this implementation supports.
const CurrentSchemaVersion = "1.0.0"

// NewOASFRecord creates a new OASF record with required fields.
func NewOASFRecord(name, version, description string) *OASFRecord {
	return &OASFRecord{
		Name:          name,
		Version:       version,
		SchemaVersion: CurrentSchemaVersion,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		Description:   description,
		Skills:        []OASFSkill{},
		Authors:       []string{},
	}
}

// AddSkill adds a skill to the OASF record.
func (r *OASFRecord) AddSkill(skill OASFSkill) {
	r.Skills = append(r.Skills, skill)
}

// AddDomain adds a domain to the OASF record.
func (r *OASFRecord) AddDomain(domain OASFDomain) {
	r.Domains = append(r.Domains, domain)
}

// AddAuthor adds an author to the OASF record.
func (r *OASFRecord) AddAuthor(author string) {
	r.Authors = append(r.Authors, author)
}

// SetExtension sets an extension value.
func (r *OASFRecord) SetExtension(key string, value any) {
	if r.Extensions == nil {
		r.Extensions = make(map[string]any)
	}
	r.Extensions[key] = value
}

// Validate checks if the OASF record is valid.
func (r *OASFRecord) Validate() error {
	if r.Name == "" {
		return fmt.Errorf("name is required")
	}
	if r.Version == "" {
		return fmt.Errorf("version is required")
	}
	if r.SchemaVersion == "" {
		return fmt.Errorf("schema_version is required")
	}
	if r.CreatedAt == "" {
		return fmt.Errorf("created_at is required")
	}

	// Validate each skill
	for i, skill := range r.Skills {
		if err := skill.Validate(); err != nil {
			return fmt.Errorf("skill[%d]: %w", i, err)
		}
	}

	// Validate each domain
	for i, domain := range r.Domains {
		if err := domain.Validate(); err != nil {
			return fmt.Errorf("domain[%d]: %w", i, err)
		}
	}

	return nil
}

// MarshalJSON implements json.Marshaler.
func (r *OASFRecord) MarshalJSON() ([]byte, error) {
	type Alias OASFRecord
	return json.Marshal((*Alias)(r))
}

// Validate checks if the OASF skill is valid.
func (s *OASFSkill) Validate() error {
	if s.ID == 0 {
		return fmt.Errorf("id is required (zero is not a valid OASF class ID)")
	}
	if s.Name == "" {
		return fmt.Errorf("name is required")
	}
	if s.Confidence < 0 || s.Confidence > 1 {
		return fmt.Errorf("confidence must be between 0 and 1, got %f", s.Confidence)
	}
	return nil
}

// Validate checks if the OASF domain is valid.
func (d *OASFDomain) Validate() error {
	if d.Name == "" {
		return fmt.Errorf("name is required")
	}
	return nil
}
