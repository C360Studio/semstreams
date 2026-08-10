package graphquery

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/clustering"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

type controlledCommunityWatcher struct {
	updates chan jetstream.KeyValueEntry
	stopped chan struct{}
	once    sync.Once
}

func newControlledCommunityWatcher() *controlledCommunityWatcher {
	return &controlledCommunityWatcher{
		updates: make(chan jetstream.KeyValueEntry, 8),
		stopped: make(chan struct{}),
	}
}

func (w *controlledCommunityWatcher) Updates() <-chan jetstream.KeyValueEntry { return w.updates }
func (w *controlledCommunityWatcher) Stop() error {
	w.once.Do(func() { close(w.stopped) })
	return nil
}

type controlledCommunityReader struct {
	graph.CatalogReader
	watcher *controlledCommunityWatcher
	called  chan struct{}
	once    sync.Once
}

func (r *controlledCommunityReader) WatchAll(context.Context, ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	r.once.Do(func() { close(r.called) })
	return r.watcher, nil
}

type communityEntry struct {
	key       string
	value     []byte
	operation jetstream.KeyValueOp
}

func (e communityEntry) Bucket() string                  { return graph.BucketCommunityIndex }
func (e communityEntry) Key() string                     { return e.key }
func (e communityEntry) Value() []byte                   { return e.value }
func (e communityEntry) Revision() uint64                { return 1 }
func (e communityEntry) Created() time.Time              { return time.Time{} }
func (e communityEntry) Delta() uint64                   { return 0 }
func (e communityEntry) Operation() jetstream.KeyValueOp { return e.operation }

func communityPutEntry(t *testing.T, level int, id string, members ...string) jetstream.KeyValueEntry {
	t.Helper()
	value, err := json.Marshal(clustering.Community{ID: id, Level: level, Members: members})
	require.NoError(t, err)
	return communityEntry{key: communityKVKey(level, id), value: value, operation: jetstream.KeyValuePut}
}

