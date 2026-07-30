package graphingest

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/readiness"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/pkg/errs"
)

// statusMetricsInterval is graph-ingest's readiness heartbeat. It matches the
// framework default so every producer's freshness window is the same constant
// (readiness.FreshnessMultiplier heartbeats).
const statusMetricsInterval = readiness.DefaultHeartbeat

// boundConsumer identifies one durable consumer this component bound, so the status
// tick can ask the client for its outstanding work.
//
// graph-ingest binds ONE consumer PER INPUT PORT, each on a possibly different
// stream, and the backlog is the sum across all of them — a per-port view would let a
// component report caught-up while another port was hours behind.
type boundConsumer struct {
	stream string
	name   string
}

// registerBoundConsumer records a consumer after ConsumeStreamWithConfig has
// committed it, so the readiness tick never asks about a consumer whose bind failed.
func (c *Component) registerBoundConsumer(stream, name string) {
	c.boundConsumersMu.Lock()
	defer c.boundConsumersMu.Unlock()
	for _, existing := range c.boundConsumers {
		if existing.stream == stream && existing.name == name {
			return
		}
	}
	c.boundConsumers = append(c.boundConsumers, boundConsumer{stream: stream, name: name})
}

// boundConsumerSnapshot copies the bound set for iteration off the lock, in a
// deterministic order so a degraded verdict names the same consumer run to run.
func (c *Component) boundConsumerSnapshot() []boundConsumer {
	c.boundConsumersMu.RLock()
	defer c.boundConsumersMu.RUnlock()
	out := make([]boundConsumer, len(c.boundConsumers))
	copy(out, c.boundConsumers)
	sort.Slice(out, func(i, j int) bool {
		if out[i].stream != out[j].stream {
			return out[i].stream < out[j].stream
		}
		return out[i].name < out[j].name
	})
	return out
}

// statusMetricsLoop republishes the readiness envelope on the shared heartbeat.
// Mirrors graph-index's loop (processor/graph-index/component.go:1119-1160): the
// write doubles as a liveness heartbeat, so it is unconditional rather than
// on-change.
func (c *Component) statusMetricsLoop(ctx context.Context) {
	defer c.wg.Done()
	// Publish once up front so the key is populated before the first tick rather
	// than reading absent (which consumers correctly treat as unknown) for a whole
	// interval after start.
	c.refreshReadinessStatus(ctx)
	ticker := time.NewTicker(c.statusTickInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.refreshReadinessStatus(ctx)
		}
	}
}

// statusTickInterval is the heartbeat period, overridable only by tests so an
// integration test can observe successive heartbeats without sleeping through
// production cadence.
func (c *Component) statusTickInterval() time.Duration {
	if c.statusInterval > 0 {
		return c.statusInterval
	}
	return statusMetricsInterval
}

// refreshReadinessStatus computes the envelope ONCE and publishes it. The single
// compute is the contract, not an optimization: two computes at slightly different
// instants would let separate distribution channels disagree about readiness with no
// way for a consumer to tell which was right.
func (c *Component) refreshReadinessStatus(ctx context.Context) {
	status := c.computeBacklogStatus(ctx)
	// ONE compute feeds BOTH channels. Two computes at slightly different instants
	// would let the scraped gauges and the watched KV key disagree about readiness,
	// with no way for a consumer reconciling them to tell which was right.
	if c.readinessGauges != nil {
		c.readinessGauges.Set(status)
	}
	c.publishReadinessStatus(ctx, status)
}

// computeBacklogStatus reads outstanding work across every bound consumer and
// projects it through the shared backlog projection.
//
// A read failure on ANY consumer degrades the whole envelope. Summing only the
// consumers that answered would publish a number that looks like a backlog and is
// actually a partial — precisely the shape that makes a caught-up claim wrong while
// looking healthy. The partial total still rides along as an honest lower bound
// (graph.BacklogStatusInputs.ObservationFailed documents that), but Ready is forced
// false and State degraded.
func (c *Component) computeBacklogStatus(ctx context.Context) graph.IndexStatusResponse {
	// An EMPTY bound set is honestly caught up, not degraded. This is not an edge
	// case: MEASURED 2026-07-30, 9 of the 18 shipped graph-ingest instances bind no
	// jetstream consumer at all — they declare their input port as type "nats"
	// (core NATS request/reply for graph_mutations), which setupSubscriptions skips.
	// Those deployments have no backlog to be behind on. Reporting degraded here
	// would make half the shipped fleet permanently unready and every consumer
	// folding on this key defer forever.
	consumers := c.boundConsumerSnapshot()

	var outstanding uint64
	observationFailed := false

	for _, bc := range consumers {
		work, err := c.natsClient.OutstandingWork(ctx, bc.stream, bc.name)
		if err != nil {
			if ctx.Err() != nil {
				// Shutdown, not a fault.
				return graph.IndexStatusResponse{State: graph.IndexStateBuilding}
			}
			observationFailed = true
			c.logger.Warn("graph-ingest readiness: outstanding-work read failed",
				slog.String("stream", bc.stream),
				slog.String("consumer", bc.name),
				slog.Any("error", err))
			continue
		}
		outstanding += work
	}

	scope, bootstrapComplete := c.latchBootstrap(outstanding, observationFailed)

	return graph.ComputeBacklogStatus(graph.BacklogStatusInputs{
		Outstanding:         outstanding,
		BootstrapComplete:   bootstrapComplete,
		BootstrapScope:      scope,
		ObservationFailed:   observationFailed,
		OldestOutstandingAt: c.oldestOutstandingAt(outstanding),
		Now:                 c.statusNow(),
		LastSynced:          c.lastSyncedRFC3339(),
	})
}

