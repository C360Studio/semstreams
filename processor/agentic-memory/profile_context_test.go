package agenticmemory

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"

	operatingmodel "github.com/c360studio/semstreams/agentic/operating-model"
)

// --- AssembleProfileContext (pure) ---

func sampleEntries() []operatingmodel.Entry {
	return []operatingmodel.Entry{
		{EntryID: "e1", Title: "Weekly planning", Summary: "Mondays 9-10am", Cadence: "weekly", Status: operatingmodel.StatusActive},
		{EntryID: "e2", Title: "Finance emails", Summary: "Friday morning review", Cadence: "weekly", Status: operatingmodel.StatusActive},
		{EntryID: "e3", Title: "Stale decision", Summary: "from last quarter", Status: operatingmodel.StatusSuperseded},
		{EntryID: "e4", Title: "Needs follow-up", Summary: "not resolved yet", Status: operatingmodel.StatusUnresolved},
	}
}

func TestAssembleProfileContext_PopulatesOperatingModelSlice(t *testing.T) {
	in := ProfileContextInputs{
		UserID:         "coby",
		LoopID:         "loop-abc",
		ProfileVersion: 2,
		Entries:        sampleEntries(),
		TokenBudget:    800,
		Now:            time.Date(2026, 4, 15, 10, 0, 0, 0, time.UTC),
	}
	got := AssembleProfileContext(in)

	if got.UserID != "coby" || got.LoopID != "loop-abc" {
		t.Errorf("ids wrong: %+v", got)
	}
	if got.ProfileVersion != 2 {
		t.Errorf("ProfileVersion = %d, want 2", got.ProfileVersion)
	}
	if got.OperatingModel.EntryCount == 0 {
		t.Errorf("expected non-zero entry count, got 0")
	}
	if got.OperatingModel.TokenCount == 0 {
		t.Errorf("expected non-zero token count, got 0")
	}
	if got.OperatingModel.Content == "" || !strings.Contains(got.OperatingModel.Content, "Weekly planning") {
		t.Errorf("rendered content missing key entry:\n%s", got.OperatingModel.Content)
	}
	if got.LessonsLearned.Content != "" {
		t.Errorf("lessons_learned should be empty in v1, got %q", got.LessonsLearned.Content)
	}
	if err := got.Validate(); err != nil {
		t.Errorf("assembled payload fails Validate: %v", err)
	}
}

func TestAssembleProfileContext_ActiveEntriesRankedFirst(t *testing.T) {
	// Superseded and unresolved entries should sort after active ones.
	in := ProfileContextInputs{
		UserID:      "coby",
		LoopID:      "loop-1",
		Entries:     sampleEntries(),
		TokenBudget: 800,
	}
	got := AssembleProfileContext(in)
	content := got.OperatingModel.Content
	// Both active entries ("Weekly planning", "Finance emails") should appear
	// before the superseded ("Stale decision") in the rendered output.
	idxActive := strings.Index(content, "Finance emails")
	idxSuperseded := strings.Index(content, "Stale decision")
	if idxActive == -1 || idxSuperseded == -1 {
		t.Fatalf("expected both entries rendered; content:\n%s", content)
	}
	if idxActive > idxSuperseded {
		t.Errorf("superseded appears before active:\n%s", content)
	}
}

func TestAssembleProfileContext_EmptyEntriesProducesNoContent(t *testing.T) {
	got := AssembleProfileContext(ProfileContextInputs{
		UserID: "coby", LoopID: "loop-1", TokenBudget: 800,
	})
	if got.OperatingModel.Content != "" {
		t.Errorf("expected empty content, got %q", got.OperatingModel.Content)
	}
	if got.HasOperatingModel() {
		t.Errorf("HasOperatingModel true with no entries")
	}
	if err := got.Validate(); err != nil {
		t.Errorf("empty-entries payload fails Validate: %v", err)
	}
}

