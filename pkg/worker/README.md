# Worker

Generic worker pool implementation with Go generics, graceful shutdown, queue management, and comprehensive Prometheus metrics integration.

## Overview

The worker package provides a high-performance, thread-safe worker pool implementation designed for concurrent processing of tasks in distributed systems. It uses Go generics to support any work item type, includes comprehensive lifecycle management, and provides detailed operational metrics for monitoring and alerting.

The worker pool implements proper graceful shutdown semantics, ensuring all queued work is processed before termination. It includes configurable queue sizes, worker counts, and supports both blocking and non-blocking work submission patterns with automatic load shedding when queues are full.

The package integrates with Prometheus for comprehensive operational visibility including queue depth, worker utilization, processing time distributions, success/failure rates, and throughput metrics.

## Installation

```go
import "github.com/c360/semstreams/pkg/worker"
```

## core Concepts

### Generic Worker Pool

Type-safe worker pool using Go generics that can process any work item type with configurable worker count, queue size, and custom processing functions for flexible concurrent task execution.

### Graceful Shutdown

Proper lifecycle management with graceful shutdown that drains work queues, waits for workers to complete current tasks, and provides timeout protection against stuck workers.

### Queue Management

Buffered work queue with configurable size, non-blocking submission with load shedding when full, and real-time queue depth monitoring for operational visibility.

### Comprehensive Metrics

Prometheus integration tracking queue utilization, processing time distributions, success/failure rates, throughput statistics, and dropped work counts for complete operational monitoring.

## Usage

### Basic Example

```go
import (
    "context"
    "log"
    "github.com/c360/semstreams/pkg/worker"
)

// Define work item type
type Task struct {
    ID      int
    Payload string
}

// Create processing function
processor := func(ctx context.Context, task Task) error {
    log.Printf("Processing task %d: %s", task.ID, task.Payload)
    // Simulate work
    time.Sleep(100 * time.Millisecond)
    return nil
}

// Create worker pool
pool := worker.NewPool(5, 100, processor) // 5 workers, queue size 100

// Start the pool
ctx := context.Background()
if err := pool.Start(ctx); err != nil {
    log.Printf("Failed to start pool: %v", err)
}

// Submit work
for i := 0; i < 50; i++ {
    task := Task{ID: i, Payload: fmt.Sprintf("task-%d", i)}
    if err := pool.Submit(task); err != nil {
        log.Printf("Failed to submit task: %v", err)
    }
}

// Graceful shutdown
if err := pool.Stop(); err != nil {
    log.Printf("Error during shutdown: %v", err)
}
```

### Advanced Usage - With Metrics and Monitoring

```go
// Create worker pool with Prometheus metrics
pool := worker.NewPoolWithMetrics(10, 1000, processor, "message_processor")

// Register metrics with your metrics registry
registry.RegisterGauge("message-processor", "queue_depth", pool.metrics.queueDepth)
registry.RegisterGauge("message-processor", "utilization", pool.metrics.utilization)
registry.RegisterCounter("message-processor", "processed", pool.metrics.processed)
registry.RegisterHistogram("message-processor", "processing_time", pool.metrics.processingTime)

// Start pool with context
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

if err := pool.Start(ctx); err != nil {
    log.Fatalf("Failed to start pool: %v", err)
}

// Submit work with error handling
for i := 0; i < 1000; i++ {
    work := ProcessingTask{Data: generateData(i)}

    if err := pool.Submit(work); err != nil {
        // Queue is full - implement backpressure
        time.Sleep(10 * time.Millisecond)
        // Retry or handle dropped work
        log.Printf("Work dropped due to full queue: %v", err)
    }
}

// Monitor statistics
go func() {
    ticker := time.NewTicker(10 * time.Second)
    defer ticker.Stop()

    for range ticker.C {
        stats := pool.Stats()
        log.Printf("Pool stats - Queue: %d/%d, Processed: %d, Failed: %d, Dropped: %d",
            stats.QueueDepth, stats.QueueSize, stats.Processed, stats.Failed, stats.Dropped)
    }
}()

// Graceful shutdown with timeout
shutdownTimeout := 30 * time.Second
shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
defer shutdownCancel()

if err := pool.Stop(); err != nil {
    log.Printf("Pool shutdown error: %v", err)
}
```

