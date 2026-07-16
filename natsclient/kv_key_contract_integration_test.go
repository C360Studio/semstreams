//go:build integration

package natsclient

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	gonats "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	normativeNATSServerVersion = "2.12.4-alpine"
	normativeNATSServerDigest  = "sha256:31c6ed3b2da61645aaa3ad9217b5a52b34b6ebd555ecb71259cd7723c59ae1ea"
	pinnedNATSGoVersion        = "v1.48.0"
)

func TestKVKeyContractNormativeNATS(t *testing.T) {
	testClient := NewTestClient(t,
		WithKV(),
		WithNATSVersion(normativeNATSServerVersion+"@"+normativeNATSServerDigest),
		WithTestTimeout(15*time.Second),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	t.Logf("normative_server=%s@%s sdk=%s platform=%s/%s config=default",
		normativeNATSServerVersion, normativeNATSServerDigest, pinnedNATSGoVersion, runtime.GOOS, runtime.GOARCH)

	dialer := &recordingDialer{}
	recordedConn, err := gonats.Connect(
		testClient.URL,
		gonats.Timeout(15*time.Second),
		gonats.MaxReconnects(0),
		gonats.SetCustomDialer(dialer),
	)
	if err != nil {
		t.Fatalf("connect recorded SDK client: %v", err)
	}
	defer recordedConn.Close()
	currentJS, err := jetstream.New(recordedConn)
	if err != nil {
		t.Fatalf("current JetStream: %v", err)
	}
	current, err := currentJS.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: "KVCONTRACTCURRENT"})
	if err != nil {
		t.Fatalf("create current KV bucket: %v", err)
	}
	wrapperBucket, err := currentJS.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: "KVCONTRACTWRAPPER"})
	if err != nil {
		t.Fatalf("create wrapper KV bucket: %v", err)
	}
	wrapperClient, err := NewClient(testClient.URL)
	if err != nil {
		t.Fatalf("create wrapper client: %v", err)
	}
	wrapperClient.conn = recordedConn
	wrapperClient.js = currentJS
	wrapperClient.logger = slog.Default()
	wrapperClient.status.Store(StatusConnected)
	wrapper := wrapperClient.NewKVStore(wrapperBucket)

	legacyJS, err := recordedConn.JetStream()
	if err != nil {
		t.Fatalf("legacy JetStream: %v", err)
	}
	legacy, err := legacyJS.CreateKeyValue(&gonats.KeyValueConfig{Bucket: "KVCONTRACTLEGACY"})
	if err != nil {
		t.Fatalf("create legacy KV bucket: %v", err)
	}

	boundaryKey := strings.Repeat("a", MaxKVLiteralTokenBytes) + "." +
		strings.Repeat("b", MaxKVLiteralKeyBytes-MaxKVLiteralTokenBytes-1)
	maxTokenKey := strings.Repeat("a.", MaxKVLiteralKeyTokens-1) + "a"
	boundaryPrefix := strings.Repeat("p", MaxKVLiteralTokenBytes) + "." +
		strings.Repeat("q", MaxKVWildcardFilterBytes-MaxKVLiteralTokenBytes-3) + "."
	boundaryWildcardFilter := boundaryPrefix + ">"
	boundaryMatchedKey := boundaryPrefix + "z"
	maxTokenFilter := strings.Repeat("a.", MaxKVWildcardFilterTokens-1) + "*"
	if err := ValidateKVLiteralKey(boundaryKey); err != nil {
		t.Fatalf("validate boundary key: %v", err)
	}
	if err := ValidateKVLiteralKey(maxTokenKey); err != nil {
		t.Fatalf("validate maximum-token key: %v", err)
	}
	if err := ValidateKVWildcardFilter(boundaryWildcardFilter); err != nil {
		t.Fatalf("validate boundary wildcard filter: %v", err)
	}
	if err := ValidateKVLiteralKey(boundaryMatchedKey); err != nil {
		t.Fatalf("validate boundary matched key: %v", err)
	}
	if err := ValidateKVWildcardFilter(maxTokenFilter); err != nil {
		t.Fatalf("validate maximum-token filter: %v", err)
	}
	dialer.recorder.reset()

	t.Run("current SDK boundary CRUD", func(t *testing.T) {
		revision, putErr := current.Put(ctx, boundaryKey, []byte("one"))
		if putErr != nil {
			t.Fatalf("Put: %v", putErr)
		}
		entry, getErr := current.Get(ctx, boundaryKey)
		if getErr != nil || string(entry.Value()) != "one" {
			t.Fatalf("Get: value=%q err=%v", valueOfCurrent(entry), getErr)
		}
		revision, updateErr := current.Update(ctx, boundaryKey, []byte("two"), revision)
		if updateErr != nil {
			t.Fatalf("Update: %v", updateErr)
		}
		if deleteErr := current.Delete(ctx, boundaryKey, jetstream.LastRevision(revision)); deleteErr != nil {
			t.Fatalf("Delete: %v", deleteErr)
		}
		if _, createErr := current.Create(ctx, boundaryKey, []byte("three")); createErr != nil {
			t.Fatalf("Create after delete: %v", createErr)
		}
		if _, putErr := current.Put(ctx, maxTokenKey, []byte("tokens")); putErr != nil {
			t.Fatalf("64-token Put: %v", putErr)
		}
		if _, putErr := current.Put(ctx, boundaryMatchedKey, []byte("filter")); putErr != nil {
			t.Fatalf("boundary-filter fixture Put: %v", putErr)
		}
		assertCurrentExactFilter(t, ctx, current, maxTokenKey)
	})

	t.Run("legacy SDK boundary CRUD", func(t *testing.T) {
		revision, putErr := legacy.Put(boundaryKey, []byte("one"))
		if putErr != nil {
			t.Fatalf("Put: %v", putErr)
		}
		entry, getErr := legacy.Get(boundaryKey)
		if getErr != nil || string(entry.Value()) != "one" {
			t.Fatalf("Get: value=%q err=%v", valueOfLegacy(entry), getErr)
		}
		revision, updateErr := legacy.Update(boundaryKey, []byte("two"), revision)
		if updateErr != nil {
			t.Fatalf("Update: %v", updateErr)
		}
		if deleteErr := legacy.Delete(boundaryKey, gonats.LastRevision(revision)); deleteErr != nil {
			t.Fatalf("Delete: %v", deleteErr)
		}
		if _, createErr := legacy.Create(boundaryKey, []byte("three")); createErr != nil {
			t.Fatalf("Create after delete: %v", createErr)
		}
		if _, putErr := legacy.Put(maxTokenKey, []byte("tokens")); putErr != nil {
			t.Fatalf("64-token Put: %v", putErr)
		}
		if _, putErr := legacy.Put(boundaryMatchedKey, []byte("filter")); putErr != nil {
			t.Fatalf("boundary-filter fixture Put: %v", putErr)
		}
		watcher, watchErr := legacy.Watch(maxTokenKey)
		if watchErr != nil {
			t.Fatalf("64-token Watch: %v", watchErr)
		}
		_ = watcher.Stop()
	})

	t.Run("existing wrapper paths remain usable", func(t *testing.T) {
		if _, putErr := wrapper.Put(ctx, boundaryKey, []byte("one")); putErr != nil {
			t.Fatalf("Put: %v", putErr)
		}
		if _, getErr := wrapper.Get(ctx, boundaryKey); getErr != nil {
			t.Fatalf("Get: %v", getErr)
		}
		if updateErr := wrapper.UpdateWithRetry(ctx, "direct.create", func(current []byte) ([]byte, error) {
			if current != nil {
				return nil, fmt.Errorf("unexpected current value")
			}
			return []byte("created"), nil
		}); updateErr != nil {
			t.Fatalf("UpdateWithRetry direct Create: %v", updateErr)
		}
		if deleteErr := wrapper.Delete(ctx, boundaryKey); deleteErr != nil {
			t.Fatalf("Delete: %v", deleteErr)
		}
		if _, putErr := wrapper.Put(ctx, boundaryMatchedKey, []byte("filter")); putErr != nil {
			t.Fatalf("boundary-filter fixture Put: %v", putErr)
		}
		if _, putErr := wrapper.Put(ctx, maxTokenKey, []byte("tokens")); putErr != nil {
			t.Fatalf("64-token fixture Put: %v", putErr)
		}
	})

	writeMatchSet(t, ctx, current, wrapper, legacy)
	assertCurrentFilterMatchSets(t, ctx, current)
	assertWrapperFilterMatchSets(t, ctx, wrapper)
	assertCurrentWatchMatch(t, ctx, current)
	assertLegacyWatchMatch(t, legacy)
	assertWrapperWatchMatch(t, ctx, wrapper, wrapperBucket)
	assertExistingPermissivePathsUnchanged(t, ctx, current, wrapper)
	logCapturedControlLines(t, "accepted_boundary_matrix", dialer.recorder)
	assertMalformedControlsHaveNoServerSideEffect(t, ctx, current, recordedConn, dialer.recorder)
	assertNormativeWireEvidence(
		t,
		ctx,
		recordedConn,
		dialer.recorder,
		current,
		legacy,
		wrapper,
		wrapperBucket,
		boundaryKey,
		boundaryPrefix,
		boundaryWildcardFilter,
		maxTokenKey,
		maxTokenFilter,
	)
}

