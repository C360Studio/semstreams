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
	token, err := Bind(ctx, reg, "cs-api", csapiSystem(t))
	if err != nil {
		t.Fatalf("bind cs-api System projection: %v", err)
	}
	// ADR-056 PR-3.5: Bind surfaces the bound owner's typed OwnerToken so the
	// producer can stamp it without hand-composing the credential. It must equal
	// the registry's own mint for the same owner.
	if got, want := token.Wire(), reg.OwnerToken("cs-api").Wire(); got != want || got == "" {
		t.Errorf("Bind returned token.Wire() = %q, want %q (non-empty)", got, want)
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
	if _, err := Bind(ctx, reg, "cs-api", csapiSystem(t)); err != nil {
		t.Fatal(err)
	}
	poacher := Contract{
		Name:          "poacher.system",
		EntityPattern: sysPat,
		Groups:        []PredicateGroup{{Mode: ownership.ModeReplaceOwned, Predicates: []string{"sensorml.process.label"}}},
	}
	token, err := Bind(ctx, reg, "other", poacher)
	if !errors.Is(err, ownership.ErrOwnershipOverlap) {
		t.Errorf("cross-owner overlap via Bind should reject with ErrOwnershipOverlap, got %v", err)
	}
	// ADR-056 PR-3.5: a rejected bind surfaces the zero token, never a usable
	// credential for an owner that holds no recorded claim.
	if !token.IsZero() {
		t.Errorf("Bind on overlap must return the zero token, got Wire()=%q", token.Wire())
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

	if _, err := BindAndHeartbeat(ctx, reg, hb, "cs-api", csapiSystem(t)); err != nil {
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
	if _, err := Bind(ctx, reg, "cs-api", csapiSystem(t)); err != nil {
		t.Fatal(err)
	}
	hb := reg.NewHeartbeater(ownership.HeartbeatInterval)
	poacher := Contract{
		Name:          "poacher.system",
		EntityPattern: sysPat,
		Groups:        []PredicateGroup{{Mode: ownership.ModeReplaceOwned, Predicates: []string{"sensorml.process.label"}}},
	}
	if _, err := BindAndHeartbeat(ctx, reg, hb, "other", poacher); !errors.Is(err, ownership.ErrOwnershipOverlap) {
		t.Fatalf("overlap should reject with ErrOwnershipOverlap, got %v", err)
	}
	if hb.IsEnrolled("other") {
		t.Error("a rejected owner must NOT be enrolled in the heartbeater")
	}
}
