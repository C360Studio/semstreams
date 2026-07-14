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
	got, err := cr.UnclaimedForeignEdges(ctx, "test.fixture.v1", []string{"test.edge.b", "test.edge.a", "test.edge.a"})
	if err != nil {
		t.Fatalf("UnclaimedForeignEdges (empty): %v", err)
	}
	if len(got) != 2 || got[0] != "test.edge.a" || got[1] != "test.edge.b" {
		t.Fatalf("empty registry: got %v, want [test.edge.a test.edge.b] (deduped, sorted)", got)
	}

	// Register an exact-producer claim on test.edge.claimed and a Producer-empty
	// (any-producer) claim on test.edge.shared.
	if err := reg.RegisterOwner(ctx, Registration{
		Owner: "producer-a",
		ForeignEdges: []ForeignEdgeClaim{
			{Owner: "producer-a", Predicate: "test.edge.claimed", Mode: EdgeNoBirthStub, Producer: "test.fixture.v1"},
		},
	}); err != nil {
		t.Fatalf("register producer-a: %v", err)
	}
	if err := reg.RegisterOwner(ctx, Registration{
		Owner: "any-producer-owner",
		ForeignEdges: []ForeignEdgeClaim{
			{Owner: "any-producer-owner", Predicate: "test.edge.shared", Mode: EdgeNoBirthStub, Producer: ""},
		},
	}); err != nil {
		t.Fatalf("register any-producer-owner: %v", err)
	}

	// Exact producer: test.edge.claimed is covered; test.edge.unclaimed is not.
	got, err = cr.UnclaimedForeignEdges(ctx, "test.fixture.v1", []string{"test.edge.claimed", "test.edge.unclaimed"})
	if err != nil {
		t.Fatalf("UnclaimedForeignEdges (exact): %v", err)
	}
	if len(got) != 1 || got[0] != "test.edge.unclaimed" {
		t.Fatalf("exact producer: got %v, want [test.edge.unclaimed]", got)
	}

	// A DIFFERENT producer does not match producer-a's exact claim, but DOES
	// match the any-producer test.edge.shared claim.
	got, err = cr.UnclaimedForeignEdges(ctx, "other.type.v1", []string{"test.edge.claimed", "test.edge.shared"})
	if err != nil {
		t.Fatalf("UnclaimedForeignEdges (other producer): %v", err)
	}
	if len(got) != 1 || got[0] != "test.edge.claimed" {
		t.Fatalf("other producer: got %v, want [test.edge.claimed] (test.edge.shared covered by any-producer claim)", got)
	}

	// ForeignEdgeMode is the routing seam's mode lookup (routeForeignEdges
	// branches NoBirthStub/Strict/... on it) — prove it resolves the REGISTERED
	// mode against the live epoch, not just claimed/unclaimed. Same claims as
	// above (test.edge.claimed = exact-producer NoBirthStub; test.edge.shared = any-producer
	// NoBirthStub).
	if mode, ok, err := cr.ForeignEdgeMode(ctx, "test.fixture.v1", "test.edge.claimed"); err != nil || !ok || mode != EdgeNoBirthStub {
		t.Fatalf("ForeignEdgeMode(exact test.edge.claimed) = %q,%v,%v; want %q,true,nil", mode, ok, err, EdgeNoBirthStub)
	}
	if mode, ok, err := cr.ForeignEdgeMode(ctx, "test.fixture.v1", "test.edge.unclaimed"); err != nil || ok {
		t.Fatalf("ForeignEdgeMode(test.edge.unclaimed) = %q,%v,%v; want \"\",false,nil (no covering claim)", mode, ok, err)
	}
	// any-producer claim: a DIFFERENT producer still matches the Producer-empty claim.
	if mode, ok, err := cr.ForeignEdgeMode(ctx, "other.type.v1", "test.edge.shared"); err != nil || !ok || mode != EdgeNoBirthStub {
		t.Fatalf("ForeignEdgeMode(any-producer test.edge.shared) = %q,%v,%v; want %q,true,nil", mode, ok, err, EdgeNoBirthStub)
	}
	// exact-producer claim does NOT match a different producer.
	if mode, ok, err := cr.ForeignEdgeMode(ctx, "other.type.v1", "test.edge.claimed"); err != nil || ok {
		t.Fatalf("ForeignEdgeMode(other producer, exact test.edge.claimed) = %q,%v,%v; want \"\",false,nil (exact-producer miss)", mode, ok, err)
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