func writeMatchSet(
	t *testing.T,
	ctx context.Context,
	current jetstream.KeyValue,
	wrapper *KVStore,
	legacy gonats.KeyValue,
) {
	t.Helper()
	keys := []string{
		"domain.category",
		"domain.category.property",
		"domain.other.property",
		"domain.category.child",
		"domain.category.property.extra",
		"neighbor.category.property",
	}
	for _, key := range keys {
		if err := ValidateKVLiteralKey(key); err != nil {
			t.Fatalf("validate fixture %q: %v", key, err)
		}
		if _, err := current.Put(ctx, key, []byte(key)); err != nil {
			t.Fatalf("current Put(%q): %v", key, err)
		}
		if _, err := wrapper.Put(ctx, key, []byte(key)); err != nil {
			t.Fatalf("wrapper Put(%q): %v", key, err)
		}
		if _, err := legacy.Put(key, []byte(key)); err != nil {
			t.Fatalf("legacy Put(%q): %v", key, err)
		}
	}
}

func assertCurrentFilterMatchSets(t *testing.T, ctx context.Context, bucket jetstream.KeyValue) {
	t.Helper()
	tests := []struct {
		filter string
		want   []string
	}{
		{filter: "domain.category.property", want: []string{"domain.category.property"}},
		{filter: "domain.*.property", want: []string{"domain.category.property", "domain.other.property"}},
		{filter: "domain.category.>", want: []string{
			"domain.category.child", "domain.category.property", "domain.category.property.extra",
		}},
	}
	for _, tt := range tests {
		if err := ValidateKVWildcardFilter(tt.filter); err != nil {
			t.Fatalf("validate filter %q: %v", tt.filter, err)
		}
		lister, err := bucket.ListKeysFiltered(ctx, tt.filter)
		if err != nil {
			t.Fatalf("ListKeysFiltered(%q): %v", tt.filter, err)
		}
		got := collectCurrentKeys(lister)
		assertSameKeys(t, tt.filter, got, tt.want)
	}
	allLister, err := bucket.ListKeys(ctx)
	if err != nil {
		t.Fatalf("ListKeys: %v", err)
	}
	if got := collectCurrentKeys(allLister); len(got) < 6 {
		t.Fatalf("ListKeys returned %d keys, want at least 6", len(got))
	}
}

