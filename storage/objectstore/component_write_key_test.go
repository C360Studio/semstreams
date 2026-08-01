package objectstore

// #741: the raw write lane keyed EVERY message as
// message/YYYY/MM/DD/HH/unknown_<unixSeconds>, for two stacked reasons:
//
//  1. processWriteMessage passed the ORIGINAL data []byte to the store even
//     when registry decode SUCCEEDED (deliberately — re-marshaling a decoded
//     envelope base64-encodes []byte fields and corrupts the JSON), so
//     DefaultKeyGenerator's type/ID extraction was structurally unreachable
//     on the whole fallback lane.
//  2. DefaultKeyGenerator's key suffix was a clock reading (seconds; then
//     UnixNano in the first fix round, which still collided on hosts with
//     microsecond clock quantization), so the shared unknown-identifier keys
//     collided across distinct messages landing on one clock step —
//     ObjectStore Put replaces, and the first message was silently lost.
//
// These tests drive the PRODUCTION processWriteMessage seam with a
// key-recording fake backend underneath the real Store type, pinning both
// halves of the fix: keys derive from the decoded envelope when decode
// succeeded (while the stored payload remains the original wire bytes), and
// true undecodable bytes get unique keys from a per-write UUID nonce — never
// from the clock.

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadregistry"
)

type recordedPut struct {
	key  string
	data []byte
}

// keyRecordingObjectStore records every PutBytes underneath the REAL Store
// type so processWriteMessage exercises the production key-generation path.
// Any method the write path should not touch panics via the embedded nil
// interface (loud, not silent).
type keyRecordingObjectStore struct {
	jetstream.ObjectStore
	mu   sync.Mutex
	puts []recordedPut
}

func (f *keyRecordingObjectStore) PutBytes(
	_ context.Context, key string, data []byte,
) (*jetstream.ObjectInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]byte, len(data))
	copy(cp, data)
	f.puts = append(f.puts, recordedPut{key: key, data: cp})
	return &jetstream.ObjectInfo{}, nil
}

func (f *keyRecordingObjectStore) recorded() []recordedPut {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]recordedPut, len(f.puts))
	copy(out, f.puts)
	return out
}

// newWriteKeyTestComponent builds a Component over the recording backend. No
// ports are configured, so the "stored" emit is a by-design skip and every
// write's key + payload land in the fake.
func newWriteKeyTestComponent(dec *message.Decoder, backend *keyRecordingObjectStore) *Component {
	return &Component{
		instanceName: "unit",
		decoder:      dec,
		logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		store: &Store{
			bucketName:   "UNIT",
			instanceName: "unit",
			store:        backend,
			keyGenerator: &DefaultKeyGenerator{},
		},
	}
}

// TestProcessWriteMessage_DecodedEnvelopeKeysCarryTypeAndID is the
// discriminating test for #741's PRIMARY lane: a decodable message whose
// payload is NOT ContentStorable (the protocol-flow shape — JSONMap emits
// core.json.v1) must be keyed from the decoded envelope's type and message
// ID, while the stored payload remains the ORIGINAL wire bytes.
func TestProcessWriteMessage_DecodedEnvelopeKeysCarryTypeAndID(t *testing.T) {
	reg := payloadregistry.New()
	require.NoError(t, message.RegisterPayloads(reg))

	payload := message.NewGenericJSON(map[string]any{"sensor": "temp-001", "value": 23.5})
	baseMsg := message.NewBaseMessage(payload.Schema(), payload, "unit-test")
	wire, err := baseMsg.MarshalJSON()
	require.NoError(t, err)

	backend := &keyRecordingObjectStore{}
	c := newWriteKeyTestComponent(message.NewDecoder(reg), backend)

	require.NoError(t, c.processWriteMessage(context.Background(), wire))

	puts := backend.recorded()
	require.Len(t, puts, 1)
	assert.True(t, strings.HasPrefix(puts[0].key, "core.json.v1/"),
		"key must carry the decoded envelope's type, got %q", puts[0].key)
	assert.Contains(t, puts[0].key, baseMsg.ID()+"_",
		"key must carry the decoded envelope's message ID, got %q", puts[0].key)
	assert.NotContains(t, puts[0].key, "unknown",
		"a decodable message must never take the unknown key family (#741)")
	assert.Equal(t, wire, puts[0].data,
		"stored payload must remain the ORIGINAL wire bytes — re-marshaling the "+
			"decoded envelope base64-corrupts []byte JSON")
}

// TestProcessWriteMessage_UndecodableSameSecondWritesGetDistinctKeys pins the
// residual lane: true undecodable bytes legitimately take the unknown key
// family, so their uniqueness rests ENTIRELY on the per-write nonce. The
// clock seam freezes the key generator's wall-clock reading, so both writes
// observe the IDENTICAL time — deterministically reproducing what real hosts
// do anyway (adjacent time.Now() calls collided under -count=25 on
// microsecond-quantized clocks, per the #741 Codex review) — and a
// regression to ANY clock-derived suffix fails on every run, not
// probabilistically.
func TestProcessWriteMessage_UndecodableSameSecondWritesGetDistinctKeys(t *testing.T) {
	backend := &keyRecordingObjectStore{}
	// Empty registry: every decode fails, forcing the raw fallback lane.
	c := newWriteKeyTestComponent(message.NewDecoder(payloadregistry.New()), backend)
	frozen := time.Date(2026, 8, 1, 15, 4, 5, 0, time.UTC)
	c.store.keyGenerator = &DefaultKeyGenerator{now: func() time.Time { return frozen }}

	require.NoError(t, c.processWriteMessage(context.Background(), []byte(`not-json-a`)))
	require.NoError(t, c.processWriteMessage(context.Background(), []byte(`not-json-b`)))

	puts := backend.recorded()
	require.Len(t, puts, 2)
	assert.NotEqual(t, puts[0].key, puts[1].key,
		"two DISTINCT undecodable messages at the IDENTICAL clock reading must never "+
			"share a key: ObjectStore Put replaces and the first write is silently lost (#741)")
}
