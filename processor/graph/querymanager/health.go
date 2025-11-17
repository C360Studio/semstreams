// Package querymanager provides the Querier interface and QueryManager implementation.
package querymanager

import "time"

// Health represents the health status of the query manager
type Health struct {
	IsHealthy           bool          `json:"is_healthy"`
	LastUpdate          time.Time     `json:"last_update"`
	CacheHealth         CacheHealth   `json:"cache_health"`
	WatcherHealth       WatcherHealth `json:"watcher_health"`
	ErrorCount          int64         `json:"error_count"`
	LastError           string        `json:"last_error"`
	KVConnected         bool          `json:"kv_connected"`
	IndexManagerHealthy bool          `json:"index_manager_healthy"`
	LatencyP95          float64       `json:"latency_p95"`
}

// CacheHealth represents the health of the cache tiers
type CacheHealth struct {
	L1Healthy     bool    `json:"l1_healthy"`
	L2Healthy     bool    `json:"l2_healthy"`
	L3Healthy     bool    `json:"l3_healthy"`
	MemoryUsageMB float64 `json:"memory_usage_mb"`
}

// WatcherHealth represents the health of KV watchers
type WatcherHealth struct {
	WatchersActive bool          `json:"watchers_active"`
	WatcherCount   int           `json:"watcher_count"`
	LastWatchEvent time.Time     `json:"last_watch_event"`
	WatchLag       time.Duration `json:"watch_lag"`
	WatchErrors    int64         `json:"watch_errors"`
}

// CacheStats provides statistics on multi-tier cache performance
type CacheStats struct {
	// L1 Cache (Hot - LRU)
	L1Hits      int64   `json:"l1_hits"`
	L1Misses    int64   `json:"l1_misses"`
	L1Size      int     `json:"l1_size"`
	L1HitRatio  float64 `json:"l1_hit_ratio"`
	L1Evictions int64   `json:"l1_evictions"`

	// L2 Cache (Warm - TTL)
	L2Hits      int64   `json:"l2_hits"`
	L2Misses    int64   `json:"l2_misses"`
	L2Size      int     `json:"l2_size"`
	L2HitRatio  float64 `json:"l2_hit_ratio"`
	L2Evictions int64   `json:"l2_evictions"`
	L2Expired   int64   `json:"l2_expired"`

	// L3 Cache (Query Results - Hybrid)
	L3Hits      int64   `json:"l3_hits"`
	L3Misses    int64   `json:"l3_misses"`
	L3Size      int     `json:"l3_size"`
	L3HitRatio  float64 `json:"l3_hit_ratio"`
	L3Evictions int64   `json:"l3_evictions"`

	// Overall statistics
	TotalHits       int64   `json:"total_hits"`
	TotalMisses     int64   `json:"total_misses"`
	OverallHitRatio float64 `json:"overall_hit_ratio"`
	KVFetches       int64   `json:"kv_fetches"`

	// Invalidation statistics
	InvalidationsTotal     int64   `json:"invalidations_total"`
	InvalidationsBatched   int64   `json:"invalidations_batched"`
	InvalidationLatencyP95 float64 `json:"invalidation_latency_p95"`
}