## API Reference

### Types

#### `Pool[T any]`

Generic worker pool that processes work items of type T.

```go
type Pool[T any] struct {
    // private fields for thread safety and lifecycle management
}

func NewPool[T any](workers, queueSize int, processor func(context.Context, T) error) *Pool[T]
func NewPoolWithMetrics[T any](workers, queueSize int, processor func(context.Context, T) error, metricsPrefix string) *Pool[T]

// Lifecycle management
func (p *Pool[T]) Start(ctx context.Context) error    // Start worker goroutines
func (p *Pool[T]) Stop() error                       // Graceful shutdown with queue drain
func (p *Pool[T]) Submit(work T) error               // Submit work item (non-blocking)
func (p *Pool[T]) Stats() PoolStats                  // Get current statistics
```

#### `Metrics`

Prometheus metrics for worker pool monitoring.

```go
type Metrics struct {
    queueDepth     prometheus.Gauge            // Current queue depth
    utilization    prometheus.Gauge            // Queue utilization (0-1)
    submitted      prometheus.Counter          // Total work items submitted
    processed      prometheus.Counter          // Total work items processed
    failed         prometheus.Counter          // Total processing failures
    dropped        prometheus.Counter          // Total items dropped (queue full)
    processingTime *prometheus.HistogramVec    // Processing time distribution by status
}
```

#### `PoolStats`

Runtime statistics for the worker pool.

```go
type PoolStats struct {
    Workers    int   `json:"workers"`      // Number of worker goroutines
    QueueSize  int   `json:"queue_size"`   // Maximum queue capacity
    QueueDepth int   `json:"queue_depth"`  // Current items in queue
    Submitted  int64 `json:"submitted"`    // Total items submitted
    Processed  int64 `json:"processed"`    // Total items processed
    Failed     int64 `json:"failed"`       // Total processing failures
    Dropped    int64 `json:"dropped"`      // Total items dropped due to full queue
}
```

### Functions

#### `NewPool[T any](workers, queueSize int, processor func(context.Context, T) error) *Pool[T]`

Creates a new worker pool without metrics. Uses default values for zero workers (10) and zero queueSize (1000).

#### `NewPoolWithMetrics[T any](workers, queueSize int, processor func(context.Context, T) error, metricsPrefix string) *Pool[T]`

Creates a worker pool with Prometheus metrics using the specified metric name prefix.

## Architecture

### Design Decisions

**Go Generics for Type Safety**: Used Go 1.18+ generics to create type-safe worker pools without any boxing overhead or runtime type assertions.

- Trade-off: Gained compile-time type safety and performance but requires Go 1.18+
- Alternative considered: Interface-based approach (would require type assertions and reduce type safety)

**Non-Blocking Submit with Load Shedding**: Chose non-blocking submit that drops work when queue is full rather than blocking submission to prevent cascading backpressure.

- Rationale: Prevents upstream systems from blocking when worker pool is overwhelmed
- Trade-off: Gained system resilience but requires application-level handling of dropped work

**Graceful Shutdown with Timeout**: Implemented graceful shutdown that drains queues but includes timeout protection against stuck workers.

- Chose graceful drain over immediate termination because work completion is often important
- Trade-off: Gained work completion guarantees but added shutdown complexity

### Integration Points

- **Dependencies**: Prometheus for metrics, Go 1.18+ for generics
- **Used By**: Input components for message processing, service layer for concurrent task execution
- **Data Flow**: `Work Submission → Queue → Worker Selection → Processing → Statistics Update → Metrics Recording`