func TestAssembleProfileContext_TokenBudgetTruncates(t *testing.T) {
	// Build many entries so we exceed a small budget.
	entries := make([]operatingmodel.Entry, 30)
	for i := range entries {
		entries[i] = operatingmodel.Entry{
			EntryID: "e" + string(rune('a'+i%26)),
			Title:   strings.Repeat("x", 50),
			Summary: strings.Repeat("y", 200),
			Status:  operatingmodel.StatusActive,
		}
	}
	got := AssembleProfileContext(ProfileContextInputs{
		UserID: "coby", LoopID: "loop-1",
		Entries:     entries,
		TokenBudget: 200, // tight budget
	})
	if got.OperatingModel.TokenCount > got.TokenBudget+50 {
		// +50 tolerance: we only enforce the budget on the *next* entry, so a
		// single rendered line can overshoot, but not by much.
		t.Errorf("TokenCount = %d exceeds TokenBudget %d by too much",
			got.OperatingModel.TokenCount, got.TokenBudget)
	}
	if got.OperatingModel.EntryCount >= len(entries) {
		t.Errorf("expected truncation, got all %d entries rendered", got.OperatingModel.EntryCount)
	}
	if err := got.Validate(); err != nil {
		t.Errorf("truncated payload fails Validate: %v", err)
	}
}

func TestAssembleProfileContext_DeterministicAcrossCalls(t *testing.T) {
	// Same input → same rendered content (tie-breaking on title is stable).
	in := ProfileContextInputs{
		UserID: "coby", LoopID: "loop-1",
		Entries:     sampleEntries(),
		TokenBudget: 800,
		Now:         time.Date(2026, 4, 15, 10, 0, 0, 0, time.UTC),
	}
	a := AssembleProfileContext(in)
	b := AssembleProfileContext(in)
	if a.OperatingModel.Content != b.OperatingModel.Content {
		t.Errorf("non-deterministic content between calls")
	}
}

// --- handleLoopCreated (handler wired through) ---

// stubProfileReader returns a fixed result set.
type stubProfileReader struct {
	entries []operatingmodel.Entry
	version int
	err     error
}

func (s stubProfileReader) ReadOperatingModel(ctx context.Context, _, _, _ string) (*operatingmodel.ProfileResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.err != nil {
		return nil, s.err
	}
	return &operatingmodel.ProfileResult{Entries: s.entries, Version: s.version}, nil
}

func newProfileContextTestComponent(t *testing.T, reader operatingmodel.ProfileReader) *Component {
	t.Helper()
	cfg := DefaultConfig()
	rawCfg, _ := json.Marshal(cfg)
	deps := component.Dependencies{
		NATSClient: nil,
		Platform:   component.PlatformMeta{Org: "c360", Platform: "ops"},
	}
	comp, err := NewComponent(rawCfg, deps)
	if err != nil {
		t.Fatalf("NewComponent: %v", err)
	}
	c := comp.(*Component)
	c.SetProfileReader(reader)
	return c
}

func wrapLoopCreated(t *testing.T, event *agentic.LoopCreatedEvent) []byte {
	t.Helper()
	baseMsg := message.NewBaseMessage(event.Schema(), event, "test")
	data, err := baseMsg.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal loop_created: %v", err)
	}
	return data
}

func validLoopCreated(userID string) *agentic.LoopCreatedEvent {
	return &agentic.LoopCreatedEvent{
		LoopID:        "loop-abc",
		TaskID:        "task-xyz",
		Role:          "general",
		Model:         "claude-haiku",
		MaxIterations: 20,
		CreatedAt:     time.Now(),
		Metadata: map[string]any{
			"user_id": userID,
		},
	}
}

func TestHandleLoopCreated_Success(t *testing.T) {
	reader := stubProfileReader{
		entries: sampleEntries(),
		version: 1,
	}
	c := newProfileContextTestComponent(t, reader)
	data := wrapLoopCreated(t, validLoopCreated("coby"))

	before := atomic.LoadInt64(&c.eventsProcessed)
	beforeErrs := atomic.LoadInt64(&c.errors)

	c.handleLoopCreated(context.Background(), data)

	if got := atomic.LoadInt64(&c.eventsProcessed); got != before+1 {
		t.Errorf("eventsProcessed = %d, want %d", got, before+1)
	}
	if got := atomic.LoadInt64(&c.errors); got != beforeErrs {
		t.Errorf("errors = %d, want unchanged %d", got, beforeErrs)
	}
}

func TestHandleLoopCreated_SkipsWhenUserIDAbsent(t *testing.T) {
	c := newProfileContextTestComponent(t, stubProfileReader{entries: sampleEntries(), version: 1})
	event := validLoopCreated("")
	event.Metadata = nil // strip user_id
	data := wrapLoopCreated(t, event)

	c.handleLoopCreated(context.Background(), data)

	if got := atomic.LoadInt64(&c.eventsProcessed); got != 0 {
		t.Errorf("eventsProcessed = %d, want 0 (skipped)", got)
	}
	if got := atomic.LoadInt64(&c.errors); got != 0 {
		t.Errorf("errors = %d, want 0 (skip is not an error)", got)
	}
}

