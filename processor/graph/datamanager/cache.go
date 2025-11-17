// Package datamanager consolidates entity and edge operations into a unified data management service.
package datamanager

import "time"

// L1CacheConfig configures the L1 (hot) LRU cache
type L1CacheConfig struct {
	Type      string `default:"lru"`
	Size      int    `default:"1000"`
	Metrics   bool   `default:"true"`
	Component string `default:"entity_l1"`
}

// L2CacheConfig configures the L2 (warm) TTL cache
type L2CacheConfig struct {
	Type            string        `default:"ttl"`
	Size            int           `default:"10000"`
	TTL             time.Duration `default:"5m"`
	CleanupInterval time.Duration `default:"1m"`
	Metrics         bool          `default:"true"`
	Component       string        `default:"entity_l2"`
}

// CacheStats holds cache statistics
type CacheStats struct {
	// L1 Stats
	L1Hits      int64   `json:"l1_hits"`
	L1Misses    int64   `json:"l1_misses"`
	L1Size      int     `json:"l1_size"`
	L1HitRatio  float64 `json:"l1_hit_ratio"`
	L1Evictions int64   `json:"l1_evictions"`

	// L2 Stats
	L2Hits      int64   `json:"l2_hits"`
	L2Misses    int64   `json:"l2_misses"`
	L2Size      int     `json:"l2_size"`
	L2HitRatio  float64 `json:"l2_hit_ratio"`
	L2Evictions int64   `json:"l2_evictions"`

	// Overall Stats
	TotalHits       int64   `json:"total_hits"`
	TotalMisses     int64   `json:"total_misses"`
	OverallHitRatio float64 `json:"overall_hit_ratio"`
	KVFetches       int64   `json:"kv_fetches"`

	// Invalidation Stats
	InvalidationsTotal   int64 `json:"invalidations_total"`
	InvalidationsBatched int64 `json:"invalidations_batched"`
}

// EntityIndexStatus represents index consistency status
type EntityIndexStatus struct {
	OutgoingEdgesConsistent bool     `json:"outgoing_edges_consistent"`
	InconsistentEdges       []string `json:"inconsistent_edges,omitempty"`
}
