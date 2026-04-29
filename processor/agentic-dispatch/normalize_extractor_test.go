package agenticdispatch

import (
	"context"
	"errors"
	"strings"
	"testing"

	operatingmodel "github.com/c360studio/semstreams/agentic/operating-model"
)

func TestNormalizeLayerAnswer_StubWhenNoNormalizer(t *testing.T) {
	c, _ := newInterviewTestComponent(t)
	c.SetLayerNormalizer(nil)

	got := c.normalizeLayerAnswer(context.Background(),
		operatingmodel.LayerOperatingRhythms,
		"Weekly planning\nBlock Mondays")
	if len(got) != 1 {
		t.Fatalf("expected 1 stub entry, got %d", len(got))
	}
	if got[0].Title != "Weekly planning" {
		t.Errorf("Title = %q, want %q (stub first-line behavior)", got[0].Title, "Weekly planning")
	}
}

func TestNormalizeLayerAnswer_StubWhenNormalizerErrors(t *testing.T) {
	c, _ := newInterviewTestComponent(t)
	c.SetLayerNormalizer(func(_ context.Context, _, _ string) ([]operatingmodel.Entry, error) {
		return nil, errors.New("model down")
	})

	got := c.normalizeLayerAnswer(context.Background(),
		operatingmodel.LayerFriction,
		"context-switch tax")
	if len(got) != 1 {
		t.Fatalf("expected stub fallback (1 entry) on normalizer error, got %d", len(got))
	}
}

func TestNormalizeLayerAnswer_StubWhenNormalizerEmpty(t *testing.T) {
	// A normalizer that returns no entries is treated identically to an
	// error — fall back to the stub so the user never gets "I extracted
	// nothing" silence.
	c, _ := newInterviewTestComponent(t)
	c.SetLayerNormalizer(func(_ context.Context, _, _ string) ([]operatingmodel.Entry, error) {
		return nil, nil
	})

	got := c.normalizeLayerAnswer(context.Background(),
		operatingmodel.LayerOperatingRhythms,
		"daily standups")
	if len(got) != 1 {
		t.Fatalf("expected stub fallback (1 entry) on empty normalizer, got %d", len(got))
	}
}

func TestNormalizeLayerAnswer_UsesNormalizerWhenSuccess(t *testing.T) {
	c, _ := newInterviewTestComponent(t)
	c.SetLayerNormalizer(func(_ context.Context, _, _ string) ([]operatingmodel.Entry, error) {
		return []operatingmodel.Entry{
			{Title: "Weekly planning", Summary: "Mondays 9-10am", Cadence: "weekly", Trigger: "Monday 9am"},
			{Title: "Daily standup", Summary: "10am team sync", Cadence: "daily", Trigger: "10am"},
		}, nil
	})

	got := c.normalizeLayerAnswer(context.Background(),
		operatingmodel.LayerOperatingRhythms,
		"weekly planning monday 9am, daily standup 10am")
	if len(got) != 2 {
		t.Fatalf("expected 2 entries from normalizer, got %d", len(got))
	}
	for i, e := range got {
		if e.EntryID == "" {
			t.Errorf("entry[%d] EntryID empty — finalize must generate one", i)
		}
		if strings.Contains(e.EntryID, ".") {
			t.Errorf("entry[%d] EntryID %q contains a dot", i, e.EntryID)
		}
		if e.SourceConfidence != operatingmodel.ConfidenceConfirmed {
			t.Errorf("entry[%d] SourceConfidence = %q, want default %q",
				i, e.SourceConfidence, operatingmodel.ConfidenceConfirmed)
		}
		if e.Status != operatingmodel.StatusActive {
			t.Errorf("entry[%d] Status = %q, want default %q",
				i, e.Status, operatingmodel.StatusActive)
		}
	}
	if got[0].Cadence != "weekly" {
		t.Errorf("Cadence preserved through finalize? got %q, want %q", got[0].Cadence, "weekly")
	}
}

func TestNormalizeLayerAnswer_FinalizeDropsInvalidEntries(t *testing.T) {
	// LLM returns a valid + an invalid entry (missing summary). finalize
	// must drop the invalid one rather than fail the whole extraction.
	c, _ := newInterviewTestComponent(t)
	c.SetLayerNormalizer(func(_ context.Context, _, _ string) ([]operatingmodel.Entry, error) {
		return []operatingmodel.Entry{
			{Title: "Valid", Summary: "with summary"},
			{Title: "Invalid"}, // no summary
			{Summary: "no title"},
		}, nil
	})

	got := c.normalizeLayerAnswer(context.Background(),
		operatingmodel.LayerOperatingRhythms,
		"any answer")
	if len(got) != 1 {
		t.Fatalf("expected 1 surviving entry, got %d (%+v)", len(got), got)
	}
	if got[0].Title != "Valid" {
		t.Errorf("survivor = %q, want %q", got[0].Title, "Valid")
	}
}

func TestParseNormalizationResponse_DirectJSON(t *testing.T) {
	resp := `{"entries": [{"title": "t1", "summary": "s1"}, {"title": "t2", "summary": "s2"}]}`
	got, err := parseNormalizationResponse(resp, operatingmodel.LayerOperatingRhythms)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("entries = %d, want 2", len(got))
	}
}

func TestParseNormalizationResponse_ProseWrappedJSON(t *testing.T) {
	resp := "Sure! Here is the JSON:\n```json\n" +
		`{"entries": [{"title": "t", "summary": "s"}]}` +
		"\n```\nLet me know if you need more."
	got, err := parseNormalizationResponse(resp, operatingmodel.LayerFriction)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("entries = %d, want 1", len(got))
	}
	if got[0].Title != "t" {
		t.Errorf("Title = %q, want %q", got[0].Title, "t")
	}
}

