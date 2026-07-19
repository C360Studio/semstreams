package agenticloop

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/processor/agentic-loop/lessonmatch"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

// fakeLessonReader is the minimal in-process LessonReader for handler-level
// brief-assembly tests. It returns a fixed candidate slice (all statuses) so
// the handler's matcher performs the active-only exclusion, exactly as the
// production prefix-query reader does.
type fakeLessonReader struct {
	lessons []lessonmatch.Lesson
	err     error
	calls   int
}

func (f *fakeLessonReader) ReadLessons(_ context.Context, _ string) ([]lessonmatch.Lesson, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.lessons, nil
}

func lessonHandler(reader LessonReader) *MessageHandler {
	return &MessageHandler{
		config:       Config{},
		platform:     todoTestPlatform(),
		lessonReader: reader,
		logger:       todoTestLogger(),
		// promptRegistry deliberately nil ⇒ base prompt "" ⇒ assembleSystemPrompt
		// returns exactly the lesson block, so tests assert on it directly.
	}
}

func opsTask() TaskMessage {
	return TaskMessage{TaskID: "t1", Role: "ops", Model: "m", Prompt: "go"}
}

// activeLesson is a terse constructor for a fixture lesson entity projection.
func activeLesson(id, sev, createdAt, form string, appliesTo ...string) lessonmatch.Lesson {
	return lessonmatch.Lesson{
		EntityID:      id,
		Status:        "active",
		Severity:      sev,
		CreatedAt:     createdAt,
		AppliesTo:     appliesTo,
		InjectionForm: form,
	}
}

// --- Spec: Matching active lessons arrive in the brief ---

func TestAssembleSystemPrompt_InjectsMatchingActiveLessons(t *testing.T) {
	reader := &fakeLessonReader{lessons: []lessonmatch.Lesson{
		activeLesson("acme.ops.agent.lesson.record.a", "warning", "2026-07-19T10:00:00Z", "Scope deletes to COMPLETE_* keys.", "tag:ops"),
		activeLesson("acme.ops.agent.lesson.record.b", "info", "2026-07-19T09:00:00Z", "Prefer KV watch for facts.", "tag:ops"),
	}}
	h := lessonHandler(reader)

	got := h.assembleSystemPrompt(context.Background(), opsTask())

	if reader.calls != 1 {
		t.Errorf("reader called %d times, want 1", reader.calls)
	}
	for _, want := range []string{
		"Scope deletes to COMPLETE_* keys.",
		"acme.ops.agent.lesson.record.a",
		"Prefer KV watch for facts.",
		"acme.ops.agent.lesson.record.b",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("brief missing %q\nblock:\n%s", want, got)
		}
	}
	// Both injection forms carry their entity ID (governed dereference).
	if !strings.Contains(got, "matched 2, showing 2") {
		t.Errorf("header must state counts; block:\n%s", got)
	}
}

// --- Spec: Proposed lesson is not injected ---

func TestAssembleSystemPrompt_ProposedLessonNotInjected(t *testing.T) {
	proposed := activeLesson("acme.ops.agent.lesson.record.p", "critical", "2026-07-19T10:00:00Z", "should not appear", "tag:ops")
	proposed.Status = "proposed"
	h := lessonHandler(&fakeLessonReader{lessons: []lessonmatch.Lesson{proposed}})

	got := h.assembleSystemPrompt(context.Background(), opsTask())
	if got != "" {
		t.Errorf("proposed lesson must not be injected; got:\n%s", got)
	}
}

// A retired lesson likewise leaves the brief (matcher exclusion, reader path).
func TestAssembleSystemPrompt_RetiredLessonNotInjected(t *testing.T) {
	retired := activeLesson("acme.ops.agent.lesson.record.r", "critical", "2026-07-19T10:00:00Z", "should not appear", "tag:ops")
	retired.Status = "retired"
	h := lessonHandler(&fakeLessonReader{lessons: []lessonmatch.Lesson{retired}})

	if got := h.assembleSystemPrompt(context.Background(), opsTask()); got != "" {
		t.Errorf("retired lesson must not be injected; got:\n%s", got)
	}
}

// --- Spec: Delivery is bounded and observable ---

func TestAssembleSystemPrompt_BoundedAndObservable(t *testing.T) {
	// 12 matching active lessons; default K=10 ⇒ 10 shown, header states 12/10.
	var lessons []lessonmatch.Lesson
	for i := 0; i < 12; i++ {
		id := "acme.ops.agent.lesson.record." + string(rune('a'+i))
		lessons = append(lessons, activeLesson(id, "info", "", "form-"+string(rune('a'+i)), "tag:ops"))
	}
	h := lessonHandler(&fakeLessonReader{lessons: lessons})

	got := h.assembleSystemPrompt(context.Background(), opsTask())
	if !strings.Contains(got, "matched 12, showing 10") {
		t.Errorf("block must state matched-vs-included counts (12/10); block:\n%s", got)
	}
	if n := strings.Count(got, "\n- "); n != 10 {
		t.Errorf("rendered %d lesson lines, want 10 (K bound)", n)
	}
}

