package operatingmodel

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semstreams/message"
)

// ProfileContext is the assembled context payload teams-memory publishes on
// loop-starting events. The teams-loop hydrator consumes it and prepends a
// "How this user works" section to the system prompt.
//
// The payload shape reserves fields for multiple memory categories. v1 only
// populates OperatingModel; future PRs will fill LessonsLearned.
//
// Message type: operating_model.profile_context.v1.
type ProfileContext struct {
	// UserID is the profile owner.
	UserID string `json:"user_id"`

	// LoopID is the loop this context is being delivered to.
	LoopID string `json:"loop_id"`

	// ProfileVersion is the version of the operating-model profile the
	// OperatingModel slice was assembled from. Zero if no profile exists yet.
	ProfileVersion int `json:"profile_version"`

	// OperatingModel is the operating-model slice of the context. Populated
	// from the user's profile triples in the graph. Empty if the user has
	// not completed an onboarding interview yet.
	OperatingModel ProfileContextSlice `json:"operating_model"`

	// LessonsLearned is the lessons-learned slice. Reserved — not populated
	// in v1. Future hydrator passes will fill this from
	// compaction-extracted triples.
	LessonsLearned ProfileContextSlice `json:"lessons_learned"`

	// TokenBudget is the total token budget applied when assembling this
	// payload. The sum of OperatingModel.TokenCount and LessonsLearned.TokenCount
	// should not exceed it.
	TokenBudget int `json:"token_budget"`

	// AssembledAt is the timestamp of assembly.
	AssembledAt time.Time `json:"assembled_at"`
}

// ProfileContextSlice is a single category of injected context.
type ProfileContextSlice struct {
	// Content is the rendered natural-language text for this slice, ready to
	// prepend to the system prompt.
	Content string `json:"content"`

	// TokenCount is the estimated token count for Content.
	TokenCount int `json:"token_count"`

	// EntryCount is the number of distinct entries condensed into Content.
	// Informational; useful for telemetry and UI displays.
	EntryCount int `json:"entry_count"`
}

// Schema implements message.Payload. Schema returns static constants so it
// remains callable on a zero-value receiver; the payload registry invokes
// Schema() on a freshly constructed payload during init-time validation.
func (p *ProfileContext) Schema() message.Type {
	return message.Type{
		Domain:   Domain,
		Category: CategoryProfileContext,
		Version:  SchemaVersion,
	}
}

// Validate implements message.Payload.
func (p *ProfileContext) Validate() error {
	if strings.TrimSpace(p.UserID) == "" {
		return fmt.Errorf("user_id required")
	}
	if strings.TrimSpace(p.LoopID) == "" {
		return fmt.Errorf("loop_id required")
	}
	if p.ProfileVersion < 0 {
		return fmt.Errorf("profile_version must be >= 0, got %d", p.ProfileVersion)
	}
	if p.TokenBudget < 0 {
		return fmt.Errorf("token_budget must be >= 0, got %d", p.TokenBudget)
	}
	if err := p.OperatingModel.validate("operating_model"); err != nil {
		return err
	}
	if err := p.LessonsLearned.validate("lessons_learned"); err != nil {
		return err
	}
	if p.TokenBudget > 0 {
		used := p.OperatingModel.TokenCount + p.LessonsLearned.TokenCount
		if used > p.TokenBudget {
			return fmt.Errorf("slices exceed token_budget: used=%d budget=%d",
				used, p.TokenBudget)
		}
	}
	return nil
}

func (s *ProfileContextSlice) validate(name string) error {
	if s.TokenCount < 0 {
		return fmt.Errorf("%s.token_count must be >= 0, got %d", name, s.TokenCount)
	}
	if s.EntryCount < 0 {
		return fmt.Errorf("%s.entry_count must be >= 0, got %d", name, s.EntryCount)
	}
	if s.EntryCount > 0 && strings.TrimSpace(s.Content) == "" {
		return fmt.Errorf("%s.entry_count=%d but content is empty", name, s.EntryCount)
	}
	return nil
}

// MarshalJSON implements json.Marshaler using the Alias pattern to avoid
// recursion via the Payload interface.
func (p *ProfileContext) MarshalJSON() ([]byte, error) {
	type Alias ProfileContext
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler using the Alias pattern to avoid
// recursion via the Payload interface.
func (p *ProfileContext) UnmarshalJSON(data []byte) error {
	type Alias ProfileContext
	return json.Unmarshal(data, (*Alias)(p))
}

// HasOperatingModel reports whether the operating-model slice carries any
// rendered content — a quick check for consumers deciding whether to inject.
func (p *ProfileContext) HasOperatingModel() bool {
	return strings.TrimSpace(p.OperatingModel.Content) != ""
}

// SystemPromptPreamble renders the payload into the final string the
// teams-loop hydrator prepends to the system prompt. Returns "" when there is
// nothing to inject. Slice content is trimmed of surrounding whitespace and
// terminated with exactly one newline to keep the rendered prompt stable
// regardless of producer formatting.
func (p *ProfileContext) SystemPromptPreamble() string {
	om := strings.TrimSpace(p.OperatingModel.Content)
	lessons := strings.TrimSpace(p.LessonsLearned.Content)
	if om == "" && lessons == "" {
		return ""
	}
	var b strings.Builder
	if om != "" {
		b.WriteString("## How this user works\n")
		b.WriteString(om)
		b.WriteString("\n")
	}
	if lessons != "" {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("## Lessons from prior sessions\n")
		b.WriteString(lessons)
		b.WriteString("\n")
	}
	return b.String()
}
