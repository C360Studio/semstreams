package agenticmemory

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/agentic"
	operatingmodel "github.com/c360studio/semstreams/agentic/operating-model"
	"github.com/c360studio/semstreams/message"
)

// profileContextSubject returns the NATS subject used to publish an assembled
// operating_model.profile_context event to teams-loop, resolved from the
// component's port config. Kept as a method so tests can exercise it.
func (c *Component) profileContextSubject(loopID string) string {
	return c.outputSubject("profile_context", loopID)
}

// defaultProfileContextTokenBudget matches the plan's 800-token budget for
// system-prompt injection. Kept as a package constant so tests and future
// config plumbing have a single source of truth.
const defaultProfileContextTokenBudget = 800

// metadataUserID extracts the user_id string from a LoopCreatedEvent's
// Metadata map. Returns "" when absent or non-string.
func metadataUserID(meta map[string]any) string {
	if meta == nil {
		return ""
	}
	if v, ok := meta["user_id"].(string); ok {
		return v
	}
	return ""
}

// handleLoopCreated subscribes to agent.created.* events. For each created
// loop it assembles a ProfileContext payload (operating-model slice populated
// from the graph; lessons_learned stubbed) and publishes it to
// agent.context.profile.{loop_id} for teams-loop to consume.
//
// Loops without a user_id in Metadata are skipped silently — they are either
// system-initiated (no user to personalize for) or pre-date the user_id
// propagation wiring in teams-dispatch.
func (c *Component) handleLoopCreated(ctx context.Context, data []byte) {
	event, ok := c.unmarshalLoopCreated(data)
	if !ok {
		return
	}
	userID := metadataUserID(event.Metadata)
	if userID == "" {
		c.logger.DebugContext(ctx, "loop_created without user_id; skipping profile context",
			"loop_id", event.LoopID)
		return
	}

	if err := ctx.Err(); err != nil {
		return
	}

	payload, err := c.assembleProfileContextFromGraph(ctx, userID, event.LoopID)
	if err != nil {
		c.logger.Error("Failed to assemble profile context",
			"loop_id", event.LoopID,
			"user_id", userID,
			"error", err)
		atomic.AddInt64(&c.errors, 1)
		return
	}

	if err := c.publishProfileContext(ctx, payload); err != nil {
		c.logger.Error("Failed to publish profile context",
			"loop_id", event.LoopID,
			"user_id", userID,
			"error", err)
		atomic.AddInt64(&c.errors, 1)
		return
	}

	c.logger.Debug("Published profile context",
		"loop_id", event.LoopID,
		"user_id", userID,
		"entries", payload.OperatingModel.EntryCount,
		"tokens", payload.OperatingModel.TokenCount)
	atomic.AddInt64(&c.eventsProcessed, 1)
	c.mu.Lock()
	c.lastActivity = time.Now()
	c.mu.Unlock()
}

// unmarshalLoopCreated decodes a BaseMessage envelope and type-asserts the
// LoopCreatedEvent payload. Returns (event, true) on success, (nil, false) after
// logging+counting on any failure.
func (c *Component) unmarshalLoopCreated(data []byte) (*agentic.LoopCreatedEvent, bool) {
	baseMsg, err := c.decoder.Decode(data)
	if err != nil {
		c.logger.Error("Failed to unmarshal loop_created BaseMessage", "error", err)
		atomic.AddInt64(&c.errors, 1)
		return nil, false
	}
	event, ok := baseMsg.Payload().(*agentic.LoopCreatedEvent)
	if !ok {
		c.logger.Error("Unexpected loop_created payload type",
			"type", baseMsg.Type().String())
		atomic.AddInt64(&c.errors, 1)
		return nil, false
	}
	return event, true
}

// assembleProfileContextFromGraph reads the user's operating-model profile
// and runs it through the pure assembler. Extracted so the assembler can be
// unit-tested without the I/O boundary.
func (c *Component) assembleProfileContextFromGraph(
	ctx context.Context,
	userID, loopID string,
) (*operatingmodel.ProfileContext, error) {
	reader := c.getProfileReader()
	result, err := reader.ReadOperatingModel(ctx, c.platform.Org, c.platform.Platform, userID)
	if err != nil {
		return nil, fmt.Errorf("read operating model: %w", err)
	}
	var entries []operatingmodel.Entry
	var profileVersion int
	if result != nil {
		entries = result.Entries
		profileVersion = result.Version
	}
	return AssembleProfileContext(ProfileContextInputs{
		UserID:         userID,
		LoopID:         loopID,
		ProfileVersion: profileVersion,
		Entries:        entries,
		TokenBudget:    defaultProfileContextTokenBudget,
		Now:            time.Now().UTC(),
	}), nil
}

// ProfileContextInputs carries everything AssembleProfileContext needs to
// produce a ProfileContext. Split into a struct so the pure function has no
// optional-parameter ambiguity.
type ProfileContextInputs struct {
	UserID         string
	LoopID         string
	ProfileVersion int
	Entries        []operatingmodel.Entry
	TokenBudget    int
	Now            time.Time
}

