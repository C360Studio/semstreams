package graphembedding

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/storage/objectstore"
)

// Regression guard for the gh#354-session find: #264 (ADR-055 Wave 0) began
// lifting StorageRef onto the EntityState at the ingest seam. graph-embedding
// then committed to the StorageRef→ObjectStore fetch path whenever a StorageRef
// was present — but in configs with no content store wired (e.g. the BM25
// statistical tier), fetchTextFromStorage hard-fails ("content store not
// configured"), markFailed fires, and ALL embedding/search silently collapses
// (statistical known-answer 7/7→0/7, shipped beta.113–115, masked as e2e
// warnings).
//
// The fix: only take the StorageRef path when a content store is actually wired;
// otherwise fall back to inline text extraction. shouldFetchViaStorageRef
// encodes that decision.
func TestShouldFetchViaStorageRef(t *testing.T) {
	storageRef := &message.StorageReference{StorageInstance: "objstore", Key: "k/1"}

	cases := []struct {
		name         string
		hasRef       bool
		contentStore *objectstore.Store // nil => no store-read port configured
		want         bool
	}{
		{
			name:         "StorageRef + content store wired -> fetch offloaded content",
			hasRef:       true,
			contentStore: &objectstore.Store{},
			want:         true,
		},
		{
			name:         "StorageRef but NO content store -> fall back to inline (the regression)",
			hasRef:       true,
			contentStore: nil,
			want:         false,
		},
		{
			name:         "no StorageRef -> inline path regardless",
			hasRef:       false,
			contentStore: &objectstore.Store{},
			want:         false,
		},
		{
			name:         "no StorageRef, no content store -> inline path",
			hasRef:       false,
			contentStore: nil,
			want:         false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			es := &graph.EntityState{ID: "c360.platform.test.sys.widget.001"}
			if tc.hasRef {
				es.StorageRef = storageRef
			}
			c := &Component{contentStore: tc.contentStore}
			assert.Equal(t, tc.want, c.shouldFetchViaStorageRef(es))
		})
	}
}

// TestExtractTextForEmbedding_RecoversInlineContentForStorageRefEntity locks the
// other half of the fallback: an entity that carries BOTH a StorageRef and
// inline content triples (the exact shape of the e2e document/sensor entities)
// still yields non-empty text via inline extraction, so the no-content-store
// fallback path actually has something to embed.
func TestExtractTextForEmbedding_RecoversInlineContentForStorageRefEntity(t *testing.T) {
	c := &Component{}
	es := &graph.EntityState{
		ID:         "c360.logistics.maintenance.work.completed.maint-001",
		StorageRef: &message.StorageReference{StorageInstance: "objstore", Key: "k/maint-001"},
		Triples: []message.Triple{
			{Subject: "x", Predicate: "maintenance.title", Object: "Hydraulic pump service"},
			{Subject: "x", Predicate: "maintenance.description", Object: "Replaced seals on hydraulic reservoir"},
		},
	}
	got := c.extractTextForEmbedding(es)
	assert.NotEmpty(t, got, "inline content triples must still be extractable so the no-content-store fallback can embed")
	assert.Contains(t, got, "Hydraulic pump service")
	assert.Contains(t, got, "Replaced seals")
}
