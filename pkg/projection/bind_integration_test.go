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
