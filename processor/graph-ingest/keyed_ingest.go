package graphingest

import (
	"context"
	"encoding/binary"
	"log/slog"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/dispatch"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
)

// ingestWork is one unit submitted to the keyed ingest pool: a decoded entity
// plus the JetStream message (for ack/nak) and the (stream, sequence) the
// redelivery guard keys on. Partitioned by entityID so same-entity updates
// serialize on one lane in arrival order (ADR-072, gh#480).
type ingestWork struct {
	entity   *graph.EntityState
	msg      jetstream.Msg
	entityID string
	stream   string
	seq      uint64
}

// laneGuard is one lane's in-memory applied-sequence cache — tier 1 of the
// redelivery guard. It is accessed only by that lane's single goroutine, so it
// needs no locking. Bounded: past max, one arbitrary entry is evicted — a cache
// miss just falls through to the durable tier, so which entry leaves the cache
// never affects correctness (ADR-072 B3).
type laneGuard struct {
	seq      map[string]uint64
	capacity int
}

func newLaneGuard(capacity int) *laneGuard {
	return &laneGuard{seq: make(map[string]uint64), capacity: capacity}
}

func (g *laneGuard) get(key string) (uint64, bool) {
	v, ok := g.seq[key]
	return v, ok
}

func (g *laneGuard) set(key string, v uint64) {
	if _, exists := g.seq[key]; !exists && len(g.seq) >= g.capacity {
		// Evict one arbitrary entry (map iteration order). The durable tier backs
		// correctness, so which entry leaves the cache is immaterial.
		for k := range g.seq {
			delete(g.seq, k)
			break
		}
	}
	g.seq[key] = v
}

// guardKey composes the durable/in-memory guard key. entityID is dotted and
// stream is uppercase alphanumeric; "/" separates them and is a valid NATS KV
// key character, so the composite is a valid durable-bucket key.
func guardKey(entityID, stream string) string {
	return entityID + "/" + stream
}

// buildIngestPool constructs the keyed-concurrent ingest pool + the in-memory
// guard shards. Called from Start BEFORE subscriptions (ADR-072 M3). The pool
// and submit contexts are independent of the consume context, so a consumer-ctx
// cancel on Stop does not abort in-flight merges; Stop cancels the submit ctx
// first, then drains, then cancels the pool ctx.
func (c *Component) buildIngestPool() error {
	lanes := c.config.IngestLanes
	if lanes < 1 {
		lanes = 1
	}

	c.ingestPoolCtx, c.ingestPoolCancel = context.WithCancel(context.Background())
	c.ingestSubmitCtx, c.ingestSubmitCancel = context.WithCancel(context.Background())

	c.ingestGuardMem = make([]*laneGuard, lanes)
	for i := range c.ingestGuardMem {
		c.ingestGuardMem[i] = newLaneGuard(ingestGuardMemMaxPerLane)
	}

	pool, err := dispatch.NewKeyedPool(c.ingestPoolCtx, dispatch.KeyedConfig[ingestWork]{
		Lanes:      lanes,
		QueueDepth: ingestLaneQueueDepth,
		Name:       "graph_ingest",
		KeyOf:      func(w ingestWork) string { return w.entityID },
		Process:    c.processIngest,
		OnPanic: func(w ingestWork, _ any) {
			// The lane recovered a panic in processIngest; Nak so the server
			// redelivers rather than silently losing the message.
			if err := w.msg.Nak(); err != nil {
				c.logger.Error("Failed to Nak after ingest panic", slog.Any("error", err))
			}
		},
	}, dispatch.KeyedDeps{MetricsRegistry: c.metricsRegistry, Logger: c.logger})
	if err != nil {
		return errs.Wrap(err, "Component", "buildIngestPool", "keyed pool construction")
	}
	c.ingestPool = pool
	return nil
}

// teardownIngestPool aborts the keyed ingest pool on a boot-failure path (Start
// returning an error after buildIngestPool succeeded, where Stop won't run
// because running is still false). Cancelling the pool ctx makes the lane
// goroutines + metricsUpdater return; cancelling the submit ctx unblocks any
// (not-yet-possible) parked submit. Nil-safe.
func (c *Component) teardownIngestPool() {
	if c.ingestSubmitCancel != nil {
		c.ingestSubmitCancel()
	}
	if c.ingestPoolCancel != nil {
		c.ingestPoolCancel()
	}
}

