//go:build integration

package ownership

import (
	"context"
	"testing"

	"github.com/c360studio/semstreams/natsclient"
)

// ClaimReader classifies a producer's foreign-edge predicates against the live
// epoch: exact-producer match wins, a Producer-empty claim is the any-producer
// fallback, an empty/absent registry means everything is unclaimed, and an
// absent bucket errors so the caller graceful-skips.
func TestIntegration_ClaimReader_Classification(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	ctx := context.Background()

	reg, err := EnsureBuckets(ctx, tc.Client, nil, nil)
	if err != nil {
		t.Fatalf("EnsureBuckets: %v", err)
	}

	cr, err := NewClaimReader(ctx, tc.Client, nil)
	if err != nil {
		t.Fatalf("NewClaimReader: %v", err)
	}

	// Empty registry → everything is unclaimed (deduped, sorted).
	got, err := cr.UnclaimedForeignEdges(ctx, "test.fixture.v1", []string{"b.edge", "a.edge", "a.edge"})
	if err != nil {
		t.Fatalf("UnclaimedForeignEdges (empty): %v", err)
	}
	if len(got) != 2 || got[0] != "a.edge" || got[1] != "b.edge" {
		t.Fatalf("empty registry: got %v, want [a.edge b.edge] (deduped, sorted)", got)
	}

	// Register an exact-producer claim on claimed.edge and a Producer-empty
	// (any-producer) claim on shared.edge.
	if err := reg.RegisterOwner(ctx, Registration{
		Owner: "producer-a",
		ForeignEdges: []ForeignEdgeClaim{
			{Owner: "producer-a", Predicate: "claimed.edge", Mode: EdgeNoBirthStub, Producer: "test.fixture.v1"},
		},
	}); err != nil {
		t.Fatalf("register producer-a: %v", err)
	}
	if err := reg.RegisterOwner(ctx, Registration{
		Owner: "any-producer-owner",
		ForeignEdges: []ForeignEdgeClaim{
			{Owner: "any-producer-owner", Predicate: "shared.edge", Mode: EdgeNoBirthStub, Producer: ""},
		},
	}); err != nil {
		t.Fatalf("register any-producer-owner: %v", err)
	}

	// Exact producer: claimed.edge is covered; unclaimed.edge is not.
	got, err = cr.UnclaimedForeignEdges(ctx, "test.fixture.v1", []string{"claimed.edge", "unclaimed.edge"})
	if err != nil {
		t.Fatalf("UnclaimedForeignEdges (exact): %v", err)
	}
	if len(got) != 1 || got[0] != "unclaimed.edge" {
		t.Fatalf("exact producer: got %v, want [unclaimed.edge]", got)
	}

	// A DIFFERENT producer does not match producer-a's exact claim, but DOES
	// match the any-producer shared.edge claim.
	got, err = cr.UnclaimedForeignEdges(ctx, "other.type.v1", []string{"claimed.edge", "shared.edge"})
	if err != nil {
		t.Fatalf("UnclaimedForeignEdges (other producer): %v", err)
	}
	if len(got) != 1 || got[0] != "claimed.edge" {
		t.Fatalf("other producer: got %v, want [claimed.edge] (shared.edge covered by any-producer claim)", got)
	}
}

// NewClaimReader on a deploy that never created OWNER_CLAIMS returns an error so
// the caller (graph-ingest) graceful-skips classification.
func TestIntegration_ClaimReader_AbsentBucketErrors(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	if _, err := NewClaimReader(context.Background(), tc.Client, nil); err == nil {
		t.Fatal("NewClaimReader must error when OWNER_CLAIMS is absent (graceful-skip signal)")
	}
}
