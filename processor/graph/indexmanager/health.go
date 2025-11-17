// Package indexmanager provides health and metrics types
package indexmanager

import "time"

// IndexHealth represents the health status of all indexes
type IndexHealth struct {
	IsHealthy      bool            `json:"is_healthy"`
	LastUpdate     time.Time       `json:"last_update"`
	ProcessingLag  time.Duration   `json:"processing_lag"`
	IndexStatuses  map[string]bool `json:"index_statuses"`
	ErrorCount     int64           `json:"error_count"`
	LastError      string          `json:"last_error"`
	WatchersActive bool            `json:"watchers_active"`
	BacklogSize    int             `json:"backlog_size"`
}

// Metrics holds metrics for the IndexManager
type Metrics struct {
	// Event processing
	EventsTotal       int64   `json:"events_total"`
	EventsProcessed   int64   `json:"events_processed"`
	EventsFailed      int64   `json:"events_failed"`
	EventsDropped     int64   `json:"events_dropped"`
	ProcessLatencyP95 float64 `json:"process_latency_p95"`

	// Deduplication
	DuplicateEvents   int64   `json:"duplicate_events"`
	DeduplicationRate float64 `json:"deduplication_rate"`

	// Index operations
	IndexUpdatesTotal     int64   `json:"index_updates_total"`
	IndexUpdatesFailed    int64   `json:"index_updates_failed"`
	IndexUpdateLatencyP95 float64 `json:"index_update_latency_p95"`

	// Queries
	QueriesTotal    int64   `json:"queries_total"`
	QueriesFailed   int64   `json:"queries_failed"`
	QueryLatencyP95 float64 `json:"query_latency_p95"`

	// Buffer stats
	BufferSize        int     `json:"buffer_size"`
	BufferUtilization float64 `json:"buffer_utilization"`
	BacklogSize       int     `json:"backlog_size"`

	// Health
	IsHealthy     bool          `json:"is_healthy"`
	LastError     string        `json:"last_error"`
	LastSuccess   time.Time     `json:"last_success"`
	ProcessingLag time.Duration `json:"processing_lag"`
}
