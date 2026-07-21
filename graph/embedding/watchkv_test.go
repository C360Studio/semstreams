package embedding

import (
	"context"
	"io"
	"log/slog"
	"sync"

	"github.com/nats-io/nats.go/jetstream"
)

// discardLogger silences worker output for tests that assert on state rather than
// logs. It is passed explicitly, never via slog.SetDefault, so callers stay safe
// under t.Parallel().
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// watchableKV extends memKV with the WatchAll surface Worker.Start requires, so
// worker tests drive the real production wire — Start -> WatchAll -> N goroutines
// -> handleKVEntry — instead of calling the loop's internals directly.
//
// It is a separate type rather than methods on memKV so the existing Storage-only
// tests keep memKV's "stray off the covered surface and fail loudly" property.
type watchableKV struct {
	*memKV

	mu       sync.Mutex
	watchers []*memWatcher
}

func newWatchableKV() *watchableKV {
	return &watchableKV{memKV: newMemKV()}
}

// memWatcher delivers entries to a single Updates channel. Worker spawns N
// goroutines that all range over the one watcher, exactly as they do against a
// real jetstream.KeyWatcher, so competing-consumer behaviour is preserved.
type memWatcher struct {
	updates chan jetstream.KeyValueEntry
	once    sync.Once
}

func (w *memWatcher) Updates() <-chan jetstream.KeyValueEntry { return w.updates }

func (w *memWatcher) Stop() error {
	w.once.Do(func() { close(w.updates) })
	return nil
}

func (k *watchableKV) WatchAll(_ context.Context, _ ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	w := &memWatcher{updates: make(chan jetstream.KeyValueEntry, 256)}

	k.mu.Lock()
	k.watchers = append(k.watchers, w)
	k.mu.Unlock()

	// Empty initial snapshot, then the nil end-of-snapshot delimiter, matching the
	// real watcher's contract. Tests seed AFTER Start so delivery order is
	// deterministic rather than dependent on map iteration order.
	w.updates <- nil

	return w, nil
}

// fanout notifies watchers outside memKV's data lock. Holding it across a channel
// send would let a full buffer deadlock against a consumer that needs the lock.
func (k *watchableKV) fanout(entry jetstream.KeyValueEntry) {
	k.mu.Lock()
	watchers := make([]*memWatcher, len(k.watchers))
	copy(watchers, k.watchers)
	k.mu.Unlock()

	for _, w := range watchers {
		w.updates <- entry
	}
}

func (k *watchableKV) Put(ctx context.Context, key string, value []byte) (uint64, error) {
	rev, err := k.memKV.Put(ctx, key, value)
	if err != nil {
		return rev, err
	}
	k.fanout(&memKVEntry{
		key:   key,
		value: append([]byte(nil), value...),
		rev:   rev,
		op:    jetstream.KeyValuePut,
	})
	return rev, nil
}

func (k *watchableKV) Delete(ctx context.Context, key string, opts ...jetstream.KVDeleteOpt) error {
	if err := k.memKV.Delete(ctx, key, opts...); err != nil {
		return err
	}
	k.fanout(&memKVEntry{key: key, op: jetstream.KeyValueDelete})
	return nil
}

// stubEmbedder is an Embedder whose Generate behaviour each test supplies, so a
// test can make generation panic, or delete the entity's index key mid-flight to
// simulate the hop-1 tombstone landing during the round trip.
type stubEmbedder struct {
	model      string
	dimensions int
	generate   func(texts []string) ([][]float32, error)
}

func (e *stubEmbedder) Generate(_ context.Context, texts []string) ([][]float32, error) {
	return e.generate(texts)
}

func (e *stubEmbedder) GenerateQuery(ctx context.Context, texts []string) ([][]float32, error) {
	return e.Generate(ctx, texts)
}

func (e *stubEmbedder) Dimensions() int { return e.dimensions }
func (e *stubEmbedder) Model() string   { return e.model }
func (e *stubEmbedder) Close() error    { return nil }
