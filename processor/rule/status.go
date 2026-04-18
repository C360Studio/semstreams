package rule

import "time"

// Status represents the current status of rule evaluation for debug observability
type Status struct {
	DebounceDelayMs    int       `json:"debounce_delay_ms"`
	PendingEvaluations int       `json:"pending_evaluations"`
	TotalEvaluations   int       `json:"total_evaluations"`
	TotalTriggers      int       `json:"total_triggers"`
	DebouncedCount     int       `json:"debounced_count"` // Matches Tester's test expectations
	RulesLoaded        int       `json:"rules_loaded"`
	LastEvaluationTime time.Time `json:"last_evaluation_time,omitempty"`

	// TrackedRevisions is the total number of (rule,entity,revision) tuples
	// currently in the feedback-loop-prevention map. Monitor this to detect
	// leaks from writes that the watcher never delivers — a healthy
	// processor's count should stay roughly proportional to in-flight rule
	// actions, bounded by the sweeper.
	TrackedRevisions int `json:"tracked_revisions"`
}
