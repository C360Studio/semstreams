package dispatch

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/metric"
	"github.com/prometheus/client_golang/prometheus"
)

// KeyedPool is the framework's keyed-ordered bounded-concurrency
// primitive: a sibling to BoundedDispatcher (ADR-048) that partitions
// work into N lanes by a caller-supplied key. All items sharing a key
// route to the same lane and are processed serially in submit order,
// while items with different keys spread across lanes and run
// concurrently. This is distinct from pkg/worker.Pool /
// BoundedDispatcher, which distribute work across workers with no key
// affinity or ordering guarantee.
//
// It exists for at-least-once consumers that need per-key ordered
// concurrency — e.g. graph-ingest, whose arrival-order (full-set-
// replace) merge requires that all messages for one entity apply in
// order, but which is otherwise latency-bound on a serial Get+CAS
// chain (ADR-072, gh#480).
//
// Structure: N separate bounded lane channels, one goroutine per lane.
// A shared channel + N workers (worker.Pool's shape) cannot preserve
// per-key order, so this is a distinct type, not a mode on Pool.
//
// Concurrency: Submit / SubmitBlocking are safe to call from
// concurrent goroutines. Stop is idempotent. The Process function for
// a given lane is only ever invoked by that lane's single goroutine,
// so a composer maintaining per-lane state (indexed by the lane number
// passed into Process) needs no locking on it.
type KeyedPool[W any] struct {
	lanes   int
	keyOf   func(W) string
	process func(ctx context.Context, lane int, work W) error
	onPanic func(work W, recovered any)
	logger  *slog.Logger
	name    string

	laneCh []chan keyedItem[W]
	wg     sync.WaitGroup

	// drainCh is closed by Stop to signal a graceful drain: lane
	// goroutines finish their buffered items then exit, and a
	// SubmitBlocking parked on a full lane unblocks with ErrStopped.
	drainCh chan struct{}

	lifecycleMu sync.Mutex
	stopped     bool

	// submitWG tracks in-flight Submit/SubmitBlocking calls between the
	// stopped-check (which registers the WG) and the channel send. Stop
	// waits on it BEFORE closing drainCh, so no submit can land an item
	// after the lanes begin draining — the accept/stop handoff is atomic
	// (a submit either lands before drain starts and is guaranteed drained,
	// or sees stopped=true and is rejected).
	submitWG sync.WaitGroup

	metrics *keyedMetrics

	// Statistics (atomic).
	submitted int64
	completed int64
	dropped   int64
	inflight  int64
}

// keyedItem wraps a work item with the timestamp it was submitted, so
// the lane can observe queue-wait (submit → pickup) at process start.
// W is generic, so the timestamp can't live on the work value itself.
type keyedItem[W any] struct {
	work     W
	submitAt time.Time
}

// KeyedConfig parameterizes KeyedPool construction. The zero value is
// NOT valid — New rejects Lanes <= 0, QueueDepth <= 0, and a missing
// KeyOf or Process.
type KeyedConfig[W any] struct {
	// Lanes is the number of parallel serial lanes. Same-key work is
	// serialized within a lane; different keys spread across lanes.
	// Must be > 0. Fixed for the pool's lifetime (lane assignment is a
	// pure function of the key over this count).
	Lanes int

	// QueueDepth bounds each lane's queue. Submit returns ErrLaneFull
	// when the target lane is at capacity; SubmitBlocking waits. Must
	// be > 0.
	QueueDepth int

	// KeyOf maps a work item to its partition key. Items with equal
	// keys are processed serially in submit order on one lane. Must be
	// non-nil and pure (called once per Submit).
	KeyOf func(W) string

	// Process handles one work item. It is called by the assigned
	// lane's single goroutine, with that lane's index — a composer can
	// use the index to shard per-lane state without locking (the pool
	// guarantees at most one goroutine per lane). Must be non-nil.
	Process func(ctx context.Context, lane int, work W) error

	// OnPanic — optional. When Process panics, the pool recovers it,
	// keeps the lane goroutine alive, and (if set) calls OnPanic with
	// the work item and the recovered value so the composer can
	// dispose of it (e.g. Nak the underlying message). Called from the
	// lane goroutine. If nil, a recovered panic is logged and dropped.
	OnPanic func(work W, recovered any)

	// Name labels this pool's metrics (the `pool` label). Optional;
	// metrics are only registered when Deps.MetricsRegistry is set.
	Name string
}

