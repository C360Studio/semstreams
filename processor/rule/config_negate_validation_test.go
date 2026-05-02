// Package rule - Validation tests for negate:true and logic:"not" constraints.
//
// Design constraints validated here:
//  1. negate:true on the transition operator MUST be rejected at config-load time.
//  2. logic:"not" requires EXACTLY ONE condition (0 or 2+ are rejected).
//  3. logic:"not" combined with a per-condition negate:true is rejected (double-negation anti-pattern).
//  4. logic:"not" with a single, clean condition is accepted.
package rule

import (
	"log/slog"
	"testing"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newValidationProcessor returns a minimal *Processor suitable for calling
// ValidateConfigUpdate. It mirrors the pattern used in runtime_config_test.go.
func newValidationProcessor() *Processor {
	return &Processor{
		natsClient: &natsclient.Client{},
		logger:     slog.Default(),
		rules:      make(map[string]Rule),
	}
}

// TestValidate_NegateOnTransition_Rejected verifies that a condition using the
// transition operator combined with negate:true is rejected at validation.
// The transition operator has three plausible negation meanings (NOT-transitioned,
// NOT-current-matches, NOT-prev-was-in-from) — rejecting forces explicit re-formulation.
func TestValidate_NegateOnTransition_Rejected(t *testing.T) {
	t.Parallel()
	p := newValidationProcessor()

	changes := map[string]any{
		"rules": map[string]any{
			"trans-negate-rule": map[string]any{
				"type": "test_rule",
				"conditions": []any{
					map[string]any{
						"field":    "status",
						"operator": "transition",
						"value":    "active",
						"from":     []any{"inactive"},
						"negate":   true,
					},
				},
				"logic": "and",
			},
		},
	}

	err := p.ValidateConfigUpdate(changes)
	require.Error(t, err, "negate:true on transition operator should be rejected")
	assert.Contains(t, err.Error(), "negate")
	assert.Contains(t, err.Error(), "transition")
}

// TestValidate_LogicNot_RequiresSingleCondition_Zero verifies that logic:"not" with
// zero conditions is rejected. (Zero conditions would also be caught by the existing
// "must have at least one condition" check, but explicit coverage is worthwhile.)
func TestValidate_LogicNot_RequiresSingleCondition_Zero(t *testing.T) {
	t.Parallel()
	p := newValidationProcessor()

	changes := map[string]any{
		"rules": map[string]any{
			"not-zero-conds": map[string]any{
				"type":       "test_rule",
				"conditions": []any{},
				"logic":      "not",
			},
		},
	}

	err := p.ValidateConfigUpdate(changes)
	require.Error(t, err, "logic:not with 0 conditions should be rejected")
}

// TestValidate_LogicNot_RequiresSingleCondition_Two verifies that logic:"not" with
// two conditions is rejected. De Morgan's ambiguity: not(A AND B) ≠ (not A) OR (not B).
// Authors who need compound negation must use per-condition negate:true instead.
func TestValidate_LogicNot_RequiresSingleCondition_Two(t *testing.T) {
	t.Parallel()
	p := newValidationProcessor()

	changes := map[string]any{
		"rules": map[string]any{
			"not-two-conds": map[string]any{
				"type": "test_rule",
				"conditions": []any{
					map[string]any{
						"field":    "status",
						"operator": "eq",
						"value":    "active",
					},
					map[string]any{
						"field":    "level",
						"operator": "gt",
						"value":    50.0,
					},
				},
				"logic": "not",
			},
		},
	}

	err := p.ValidateConfigUpdate(changes)
	require.Error(t, err, "logic:not with 2 conditions should be rejected")
	assert.Contains(t, err.Error(), "not")
}

// TestValidate_LogicNot_RejectsCombinedNegate verifies that logic:"not" combined with
// a condition that has negate:true is rejected (double-negation anti-pattern).
// Double negation cancels (NOT (NOT X) = X) and confuses readers; authors should
// remove one of the negations.
func TestValidate_LogicNot_RejectsCombinedNegate(t *testing.T) {
	t.Parallel()
	p := newValidationProcessor()

	changes := map[string]any{
		"rules": map[string]any{
			"double-negate-rule": map[string]any{
				"type": "test_rule",
				"conditions": []any{
					map[string]any{
						"field":    "status",
						"operator": "eq",
						"value":    "active",
						"negate":   true, // combined with logic:not → double negation
					},
				},
				"logic": "not",
			},
		},
	}

	err := p.ValidateConfigUpdate(changes)
	require.Error(t, err, "logic:not + condition negate:true should be rejected (double-negation)")
	assert.Contains(t, err.Error(), "negate")
}

// TestValidate_LogicNot_SingleClean_Accepted verifies that logic:"not" with a single
// condition that does NOT have negate:true is accepted.
func TestValidate_LogicNot_SingleClean_Accepted(t *testing.T) {
	t.Parallel()
	p := newValidationProcessor()

	changes := map[string]any{
		"rules": map[string]any{
			"not-single-clean": map[string]any{
				"type": "test_rule",
				"conditions": []any{
					map[string]any{
						"field":    "status",
						"operator": "eq",
						"value":    "active",
					},
				},
				"logic": "not",
			},
		},
	}

	err := p.ValidateConfigUpdate(changes)
	assert.NoError(t, err, "logic:not with one clean condition should be accepted")
}

// TestValidate_NegateOnNonTransition_Accepted verifies that negate:true on operators
// other than transition is accepted (eq, in, contains, etc.).
func TestValidate_NegateOnNonTransition_Accepted(t *testing.T) {
	t.Parallel()
	p := newValidationProcessor()

	operators := []string{"eq", "ne", "lt", "lte", "gt", "gte", "in", "not_in", "contains", "starts_with", "ends_with", "regex", "between"}

	for _, op := range operators {
		op := op // capture
		t.Run(op, func(t *testing.T) {
			t.Parallel()

			var value interface{} = "test"
			if op == "in" || op == "not_in" {
				value = []interface{}{"a", "b"}
			} else if op == "between" {
				value = []interface{}{1.0, 10.0}
			} else if op == "lt" || op == "lte" || op == "gt" || op == "gte" {
				value = 42.0
			}

			changes := map[string]any{
				"rules": map[string]any{
					"negate-" + op: map[string]any{
						"type": "test_rule",
						"conditions": []any{
							map[string]any{
								"field":    "some.field",
								"operator": op,
								"value":    value,
								"negate":   true,
							},
						},
						"logic": "and",
					},
				},
			}

			err := p.ValidateConfigUpdate(changes)
			assert.NoError(t, err, "negate:true on %s should be accepted", op)
		})
	}
}

// TestValidate_LogicNot_ErrorMessage_MentionsCount verifies that the error message
// for logic:"not" with too many conditions names the count so authors can debug quickly.
func TestValidate_LogicNot_ErrorMessage_MentionsCount(t *testing.T) {
	t.Parallel()
	p := newValidationProcessor()

	changes := map[string]any{
		"rules": map[string]any{
			"count-check-rule": map[string]any{
				"type": "test_rule",
				"conditions": []any{
					map[string]any{"field": "a", "operator": "eq", "value": "x"},
					map[string]any{"field": "b", "operator": "eq", "value": "y"},
					map[string]any{"field": "c", "operator": "eq", "value": "z"},
				},
				"logic": "not",
			},
		},
	}

	err := p.ValidateConfigUpdate(changes)
	require.Error(t, err)
	// Error should mention the actual count (3) and the requirement (1)
	assert.Contains(t, err.Error(), "3", "error should mention the actual condition count")
}
