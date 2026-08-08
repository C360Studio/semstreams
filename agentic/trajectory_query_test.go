package agentic

import (
	"encoding/json"
	"testing"
	"time"
)

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

func TestTrajectoryPageCarriesOnlyFactMetadataReferencesAndContinuation(t *testing.T) {
	page := TrajectoryPage{
		SchemaVersion:    TrajectorySchemaV1,
		LoopID:           "loop-1",
		Coverage:         "observed",
		TerminalObserved: false,
		Facts: []TrajectoryFactV1{{
			SchemaVersion:   TrajectorySchemaV1,
			LoopDigest:      TrajectoryLoopDigest("loop-1"),
			AttemptID:       "attempt1",
			AttemptOrdinal:  1,
			Kind:            TrajectoryKindLoopStarted,
			CausalPhase:     TrajectoryPhaseLoopStart,
			ObservedAt:      mustTrajectoryQueryTime(t, "2026-08-07T00:00:00Z"),
			EvidenceCapture: TrajectoryEvidenceNone,
		}},
		NextCursor: "opaque",
	}
	encoded, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	var shape map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &shape); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"schema_version", "loop_id", "coverage", "observed_totals", "terminal_observed", "facts", "next_cursor"} {
		if _, ok := shape[key]; !ok {
			t.Fatalf("TrajectoryPage missing %q: %s", key, encoded)
		}
	}
	for _, forbidden := range []string{"evidence_body", "evidence_status", "hydrateEvidence"} {
		if _, ok := shape[forbidden]; ok {
			t.Fatalf("TrajectoryPage retained %q: %s", forbidden, encoded)
		}
	}
}

func mustTrajectoryQueryTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
