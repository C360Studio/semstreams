// Package worker provides a generic, thread-safe worker pool for concurrent task processing.
//
// # Overview
//
// The worker package implements a production-ready worker pool pattern with:
//   - Generic type support (Go 1.18+) for type-safe work processing
//   - Bounded queues with backpressure (non-blocking submit)
//   - Context-aware cancellation and graceful shutdown
//   - Dual-tracking observability (always-on statistics + optional Prometheus metrics)
//   - Configurable worker count and queue sizing
//
// # Core Concepts
//
// Worker Pool Pattern:
//
// The worker pool manages a fixed number of goroutines (workers) that process work items
// from a bounded channel (queue). This pattern provides:
//   - Resource control: Fixed memory and goroutine overhead
//   - Backpressure: Queue fills when workers can't keep up
//   - Load distribution: Work items evenly distributed across workers
//   - Observability: Statistics on throughput, latency, and queue depth
//
// Generic Type Safety:
//
// Using Go generics, the pool can process any work type T without type assertions:
//
//	type MessageTask struct {
//	    ID      string
//	    Payload []byte
//	}
//
//	pool := worker.NewPool[MessageTask](
//	    10,    // workers
//	    1000,  // queue size
//	    func(ctx context.Context, task MessageTask) error {
//	        // Process task
//	        return nil
//	    },
//	)
//
// Dual-Tracking Observability:
//
// Following the framework pattern:
//   - Statistics: ALWAYS tracked using atomic operations (zero-allocation)
//   - Metrics: OPTIONAL Prometheus metrics for external monitoring
//
// This ensures internal observability is always available while allowing
// users to opt-in to Prometheus integration.
//
// # Architecture Decisions
//
// Non-Blocking Submit with Backpressure:
//
// Submit() uses a non-blocking send (select with default case) rather than
// blocking on a full queue. This provides:
//   - Predictable latency: Callers never block waiting for queue space
//   - Clear semantics: ErrQueueFull indicates system overload
//   - Backpressure signal: Dropped work indicates workers can't keep up
//
// Alternative considered: Blocking submit with timeout
// Rejected because: Forces callers to handle timeout vs full queue separately,
// and blocking semantics complicate error handling in request paths.
//
// Context-Based Cancellation:
//
// Workers receive context from Start() and check it on each iteration. This enables:
//   - Clean shutdown: In-flight work completes, no new work starts
//   - Timeout enforcement: Caller can use context.WithTimeout
//   - Cancellation propagation: Work processors receive same context
//
// The processor function signature: func(context.Context, T) error
// This allows work processors to respect cancellation themselves.
//
// Graceful Shutdown with Timeout:
//
// Stop(timeout) provides best-effort graceful shutdown:
//  1. Close work channel (no new submissions)
//  2. Workers drain remaining queue items
//  3. Wait for all workers with timeout
//  4. Return ErrStopTimeout if workers don't finish
//
// Note: Individual workers don't have per-worker timeouts. The timeout applies
// to the entire pool shutdown. If you need per-work-item timeouts, implement
// them in the processor function using the context.
//
// # Usage Examples
//
// Basic Worker Pool:
//
//	type Job struct {
//	    ID   int
//	    Data string
//	}
//
//	// Create pool
//	pool := worker.NewPool[Job](
//	    5,     // 5 workers
//	    100,   // queue holds 100 jobs
//	    func(ctx context.Context, job Job) error {
//	        // Process job
//	        log.Printf("Processing job %d: %s", job.ID, job.Data)
//	        return nil
//	    },
//	)
//
//	// Start pool
//	ctx := context.Background()
//	if err := pool.Start(ctx); err != nil {
//	    log.Fatal(err)
//	}
//	defer pool.Stop(5 * time.Second)
//
//	// Submit work
//	for i := 0; i < 1000; i++ {
//	    job := Job{ID: i, Data: fmt.Sprintf("task-%d", i)}
//	    if err := pool.Submit(job); err != nil {
//	        if errors.Is(err, worker.ErrQueueFull) {
//	            // Queue full - implement backoff or reject request
//	            log.Printf("Queue full, dropping job %d", i)
//	        }
//	    }
//	}
//
// With Prometheus Metrics:
//
//	import "github.com/c360/semstreams/metric"
//
//	registry := metric.NewMetricsRegistry()
//
//	pool := worker.NewPool[Job](
//	    10, 1000, processJob,
//	    worker.WithMetricsRegistry[Job](registry, "message_processor"),
//	)
//
//	// Metrics exposed:
//	// - message_processor_queue_depth (current queue depth)
//	// - message_processor_utilization (queue depth / queue size)
//	// - message_processor_submitted_total (total submitted)
//	// - message_processor_processed_total (total processed)
//	// - message_processor_failed_total (total failed)
//	// - message_processor_dropped_total (total dropped when queue full)
//	// - message_processor_processing_duration_seconds (histogram by status)
//
// Graceful Shutdown:
//
//	// Create context with timeout for shutdown
//	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
//	defer cancel()
//
//	pool.Start(ctx)
//
//	// ... submit work ...
//
//	// Graceful shutdown: wait up to 10 seconds for workers to finish
//	if err := pool.Stop(10 * time.Second); err != nil {
//	    if errors.Is(err, worker.ErrStopTimeout) {
//	        log.Println("Workers didn't finish in time")
//	    }
//	}
//
// # Performance Characteristics
//
// Throughput:
//
// Throughput is primarily limited by:
//  1. Processor function speed (work processing time)
//  2. Worker count (parallelism)
//  3. Queue size (buffer for bursty traffic)
//
// Typical performance:
//   - Submit: ~1μs (atomic increment + channel send)
//   - Process: Depends on processor function
//   - Metrics update: ~100ns (atomic operations)
//   - Queue depth check: O(1) with 1-second granularity
//
// Memory:
//
// Memory usage is bounded and predictable:
//   - Workers: O(workers) - one goroutine per worker + stack (~8KB each)
//   - Queue: O(queueSize * sizeof(T)) - buffered channel allocation
//   - Statistics: O(1) - 6 atomic int64 counters (48 bytes)
//   - Metrics: O(1) - Prometheus metric objects (negligible)
//
// For example, a pool with 10 workers and 1000-item queue of 100-byte structs:
//   - Workers: ~80KB (10 goroutines)
//   - Queue: ~100KB (1000 items * 100 bytes)
//   - Total: ~180KB (fixed, regardless of load)
//
// Latency:
//
// Submit latency is consistently low (~1μs) until queue fills:
//   - Queue has space: Single channel send
//   - Queue full: Returns ErrQueueFull immediately
//
// Processing latency depends entirely on the processor function.
//
// # Thread Safety
//
// All public methods are safe for concurrent use:
//
//   - Submit(): Lock-free using channel semantics
//   - Start(): Protected by lifecycleMu mutex
//   - Stop(): Protected by lifecycleMu mutex
//   - Stats(): Atomic loads, no locks required
//
// Internal worker goroutines safely share:
//   - workChan: Read-only after Start()
//   - processor: Read-only, no mutations
//   - Statistics: Atomic operations (AddInt64, LoadInt64)
//   - Metrics: Thread-safe by Prometheus design
//
// Lifecycle guarantees:
//   - Start() can only be called once
//   - Submit() fails if not started or already stopped
//   - Stop() is idempotent (safe to call multiple times)
//   - Workers complete in-flight work before exiting
//
// # Integration with Framework
//
// The worker package integrates with the StreamKit framework:
//
// Metrics Integration:
//
//	import "github.com/c360/semstreams/metric"
//
//	registry := metric.NewMetricsRegistry()
//	pool := worker.NewPool[T](
//	    workers, queueSize, processor,
//	    worker.WithMetricsRegistry[T](registry, "my_pool"),
//	)
//
// All metrics are automatically registered with the framework's registry and
// exposed via the standard metrics endpoint.
//
// Error Handling:
//
// The worker package uses standard errors (not the framework's error classification)
// because worker pool errors are always programming errors or resource exhaustion:
//
//   - ErrPoolNotStarted: Programming error (Submit before Start)
//   - ErrPoolAlreadyStarted: Programming error (Start called twice)
//   - ErrPoolStopped: Expected after Stop() called
//   - ErrQueueFull: Resource exhaustion (backpressure signal)
//   - ErrNilProcessor: Programming error (validation failure)
//   - ErrStopTimeout: Resource exhaustion (workers stuck)
//
// Processor functions can return framework-classified errors (Fatal, Transient, Invalid)
// and the worker pool will track them in the failed counter, but doesn't interpret them.
//
// Context Pattern:
//
// Workers follow the framework's context pattern:
//   - Context passed as first parameter to Start()
//   - Same context passed to processor function
//   - Context cancellation triggers clean shutdown
//   - Workers exit when context cancelled OR channel closed
//
// # Common Patterns
//
// Rate Limiting:
//
//	// Limit submit rate using time.Ticker
//	ticker := time.NewTicker(time.Millisecond * 10) // Max 100/second
//	defer ticker.Stop()
//
//	for _, job := range jobs {
//	    <-ticker.C // Wait for rate limit
//	    if err := pool.Submit(job); err != nil {
//	        // Handle error
//	    }
//	}
//
// Retry on Queue Full:
//
//	import "github.com/c360/semstreams/pkg/retry"
//
//	cfg := retry.Quick() // Fast retries with exponential backoff
//	err := retry.Do(ctx, cfg, func() error {
//	    return pool.Submit(job)
//	})
//
// Dynamic Scaling (Not Supported):
//
// This implementation does NOT support dynamic worker scaling. Worker count is
// fixed at pool creation. This is intentional:
//   - Predictable resource usage
//   - Simpler implementation (no worker lifecycle management)
//   - Avoids complexity of work stealing, load balancing
//
// If you need dynamic scaling, create multiple pools or use separate goroutines.
//
// # Testing
//
// The worker package is tested with:
//   - Unit tests: Basic functionality, error handling
//   - Race detector: All tests pass with -race flag
//   - Integration tests: Real work processing, metrics, shutdown
//   - Benchmark tests: Throughput and latency measurements
//
// Run tests with race detector:
//
//	go test -race ./worker
//
// # Known Limitations
//
//  1. No per-work-item timeout: Implement in processor function
//  2. No priority queues: All work processed FIFO
//  3. No work cancellation: Can't cancel individual queued items
//  4. Queue depth metrics: 1-second granularity (ticker-based)
//  5. No dynamic worker scaling: Worker count is fixed
//
// These are design decisions, not bugs. The package prioritizes simplicity,
// predictability, and correctness over feature richness.
//
// # See Also
//
//   - buffer package: For buffering with overflow policies
//   - retry package: For retry logic with exponential backoff
//   - metric package: For framework metrics integration
package worker
