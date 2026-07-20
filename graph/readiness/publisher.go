package readiness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/nats-io/nats.go/jetstream"
)

// This file is the PRODUCER side of the ADR-083 contract; watcher.go is the consumer
// side. Both live here so the bucket name, key names, history depth, heartbeat, and
// value encoding cannot drift apart across two processors and four consumers.

// bucketDescription is the operator-facing "what is this and who owns it" string on
// the bucket itself, so `nats kv ls` explains the bucket without the spec.
const bucketDescription = "ADR-083 readiness envelopes, one key per producer (operational component status, not graph data)"

// publishTimeout bounds one heartbeat write. It is well under DefaultHeartbeat so a
// wedged Put cannot eat the whole interval it exists to announce: the tick loop must
// come back around and try again (and a Ticker drops, never queues, missed ticks).
const publishTimeout = 2 * time.Second

// BucketCreator creates-or-opens a KV bucket. *natsclient.Client satisfies it via
// CreateKeyValueBucket — declared here as the narrow method set with the EXACT
// signature (a divergent embedded signature silently fails the structural match and
// no-ops the capability) rather than importing natsclient, which would make this
// package non-leaf and drag a live NATS into its unit tests.
type BucketCreator interface {
	CreateKeyValueBucket(ctx context.Context, cfg jetstream.KeyValueConfig) (jetstream.KeyValue, error)
}

// StatusWriter writes one status value. jetstream.KeyValue satisfies it; taking the
// one method the publisher needs keeps the seam narrow and the unit tests honest.
type StatusWriter interface {
	Put(ctx context.Context, key string, value []byte) (uint64, error)
}

// EnsureBucket creates-or-opens the GRAPH_STATUS bucket and returns a handle. Every
// producer calls it at Start, EAGERLY — before any consumer could bind — because a
// consumer that binds a watch to a not-yet-existent bucket reads permanently unknown
// (fail-closed) until it happens to rebind, and the whole point of this contract is
// that unknown means something is wrong.
//
// It is IDEMPOTENT across producers and restarts: graph-index and graph-embedding both
// run in cmd/semstreams and both call it, in either order and possibly concurrently.
// Idempotency comes from natsclient's CreateKeyValueBucket, which gets an existing
// bucket before attempting a create and treats a concurrent create as success.
//
// That get-first behavior also means an existing bucket is adopted WITHOUT comparing
// its config, so a pre-existing GRAPH_STATUS with a different History is used as-is
// rather than rejected. Nothing depends on that today precisely because every caller
// routes through THIS function and therefore asks for one shape — which is the reason
// to keep it that way rather than hand-rolling a second bucket config elsewhere.
//
// The bucket carries no TTL and no size-based eviction: staleness is judged
// consumer-side from arrival time (D2), so expiring the key server-side would only
// destroy the last-known state a consumer needs to diagnose a dead producer.
func EnsureBucket(ctx context.Context, creator BucketCreator) (jetstream.KeyValue, error) {
	if creator == nil {
		return nil, errors.New("readiness: nil bucket creator")
	}
	bucket, err := creator.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:      BucketGraphStatus,
		Description: bucketDescription,
		History:     BucketHistory,
	})
	if err != nil {
		return nil, fmt.Errorf("readiness: ensure bucket %s: %w", BucketGraphStatus, err)
	}
	return bucket, nil
}

// Publisher writes one producer's readiness envelope to its GRAPH_STATUS key. One
// instance per producer; Publish is called from the status tick and is safe to call
// from any single goroutine.
type Publisher struct {
	bucket  StatusWriter
	key     string
	timeout time.Duration
}

// NewPublisher builds a publisher for one producer key (KeyGraphIndex,
// KeyGraphEmbedding). It returns nil when the wiring is incomplete, and a nil
// *Publisher's Publish is a safe no-op: readiness reporting must never be the thing
// that panics the component whose health it reports.
func NewPublisher(bucket StatusWriter, key string) *Publisher {
	if bucket == nil || key == "" {
		return nil
	}
	return &Publisher{bucket: bucket, key: key, timeout: publishTimeout}
}

// Publish writes the envelope as the KV value for this producer's key.
//
// The value is PLAIN graph.IndexStatusResponse JSON — the same struct fusion decodes,
// with no BaseMessage wrapper. The payload registry governs polymorphic message
// publishes on subjects, where a receiver must discriminate a type it did not choose;
// this is a KV value on a fixed key whose type is fixed by the contract, and wrapping
// it would break every consumer's plain decode (graph/readiness, pkg/fusion) for
// nothing.
//
// Callers publish on EVERY tick, unconditionally, without comparing to the last
// value: the write is the liveness heartbeat that lets a consumer tell "not ready"
// from "the producer is gone", and skipping unchanged values would make a healthy
// steady state indistinguishable from a dead one.
//
// The error is returned, never swallowed, so the tick loop can count and log it; the
// loop must keep ticking regardless, because the next heartbeat is the recovery.
func (p *Publisher) Publish(ctx context.Context, status graph.IndexStatusResponse) error {
	if p == nil || p.bucket == nil {
		return nil
	}
	data, err := json.Marshal(status)
	if err != nil {
		// Unreachable (all-scalar struct) but wrapped rather than ignored: a silent
		// marshal failure would stop the heartbeat with no evidence.
		return fmt.Errorf("readiness: marshal status for %s/%s: %w", BucketGraphStatus, p.key, err)
	}
	putCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	if _, err := p.bucket.Put(putCtx, p.key, data); err != nil {
		return fmt.Errorf("readiness: put %s/%s: %w", BucketGraphStatus, p.key, err)
	}
	return nil
}

// Key reports the producer key this publisher writes, for the caller's failure log.
func (p *Publisher) Key() string {
	if p == nil {
		return ""
	}
	return p.key
}
