package graphingest

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
)

// The structural-identity predicate gate (enforce-structural-invariants) rejects
// non-3-part predicates at the mutation boundary. Default is FAIL-CLOSED: it meters
// mutation_rejections{reason=structural_predicate_invalid} + logs AND returns a
// classified ErrorCodeStructuralInvalid so the write is rejected. The escape hatch
// AllowNonConformingPredicates downgrades to observe-only (meter + log, commit).

const structuralGateEntity = "acme.ops.robotics.gcs.drone.001"

func TestValidateTriplePredicates_EscapeHatch_MetersButAllows(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	comp.config.AllowNonConformingPredicates = true // escape hatch: observe-only
	const subj = "test.structural.observe"

	counter := comp.mutationRejections.WithLabelValues(subj, "structural_predicate_invalid")
	before := testutil.ToFloat64(counter)

	// A 2-part predicate is structurally invalid (the bad-fixture pattern).
	triples := []message.Triple{
		{Subject: structuralGateEntity, Predicate: "agent.role", Object: "researcher"},
	}
	err := comp.validateTriplePredicates(subj, triples)
	require.NoError(t, err, "observe-only mode must not reject the mutation")
	assert.InDelta(t, before+1, testutil.ToFloat64(counter), 0.0001,
		"observe-only mode still meters the structural violation for the dry-run audit")
}

func TestValidateTriplePredicates_DefaultFailClosed_RejectsClassified(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	// Default (zero-value config): fail-closed — no field set.
	const subj = "test.structural.enforce"

	triples := []message.Triple{
		{Subject: structuralGateEntity, Predicate: "agent.role", Object: "researcher"},
	}
	err := comp.validateTriplePredicates(subj, triples)
	require.Error(t, err, "enforce mode must reject a non-3-part predicate")
	var ce *errs.ClassifiedError
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, graph.ErrorCodeStructuralInvalid, ce.Code,
		"a structural rejection carries ErrorCodeStructuralInvalid")
}

func TestValidateTriplePredicates_ValidPredicate_Untouched(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	// Default fail-closed mode — a conforming predicate must still pass untouched.
	const subj = "test.structural.valid"

	counter := comp.mutationRejections.WithLabelValues(subj, "structural_predicate_invalid")
	before := testutil.ToFloat64(counter)

	triples := []message.Triple{
		{Subject: structuralGateEntity, Predicate: "sensor.temperature.celsius", Object: 22.5},
		// gh#519 collision guard: a valid 3-part predicate whose last segment is
		// literally "value" MUST pass (it is a real predicate, not a suffix).
		{Subject: structuralGateEntity, Predicate: "sensorml.capability.value", Object: "50m"},
	}
	err := comp.validateTriplePredicates(subj, triples)
	require.NoError(t, err, "conforming 3-part predicates pass even in enforce mode")
	assert.InDelta(t, before, testutil.ToFloat64(counter), 0.0001,
		"no rejection metered for valid predicates")
}
