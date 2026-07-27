//go:build integration

package ownership

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// newTestRegistry spins up a real NATS KV (testcontainer) with the two
// ownership buckets and returns a Registry plus the claims KVStore (so tests
// can read the epoch back through the in-package helpers).
func newTestRegistry(t *testing.T) (*Registry, *natsclient.KVStore, context.Context) {
	t.Helper()
	registry, claims, _, ctx := newTestRegistryWithPresence(t)
	return registry, claims, ctx
}

func newTestRegistryWithPresence(
	t *testing.T,
) (*Registry, *natsclient.KVStore, jetstream.KeyValue, context.Context) {
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
	return NewRegistry(claims, presence, nil), claims, presenceBucket, ctx
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

func TestRegistry_SameInstanceOwnerCanBindOnlyOnceWithoutSideEffects(t *testing.T) {
	r, claims, ctx := newTestRegistry(t)
	entity := "c360.semconnect.systems.csapi.system.drone-001"

	if err := r.RegisterOwner(ctx, Registration{Owner: "cs-api", Claims: []OwnerClaim{OwnerClaim{Owner: "cs-api", Pattern: sysPat, Mode: ModeReplaceOwned, Predicates: []string{"test.value.a"}}}}); err != nil {
		t.Fatal(err)
	}
	beforeEpoch := readEpoch(t, claims, ctx)
	beforePresence, err := r.presence.Get(ctx, presenceKeyPrefix+"cs-api")
	if err != nil {
		t.Fatalf("read initial presence: %v", err)
	}

	err = r.RegisterOwner(ctx, Registration{Owner: "cs-api", Claims: []OwnerClaim{OwnerClaim{Owner: "cs-api", Pattern: sysPat, Mode: ModeReplaceOwned, Predicates: []string{"test.value.b"}}}})
	if !errors.Is(err, ErrOwnerAlreadyBound) {
		t.Fatalf("second registration error = %v, want ErrOwnerAlreadyBound", err)
	}

	owner, ok, err := r.OwnerOf(ctx, entity, "test.value.a")
	if err != nil || !ok || owner != "cs-api" {
		t.Errorf("first claim changed after rejected bind: %q,%v,%v", owner, ok, err)
	}
	if _, ok, _ := r.OwnerOf(ctx, entity, "test.value.b"); ok {
		t.Error("rejected second claim mutated the epoch")
	}
	afterEpoch := readEpoch(t, claims, ctx)
	afterPresence, err := r.presence.Get(ctx, presenceKeyPrefix+"cs-api")
	if err != nil {
		t.Fatalf("read final presence: %v", err)
	}
	if afterEpoch.Version != beforeEpoch.Version ||
		afterPresence.Revision != beforePresence.Revision {
		t.Fatalf(
			"rejected bind changed epoch/presence revisions: epoch %d→%d presence %d→%d",
			beforeEpoch.Version,
			afterEpoch.Version,
			beforePresence.Revision,
			afterPresence.Revision,
		)
	}
}

func TestRegistry_ConcurrentSameOwnerRegistrationHasOneSideEffectingWinner(t *testing.T) {
	r, claims, ctx := newTestRegistry(t)
	registrations := []Registration{
		{Owner: "race-owner", Claims: []OwnerClaim{{
			Owner: "race-owner", Pattern: sysPat, Mode: ModeReplaceOwned,
			Predicates: []string{"test.value.a"},
		}}},
		{Owner: "race-owner", Claims: []OwnerClaim{{
			Owner: "race-owner", Pattern: sysPat, Mode: ModeReplaceOwned,
			Predicates: []string{"test.value.b"},
		}}},
	}
	start := make(chan struct{})
	results := make(chan error, len(registrations))
	for _, registration := range registrations {
		registration := registration
		go func() {
			<-start
			results <- r.RegisterOwner(ctx, registration)
		}()
	}
	close(start)
	var success, rejected int
	for range registrations {
		switch err := <-results; {
		case err == nil:
			success++
		case errors.Is(err, ErrOwnerAlreadyBound):
			rejected++
		default:
			t.Fatalf("registration error = %v", err)
		}
	}
	if success != 1 || rejected != 1 {
		t.Fatalf("success/rejected = %d/%d, want 1/1", success, rejected)
	}
	ep := readEpoch(t, claims, ctx)
	if ep.Version != 1 || len(ep.Owners) != 1 {
		t.Fatalf("epoch = %#v, want one committed registration", ep)
	}
	presence, err := r.presence.Get(ctx, presenceKeyPrefix+"race-owner")
	if err != nil {
		t.Fatalf("presence: %v", err)
	}
	if presence.Revision != 1 {
		t.Fatalf("presence revision = %d, want one heartbeat write", presence.Revision)
	}
}

func TestRegistry_PresenceAndRevivalMonitoringFollowOwningSemantics(t *testing.T) {
	r, claims, _, ctx := newTestRegistryWithPresence(t)
	registrations := []struct {
		name   string
		owner  string
		claims []OwnerClaim
		edges  []ForeignEdgeClaim
		owning bool
	}{
		{
			name:  "foreign edge only",
			owner: "fe-only",
			edges: []ForeignEdgeClaim{{
				Owner: "fe-only", Predicate: "test.edge.claimed", Mode: EdgeStrict,
				Producer: "test.fe-only.v1", TargetPattern: sysPat,
			}},
		},
		{
			name:  "append only",
			owner: "append-only",
			claims: []OwnerClaim{{
				Owner: "append-only", Pattern: sysPat, Mode: ModeAppendEvidence,
				Predicates: []string{"test.value.a"},
			}},
		},
		{
			name:  "foreign edge plus append",
			owner: "fe-append",
			claims: []OwnerClaim{{
				Owner: "fe-append", Pattern: sysPat, Mode: ModeAppendEvidence,
				Predicates: []string{"test.value.b"},
			}},
			edges: []ForeignEdgeClaim{{
				Owner: "fe-append", Predicate: "test.edge.shared", Mode: EdgeStrict,
				Producer: "test.fe-append.v1", TargetPattern: depPat,
			}},
		},
		{
			name:  "replace owned",
			owner: "replace-owner",
			claims: []OwnerClaim{{
				Owner: "replace-owner", Pattern: sysPat, Mode: ModeReplaceOwned,
				Predicates: []string{"sensorml.process.label"},
			}},
			owning: true,
		},
		{
			name:  "cas transition",
			owner: "cas-owner",
			claims: []OwnerClaim{{
				Owner: "cas-owner", Pattern: depPat, Mode: ModeCASTransition,
				Predicates: []string{"sensorml.process.label"},
			}},
			owning: true,
		},
		{
			name:  "mixed",
			owner: "mixed-owner",
			claims: []OwnerClaim{
				{
					Owner: "mixed-owner", Pattern: "c360.semconnect.systems.csapi.mixed.*",
					Mode: ModeAppendEvidence, Predicates: []string{"test.value.a"},
				},
				{
					Owner: "mixed-owner", Pattern: "c360.semconnect.systems.csapi.mixed.*",
					Mode: ModeReplaceOwned, Predicates: []string{"test.value.b"},
				},
			},
			edges: []ForeignEdgeClaim{{
				Owner: "mixed-owner", Predicate: "test.edge.p", Mode: EdgeStrict,
				Producer: "test.mixed.v1", TargetPattern: "c360.semconnect.systems.csapi.mixed.*",
			}},
			owning: true,
		},
	}

	for _, test := range registrations {
		err := r.RegisterOwner(ctx, Registration{
			Owner: test.owner, Claims: test.claims, ForeignEdges: test.edges,
		})
		if err != nil {
			t.Fatalf("%s registration: %v", test.name, err)
		}
		ep := readEpoch(t, claims, ctx)
		if _, ok := ep.Owners[test.owner]; !ok {
			t.Fatalf("%s registration did not persist", test.name)
		}
		_, presenceErr := r.presence.Get(ctx, presenceKeyPrefix+test.owner)
		if test.owning && presenceErr != nil {
			t.Fatalf("%s owning registration presence: %v", test.name, presenceErr)
		}
		if !test.owning && !errors.Is(presenceErr, natsclient.ErrKVKeyNotFound) {
			t.Fatalf("%s non-owning presence error = %v, want key not found", test.name, presenceErr)
		}
		_, monitored := r.registeredOwners()[test.owner]
		if monitored != test.owning {
			t.Fatalf("%s revival monitoring = %v, want %v", test.name, monitored, test.owning)
		}
	}

	// A later owning registration triggers compaction. Every non-owning entry
	// must persist without presence; liveness applies only to the owning entries.
	ep := readEpoch(t, claims, ctx)
	for _, owner := range []string{"fe-only", "append-only", "fe-append"} {
		if _, ok := ep.Owners[owner]; !ok {
			t.Errorf("non-owning registration %q was compacted without presence", owner)
		}
	}
}

func TestRegistry_ValidNonOwningRegistrationsDoNotWarn(t *testing.T) {
	r, _, ctx := newTestRegistry(t)
	var logs bytes.Buffer
	r.logger = slog.New(slog.NewTextHandler(&logs, nil))

	registrations := []Registration{
		{
			Owner: "append-valid",
			Claims: []OwnerClaim{{
				Owner: "append-valid", Pattern: sysPat, Mode: ModeAppendEvidence,
				Predicates: []string{"test.value.a"},
			}},
		},
		{
			Owner: "fe-append-valid",
			Claims: []OwnerClaim{{
				Owner: "fe-append-valid", Pattern: sysPat, Mode: ModeAppendEvidence,
				Predicates: []string{"test.value.b"},
			}},
			ForeignEdges: []ForeignEdgeClaim{{
				Owner: "fe-append-valid", Predicate: "test.edge.claimed", Mode: EdgeStrict,
				Producer: "test.fe-append-valid.v1", TargetPattern: sysPat,
			}},
		},
	}
	for _, registration := range registrations {
		if err := r.RegisterOwner(ctx, registration); err != nil {
			t.Fatalf("register %q: %v", registration.Owner, err)
		}
	}
	if output := logs.String(); strings.Contains(output, "level=WARN") {
		t.Fatalf("valid non-owning registrations emitted warning:\n%s", output)
	}
}

func TestRegistry_NonOwningRepeatAndConcurrentHaveOneEpochWriteNoPresence(t *testing.T) {
	t.Run("repeat", func(t *testing.T) {
		r, claims, presence, ctx := newTestRegistryWithPresence(t)
		registration := Registration{
			Owner: "append-repeat",
			Claims: []OwnerClaim{{
				Owner: "append-repeat", Pattern: sysPat, Mode: ModeAppendEvidence,
				Predicates: []string{"test.value.a"},
			}},
		}
		if err := r.RegisterOwner(ctx, registration); err != nil {
			t.Fatal(err)
		}
		before := readEpoch(t, claims, ctx)
		if err := r.RegisterOwner(ctx, registration); !errors.Is(err, ErrOwnerAlreadyBound) {
			t.Fatalf("repeat registration error = %v, want ErrOwnerAlreadyBound", err)
		}
		after := readEpoch(t, claims, ctx)
		if after.Version != before.Version {
			t.Fatalf("repeat changed epoch version %d -> %d", before.Version, after.Version)
		}
		lastSeq, err := natsclient.BucketLastSeq(ctx, presence)
		if err != nil {
			t.Fatal(err)
		}
		if lastSeq != 0 {
			t.Fatalf("non-owning repeat presence last sequence = %d, want 0", lastSeq)
		}
	})

	t.Run("concurrent", func(t *testing.T) {
		r, claims, presence, ctx := newTestRegistryWithPresence(t)
		registration := Registration{
			Owner: "fe-race",
			ForeignEdges: []ForeignEdgeClaim{{
				Owner: "fe-race", Predicate: "test.edge.claimed", Mode: EdgeStrict,
				Producer: "test.fe-race.v1", TargetPattern: sysPat,
			}},
		}
		start := make(chan struct{})
		results := make(chan error, 2)
		for range 2 {
			go func() {
				<-start
				results <- r.RegisterOwner(ctx, registration)
			}()
		}
		close(start)
		var success, rejected int
		for range 2 {
			switch err := <-results; {
			case err == nil:
				success++
			case errors.Is(err, ErrOwnerAlreadyBound):
				rejected++
			default:
				t.Fatalf("concurrent registration: %v", err)
			}
		}
		if success != 1 || rejected != 1 {
			t.Fatalf("success/rejected = %d/%d, want 1/1", success, rejected)
		}
		ep := readEpoch(t, claims, ctx)
		if ep.Version != 1 || len(ep.Owners) != 1 {
			t.Fatalf("epoch = %#v, want one non-owning registration", ep)
		}
		lastSeq, err := natsclient.BucketLastSeq(ctx, presence)
		if err != nil {
			t.Fatal(err)
		}
		if lastSeq != 0 {
			t.Fatalf("non-owning concurrent presence last sequence = %d, want 0", lastSeq)
		}
	})
}

func TestRegistry_PresenceRollbackOnlyForOwningRegistration(t *testing.T) {
	t.Run("rejected non owning never writes presence", func(t *testing.T) {
		r, _, presence, ctx := newTestRegistryWithPresence(t)
		if err := r.RegisterOwner(ctx, Registration{
			Owner: "incumbent",
			Claims: []OwnerClaim{{
				Owner: "incumbent", Pattern: sysPat, Mode: ModeReplaceOwned,
				Predicates: []string{"test.value.p"},
			}},
		}); err != nil {
			t.Fatal(err)
		}
		before, err := natsclient.BucketLastSeq(ctx, presence)
		if err != nil {
			t.Fatal(err)
		}
		err = r.RegisterOwner(ctx, Registration{
			Owner: "foreign-rejected",
			ForeignEdges: []ForeignEdgeClaim{{
				Owner: "foreign-rejected", Predicate: "test.value.p", Mode: EdgeStrict,
				Producer: "test.foreign-rejected.v1", TargetPattern: sysPat,
			}},
		})
		if !errors.Is(err, ErrOwnershipOverlap) {
			t.Fatalf("foreign-edge overlap error = %v", err)
		}
		after, err := natsclient.BucketLastSeq(ctx, presence)
		if err != nil {
			t.Fatal(err)
		}
		if after != before {
			t.Fatalf("rejected non-owning registration wrote/rolled back presence: %d -> %d", before, after)
		}
	})

	t.Run("cancelled non owning never writes presence", func(t *testing.T) {
		r, _, presence, ctx := newTestRegistryWithPresence(t)
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		err := r.RegisterOwner(cancelled, Registration{
			Owner: "append-cancelled",
			Claims: []OwnerClaim{{
				Owner: "append-cancelled", Pattern: sysPat, Mode: ModeAppendEvidence,
				Predicates: []string{"test.value.a"},
			}},
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled registration error = %v, want context.Canceled", err)
		}
		lastSeq, seqErr := natsclient.BucketLastSeq(ctx, presence)
		if seqErr != nil {
			t.Fatal(seqErr)
		}
		if lastSeq != 0 {
			t.Fatalf("cancelled non-owning registration presence last sequence = %d, want 0", lastSeq)
		}
	})

	t.Run("rejected owning still rolls back initial presence", func(t *testing.T) {
		r, _, presence, ctx := newTestRegistryWithPresence(t)
		if err := r.RegisterOwner(ctx, Registration{
			Owner: "owning-incumbent",
			Claims: []OwnerClaim{{
				Owner: "owning-incumbent", Pattern: sysPat, Mode: ModeReplaceOwned,
				Predicates: []string{"test.value.p"},
			}},
		}); err != nil {
			t.Fatal(err)
		}
		before, err := natsclient.BucketLastSeq(ctx, presence)
		if err != nil {
			t.Fatal(err)
		}
		err = r.RegisterOwner(ctx, Registration{
			Owner: "owning-rejected",
			Claims: []OwnerClaim{{
				Owner: "owning-rejected", Pattern: sysPat, Mode: ModeCASTransition,
				Predicates: []string{"test.value.p"},
			}},
		})
		if !errors.Is(err, ErrOwnershipOverlap) {
			t.Fatalf("owning overlap error = %v", err)
		}
		if _, presenceErr := r.presence.Get(ctx, presenceKeyPrefix+"owning-rejected"); !errors.Is(
			presenceErr,
			natsclient.ErrKVKeyNotFound,
		) {
			t.Fatalf("rejected owning presence error = %v, want key not found", presenceErr)
		}
		after, err := natsclient.BucketLastSeq(ctx, presence)
		if err != nil {
			t.Fatal(err)
		}
		if after != before+2 {
			t.Fatalf("owning rejection must put then roll back presence: %d -> %d", before, after)
		}
	})
}

func TestRegistry_StaleMixedOwningEntryIsRemovedAtomically(t *testing.T) {
	r, claims, _, ctx := newTestRegistryWithPresence(t)
	if err := r.RegisterOwner(ctx, Registration{
		Owner: "mixed-stale",
		Claims: []OwnerClaim{
			{
				Owner: "mixed-stale", Pattern: sysPat, Mode: ModeReplaceOwned,
				Predicates: []string{"test.value.a"},
			},
			{
				Owner: "mixed-stale", Pattern: sysPat, Mode: ModeAppendEvidence,
				Predicates: []string{"test.value.b"},
			},
		},
		ForeignEdges: []ForeignEdgeClaim{{
			Owner: "mixed-stale", Predicate: "test.edge.claimed", Mode: EdgeStrict,
			Producer: "test.mixed-stale.v1", TargetPattern: sysPat,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Resign(ctx, "mixed-stale"); err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterOwner(ctx, Registration{
		Owner: "compaction-trigger",
		Claims: []OwnerClaim{{
			Owner: "compaction-trigger", Pattern: depPat, Mode: ModeReplaceOwned,
			Predicates: []string{"test.value.a"},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := readEpoch(t, claims, ctx).Owners["mixed-stale"]; ok {
		t.Fatal("stale mixed owning entry must be removed atomically, including append and foreign-edge claims")
	}
}

func TestRegistry_FailedFirstRegistrationReleasesOwnerBinding(t *testing.T) {
	r, _, ctx := newTestRegistry(t)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	registration := Registration{Owner: "retry-owner", Claims: []OwnerClaim{{
		Owner: "retry-owner", Pattern: sysPat, Mode: ModeReplaceOwned,
		Predicates: []string{"test.value.a"},
	}}}
	if err := r.RegisterOwner(canceled, registration); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled registration error = %v, want context.Canceled", err)
	}
	if err := r.RegisterOwner(ctx, registration); err != nil {
		t.Fatalf("retry after failed first registration: %v", err)
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