func TestHandleLoopCreated_InvalidJSON(t *testing.T) {
	c := newProfileContextTestComponent(t, operatingmodel.EmptyProfileReader{})
	c.handleLoopCreated(context.Background(), []byte("{nope"))
	if got := atomic.LoadInt64(&c.errors); got != 1 {
		t.Errorf("errors = %d, want 1", got)
	}
}

func TestHandleLoopCreated_WrongPayloadType(t *testing.T) {
	c := newProfileContextTestComponent(t, operatingmodel.EmptyProfileReader{})
	// Wrap a ProfileContext (different type) in a BaseMessage.
	foreign := &operatingmodel.ProfileContext{UserID: "u", LoopID: "l"}
	baseMsg := message.NewBaseMessage(foreign.Schema(), foreign, "test")
	data, _ := baseMsg.MarshalJSON()

	c.handleLoopCreated(context.Background(), data)
	if got := atomic.LoadInt64(&c.errors); got != 1 {
		t.Errorf("errors = %d, want 1 (wrong payload type)", got)
	}
}

func TestHandleLoopCreated_ReaderError(t *testing.T) {
	c := newProfileContextTestComponent(t, stubProfileReader{err: errors.New("graph down")})
	data := wrapLoopCreated(t, validLoopCreated("coby"))

	c.handleLoopCreated(context.Background(), data)
	if got := atomic.LoadInt64(&c.errors); got != 1 {
		t.Errorf("errors = %d, want 1 when reader fails", got)
	}
	if got := atomic.LoadInt64(&c.eventsProcessed); got != 0 {
		t.Errorf("eventsProcessed = %d, want 0 on reader failure", got)
	}
}

func TestHandleLoopCreated_EmptyProfileStillSucceeds(t *testing.T) {
	// A user who hasn't onboarded yet: reader returns (nil, 0, nil). The
	// handler must still publish an (empty-slice) profile-context so
	// downstream consumers get consistent signaling.
	c := newProfileContextTestComponent(t, operatingmodel.EmptyProfileReader{})
	data := wrapLoopCreated(t, validLoopCreated("new-user"))

	c.handleLoopCreated(context.Background(), data)
	if got := atomic.LoadInt64(&c.eventsProcessed); got != 1 {
		t.Errorf("eventsProcessed = %d, want 1 (empty profile is still a publish)", got)
	}
	if got := atomic.LoadInt64(&c.errors); got != 0 {
		t.Errorf("errors = %d, want 0", got)
	}
}

func TestHandlerForPort_RoutesLoopCreatedEvents(t *testing.T) {
	c := newProfileContextTestComponent(t, operatingmodel.EmptyProfileReader{})
	h, ok := c.handlerForPort("loop_created_events")
	if !ok {
		t.Fatal("loop_created_events port not routed")
	}
	if h == nil {
		t.Error("loop_created_events handler is nil")
	}
}

func TestSetProfileReader_NilRestoresEmpty(t *testing.T) {
	c := newProfileContextTestComponent(t, stubProfileReader{entries: sampleEntries(), version: 1})
	c.SetProfileReader(nil)
	if _, ok := c.getProfileReader().(operatingmodel.EmptyProfileReader); !ok {
		t.Errorf("expected operatingmodel.EmptyProfileReader, got %T", c.getProfileReader())
	}
}

func TestMetadataUserID(t *testing.T) {
	cases := []struct {
		name string
		meta map[string]any
		want string
	}{
		{"nil map", nil, ""},
		{"missing key", map[string]any{"other": "x"}, ""},
		{"wrong type", map[string]any{"user_id": 42}, ""},
		{"string value", map[string]any{"user_id": "coby"}, "coby"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := metadataUserID(c.meta); got != c.want {
				t.Errorf("metadataUserID = %q, want %q", got, c.want)
			}
		})
	}
}

func TestRankEntries_StableBetweenCallsWithSameInput(t *testing.T) {
	entries := sampleEntries()
	a := rankEntries(entries)
	b := rankEntries(entries)
	if len(a) != len(b) {
		t.Fatalf("rank length mismatch: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].EntryID != b[i].EntryID {
			t.Errorf("rank non-stable at i=%d: %q vs %q", i, a[i].EntryID, b[i].EntryID)
		}
	}
}
