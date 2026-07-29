package readiness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// This file is the PRODUCER side of the ADR-083 contract; watcher.go is the consumer
// side. Both live here so the key names, heartbeat, and value encoding cannot drift
// apart across two processors and four consumers. The bucket's SHAPE (name, History,
// retention) is declared once in the framework KV catalog (graph/kvcatalog.go).

// publishTimeout bounds one heartbeat write. It is well under DefaultHeartbeat so a
// wedged Put cannot eat the whole interval it exists to announce: the tick loop must
// come back around and try again (and a Ticker drops, never queues, missed ticks).
const publishTimeout = 2 * time.Second

// StatusWriter writes one status value. jetstream.KeyValue satisfies it; taking the
// one method the publisher needs keeps the seam narrow and the unit tests honest.
type StatusWriter interface {
	Put(ctx context.Context, key string, value []byte) (uint64, error)
}

// EnsureBucket acquires the GRAPH_STATUS bucket through the catalog owner seam
// and returns a handle. Every producer calls it at Start, EAGERLY — before any
// consumer could bind — because a consumer that binds a watch to a
// not-yet-existent bucket reads permanently unknown (fail-closed) until it
// happens to rebind, and the whole point of this contract is that unknown
// means something is wrong.
//
// It is IDEMPOTENT across producers and restarts: graph-index and
// graph-embedding both run in cmd/semstreams and both call it, in either order
// and possibly concurrently; the seam's create-or-open resolves the race, and
// an adopted bucket is RECONCILED to the catalog declaration (History,
// no-lifecycle retention) rather than adopted config-unseen.
func EnsureBucket(ctx context.Context, client *natsclient.Client) (jetstream.KeyValue, error) {
	if client == nil {
		return nil, errors.New("readiness: nil NATS client")
	}
	bucket, err := graph.EnsureCatalogBucket(ctx, client, BucketGraphStatus)
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
