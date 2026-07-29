//go:build integration

package ownership

import (
	"context"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
)

// EnsureBuckets must create both framework buckets with the load-bearing
// config: OWNER_PRESENCE with the PresenceTTL backstop (an unset TTL means a
// crashed owner is NEVER reaped and blocks every future registrant forever),
// and OWNER_CLAIMS with NO TTL (a TTL there would age out the durable epoch).
// It must also return a Registry that actually works end-to-end.
func TestIntegration_EnsureBuckets_ConfigAndUsable(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	ctx := context.Background()

	r, err := EnsureBuckets(ctx, tc.Client, nil, nil)
	if err != nil {
		t.Fatalf("EnsureBuckets: %v", err)
	}

	presence, err := tc.Client.GetKeyValueBucket(ctx, BucketOwnerPresence)
	if err != nil {
		t.Fatalf("open %s: %v", BucketOwnerPresence, err)
	}
	pStatus, err := presence.Status(ctx)
	if err != nil {
		t.Fatalf("status %s: %v", BucketOwnerPresence, err)
	}
	if pStatus.TTL() != PresenceTTL {
		t.Errorf("%s TTL = %v, want %v (the staleness backstop)", BucketOwnerPresence, pStatus.TTL(), PresenceTTL)
	}

	claims, err := tc.Client.GetKeyValueBucket(ctx, BucketOwnerClaims)
	if err != nil {
		t.Fatalf("open %s: %v", BucketOwnerClaims, err)
	}
	cStatus, err := claims.Status(ctx)
	if err != nil {
		t.Fatalf("status %s: %v", BucketOwnerClaims, err)
	}
	if cStatus.TTL() != 0 {
		t.Errorf("%s TTL = %v, want 0 (a TTL would age out the durable epoch)", BucketOwnerClaims, cStatus.TTL())
	}
	claimsSpec, ok := graph.SpecFor(BucketOwnerClaims)
	if !ok {
		t.Fatalf("%s must be a catalog bucket", BucketOwnerClaims)
	}
	if cStatus.History() != int64(claimsSpec.History) {
		t.Errorf("%s History = %d, want %d (registration audit trail, catalog-declared)",
			BucketOwnerClaims, cStatus.History(), claimsSpec.History)
	}

	// The returned Registry is usable.
	registration := Registration{
		Owner: "cs-api",
		Claims: []OwnerClaim{{
			Owner: "cs-api", Pattern: sysPat, Mode: ModeReplaceOwned,
			Predicates: []string{"sensorml.process.label"},
		}},
	}
	if err := r.RegisterOwner(ctx, registration); err != nil {
		t.Fatalf("RegisterOwner through EnsureBuckets registry: %v", err)
	}
	// EnsureBuckets is idempotent — a second call (a second process) returns a
	// working Registry over the same buckets and sees the first's claim.
	r2, err := EnsureBuckets(ctx, tc.Client, nil, nil)
	if err != nil {
		t.Fatalf("EnsureBuckets (2nd): %v", err)
	}
	owner, ok, err := r2.OwnerOf(ctx, "c360.semconnect.systems.csapi.system.001", "sensorml.process.label")
	if err != nil || !ok || owner != "cs-api" {
		t.Fatalf("second registry must see first's claim: owner=%q ok=%v err=%v", owner, ok, err)
	}
}

// The Heartbeater ticks Registry.Heartbeat for every enrolled owner until its
// context is cancelled, at which point Run returns promptly (the lifecycle the
// Manager relies on instead of a Close method).
func TestIntegration_Heartbeater_BumpsThenStopsOnCancel(t *testing.T) {
	r, _, _ := newTestRegistry(t)
	ctx, cancel := context.WithCancel(context.Background())

	const owner = "hb-owner"
	hb := r.NewHeartbeater(40 * time.Millisecond)
	hb.Add(owner)

	done := make(chan struct{})
	go func() { hb.Run(ctx); close(done) }()

	// The presence key appears after the first tick (condition-based wait, not a
	// duration assertion — robust to host-load variation).
	first := waitForPresence(t, r, owner, 3*time.Second)
	// And gets re-bumped on a later tick (the value is a fresh unix-nanos stamp).
	waitForPresenceChange(t, r, owner, first, 3*time.Second)

	// Cancellation stops the loop: Run returns promptly. This is the
	// deterministic substitute for a Close() — the app root ctx drives the
	// heartbeat lifetime.
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Heartbeater.Run did not return within 2s of ctx cancel")
	}
}

// waitForPresence polls until the owner's presence key exists, returning its
// value. Fails the test if it never appears within timeout.
func waitForPresence(t *testing.T, r *Registry, owner string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		entry, err := r.presence.Get(context.Background(), presenceKeyPrefix+owner)
		if err == nil {
			return string(entry.Value)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("presence key for %q never appeared within %v", owner, timeout)
	return ""
}

// waitForPresenceChange polls until the owner's presence value differs from
// prev, proving a re-bump tick fired.
func waitForPresenceChange(t *testing.T, r *Registry, owner, prev string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		entry, err := r.presence.Get(context.Background(), presenceKeyPrefix+owner)
		if err == nil && string(entry.Value) != prev {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("presence key for %q never re-bumped within %v", owner, timeout)
}