func assertExistingPermissivePathsUnchanged(
	t *testing.T,
	ctx context.Context,
	raw jetstream.KeyValue,
	wrapper *KVStore,
) {
	t.Helper()
	fixtures := []struct {
		name string
		key  string
	}{
		{name: "513-byte token", key: strings.Repeat("c", MaxKVLiteralTokenBytes+1)},
		{name: "65-token key", key: strings.Repeat("d.", MaxKVLiteralKeyTokens) + "d"},
	}
	for _, fixture := range fixtures {
		if err := ValidateKVLiteralKey(fixture.key); err == nil {
			t.Fatalf("%s unexpectedly accepted by shared contract", fixture.name)
		}
		if err := ValidateKVWildcardFilter(fixture.key); err == nil {
			t.Fatalf("%s unexpectedly accepted as shared filter", fixture.name)
		}

		value := []byte("compatibility:" + fixture.name)
		if _, err := raw.Put(ctx, fixture.key, value); err != nil {
			t.Fatalf("raw Put %s: %v", fixture.name, err)
		}
		rawEntry, err := raw.Get(ctx, fixture.key)
		if err != nil {
			t.Fatalf("raw Get %s: %v", fixture.name, err)
		}
		if rawEntry.Key() != fixture.key || !bytes.Equal(rawEntry.Value(), value) {
			t.Fatalf("raw round trip %s changed key/value", fixture.name)
		}
		rawLister, err := raw.ListKeysFiltered(ctx, fixture.key)
		if err != nil {
			t.Fatalf("raw exact filter %s: %v", fixture.name, err)
		}
		assertSameKeys(t, "raw "+fixture.name, collectCurrentKeys(rawLister), []string{fixture.key})
		rawWatcher, err := raw.Watch(ctx, fixture.key)
		if err != nil {
			t.Fatalf("raw Watch %s: %v", fixture.name, err)
		}
		_ = rawWatcher.Stop()

		if _, err := wrapper.Put(ctx, fixture.key, value); err != nil {
			t.Fatalf("wrapper Put %s: %v", fixture.name, err)
		}
		wrapperEntry, err := wrapper.Get(ctx, fixture.key)
		if err != nil {
			t.Fatalf("wrapper Get %s: %v", fixture.name, err)
		}
		if wrapperEntry.Key != fixture.key || !bytes.Equal(wrapperEntry.Value, value) {
			t.Fatalf("wrapper round trip %s changed key/value", fixture.name)
		}
		keys, err := wrapper.KeysByFilter(ctx, fixture.key)
		if err != nil {
			t.Fatalf("wrapper exact filter %s: %v", fixture.name, err)
		}
		assertSameKeys(t, "wrapper "+fixture.name, keys, []string{fixture.key})
		wrapperWatcher, err := wrapper.Watch(ctx, fixture.key)
		if err != nil {
			t.Fatalf("wrapper Watch %s: %v", fixture.name, err)
		}
		_ = wrapperWatcher.Stop()
	}
}