## Configuration

### Worker Pool Sizing

```yaml
# Worker pool configuration examples
worker_pools:
  message_processor:
    workers: 10          # CPU-bound: # of core s
    queue_size: 1000     # 2-10x workers for buffering

  network_io:
    workers: 50          # I/O-bound: higher worker count
    queue_size: 5000     # Larger queue for bursty traffic

  batch_processor:
    workers: 5           # Memory-intensive: fewer workers
    queue_size: 100      # Smaller queue to limit memory usage
```

### Metrics Configuration

```yaml
# Prometheus metrics configuration
metrics:
  worker_pools:
    prefixes:
      - "message_processor"
      - "batch_processor"
      - "network_handler"
    update_interval: "1s"    # Metrics update frequency
```

## Error Handling

### Work Processing Errors

```go
// Processor function with error handling
processor := func(ctx context.Context, work MyWork) error {
    // Check context cancellation
    if ctx.Err() != nil {
        return ctx.Err()
    }

    // Process work with timeout
    processCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
    defer cancel()

    if err := processWork(processCtx, work); err != nil {
        // Log error but don't panic - pool continues running
        log.Printf("Work processing failed: %v", err)
        return err // Counted in failure metrics
    }

    return nil
}
```

### Queue Full Handling

```go
// Handle queue full scenarios
for _, work := range workItems {
    if err := pool.Submit(work); err != nil {
        if strings.Contains(err.Error(), "queue is full") {
            // Implement backpressure strategy
            select {
            case <-time.After(10 * time.Millisecond):
                // Brief backoff, then retry
                continue
            case <-ctx.Done():
                return ctx.Err()
            }
        } else {
            // Other submission error
            return fmt.Errorf("work submission failed: %w", err)
        }
    }
    break // Success
}
```

### Lifecycle Error Handling

```go
// Proper pool lifecycle management
func useWorkerPool() error {
    pool := worker.NewPool(5, 100, processor)

    if err := pool.Start(ctx); err != nil {
        return fmt.Errorf("failed to start worker pool: %w", err)
    }

    // Ensure cleanup on any exit path
    defer func() {
        if err := pool.Stop(); err != nil {
            log.Printf("Error during pool shutdown: %v", err)
        }
    }()

    // Use pool...
    return nil
}
```

### Best Practices

```go
// DO: Use appropriate worker count for workload type
cpuBoundWorkers := runtime.NumCPU()           // CPU-intensive tasks
ioBoundWorkers := runtime.NumCPU() * 4        // I/O-intensive tasks

// DO: Size queue appropriately for traffic patterns
steadyQueue := workers * 2                    // Steady load
burstyQueue := workers * 10                   // Bursty traffic

// DO: Handle context cancellation in processor
processor := func(ctx context.Context, work Work) error {
    select {
    case <-ctx.Done():
        return ctx.Err()
    default:
        return processWork(ctx, work)
    }
}

// DO: Monitor pool statistics
stats := pool.Stats()
if float64(stats.QueueDepth)/float64(stats.QueueSize) > 0.8 {
    log.Printf("Queue utilization high: %d/%d", stats.QueueDepth, stats.QueueSize)
}

// DON'T: Block in processor function
processor := func(ctx context.Context, work Work) error {
    // Don't do this - blocks other work
    time.Sleep(1 * time.Hour)
    return nil
}

// DON'T: Ignore submission errors
pool.Submit(work) // Missing error check - work may be dropped
```

## Testing

### Test Utilities