// KeyedDeps carries the framework dependencies. Both are optional:
// MetricsRegistry nil → metrics are created but not registered
// (observing is a harmless no-op); Logger nil → slog.Default.
type KeyedDeps struct {
	MetricsRegistry *metric.MetricsRegistry
	Logger          *slog.Logger
}

// NewKeyedPool constructs and starts a KeyedPool. Lane goroutines are
// running and ready to receive Submit calls when it returns — callers
// don't call a separate Start.
//
// The ctx is the pool's run context: it is passed to Process, and its
// cancellation aborts all lanes immediately (in-flight Process calls
// see the cancellation via their ctx; buffered items are NOT drained).
// For a graceful drain that DOES finish buffered work, call Stop.
// The ctx must be non-nil.
//
// Returns ErrInvalidConfig for a missing or out-of-range required
// field.
func NewKeyedPool[W any](ctx context.Context, cfg KeyedConfig[W], deps KeyedDeps) (*KeyedPool[W], error) {
	if ctx == nil {
		return nil, fmt.Errorf("%w: context is required", ErrInvalidConfig)
	}
	if cfg.Lanes <= 0 {
		return nil, fmt.Errorf("%w: Lanes must be > 0 (got %d)", ErrInvalidConfig, cfg.Lanes)
	}
	if cfg.QueueDepth <= 0 {
		return nil, fmt.Errorf("%w: QueueDepth must be > 0 (got %d)", ErrInvalidConfig, cfg.QueueDepth)
	}
	if cfg.KeyOf == nil {
		return nil, fmt.Errorf("%w: KeyOf is required", ErrInvalidConfig)
	}
	if cfg.Process == nil {
		return nil, fmt.Errorf("%w: Process is required", ErrInvalidConfig)
	}

	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	p := &KeyedPool[W]{
		lanes:   cfg.Lanes,
		keyOf:   cfg.KeyOf,
		process: cfg.Process,
		onPanic: cfg.OnPanic,
		logger:  logger,
		name:    cfg.Name,
		laneCh:  make([]chan keyedItem[W], cfg.Lanes),
		drainCh: make(chan struct{}),
	}

	if deps.MetricsRegistry != nil {
		p.metrics = newKeyedMetrics(deps.MetricsRegistry, cfg.Name)
	}

	for i := 0; i < cfg.Lanes; i++ {
		p.laneCh[i] = make(chan keyedItem[W], cfg.QueueDepth)
		p.wg.Add(1)
		go p.lane(ctx, i, p.laneCh[i])
	}
	if p.metrics != nil {
		p.wg.Add(1)
		go p.metricsUpdater(ctx)
	}

	return p, nil
}

// laneFor maps a work item to its lane via FNV-1a over the key mod
// Lanes. Inlined (no hasher allocation) since it is on the submit hot
// path.
func (p *KeyedPool[W]) laneFor(work W) int {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	h := uint64(offset64)
	key := p.keyOf(work)
	for i := 0; i < len(key); i++ {
		h ^= uint64(key[i])
		h *= prime64
	}
	return int(h % uint64(p.lanes))
}

// Submit routes work to its lane without blocking. Returns ErrLaneFull
// if the target lane's queue is at capacity, or ErrStopped after Stop.
// Safe for concurrent use.
func (p *KeyedPool[W]) Submit(work W) error {
	p.lifecycleMu.Lock()
	if p.stopped {
		p.lifecycleMu.Unlock()
		return ErrStopped
	}
	// Register in-flight UNDER the lock, alongside the stopped-check, so Stop's
	// submitWG.Wait() cannot begin until this send completes (atomic handoff).
	p.submitWG.Add(1)
	p.lifecycleMu.Unlock()
	defer p.submitWG.Done()

	lane := p.laneFor(work)
	item := keyedItem[W]{work: work, submitAt: time.Now()}
	select {
	case p.laneCh[lane] <- item:
		atomic.AddInt64(&p.submitted, 1)
		if p.metrics != nil {
			p.metrics.submitted.Inc()
		}
		return nil
	default:
		atomic.AddInt64(&p.dropped, 1)
		if p.metrics != nil {
			p.metrics.dropped.Inc()
		}
		return ErrLaneFull
	}
}