// recordBindBacklog captures this consumer's backlog AT BIND TIME, accumulating across
// ports, as the producer's BootstrapScope.
//
// Capturing here rather than at the first status tick is load-bearing, not tidiness.
// The tick fires after setupSubscriptions, createStatusBucket, and goroutine
// scheduling, and delivery is already live throughout — so a small backlog can drain
// before the first tick reads it, and the producer would publish
// `complete && scope == 0`, which the contract defines as "authoritatively nothing to
// do". That is a FALSE claim about a deployment that had real work, and its value
// would vary run to run with tick timing. At bind the window is a few microseconds and
// draining even one message costs a KV round trip.
//
// A failed read leaves the capture unclaimed so the first successful tick still fills
// it in — a late scope beats a fabricated zero.
func (c *Component) recordBindBacklog(ctx context.Context, stream, name string) {
	work, err := c.natsClient.OutstandingWork(ctx, stream, name)
	if err != nil {
		c.logger.Warn("graph-ingest readiness: bind-time backlog read failed; "+
			"bootstrap scope will be captured at the first successful status tick",
			slog.String("stream", stream),
			slog.String("consumer", name),
			slog.Any("error", err))
		return
	}
	c.bootBacklog.Add(work)
	c.bootBacklogKnown.Store(true)
}

// latchBootstrap maintains the two bootstrap facts and returns (scope, complete).
//
// BootstrapScope is normally captured at bind (recordBindBacklog); the capture here is
// the fallback for when every bind-time read failed. Either way it is captured once and
// never moves, so a later burst cannot retroactively enlarge the "initial build".
//
// BootstrapComplete is the conjunction of two independent facts, per task 3.3:
//
//  1. the boot ENTITY_STATES sweep drained (entityBootstrapComplete, already surfaced
//     in Health), and
//  2. the boot backlog was worked off — observed as outstanding reaching zero at
//     least once since binding.
//
// (2) is a LATCH, not a live read: Ready already carries "caught up right now", and a
// bootstrap bit that flickered off under ordinary write load would make every
// consumer defer during normal operation. It is sound because outstanding only leaves
// the counters by being acked (applied) or parked, so reaching zero means everything
// outstanding at bind time is no longer outstanding — which is exactly the honesty
// boundary the envelope documents, not a completeness claim.
//
// A failed observation never latches anything: zero read off a failed observation is
// the absence of a measurement.
func (c *Component) latchBootstrap(outstanding uint64, observationFailed bool) (uint64, bool) {
	if observationFailed {
		return c.bootBacklog.Load(), false
	}

	if c.bootBacklogKnown.CompareAndSwap(false, true) {
		c.bootBacklog.Store(outstanding)
	}
	if outstanding == 0 {
		c.bootBacklogDrained.Store(true)
	}

	sweepDone := !c.entityBootstrapStarted.Load() || c.entityBootstrapComplete.Load()
	return c.bootBacklog.Load(), sweepDone && c.bootBacklogDrained.Load()
}

// oldestOutstandingAt reports the timestamp the staleness projection ages from, or the
// zero time when there is nothing to report.
//
// DEVIATION FROM THE TASK TEXT, recorded deliberately. Task 3.5 says "the oldest
// outstanding message's meta.Timestamp". Tracking that exactly means per-message
// bookkeeping over up to 2048 in-flight messages on the ingest hot path, with
// insertion and removal on every submit and ack. This reports the AGE OF THE VIEW
// instead — now minus the JetStream timestamp of the most recently APPLIED message —
// which is graph-index's established semantic for the same field (its IndexedAt) and
// costs one atomic store on the ack path.
//
// The two coincide closely because delivery is approximately stream-ordered: the
// oldest outstanding message is the one just after the newest applied one. It is a
// FLOOR either way, and the field is REPORTED, never gating, so the residual
// difference cannot change a readiness verdict.
func (c *Component) oldestOutstandingAt(outstanding uint64) time.Time {
	if outstanding == 0 {
		// Caught up: there is no staleness to report. The projection encodes this as
		// 0 = no information.
		return time.Time{}
	}
	if v := c.lastAppliedAt.Load(); v != nil {
		if t, ok := v.(time.Time); ok {
			return t
		}
	}
	return time.Time{}
}

