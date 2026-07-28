package graph

import "testing"

func contains(set []string, want string) bool {
	for _, s := range set {
		if s == want {
			return true
		}
	}
	return false
}

// TestFrameworkOwnedBuckets_IncludesEntitySuffixIndex pins the write-ownership
// fix: ENTITY_SUFFIX_INDEX is a member of the owned set, so a generic rule
// update_kv into it is rejected at both load and runtime
// (framework-owned-bucket-guards; #622).
func TestFrameworkOwnedBuckets_IncludesEntitySuffixIndex(t *testing.T) {
	t.Parallel()
	if !IsFrameworkOwnedBucket(BucketEntitySuffixIndex) {
		t.Fatalf("%s must be reported as framework-owned", BucketEntitySuffixIndex)
	}
	if !contains(FrameworkOwnedBuckets(), BucketEntitySuffixIndex) {
		t.Fatalf("FrameworkOwnedBuckets() must include %s", BucketEntitySuffixIndex)
	}
}

// TestFrameworkOwnedBuckets_IncludesOperationalBuckets pins the F2/F3
// write-ownership fixes: GRAPH_INGEST_APPLIED_SEQ (redelivery-guard stamps, #715)
// and GRAPH_STATUS (readiness envelopes) are members of the owned set, so a
// generic rule update_kv cannot forge a sequence stamp or readiness at either
// guard site. Both are correctness-critical no-eviction state, so the retention
// sweep covers them (they are NOT the excluded rebuildable cache).
func TestFrameworkOwnedBuckets_IncludesOperationalBuckets(t *testing.T) {
	t.Parallel()
	for _, bucket := range []string{BucketGraphIngestAppliedSeq, BucketGraphStatus} {
		if !IsFrameworkOwnedBucket(bucket) {
			t.Errorf("%s must be reported as framework-owned", bucket)
		}
		if !contains(FrameworkOwnedBuckets(), bucket) {
			t.Errorf("FrameworkOwnedBuckets() must include %s", bucket)
		}
		if !contains(retentionGuardedBuckets(), bucket) {
			t.Errorf("retentionGuardedBuckets() must include %s (correctness-critical no-eviction state)", bucket)
		}
	}
}

// TestRetentionGuardedBuckets_ExcludesEmbeddingsCacheOnly proves the retention
// sweep set is FrameworkOwnedBuckets() minus exactly EMBEDDINGS_CACHE: the cache
// is excluded from the retention guard (its capacity policy is owned elsewhere)
// but stays write-ownership-protected in the owned set.
func TestRetentionGuardedBuckets_ExcludesEmbeddingsCacheOnly(t *testing.T) {
	t.Parallel()
	guarded := retentionGuardedBuckets()

	if contains(guarded, BucketEmbeddingsCache) {
		t.Errorf("retentionGuardedBuckets() must EXCLUDE %s (rebuildable cache)", BucketEmbeddingsCache)
	}
	// EMBEDDINGS_CACHE remains write-ownership-protected even though excluded
	// from the retention sweep.
	if !IsFrameworkOwnedBucket(BucketEmbeddingsCache) {
		t.Errorf("%s must remain framework-owned (write-protected)", BucketEmbeddingsCache)
	}

	// The guarded set is exactly the owned set minus the one cache.
	owned := FrameworkOwnedBuckets()
	if len(guarded) != len(owned)-1 {
		t.Fatalf("guarded set size = %d, want %d (owned minus EMBEDDINGS_CACHE)", len(guarded), len(owned)-1)
	}
	for _, b := range owned {
		if b == BucketEmbeddingsCache {
			continue
		}
		if !contains(guarded, b) {
			t.Errorf("guarded set is missing owned bucket %s", b)
		}
	}

	// A representative derived index and ENTITY_STATES are guarded.
	for _, b := range []string{BucketEntityStates, BucketEmbeddingIndex, BucketCommunityIndex, BucketEntitySuffixIndex} {
		if !contains(guarded, b) {
			t.Errorf("retentionGuardedBuckets() must include %s", b)
		}
	}
}