// SubmitBlocking routes work to its lane, blocking until the lane has
// capacity, the ctx is cancelled (returns ctx.Err()), or the pool is
// stopped (returns ErrStopped). This is the backpressure form — a full
// lane blocks the caller rather than dropping.
//
// Composer note (ADR-072 M3): on shutdown, cancel the ctx passed here
// BEFORE calling Stop, so a caller parked on a full lane unblocks and
// can dispose of its message; otherwise a synchronous producer (e.g. a
// NATS consume callback) can wedge teardown. Stop closing drainCh is a
// backstop that also unblocks this call.
func (p *KeyedPool[W]) SubmitBlocking(ctx context.Context, work W) error {
	p.lifecycleMu.Lock()
	if p.stopped {
		p.lifecycleMu.Unlock()
		return ErrStopped
	}
	p.submitWG.Add(1) // see Submit — atomic accept/stop handoff
	p.lifecycleMu.Unlock()
	defer p.submitWG.Done()

	lane := p.laneFor(work)
	item := keyedItem[W]{work: work, submitAt: time.Now()}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-p.drainCh:
		return ErrStopped
	case p.laneCh[lane] <- item:
		atomic.AddInt64(&p.submitted, 1)
		if p.metrics != nil {
			p.metrics.submitted.Inc()
		}
		return nil
	}
}

// lane is one lane's goroutine: it drains its channel in order,
// running each item through Process. On the pool's ctx cancellation it
// aborts immediately; on Stop (drainCh closed) it finishes buffered
// items then exits.
func (p *KeyedPool[W]) lane(ctx context.Context, idx int, ch chan keyedItem[W]) {
	defer p.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case item := <-ch:
			p.runOne(ctx, idx, item)
		case <-p.drainCh:
			// Graceful stop: drain buffered items, then exit.
			for {
				select {
				case item := <-ch:
					p.runOne(ctx, idx, item)
				default:
					return
				}
			}
		}
	}
}

// runOne invokes Process for one item with panic recovery (ADR-072
// H2): a panic in Process must not crash the host or wedge the lane.
// The recovered panic is logged, the composer's OnPanic disposition is
// invoked, and the lane goroutine continues to the next item.
func (p *KeyedPool[W]) runOne(ctx context.Context, lane int, item keyedItem[W]) {
	if p.metrics != nil {
		p.metrics.queueWait.Observe(time.Since(item.submitAt).Seconds())
		p.metrics.inflight.Inc()
	}
	atomic.AddInt64(&p.inflight, 1)
	defer func() {
		atomic.AddInt64(&p.inflight, -1)
		atomic.AddInt64(&p.completed, 1)
		if p.metrics != nil {
			p.metrics.inflight.Dec()
			p.metrics.completed.Inc()
		}
		if r := recover(); r != nil {
			p.logger.Error("dispatch: recovered panic in Process; lane continues",
				slog.String("pool", p.name),
				slog.Int("lane", lane),
				slog.Any("panic", r))
			if p.onPanic != nil {
				p.onPanic(item.work, r)
			}
		}
	}()

	start := time.Now()
	err := p.process(ctx, lane, item.work)
	if p.metrics != nil {
		p.metrics.processing.Observe(time.Since(start).Seconds())
	}
	if err != nil {
		p.logger.Debug("dispatch: Process returned error",
			slog.String("pool", p.name),
			slog.Int("lane", lane),
			slog.String("error", err.Error()))
	}
}

// Stop halts the pool gracefully: it stops accepting new work, drains
// each lane's buffered items to completion, and returns when all lanes
// are idle or the given ctx expires (returns ctx.Err()). Idempotent —
// subsequent calls return nil.
//
// Stop does NOT cancel the pool's run context, so in-flight and
// buffered Process calls run to completion. If the ctx expires first,
// the lanes keep draining in the background; the caller has simply
// stopped waiting.
func (p *KeyedPool[W]) Stop(ctx context.Context) error {
	p.lifecycleMu.Lock()
	if p.stopped {
		p.lifecycleMu.Unlock()
		return nil
	}
	p.stopped = true // reject NEW submits (they see stopped under the lock)
	p.lifecycleMu.Unlock()

	// Atomic accept/stop handoff: wait for in-flight submits (those that passed
	// the stopped-check and registered on submitWG) to finish landing or aborting
	// BEFORE signaling drain, so no item can land after the lanes start draining
	// and exiting. drainCh stays OPEN during this wait, so a SubmitBlocking parked
	// on a full lane still lands as the (still-running) lane frees capacity — or
	// aborts via its own ctx (the M3 composer cancels it first). Bounded by the
	// Stop ctx: on timeout we proceed best-effort (shutdown is already incomplete).
	submitsDone := make(chan struct{})
	go func() {
		p.submitWG.Wait()
		close(submitsDone)
	}()
	select {
	case <-submitsDone:
	case <-ctx.Done():
		p.logger.Warn("dispatch: Stop timed out waiting for in-flight submits",
			slog.String("pool", p.name))
		close(p.drainCh)
		return ctx.Err()
	}

	close(p.drainCh)

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		p.logger.Warn("dispatch: Stop timed out before lanes drained",
			slog.String("pool", p.name))
		return ctx.Err()
	}
}

