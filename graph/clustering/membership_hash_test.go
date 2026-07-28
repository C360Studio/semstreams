package clustering

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// evalFormulaHash reproduces the ORIGINAL inline membership-hash formula that
// lived in test/e2e/scenarios/validate_thematic_eval.go (:530-533) before it was
// refactored to call clustering.MembershipHash. The parity test below asserts the
// shared helper is byte-identical to that formula, so the store, the read-join,
// and the B0 eval can never drift into two hashes that never join.
func evalFormulaHash(members []string) string {
	sorted := append([]string(nil), members...)
	sort.Strings(sorted)
	sum := sha256.Sum256([]byte(strings.Join(sorted, "\n")))
	return hex.EncodeToString(sum[:])
}

func TestMembershipHash_ParityWithEvalFormula(t *testing.T) {
	t.Parallel()

	members := []string{
		"acme.ops.robotics.gcs.drone.003",
		"acme.ops.robotics.gcs.drone.001",
		"acme.ops.robotics.gcs.drone.002",
	}

	want := evalFormulaHash(members)
	got := MembershipHash(members)
	require.Equal(t, want, got, "MembershipHash must be byte-identical to the eval's original formula")
}

func TestMembershipHash_IsSortStable(t *testing.T) {
	t.Parallel()

	a := []string{"c", "a", "b"}
	b := []string{"b", "c", "a"}
	// Same member set, different input order → identical hash (no map-iteration or
	// caller-order dependence in key derivation).
	require.Equal(t, MembershipHash(a), MembershipHash(b))
}

func TestMembershipHash_DistinctMembershipsDiffer(t *testing.T) {
	t.Parallel()

	require.NotEqual(t, MembershipHash([]string{"a", "b"}), MembershipHash([]string{"a", "b", "c"}))
}
