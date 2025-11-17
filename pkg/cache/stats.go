package cache

import (
	"sync"
	"sync/atomic"
	"time"
)

// Statistics tracks cache performance metrics.
type Statistics struct {
	// Atomic counters for thread-safe updates
	hits      int64
	misses    int64
	sets      int64
	deletes   int64
	evictions int64

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

// Hit records a cache hit.
func (s *Statistics) Hit() {
	atomic.AddInt64(&s.hits, 1)
}

// Miss records a cache miss.
func (s *Statistics) Miss() {
	atomic.AddInt64(&s.misses, 1)
}

// Set records a cache set operation.
func (s *Statistics) Set() {
	atomic.AddInt64(&s.sets, 1)
}

// Delete records a cache delete operation.
func (s *Statistics) Delete() {
	atomic.AddInt64(&s.deletes, 1)
}

// Eviction records a cache eviction.
func (s *Statistics) Eviction() {
	atomic.AddInt64(&s.evictions, 1)
}

// UpdateSize updates the current cache size.
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

// Hits returns the total number of cache hits.
func (s *Statistics) Hits() int64 {
	return atomic.LoadInt64(&s.hits)
}

// Misses returns the total number of cache misses.
func (s *Statistics) Misses() int64 {
	return atomic.LoadInt64(&s.misses)
}

// Sets returns the total number of set operations.
func (s *Statistics) Sets() int64 {
	return atomic.LoadInt64(&s.sets)
}

// Deletes returns the total number of delete operations.
func (s *Statistics) Deletes() int64 {
	return atomic.LoadInt64(&s.deletes)
}

// Evictions returns the total number of evictions.
func (s *Statistics) Evictions() int64 {
	return atomic.LoadInt64(&s.evictions)
}

// CurrentSize returns the current number of entries in the cache.
func (s *Statistics) CurrentSize() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentSize
}

// MaxSize returns the maximum number of entries the cache has held.
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

// HitRatio returns the cache hit ratio as a percentage (0.0 to 1.0).
func (s *Statistics) HitRatio() float64 {
	hits := s.Hits()
	misses := s.Misses()
	total := hits + misses

	if total == 0 {
		return 0.0
	}

	return float64(hits) / float64(total)
}

// MissRatio returns the cache miss ratio as a percentage (0.0 to 1.0).
func (s *Statistics) MissRatio() float64 {
	return 1.0 - s.HitRatio()
}

// RequestsPerSecond returns the average number of requests (hits + misses) per second.
func (s *Statistics) RequestsPerSecond() float64 {
	s.mu.RLock()
	elapsed := time.Since(s.startTime)
	s.mu.RUnlock()

	if elapsed == 0 {
		return 0.0
	}

	totalRequests := s.Hits() + s.Misses()
	return float64(totalRequests) / elapsed.Seconds()
}

// Uptime returns how long the cache has been running.
func (s *Statistics) Uptime() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return time.Since(s.startTime)
}

// Reset resets all statistics to zero.
func (s *Statistics) Reset() {
	atomic.StoreInt64(&s.hits, 0)
	atomic.StoreInt64(&s.misses, 0)
	atomic.StoreInt64(&s.sets, 0)
	atomic.StoreInt64(&s.deletes, 0)
	atomic.StoreInt64(&s.evictions, 0)

	s.mu.Lock()
	s.startTime = time.Now()
	s.currentSize = 0
	s.maxSize = 0
	s.memoryUsage = 0
	s.mu.Unlock()
}

// StatsSummary returns a snapshot of all statistics.
type StatsSummary struct {
	Hits              int64         `json:"hits"`
	Misses            int64         `json:"misses"`
	Sets              int64         `json:"sets"`
	Deletes           int64         `json:"deletes"`
	Evictions         int64         `json:"evictions"`
	CurrentSize       int64         `json:"current_size"`
	MaxSize           int64         `json:"max_size"`
	MemoryUsage       int64         `json:"memory_usage"`
	HitRatio          float64       `json:"hit_ratio"`
	MissRatio         float64       `json:"miss_ratio"`
	RequestsPerSecond float64       `json:"requests_per_second"`
	Uptime            time.Duration `json:"uptime"`
}

// Summary returns a snapshot of all statistics.
func (s *Statistics) Summary() StatsSummary {
	return StatsSummary{
		Hits:              s.Hits(),
		Misses:            s.Misses(),
		Sets:              s.Sets(),
		Deletes:           s.Deletes(),
		Evictions:         s.Evictions(),
		CurrentSize:       s.CurrentSize(),
		MaxSize:           s.MaxSize(),
		MemoryUsage:       s.MemoryUsage(),
		HitRatio:          s.HitRatio(),
		MissRatio:         s.MissRatio(),
		RequestsPerSecond: s.RequestsPerSecond(),
		Uptime:            s.Uptime(),
	}
}
