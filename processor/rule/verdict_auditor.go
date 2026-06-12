package rule

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/c360studio/semstreams/governance"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
)

// streamVerdictAuditor is the concrete VerdictAuditor (ADR-055 §3a). It wraps a
// verdict event in a BaseMessage envelope (so registry-based readers — audit/ops
// dashboards replaying the stream — can decode it off the wire) and publishes to
// the GOVERNANCE_VERDICT_AUDIT stream via JetStream. On failure it increments the
// failure metric and returns the error; it does NOT log (the caller logs at Error
// with full verdict context) and NEVER changes a verdict.
//
// It deliberately depends on a minimal publish func + a counter rather than the
// whole Processor, so the emit path is unit-testable without a NATS client.
type streamVerdictAuditor struct {
	publish  func(ctx context.Context, subject string, data []byte) error
	failures *prometheus.CounterVec
}

// newVerdictAuditor builds a verdict auditor that publishes to the audit stream
// via the processor's NATS client. The publish func always targets JetStream
// (PublishToStream) because governance.verdict.> is a stream subject. Requires a
// non-nil natsClient (the rule processor only wires the auditor when one exists).
func newVerdictAuditor(rp *Processor) *streamVerdictAuditor {
	var failures *prometheus.CounterVec
	if rp.metrics != nil {
		failures = rp.metrics.governanceVerdictAuditFailures
	}
	return &streamVerdictAuditor{
		publish:  rp.natsClient.PublishToStream,
		failures: failures,
	}
}

// EmitVerdict implements VerdictAuditor.
func (a *streamVerdictAuditor) EmitVerdict(ctx context.Context, ev governance.VerdictEvent) error {
	// Never publish an event that fails its own envelope contract (e.g. a blank
	// rule ID from a misconfigured rule) — an un-attributable audit record is
	// worse than a metered gap, and would not survive a registry decode on read.
	// A reject here counts as an audit failure like any other.
	if err := ev.Validate(); err != nil {
		a.recordFailure(ev.Decision)
		return errs.WrapInvalid(err, "verdictAuditor", "EmitVerdict", "invalid verdict event")
	}

	subject := governance.VerdictSubject(ev.Decision, ev.RuleID)

	// Wrap in a BaseMessage envelope so registry decoders can read the verdict
	// off the wire (the §2 semantic-envelope requirement; also
	// feedback_nats_publishes_use_payload_registry).
	baseMsg := message.NewBaseMessage(ev.Schema(), &ev, "rule_engine")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		a.recordFailure(ev.Decision)
		return errs.Wrap(err, "verdictAuditor", "EmitVerdict", "marshal verdict envelope")
	}

	if err := a.publish(ctx, subject, data); err != nil {
		a.recordFailure(ev.Decision)
		return errs.WrapTransient(err, "verdictAuditor", "EmitVerdict", fmt.Sprintf("publish to %s", subject))
	}
	return nil
}

// recordFailure increments the audit-failure counter (nil-safe for NATS-less or
// metric-less test wiring).
func (a *streamVerdictAuditor) recordFailure(decision string) {
	if a.failures != nil {
		a.failures.WithLabelValues(decision).Inc()
	}
}
