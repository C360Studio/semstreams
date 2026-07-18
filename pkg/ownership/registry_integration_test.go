//go:build integration

package ownership

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/c360studio/semstreams/natsclient"
)

// newTestRegistry spins up a real NATS KV (testcontainer) with the two
// ownership buckets and returns a Registry plus the claims KVStore (so tests
// can read the epoch back through the in-package helpers).
func newTestRegistry(t *testing.T) (*Registry, *natsclient.KVStore, context.Context) {
	t.Helper()
	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	ctx := context.Background()

	claimsBucket, err := tc.CreateKVBucket(ctx, BucketOwnerClaims)
	if err != nil {
		t.Fatalf("create OWNER_CLAIMS: %v", err)
	}
	presenceBucket, err := tc.CreateKVBucket(ctx, BucketOwnerPresence)
	if err != nil {
		t.Fatalf("create OWNER_PRESENCE: %v", err)
	}
	claims := tc.Client.NewKVStore(claimsBucket)
	presence := tc.Client.NewKVStore(presenceBucket)
	return NewRegistry(claims, presence, nil), claims, ctx
}

// readEpoch reads and decodes the current epoch for assertions.
func readEpoch(t *testing.T, claims *natsclient.KVStore, ctx context.Context) *epoch {
	t.Helper()
	entry, err := claims.Get(ctx, registryKey)
	if err != nil {
		if errors.Is(err, natsclient.ErrKVKeyNotFound) {
			return newEpoch()
		}
		t.Fatalf("read epoch: %v", err)
	}
	ep, err := decodeEpoch(entry.Value)
	if err != nil {
		t.Fatalf("decode epoch: %v", err)
	}
	return ep
}

func TestRegistry_RegisterAndReject(t *testing.T) {
	r, claims, ctx := newTestRegistry(t)

	if err := r.RegisterOwner(ctx, Registration{Owner: "cs-api", Claims: []OwnerClaim{OwnerClaim{Owner: "cs-api", Pattern: sysPat, Mode: ModeReplaceOwned, Predicates: []string{"sensorml.process.label"}}}}); err != nil {
		t.Fatalf("first registration should succeed: %v", err)
	}

	// Overlapping owner on the same cell → reject, epoch unchanged.
	err := r.RegisterOwner(ctx, Registration{Owner: "other", Claims: []OwnerClaim{OwnerClaim{Owner: "other", Pattern: sysPat, Mode: ModeReplaceOwned, Predicates: []string{"sensorml.process.label"}}}})
	if !errors.Is(err, ErrOwnershipOverlap) {
		t.Fatalf("overlapping registration should fail with ErrOwnershipOverlap, got %v", err)
	}
	var oe *OverlapError
	if !errors.As(err, &oe) || oe.With != "cs-api" {
		t.Errorf("OverlapError should name incumbent cs-api, got %+v", oe)
	}

	// Disjoint id-space → allowed.
	if err := r.RegisterOwner(ctx, Registration{Owner: "other", Claims: []OwnerClaim{OwnerClaim{Owner: "other", Pattern: depPat, Mode: ModeReplaceOwned, Predicates: []string{"sensorml.process.label"}}}}); err != nil {
		t.Fatalf("disjoint registration should succeed: %v", err)
	}

	ep := readEpoch(t, claims, ctx)
	if _, ok := ep.Owners["cs-api"]; !ok {
		t.Error("cs-api should be in the epoch")
	}
	if _, ok := ep.Owners["other"]; !ok {
		t.Error("other should be in the epoch (disjoint claim landed)")
	}
	if ep.Version < 2 {
		t.Errorf("epoch version should have advanced past 2 successful registrations, got %d", ep.Version)
	}
}

func TestRegistry_Idempotent(t *testing.T) {
	r, _, ctx := newTestRegistry(t)
	entity := "c360.semconnect.systems.csapi.system.drone-001"

	if err := r.RegisterOwner(ctx, Registration{Owner: "cs-api", Claims: []OwnerClaim{OwnerClaim{Owner: "cs-api", Pattern: sysPat, Mode: ModeReplaceOwned, Predicates: []string{"test.value.a"}}}}); err != nil {
		t.Fatal(err)
	}
	// Re-register the SAME owner with a different predicate set → replaces, no overlap.
	if err := r.RegisterOwner(ctx, Registration{Owner: "cs-api", Claims: []OwnerClaim{OwnerClaim{Owner: "cs-api", Pattern: sysPat, Mode: ModeReplaceOwned, Predicates: []string{"test.value.b"}}}}); err != nil {
		t.Fatalf("re-registration of same owner should succeed: %v", err)
	}

	if _, ok, _ := r.OwnerOf(ctx, entity, "test.value.a"); ok {
		t.Error("test.value.a should no longer be owned after re-registration replaced it")
	}
	owner, ok, err := r.OwnerOf(ctx, entity, "test.value.b")
	if err != nil || !ok || owner != "cs-api" {
		t.Errorf("OwnerOf(test.value.b) = %q,%v,%v want cs-api,true,nil", owner, ok, err)
	}
}

