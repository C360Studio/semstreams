package sensorml

import (
	"testing"

	"github.com/c360studio/semstreams/vocabulary"
)

// ADR-056 Decision-4 BLOCKING-B part 4: PredHosts/PredIsHostedBy are registered
// as mutual inverses (predicates.go init()), so GetInversePredicate resolves both
// ways — the Backfill recoverability floor for the foreign isHostedBy edge.
// Before the fix it returned "" (the false backstop the original design rested
// on), even though the constant doc-comment asserted the inverse.
func TestHostsIsHostedByMutualInverse(t *testing.T) {
	if got := vocabulary.GetInversePredicate(PredHosts); got != PredIsHostedBy {
		t.Errorf("GetInversePredicate(%q) = %q, want %q", PredHosts, got, PredIsHostedBy)
	}
	if got := vocabulary.GetInversePredicate(PredIsHostedBy); got != PredHosts {
		t.Errorf("GetInversePredicate(%q) = %q, want %q", PredIsHostedBy, got, PredHosts)
	}
}