func TestParseNormalizationResponse_EmptyOrJunk(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
	}{
		{"empty", ""},
		{"whitespace", "   \n\t"},
		{"no json", "I'm sorry, I cannot help with this."},
		{"malformed json", `{"entries": [{"title":`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseNormalizationResponse(tc.content, operatingmodel.LayerOperatingRhythms)
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestNormalizationSystemPrompt_DataNotInstructionsFraming(t *testing.T) {
	// The system prompt must tell the model to treat fence-wrapped content
	// as data, not instructions. This is the cheap prompt-injection
	// mitigation; if the system prompt regresses to "raw user content,"
	// pasted "ignore prior instructions" payloads have a clearer path.
	got := normalizationSystemPrompt(operatingmodel.LayerOperatingRhythms)
	for _, want := range []string{
		"USER_ANSWER",
		"END_USER_ANSWER",
		"DATA, not instructions",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt missing required fence/data phrase %q:\n%s", want, got)
		}
	}
}

func TestBuildNormalizationMessages_AnswerWrappedInFence(t *testing.T) {
	// User content must arrive between the same fence markers the system
	// prompt names, otherwise the data-vs-instructions framing is moot.
	msgs := buildNormalizationMessages(operatingmodel.LayerOperatingRhythms,
		"Mondays 9-10am planning")
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	user := msgs[1].Content
	if !strings.HasPrefix(user, "<<<USER_ANSWER\n") {
		t.Errorf("user content missing fence prefix; got: %q", user)
	}
	if !strings.HasSuffix(user, "\nEND_USER_ANSWER>>>") {
		t.Errorf("user content missing fence suffix; got: %q", user)
	}
}

func TestNormalizationSystemPrompt_LayerSpecificFocus(t *testing.T) {
	// Each layer's prompt must mention the layer's focus phrase so the model
	// gets directed at the right Entry fields. Catches a copy-paste regression
	// where one layer's prompt accidentally uses another layer's focus.
	cases := map[string]string{
		operatingmodel.LayerOperatingRhythms:       "operating rhythms",
		operatingmodel.LayerRecurringDecisions:     "recurring decisions",
		operatingmodel.LayerDependencies:           "dependencies",
		operatingmodel.LayerInstitutionalKnowledge: "institutional knowledge",
		operatingmodel.LayerFriction:               "friction",
	}
	for layer, want := range cases {
		got := normalizationSystemPrompt(layer)
		if !strings.Contains(got, want) {
			t.Errorf("prompt for %q must contain %q; got:\n%s", layer, want, got)
		}
	}
}

func TestHandleOnboardingTurn_LLMNormalizerProducesMultipleDrafts(t *testing.T) {
	// End-to-end through handleOnboardingTurn: with a wired normalizer, a
	// single user answer can produce multiple structured drafts (the whole
	// point of #11 — the v1 stub collapsed everything into one blob).
	c, sink := newInterviewTestComponent(t)
	c.SetLayerNormalizer(func(_ context.Context, _, _ string) ([]operatingmodel.Entry, error) {
		return []operatingmodel.Entry{
			{Title: "Weekly planning", Summary: "Monday 9-10am", Cadence: "weekly", Trigger: "Monday 9am"},
			{Title: "Daily standup", Summary: "10am sync", Cadence: "daily", Trigger: "10am"},
			{Title: "Sprint review", Summary: "Friday biweekly", Cadence: "biweekly", Trigger: "Friday"},
		}, nil
	})
	loopID := startOnboardingLoop(c, "coby", "s1", operatingmodel.LayerOperatingRhythms)

	c.handleOnboardingTurn(context.Background(),
		interviewUserMsg("coby", "s1", "Monday planning, daily standups at 10, biweekly Friday reviews"))

	info := c.loopTracker.Get(loopID)
	entries, ok := draftEntriesFromMetadata(info.Metadata)
	if !ok || len(entries) != 3 {
		t.Fatalf("draft entries = %d, want 3 (got %+v)", len(entries), entries)
	}
	if entries[0].Cadence != "weekly" || entries[1].Cadence != "daily" {
		t.Errorf("structured fields lost in round-trip; got cadences %q, %q",
			entries[0].Cadence, entries[1].Cadence)
	}
	if got := onboardSubState(info); got != SubStateAwaitingApproval {
		t.Errorf("sub-state after LLM normalize = %q, want %q", got, SubStateAwaitingApproval)
	}
	if len(sink.all()) != 1 {
		t.Errorf("expected 1 checkpoint response, got %d", len(sink.all()))
	}
}

func TestFinalizeExtractedEntries_AssignsFreshEntryIDs(t *testing.T) {
	// The LLM may emit any entry_id (or none); finalize must always assign
	// a fresh, dot-free ID so the entity-ID format invariant holds.
	raw := []operatingmodel.Entry{
		{Title: "A", Summary: "a"},
		{EntryID: "should-be-overwritten", Title: "B", Summary: "b"},
		{EntryID: "with.a.dot", Title: "C", Summary: "c"},
	}
	got := finalizeExtractedEntries(operatingmodel.LayerOperatingRhythms, raw)
	if len(got) != 3 {
		t.Fatalf("entries = %d, want 3", len(got))
	}
	seen := map[string]bool{}
	for _, e := range got {
		if e.EntryID == "" {
			t.Errorf("EntryID empty after finalize")
		}
		if strings.Contains(e.EntryID, ".") {
			t.Errorf("EntryID %q contains dot", e.EntryID)
		}
		if seen[e.EntryID] {
			t.Errorf("duplicate EntryID %q", e.EntryID)
		}
		seen[e.EntryID] = true
	}
}