// TestRegistry_RegisterStampsIncarnationOnStoredClaim drives the REAL
// RegisterOwner through live NATS KV and reads the stored epoch back, asserting
// every persisted OwnerClaim carries the registry's per-process incarnation
// nonce (the storage side of the ADR-056 PR-1 fence — a later PR's read path
// returns this for the write-time lease comparison). Unlike the unit test that
// replicates the copy-stamp in isolation, this would FAIL if RegisterOwner's
// stamp loop were deleted.
func TestRegistry_RegisterStampsIncarnationOnStoredClaim(t *testing.T) {
	r, claims, ctx := newTestRegistry(t)

	if err := r.RegisterOwner(ctx, Registration{Owner: "cs-api", Claims: []OwnerClaim{OwnerClaim{Owner: "cs-api", Pattern: sysPat, Mode: ModeReplaceOwned, Predicates: []string{"sensorml.process.label"}}}}); err != nil {
		t.Fatalf("register: %v", err)
	}

	ep := readEpoch(t, claims, ctx)
	entry, ok := ep.Owners["cs-api"]
	if !ok {
		t.Fatal("cs-api should be present in the stored epoch")
	}
	if len(entry.Claims) == 0 {
		t.Fatal("cs-api entry should carry its registered claim")
	}
	for _, c := range entry.Claims {
		if c.Incarnation == "" {
			t.Error("stored claim must carry a non-empty incarnation (the fence)")
		}
		if c.Incarnation != r.Incarnation() {
			t.Errorf("stored claim Incarnation = %q, want the registry's per-process nonce %q",
				c.Incarnation, r.Incarnation())
		}
	}
}

func TestRegistry_OwnerOf(t *testing.T) {
	r, _, ctx := newTestRegistry(t)
	entity := "c360.semconnect.systems.csapi.system.drone-001"

	// Empty registry: nothing owned.
	if _, ok, err := r.OwnerOf(ctx, entity, "test.value.p"); ok || err != nil {
		t.Errorf("empty registry OwnerOf should be false,nil; got ok=%v err=%v", ok, err)
	}

	if err := r.RegisterOwner(ctx, Registration{Owner: "cs-api", Claims: []OwnerClaim{OwnerClaim{Owner: "cs-api", Pattern: sysPat, Mode: ModeReplaceOwned, Predicates: []string{"sensorml.process.label"}}}}); err != nil {
		t.Fatal(err)
	}
	owner, ok, err := r.OwnerOf(ctx, entity, "sensorml.process.label")
	if err != nil || !ok {
		t.Fatalf("OwnerOf after registration: ok=%v err=%v", ok, err)
	}
	if owner != "cs-api" {
		t.Errorf("OwnerOf = %q want cs-api (the owner id is the lease handle)", owner)
	}
}

// TestRegistry_ForeignEdgeClaimFor proves the T2-seam reject lookup: a
// registered ForeignEdgeClaim is found by (producer message type, predicate),
// and an unclaimed foreign edge reports ok=false (the seam rejects it).
func TestRegistry_ForeignEdgeClaimFor(t *testing.T) {
	r, _, ctx := newTestRegistry(t)
	const isHostedBy = "sensorml.component.is-hosted-by"
	if err := r.RegisterOwner(ctx, Registration{
		Owner: "sensorml-producer",
		ForeignEdges: []ForeignEdgeClaim{{
			Owner: "sensorml-producer", Predicate: "sensorml.component.is-hosted-by", Mode: EdgeNoBirthStub,
			Producer: "sensorml.asset.v1", TargetPattern: sysPat,
		}},
	}); err != nil {
		t.Fatalf("register foreign-edge claim: %v", err)
	}

	c, ok, err := r.ForeignEdgeClaimFor(ctx, "sensorml.asset.v1", isHostedBy)
	if err != nil || !ok || c.Owner != "sensorml-producer" {
		t.Errorf("ForeignEdgeClaimFor(claimed) = %+v,%v,%v", c, ok, err)
	}
	if _, ok, _ := r.ForeignEdgeClaimFor(ctx, "sensorml.asset.v1", "test.unclaimed.edge"); ok {
		t.Error("an unclaimed foreign edge must report ok=false (the seam rejects it)")
	}
	if _, ok, _ := r.ForeignEdgeClaimFor(ctx, "other.type.v1", isHostedBy); ok {
		t.Error("a producer-specific claim must not cover a different producer")
	}
}

