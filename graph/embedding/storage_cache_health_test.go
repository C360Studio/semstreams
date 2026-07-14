package embedding

import (
	"testing"
	"time"
)

func TestVectorCacheInvalidationLinearizesWithActiveRead(t *testing.T) {
	s := &Storage{
		vectorCache: map[string][]float32{"entity-1": {1, 0}},
		cacheReady:  make(chan struct{}),
	}
	close(s.cacheReady)
	s.cacheWatchHealthy = true

	readEntered := make(chan struct{})
	releaseRead := make(chan struct{})
	queryDone := make(chan bool, 1)
	go func() {
		_, ok := s.FindSimilarFromCache("", []float32{1, 0}, func(string) bool {
			close(readEntered)
			<-releaseRead
			return true
		}, 10)
		queryDone <- ok
	}()
	<-readEntered // FindSimilarFromCache now holds vectorCacheMu.RLock.

	invalidationStarted := make(chan struct{})
	invalidationDone := make(chan struct{})
	go func() {
		close(invalidationStarted)
		s.invalidateVectorCache()
		close(invalidationDone)
	}()
	<-invalidationStarted

	// Invalidation uses the write side of the same mutex and therefore cannot
	// overtake the already-linearized cache read. The old atomic-bool design
	// completed here while the read still held vectorCacheMu.RLock.
	select {
	case <-invalidationDone:
		t.Fatal("cache invalidation overtook an active cache read")
	case <-time.After(100 * time.Millisecond):
	}

	close(releaseRead)
	if ok := <-queryDone; !ok {
		t.Fatal("cache read that linearized before invalidation was rejected")
	}
	select {
	case <-invalidationDone:
	case <-time.After(time.Second):
		t.Fatal("cache invalidation did not complete after active read released")
	}

	if _, ok := s.FindSimilarFromCache("", []float32{1, 0}, nil, 10); ok {
		t.Fatal("cache remained authoritative after invalidation completed")
	}
}