// --- Spec: Ordering is replay-stable (created-at, not KV revision/UpdatedAt) ---

func TestAssembleSystemPrompt_OrderingReplayStableAcrossReads(t *testing.T) {
	// Entity IDs are inserted in an order that CONTRADICTS the created-at order,
	// so a stable result proves the order derives from created-at (the immutable
	// birth triple), not insertion order / KV revision / UpdatedAt.
	lessons := []lessonmatch.Lesson{
		activeLesson("acme.ops.agent.lesson.record.zzz", "warning", "2026-07-19T12:00:00Z", "newest", "tag:ops"), // newest
		activeLesson("acme.ops.agent.lesson.record.aaa", "warning", "2026-07-19T08:00:00Z", "oldest", "tag:ops"), // oldest
		activeLesson("acme.ops.agent.lesson.record.mmm", "warning", "2026-07-19T10:00:00Z", "middle", "tag:ops"),
	}
	h := lessonHandler(&fakeLessonReader{lessons: lessons})

	first := h.assembleSystemPrompt(context.Background(), opsTask())
	second := h.assembleSystemPrompt(context.Background(), opsTask())
	if first != second {
		t.Fatalf("same scope over unchanged candidates must inject identically\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	// created-at DESC: newest → middle → oldest, DESPITE entity-ID ASC being the
	// opposite (aaa < mmm < zzz).
	iNew := strings.Index(first, "newest")
	iMid := strings.Index(first, "middle")
	iOld := strings.Index(first, "oldest")
	if !(iNew < iMid && iMid < iOld) {
		t.Errorf("order must derive from created-at DESC (newest, middle, oldest), not entity-ID; block:\n%s", first)
	}
}

// --- Nil-safety / back-compat and scope gating ---

func TestAssembleSystemPrompt_NoReaderNoInjection(t *testing.T) {
	h := &MessageHandler{config: Config{}, platform: todoTestPlatform(), logger: todoTestLogger()}
	if got := h.assembleSystemPrompt(context.Background(), opsTask()); got != "" {
		t.Errorf("no reader must yield no injection; got:\n%s", got)
	}
}

func TestAssembleSystemPrompt_EmptyRoleSkipsRead(t *testing.T) {
	reader := &fakeLessonReader{lessons: []lessonmatch.Lesson{
		activeLesson("acme.ops.agent.lesson.record.a", "warning", "2026-07-19T10:00:00Z", "x", "tag:ops"),
	}}
	h := lessonHandler(reader)
	task := opsTask()
	task.Role = "" // empty scope ⇒ never a firehose; skip the read entirely

	if got := h.assembleSystemPrompt(context.Background(), task); got != "" {
		t.Errorf("empty role must yield no injection; got:\n%s", got)
	}
	if reader.calls != 0 {
		t.Errorf("empty scope must skip the read; reader called %d times", reader.calls)
	}
}

func TestAssembleSystemPrompt_ReadFailureFailsOpen(t *testing.T) {
	h := lessonHandler(&fakeLessonReader{err: errors.New("graph gateway transient")})
	if got := h.assembleSystemPrompt(context.Background(), opsTask()); got != "" {
		t.Errorf("read failure must fail open (no block), not panic; got:\n%s", got)
	}
}

func TestAssembleSystemPrompt_MissingPlatformSkipsInjection(t *testing.T) {
	h := &MessageHandler{
		config:       Config{},
		lessonReader: &fakeLessonReader{lessons: []lessonmatch.Lesson{activeLesson("acme.ops.agent.lesson.record.a", "warning", "", "x", "tag:ops")}},
		logger:       todoTestLogger(),
		// platform intentionally zero-valued
	}
	if got := h.assembleSystemPrompt(context.Background(), opsTask()); got != "" {
		t.Errorf("zero platform must skip lesson injection; got:\n%s", got)
	}
}

// The lesson block appends AFTER the persona base (both present ⇒ separated).
func TestAssembleSystemPrompt_AppendsAfterBase(t *testing.T) {
	block := renderLessonBlock(lessonmatch.Result{
		Included:      []lessonmatch.MatchedLesson{{EntityID: "acme.ops.agent.lesson.record.a", InjectionForm: "do the thing"}},
		MatchedCount:  1,
		IncludedCount: 1,
	})
	base := "You are the ops agent."
	joined := joinPromptSections(base, block)
	if !strings.HasPrefix(joined, base) {
		t.Errorf("base must come first; joined:\n%s", joined)
	}
	if !strings.Contains(joined, "do the thing") {
		t.Errorf("lesson block must be appended; joined:\n%s", joined)
	}
	if !strings.Contains(joined, "\n\n") {
		t.Errorf("sections must be blank-line separated; joined:\n%s", joined)
	}
}

// --- Observability: matched/included counter records on the injection step ---

func TestAssembleSystemPrompt_RecordsInjectionMetric(t *testing.T) {
	m := getMetrics(nil)
	beforeMatched := testutil.ToFloat64(m.lessonInjection.WithLabelValues("matched"))
	beforeIncluded := testutil.ToFloat64(m.lessonInjection.WithLabelValues("included"))

	// 3 matching active lessons; DefaultK=10 > 3 ⇒ matched 3 / included 3 (both
	// counters advance by 3, proving the injection step records observability).
	h := lessonHandler(&fakeLessonReader{lessons: []lessonmatch.Lesson{
		activeLesson("acme.ops.agent.lesson.record.a", "critical", "2026-07-19T10:00:03Z", "a", "tag:ops"),
		activeLesson("acme.ops.agent.lesson.record.b", "critical", "2026-07-19T10:00:02Z", "b", "tag:ops"),
		activeLesson("acme.ops.agent.lesson.record.c", "critical", "2026-07-19T10:00:01Z", "c", "tag:ops"),
	}})
	h.metrics = m
	_ = h.assembleSystemPrompt(context.Background(), opsTask())

	afterMatched := testutil.ToFloat64(m.lessonInjection.WithLabelValues("matched"))
	afterIncluded := testutil.ToFloat64(m.lessonInjection.WithLabelValues("included"))
	if d := afterMatched - beforeMatched; d != 3 {
		t.Errorf("matched counter delta = %v, want 3", d)
	}
	if d := afterIncluded - beforeIncluded; d != 3 {
		t.Errorf("included counter delta = %v, want 3", d)
	}
}

// --- FIX A: page-cap exhaustion emits an operator Warn (no silent truncation) ---

func TestWarnIfLessonPageCapHit(t *testing.T) {
	t.Run("warns when cursor still non-empty (cap hit ⇒ partial coverage)", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
		warnIfLessonPageCapHit(logger, "acme.ops.agent.lesson.record", "still-more", maxLessonPages, 16000)
		out := buf.String()
		if !strings.Contains(out, "lesson page cap hit") {
			t.Errorf("expected page-cap Warn, got: %q", out)
		}
		if !strings.Contains(out, "\"level\":\"WARN\"") {
			t.Errorf("must log at WARN level, got: %q", out)
		}
		if !strings.Contains(out, "\"cap\":16") {
			t.Errorf("Warn must include the page cap, got: %q", out)
		}
	})

	t.Run("silent when cursor empty (pagination completed ⇒ full coverage)", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
		warnIfLessonPageCapHit(logger, "acme.ops.agent.lesson.record", "", 3, 42)
		if buf.Len() != 0 {
			t.Errorf("exhausted pagination must not warn, got: %q", buf.String())
		}
	})
}