```go
// Test worker pool behavior
func TestWorkerPool(t *testing.T) {
    processed := make(chan int, 100)

    processor := func(ctx context.Context, work int) error {
        processed <- work
        return nil
    }

    pool := worker.NewPool(2, 10, processor)

    ctx := context.Background()
    err := pool.Start(ctx)
    require.NoError(t, err)
    defer pool.Stop()

    // Submit work
    for i := 0; i < 5; i++ {
        err := pool.Submit(i)
        require.NoError(t, err)
    }

    // Verify processing
    timeout := time.After(1 * time.Second)
    processedCount := 0

    for processedCount < 5 {
        select {
        case <-processed:
            processedCount++
        case <-timeout:
            t.Fatalf("Timeout waiting for work processing")
        }
    }

    // Check statistics
    stats := pool.Stats()
    assert.Equal(t, 5, int(stats.Submitted))
    assert.Equal(t, 5, int(stats.Processed))
    assert.Equal(t, 0, int(stats.Failed))
}

// Test graceful shutdown
func TestGracefulShutdown(t *testing.T) {
    processing := make(chan struct{})
    canComplete := make(chan struct{})

    processor := func(ctx context.Context, work int) error {
        processing <- struct{}{}
        <-canComplete // Wait for signal to complete
        return nil
    }

    pool := worker.NewPool(1, 10, processor)
    pool.Start(context.Background())

    // Submit work
    pool.Submit(1)

    // Wait for processing to start
    <-processing

    // Start shutdown in background
    shutdownDone := make(chan error)
    go func() {
        shutdownDone <- pool.Stop()
    }()

    // Verify shutdown waits for completion
    select {
    case <-shutdownDone:
        t.Fatal("Shutdown completed before work finished")
    case <-time.After(100 * time.Millisecond):
        // Expected - shutdown is waiting
    }

    // Allow work to complete
    close(canComplete)

    // Verify shutdown completes
    select {
    case err := <-shutdownDone:
        assert.NoError(t, err)
    case <-time.After(1 * time.Second):
        t.Fatal("Shutdown did not complete")
    }
}
```

### Testing Patterns

- Test both successful and error processing scenarios
- Verify graceful shutdown behavior with pending work
- Test queue full scenarios and load shedding
- Use channels for synchronization in tests, not time-based waits
- Test metrics recording with mock Prometheus metrics

## Performance Considerations

- **Memory Usage**: Linear with queue size and work item size - monitor queue depth to prevent memory growth
- **CPU Overhead**: Worker goroutines have minimal overhead (~2KB stack each), atomic operations for statistics
- **Lock Contention**: Uses atomic operations for statistics, single mutex only for lifecycle management
- **Queue Throughput**: Buffered channel provides high throughput with configurable backpressure behavior

## Examples

### Example 1: Message Processing Pipeline

