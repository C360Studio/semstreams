//go:build integration

package graphindex

import (
	"reflect"
	"testing"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/require"
)

// TestOwnerLoadCIProfile_ContractedBudgets pins what the CI owner-filter profile is AFTER the
// #1284 owner ruling of 2026-09-11, so a future "flake fix" cannot quietly reintroduce the gate the
// ruling deleted, and cannot relax the percentiles that remain by editing a constant.
//
// The ruling demoted this profile to a regression guard. ADR-077 section 8 condition 4
// (docs/adr/077-bounded-owner-discovery-and-incoming-ownership.md:139) is satisfied by the
// supervised record in docs/operations/32-predicate-layout-smoke-harness.md, section "Owner-filter acceptance record",
// recorded against
// the current server and SDK pin — not by a shared-runner CI job. Its predecessor pinned
// operationBudget == 3s as a contracted activation gate; that claim is retired, and the assertion
// it guarded is replaced here rather than dropped with it.
//
// Two properties survive the move:
//
//  1. No predicted per-operation wall-clock budget may re-enter the profile under ANY name. The
//     framework-enforced KV deadline (natsclient/kv.go:39) is the single absolute ceiling on a
//     directly measured key listing, observed as the operation's own typed error. A predicted
//     budget below it fires on stalls the framework tolerates (gh#750, #1284); above it, it can
//     never fire (#1286). There is no correct third value, so the knob does not come back.
//  2. The percentile budgets that remain are checkable against a published measurement, and they
//     sit below the framework deadline, because a budget above it is unreachable by construction.
func TestOwnerLoadCIProfile_ContractedBudgets(t *testing.T) {
	ci := ownerLoadCIProfile()

	// Property 1, enforced over the CLASS rather than the retired field name: every field whose type
	// IS time.Duration must be one of the two the ruling kept. A rename — perOpBudget,
	// operationCeiling, maxOperation — trips this just as operationBudget would, and so does a
	// time.Duration alias, since an alias is the identical type.
	//
	// The check is type identity, so it does NOT catch a defined type (`type d time.Duration`) or a
	// raw int64 of nanoseconds. That is deliberate: the regression this guards is PR #755's shape —
	// re-adding or widening a named duration knob — not an author deliberately disguising one.
	allowed := map[string]bool{"p95Budget": true, "p99Budget": true}
	profileType := reflect.TypeOf(ownerLoadProfile{})
	for i := range profileType.NumField() {
		field := profileType.Field(i)
		if field.Type != reflect.TypeOf(time.Duration(0)) {
			continue
		}
		require.True(t, allowed[field.Name],
			"ownerLoadProfile gained a wall-clock budget %q; #1284 ruled that the framework-enforced "+
				"KV deadline is the only absolute ceiling on a measured key listing, observed as a "+
				"typed error and never restated as a predicted per-operation budget", field.Name)
	}

	// Property 2. 3s is the ruled value, deliberately loose: the worst healthy p95 measured on the
	// quiet box is 77.861 ms and on a shared runner 175.4 ms, so this is ~38x and ~17x respectively.
	// gh#1287 re-derives it from the recorded submission-order distributions; until that data
	// exists, tightening here would trade a measured flake for an unmeasured one.
	require.Equal(t, 3*time.Second, ci.p95Budget,
		"#1284 ruled the CI p95 budget stays at 3s pending the gh#1287 re-derivation")
	require.Equal(t, 3*time.Second, ci.p99Budget,
		"#1284 ruled the CI p99 budget stays at 3s pending the gh#1287 re-derivation")

	deadline := natsclient.DefaultKVOptions().Timeout
	require.Less(t, ci.p95Budget, deadline,
		"a percentile budget at or above the framework KV deadline can never fire — the #1286 defect")
	require.Less(t, ci.p99Budget, deadline,
		"a percentile budget at or above the framework KV deadline can never fire — the #1286 defect")

	// ADR-077 section 8 condition 4 fixes the guard's workload; the ruling moved its evidence home,
	// not its shape.
	require.Equal(t, 5_000, ci.entities, "ADR-077 s8 condition 4 fixes the CI guard at 5,000 hot members")
	require.Equal(t, 20, ci.spread, "ADR-077 s8 condition 4 fixes the CI guard at 20 spread predicates")
}

// TestOwnerLoadPercentiles_DoNotCoverTheMax proves that the percentile gates never examine the
// largest sample, which is WHY the absolute ceiling has to be the framework-enforced KV deadline
// rather than a percentile.
//
// Its predecessor drew the opposite conclusion from the same arithmetic — that the per-repetition
// operationBudget "MUST remain". #1284 measured what that gate actually caught: five runner stalls
// ~22x off the same run's own forward-filter distribution, and zero layout regressions. The
// arithmetic below is unchanged and still worth pinning; only the conclusion moves.
//
// At repetitions=n the percentiles index (n-1)*p/100, which is strictly less than n-1 for every
// n > 1 and p <= 99. So a single slow-but-legal operation — one that completes below the framework
// deadline, and therefore returns keys rather than a typed error — is invisible to both gates. What
// rejects a slower one is the deadline itself.
func TestOwnerLoadPercentiles_DoNotCoverTheMax(t *testing.T) {
	ci := ownerLoadCIProfile()
	require.Greater(t, ci.repetitions, 1, "a percentile over a single sample is not a percentile")

	// The fixture is built FROM the profile, so raising repetitions (#1284 design Q9) re-derives
	// this proof instead of silently invalidating a hard-coded five-sample fixture.
	deadline := natsclient.DefaultKVOptions().Timeout
	outlier := deadline - 100*time.Millisecond
	durations := make([]time.Duration, ci.repetitions)
	for i := range durations[:len(durations)-1] {
		durations[i] = time.Duration(100+i) * time.Millisecond
	}
	durations[len(durations)-1] = outlier
	require.Greater(t, outlier, ci.p95Budget,
		"the fixture's outlier must breach the percentile budget for this proof to say anything")

	p95Index := (len(durations) - 1) * 95 / 100
	p99Index := (len(durations) - 1) * 99 / 100
	require.Less(t, p99Index, len(durations)-1,
		"neither percentile index ever reaches the max, at any repetition count")
	require.LessOrEqual(t, durations[p95Index], ci.p95Budget, "the outlier is invisible to the p95 gate")
	require.LessOrEqual(t, durations[p99Index], ci.p99Budget, "the outlier is invisible to the p99 gate")

	// The conclusion, restated: this sample is a legal success — it is below the framework-enforced
	// KV deadline, so KeysByFilter returns keys. Nothing in the profile rejects it, and nothing
	// should: the only operation this harness refuses is one that reaches the deadline, and it
	// refuses that as the operation's own typed error, not as a budget comparison.
	require.Less(t, durations[len(durations)-1], deadline,
		"the ceiling is the framework KV deadline (natsclient/kv.go:39), observed rather than predicted")
}