// TestRegistry_StaleCompaction proves a crashed owner's claim is compacted out
// by the next registrant — the availability-over-stale-claim call. We simulate
// the crash by deleting the dead owner's presence key (TTL expiry, sans wait).
func TestRegistry_StaleCompaction(t *testing.T) {
	r, claims, ctx := newTestRegistry(t)
	entity := "c360.semconnect.systems.csapi.system.drone-001"

	if err := r.RegisterOwner(ctx, Registration{Owner: "owner-a", Claims: []OwnerClaim{OwnerClaim{Owner: "owner-a", Pattern: sysPat, Mode: ModeReplaceOwned, Predicates: []string{"test.value.p"}}}}); err != nil {
		t.Fatal(err)
	}
	// owner-a "crashes": its heartbeat key disappears.
	if err := r.Resign(ctx, "owner-a"); err != nil {
		t.Fatalf("resign owner-a: %v", err)
	}

	// owner-b claims the SAME cell. Without compaction this overlaps owner-a;
	// with compaction owner-a is evicted first and owner-b succeeds.
	if err := r.RegisterOwner(ctx, Registration{Owner: "owner-b", Claims: []OwnerClaim{OwnerClaim{Owner: "owner-b", Pattern: sysPat, Mode: ModeReplaceOwned, Predicates: []string{"test.value.p"}}}}); err != nil {
		t.Fatalf("owner-b should claim the freed cell after owner-a compaction: %v", err)
	}

	ep := readEpoch(t, claims, ctx)
	if _, ok := ep.Owners["owner-a"]; ok {
		t.Error("owner-a should have been compacted out of the epoch")
	}
	owner, ok, _ := r.OwnerOf(ctx, entity, "test.value.p")
	if !ok || owner != "owner-b" {
		t.Errorf("cell should now be owned by owner-b, got %q,%v", owner, ok)
	}
}

// TestRegistry_HeartbeatedOwnerSurvivesCompaction is the inverse of
// TestRegistry_StaleCompaction and the durability property the static-projection
// heartbeat fix depends on (Codex review of #277): an owner whose presence key
// ages out but is RE-BUMPED (as a live owner's Heartbeater does each tick) is NOT
// compacted by the next registrant. owner-a holds a real OwnerClaim, so it is not
// covered by the FE-only compaction exemption — heartbeating is the only thing
// keeping its claim alive.
func TestRegistry_HeartbeatedOwnerSurvivesCompaction(t *testing.T) {
	r, claims, ctx := newTestRegistry(t)
	entity := "c360.semconnect.systems.csapi.system.drone-001"

	if err := r.RegisterOwner(ctx, Registration{Owner: "owner-a", Claims: []OwnerClaim{OwnerClaim{Owner: "owner-a", Pattern: sysPat, Mode: ModeReplaceOwned, Predicates: []string{"test.value.p"}}}}); err != nil {
		t.Fatal(err)
	}
	// owner-a's presence key ages out (TTL expiry, sans wait)...
	if err := r.Resign(ctx, "owner-a"); err != nil {
		t.Fatalf("resign owner-a: %v", err)
	}
	// ...but owner-a is still ALIVE: its Heartbeater re-bumps the presence key.
	if err := r.Heartbeat(ctx, "owner-a"); err != nil {
		t.Fatalf("heartbeat owner-a: %v", err)
	}

	// owner-b registers on a DISJOINT cell, triggering a compaction sweep.
	// owner-a's presence is live again → it must survive.
	if err := r.RegisterOwner(ctx, Registration{Owner: "owner-b", Claims: []OwnerClaim{OwnerClaim{Owner: "owner-b", Pattern: depPat, Mode: ModeReplaceOwned, Predicates: []string{"test.value.p"}}}}); err != nil {
		t.Fatalf("register owner-b: %v", err)
	}

	ep := readEpoch(t, claims, ctx)
	if _, ok := ep.Owners["owner-a"]; !ok {
		t.Error("heartbeated owner-a must NOT be compacted out of the epoch")
	}
	owner, ok, _ := r.OwnerOf(ctx, entity, "test.value.p")
	if !ok || owner != "owner-a" {
		t.Errorf("owner-a should still own its cell, got %q,%v", owner, ok)
	}
}

// TestRegistry_ConcurrentDisjoint proves the single-epoch CAS serializes
// concurrent registrants with no lost update: N disjoint owners registered in
// parallel all land in the epoch.
func TestRegistry_ConcurrentDisjoint(t *testing.T) {
	r, claims, ctx := newTestRegistry(t)
	const n = 8

	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			owner := fmt.Sprintf("owner-%d", i)
			pattern := fmt.Sprintf("c360.semconnect.systems.csapi.type%d.*", i)
			errs[i] = r.RegisterOwner(ctx, Registration{Owner: owner, Claims: []OwnerClaim{OwnerClaim{Owner: owner, Pattern: pattern, Mode: ModeReplaceOwned, Predicates: []string{"test.value.p"}}}})
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("concurrent registration %d failed: %v", i, err)
		}
	}
	ep := readEpoch(t, claims, ctx)
	if len(ep.Owners) != n {
		t.Errorf("epoch should hold all %d disjoint owners (no lost update), got %d", n, len(ep.Owners))
	}
}
