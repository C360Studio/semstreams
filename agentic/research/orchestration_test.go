package research

import (
	"testing"
	"time"

	"github.com/c360studio/semstreams/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixedTime gives the table tests a deterministic timestamp so the
// RFC3339Nano-formatted predicate values stay reproducible regardless
// of where the suite runs.
var fixedTime = time.Date(2026, 6, 2, 18, 30, 45, 123456789, time.UTC)

const fixedTimeRFC = "2026-06-02T18:30:45.123456789Z"

const testLoopEntityID = "acme.ops.agent.agentic-loop.execution.rg_abc12345"

// assertSharedSubject is the cross-cutting atomicity assertion: every
// builder must return triples that all share one Subject so graph-ingest's
// per-Subject CAS batch path keeps the chain's rule-matching contract
// intact under partial-write conditions.
func assertSharedSubject(t *testing.T, triples []message.Triple) {
	t.Helper()
	require.NotEmpty(t, triples)
	first := triples[0].Subject
	for _, tr := range triples[1:] {
		assert.Equal(t, first, tr.Subject, "all triples in a stage's batch must share Subject for atomic landing")
	}
}

// byPredicate flattens a triple batch into a predicate→object map for
// assertion convenience. The batches are small (2-3 triples each), so
// quadratic indexing is fine.
func byPredicate(triples []message.Triple) map[string]any {
	out := make(map[string]any, len(triples))
	for _, tr := range triples {
		out[tr.Predicate] = tr.Object
	}
	return out
}

// TestBuildClassifyCompleteTriples_HappyPath locks the wire contract
// R1 reads: complete + candidate_count + degraded triples, all sharing
// the loop entity Subject. Atomicity is load-bearing — a fresh complete
// paired with a stale candidate_count would silently corrupt operator
// dashboards.
func TestBuildClassifyCompleteTriples_HappyPath(t *testing.T) {
	triples := BuildClassifyCompleteTriples(testLoopEntityID, 12, false, fixedTime)
	require.Len(t, triples, 3)
	assertSharedSubject(t, triples)
	for _, tr := range triples {
		assert.Equal(t, testLoopEntityID, tr.Subject)
		assert.Equal(t, SourceClassify, tr.Source)
		assert.Equal(t, fixedTime, tr.Timestamp)
		assert.Equal(t, 1.0, tr.Confidence)
	}
	got := byPredicate(triples)
	assert.Equal(t, fixedTimeRFC, got[PredicateResearchClassifyComplete])
	assert.Equal(t, "12", got[PredicateResearchClassifyCandidateCount])
	assert.Equal(t, "false", got[PredicateResearchClassifyDegraded])
}

func TestBuildClassifyCompleteTriples_DegradedTrue(t *testing.T) {
	triples := BuildClassifyCompleteTriples(testLoopEntityID, 0, true, fixedTime)
	got := byPredicate(triples)
	assert.Equal(t, "true", got[PredicateResearchClassifyDegraded], "degraded path must stamp \"true\" for R0/R1 quality filtering")
}

func TestBuildRouteCompleteTriples_AllFourActions(t *testing.T) {
	for _, action := range []string{
		ActionSynthesizeDirectly,
		ActionRetighten,
		ActionWalkSeeds,
		ActionDecompose,
	} {
		t.Run(action, func(t *testing.T) {
			triples := BuildRouteCompleteTriples(testLoopEntityID, action, fixedTime)
			require.Len(t, triples, 2)
			assertSharedSubject(t, triples)
			got := byPredicate(triples)
			assert.Equal(t, fixedTimeRFC, got[PredicateResearchRouteComplete])
			assert.Equal(t, action, got[PredicateResearchRouteAction], "R2's action-level when clauses match on the exact action string")
		})
	}
}

func TestBuildExecuteCompleteTriples(t *testing.T) {
	triples := BuildExecuteCompleteTriples(testLoopEntityID, 7, fixedTime)
	require.Len(t, triples, 2)
	assertSharedSubject(t, triples)
	got := byPredicate(triples)
	assert.Equal(t, fixedTimeRFC, got[PredicateResearchExecuteComplete])
	assert.Equal(t, "7", got[PredicateResearchExecuteEvidenceCount])
}

func TestBuildAssessCompleteTriples_SufficientPaths(t *testing.T) {
	for _, tc := range []struct {
		name       string
		sufficient bool
		want       string
	}{
		{"sufficient_true", true, "true"},
		{"sufficient_false", false, "false"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			triples := BuildAssessCompleteTriples(testLoopEntityID, tc.sufficient, fixedTime)
			got := byPredicate(triples)
			assert.Equal(t, tc.want, got[PredicateResearchAssessSufficient], "R4 branches on the exact \"true\"/\"false\" string")
		})
	}
}

func TestBuildSearchResultCompleteTriples(t *testing.T) {
	triples := BuildSearchResultCompleteTriples(
		testLoopEntityID,
		"search_result.complete.rg_abc12345",
		fixedTime,
	)
	require.Len(t, triples, 2)
	assertSharedSubject(t, triples)
	got := byPredicate(triples)
	assert.Equal(t, fixedTimeRFC, got[PredicateResearchSearchResultComplete])
	assert.Equal(t, "search_result.complete.rg_abc12345", got[PredicateResearchSearchResultRef])
}
