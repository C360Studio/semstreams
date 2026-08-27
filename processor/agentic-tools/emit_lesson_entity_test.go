package agentictools

import (
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

type lessonPredicateObject struct {
	predicate string
	object    string
}

func lessonMultiset(triples []message.Triple) map[lessonPredicateObject]int {
	set := make(map[lessonPredicateObject]int, len(triples))
	for _, triple := range triples {
		object, _ := triple.Object.(string)
		set[lessonPredicateObject{triple.Predicate, object}]++
	}
	return set
}

func goldenLessonMultiset(rows []lessonPredicateObject) map[lessonPredicateObject]int {
	set := make(map[lessonPredicateObject]int, len(rows))
	for _, row := range rows {
		set[row]++
	}
	return set
}

// TestEmitLessonBuildsEntityTriples: for the same arguments, the predicate /
// object multiset from AgentLessonEntity.Triples() equals the multiset the
// former buildEmitLessonTriples produced (golden captured at 08660fc5).
func TestEmitLessonBuildsEntityTriples(t *testing.T) {
	const lessonID = "acme.ops.lesson.agent.record.11111111-1111-5111-8111-111111111111"
	const loopID = "acme.ops.agentic-loop.agent.execution.loop-ops-abc"
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	entity := &agentic.AgentLessonEntity{
		Org: "acme", Platform: "ops", ID: "11111111-1111-5111-8111-111111111111",
		Category: "retention-policy", Polarity: "avoid", Severity: "warning", Status: lessonBornStatus,
		CreatedAt: now,
		Summary:   "cap retention sweeps", Detail: "the detail", InjectionForm: "Cap sweeps.",
		Evidence:     []string{"acme.ops.agentic-loop.agent.execution.loop-1", "acme.ops.agentic-loop.agent.execution.loop-2"},
		AppliesTo:    []string{"tag:go", "id:acme.ops.agent"},
		ObservedRole: "ops", ExecutedBy: loopID,
	}
	if got := entity.EntityID(); got != lessonID {
		t.Fatalf("EntityID() = %q, want %q", got, lessonID)
	}

	golden := []lessonPredicateObject{
		{agvocab.LessonCategory, "retention-policy"},
		{agvocab.LessonPolarity, "avoid"},
		{agvocab.LessonSeverity, "warning"},
		{agvocab.LessonStatus, "proposed"},
		{agvocab.LessonCreatedAt, "2026-08-26T12:00:00Z"},
		{agvocab.LessonSummary, "cap retention sweeps"},
		{agvocab.LessonDetail, "the detail"},
		{agvocab.LessonInjectionForm, "Cap sweeps."},
		{agvocab.LessonEvidence, "acme.ops.agentic-loop.agent.execution.loop-1"},
		{agvocab.LessonEvidence, "acme.ops.agentic-loop.agent.execution.loop-2"},
		{agvocab.LessonAppliesTo, "tag:go"},
		{agvocab.LessonAppliesTo, "id:acme.ops.agent"},
		{agvocab.LessonObservedRole, "ops"},
		{agvocab.ActionExecutedBy, loopID},
	}
	got := entity.Triples()
	if len(got) != len(golden) {
		t.Fatalf("Triples() emitted %d triples, want %d", len(got), len(golden))
	}
	gotSet, wantSet := lessonMultiset(got), goldenLessonMultiset(golden)
	for row, count := range wantSet {
		if gotSet[row] != count {
			t.Errorf("predicate %q object %q: got %d, want %d", row.predicate, row.object, gotSet[row], count)
		}
	}
	for row := range gotSet {
		if _, ok := wantSet[row]; !ok {
			t.Errorf("unexpected triple %q = %q", row.predicate, row.object)
		}
	}
	for _, triple := range got {
		if triple.Subject != lessonID || triple.Source != "ops-emit-lesson" || triple.Confidence != 1.0 {
			t.Errorf("triple %q: subject %q source %q confidence %v", triple.Predicate, triple.Subject, triple.Source, triple.Confidence)
		}
	}

	t.Run("observed role omitted when empty", func(t *testing.T) {
		noRole := *entity
		noRole.ObservedRole = ""
		got := noRole.Triples()
		if len(got) != len(golden)-1 {
			t.Fatalf("Triples() emitted %d triples, want %d", len(got), len(golden)-1)
		}
		if _, present := lessonMultiset(got)[lessonPredicateObject{agvocab.LessonObservedRole, ""}]; present {
			t.Fatal("an empty observed role must not be emitted")
		}
	})
}
