package graphembedding

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph/embedding"
	"github.com/nats-io/nats.go/jetstream"
)

func TestSimilarQueryPropagatesCallerCancellation(t *testing.T) {
	t.Parallel()

	record := embedding.Record{
		EntityID: "acme.ops.robotics.gcs.drone.001",
		Vector:   []float32{1, 0},
		Status:   embedding.StatusGenerated,
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	bucket := newMockKVBucket()
	bucket.getFunc = func(ctx context.Context, _ string) (jetstream.KeyValueEntry, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return &mockKVEntry{data: data}, nil
	}
	c := &Component{storage: embedding.NewStorage(bucket, nil)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = c.handleQuerySimilarNATS(ctx, []byte(`{"entity_id":"acme.ops.robotics.gcs.drone.001"}`))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("query error = %v, want caller context cancellation", err)
	}
}

func TestSimilarityScanDoesNotReturnPartialResultsOnEntryFailure(t *testing.T) {
	t.Parallel()

	backendErr := errors.New("injected embedding read failure")
	bucket := &embeddingQueryBucket{mockKVBucket: newMockKVBucket(), keys: []string{"candidate"}}
	bucket.getFunc = func(context.Context, string) (jetstream.KeyValueEntry, error) {
		return nil, backendErr
	}
	c := &Component{storage: embedding.NewStorage(bucket, nil)}

	_, err := c.findSimilarEntities(context.Background(), "", []float32{1, 0}, nil, 10)
	if !errors.Is(err, backendErr) {
		t.Fatalf("scan error = %v, want injected backend failure", err)
	}
}

func TestSimilarityScanRejectsMalformedEmbeddingRecord(t *testing.T) {
	t.Parallel()

	bucket := &embeddingQueryBucket{mockKVBucket: newMockKVBucket(), keys: []string{"candidate"}}
	bucket.getFunc = func(context.Context, string) (jetstream.KeyValueEntry, error) {
		return &mockKVEntry{data: []byte(`{"entity_id":`)}, nil
	}
	c := &Component{storage: embedding.NewStorage(bucket, nil)}

	if _, err := c.findSimilarEntities(context.Background(), "", []float32{1, 0}, nil, 10); err == nil {
		t.Fatal("malformed embedding record was silently omitted from a successful response")
	}
}

func TestSimilarityScanRejectsCancellationWithoutPartialResults(t *testing.T) {
	t.Parallel()

	bucket := &embeddingQueryBucket{mockKVBucket: newMockKVBucket(), keys: []string{"candidate"}}
	bucket.getFunc = func(ctx context.Context, _ string) (jetstream.KeyValueEntry, error) {
		return nil, ctx.Err()
	}
	c := &Component{storage: embedding.NewStorage(bucket, nil)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.findSimilarEntities(ctx, "", []float32{1, 0}, nil, 10)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("scan error = %v, want context cancellation", err)
	}
}

func TestVectorCacheStopsBeingAuthoritativeAfterWatcherLoss(t *testing.T) {
	t.Parallel()

	watcher := &bootstrapWatcher{updates: make(chan jetstream.KeyValueEntry, 2)}
	queryBucket := &embeddingQueryBucket{mockKVBucket: newMockKVBucket(), keys: []string{"entity-1"}}
	bucket := &vectorCacheBucket{embeddingQueryBucket: queryBucket, watcher: watcher}
	storage := embedding.NewStorage(bucket, nil)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := storage.StartVectorCache(ctx); err != nil {
		t.Fatal(err)
	}

	watcher.updates <- generatedEmbeddingEntry(t, "entity-1")
	watcher.updates <- nil
	waitForCacheAuthority(t, storage, true)
	close(watcher.updates)
	waitForCacheAuthority(t, storage, false)

	// A lost watcher must force the query path back to authoritative KV rather
	// than serving the stale in-memory snapshot.
	data := generatedEmbeddingData(t, "entity-1")
	if _, err := queryBucket.Put(context.Background(), "entity-1", data); err != nil {
		t.Fatal(err)
	}
	c := &Component{storage: storage}
	results, err := c.findSimilarEntities(context.Background(), "", []float32{1, 0}, nil, 10)
	if err != nil {
		t.Fatalf("authoritative fallback failed: %v", err)
	}
	if len(results) != 1 || results[0].EntityID != "entity-1" {
		t.Fatalf("authoritative fallback results = %#v, want entity-1", results)
	}
}

func TestVectorCacheStopsBeingAuthoritativeAfterMalformedLiveUpdate(t *testing.T) {
	t.Parallel()

	watcher := &bootstrapWatcher{updates: make(chan jetstream.KeyValueEntry, 3)}
	queryBucket := &embeddingQueryBucket{mockKVBucket: newMockKVBucket(), keys: []string{"entity-1"}}
	bucket := &vectorCacheBucket{embeddingQueryBucket: queryBucket, watcher: watcher}
	storage := embedding.NewStorage(bucket, nil)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := storage.StartVectorCache(ctx); err != nil {
		t.Fatal(err)
	}

	watcher.updates <- generatedEmbeddingEntry(t, "entity-1")
	watcher.updates <- nil
	waitForCacheAuthority(t, storage, true)
	watcher.updates <- &bootstrapEntry{key: "entity-1", data: []byte(`{"entity_id":`), revision: 2}
	waitForCacheAuthority(t, storage, false)

	// The malformed record is authoritative too, so the fallback must return an
	// error instead of exposing the stale cached vector as a successful result.
	if _, err := queryBucket.Put(context.Background(), "entity-1", []byte(`{"entity_id":`)); err != nil {
		t.Fatal(err)
	}
	c := &Component{storage: storage}
	if _, err := c.findSimilarEntities(context.Background(), "", []float32{1, 0}, nil, 10); err == nil {
		t.Fatal("malformed authoritative record was hidden by stale cache success")
	}
}

type embeddingQueryBucket struct {
	*mockKVBucket
	keys []string
}

func (b *embeddingQueryBucket) ListKeys(context.Context, ...jetstream.WatchOpt) (jetstream.KeyLister, error) {
	keys := make(chan string, len(b.keys))
	for _, key := range b.keys {
		keys <- key
	}
	close(keys)
	return &embeddingKeyLister{keys: keys}, nil
}

type embeddingKeyLister struct {
	keys chan string
}

func (l *embeddingKeyLister) Keys() <-chan string                    { return l.keys }
func (l *embeddingKeyLister) Status() <-chan jetstream.KeyValueEntry { return nil }
func (l *embeddingKeyLister) Stop() error                            { return nil }

type vectorCacheBucket struct {
	*embeddingQueryBucket
	watcher jetstream.KeyWatcher
}

func (b *vectorCacheBucket) WatchAll(context.Context, ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	return b.watcher, nil
}

func generatedEmbeddingEntry(t *testing.T, entityID string) jetstream.KeyValueEntry {
	t.Helper()
	return &bootstrapEntry{key: entityID, data: generatedEmbeddingData(t, entityID), revision: 1}
}

func generatedEmbeddingData(t *testing.T, entityID string) []byte {
	t.Helper()
	data, err := json.Marshal(embedding.Record{
		EntityID: entityID,
		Vector:   []float32{1, 0},
		Status:   embedding.StatusGenerated,
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func waitForCacheAuthority(t *testing.T, storage *embedding.Storage, want bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		_, got := storage.FindSimilarFromCache("", []float32{1, 0}, nil, 10)
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("cache authority = %t, want %t", got, want)
		}
		time.Sleep(time.Millisecond)
	}
}
