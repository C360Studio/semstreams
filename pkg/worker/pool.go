// Package worker provides a generic worker pool for concurrent task processing
package worker

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360/semstreams/metric"
	"github.com/prometheus/client_golang/prometheus"
)

// Pool represents a generic worker pool that can process any work type T
type Pool[T any] struct {
	// Configuration
	workers   int
	queueSize int
	processor func(context.Context, T) error

	// Runtime state
	workChan chan T
	metrics  *Metrics
	wg       *sync.WaitGroup

	// Lifecycle management
	lifecycleMu sync.Mutex
	started     bool
	stopped     bool

	// Statistics (atomic)
	submitted int64
	processed int64
	failed    int64
	dropped   int64

	// Metrics configuration
	metricsRegistry *metric.MetricsRegistry
	metricsPrefix   string
}

// Metrics holds Prometheus metrics for worker pool monitoring
type Metrics struct {
	queueDepth     prometheus.Gauge
	utilization    prometheus.Gauge
	submitted      prometheus.Counter
	processed      prometheus.Counter
	failed         prometheus.Counter
	dropped        prometheus.Counter
	processingTime *prometheus.HistogramVec
}

// Option represents a configuration option for the worker pool
type Option[T any] func(*Pool[T])

// WithMetricsRegistry configures the pool to register metrics with the framework's registry
func WithMetricsRegistry[T any](registry *metric.MetricsRegistry, prefix string) Option[T] {
	return func(p *Pool[T]) {
		p.metricsRegistry = registry
		p.metricsPrefix = prefix
	}
}

// NewPool creates a new generic worker pool with optional configuration
func NewPool[T any](workers, queueSize int, processor func(context.Context, T) error, opts ...Option[T]) *Pool[T] {
	if workers <= 0 {
		workers = 10 // Default worker count
	}
	if queueSize <= 0 {
		queueSize = 1000 // Default queue size
	}
	if processor == nil {
		panic(ErrNilProcessor)
	}

	pool := &Pool[T]{
		workers:   workers,
		queueSize: queueSize,
		processor: processor,
		workChan:  make(chan T, queueSize),
	}

	// Apply options
	for _, opt := range opts {
		opt(pool)
	}

	// Initialize metrics if registry provided
	if pool.metricsRegistry != nil && pool.metricsPrefix != "" {
		pool.initializeMetrics()
	}

	return pool
}

// initializeMetrics creates and registers metrics with the framework's registry
func (p *Pool[T]) initializeMetrics() {
	prefix := p.metricsPrefix

	// Create metrics
	queueDepth := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: prefix + "_queue_depth",
		Help: "Current worker pool queue depth",
	})
	utilization := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: prefix + "_utilization",
		Help: "Worker pool utilization (0-1)",
	})
	submitted := prometheus.NewCounter(prometheus.CounterOpts{
		Name: prefix + "_submitted_total",
		Help: "Total work items submitted",
	})
	processed := prometheus.NewCounter(prometheus.CounterOpts{
		Name: prefix + "_processed_total",
		Help: "Total work items processed",
	})
	failed := prometheus.NewCounter(prometheus.CounterOpts{
		Name: prefix + "_failed_total",
		Help: "Total work items that failed processing",
	})
	dropped := prometheus.NewCounter(prometheus.CounterOpts{
		Name: prefix + "_dropped_total",
		Help: "Total work items dropped due to full queue",
	})
	processingTime := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    prefix + "_processing_duration_seconds",
		Help:    "Time spent processing work items",
		Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0},
	}, []string{"status"})

	// Register with framework's registry
	serviceName := "worker_pool"
	p.metricsRegistry.RegisterGauge(serviceName, prefix+"_queue_depth", queueDepth)
	p.metricsRegistry.RegisterGauge(serviceName, prefix+"_utilization", utilization)
	p.metricsRegistry.RegisterCounter(serviceName, prefix+"_submitted_total", submitted)
	p.metricsRegistry.RegisterCounter(serviceName, prefix+"_processed_total", processed)
	p.metricsRegistry.RegisterCounter(serviceName, prefix+"_failed_total", failed)
	p.metricsRegistry.RegisterCounter(serviceName, prefix+"_dropped_total", dropped)
	p.metricsRegistry.RegisterHistogramVec(serviceName, prefix+"_processing_duration_seconds", processingTime)

	// Store metrics for use
	p.metrics = &Metrics{
		queueDepth:     queueDepth,
		utilization:    utilization,
		submitted:      submitted,
		processed:      processed,
		failed:         failed,
		dropped:        dropped,
		processingTime: processingTime,
	}
}