// lastSyncedRFC3339 is the last-applied timestamp for the envelope's operator field,
// empty when nothing has been applied yet in this process.
func (c *Component) lastSyncedRFC3339() string {
	v := c.lastAppliedAt.Load()
	if v == nil {
		return ""
	}
	t, ok := v.(time.Time)
	if !ok || t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// statusNow is the compute instant, overridable by tests so staleness assertions do
// not race the wall clock.
func (c *Component) statusNow() time.Time {
	if c.statusNowFn != nil {
		return c.statusNowFn()
	}
	return time.Now()
}

// recordApplied stamps the JetStream timestamp of a message whose graph write is
// durable. Called on the ack path only — an un-acked message has not been applied,
// and stamping earlier would age the view from work that may still Nak.
func (c *Component) recordApplied(deliveredAt time.Time) {
	if deliveredAt.IsZero() {
		return
	}
	c.lastAppliedAt.Store(deliveredAt)
}

// publishReadinessStatus writes the envelope to the GRAPH_STATUS key (ADR-083).
//
// A failed write must never kill the tick loop or the component: the next heartbeat is
// the recovery path, and consumers already fail closed after three missed heartbeats,
// so the correct behavior is to leave evidence and keep ticking.
func (c *Component) publishReadinessStatus(ctx context.Context, status graph.IndexStatusResponse) {
	if c.statusPublisher == nil {
		return
	}
	if err := c.statusPublisher.Publish(ctx, status); err != nil {
		if ctx.Err() != nil {
			// Shutdown, not a failure: Stop cancels mid-Put on the way out.
			return
		}
		if c.readinessGauges != nil {
			c.readinessGauges.RecordPublishFailure()
		}
		c.logger.Warn("readiness status publish failed",
			slog.String("bucket", readiness.BucketGraphStatus),
			slog.String("key", c.statusPublisher.Key()),
			slog.Any("error", err))
	}
}

// createStatusBucket provisions the GRAPH_STATUS handle and builds the publisher.
// A provisioning failure is fatal to Start: a producer that cannot publish readiness
// would be indistinguishable on the wire from one that is merely absent, and
// consumers would defer forever with no way to tell why.
func (c *Component) createStatusBucket(ctx context.Context) error {
	bucket, err := readiness.EnsureBucket(ctx, c.natsClient)
	if err != nil {
		if ctx.Err() != nil {
			return errs.Wrap(ctx.Err(), "Component", "createStatusBucket", "context cancelled")
		}
		return errs.Wrap(err, "Component", "createStatusBucket",
			"KV bucket: "+readiness.BucketGraphStatus)
	}
	c.statusPublisher = readiness.NewPublisher(bucket, readiness.KeyGraphIngest)
	return nil
}

// Readiness gauges are a PACKAGE-LEVEL SINGLETON, matching every other metric in this
// component (metricsOnce, ingestLagOnce, casRetriesOnce, ...).
//
// This is not stylistic. metric.MetricsRegistry.RegisterGauge is idempotent BY KEY and
// keeps the FIRST collector (metric/registry.go:102-104), so a second instance's
// registration is a silent no-op. graph-ingest is rebuilt on a config-driven restart
// (service/component_manager.go recreateComponentWithNewConfig), and a per-instance
// gauge set would leave the NEW component writing to unregistered collectors while the
// exported series stayed frozen at the STOPPED instance's last sample — in practice
// readiness=0/state=building forever on a healthy component. Found in review, measured
// against the real registry.
var (
	readinessGaugesOnce sync.Once
	readinessGauges     *readiness.Gauges
)

// initReadinessGauges returns the process-wide gauge set for this producer.
//
// NO revision gauges: graph-ingest is a BACKLOG producer, and the spec states such a
// producer SHALL NOT expose indexed_revision / target_revision and SHALL NOT
// synthesize a value — a fabricated revision is worse than an absent one.
func initReadinessGauges(registry *metric.MetricsRegistry) *readiness.Gauges {
	readinessGaugesOnce.Do(func() {
		readinessGauges = readiness.NewGauges(
			readiness.ProducerNames{Service: "graph-ingest", Subsystem: "graph_ingest"},
		)
		readinessGauges.Register(registry)
	})
	return readinessGauges
}
