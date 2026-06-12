package rule

import (
	"context"
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/governance"
	"github.com/c360studio/semstreams/payloadbuiltins"
)

func testVerdictFailuresCounter() *prometheus.CounterVec {
	return prometheus.NewCounterVec(
		prometheus.CounterOpts{Name: "test_governance_verdict_audit_failures_total"},
		[]string{"decision"},
	)
}

// TestStreamVerdictAuditor_EmitsEnvelopeToCorrectSubject drives the real auditor
// through its injected-publish seam: it must publish to the verdict subject and
// the bytes must decode through the production registry decoder back into a
// *governance.VerdictEvent (the §2 envelope contract over the wire).
func TestStreamVerdictAuditor_EmitsEnvelopeToCorrectSubject(t *testing.T) {
	var gotSubject string
	var gotData []byte
	a := &streamVerdictAuditor{
		failures: testVerdictFailuresCounter(),
		publish: func(_ context.Context, subject string, data []byte) error {
			gotSubject = subject
			gotData = data
			return nil
		},
	}
	ev := governance.VerdictEvent{
		Decision: governance.DecisionApprove,
		RuleID:   "approve-rule",
		Reason:   "policy permits",
		EntityID: "acme.ops.test.svc.entity.001",
	}
	require.NoError(t, a.EmitVerdict(context.Background(), ev))

	assert.Equal(t, governance.VerdictSubject(governance.DecisionApprove, "approve-rule"), gotSubject,
		"must publish to governance.verdict.{decision}.{rule_token}")

	decoded, err := payloadbuiltins.NewTestDecoder(t).Decode(gotData)
	require.NoError(t, err, "published bytes must be a decodable BaseMessage envelope")
	got, ok := decoded.Payload().(*governance.VerdictEvent)
	require.True(t, ok, "payload must decode to *VerdictEvent, got %T", decoded.Payload())
	assert.Equal(t, governance.DecisionApprove, got.Decision)
	assert.Equal(t, "approve-rule", got.RuleID)
	assert.Equal(t, "policy permits", got.Reason)
	assert.Equal(t, "acme.ops.test.svc.entity.001", got.EntityID)
}

// TestStreamVerdictAuditor_PublishFailureMetersAndReturnsError pins the
// best-effort observability contract: a failed publish increments the
// decision-labelled failure counter and returns the error (so the caller logs
// it), but never panics or blocks.
func TestStreamVerdictAuditor_PublishFailureMetersAndReturnsError(t *testing.T) {
	failures := testVerdictFailuresCounter()
	a := &streamVerdictAuditor{
		failures: failures,
		publish: func(_ context.Context, _ string, _ []byte) error {
			return errors.New("jetstream down")
		},
	}
	err := a.EmitVerdict(context.Background(), governance.VerdictEvent{
		Decision: governance.DecisionDeny,
		RuleID:   "deny-rule",
	})
	require.Error(t, err)
	assert.InDelta(t, 1.0, testutil.ToFloat64(failures.WithLabelValues(governance.DecisionDeny)), 0.0001,
		"a failed emit must increment the failure counter for that decision")
	assert.InDelta(t, 0.0, testutil.ToFloat64(failures.WithLabelValues(governance.DecisionApprove)), 0.0001,
		"the other decision label must be untouched")
}

// TestStreamVerdictAuditor_NilCounterIsSafe guards the metric-less wiring path
// (no metrics registry): a failure must still return the error without a panic.
func TestStreamVerdictAuditor_NilCounterIsSafe(t *testing.T) {
	a := &streamVerdictAuditor{
		failures: nil,
		publish: func(_ context.Context, _ string, _ []byte) error {
			return errors.New("boom")
		},
	}
	assert.Error(t, a.EmitVerdict(context.Background(), governance.VerdictEvent{
		Decision: governance.DecisionDeny,
		RuleID:   "deny-rule",
	}))
}

// TestStreamVerdictAuditor_RejectsInvalidEventWithoutPublishing pins the guard
// that an event failing its own envelope contract (e.g. a blank rule ID from a
// misconfigured rule) is NOT published — an un-attributable audit record is
// worse than a metered gap — and is metered as a failure.
func TestStreamVerdictAuditor_RejectsInvalidEventWithoutPublishing(t *testing.T) {
	failures := testVerdictFailuresCounter()
	published := false
	a := &streamVerdictAuditor{
		failures: failures,
		publish: func(_ context.Context, _ string, _ []byte) error {
			published = true
			return nil
		},
	}
	err := a.EmitVerdict(context.Background(), governance.VerdictEvent{Decision: governance.DecisionDeny}) // empty RuleID
	require.Error(t, err)
	assert.False(t, published, "an invalid event must never reach the wire")
	assert.InDelta(t, 1.0, testutil.ToFloat64(failures.WithLabelValues(governance.DecisionDeny)), 0.0001,
		"a rejected invalid event must meter as an audit failure")
}