// AssembleProfileContext builds an operating_model.profile_context.v1 payload
// from a set of operating-model entries. Entries are ranked (active status +
// friction priority + recency) and truncated to fit within TokenBudget.
//
// The lessons_learned slice is intentionally left empty in v1 — the shape is
// reserved for a later phase that folds compaction-extracted facts in.
func AssembleProfileContext(in ProfileContextInputs) *operatingmodel.ProfileContext {
	assembledAt := in.Now
	if assembledAt.IsZero() {
		assembledAt = time.Now().UTC()
	}

	ranked := rankEntries(in.Entries)
	rendered, entryCount, tokenCount := renderOperatingModelSlice(ranked, in.TokenBudget)

	return &operatingmodel.ProfileContext{
		UserID:         in.UserID,
		LoopID:         in.LoopID,
		ProfileVersion: in.ProfileVersion,
		OperatingModel: operatingmodel.ProfileContextSlice{
			Content:    rendered,
			TokenCount: tokenCount,
			EntryCount: entryCount,
		},
		LessonsLearned: operatingmodel.ProfileContextSlice{}, // reserved
		TokenBudget:    in.TokenBudget,
		AssembledAt:    assembledAt,
	}
}

// rankEntries orders entries so active ones land first, then unresolved,
// then superseded. Ties within a status bucket are broken alphabetically by
// Title so rendered output is deterministic across calls with the same input.
// A future phase may extend this with friction-priority sub-ranking once the
// Entry schema carries that field.
func rankEntries(entries []operatingmodel.Entry) []operatingmodel.Entry {
	sorted := make([]operatingmodel.Entry, len(entries))
	copy(sorted, entries)
	sort.SliceStable(sorted, func(i, j int) bool {
		si, sj := statusWeight(sorted[i]), statusWeight(sorted[j])
		if si != sj {
			return si < sj
		}
		// Title is a deterministic tiebreak so rendered output is stable
		// across assembler invocations with the same input set.
		return sorted[i].Title < sorted[j].Title
	})
	return sorted
}

// statusWeight assigns a sort weight where lower = higher priority for
// inclusion in the context.
func statusWeight(e operatingmodel.Entry) int {
	switch e.ResolvedStatus() {
	case operatingmodel.StatusActive:
		return 0
	case operatingmodel.StatusUnresolved:
		return 1
	case operatingmodel.StatusSuperseded:
		return 2
	}
	return 3
}

// renderOperatingModelSlice turns entries into a rendered bullet list, adding
// entries in ranked order until the token budget is exhausted. Returns the
// rendered content, the number of entries actually rendered, and the token
// count.
//
// Approx-token heuristic: 4 chars per token, matching the rough convention
// used throughout semstreams (see processor/teams-loop/context_manager.go
// estimateTokens).
//
// Contract note: at least one entry is always rendered if any are provided,
// even if that entry alone exceeds the budget. This keeps the rendered
// slice from being useless-by-default and matches the philosophy that a
// too-long single fact is still more useful than nothing at all. Callers
// with hard-limit budgets should pre-truncate entry summaries instead.
func renderOperatingModelSlice(entries []operatingmodel.Entry, budget int) (string, int, int) {
	if len(entries) == 0 {
		return "", 0, 0
	}
	var b strings.Builder
	count := 0
	for _, e := range entries {
		line := renderEntryLine(e)
		projected := estimatePromptTokens(b.String()) + estimatePromptTokens(line)
		if budget > 0 && projected > budget && count > 0 {
			break
		}
		b.WriteString(line)
		count++
		if budget > 0 && estimatePromptTokens(b.String()) >= budget {
			break
		}
	}
	content := b.String()
	return content, count, estimatePromptTokens(content)
}

// renderEntryLine produces a single compact line per entry. Format keeps the
// title + cadence on one line for readability in the rendered system prompt.
func renderEntryLine(e operatingmodel.Entry) string {
	var b strings.Builder
	b.WriteString("- ")
	b.WriteString(e.Title)
	if e.Cadence != "" {
		fmt.Fprintf(&b, " (%s)", e.Cadence)
	}
	b.WriteString(": ")
	b.WriteString(e.Summary)
	b.WriteString("\n")
	return b.String()
}

// estimatePromptTokens approximates token count for budgeting. Matches
// semstreams' context-manager convention (~4 chars per token).
func estimatePromptTokens(s string) int {
	return (len(s) + 3) / 4
}

// publishProfileContext wraps the payload in a BaseMessage envelope and
// publishes to agent.context.profile.{loop_id} on JetStream.
func (c *Component) publishProfileContext(ctx context.Context, payload *operatingmodel.ProfileContext) error {
	if err := payload.Validate(); err != nil {
		return fmt.Errorf("profile_context validate: %w", err)
	}
	baseMsg := message.NewBaseMessage(payload.Schema(), payload, "teams-memory")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal profile_context: %w", err)
	}
	subject := c.profileContextSubject(payload.LoopID)
	if c.natsClient == nil {
		c.logger.InfoContext(ctx, "profile_context publish skipped (no NATS client)",
			"subject", subject)
		return nil
	}
	return c.natsClient.PublishToStream(ctx, subject, data)
}