```go
package main

import (
    "context"
    "encoding/json"
    "log"
    "time"

    "github.com/c360/semstreams/pkg/worker"
)

type Message struct {
    ID      string          `json:"id"`
    Type    string          `json:"type"`
    Payload json.RawMessage `json:"payload"`
    Source  string          `json:"source"`
}

type MessageProcessor struct {
    pool *worker.Pool[Message]
}

func NewMessageProcessor() *MessageProcessor {
    processor := func(ctx context.Context, msg Message) error {
        return processMessage(ctx, msg)
    }

    pool := worker.NewPoolWithMetrics(
        8,    // workers
        2000, // queue size
        processor,
        "message_processor",
    )

    return &MessageProcessor{pool: pool}
}

func (mp *MessageProcessor) Start(ctx context.Context) error {
    return mp.pool.Start(ctx)
}

func (mp *MessageProcessor) Stop() error {
    return mp.pool.Stop()
}

func (mp *MessageProcessor) ProcessMessage(msg Message) error {
    return mp.pool.Submit(msg)
}

func (mp *MessageProcessor) Stats() worker.PoolStats {
    return mp.pool.Stats()
}

func processMessage(ctx context.Context, msg Message) error {
    log.Printf("Processing message: %s (type: %s) from %s", msg.ID, msg.Type, msg.Source)

    // Simulate message processing
    switch msg.Type {
    case "telemetry":
        return processTelemetry(ctx, msg.Payload)
    case "command":
        return processCommand(ctx, msg.Payload)
    case "status":
        return processStatus(ctx, msg.Payload)
    default:
        log.Printf("Unknown message type: %s", msg.Type)
        return nil
    }
}

func processTelemetry(ctx context.Context, payload json.RawMessage) error {
    // Simulate telemetry processing
    time.Sleep(50 * time.Millisecond)
    log.Printf("Telemetry processed: %s", string(payload))
    return nil
}

func processCommand(ctx context.Context, payload json.RawMessage) error {
    // Simulate command processing
    time.Sleep(100 * time.Millisecond)
    log.Printf("Command processed: %s", string(payload))
    return nil
}

func processStatus(ctx context.Context, payload json.RawMessage) error {
    // Simulate status processing
    time.Sleep(25 * time.Millisecond)
    log.Printf("Status processed: %s", string(payload))
    return nil
}

func main() {
    processor := NewMessageProcessor()

    ctx := context.Background()
    if err := processor.Start(ctx); err != nil {
        log.Fatalf("Failed to start message processor: %v", err)
    }
    defer processor.Stop()

    // Simulate message flow
    messages := []Message{
        {ID: "msg-1", Type: "telemetry", Payload: json.RawMessage(`{"temperature": 22.5}`), Source: "sensor-1"},
        {ID: "msg-2", Type: "command", Payload: json.RawMessage(`{"action": "calibrate"}`), Source: "control-system"},
        {ID: "msg-3", Type: "status", Payload: json.RawMessage(`{"online": true}`), Source: "device-1"},
        {ID: "msg-4", Type: "telemetry", Payload: json.RawMessage(`{"pressure": 1013.25}`), Source: "sensor-2"},
    }

    // Submit messages for processing
    for _, msg := range messages {
        if err := processor.ProcessMessage(msg); err != nil {
            log.Printf("Failed to submit message %s: %v", msg.ID, err)
        }
    }

    // Monitor processing
    go func() {
        ticker := time.NewTicker(2 * time.Second)
        defer ticker.Stop()

        for range ticker.C {
            stats := processor.Stats()
            log.Printf("Processing stats - Queue: %d/%d, Processed: %d, Failed: %d",
                stats.QueueDepth, stats.QueueSize, stats.Processed, stats.Failed)
        }
    }()

    // Let processing complete
    time.Sleep(5 * time.Second)

    log.Printf("Message processing demo completed")
}
```

### Example 2: Batch Data Processor

