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
// guard site. Both are correctness-critical no-eviction state; the retention
// sweep ranges the owned set directly, so membership alone puts them under it.
func TestFrameworkOwnedBuckets_IncludesOperationalBuckets(t *testing.T) {
	t.Parallel()
	for _, bucket := range []string{BucketGraphIngestAppliedSeq, BucketGraphStatus} {
		if !IsFrameworkOwnedBucket(bucket) {
			t.Errorf("%s must be reported as framework-owned", bucket)
		}
		if !contains(FrameworkOwnedBuckets(), bucket) {
			t.Errorf("FrameworkOwnedBuckets() must include %s", bucket)
		}
	}
}

// TestFrameworkOwnedBuckets_NoEmbeddingsCache pins the EMBEDDINGS_CACHE
// deletion (reopen-framework-owned-bucket-guards): the dead
// created-but-never-read-or-written surface is gone from the owned set, so the
// retention sweep guards FrameworkOwnedBuckets() with NO exceptions — no
// framework bucket may exist solely to carry a guard exemption.
func TestFrameworkOwnedBuckets_NoEmbeddingsCache(t *testing.T) {
	t.Parallel()
	if contains(FrameworkOwnedBuckets(), "EMBEDDINGS_CACHE") {
		t.Error("FrameworkOwnedBuckets() must not include the deleted EMBEDDINGS_CACHE bucket")
	}
	if IsFrameworkOwnedBucket("EMBEDDINGS_CACHE") {
		t.Error("EMBEDDINGS_CACHE must not be reported as framework-owned (surface deleted)")
	}
}
