//go:build integration

package service

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bucketSweepRecordingHandler captures emitted slog records so a test can assert
// the post-start owned-bucket sweep WARNed naming a stripped bucket.
type bucketSweepRecordingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *bucketSweepRecordingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *bucketSweepRecordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}
func (h *bucketSweepRecordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *bucketSweepRecordingHandler) WithGroup(string) slog.Handler      { return h }

func (h *bucketSweepRecordingHandler) warnMentioning(bucket string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.records {
		if r.Level != slog.LevelWarn {
			continue
		}
		if strings.Contains(r.Message, bucket) {
			return true
		}
		found := false
		r.Attrs(func(a slog.Attr) bool {
			if strings.Contains(a.Value.String(), bucket) {
				found = true
				return false
			}
			return true
		})
		if found {
			return true
		}
	}
	return false
}

// guardedBucketCreatingService models an owning graph component whose Start
// get-or-creates a framework-owned bucket. CreateKeyValueBucket adopts a
// pre-existing (dirty) bucket UNCHANGED, which is exactly the create-race the
// pre-start WireOwnership belt cannot see: the bucket is absent when the belt
// runs, then created/adopted dirty during the service-start loop.
type guardedBucketCreatingService struct {
	*mockService
	client *natsclient.Client
	bucket string
}

func (s *guardedBucketCreatingService) Start(ctx context.Context) error {
	if err := s.mockService.Start(ctx); err != nil {
		return err
	}
	_, err := s.client.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{Bucket: s.bucket})
	return err
}

// TestIntegration_StartAll_PostStartSweepStripsCreateRaceTTL is the F1
// production-wire test: it drives the real Manager.StartAll seam (not the sweep
// helper directly) and proves the SECOND, post-start owned-bucket sweep closes
// the create-race coverage hole the pre-start belt cannot reach.
//
// A guarded bucket is pre-created DIRTY (foreign 7-day TTL + a stored key)
// before StartAll; an owning-style service's Start get-or-creates it, adopting
// the dirty config unchanged (CreateKeyValueBucket never reconciles). After the
// service-start loop, StartAll's post-start sweep must strip the TTL in place,
// preserve the stored key, and WARN naming the bucket — all BEFORE the HTTP
// surface reports healthy.
func TestIntegration_StartAll_PostStartSweepStripsCreateRaceTTL(t *testing.T) {
	ctx := context.Background()
	client := natsclient.NewTestClient(t, natsclient.WithKV()).Client

	dirty, err := client.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: graph.BucketEmbeddingIndex,
		TTL:    7 * 24 * time.Hour,
	})
	require.NoError(t, err)
	_, err = dirty.Put(ctx, "entity.key.one", []byte("survivor"))
	require.NoError(t, err)

	// Precondition: the TTL is really set.
	maxAge, _, err := natsclient.BucketRetention(ctx, dirty)
	require.NoError(t, err)
	require.Equal(t, 7*24*time.Hour, maxAge, "precondition: bucket must carry the foreign TTL")

	rec := &bucketSweepRecordingHandler{}
	registry := NewServiceRegistry()
	manager := NewServiceManager(registry)
	// Inject a recording logger so the sweep's WARN is observable, and the NATS
	// client the post-start sweep reads. Both are in-package private fields.
	manager.BaseService = NewBaseServiceWithOptions("service-manager-registry", nil, WithLogger(slog.New(rec)))
	manager.natsClient = client

	// A mandatory component-manager mock so createMandatoryServices is a no-op,
	// and the owning-style service whose Start adopts the dirty guarded bucket.
	manager.RegisterInstance("component-manager", newMockService("component-manager"))
	manager.RegisterInstance("embedding-index-owner", &guardedBucketCreatingService{
		mockService: newMockService("embedding-index-owner"),
		client:      client,
		bucket:      graph.BucketEmbeddingIndex,
	})

	require.NoError(t, manager.StartAll(ctx))
	defer func() { _ = manager.StopAll(2 * time.Second) }()

	// The post-start sweep stripped the create-race TTL in place.
	fresh, err := client.GetKeyValueBucket(ctx, graph.BucketEmbeddingIndex)
	require.NoError(t, err)
	var maxBytes int64
	maxAge, maxBytes, err = natsclient.BucketRetention(ctx, fresh)
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), maxAge, "StartAll's post-start sweep must strip the create-race TTL")
	assert.LessOrEqual(t, maxBytes, int64(0), "the sweep must leave MaxBytes non-binding")

	// The stored key survived the strip.
	entry, err := fresh.Get(ctx, "entity.key.one")
	require.NoError(t, err, "the stored key must survive the strip")
	assert.Equal(t, []byte("survivor"), entry.Value())

	// A WARN naming the stripped bucket fired.
	assert.True(t, rec.warnMentioning(graph.BucketEmbeddingIndex),
		"the post-start sweep must WARN naming the stripped bucket %s", graph.BucketEmbeddingIndex)
}
