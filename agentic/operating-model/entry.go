package operatingmodel

import (
	"fmt"
	"strings"
)

// Entry is a single structured fact captured during a layer of the operating-model
// interview. Fields match OB1's schema.sql one-for-one so operating-model exports,
// if ever added, are a templated graph query rather than a schema migration.
type Entry struct {
	// EntryID is the stable identifier for this entry. Callers should supply a
	// UUID or other dot-free string. Used as the instance part of the entity ID.
	EntryID string `json:"entry_id"`

	// Title is a short human-readable label for the entry.
	Title string `json:"title"`

	// Summary is a one-or-two-sentence description.
	Summary string `json:"summary"`

	// Cadence is an optional cadence string (e.g., "weekly", "daily", "quarterly").
	Cadence string `json:"cadence,omitempty"`

	// Trigger is an optional trigger description (e.g., "Monday 9am").
	Trigger string `json:"trigger,omitempty"`

	// Inputs lists the inputs this entry depends on.
	Inputs []string `json:"inputs,omitempty"`

	// Stakeholders lists the people or systems involved.
	Stakeholders []string `json:"stakeholders,omitempty"`

	// Constraints lists the constraints that apply.
	Constraints []string `json:"constraints,omitempty"`

	// SourceConfidence is ConfidenceConfirmed or ConfidenceSynthesized.
	// Defaults to ConfidenceConfirmed if empty.
	SourceConfidence string `json:"source_confidence,omitempty"`

	// Status is StatusActive, StatusUnresolved, or StatusSuperseded.
	// Defaults to StatusActive if empty.
	Status string `json:"status,omitempty"`
}

// Validate checks that required fields are present and enum fields hold
// canonical values. Required fields: EntryID, Title, Summary.
func (e *Entry) Validate() error {
	if strings.TrimSpace(e.EntryID) == "" {
		return fmt.Errorf("entry_id required")
	}
	if strings.Contains(e.EntryID, ".") {
		return fmt.Errorf("entry_id %q must not contain dots", e.EntryID)
	}
	if strings.TrimSpace(e.Title) == "" {
		return fmt.Errorf("title required")
	}
	if strings.TrimSpace(e.Summary) == "" {
		return fmt.Errorf("summary required")
	}
	if e.SourceConfidence != "" &&
		e.SourceConfidence != ConfidenceConfirmed &&
		e.SourceConfidence != ConfidenceSynthesized {
		return fmt.Errorf("source_confidence %q must be %q or %q",
			e.SourceConfidence, ConfidenceConfirmed, ConfidenceSynthesized)
	}
	if e.Status != "" &&
		e.Status != StatusActive &&
		e.Status != StatusUnresolved &&
		e.Status != StatusSuperseded {
		return fmt.Errorf("status %q must be one of %q, %q, %q",
			e.Status, StatusActive, StatusUnresolved, StatusSuperseded)
	}
	return nil
}

// ResolvedSourceConfidence returns SourceConfidence or the default
// ConfidenceConfirmed when empty.
func (e *Entry) ResolvedSourceConfidence() string {
	if e.SourceConfidence == "" {
		return ConfidenceConfirmed
	}
	return e.SourceConfidence
}

// ResolvedStatus returns Status or the default StatusActive when empty.
func (e *Entry) ResolvedStatus() string {
	if e.Status == "" {
		return StatusActive
	}
	return e.Status
}
