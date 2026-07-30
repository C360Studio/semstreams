package graph

import (
	"context"
	"testing"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/natsclient"
)

// attributionLister is a minimal account listing over a fixed set of infos, so
// this test can drive the real collector against real catalog names without a
// server. The two listings agree: this test is about attribution, not about the
// offline-resource reconciliation.
type attributionLister struct {
	infos []*jetstream.StreamInfo
}

func (l *attributionLister) ListStreams(
	_ context.Context, _ ...jetstream.StreamListOpt,
) jetstream.StreamInfoLister {
	return &attributionInfoWalk{infos: l.infos}
}

func (l *attributionLister) StreamNames(
	_ context.Context, _ ...jetstream.StreamListOpt,
) jetstream.StreamNameLister {
	names := make([]string, 0, len(l.infos))
	for _, info := range l.infos {
		names = append(names, info.Config.Name)
	}
	return &attributionNameWalk{names: names}
}

type attributionInfoWalk struct {
	infos []*jetstream.StreamInfo
}

func (w *attributionInfoWalk) Info() <-chan *jetstream.StreamInfo {
	ch := make(chan *jetstream.StreamInfo, len(w.infos))
	for _, info := range w.infos {
		ch <- info
	}
	close(ch)
	return ch
}

func (w *attributionInfoWalk) Err() error { return nil }

type attributionNameWalk struct {
	names []string
}

func (w *attributionNameWalk) Name() <-chan string {
	ch := make(chan string, len(w.names))
	for _, name := range w.names {
		ch <- name
	}
	close(ch)
	return ch
}

func (w *attributionNameWalk) Err() error { return nil }

func attributionStream(name string) *jetstream.StreamInfo {
	return &jetstream.StreamInfo{Config: jetstream.StreamConfig{Name: name}}
}

// TestOwnerOfIsTheStorageInventoryAttributionSeam is the cross-pin that makes
// "the inventory MUST NOT maintain its own bucket-to-owner mapping" mechanical
// rather than review-enforced.
//
// It proves three things at once:
//
//   - OwnerOf satisfies natsclient.OwnerResolver as-is, so the inventory can be
//     wired to the catalog directly with no adapter, no copy, and nothing to
//     keep in sync. natsclient cannot import graph (graph imports natsclient),
//     so the seam is an injected function; this is the compile-time proof that
//     the function it must be injected with fits.
//   - A real catalog bucket resolves to its DECLARED owner, read from the same
//     table the acquisition seam enforces from — not from a list maintained
//     next to the inventory that could drift from it.
//   - A doubled prefix is not over-stripped against the real catalog: the
//     backing stream KV_KV_ENTITY_STATES belongs to a bucket literally named
//     KV_ENTITY_STATES, which the catalog does not declare, so it reports
//     UNATTRIBUTED. An over-stripping collector would resolve ENTITY_STATES and
//     mis-attribute a foreign bucket to graph-ingest.
func TestOwnerOfIsTheStorageInventoryAttributionSeam(t *testing.T) {
	// Compile-time proof of the seam's shape.
	var resolver natsclient.OwnerResolver = OwnerOf

	lister := &attributionLister{infos: []*jetstream.StreamInfo{
		attributionStream(natsclient.KVStreamPrefix + BucketEntityStates),
		attributionStream(natsclient.KVStreamPrefix + natsclient.KVStreamPrefix + BucketEntityStates),
		attributionStream(natsclient.KVStreamPrefix + "A_PRODUCT_BUCKET_OUTSIDE_THE_CATALOG"),
		attributionStream(natsclient.ObjectStoreStreamPrefix + BucketEntityStates),
	}}

	collector, err := natsclient.NewStorageInventoryCollector(
		func() (natsclient.StreamLister, error) { return lister, nil },
		natsclient.StorageInventoryConfig{OwnerResolver: resolver, ProducedBy: "unit-test"},
	)
	require.NoError(t, err)

	inv, err := collector.Collect(context.Background())
	require.NoError(t, err)

	got := make(map[string]natsclient.StorageResource, len(inv.Resources))
	for _, res := range inv.Resources {
		got[res.Name] = res
	}
	require.Len(t, got, 4)

	declared, ok := SpecFor(BucketEntityStates)
	require.True(t, ok, "test premise: the bucket is in the catalog")

	entityStates := got[natsclient.KVStreamPrefix+BucketEntityStates]
	assert.Equal(t, declared.Owner, entityStates.Owner,
		"the reported owner IS the catalog descriptor's declared owner")
	assert.Equal(t, natsclient.AttributionAttributed, entityStates.Attribution)
	assert.True(t, entityStates.Attributed())
	assert.Equal(t, BucketEntityStates, entityStates.Bucket)

	doubled := got[natsclient.KVStreamPrefix+natsclient.KVStreamPrefix+BucketEntityStates]
	assert.Equal(t, natsclient.KVStreamPrefix+BucketEntityStates, doubled.Bucket)
	assert.Equal(t, natsclient.AttributionUnattributed, doubled.Attribution,
		"a bucket literally named KV_ENTITY_STATES is not the catalog's ENTITY_STATES")
	assert.Equal(t, "", doubled.Owner)
	assert.False(t, doubled.Attributed())

	outside := got[natsclient.KVStreamPrefix+"A_PRODUCT_BUCKET_OUTSIDE_THE_CATALOG"]
	assert.Equal(t, natsclient.AttributionUnattributed, outside.Attribution)
	assert.Equal(t, "", outside.Owner)
	assert.False(t, outside.Attributed())
	assert.Equal(t, natsclient.ResourceKeyValue, outside.Kind,
		"a non-catalog bucket is still enumerated, just unattributed")

	// An ObjectStore backing stream is NOT a KV bucket, so the KV catalog is not
	// consulted for it even when the trailing name collides with a catalog entry
	// — and ownership is NOT-APPLICABLE there, not merely undeclared.
	objectStore := got[natsclient.ObjectStoreStreamPrefix+BucketEntityStates]
	assert.Equal(t, natsclient.ResourceObjectStore, objectStore.Kind)
	assert.Equal(t, natsclient.AttributionNotApplicable, objectStore.Attribution)
	assert.NotEqual(t, outside.Attribution, objectStore.Attribution,
		"an escaped KV bucket is a finding; an ObjectStore having no owner concept is not")
	assert.Equal(t, "", objectStore.Owner)
}

// TestOwnerOfReportsUnattributedForRemovedCatalogRow is the spec's
// "attribution follows the catalog rather than a copy of it" scenario expressed
// against the real catalog function: OwnerOf resolves through KVCatalog() on
// every call, so a row removed from the table stops resolving immediately. The
// inventory holds no mapping that could outlive the row.
func TestOwnerOfReportsUnattributedForRemovedCatalogRow(t *testing.T) {
	require.NotEmpty(t, OwnerOf(BucketEntityStates),
		"test premise: a declared bucket resolves to an owner")

	// A name that is not (and must never become) a catalog row stands in for a
	// removed one: OwnerOf consults the table, so both answer the same way.
	assert.Equal(t, "", OwnerOf("A_BUCKET_NO_CATALOG_ROW_DECLARES"),
		"a bucket the catalog does not declare has no owner to report")

	names := make(map[string]bool)
	for _, spec := range KVCatalog() {
		names[spec.Name] = true
	}
	assert.False(t, names["A_BUCKET_NO_CATALOG_ROW_DECLARES"])
}
