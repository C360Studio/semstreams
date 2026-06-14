//go:build integration

package projection

import (
	"context"
	"errors"
	"testing"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/ownership"
)

func newOwnershipRegistry(t *testing.T) (*ownership.Registry, context.Context) {
	t.Helper()
	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	ctx := context.Background()
	cb, err := tc.CreateKVBucket(ctx, ownership.BucketOwnerClaims)
	if err != nil {
		t.Fatalf("create OWNER_CLAIMS: %v", err)
	}
	pb, err := tc.CreateKVBucket(ctx, ownership.BucketOwnerPresence)
	if err != nil {
		t.Fatalf("create OWNER_PRESENCE: %v", err)
	}
	return ownership.NewRegistry(tc.Client.NewKVStore(cb), tc.Client.NewKVStore(pb), nil), ctx
}

// TestBind_DerivesAndRegisters exercises the full Decision-6 chain: a projection
// contract → derived claims → registered with the ownership substrate → visible
// to the write-time lease lookup.
func TestBind_DerivesAndRegisters(t *testing.T) {
	reg, ctx := newOwnershipRegistry(t)
	if err := Bind(ctx, reg, "cs-api", csapiSystem()); err != nil {
		t.Fatalf("bind cs-api System projection: %v", err)
	}
	owner, ok, err := reg.OwnerOf(ctx, "c360.semconnect.systems.csapi.system.drone-001", "sensorml.process.label")
	if err != nil || !ok || owner != "cs-api" {
		t.Errorf("OwnerOf after Bind = %q,%v,%v want cs-api,true,nil", owner, ok, err)
	}
}

// TestBind_CrossOwnerOverlapRejected proves a second owner cannot bind a
// projection that claims a cell cs-api already owns — the derivation feeds the
// substrate's epoch overlap check. (Cross-PROCESS overlap is the substrate's own
// concern, covered in pkg/ownership's integration tests; here both owners share
// one Registry, so this proves cross-OWNER rejection.)
func TestBind_CrossOwnerOverlapRejected(t *testing.T) {
	reg, ctx := newOwnershipRegistry(t)
	if err := Bind(ctx, reg, "cs-api", csapiSystem()); err != nil {
		t.Fatal(err)
	}
	poacher := Contract{
		Name:          "poacher.system",
		EntityPattern: sysPat,
		Groups:        []PredicateGroup{{Mode: ownership.ModeReplaceOwned, Predicates: []string{"sensorml.process.label"}}},
	}
	if err := Bind(ctx, reg, "other", poacher); !errors.Is(err, ownership.ErrOwnershipOverlap) {
		t.Errorf("cross-owner overlap via Bind should reject with ErrOwnershipOverlap, got %v", err)
	}
}

// TestBindAndHeartbeat_EnrollsOnSuccess proves a static projection owner is
// enrolled for heartbeating on a successful Bind — the durability fix for a
// process-lifetime OwnerClaim (Codex review of #277). Without enrollment the
// owner's presence key ages out after PresenceTTL and the next registrant
// compacts its claim (see ownership.TestRegistry_HeartbeatedOwnerSurvivesCompaction).
func TestBindAndHeartbeat_EnrollsOnSuccess(t *testing.T) {
	reg, ctx := newOwnershipRegistry(t)
	hb := reg.NewHeartbeater(ownership.HeartbeatInterval)

	if err := BindAndHeartbeat(ctx, reg, hb, "cs-api", csapiSystem()); err != nil {
		t.Fatalf("BindAndHeartbeat: %v", err)
	}
	if !hb.IsEnrolled("cs-api") {
		t.Error("BindAndHeartbeat must enroll the owner in the heartbeater on a successful Bind")
	}
	owner, ok, err := reg.OwnerOf(ctx, "c360.semconnect.systems.csapi.system.drone-001", "sensorml.process.label")
	if err != nil || !ok || owner != "cs-api" {
		t.Errorf("OwnerOf after BindAndHeartbeat = %q,%v,%v want cs-api,true,nil", owner, ok, err)
	}
}

// TestBindAndHeartbeat_SkipsEnrollOnBindFailure proves a rejected/overlapping
// owner is NOT enrolled — there is no recorded claim to keep alive, and enrolling
// it would heartbeat a presence key for a non-owner.
func TestBindAndHeartbeat_SkipsEnrollOnBindFailure(t *testing.T) {
	reg, ctx := newOwnershipRegistry(t)
	if err := Bind(ctx, reg, "cs-api", csapiSystem()); err != nil {
		t.Fatal(err)
	}
	hb := reg.NewHeartbeater(ownership.HeartbeatInterval)
	poacher := Contract{
		Name:          "poacher.system",
		EntityPattern: sysPat,
		Groups:        []PredicateGroup{{Mode: ownership.ModeReplaceOwned, Predicates: []string{"sensorml.process.label"}}},
	}
	if err := BindAndHeartbeat(ctx, reg, hb, "other", poacher); !errors.Is(err, ownership.ErrOwnershipOverlap) {
		t.Fatalf("overlap should reject with ErrOwnershipOverlap, got %v", err)
	}
	if hb.IsEnrolled("other") {
		t.Error("a rejected owner must NOT be enrolled in the heartbeater")
	}
}