// processIngest is the keyed pool's per-item handler, run on lane `lane`'s
// single goroutine. It applies the redelivery guard, ingests the entity, then
// stamps the guard (durable first, then in-memory) and acks — the ADR-072
// two-tier guard, updated AFTER side effects and BEFORE ack.
func (c *Component) processIngest(ctx context.Context, lane int, work ingestWork) error {
	// Redelivery guard (ADR-072 B1/B2/B3): drop a stale re-delivery whose stream
	// sequence is not newer than the last already applied to this entity from the
	// same stream.
	stale, err := c.ingestGuardStale(ctx, lane, work)
	if err != nil {
		// Transient durable-guard read failure — Nak so a redelivery re-reads,
		// rather than risk applying a possibly-stale message or dropping a valid
		// one on a blind guess.
		c.logger.Warn("graph-ingest: redelivery-guard read failed; redelivering",
			slog.String("entity_id", work.entityID), slog.Any("error", err))
		if nakErr := work.msg.Nak(); nakErr != nil {
			c.logger.Error("Failed to Nak after guard-read error", slog.Any("error", nakErr))
		}
		return err
	}
	if stale {
		c.redeliveriesDropped.Inc()
		if ackErr := work.msg.Ack(); ackErr != nil {
			c.logger.Error("Failed to ack stale redelivery", slog.Any("error", ackErr))
		}
		return nil
	}

	// Apply. Today's semantics: a failed merge logs + counts + ack-drops (no guard
	// stamp, so a redelivery re-attempts the apply).
	start := time.Now()
	ingestErr := c.ingestEntity(ctx, work.entity)
	c.processingDuration.Observe(time.Since(start).Seconds())
	if ingestErr != nil {
		if ackErr := work.msg.Ack(); ackErr != nil {
			c.logger.Error("Failed to ack after ingest error", slog.Any("error", ackErr))
		}
		return ingestErr
	}

	// Guard stamp AFTER side effects, BEFORE ack — durable FIRST, then in-memory.
	// On a durable-write failure, Nak and leave the in-memory tier un-updated so
	// the two tiers never diverge (a tier-1 update ahead of a failed durable write
	// would let a post-restart older redelivery slip past a stale durable stamp).
	if derr := c.ingestGuardStampDurable(ctx, work); derr != nil {
		c.logger.Warn("graph-ingest: redelivery-guard durable stamp failed; redelivering",
			slog.String("entity_id", work.entityID), slog.Any("error", derr))
		if nakErr := work.msg.Nak(); nakErr != nil {
			c.logger.Error("Failed to Nak after guard-stamp error", slog.Any("error", nakErr))
		}
		return derr
	}
	c.ingestGuardMem[lane].set(guardKey(work.entityID, work.stream), work.seq)

	if ackErr := work.msg.Ack(); ackErr != nil {
		c.logger.Error("Failed to ack JetStream message", slog.Any("error", ackErr))
	}
	return nil
}

// ingestGuardStale reports whether work is a stale redelivery — its stream
// sequence is not newer than the last already applied to (entity, stream). It
// checks the in-memory tier first (lane-local, no KV op), falling back to the
// durable tier on a miss (cold / evicted / post-restart) and warming the cache.
func (c *Component) ingestGuardStale(ctx context.Context, lane int, work ingestWork) (bool, error) {
	key := guardKey(work.entityID, work.stream)

	if last, ok := c.ingestGuardMem[lane].get(key); ok {
		return work.seq <= last, nil
	}
	if c.ingestGuardBucket == nil {
		return false, nil // no durable tier provisioned → treat as first-seen
	}
	entry, err := c.ingestGuardBucket.Get(ctx, key)
	if err != nil {
		if natsclient.IsKVNotFoundError(err) {
			return false, nil // never applied → not stale
		}
		return false, err
	}
	if len(entry.Value) < 8 {
		return false, nil // corrupt/legacy value → treat as first-seen, re-stamp on apply
	}
	last := binary.BigEndian.Uint64(entry.Value)
	c.ingestGuardMem[lane].set(key, last) // warm the cache
	return work.seq <= last, nil
}

// ingestGuardStampDurable persists the last-applied sequence for (entity,
// stream) in the durable guard bucket. A plain Put (last-writer-wins) is safe:
// (entity, stream) is only ever written by one lane's goroutine (entity → one
// lane), so there is no concurrent writer to race.
func (c *Component) ingestGuardStampDurable(ctx context.Context, work ingestWork) error {
	if c.ingestGuardBucket == nil {
		return nil
	}
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], work.seq)
	_, err := c.ingestGuardBucket.Put(ctx, guardKey(work.entityID, work.stream), buf[:])
	return err
}
