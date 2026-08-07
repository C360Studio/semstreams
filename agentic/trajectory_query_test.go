package agentic

import "testing"

func TestSortTrajectoryFactsUsesCausalOrderNotArrival(t *testing.T) {
	facts := []TrajectoryFactV1{
		{AttemptID: "z", AttemptOrdinal: 1, CausalIteration: 2, CausalPhase: TrajectoryPhaseModelResult, CausalOrdinal: 0},
		{AttemptID: "b", AttemptOrdinal: 4, CausalIteration: 1, CausalPhase: TrajectoryPhaseToolResult, CausalOrdinal: 2},
		{AttemptID: "a", AttemptOrdinal: 3, CausalIteration: 1, CausalPhase: TrajectoryPhaseToolResult, CausalOrdinal: 1},
	}
	SortTrajectoryFacts(facts)
	if facts[0].AttemptID != "a" || facts[1].AttemptID != "b" || facts[2].AttemptID != "z" {
		t.Fatalf("causal order = %q, %q, %q", facts[0].AttemptID, facts[1].AttemptID, facts[2].AttemptID)
	}
}
