package agentic

import "sort"

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