func assertCurrentExactFilter(t *testing.T, ctx context.Context, bucket jetstream.KeyValue, filter string) {
	t.Helper()
	if err := ValidateKVWildcardFilter(filter); err != nil {
		t.Fatalf("validate exact boundary filter: %v", err)
	}
	lister, err := bucket.ListKeysFiltered(ctx, filter)
	if err != nil {
		t.Fatalf("ListKeysFiltered boundary: %v", err)
	}
	assertSameKeys(t, "boundary exact filter", collectCurrentKeys(lister), []string{filter})
	watcher, err := bucket.Watch(ctx, filter)
	if err != nil {
		t.Fatalf("Watch boundary: %v", err)
	}
	_ = watcher.Stop()
}

func assertWrapperFilterMatchSets(t *testing.T, ctx context.Context, wrapper *KVStore) {
	t.Helper()
	want := []string{"domain.category.child", "domain.category.property", "domain.category.property.extra"}
	got, err := wrapper.KeysByPrefix(ctx, "domain.category.")
	if err != nil {
		t.Fatalf("KeysByPrefix: %v", err)
	}
	assertSameKeys(t, "KeysByPrefix", got, want)
	got, err = wrapper.KeysByFilter(ctx, "domain.*.property")
	if err != nil {
		t.Fatalf("KeysByFilter: %v", err)
	}
	assertSameKeys(t, "KeysByFilter", got, []string{"domain.category.property", "domain.other.property"})
	got, err = FilteredKeys(ctx, wrapper.bucket, "domain.category.>")
	if err != nil {
		t.Fatalf("FilteredKeys: %v", err)
	}
	assertSameKeys(t, "FilteredKeys", got, want)
	if _, err := wrapper.Keys(ctx); err != nil {
		t.Fatalf("Keys: %v", err)
	}
}

