package agentrun

import (
	"slices"
	"testing"

	"pgregory.net/rapid"
)

// The aggregate is a decision over a SET of independently classified outcomes
// whose size and composition are open — the second shape the PBT decision names
// (docs/contributing/01-testing.md, "When to Use Property-Based Testing"). The
// examples in milestone_settlement_internal_test.go each fix one composition;
// this property searches arbitrary ones for a list whose precedence the
// implementation reads differently from the requirement.
//
// The expectation is NOT recomputed by the implementation's algorithm.
// aggregateMilestoneOutcomes takes the maximum of an ordinal; the property
// scans the drawn list for each class in the order the requirement writes them
// and takes the first class present. A defect in the ordinal's declaration
// order therefore shows up as a disagreement rather than cancelling out.
//
// Boundary coverage is by construction, not by striding: the length generator
// starts at 0 (an attempt with no registered handlers, which acknowledges) and
// the element generator is SampledFrom the whole closed outcome set, so
// "every outcome is done", "exactly one fatal, last", and "one of each" are
// ordinary draws rather than lucky ones.

// allMilestoneOutcomes is the closed set the classifier can produce.
var allMilestoneOutcomes = []milestoneOutcome{
	outcomeDone, outcomeInvalid, outcomeTransient, outcomeFatal,
}

// expectedAggregate reads the requirement directly: "any fatal outcome →
// Quarantine; else any transient outcome → Retry; else any invalid outcome →
// Terminate; else Ack".
func expectedAggregate(outcomes []milestoneOutcome) milestoneOutcome {
	for _, class := range []milestoneOutcome{outcomeFatal, outcomeTransient, outcomeInvalid} {
		if slices.Contains(outcomes, class) {
			return class
		}
	}
	return outcomeDone
}

// spec: agent-run-milestones / milestone fanout settles as one replay-safe unit
func TestMilestoneAggregateIsPureOverOrderedOutcomes(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		outcomes := rapid.SliceOfN(rapid.SampledFrom(allMilestoneOutcomes), 0, 8).
			Draw(t, "outcomes")

		got := aggregateMilestoneOutcomes(outcomes)
		if want := expectedAggregate(outcomes); got != want {
			t.Fatalf("aggregate(%v) = %d, want %d by the requirement's precedence", outcomes, got, want)
		}

		// Pure over the ORDERED list means membership decides and position does
		// not: a fatal from the last handler must weigh exactly as much as one
		// from the first.
		shuffled := rapid.Permutation(outcomes).Draw(t, "shuffled")
		if reordered := aggregateMilestoneOutcomes(shuffled); reordered != got {
			t.Fatalf("aggregate(%v) = %d but aggregate(%v) = %d — position changed the answer",
				outcomes, got, shuffled, reordered)
		}

		// An attempt that acknowledges is one where EVERY handler returned nil.
		everyOutcomeIsDone := !slices.ContainsFunc(outcomes, func(o milestoneOutcome) bool {
			return o != outcomeDone
		})
		if (got == outcomeDone) != everyOutcomeIsDone {
			t.Fatalf("aggregate(%v) = %d, but every-handler-done is %v", outcomes, got, everyOutcomeIsDone)
		}
	})
}
