package agenticmemory

import (
	"context"
	"strings"
	"testing"
	"time"

	operatingmodel "github.com/c360studio/semstreams/agentic/operating-model"
)

func sampleLessons() []operatingmodel.Lesson {
	now := time.Now().UTC()
	return []operatingmodel.Lesson{
		{LessonID: "l1", Summary: "Always cache embedding lookups",
			SessionID: "loop-x", LearnedAt: now.Add(-1 * time.Hour)},
		{LessonID: "l2", Summary: "User prefers terse summaries",
			SessionID: "loop-y", LearnedAt: now.Add(-2 * time.Hour)},
	}
}

func TestSplitTokenBudget_Default(t *testing.T) {
	lessons, om := splitTokenBudget(800)
	if lessons != 200 {
		t.Errorf("lessons share = %d, want 200 (25%% of 800)", lessons)
	}
	if om != 600 {
		t.Errorf("om share = %d, want 600", om)
	}
	if lessons+om != 800 {
		t.Errorf("split should sum to total: %d + %d != 800", lessons, om)
	}
}

func TestSplitTokenBudget_Edge(t *testing.T) {
	for _, tc := range []struct {
		name      string
		total     int
		wantSplit bool // when true, both shares > 0
	}{
		{"zero passes through", 0, false},
		{"negative passes through", -100, false},
		{"tiny budget gets minimum", 4, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lessons, om := splitTokenBudget(tc.total)
			if !tc.wantSplit {
				if lessons != 0 || om != 0 {
					t.Errorf("zero/negative total should produce 0/0 split, got %d/%d", lessons, om)
				}
				return
			}
			if lessons < 1 || om < 1 {
				t.Errorf("tiny budget should give both at least 1: got %d/%d", lessons, om)
			}
		})
	}
}

func TestAssembleProfileContext_PopulatesBothSlicesWhenInputProvided(t *testing.T) {
	in := ProfileContextInputs{
		UserID:      "coby",
		LoopID:      "loop-abc",
		Entries:     sampleEntries(),
		Lessons:     sampleLessons(),
		TokenBudget: 800,
	}
	got := AssembleProfileContext(in)

	if got.OperatingModel.EntryCount == 0 {
		t.Error("operating-model slice empty despite entries provided")
	}
	if got.LessonsLearned.EntryCount == 0 {
		t.Error("lessons_learned slice empty despite lessons provided")
	}
	if !strings.Contains(got.LessonsLearned.Content, "Always cache embedding") {
		t.Errorf("expected first lesson summary in rendered content; got:\n%s",
			got.LessonsLearned.Content)
	}

	// Sum of token counts must respect total budget (Validate enforces this
	// directly; we double-check here for redundancy).
	used := got.OperatingModel.TokenCount + got.LessonsLearned.TokenCount
	if used > got.TokenBudget {
		t.Errorf("slices exceed total budget: used=%d budget=%d", used, got.TokenBudget)
	}
	if err := got.Validate(); err != nil {
		t.Errorf("assembled payload fails Validate: %v", err)
	}
}

func TestAssembleProfileContext_SystemPromptPreambleIncludesBothSections(t *testing.T) {
	got := AssembleProfileContext(ProfileContextInputs{
		UserID:      "coby",
		LoopID:      "loop-abc",
		Entries:     sampleEntries(),
		Lessons:     sampleLessons(),
		TokenBudget: 800,
	})
	preamble := got.SystemPromptPreamble()
	if !strings.Contains(preamble, "## How this user works") {
		t.Errorf("missing operating-model section in preamble:\n%s", preamble)
	}
	if !strings.Contains(preamble, "## Lessons from prior sessions") {
		t.Errorf("missing lessons section in preamble:\n%s", preamble)
	}
	// Operating-model section should come first.
	idxOM := strings.Index(preamble, "## How this user works")
	idxLessons := strings.Index(preamble, "## Lessons from prior sessions")
	if idxOM > idxLessons {
		t.Errorf("operating-model should render before lessons; got OM at %d, lessons at %d",
			idxOM, idxLessons)
	}
}

func TestAssembleProfileContext_EmptyLessonsLeavesSliceEmpty(t *testing.T) {
	got := AssembleProfileContext(ProfileContextInputs{
		UserID:      "coby",
		LoopID:      "loop-abc",
		Entries:     sampleEntries(),
		TokenBudget: 800,
	})
	if got.LessonsLearned.Content != "" {
		t.Errorf("expected empty lessons content, got %q", got.LessonsLearned.Content)
	}
	if got.LessonsLearned.EntryCount != 0 {
		t.Errorf("expected 0 lesson entries, got %d", got.LessonsLearned.EntryCount)
	}
}

func TestAssembleProfileContext_LessonsBudgetTruncates(t *testing.T) {
	// 12 long lessons against a tight budget — only the first few should
	// render.
	lessons := make([]operatingmodel.Lesson, 12)
	now := time.Now().UTC()
	for i := range lessons {
		lessons[i] = operatingmodel.Lesson{
			LessonID:  "l-" + string(rune('a'+i)),
			Summary:   strings.Repeat("a long lesson body. ", 8), // ~30 tokens each
			LearnedAt: now.Add(time.Duration(-i) * time.Hour),
		}
	}
	got := AssembleProfileContext(ProfileContextInputs{
		UserID:      "coby",
		LoopID:      "loop-abc",
		Lessons:     lessons,
		TokenBudget: 200, // 50 tokens for lessons share
	})
	if got.LessonsLearned.EntryCount == len(lessons) {
		t.Errorf("expected truncation, got all %d lessons rendered", len(lessons))
	}
	if got.LessonsLearned.EntryCount == 0 {
		t.Errorf("at-least-one contract violated: 0 lessons rendered")
	}
}

func TestPersistCompactionAsLesson_NoOpWhenUserIDAbsent(t *testing.T) {
	// No UserID → no lesson published. Verified indirectly: we set up a
	// component without NATS and assert no panic, no errors counted, no
	// log spam (handler logs at Debug at most).
	c := newProfileContextTestComponent(t, stubProfileReader{})
	c.persistCompactionAsLesson(context.Background(), ContextEvent{
		Type:    "compaction_complete",
		LoopID:  "loop-abc",
		Summary: "we learned a thing",
		// UserID intentionally empty
	})
	// No assertion on output beyond "did not panic, did not error". The
	// real check is in the integration suite where graph-ingest can verify
	// no triple landed.
}

func TestPersistCompactionAsLesson_NoOpWhenSummaryEmpty(t *testing.T) {
	c := newProfileContextTestComponent(t, stubProfileReader{})
	c.persistCompactionAsLesson(context.Background(), ContextEvent{
		Type:   "compaction_complete",
		LoopID: "loop-abc",
		UserID: "coby",
		// Summary intentionally empty
	})
}

func TestRenderLessonsSlice_AtLeastOneRendersEvenIfOversize(t *testing.T) {
	// Matches the renderOperatingModelSlice contract: a single oversized
	// lesson should still render rather than producing empty output.
	huge := operatingmodel.Lesson{
		LessonID:  "l-big",
		Summary:   strings.Repeat("x", 4000),
		LearnedAt: time.Now().UTC(),
	}
	content, count, _ := renderLessonsSlice([]operatingmodel.Lesson{huge}, 50)
	if count != 1 {
		t.Errorf("count = %d, want 1 (at-least-one contract)", count)
	}
	if content == "" {
		t.Errorf("content should be non-empty even when oversized")
	}
}