// Submit submits work to the pool. Returns error if queue is full.
func (p *Pool[T]) Submit(work T) error {
	p.lifecycleMu.Lock()
	defer p.lifecycleMu.Unlock()

	if !p.started {
		return ErrPoolNotStarted
	}
	if p.stopped {
		return ErrPoolStopped
	}

	// Try to submit work (non-blocking)
	select {
	case p.workChan <- work:
		atomic.AddInt64(&p.submitted, 1)
		if p.metrics != nil {
			p.metrics.submitted.Inc()
			p.metrics.queueDepth.Set(float64(len(p.workChan)))
		}
		return nil
	default:
		// Queue is full - drop the work
		atomic.AddInt64(&p.dropped, 1)
		if p.metrics != nil {
			p.metrics.dropped.Inc()
		}
		return ErrQueueFull
	}
}

// Start starts the worker pool
func (p *Pool[T]) Start(ctx context.Context) error {
	p.lifecycleMu.Lock()
	defer p.lifecycleMu.Unlock()

	if p.started {
		return ErrPoolAlreadyStarted
	}

	// Initialize wait group
	p.wg = &sync.WaitGroup{}

	// Start workers with context passed through
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker(ctx, i)
	}

	// Start metrics updater if metrics enabled
	if p.metrics != nil {
		p.wg.Add(1)
		go p.metricsUpdater(ctx)
	}

	p.started = true
	return nil
}

// Stop stops the worker pool immediately
func (p *Pool[T]) Stop(timeout time.Duration) error {
	p.lifecycleMu.Lock()
	defer p.lifecycleMu.Unlock()

	if !p.started || p.stopped {
		return nil
	}

	// Close work channel to signal no more work
	close(p.workChan)

	// Wait for workers to finish with the provided timeout
	done := make(chan struct{})
	go func() {
		if p.wg != nil {
			p.wg.Wait()
		}
		close(done)
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-done:
		// Clean exit
		p.stopped = true
		return nil
	case <-timer.C:
		// Timeout - workers may be stuck
		return ErrStopTimeout
	}
}

// Stats returns current pool statistics
func (p *Pool[T]) Stats() PoolStats {
	return PoolStats{
		Workers:    p.workers,
		QueueSize:  p.queueSize,
		QueueDepth: len(p.workChan),
		Submitted:  atomic.LoadInt64(&p.submitted),
		Processed:  atomic.LoadInt64(&p.processed),
		Failed:     atomic.LoadInt64(&p.failed),
		Dropped:    atomic.LoadInt64(&p.dropped),
	}
}

// PoolStats represents worker pool statistics
type PoolStats struct {
	Workers    int   `json:"workers"`
	QueueSize  int   `json:"queue_size"`
	QueueDepth int   `json:"queue_depth"`
	Submitted  int64 `json:"submitted"`
	Processed  int64 `json:"processed"`
	Failed     int64 `json:"failed"`
	Dropped    int64 `json:"dropped"`
}

// worker processes work items from the queue
func (p *Pool[T]) worker(ctx context.Context, _ int) {
	defer p.wg.Done()

	for {
		select {
		case <-ctx.Done():
			// Context cancelled - exit immediately
			return
		case work, ok := <-p.workChan:
			if !ok {
				// Channel closed - exit immediately
				return
			}

			// Process work item with context
			start := time.Now()
			err := p.processor(ctx, work)
			duration := time.Since(start)

			// Update statistics
			atomic.AddInt64(&p.processed, 1)
			if err != nil {
				atomic.AddInt64(&p.failed, 1)
			}

			// Update metrics
			if p.metrics != nil {
				p.metrics.processed.Inc()
				status := "success"
				if err != nil {
					p.metrics.failed.Inc()
					status = "error"
				}
				p.metrics.processingTime.WithLabelValues(status).Observe(duration.Seconds())
			}
		}
	}
}

// metricsUpdater periodically updates utilization and queue depth metrics
func (p *Pool[T]) metricsUpdater(ctx context.Context) {
	defer p.wg.Done()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if p.metrics != nil {
				// Update queue depth
				queueDepth := float64(len(p.workChan))
				p.metrics.queueDepth.Set(queueDepth)

				// Calculate utilization (queue depth / queue size)
				utilization := queueDepth / float64(p.queueSize)
				p.metrics.utilization.Set(utilization)
			}
		}
	}
}
