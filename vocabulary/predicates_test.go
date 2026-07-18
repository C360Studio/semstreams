package vocabulary

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestIsValidPredicate covers the three-level structural contract
// (gh#519 / enforce-structural-invariants): exactly three non-empty
// dot-separated parts, each lower-kebab ASCII (the full canonical contract is
// enforced by ParsePredicate; see predicate_contract_test.go for its edge
// cases). The pre-contract implementation only counted dots, which admitted
// empty segments.
func TestIsValidPredicate(t *testing.T) {
	cases := []struct {
		name      string
		predicate string
		want      bool
	}{
		{"canonical 3-part", "sensor.temperature.celsius", true},
		{"3-part with hyphens", "agent.agentic-loop.step-count", true},
		{"underscore violates lower-kebab contract", "agent.agentic-loop.step_count", false},
		// A 3-part predicate whose final segment is literally "value" is VALID —
		// this is the gh#519 collision case (must NOT be mistaken for a suffix).
		{"3-part ending in value (gh#519 collision)", "sensorml.capability.value", true},
		{"empty string", "", false},
		{"1-part", "predicate", false},
		{"2-part (the bad-fixture pattern)", "agent.role", false},
		{"4-part", "openspec.change.revision.value", false},
		{"empty middle segment", "sensor..celsius", false},
		{"leading dot (empty first segment)", ".temperature.celsius", false},
		{"trailing dot (empty last segment)", "sensor.temperature.", false},
		{"only dots", "..", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, IsValidPredicate(tc.predicate))
		})
	}
}