func assertCurrentWatchMatch(t *testing.T, ctx context.Context, bucket jetstream.KeyValue) {
	t.Helper()
	filter := "watch.*.property"
	if err := ValidateKVWildcardFilter(filter); err != nil {
		t.Fatalf("validate watch filter: %v", err)
	}
	watcher, err := bucket.Watch(ctx, filter, jetstream.UpdatesOnly())
	if err != nil {
		t.Fatalf("current Watch: %v", err)
	}
	defer watcher.Stop()
	if _, err := bucket.Put(ctx, "watch.current.property", []byte("value")); err != nil {
		t.Fatalf("current watched Put: %v", err)
	}
	select {
	case entry := <-watcher.Updates():
		if entry == nil || entry.Key() != "watch.current.property" {
			t.Fatalf("current Watch entry = %#v", entry)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("current Watch timed out")
	}
}

func assertLegacyWatchMatch(t *testing.T, bucket gonats.KeyValue) {
	t.Helper()
	filter := "watch.*.property"
	if err := ValidateKVWildcardFilter(filter); err != nil {
		t.Fatalf("validate legacy watch filter: %v", err)
	}
	watcher, err := bucket.Watch(filter, gonats.UpdatesOnly())
	if err != nil {
		t.Fatalf("legacy Watch: %v", err)
	}
	defer watcher.Stop()
	if _, err := bucket.Put("watch.legacy.property", []byte("value")); err != nil {
		t.Fatalf("legacy watched Put: %v", err)
	}
	select {
	case entry := <-watcher.Updates():
		if entry == nil || entry.Key() != "watch.legacy.property" {
			t.Fatalf("legacy Watch entry = %#v", entry)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("legacy Watch timed out")
	}
}

func assertWrapperWatchMatch(
	t *testing.T,
	ctx context.Context,
	wrapper *KVStore,
	bucket jetstream.KeyValue,
) {
	t.Helper()
	watcher, err := wrapper.Watch(ctx, "watch.wrapper.*")
	if err != nil {
		t.Fatalf("wrapper Watch: %v", err)
	}
	defer watcher.Stop()
	if _, err := bucket.Put(ctx, "watch.wrapper.property", []byte("value")); err != nil {
		t.Fatalf("wrapper watched Put: %v", err)
	}
	deadline := time.After(5 * time.Second)
	for {
		select {
		case entry := <-watcher.Updates():
			if entry == nil {
				continue
			}
			if entry.Key() != "watch.wrapper.property" {
				t.Fatalf("wrapper Watch entry = %#v", entry)
			}
			return
		case <-deadline:
			t.Fatal("wrapper Watch timed out")
		}
	}
}

func assertMalformedControlsHaveNoServerSideEffect(
	t *testing.T,
	ctx context.Context,
	bucket jetstream.KeyValue,
	conn *gonats.Conn,
	recorder *recordingConn,
) {
	t.Helper()
	statusBefore, err := bucket.Status(ctx)
	if err != nil {
		t.Fatalf("status before malformed controls: %v", err)
	}
	recorder.reset()
	invalidKeys := []string{
		"foo..bar",
		"foo*bar",
		"foo.>.bar",
		strings.Repeat("a", MaxKVLiteralKeyBytes+1),
		strings.Repeat("a.", MaxKVLiteralKeyTokens) + "a",
	}
	for _, key := range invalidKeys {
		if err := ValidateKVLiteralKey(key); err == nil {
			t.Fatalf("malformed key accepted: length=%d", len(key))
		}
	}
	invalidFilters := []string{
		"foo..bar",
		"foo*bar",
		"foo.>.bar",
		strings.Repeat("a", MaxKVWildcardFilterBytes+1),
		strings.Repeat("a.", MaxKVWildcardFilterTokens) + "*",
	}
	for _, filter := range invalidFilters {
		if err := ValidateKVWildcardFilter(filter); err == nil {
			t.Fatalf("malformed filter accepted: length=%d", len(filter))
		}
	}
	if err := conn.FlushTimeout(5 * time.Second); err != nil {
		t.Fatalf("flush malformed controls: %v", err)
	}
	if lines := recorder.operationLines(); len(lines) != 0 {
		t.Fatalf("malformed prevalidation emitted NATS operations: %v", lines)
	}
	statusAfter, err := bucket.Status(ctx)
	if err != nil {
		t.Fatalf("status after malformed controls: %v", err)
	}
	if statusAfter.Values() != statusBefore.Values() {
		t.Fatalf("malformed helper controls changed server values: before=%d after=%d",
			statusBefore.Values(), statusAfter.Values())
	}
}

func assertNormativeWireEvidence(
	t *testing.T,
	ctx context.Context,
	conn *gonats.Conn,
	recorder *recordingConn,
	current jetstream.KeyValue,
	legacy gonats.KeyValue,
	wrapper *KVStore,
	wrapperBucket jetstream.KeyValue,
	boundaryKey string,
	boundaryPrefix string,
	boundaryWildcardFilter string,
	maxTokenKey string,
	maxTokenFilter string,
) {
	t.Helper()

	var currentRevision uint64
	observeWire(t, conn, recorder, "current.put", func() error {
		var err error
		currentRevision, err = current.Put(ctx, boundaryKey, []byte("one"))
		return err
	})
	observeWire(t, conn, recorder, "current.get_direct", func() error {
		_, err := current.Get(ctx, boundaryKey)
		return err
	})
	observeWire(t, conn, recorder, "current.create", func() error {
		_, err := current.Create(ctx, boundaryPrefix+"y", []byte("one"))
		return err
	})
	observeWire(t, conn, recorder, "current.update", func() error {
		_, err := current.Update(ctx, boundaryKey, []byte("two"), currentRevision)
		return err
	})
	observeWire(t, conn, recorder, "current.delete", func() error {
		return current.Delete(ctx, boundaryKey)
	})
	observeWire(t, conn, recorder, "current.list", func() error {
		lister, err := current.ListKeys(ctx)
		if err != nil {
			return err
		}
		_ = collectCurrentKeys(lister)
		return nil
	})
	observeWire(t, conn, recorder, "current.filter_list", func() error {
		lister, err := current.ListKeysFiltered(ctx, boundaryWildcardFilter)
		if err != nil {
			return err
		}
		_ = collectCurrentKeys(lister)
		return nil
	})
	observeWire(t, conn, recorder, "current.watch", func() error {
		watcher, err := current.Watch(ctx, boundaryWildcardFilter)
		if err != nil {
			return err
		}
		return watcher.Stop()
	})
	observeWire(t, conn, recorder, "current.filter_list_64_tokens", func() error {
		lister, err := current.ListKeysFiltered(ctx, maxTokenFilter)
		if err != nil {
			return err
		}
		_ = collectCurrentKeys(lister)
		return nil
	})
	observeWire(t, conn, recorder, "current.watch_64_tokens", func() error {
		watcher, err := current.Watch(ctx, maxTokenFilter)
		if err != nil {
			return err
		}
		return watcher.Stop()
	})

	var legacyRevision uint64
	observeWire(t, conn, recorder, "legacy.put", func() error {
		var err error
		legacyRevision, err = legacy.Put(boundaryKey, []byte("one"))
		return err
	})
	observeWire(t, conn, recorder, "legacy.get_direct", func() error {
		_, err := legacy.Get(boundaryKey)
		return err
	})
	observeWire(t, conn, recorder, "legacy.create", func() error {
		_, err := legacy.Create(boundaryPrefix+"x", []byte("one"))
		return err
	})
	observeWire(t, conn, recorder, "legacy.update", func() error {
		_, err := legacy.Update(boundaryKey, []byte("two"), legacyRevision)
		return err
	})
	observeWire(t, conn, recorder, "legacy.delete", func() error {
		return legacy.Delete(boundaryKey)
	})
	observeWire(t, conn, recorder, "legacy.list", func() error {
		lister, err := legacy.ListKeys(gonats.Context(ctx))
		if err != nil {
			return err
		}
		defer lister.Stop()
		for range lister.Keys() {
		}
		return nil
	})
	observeWire(t, conn, recorder, "legacy.watch", func() error {
		watcher, err := legacy.Watch(boundaryWildcardFilter)
		if err != nil {
			return err
		}
		return watcher.Stop()
	})
	observeWire(t, conn, recorder, "legacy.watch_64_tokens", func() error {
		watcher, err := legacy.Watch(maxTokenFilter)
		if err != nil {
			return err
		}
		return watcher.Stop()
	})

	var wrapperRevision uint64
	observeWire(t, conn, recorder, "wrapper.put", func() error {
		var err error
		wrapperRevision, err = wrapper.Put(ctx, boundaryPrefix+"z", []byte("one"))
		return err
	})
	observeWire(t, conn, recorder, "wrapper.get_direct", func() error {
		_, err := wrapper.Get(ctx, boundaryPrefix+"z")
		return err
	})
	observeWire(t, conn, recorder, "wrapper.create", func() error {
		_, err := wrapper.Create(ctx, boundaryPrefix+"v", []byte("one"))
		return err
	})
	observeWire(t, conn, recorder, "wrapper.update", func() error {
		_, err := wrapper.Update(ctx, boundaryPrefix+"z", []byte("two"), wrapperRevision)
		return err
	})
	observeWire(t, conn, recorder, "wrapper.direct_create", func() error {
		return wrapper.UpdateWithRetry(ctx, boundaryPrefix+"w", func([]byte) ([]byte, error) {
			return []byte("one"), nil
		})
	})
	observeWire(t, conn, recorder, "wrapper.delete", func() error {
		return wrapper.Delete(ctx, boundaryPrefix+"z")
	})
	observeWire(t, conn, recorder, "wrapper.list", func() error {
		_, err := wrapper.Keys(ctx)
		return err
	})
	observeWire(t, conn, recorder, "wrapper.prefix_list", func() error {
		_, err := wrapper.KeysByPrefix(ctx, boundaryPrefix)
		return err
	})
	observeWire(t, conn, recorder, "wrapper.filter_list", func() error {
		_, err := wrapper.KeysByFilter(ctx, boundaryWildcardFilter)
		return err
	})
	observeWire(t, conn, recorder, "raw.filtered_list", func() error {
		_, err := FilteredKeys(ctx, wrapperBucket, boundaryWildcardFilter)
		return err
	})
	observeWire(t, conn, recorder, "wrapper.watch", func() error {
		watcher, err := wrapper.Watch(ctx, boundaryWildcardFilter)
		if err != nil {
			return err
		}
		return watcher.Stop()
	})
	observeWire(t, conn, recorder, "wrapper.filter_list_64_tokens", func() error {
		_, err := wrapper.KeysByFilter(ctx, maxTokenFilter)
		return err
	})
	observeWire(t, conn, recorder, "wrapper.watch_64_tokens", func() error {
		watcher, err := wrapper.Watch(ctx, maxTokenFilter)
		if err != nil {
			return err
		}
		return watcher.Stop()
	})
	if _, err := current.Get(ctx, maxTokenKey); err != nil {
		t.Fatalf("current maximum-token Get: %v", err)
	}
	if _, err := legacy.Get(maxTokenKey); err != nil {
		t.Fatalf("legacy maximum-token Get: %v", err)
	}
	if _, err := wrapper.Get(ctx, maxTokenKey); err != nil {
		t.Fatalf("wrapper maximum-token Get: %v", err)
	}
}

func observeWire(
	t *testing.T,
	conn *gonats.Conn,
	recorder *recordingConn,
	path string,
	operation func() error,
) {
	t.Helper()
	recorder.reset()
	if err := operation(); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if err := conn.FlushTimeout(5 * time.Second); err != nil {
		t.Fatalf("%s flush: %v", path, err)
	}
	lines := recorder.operationLines()
	if len(lines) == 0 {
		t.Fatalf("%s emitted no captured NATS operation", path)
	}
	t.Logf("wire_path=%s actual=%v", path, summarizeControlLines(lines))
}

func logCapturedControlLines(t *testing.T, path string, recorder *recordingConn) {
	t.Helper()
	lines := recorder.operationLines()
	if len(lines) == 0 {
		t.Fatalf("%s emitted no captured NATS operations", path)
	}
	t.Logf("wire_path=%s actual=%v", path, summarizeControlLines(lines))
}

func summarizeControlLines(lines []string) []string {
	summaries := make([]string, 0, len(lines))
	for _, line := range lines {
		fields := strings.Fields(line)
		subjectBytes := 0
		if len(fields) > 1 {
			subjectBytes = len(fields[1])
		}
		summaries = append(summaries,
			fmt.Sprintf("%s(subject=%d,control=%d)", fields[0], subjectBytes, len(line)+2))
	}
	return summaries
}

type recordingDialer struct {
	recorder *recordingConn
}

func (dialer *recordingDialer) Dial(network, address string) (net.Conn, error) {
	conn, err := (&net.Dialer{}).Dial(network, address)
	if err != nil {
		return nil, err
	}
	dialer.recorder = &recordingConn{Conn: conn}
	return dialer.recorder, nil
}

type recordingConn struct {
	net.Conn
	mu     sync.Mutex
	writes bytes.Buffer
}

func (conn *recordingConn) Write(data []byte) (int, error) {
	conn.mu.Lock()
	_, _ = conn.writes.Write(data)
	conn.mu.Unlock()
	return conn.Conn.Write(data)
}

func (conn *recordingConn) reset() {
	conn.mu.Lock()
	conn.writes.Reset()
	conn.mu.Unlock()
}

func (conn *recordingConn) operationLines() []string {
	conn.mu.Lock()
	raw := append([]byte(nil), conn.writes.Bytes()...)
	conn.mu.Unlock()
	parts := bytes.Split(raw, []byte("\r\n"))
	lines := make([]string, 0, len(parts))
	for _, part := range parts {
		line := string(part)
		if strings.HasPrefix(line, "PUB ") ||
			strings.HasPrefix(line, "HPUB ") ||
			strings.HasPrefix(line, "SUB ") ||
			strings.HasPrefix(line, "UNSUB ") {
			lines = append(lines, line)
		}
	}
	return lines
}

func collectCurrentKeys(lister jetstream.KeyLister) []string {
	defer lister.Stop()
	var keys []string
	for key := range lister.Keys() {
		keys = append(keys, key)
	}
	return keys
}

func assertSameKeys(t *testing.T, label string, got, want []string) {
	t.Helper()
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("%s keys = %v, want %v", label, got, want)
	}
}

func valueOfCurrent(entry jetstream.KeyValueEntry) []byte {
	if entry == nil {
		return nil
	}
	return entry.Value()
}

func valueOfLegacy(entry gonats.KeyValueEntry) []byte {
	if entry == nil {
		return nil
	}
	return entry.Value()
}
