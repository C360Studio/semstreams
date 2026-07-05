package executors

import "testing"

// TestEntityStatesBucketConfig_NoTTL locks in the gh#484 fix: the agentic
// graph-query tool creates ENTITY_STATES via get-or-create, so if it wins the
// race with graph-ingest it stamps this config. ENTITY_STATES MUST have no
// lifecycle retention (ADR-068 D1) — graph-ingest fail-closes at boot if it
// finds a TTL, so a non-zero value here silently dead-locks the whole ingest
// path. A stale 24h mirror of the OLD graph/query default caused exactly that.
func TestEntityStatesBucketConfig_NoTTL(t *testing.T) {
	if entityStatesBucketConfig.Bucket != entityStatesBucket {
		t.Fatalf("bucket = %q, want %q", entityStatesBucketConfig.Bucket, entityStatesBucket)
	}
	if entityStatesBucketConfig.TTL != 0 {
		t.Errorf("ENTITY_STATES TTL = %v, want 0 (ADR-068 D1: no lifecycle retention on the live graph; a non-zero TTL trips graph-ingest's boot guardrail)", entityStatesBucketConfig.TTL)
	}
}