// metricsUpdater periodically refreshes the aggregate queue-depth
// gauge (sum of lane buffer lengths). Exits on ctx cancel or Stop.
func (p *KeyedPool[W]) metricsUpdater(ctx context.Context) {
	defer p.wg.Done()
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.drainCh:
			return
		case <-ticker.C:
			depth := 0
			for _, ch := range p.laneCh {
				depth += len(ch)
			}
			p.metrics.queueDepth.Set(float64(depth))
		}
	}
}

// KeyedStats is a point-in-time snapshot of a KeyedPool's counters.
type KeyedStats struct {
	Lanes     int   `json:"lanes"`
	Submitted int64 `json:"submitted"`
	Completed int64 `json:"completed"`
	Dropped   int64 `json:"dropped"`
	Inflight  int64 `json:"inflight"`
}

// Stats returns current pool statistics. Thread-safe (atomic reads).
func (p *KeyedPool[W]) Stats() KeyedStats {
	return KeyedStats{
		Lanes:     p.lanes,
		Submitted: atomic.LoadInt64(&p.submitted),
		Completed: atomic.LoadInt64(&p.completed),
		Dropped:   atomic.LoadInt64(&p.dropped),
		Inflight:  atomic.LoadInt64(&p.inflight),
	}
}

// keyedMetrics holds the Prometheus metrics for one KeyedPool,
// distinguished by the `pool` const-label so multiple pools coexist.
type keyedMetrics struct {
	queueWait  prometheus.Histogram
	processing prometheus.Histogram
	queueDepth prometheus.Gauge
	inflight   prometheus.Gauge
	submitted  prometheus.Counter
	completed  prometheus.Counter
	dropped    prometheus.Counter
}

func newKeyedMetrics(registry *metric.MetricsRegistry, name string) *keyedMetrics {
	labels := prometheus.Labels{"pool": name}
	m := &keyedMetrics{
		queueWait: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:        "dispatch_queue_wait_seconds",
			Help:        "Time a work item waited between submit and process start (lane queue wait)",
			ConstLabels: labels,
			Buckets:     []float64{0.0001, 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0},
		}),
		processing: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:        "dispatch_processing_duration_seconds",
			Help:        "Time spent in the Process function",
			ConstLabels: labels,
			Buckets:     []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5},
		}),
		queueDepth: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:        "dispatch_queue_depth",
			Help:        "Aggregate queued items across all lanes",
			ConstLabels: labels,
		}),
		inflight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:        "dispatch_inflight",
			Help:        "Lanes currently executing Process (achieved concurrency)",
			ConstLabels: labels,
		}),
		submitted: prometheus.NewCounter(prometheus.CounterOpts{
			Name:        "dispatch_submitted_total",
			Help:        "Total work items accepted onto a lane",
			ConstLabels: labels,
		}),
		completed: prometheus.NewCounter(prometheus.CounterOpts{
			Name:        "dispatch_completed_total",
			Help:        "Total work items whose Process returned or was recovered",
			ConstLabels: labels,
		}),
		dropped: prometheus.NewCounter(prometheus.CounterOpts{
			Name:        "dispatch_dropped_total",
			Help:        "Total work items rejected by non-blocking Submit on a full lane",
			ConstLabels: labels,
		}),
	}

	// Register with the framework registry. A registration error
	// (e.g. duplicate pool name) is non-fatal — the metric objects
	// still work for local observation; log via the registry's own
	// handling. serviceName groups these under the primitive.
	const serviceName = "keyed_dispatch"
	_ = registry.RegisterHistogram(serviceName, "dispatch_queue_wait_seconds/"+name, m.queueWait)
	_ = registry.RegisterHistogram(serviceName, "dispatch_processing_duration_seconds/"+name, m.processing)
	_ = registry.RegisterGauge(serviceName, "dispatch_queue_depth/"+name, m.queueDepth)
	_ = registry.RegisterGauge(serviceName, "dispatch_inflight/"+name, m.inflight)
	_ = registry.RegisterCounter(serviceName, "dispatch_submitted_total/"+name, m.submitted)
	_ = registry.RegisterCounter(serviceName, "dispatch_completed_total/"+name, m.completed)
	_ = registry.RegisterCounter(serviceName, "dispatch_dropped_total/"+name, m.dropped)

	return m
}
