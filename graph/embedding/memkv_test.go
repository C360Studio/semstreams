package embedding

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// memKV is an in-memory jetstream.KeyValue sufficient for exercising Storage's
// Put/Get/Delete paths without a NATS server. Only the operations Storage
// actually uses are implemented; everything else returns errNotImplemented so a
// test that strays off the covered surface fails loudly instead of silently
// succeeding on a zero value.
var errNotImplemented = errors.New("memKV: not implemented")

type memKV struct {
	mu   sync.Mutex
	data map[string][]byte
	rev  uint64
}

func newMemKV() *memKV {
	return &memKV{data: make(map[string][]byte)}
}

type memKVEntry struct {
	key   string
	value []byte
	rev   uint64
	op    jetstream.KeyValueOp
}

func (e *memKVEntry) Key() string                     { return e.key }
func (e *memKVEntry) Value() []byte                   { return e.value }
func (e *memKVEntry) Revision() uint64                { return e.rev }
func (e *memKVEntry) Created() time.Time              { return time.Time{} }
func (e *memKVEntry) Delta() uint64                   { return 0 }
func (e *memKVEntry) Operation() jetstream.KeyValueOp { return e.op }
func (e *memKVEntry) Bucket() string                  { return "MEM" }

func (m *memKV) Put(_ context.Context, key string, value []byte) (uint64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rev++
	stored := make([]byte, len(value))
	copy(stored, value)
	m.data[key] = stored
	return m.rev, nil
}

func (m *memKV) Get(_ context.Context, key string) (jetstream.KeyValueEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.data[key]
	if !ok {
		return nil, jetstream.ErrKeyNotFound
	}
	return &memKVEntry{key: key, value: v, rev: m.rev, op: jetstream.KeyValuePut}, nil
}

func (m *memKV) Delete(_ context.Context, key string, _ ...jetstream.KVDeleteOpt) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	m.rev++
	return nil
}

// has reports whether a key is currently present. Test-only observation point.
func (m *memKV) has(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.data[key]
	return ok
}

func (m *memKV) Bucket() string { return "MEM" }

func (m *memKV) PutString(ctx context.Context, key string, value string) (uint64, error) {
	return m.Put(ctx, key, []byte(value))
}

func (m *memKV) Purge(ctx context.Context, key string, opts ...jetstream.KVDeleteOpt) error {
	return m.Delete(ctx, key, opts...)
}

func (m *memKV) GetRevision(_ context.Context, _ string, _ uint64) (jetstream.KeyValueEntry, error) {
	return nil, errNotImplemented
}

func (m *memKV) Create(_ context.Context, _ string, _ []byte, _ ...jetstream.KVCreateOpt) (uint64, error) {
	return 0, errNotImplemented
}

func (m *memKV) Update(_ context.Context, _ string, _ []byte, _ uint64) (uint64, error) {
	return 0, errNotImplemented
}

func (m *memKV) Watch(_ context.Context, _ string, _ ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	return nil, errNotImplemented
}

func (m *memKV) WatchAll(_ context.Context, _ ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	return nil, errNotImplemented
}

func (m *memKV) WatchFiltered(_ context.Context, _ []string, _ ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	return nil, errNotImplemented
}

func (m *memKV) Keys(_ context.Context, _ ...jetstream.WatchOpt) ([]string, error) {
	return nil, errNotImplemented
}

func (m *memKV) ListKeys(_ context.Context, _ ...jetstream.WatchOpt) (jetstream.KeyLister, error) {
	return nil, errNotImplemented
}

func (m *memKV) ListKeysFiltered(_ context.Context, _ ...string) (jetstream.KeyLister, error) {
	return nil, errNotImplemented
}

func (m *memKV) History(_ context.Context, _ string, _ ...jetstream.WatchOpt) ([]jetstream.KeyValueEntry, error) {
	return nil, errNotImplemented
}

func (m *memKV) PurgeDeletes(_ context.Context, _ ...jetstream.KVPurgeOpt) error {
	return errNotImplemented
}

func (m *memKV) Status(_ context.Context) (jetstream.KeyValueStatus, error) {
	return nil, errNotImplemented
}