// --- projectLessonEntity: triple → matcher projection ---

func TestProjectLessonEntity(t *testing.T) {
	es := &graph.EntityState{
		ID: "acme.ops.agent.lesson.record.a",
		Triples: []message.Triple{
			{Predicate: agvocab.LessonStatus, Object: "active"},
			{Predicate: agvocab.LessonSeverity, Object: "warning"},
			{Predicate: agvocab.LessonCreatedAt, Object: "2026-07-19T10:00:00Z"},
			{Predicate: agvocab.LessonInjectionForm, Object: "do the thing"},
			{Predicate: agvocab.LessonAppliesTo, Object: "tag:ops"},
			{Predicate: agvocab.LessonAppliesTo, Object: "id:acme.ops.robotics"},
			{Predicate: agvocab.LessonSummary, Object: "ignored"},
		},
	}
	got := projectLessonEntity(es)
	if got.EntityID != es.ID || got.Status != "active" || got.Severity != "warning" ||
		got.CreatedAt != "2026-07-19T10:00:00Z" || got.InjectionForm != "do the thing" {
		t.Errorf("projection mismatch: %+v", got)
	}
	if len(got.AppliesTo) != 2 || got.AppliesTo[0] != "tag:ops" || got.AppliesTo[1] != "id:acme.ops.robotics" {
		t.Errorf("applies_to projection = %v, want [tag:ops id:acme.ops.robotics]", got.AppliesTo)
	}
}
