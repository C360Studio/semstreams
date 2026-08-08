package agentic

import "sort"

// TrajectoryQueryRequest is the strict internal request for one observed fact
// page. Cursor is opaque to callers and must be returned verbatim.
type TrajectoryQueryRequest struct {
	LoopID string `json:"loopId"`
	Limit  int    `json:"limit,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

// TrajectoryObservedTotals summarizes only the facts returned in one page.
type TrajectoryObservedTotals struct {
	Facts                 uint64 `json:"facts"`
	TokensIn              uint64 `json:"tokens_in"`
	TokensOut             uint64 `json:"tokens_out"`
	ElapsedMS             int64  `json:"elapsed_ms"`
	MessageCount          uint64 `json:"message_count"`
	ToolCount             uint64 `json:"tool_count"`
	URLCount              uint64 `json:"url_count"`
	ModelRequests         uint64 `json:"model_requests"`
	ModelCompletions      uint64 `json:"model_completions"`
	ToolRequests          uint64 `json:"tool_requests"`
	ToolCompletions       uint64 `json:"tool_completions"`
	ContextCompactions    uint64 `json:"context_compactions"`
	TerminalObservations  uint64 `json:"terminal_observations"`
	RequestedObservations uint64 `json:"requested_observations"`
	CompletedObservations uint64 `json:"completed_observations"`
	FailedObservations    uint64 `json:"failed_observations"`
	CancelledObservations uint64 `json:"cancelled_observations"`
}

// TrajectoryPage is one observed-only page of immutable fact metadata and
// durable evidence references. It never carries evidence bodies.
type TrajectoryPage struct {
	SchemaVersion    string                   `json:"schema_version"`
	LoopID           string                   `json:"loop_id"`
	Coverage         string                   `json:"coverage"`
	ObservedTotals   TrajectoryObservedTotals `json:"observed_totals"`
	TerminalObserved bool                     `json:"terminal_observed"`
	Facts            []TrajectoryFactV1       `json:"facts"`
	NextCursor       string                   `json:"next_cursor,omitempty"`
}

// SortTrajectoryFacts orders visible observations by their causal display tuple.
func SortTrajectoryFacts(facts []TrajectoryFactV1) {
	sort.SliceStable(facts, func(i, j int) bool {
		a, b := facts[i], facts[j]
		if a.CausalIteration != b.CausalIteration {
			return a.CausalIteration < b.CausalIteration
		}
		if trajectoryPhaseRank(a.CausalPhase) != trajectoryPhaseRank(b.CausalPhase) {
			return trajectoryPhaseRank(a.CausalPhase) < trajectoryPhaseRank(b.CausalPhase)
		}
		if a.CausalOrdinal != b.CausalOrdinal {
			return a.CausalOrdinal < b.CausalOrdinal
		}
		if a.AttemptOrdinal != b.AttemptOrdinal {
			return a.AttemptOrdinal < b.AttemptOrdinal
		}
		return a.AttemptID < b.AttemptID
	})
}

func trajectoryPhaseRank(phase TrajectoryPhase) int {
	switch phase {
	case TrajectoryPhaseLoopStart:
		return 0
	case TrajectoryPhaseModelRequest:
		return 1
	case TrajectoryPhaseModelResult:
		return 2
	case TrajectoryPhaseToolRequest:
		return 3
	case TrajectoryPhaseToolResult:
		return 4
	case TrajectoryPhaseCompaction:
		return 5
	case TrajectoryPhaseTerminal:
		return 6
	default:
		return -1
	}
}
