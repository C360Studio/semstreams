package readiness

import (
	"testing"

	"github.com/c360studio/semstreams/graph"
)

// TestBucketGraphStatus_MatchesGraphSourceOfTruth is the drift guard for
// framework-owned-bucket-guards F3: readiness.BucketGraphStatus is a re-export of
// graph.BucketGraphStatus (the single source of truth that also drives the
// write-ownership guard). If a future edit un-aliases readiness onto its own
// literal, the write-protected name and the name producers/consumers use here
// would silently diverge — a rule update_kv could then forge readiness into a
// bucket the guard no longer recognizes. This test fails closed on that drift.
func TestBucketGraphStatus_MatchesGraphSourceOfTruth(t *testing.T) {
	t.Parallel()
	if BucketGraphStatus != graph.BucketGraphStatus {
		t.Fatalf("readiness.BucketGraphStatus (%q) must equal graph.BucketGraphStatus (%q) — the write-ownership guard keys on the graph constant",
			BucketGraphStatus, graph.BucketGraphStatus)
	}
}
