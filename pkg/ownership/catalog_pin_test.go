package ownership

import (
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
)

// TestCatalogPins_OwnershipBuckets cross-pins the ownership substrate's
// constants against the framework KV catalog rows its buckets are acquired
// under. PresenceTTL lives HERE (next to HeartbeatInterval, which is derived
// from it — 4 beats per window) while the catalog's OWNER_PRESENCE row carries
// the same value as its declared bounded TTL; graph cannot import this package
// (ownership acquires through the catalog, so the dependency points the other
// way), so equality is pinned by test instead of by reference.
func TestCatalogPins_OwnershipBuckets(t *testing.T) {
	presence, ok := graph.SpecFor(BucketOwnerPresence)
	if !ok {
		t.Fatalf("%s must be a catalog bucket", BucketOwnerPresence)
	}
	if presence.Retention.Kind != natsclient.RetentionBoundedTTL {
		t.Fatalf("%s retention kind = %q, want bounded-ttl (the TTL IS the liveness contract)",
			BucketOwnerPresence, presence.Retention.Kind)
	}
	if presence.Retention.TTL != PresenceTTL {
		t.Errorf("catalog OWNER_PRESENCE TTL = %v, ownership.PresenceTTL = %v — these MUST be equal "+
			"(the seam converges the bucket to the catalog value; the heartbeater beats against PresenceTTL)",
			presence.Retention.TTL, PresenceTTL)
	}

	claims, ok := graph.SpecFor(BucketOwnerClaims)
	if !ok {
		t.Fatalf("%s must be a catalog bucket", BucketOwnerClaims)
	}
	if claims.Retention.Kind != natsclient.RetentionNoLifecycle {
		t.Errorf("%s retention kind = %q, want no-lifecycle (a TTL would age out the durable epoch)",
			BucketOwnerClaims, claims.Retention.Kind)
	}
}
