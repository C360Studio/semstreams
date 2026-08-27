package lessonmatch

import "testing"

// TestAppliesToThreeSegmentsIsSourceScope documents the meaning the
// agentic-lessons delta assigns to a three-position `id:` key under the
// canonical order: `id:acme.dep1.src` scopes a lesson to ONE SOURCE within one
// deployment (org.platform.system), so a loop scoped to another source of the
// same deployment does not match. Segment-boundary matching is order-agnostic
// (inventory L2); this pins the semantics, not a code change.
func TestAppliesToThreeSegmentsIsSourceScope(t *testing.T) {
	t.Parallel()

	lesson := active("acme.dep1.lesson.agent.record.l1", "info", "", []string{"id:acme.dep1.src"}, "x")
	sameSource := Match([]Lesson{lesson}, Scope{EntityIDs: []string{"acme.dep1.src.git.commit.a1"}}, Opts{})
	if sameSource.MatchedCount != 1 {
		t.Fatalf("same-source loop matched %d lessons, want 1", sameSource.MatchedCount)
	}
	otherSource := Match([]Lesson{lesson}, Scope{EntityIDs: []string{"acme.dep1.other.git.commit.a1"}}, Opts{})
	if otherSource.MatchedCount != 0 {
		t.Fatalf("other-source loop matched %d lessons, want 0 (a three-position key is a source scope, not a taxonomy)", otherSource.MatchedCount)
	}
}
