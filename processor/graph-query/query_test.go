package graphquery

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestQuerySubjects_MatchesWhatIsActuallyServed pins gh#822's contract in BOTH
// directions.
//
// One direction alone is not enough, and that is the lesson rather than a
// nicety: a test asserting only "every exported subject is registered" passes
// on an export that omits half the surface — which is exactly how SemSource's
// hand-maintained copy of this list went stale (it omitted graph.query.byName).
// An export that is a subset is worse than no export, because a consumer gate
// built on it reports "no collision" with confidence.
func TestQuerySubjects_MatchesWhatIsActuallyServed(t *testing.T) {
	exported := QuerySubjects()

	require.NotEmpty(t, exported, "the exported surface must not be empty")

	// Direction 1: every exported subject is one the component declares.
	declared := make(map[string]bool, len(querySubjects))
	for _, s := range querySubjects {
		declared[s] = true
	}
	for _, s := range exported {
		assert.True(t, declared[s], "exported subject %q is not declared", s)
	}

	// Direction 2: every declared subject is exported. This is the one that
	// catches a silently truncated export.
	assert.Len(t, exported, len(querySubjects),
		"exported set and declared set differ in size — a subset export makes a consumer collision gate lie")

	// The known-omitted subject from the SemSource incident, named explicitly so
	// a regression that drops it again fails by name rather than by count.
	assert.Contains(t, exported, "graph.query.byName",
		"graph.query.byName is the subject SemSource's hand-maintained copy omitted")
	assert.Contains(t, exported, "graph.query.summary",
		"graph.query.summary is the subject SemSource actually collided on")

	// The contract must not be mutable by a consumer.
	exported[0] = "mutated.by.caller"
	assert.NotEqual(t, "mutated.by.caller", QuerySubjects()[0],
		"QuerySubjects must return a copy — a consumer must not be able to edit the framework's declaration")
}
