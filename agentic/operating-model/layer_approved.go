package operatingmodel

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semstreams/message"
)

// LayerApproved is the payload emitted by the /onboard command handler when a
// user approves a layer checkpoint. It carries everything teams-memory needs to
// write the approved layer and its entries into the graph.
//
// Message type: operating_model.layer_approved.v1.
type LayerApproved struct {
	// UserID is the profile owner. Used as the instance part of profile and
	// layer entity IDs.
	UserID string `json:"user_id"`

	// LoopID is the agentic loop that produced the approval. Used by
	// teams-memory to correlate the graph mutation with its originating loop.
	LoopID string `json:"loop_id"`

	// Layer is one of the five canonical layer names.
	Layer string `json:"layer"`

	// ProfileVersion is the profile version this layer belongs to (starts at 1).
	// Monotonically increments when the user re-runs the full interview.
	ProfileVersion int `json:"profile_version"`

	// CheckpointSummary is the user-approved natural-language summary of the layer.
	CheckpointSummary string `json:"checkpoint_summary"`

	// Entries is the structured list of facts captured during this layer.
	Entries []Entry `json:"entries"`

	// ApprovedAt is the timestamp of user approval. Populated by the publisher.
	ApprovedAt time.Time `json:"approved_at"`
}

// Schema implements message.Payload. Schema returns static constants so it
// remains callable on a zero-value receiver; the payload registry invokes
// Schema() on a freshly constructed payload during init-time validation.
func (p *LayerApproved) Schema() message.Type {
	return message.Type{
		Domain:   Domain,
		Category: CategoryLayerApproved,
		Version:  SchemaVersion,
	}
}

// Validate implements message.Payload.
func (p *LayerApproved) Validate() error {
	if strings.TrimSpace(p.UserID) == "" {
		return fmt.Errorf("user_id required")
	}
	if strings.Contains(p.UserID, ".") {
		return fmt.Errorf("user_id %q must not contain dots", p.UserID)
	}
	if strings.TrimSpace(p.LoopID) == "" {
		return fmt.Errorf("loop_id required")
	}
	if !IsValidLayer(p.Layer) {
		return fmt.Errorf("layer %q is not a canonical layer name", p.Layer)
	}
	if p.ProfileVersion < 1 {
		return fmt.Errorf("profile_version must be >= 1, got %d", p.ProfileVersion)
	}
	if strings.TrimSpace(p.CheckpointSummary) == "" {
		return fmt.Errorf("checkpoint_summary required")
	}
	if len(p.Entries) == 0 {
		return fmt.Errorf("entries must contain at least one entry")
	}
	if p.ApprovedAt.IsZero() {
		return fmt.Errorf("approved_at required")
	}
	seen := make(map[string]struct{}, len(p.Entries))
	for i := range p.Entries {
		if err := p.Entries[i].Validate(); err != nil {
			return fmt.Errorf("entries[%d]: %w", i, err)
		}
		if _, dup := seen[p.Entries[i].EntryID]; dup {
			return fmt.Errorf("entries[%d]: entry_id %q duplicated", i, p.Entries[i].EntryID)
		}
		seen[p.Entries[i].EntryID] = struct{}{}
	}
	return nil
}

// MarshalJSON implements json.Marshaler using the Alias pattern to avoid
// recursion via the Payload interface.
func (p *LayerApproved) MarshalJSON() ([]byte, error) {
	type Alias LayerApproved
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler using the Alias pattern to avoid
// recursion via the Payload interface.
func (p *LayerApproved) UnmarshalJSON(data []byte) error {
	type Alias LayerApproved
	return json.Unmarshal(data, (*Alias)(p))
}

// Triples converts an approved layer into a slice of graph triples ready for
// publication on graph.mutation.{loopID}. Callers must call Validate() first;
// this method assumes all fields are present and valid, including ApprovedAt.
func (p *LayerApproved) Triples(org, platform string) []message.Triple {
	ref := ProfileRef{
		Org:      org,
		Platform: platform,
		UserID:   p.UserID,
		Version:  p.ProfileVersion,
	}
	return LayerTriples(ref, p.Layer, p.CheckpointSummary, p.Entries, p.ApprovedAt)
}
