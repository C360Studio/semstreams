//go:build integration

package graphindex

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestOwnerLoadCIProfile_ContractedBudgets pins the CI profile's absolute budgets against the
// contract that authorizes production activation, so a future "flake fix" cannot relax them by
// editing a constant.
//
// ADR-077 section 8 prohibits production activation until, among other conditions, "the
// 5,000-hot-member plus 20-predicate CI guard, with each operation below 3 seconds" is green. The
// graph-index spec states the same absolute budget, and
// docs/operations/32-predicate-layout-smoke-harness.md assigns the 10-second ceiling to the
// SEPARATE 21,000-entity Decision profile — not to CI.
//
// gh#750 / PR #755: this budget was raised 3s -> 10s to stop a runner-contention flake, on the
// reasoning that it "matched the full profile". It matched the wrong profile's contract and
// silently relaxed an activation gate. Relaxing it legitimately requires an architect-reviewed
// ADR/spec change that replaces the activation evidence.
func TestOwnerLoadCIProfile_ContractedBudgets(t *testing.T) {
	ci := ownerLoadCIProfile()

	require.Equal(t, 3*time.Second, ci.operationBudget,
		"ADR-077 s8 condition 4 requires EACH operation below 3s for the CI guard; raising this "+
			"relaxes a production activation gate and needs an ADR/spec change, not a test edit")
	require.Equal(t, 3*time.Second, ci.p95Budget, "ops guide 32: CI p95 <= 3s")
	require.Equal(t, 3*time.Second, ci.p99Budget, "ops guide 32: CI p99 <= 3s")
	require.Equal(t, 5_000, ci.entities, "ADR-077 s8 condition 4 fixes the CI guard at 5,000 hot members")
	require.Equal(t, 20, ci.spread, "ADR-077 s8 condition 4 fixes the CI guard at 20 spread predicates")
}

// TestOwnerLoadPercentiles_DoNotCoverTheMax proves WHY the per-repetition operationBudget gate
// cannot be deleted in favour of the aggregate percentile gates.
//
// At repetitions=5, p95 and p99 both index (len-1)*p/100 = 3 — the second-largest of five sorted
// samples. Neither ever examines durations[4], the max. So four fast samples plus one slow one
// satisfy both percentile gates while violating the contracted per-operation budget.
func TestOwnerLoadPercentiles_DoNotCoverTheMax(t *testing.T) {
	ci := ownerLoadCIProfile()

	// Four comfortably-fast samples plus one that breaches the 3s per-operation contract.
	durations := []time.Duration{
		100 * time.Millisecond,
		120 * time.Millisecond,
		140 * time.Millisecond,
		160 * time.Millisecond,
		5 * time.Second,
	}
	require.Len(t, durations, ci.repetitions, "fixture must match the CI profile's repetition count")

	p95 := durations[(len(durations)-1)*95/100]
	p99 := durations[(len(durations)-1)*99/100]

	require.Equal(t, p95, p99, "at repetitions=5 both percentiles select the same index")
	require.LessOrEqual(t, p95, ci.p95Budget, "the 5s breach is invisible to the p95 gate")
	require.LessOrEqual(t, p99, ci.p99Budget, "the 5s breach is invisible to the p99 gate")

	// The per-repetition gate is the only thing that rejects it.
	require.Greater(t, durations[len(durations)-1], ci.operationBudget,
		"the max breaches the contracted per-operation budget, so the per-rep gate MUST remain")
}