func TestCommunityGenerationSupervisorPublishesOnlySentinelAndReplacesFresh(t *testing.T) {
	cache := newTestCache()
	published := make(chan uint64, 3)
	cache.onPublished = func(generation uint64) { published <- generation }
	applied := make(chan uint64, 8)
	cache.onApplied = func(generation uint64, _ string) { applied <- generation }
	firstWatcher := newControlledCommunityWatcher()
	secondWatcher := newControlledCommunityWatcher()
	firstReader := &controlledCommunityReader{watcher: firstWatcher, called: make(chan struct{})}
	secondReader := &controlledCommunityReader{watcher: secondWatcher, called: make(chan struct{})}

	openCalls := make(chan int, 3)
	allowRetry := make(chan struct{}, 2)
	retryEntered := make(chan struct{}, 3)
	openIndex := 0
	component := &Component{
		communityCache: cache,
		logger:         cache.logger,
		openCommunityReader: func(context.Context) (graph.CatalogReader, error) {
			openIndex++
			openCalls <- openIndex
			switch openIndex {
			case 1:
				return nil, errors.New("absent")
			case 2:
				return firstReader, nil
			default:
				return secondReader, nil
			}
		},
		waitCommunityRetry: func(ctx context.Context, _ time.Duration) bool {
			retryEntered <- struct{}{}
			select {
			case <-ctx.Done():
				return false
			case <-allowRetry:
				return true
			}
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		component.superviseCommunityGenerations(ctx)
		close(done)
	}()

	require.Equal(t, 1, <-openCalls)
	require.Nil(t, cache.acquire(), "absent bucket cannot publish a generation")
	<-retryEntered
	allowRetry <- struct{}{}
	require.Equal(t, 2, <-openCalls)
	<-firstReader.called

	firstWatcher.updates <- communityPutEntry(t, 0, entDrone1, entDrone1)
	require.Equal(t, uint64(2), <-applied)
	require.Nil(t, cache.acquire(), "pre-sentinel updates remain staging")
	require.Nil(t, cache.acquire(), "staging is unreachable to requests")

	firstWatcher.updates <- nil
	require.Equal(t, uint64(2), <-published)
	firstLease := cache.acquire()
	require.NotNil(t, firstLease)
	require.NotNil(t, firstLease.getCommunity(0, entDrone1))
	require.True(t, firstLease.valid())

	firstWatcher.updates <- communityEntry{key: communityKVKey(0, entDrone1), operation: jetstream.KeyValueDelete}
	require.Equal(t, uint64(2), <-applied)
	require.Nil(t, firstLease.getCommunity(0, entDrone1), "post-sentinel delete updates the active generation")
	firstWatcher.updates <- communityPutEntry(t, 0, entDrone1, entDrone1)
	require.Equal(t, uint64(2), <-applied)
	require.NotNil(t, firstLease.getCommunity(0, entDrone1), "post-sentinel update reaches the active generation")

	close(firstWatcher.updates)
	<-retryEntered
	require.False(t, firstLease.valid())
	require.Nil(t, cache.acquire(), "watch close unpublishes before retry")
	allowRetry <- struct{}{}
	require.Equal(t, 3, <-openCalls)
	<-secondReader.called

	secondWatcher.updates <- nil // valid empty replacement generation
	require.Equal(t, uint64(3), <-published)
	secondLease := cache.acquire()
	require.NotNil(t, secondLease)
	require.NotEqual(t, firstLease.generationID(), secondLease.generationID())
	require.Empty(t, secondLease.getAllCommunities(), "replacement is fresh, never seeded from generation N")

	firstLease.generation.applyUpdate(communityKVKey(0, entDrone2), mustCommunityJSON(t,
		&clustering.Community{ID: entDrone2, Level: 0, Members: []string{entDrone2}}))
	cache.unpublish(firstLease.generation)
	require.True(t, secondLease.valid(), "late generation-N update/exit cannot affect N+1")
	require.Nil(t, secondLease.getCommunity(0, entDrone2))

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("supervisor did not stop after cancellation")
	}
	require.False(t, secondLease.valid(), "orderly cancellation must revoke the active generation")
	require.Nil(t, cache.acquire(), "orderly cancellation must unpublish readiness")
}

func TestCommunityLeaseFinalValidationRejectsReplacement(t *testing.T) {
	cache := newTestCache()
	first := newCommunityGeneration(1)
	first.applyUpdate(communityKVKey(0, entDrone1), mustCommunityJSON(t,
		&clustering.Community{ID: entDrone1, Level: 0, Members: []string{entDrone1}}))
	cache.publish(first)
	lease := cache.acquire()
	require.NotNil(t, lease.getEntityCommunity(entDrone1, 0))

	cache.publish(newCommunityGeneration(2))
	require.False(t, lease.valid(), "the same generation must validate immediately before return")
}

func TestCommunityLeaseCompletionLinearizesValidationAndAccounting(t *testing.T) {
	cache := newTestCache()
	generation := newCommunityGeneration(1)
	cache.publish(generation)
	lease := cache.acquire()
	require.NotNil(t, lease)

	accounted := false
	writerEntered := false
	require.True(t, lease.completeSuccess(func() {
		accounted = true
		writerEntered = cache.mu.TryLock()
		if writerEntered {
			cache.mu.Unlock()
		}
	}))
	require.True(t, accounted)
	require.False(t, writerEntered, "replacement/unpublish must be excluded through success accounting")

	cache.publish(newCommunityGeneration(2))
	accounted = false
	require.False(t, lease.completeSuccess(func() { accounted = true }))
	require.False(t, accounted, "an invalid lease must not execute success accounting")
}

func mustCommunityJSON(t *testing.T, community *clustering.Community) []byte {
	t.Helper()
	data, err := json.Marshal(community)
	require.NoError(t, err)
	return data
}