```go
package main

import (
    "context"
    "fmt"
    "log"
    "math/rand"
    "sync"
    "time"

    "github.com/c360/semstreams/pkg/worker"
)

type BatchJob struct {
    ID       string
    Data     []DataRecord
    Priority int
}

type DataRecord struct {
    Timestamp time.Time `json:"timestamp"`
    Value     float64   `json:"value"`
    Source    string    `json:"source"`
}

type BatchProcessor struct {
    pool    *worker.Pool[BatchJob]
    results sync.Map // Store processing results
}

func NewBatchProcessor() *BatchProcessor {
    processor := func(ctx context.Context, job BatchJob) error {
        return processBatch(ctx, job)
    }

    // Use fewer workers for memory-intensive batch processing
    pool := worker.NewPoolWithMetrics(
        4,   // workers (memory-intensive work)
        50,  // smaller queue to limit memory usage
        processor,
        "batch_processor",
    )

    return &BatchProcessor{pool: pool}
}

func (bp *BatchProcessor) Start(ctx context.Context) error {
    return bp.pool.Start(ctx)
}

func (bp *BatchProcessor) Stop() error {
    return bp.pool.Stop()
}

func (bp *BatchProcessor) SubmitBatch(job BatchJob) error {
    return bp.pool.Submit(job)
}

func (bp *BatchProcessor) GetResult(jobID string) (any, bool) {
    return bp.results.Load(jobID)
}

func (bp *BatchProcessor) Stats() worker.PoolStats {
    return bp.pool.Stats()
}

func processBatch(ctx context.Context, job BatchJob) error {
    log.Printf("Processing batch job: %s with %d records", job.ID, len(job.Data))

    start := time.Now()

    // Simulate batch processing
    var sum float64
    var count int

    for _, record := range job.Data {
        // Check for cancellation during processing
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
        }

        // Simulate processing time
        time.Sleep(time.Duration(rand.Intn(10)) * time.Millisecond)

        sum += record.Value
        count++
    }

    result := map[string]any{
        "job_id":       job.ID,
        "record_count": count,
        "average":      sum / float64(count),
        "sum":          sum,
        "processing_time": time.Since(start),
    }

    // Store result for retrieval
    bp := getBatchProcessor() // Get from context or global
    bp.results.Store(job.ID, result)

    log.Printf("Completed batch job: %s in %v", job.ID, time.Since(start))
    return nil
}

// Global reference for example (in real code, use proper dependency injection)
var globalBP *BatchProcessor

func getBatchProcessor() *BatchProcessor {
    return globalBP
}

func main() {
    processor := NewBatchProcessor()
    globalBP = processor // For example purposes

    ctx := context.Background()
    if err := processor.Start(ctx); err != nil {
        log.Fatalf("Failed to start batch processor: %v", err)
    }
    defer processor.Stop()

    // Generate sample batch jobs
    jobs := make([]BatchJob, 10)
    for i := 0; i < 10; i++ {
        // Create sample data
        data := make([]DataRecord, rand.Intn(100)+50) // 50-150 records
        for j := range data {
            data[j] = DataRecord{
                Timestamp: time.Now().Add(time.Duration(j) * time.Second),
                Value:     rand.Float64() * 100,
                Source:    fmt.Sprintf("sensor-%d", rand.Intn(10)),
            }
        }

        jobs[i] = BatchJob{
            ID:       fmt.Sprintf("batch-%d", i),
            Data:     data,
            Priority: rand.Intn(5),
        }
    }

    // Submit jobs for processing
    for _, job := range jobs {
        if err := processor.SubmitBatch(job); err != nil {
            log.Printf("Failed to submit batch job %s: %v", job.ID, err)
            continue
        }
        log.Printf("Submitted batch job: %s", job.ID)
    }

    // Monitor processing progress
    go func() {
        ticker := time.NewTicker(3 * time.Second)
        defer ticker.Stop()

        for range ticker.C {
            stats := processor.Stats()
            log.Printf("Batch processing stats - Queue: %d/%d, Processed: %d, Failed: %d",
                stats.QueueDepth, stats.QueueSize, stats.Processed, stats.Failed)

            // Check for completed results
            completedCount := 0
            processor.results.Range(func(key, value any) bool {
                completedCount++
                return true
            })
            log.Printf("Completed jobs: %d/%d", completedCount, len(jobs))
        }
    }()

    // Wait for all jobs to complete
    for {
        stats := processor.Stats()
        if stats.Processed >= int64(len(jobs)) {
            break
        }
        time.Sleep(1 * time.Second)
    }

    // Display results
    log.Printf("All batch jobs completed. Results:")
    processor.results.Range(func(key, value any) bool {
        result := value.(map[string]any)
        log.Printf("Job %s: %d records, average=%.2f, processing_time=%v",
            result["job_id"], result["record_count"], result["average"], result["processing_time"])
        return true
    })

    log.Printf("Batch processing demo completed")
}
```

## Related Packages

- [`pkg/metric`](../metric): Metrics registry for worker pool monitoring integration
- [`pkg/component`](../component): Component framework using worker pools for concurrent processing
- [`pkg/service`](../service): Service framework with worker pool integration for background tasks
- [`pkg/errors`](../errors): Error handling patterns for processor function implementations

## License

MIT
