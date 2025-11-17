package buffer

import (
	"sync"
	"sync/atomic"
	"time"
)

// Statistics tracks buffer performance metrics.
type Statistics struct {
	// Atomic counters for thread-safe updates
	writes    int64
	reads     int64
	peeks     int64
	overflows int64
	drops     int64

	// Protected by mutex
	mu          sync.RWMutex
	startTime   time.Time
	currentSize int64
	maxSize     int64
	memoryUsage int64 // Estimated memory usage in bytes
}

// NewStatistics creates a new statistics tracker.
func NewStatistics() *Statistics {
	return &Statistics{
		startTime: time.Now(),
	}
}

// Write records a buffer write operation.
func (s *Statistics) Write() {
	atomic.AddInt64(&s.writes, 1)
}

// Read records a buffer read operation.
func (s *Statistics) Read() {
	atomic.AddInt64(&s.reads, 1)
}

// Peek records a buffer peek operation.
func (s *Statistics) Peek() {
	atomic.AddInt64(&s.peeks, 1)
}

// Overflow records a buffer overflow event.
func (s *Statistics) Overflow() {
	atomic.AddInt64(&s.overflows, 1)
}

// Drop records an item drop due to overflow policy.
func (s *Statistics) Drop() {
	atomic.AddInt64(&s.drops, 1)
}

// UpdateSize updates the current buffer size.
func (s *Statistics) UpdateSize(size int64) {
	s.mu.Lock()
	s.currentSize = size
	if size > s.maxSize {
		s.maxSize = size
	}
	s.mu.Unlock()
}

// UpdateMemoryUsage updates the estimated memory usage.
func (s *Statistics) UpdateMemoryUsage(usage int64) {
	s.mu.Lock()
	s.memoryUsage = usage
	s.mu.Unlock()
}

// Writes returns the total number of write operations.
func (s *Statistics) Writes() int64 {
	return atomic.LoadInt64(&s.writes)
}

// Reads returns the total number of read operations.
func (s *Statistics) Reads() int64 {
	return atomic.LoadInt64(&s.reads)
}

// Peeks returns the total number of peek operations.
func (s *Statistics) Peeks() int64 {
	return atomic.LoadInt64(&s.peeks)
}

// Overflows returns the total number of overflow events.
func (s *Statistics) Overflows() int64 {
	return atomic.LoadInt64(&s.overflows)
}

// Drops returns the total number of dropped items.
func (s *Statistics) Drops() int64 {
	return atomic.LoadInt64(&s.drops)
}

// CurrentSize returns the current number of items in the buffer.
func (s *Statistics) CurrentSize() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentSize
}

// MaxSize returns the maximum number of items the buffer has held.
func (s *Statistics) MaxSize() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.maxSize
}

// MemoryUsage returns the estimated memory usage in bytes.
func (s *Statistics) MemoryUsage() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.memoryUsage
}

// Throughput returns the average number of writes per second.
func (s *Statistics) Throughput() float64 {
	s.mu.RLock()
	elapsed := time.Since(s.startTime)
	s.mu.RUnlock()

	if elapsed == 0 {
		return 0.0
	}

	totalWrites := s.Writes()
	return float64(totalWrites) / elapsed.Seconds()
}

// ReadThroughput returns the average number of reads per second.
func (s *Statistics) ReadThroughput() float64 {
	s.mu.RLock()
	elapsed := time.Since(s.startTime)
	s.mu.RUnlock()

	if elapsed == 0 {
		return 0.0
	}

	totalReads := s.Reads()
	return float64(totalReads) / elapsed.Seconds()
}

// DropRate returns the percentage of writes that resulted in drops (0.0 to 1.0).
func (s *Statistics) DropRate() float64 {
	writes := s.Writes()
	drops := s.Drops()

	if writes == 0 {
		return 0.0
	}

	return float64(drops) / float64(writes)
}

// OverflowRate returns the percentage of writes that caused overflows (0.0 to 1.0).
func (s *Statistics) OverflowRate() float64 {
	writes := s.Writes()
	overflows := s.Overflows()

	if writes == 0 {
		return 0.0
	}

	return float64(overflows) / float64(writes)
}

// Utilization returns the current buffer utilization as a percentage (0.0 to 1.0).
func (s *Statistics) Utilization(capacity int64) float64 {
	if capacity == 0 {
		return 0.0
	}

	currentSize := s.CurrentSize()
	return float64(currentSize) / float64(capacity)
}

// Uptime returns how long the buffer has been running.
func (s *Statistics) Uptime() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return time.Since(s.startTime)
}

// Reset resets all statistics to zero.
func (s *Statistics) Reset() {
	atomic.StoreInt64(&s.writes, 0)
	atomic.StoreInt64(&s.reads, 0)
	atomic.StoreInt64(&s.peeks, 0)
	atomic.StoreInt64(&s.overflows, 0)
	atomic.StoreInt64(&s.drops, 0)

	s.mu.Lock()
	s.startTime = time.Now()
	s.currentSize = 0
	s.maxSize = 0
	s.memoryUsage = 0
	s.mu.Unlock()
}

// StatsSummary returns a snapshot of all statistics.
type StatsSummary struct {
	Writes         int64         `json:"writes"`
	Reads          int64         `json:"reads"`
	Peeks          int64         `json:"peeks"`
	Overflows      int64         `json:"overflows"`
	Drops          int64         `json:"drops"`
	CurrentSize    int64         `json:"current_size"`
	MaxSize        int64         `json:"max_size"`
	MemoryUsage    int64         `json:"memory_usage"`
	Throughput     float64       `json:"throughput"`
	ReadThroughput float64       `json:"read_throughput"`
	DropRate       float64       `json:"drop_rate"`
	OverflowRate   float64       `json:"overflow_rate"`
	Uptime         time.Duration `json:"uptime"`
}

// Summary returns a snapshot of all statistics.
func (s *Statistics) Summary() StatsSummary {
	return StatsSummary{
		Writes:         s.Writes(),
		Reads:          s.Reads(),
		Peeks:          s.Peeks(),
		Overflows:      s.Overflows(),
		Drops:          s.Drops(),
		CurrentSize:    s.CurrentSize(),
		MaxSize:        s.MaxSize(),
		MemoryUsage:    s.MemoryUsage(),
		Throughput:     s.Throughput(),
		ReadThroughput: s.ReadThroughput(),
		DropRate:       s.DropRate(),
		OverflowRate:   s.OverflowRate(),
		Uptime:         s.Uptime(),
	}
}
