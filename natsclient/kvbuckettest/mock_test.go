// Package kvbuckettest_test verifies that MockKVBucket honours the natsclient.KVBucket contract.
//
// These are pure unit tests — no testcontainer required. They exist to catch
// regressions in the mock itself so a broken mock does not silently pass
// tracker tests that depend on correct KVBucket semantics.
package kvbuckettest_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/natsclient/kvbuckettest"
)

func TestMockKVBucket_GetPutRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := kvbuckettest.NewMockKVBucket()

	if _, err := m.Get(ctx, "missing"); !errors.Is(err, natsclient.ErrKeyNotFound) {
		t.Fatalf("Get missing key: got %v, want ErrKeyNotFound", err)
	}

	rev, err := m.Put(ctx, "k", []byte("v1"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if rev == 0 {
		t.Fatal("Put returned revision 0, want > 0")
	}

	entry, err := m.Get(ctx, "k")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(entry.Value) != "v1" {
		t.Errorf("Get value = %q, want %q", entry.Value, "v1")
	}
	if entry.Revision != rev {
		t.Errorf("Get revision = %d, want %d", entry.Revision, rev)
	}
}

func TestMockKVBucket_Update_CAS(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := kvbuckettest.NewMockKVBucket()

	rev, _ := m.Put(ctx, "k", []byte("v1"))

	// Correct revision — should succeed.
	rev2, err := m.Update(ctx, "k", []byte("v2"), rev)
	if err != nil {
		t.Fatalf("Update with correct revision: %v", err)
	}
	if rev2 <= rev {
		t.Errorf("new revision %d should be > old revision %d", rev2, rev)
	}

	// Stale revision — should return ErrKeyExists.
	_, err = m.Update(ctx, "k", []byte("v3"), rev) // rev is now stale
	if !errors.Is(err, natsclient.ErrKeyExists) {
		t.Fatalf("Update with stale revision: got %v, want ErrKeyExists", err)
	}
}

func TestMockKVBucket_UpdateHook_InjectConflict(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := kvbuckettest.NewMockKVBucket()

	rev, _ := m.Put(ctx, "k", []byte("v1"))

	// Inject a conflict on the first call, then let subsequent calls through.
	calls := 0
	m.SetUpdateHook(func(key string, lastRevision uint64) uint64 {
		calls++
		if calls == 1 {
			return lastRevision + 99 // wrong — triggers conflict
		}
		return lastRevision // correct — allows write
	})

	// First attempt should fail.
	_, err := m.Update(ctx, "k", []byte("v2"), rev)
	if !errors.Is(err, natsclient.ErrKeyExists) {
		t.Fatalf("first Update with hook: got %v, want ErrKeyExists", err)
	}

	// Second attempt should succeed (same stale revision — hook now returns it correctly).
	_, err = m.Update(ctx, "k", []byte("v2"), rev)
	if err != nil {
		t.Fatalf("second Update with hook: %v", err)
	}
}

func TestMockKVBucket_Delete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := kvbuckettest.NewMockKVBucket()

	_, _ = m.Put(ctx, "k", []byte("v"))

	if err := m.Delete(ctx, "k"); err != nil {
		t.Fatalf("Delete existing key: %v", err)
	}

	// Second delete — missing key.
	if err := m.Delete(ctx, "k"); !errors.Is(err, natsclient.ErrKeyNotFound) {
		t.Fatalf("Delete missing key: got %v, want ErrKeyNotFound", err)
	}
}

func TestMockKVBucket_Keys_EmptyBucket(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := kvbuckettest.NewMockKVBucket()

	_, err := m.Keys(ctx)
	if !errors.Is(err, natsclient.ErrNoKeysFound) {
		t.Fatalf("Keys on empty bucket: got %v, want ErrNoKeysFound", err)
	}
}

func TestMockKVBucket_Keys_ReturnsAll(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := kvbuckettest.NewMockKVBucket()

	_, _ = m.Put(ctx, "a", []byte("1"))
	_, _ = m.Put(ctx, "b", []byte("2"))
	_, _ = m.Put(ctx, "c", []byte("3"))

	keys, err := m.Keys(ctx)
	if err != nil {
		t.Fatalf("Keys: %v", err)
	}
	if len(keys) != 3 {
		t.Fatalf("Keys count = %d, want 3", len(keys))
	}
}

func TestMockKVBucket_Watch_KVTwoferBootstrapDelimiter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := kvbuckettest.NewMockKVBucket()

	_, _ = m.Put(ctx, "x", []byte("hello"))
	_, _ = m.Put(ctx, "y", []byte("world"))

	w, err := m.Watch(ctx, ">")
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer w.Stop()

	ch := w.Updates()

	// Drain the bootstrap snapshot + delimiter.
	seenKeys := map[string]bool{}
	delimiterSeen := false
	for i := 0; i < 3; i++ { // 2 initial entries + 1 delimiter
		entry := <-ch
		if entry.Value == nil {
			delimiterSeen = true
			break
		}
		seenKeys[entry.Key] = true
	}

	if !delimiterSeen {
		t.Fatal("end-of-bootstrap delimiter (Value==nil) not received after snapshot")
	}
	if !seenKeys["x"] || !seenKeys["y"] {
		t.Errorf("snapshot missing keys; got %v", seenKeys)
	}
}

func TestMockKVBucket_Watch_LiveUpdates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := kvbuckettest.NewMockKVBucket()

	w, err := m.Watch(ctx, ">")
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer w.Stop()

	ch := w.Updates()

	// Drain bootstrap delimiter (empty bucket → immediate delimiter).
	delim := <-ch
	if delim.Value != nil {
		t.Fatalf("expected delimiter, got entry with key=%q", delim.Key)
	}

	// Publish a live update.
	_, _ = m.Put(ctx, "live", []byte("update"))

	entry := <-ch
	if entry.Key != "live" {
		t.Errorf("live update key = %q, want %q", entry.Key, "live")
	}
	if string(entry.Value) != "update" {
		t.Errorf("live update value = %q, want %q", entry.Value, "update")
	}
}

func TestMockKVBucket_Watch_Stop(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := kvbuckettest.NewMockKVBucket()

	w, _ := m.Watch(ctx, ">")

	// Drain delimiter.
	<-w.Updates()

	if err := w.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Channel should be closed after Stop.
	_, open := <-w.Updates()
	if open {
		t.Fatal("channel still open after Stop")
	}
}

func TestMockKVBucket_ConcurrentPut_NoDataRace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := kvbuckettest.NewMockKVBucket()

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			key := "k"
			_, _ = m.Put(ctx, key, []byte{byte(i)})
		}()
	}
	wg.Wait()

	_, err := m.Get(ctx, "k")
	if err != nil {
		t.Fatalf("Get after concurrent Puts: %v", err)
	}
}

func TestMockKVBucket_Bucket_Name(t *testing.T) {
	t.Parallel()
	m := kvbuckettest.NewMockKVBucketNamed("test-schedules")
	if m.Bucket() != "test-schedules" {
		t.Errorf("Bucket() = %q, want %q", m.Bucket(), "test-schedules")
	}
}

// TestMockKVBucket_ImplementsKVBucket is a compile-time guard: MockKVBucket
// must satisfy the natsclient.KVBucket interface. If it doesn't, the test
// file will fail to compile before any tests run.
func TestMockKVBucket_ImplementsKVBucket(t *testing.T) {
	t.Parallel()
	var _ natsclient.KVBucket = kvbuckettest.NewMockKVBucket()
}
