package graph

import (
	"testing"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestKVCatalog_EveryRowValidates: every shipped descriptor must pass the
// seam's fail-closed validation — a catalog row the seam would refuse is a
// boot-breaking typo caught here instead.
func TestKVCatalog_EveryRowValidates(t *testing.T) {
	seen := make(map[string]bool)
	for _, spec := range KVCatalog() {
		require.NoError(t, spec.Validate(), "catalog row %q must validate", spec.Name)
		assert.False(t, seen[spec.Name], "catalog row %q must be declared exactly once", spec.Name)
		seen[spec.Name] = true
	}
	assert.Len(t, seen, 22, "the catalog carries the 22 framework-guaranteed buckets")
}

// TestKVCatalog_DeclaredPolicies pins the architect-census policy decisions
// that enforcement derives from.
func TestKVCatalog_DeclaredPolicies(t *testing.T) {
	spec := func(name string) natsclient.BucketSpec {
		s, ok := SpecFor(name)
		require.True(t, ok, "catalog must declare %s", name)
		return s
	}

	// Owner decision 2026-07-28: ENTITY_STATES History = 1.
	assert.Equal(t, uint8(1), spec(BucketEntityStates).History)
	assert.Equal(t, natsclient.ClassAuthoritative, spec(BucketEntityStates).Class)
	assert.Equal(t, "graph-ingest", spec(BucketEntityStates).Owner)

	// GRAPH_STATUS keeps its readiness replay depth.
	assert.Equal(t, uint8(3), spec(BucketGraphStatus).History)
	// OWNER_CLAIMS keeps its registration audit depth.
	assert.Equal(t, uint8(10), spec(BucketOwnerClaims).History)

	// OWNER_PRESENCE is the live bounded-ttl proof: retention is per-descriptor.
	presence := spec(BucketOwnerPresence)
	assert.Equal(t, natsclient.RetentionBoundedTTL, presence.Retention.Kind)
	assert.Equal(t, ownerPresenceTTL, presence.Retention.TTL)

	// COMPONENT_STATUS: diagnostic / write-open / retention-unmanaged (#717).
	status := spec(BucketComponentStatus)
	assert.Equal(t, natsclient.ClassDiagnostic, status.Class)
	assert.Equal(t, natsclient.WriteOpen, status.Write)
	assert.Equal(t, natsclient.RetentionUnmanaged, status.Retention.Kind)

	// Everything except COMPONENT_STATUS is owner-only and, except
	// OWNER_PRESENCE, no-lifecycle.
	for _, s := range KVCatalog() {
		if s.Name == BucketComponentStatus {
			continue
		}
		assert.Equal(t, natsclient.WriteOwnerOnly, s.Write, "%s must be owner-only", s.Name)
		if s.Name == BucketOwnerPresence {
			continue
		}
		assert.Equal(t, natsclient.RetentionNoLifecycle, s.Retention.Kind,
			"%s must declare no lifecycle retention", s.Name)
	}
}

// TestFrameworkOwnedBuckets_DerivesFromWritePolicy is the derivation-not-
// snapshot proof at the unit level: a fixture WriteOwnerOnly entry appended to
// a descriptor slice appears in the derived owned view with no hand list to
// forget it from, and a write-open entry never does.
func TestFrameworkOwnedBuckets_DerivesFromWritePolicy(t *testing.T) {
	fixture := natsclient.BucketSpec{
		Name:      "FIXTURE_OWNED_BUCKET",
		Owner:     "fixture-owner",
		Class:     natsclient.ClassDerived,
		Retention: natsclient.RetentionPolicy{Kind: natsclient.RetentionNoLifecycle},
		Write:     natsclient.WriteOwnerOnly,
		Posture:   natsclient.PostureOwnerCreates,
		History:   1,
		Replicas:  1,
	}
	openFixture := fixture
	openFixture.Name = "FIXTURE_OPEN_BUCKET"
	openFixture.Write = natsclient.WriteOpen

	derivedView := frameworkOwnedFrom(append(KVCatalog(), fixture, openFixture))
	assert.Contains(t, derivedView, "FIXTURE_OWNED_BUCKET",
		"a WriteOwnerOnly entry must appear in the derived owned set")
	assert.NotContains(t, derivedView, "FIXTURE_OPEN_BUCKET",
		"a write-open entry must never appear in the owned set")
}

// TestFrameworkOwnedBuckets_ProductionView pins the production derived view:
// the 19 buckets of the retired hand list PLUS the two ownership buckets the
// derivation newly covers (they were always owner-written; the hand list had
// simply never been extended to them — the drift class the catalog kills),
// and NOT the write-open COMPONENT_STATUS.
func TestFrameworkOwnedBuckets_ProductionView(t *testing.T) {
	owned := FrameworkOwnedBuckets()
	assert.Len(t, owned, 21)
	for _, name := range []string{
		BucketEntityStates, BucketPredicateIndex, BucketIncomingIndex, BucketOutgoingIndex,
		BucketAliasIndex, BucketNameIndex, BucketEntitySuffixIndex, BucketSpatialIndex,
		BucketTemporalIndex, BucketTemporalIndexReverse, BucketContextIndex,
		BucketEmbeddingIndex, BucketEmbeddingDedup, BucketCommunityIndex,
		BucketCommunitySummaries, BucketAnomalyIndex, BucketStructuralIndex,
		BucketGraphIngestAppliedSeq, BucketGraphStatus,
		BucketOwnerClaims, BucketOwnerPresence,
	} {
		assert.True(t, IsFrameworkOwnedBucket(name), "%s must be framework-owned", name)
	}
	assert.False(t, IsFrameworkOwnedBucket(BucketComponentStatus),
		"COMPONENT_STATUS is write-open by declaration (#717), not owned")
	assert.False(t, IsFrameworkOwnedBucket("AGENT_LOOPS"),
		"application buckets are outside the catalog by rule")
}

// TestSpecFor_UnknownNameResolvesFalse is the F2 resolution contract: a name
// outside the catalog resolves to nothing, which callers turn into a boot
// failure naming the subject.
func TestSpecFor_UnknownNameResolvesFalse(t *testing.T) {
	_, ok := SpecFor("OUTGOING_INDEX_TYPO")
	assert.False(t, ok)
	assert.Empty(t, OwnerOf("OUTGOING_INDEX_TYPO"))

	spec, ok := SpecFor(BucketOutgoingIndex)
	require.True(t, ok)
	assert.Equal(t, "graph-index", spec.Owner)
	assert.Equal(t, spec.Owner, OwnerOf(BucketOutgoingIndex))
}

// TestErrorCodeBucketNotReady_MatchesIndexNotReady cross-pins the classified
// code the reader seam emits to the code graph/query already emits for a
// not-sound index, so classified consumers can never see the two drift apart.
func TestErrorCodeBucketNotReady_MatchesIndexNotReady(t *testing.T) {
	assert.Equal(t, ErrorCodeIndexNotReady, natsclient.ErrorCodeBucketNotReady)
}
