package readiness

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/nats-io/nats.go/jetstream"
)

// fakeCreator records the bucket config both producers ask for. The whole point of
// EnsureBucket is that graph-index and graph-embedding cannot ask for DIFFERENT
// shapes of the same bucket, so the test asserts on the recorded config rather than
// on a successful call.
type fakeCreator struct {
	configs []jetstream.KeyValueConfig
	bucket  jetstream.KeyValue
	err     error
}

func (c *fakeCreator) CreateKeyValueBucket(_ context.Context, cfg jetstream.KeyValueConfig) (jetstream.KeyValue, error) {
	c.configs = append(c.configs, cfg)
	return c.bucket, c.err
}

// fakeWriter captures Put calls. It is the narrow producer-side method set, so the
// test needs neither a live NATS nor the other 19 jetstream.KeyValue methods.
type fakeWriter struct {
	key   string
	value []byte
	calls int
	err   error
}

func (w *fakeWriter) Put(_ context.Context, key string, value []byte) (uint64, error) {
	w.calls++
	w.key = key
	// Copy: the caller owns the marshal buffer and a later tick may reuse it.
	w.value = append([]byte(nil), value...)
	if w.err != nil {
		return 0, w.err
	}
	return uint64(w.calls), nil
}

func TestEnsureBucket_BothProducersAskForOneShape(t *testing.T) {
	creator := &fakeCreator{bucket: &fakeBucket{}}

	// Two producers in one binary, as in cmd/semstreams: the second Start must ask
	// for exactly what the first did, or the create is not idempotent.
	for i := 0; i < 2; i++ {
		if _, err := EnsureBucket(context.Background(), creator); err != nil {
			t.Fatalf("EnsureBucket call %d: %v", i, err)
		}
	}

	if len(creator.configs) != 2 {
		t.Fatalf("want 2 create calls, got %d", len(creator.configs))
	}
	for i, cfg := range creator.configs {
		if cfg.Bucket != BucketGraphStatus {
			t.Errorf("call %d: bucket = %q, want %q", i, cfg.Bucket, BucketGraphStatus)
		}
		if cfg.History != BucketHistory {
			t.Errorf("call %d: history = %d, want %d", i, cfg.History, BucketHistory)
		}
		// The readiness bucket is NOT the live graph, but ADR-068 discipline holds:
		// no TTL, no size-based eviction. Freshness is judged consumer-side.
		if cfg.TTL != 0 {
			t.Errorf("call %d: TTL = %v, want 0 (freshness is consumer-side, ADR-083 D2)", i, cfg.TTL)
		}
	}
	if !reflect.DeepEqual(creator.configs[0], creator.configs[1]) {
		t.Errorf("producers asked for different configs:\n%+v\n%+v", creator.configs[0], creator.configs[1])
	}
}

func TestEnsureBucket_Errors(t *testing.T) {
	boom := errors.New("jetstream unavailable")
	tests := []struct {
		name    string
		creator BucketCreator
		wantIs  error
	}{
		{name: "nil creator", creator: nil},
		{name: "create fails", creator: &fakeCreator{err: boom}, wantIs: boom},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := EnsureBucket(context.Background(), tt.creator)
			if err == nil {
				t.Fatal("want error, got nil")
			}
			if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
				t.Errorf("error does not wrap the cause: %v", err)
			}
		})
	}
}

// TestPublisher_ValueIsPlainEnvelopeJSON pins the wire contract from BOTH ends: the
// producer writes a bare graph.IndexStatusResponse (a KV value, NOT a payload-registry
// BaseMessage publish) and the production consumer — the real Watcher — decodes it
// back to an equal envelope. A registry envelope here would round-trip through neither.
func TestPublisher_ValueIsPlainEnvelopeJSON(t *testing.T) {
	tests := []struct {
		name   string
		key    string
		status graph.IndexStatusResponse
	}{
		{
			name: "ready",
			key:  KeyGraphIndex,
			status: graph.IndexStatusResponse{
				Ready: true, State: graph.IndexStateReady,
				IndexedRevision: 500, TargetRevision: 500, Revision: "500",
			},
		},
		{
			name: "building with staleness",
			key:  KeyGraphEmbedding,
			status: graph.IndexStatusResponse{
				State:           graph.IndexStateBuilding,
				IndexedRevision: 480, TargetRevision: 500, Lag: 20, StalenessMs: 1200,
			},
		},
		{
			name: "reset required carries code and reason",
			key:  KeyGraphIndex,
			status: graph.IndexStatusResponse{
				State:  graph.IndexStateResetRequired,
				Code:   graph.ErrorCodeGraphStateResetRequired,
				Reason: string(graph.GraphStateReasonNoncanonicalPredicate),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writer := &fakeWriter{}
			pub := NewPublisher(writer, tt.key)

			if err := pub.Publish(context.Background(), tt.status); err != nil {
				t.Fatalf("Publish: %v", err)
			}
			if writer.key != tt.key {
				t.Errorf("key = %q, want %q", writer.key, tt.key)
			}

			// No payload-registry envelope: the value is the struct itself, so a
			// BaseMessage discriminator must not appear.
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(writer.value, &raw); err != nil {
				t.Fatalf("value is not a JSON object: %v", err)
			}
			for _, envelopeKey := range []string{"type", "payload", "message_type"} {
				if _, found := raw[envelopeKey]; found {
					t.Errorf("value carries payload-registry envelope key %q: %s", envelopeKey, writer.value)
				}
			}

			// The production consumer decode path, not a local json.Unmarshal.
			h := newHarness(t)
			h.deliver(fakeEntry{key: tt.key, value: writer.value, rev: 1, op: jetstream.KeyValuePut})

			got := h.watcher.Read()
			if !got.Known {
				t.Fatal("watcher did not accept the published value")
			}
			if got.Status != tt.status {
				t.Errorf("round-trip mismatch:\n got %+v\nwant %+v", got.Status, tt.status)
			}
		})
	}
}

func TestPublisher_PutFailureIsReturnedNotSwallowed(t *testing.T) {
	// The tick loop must be able to count and log the failure, so it has to see it.
	boom := errors.New("no responders")
	pub := NewPublisher(&fakeWriter{err: boom}, KeyGraphIndex)

	err := pub.Publish(context.Background(), graph.IndexStatusResponse{State: graph.IndexStateBuilding})
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !errors.Is(err, boom) {
		t.Errorf("error does not wrap the Put cause: %v", err)
	}
}

func TestNewPublisher_RejectsIncompleteWiring(t *testing.T) {
	tests := []struct {
		name   string
		writer StatusWriter
		key    string
	}{
		{name: "nil writer", writer: nil, key: KeyGraphIndex},
		{name: "empty key", writer: &fakeWriter{}, key: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pub := NewPublisher(tt.writer, tt.key)
			if pub != nil {
				t.Fatalf("want nil publisher for incomplete wiring, got %+v", pub)
			}
			// A nil publisher must stay a safe no-op: the tick loop guards on it, and
			// a panic there would take down the component the status describes.
			if err := pub.Publish(context.Background(), graph.IndexStatusResponse{}); err != nil {
				t.Errorf("nil publisher Publish = %v, want nil", err)
			}
		})
	}
}
