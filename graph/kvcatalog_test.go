package graph

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"testing"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCatalogReader_ExportedSurfaceIsReadOnly pins the complete exported
// capability. Adding a method is an API change and must re-enter the exported-
// surface gate; in particular, no mutation or raw-handle escape hatch belongs
// here.
func TestCatalogReader_ExportedSurfaceIsReadOnly(t *testing.T) {
	typeOfReader := reflect.TypeOf((*CatalogReader)(nil)).Elem()
	methods := make([]string, 0, typeOfReader.NumMethod())
	for i := 0; i < typeOfReader.NumMethod(); i++ {
		methods = append(methods, typeOfReader.Method(i).Name)
	}
	sort.Strings(methods)

	assert.Equal(t, []string{
		"Get",
		"Keys",
		"ListKeys",
		"ListKeysFiltered",
		"Status",
		"Watch",
		"WatchAll",
	}, methods)
}

// TestOpenCatalogReader_InvalidNamePreservesClassification proves catalog
// resolution still fails before client access and retains the canonical
// invalid outcome shape.
func TestOpenCatalogReader_InvalidNamePreservesClassification(t *testing.T) {
	_, err := OpenCatalogReader(context.Background(), nil, "OUTGOING_INDEX_TYPO")
	require.Error(t, err)

	var classified *errs.ClassifiedError
	require.True(t, errors.As(err, &classified))
	assert.Equal(t, errs.ErrorInvalid, classified.Class)
	assert.Empty(t, classified.Code)
	assert.Nil(t, classified.Detail)
	assert.Equal(t, "graph", classified.Component)
	assert.Equal(t, "OpenCatalogReader", classified.Operation)
	assert.Contains(t, err.Error(), `bucket "OUTGOING_INDEX_TYPO" is not in the framework KV catalog`)
	wrapped := errors.Unwrap(classified)
	require.Error(t, wrapped)
	assert.EqualError(t, errors.Unwrap(wrapped), `bucket "OUTGOING_INDEX_TYPO" is not in the framework KV catalog`)
}

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
	assert.Len(t, seen, 18, "the catalog carries the 18 retained framework-guaranteed buckets")
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
	// Every catalog bucket is owner-only and no-lifecycle.
	for _, s := range KVCatalog() {
		assert.Equal(t, natsclient.WriteOwnerOnly, s.Write, "%s must be owner-only", s.Name)
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

// TestFrameworkOwnedBuckets_ProductionView pins the production derived view.
func TestFrameworkOwnedBuckets_ProductionView(t *testing.T) {
	owned := FrameworkOwnedBuckets()
	assert.Len(t, owned, 18)
	for _, name := range []string{
		BucketEntityStates, BucketPredicateIndex, BucketIncomingIndex, BucketOutgoingIndex,
		BucketAliasIndex, BucketNameIndex, BucketEntitySuffixIndex, BucketSpatialIndex,
		BucketTemporalIndex, BucketTemporalIndexReverse,
		BucketEmbeddingIndex, BucketEmbeddingDedup, BucketCommunityIndex,
		BucketCommunitySummaries, BucketAnomalyIndex,
		BucketGraphIngestAppliedSeq, BucketGraphStatus, BucketStorageReport,
	} {
		assert.True(t, IsFrameworkOwnedBucket(name), "%s must be framework-owned", name)
	}
	assert.False(t, IsFrameworkOwnedBucket("CONTEXT_INDEX"),
		"the retired provenance-only bucket must not remain in the generic write guard")
	assert.False(t, IsFrameworkOwnedBucket("STRUCTURAL_INDEX"),
		"the retired structural persistence bucket must not remain in the generic write guard")
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
// code the catalog-reader seam emits to the code graph-index emits for a
// not-sound index, so classified consumers can never see the two drift apart.
func TestErrorCodeBucketNotReady_MatchesIndexNotReady(t *testing.T) {
	assert.Equal(t, ErrorCodeIndexNotReady, natsclient.ErrorCodeBucketNotReady)
}
