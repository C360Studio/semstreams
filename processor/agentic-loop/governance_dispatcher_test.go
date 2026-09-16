package agenticloop

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockVerdictPublisher captures every publish call so assertions can
// inspect the published payloads. err is returned from PublishToStream
// when non-nil.
type mockVerdictPublisher struct {
	mu        sync.Mutex
	published []publishedVerdict
	err       error
}

func governanceTestCall(id, name string) agentic.ToolCall {
	return agentic.ToolCall{
		ID: id, Name: name, RequestID: "request-" + id,
		ExecutionID: "execution-" + id, CallOrdinal: 1,
	}
}

func governanceTestVerdict(decision, executionID string) VerdictPayload {
	return VerdictPayload{Decision: decision, ExecutionID: executionID, LoopID: "loop-1",
		RequestID: "request-1", ProposalFingerprint: "fingerprint"}
}

func governanceTestProposal(executionID string) ProposedToolCallPayload {
	return ProposedToolCallPayload{LoopID: "loop-1", RequestID: "request-1", ExecutionID: executionID,
		CallID: "call-1", CallOrdinal: 1, ProposalFingerprint: "fingerprint", ToolName: "lookup"}
}

// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestGovernanceDispatcherHandleVerdictDeclaresDeliveryOutcome(t *testing.T) {
	t.Parallel()

	disabled := NewGovernanceDispatcher(ToolCallGovernanceConfig{Mode: ToolCallGovernanceModeDisabled}, nil, slog.Default(), nil)
	decision, err := disabled.HandleVerdict(governanceTestVerdict("approved", "call-disabled"))
	require.NoError(t, err)
	require.Equal(t, natsclient.DeliveryDecisionAck, decision)

	audit := NewGovernanceDispatcher(ToolCallGovernanceConfig{Mode: ToolCallGovernanceModeAudit}, nil, slog.Default(), nil)
	decision, err = audit.HandleVerdict(governanceTestVerdict("approved", "call-audit"))
	require.NoError(t, err)
	require.Equal(t, natsclient.DeliveryDecisionAck, decision)

	enforce := NewGovernanceDispatcher(
		ToolCallGovernanceConfig{Mode: ToolCallGovernanceModeEnforce, Timeout: "1s"},
		nil, slog.Default(), nil,
	).(*enforceDispatcher)

	decision, err = enforce.HandleVerdict(governanceTestVerdict("approved", "missing"))
	require.Error(t, err)
	require.Equal(t, natsclient.DeliveryDecisionRetry, decision)

	delivered := enforce.registerWaiter(governanceTestProposal("delivered")).arrivals
	decision, err = enforce.HandleVerdict(governanceTestVerdict("approved", "delivered"))
	require.NoError(t, err)
	require.Equal(t, natsclient.DeliveryDecisionAck, decision)
	require.Equal(t, "approved", (<-delivered).decision)
	enforce.releaseWaiter("delivered")

	full := enforce.registerWaiter(governanceTestProposal("full")).arrivals
	full <- verdictArrival{decision: "approved"}
	decision, err = enforce.HandleVerdict(governanceTestVerdict("rejected", "full"))
	require.Error(t, err)
	require.Equal(t, natsclient.DeliveryDecisionQuarantine, decision)
	enforce.releaseWaiter("full")
}

type publishedVerdict struct {
	subject string
	data    []byte
}

func (m *mockVerdictPublisher) PublishToStream(_ context.Context, subject string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	cpy := make([]byte, len(data))
	copy(cpy, data)
	m.published = append(m.published, publishedVerdict{subject: subject, data: cpy})
	return nil
}

func (m *mockVerdictPublisher) Published() []publishedVerdict {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]publishedVerdict, len(m.published))
	copy(out, m.published)
	return out
}

// --- disabled mode --------------------------------------------------

func TestDispatcher_DisabledModePassThroughNoPublish(t *testing.T) {
	t.Parallel()

	pub := &mockVerdictPublisher{}
	d := NewGovernanceDispatcher(ToolCallGovernanceConfig{Mode: ToolCallGovernanceModeDisabled}, pub, slog.Default(), nil)

	calls := []agentic.ToolCall{
		{ID: "c1", Name: "bash"},
		{ID: "c2", Name: "http_request"},
	}
	result, err := d.Propose(context.Background(), "loop-1", "", calls)
	require.NoError(t, err)

	assert.Equal(t, calls, result.Approved, "disabled must pass all calls through as approved")
	assert.Empty(t, result.Rejected, "disabled never rejects")
	assert.Empty(t, pub.Published(), "disabled must NOT publish")
	assert.Equal(t, ToolCallGovernanceModeDisabled, d.Mode())
}

// --- audit mode -----------------------------------------------------

func TestDispatcher_AuditModePublishesAndPassesThrough(t *testing.T) {
	t.Parallel()

	pub := &mockVerdictPublisher{}
	d := NewGovernanceDispatcher(ToolCallGovernanceConfig{Mode: ToolCallGovernanceModeAudit}, pub, slog.Default(), nil)

	calls := []agentic.ToolCall{
		governanceTestCall("c1", "bash"),
		governanceTestCall("c2", "http_request"),
	}
	calls[0].Arguments = map[string]any{"command": "ls /tmp"}
	calls[1].Arguments = map[string]any{"url": "https://example.com"}
	result, err := d.Propose(context.Background(), "loop-abc", "parent-loop", calls)
	require.NoError(t, err)

	assert.Equal(t, calls, result.Approved, "audit must pass all calls through as approved")
	assert.Empty(t, result.Rejected, "audit never rejects locally — verdicts that arrive late are observability only")

	published := pub.Published()
	require.Len(t, published, 2, "audit must publish every call to agent.toolcall.proposed.*")

	for i, expected := range []string{"c1", "c2"} {
		assert.Equal(t, "agent.toolcall.proposed.loop-abc", published[i].subject)
		payload := unwrapProposedFromBaseMessage(t, published[i].data)
		assert.Equal(t, "loop-abc", payload.LoopID)
		assert.Equal(t, "parent-loop", payload.ParentLoopID, "parent_loop_id rides along from day one (ADR-039)")
		assert.Equal(t, expected, payload.CallID)
	}
	// Flattened conveniences
	firstPayload := unwrapProposedFromBaseMessage(t, published[0].data)
	assert.Equal(t, "ls /tmp", firstPayload.Command, "bash command should flatten to Command field for rule readability")

	secondPayload := unwrapProposedFromBaseMessage(t, published[1].data)
	assert.Equal(t, "https://example.com", secondPayload.URL, "http_request url should flatten to URL field")
}

// unwrapProposedFromBaseMessage extracts a ProposedToolCallPayload from
// the BaseMessage wire envelope the dispatcher publishes. Mirrors the
// rule processor's decode path: pull `payload.data` out of the wire
// form, re-marshal to bytes, decode into the typed payload.
//
// Kept as a test helper because production consumers (rule processor +
// agentic-loop verdict handler) use different code paths — rules read
// via GenericJSONPayload.Data; agentic-loop reads VerdictPayload off
// the verdict subject. Tests need the typed view of the proposed-call
// shape to assert on the canonical fields.
func unwrapProposedFromBaseMessage(t *testing.T, data []byte) ProposedToolCallPayload {
	t.Helper()
	// wireFormat carries the payload under "payload" — extract it as a
	// raw RawMessage, then re-unmarshal into the typed struct.
	var envelope struct {
		Payload json.RawMessage `json:"payload"`
	}
	require.NoError(t, json.Unmarshal(data, &envelope), "envelope must be wireFormat-shaped")
	// The payload is a GenericJSONPayload: { "data": { ... proposed-call fields ... } }
	var generic struct {
		Data ProposedToolCallPayload `json:"data"`
	}
	require.NoError(t, json.Unmarshal(envelope.Payload, &generic), "payload must be GenericJSONPayload-shaped")
	return generic.Data
}

// Audit-mode publish failure logs but DOES NOT prevent dispatch.
// Surface as a Warn, return Approved as if nothing happened.
func TestDispatcher_AuditModeIgnoresPublishFailure(t *testing.T) {
	t.Parallel()

	pub := &mockVerdictPublisher{err: errors.New("nats unavailable")}
	d := NewGovernanceDispatcher(ToolCallGovernanceConfig{Mode: ToolCallGovernanceModeAudit}, pub, slog.Default(), nil)

	calls := []agentic.ToolCall{governanceTestCall("c1", "bash")}
	result, err := d.Propose(context.Background(), "loop-1", "", calls)
	require.NoError(t, err, "audit publish failure must not propagate")
	assert.Equal(t, calls, result.Approved, "audit must still pass calls through even when publish fails")
}

// --- enforce mode ---------------------------------------------------

func TestDispatcher_EnforceModeWaitsForApproveVerdict(t *testing.T) {
	t.Parallel()

	pub := &raceTestPublisher{}
	d := NewGovernanceDispatcher(
		ToolCallGovernanceConfig{Mode: ToolCallGovernanceModeEnforce, Timeout: "2s"},
		pub, slog.Default(), nil,
	)

	calls := []agentic.ToolCall{governanceTestCall("call-001", "bash")}

	pub.onPublish = func() {
		payload := verdictForPublishedProposal(publishedGovernanceProposal(t, pub.published[0].data), "approved")
		payload.RuleID, payload.Reason = "rule-allow", "policy permits"
		decision, err := d.HandleVerdict(payload)
		require.NoError(t, err)
		require.Equal(t, natsclient.DeliveryDecisionAck, decision)
	}

	result, err := d.Propose(context.Background(), "loop-1", "", calls)
	require.NoError(t, err)

	assert.Len(t, result.Approved, 1)
	assert.Empty(t, result.Rejected)
	assert.Equal(t, "call-001", result.Approved[0].ID)
}

func TestDispatcher_EnforceModeRejectsOnDenyVerdict(t *testing.T) {
	t.Parallel()

	pub := &raceTestPublisher{}
	d := NewGovernanceDispatcher(
		ToolCallGovernanceConfig{Mode: ToolCallGovernanceModeEnforce, Timeout: "2s"},
		pub, slog.Default(), nil,
	)

	calls := []agentic.ToolCall{governanceTestCall("call-001", "bash")}

	pub.onPublish = func() {
		payload := verdictForPublishedProposal(publishedGovernanceProposal(t, pub.published[0].data), "rejected")
		payload.RuleID, payload.Reason = "block-bash", "bash disallowed"
		decision, err := d.HandleVerdict(payload)
		require.NoError(t, err)
		require.Equal(t, natsclient.DeliveryDecisionAck, decision)
	}

	result, err := d.Propose(context.Background(), "loop-1", "", calls)
	require.NoError(t, err)

	assert.Empty(t, result.Approved)
	require.Len(t, result.Rejected, 1)
	assert.Equal(t, "call-001", result.Rejected[0].Call.ID)
	assert.Contains(t, result.Rejected[0].Reason, "bash disallowed")
	assert.Contains(t, result.Rejected[0].Reason, "block-bash")
}

// Fail-closed on timeout — the canonical safety invariant. If governance
// rules don't fire within the timeout, treat as a reject so missing
// rules can't become silent approve.
func TestDispatcher_EnforceModeFailsClosedOnTimeout(t *testing.T) {
	t.Parallel()

	pub := &mockVerdictPublisher{}
	d := NewGovernanceDispatcher(
		ToolCallGovernanceConfig{Mode: ToolCallGovernanceModeEnforce, Timeout: "100ms"},
		pub, slog.Default(), nil,
	)

	calls := []agentic.ToolCall{governanceTestCall("call-001", "bash")}

	start := time.Now()
	result, err := d.Propose(context.Background(), "loop-1", "", calls)
	elapsed := time.Since(start)
	require.NoError(t, err)

	assert.Empty(t, result.Approved, "no verdict within timeout must result in zero approved")
	require.Len(t, result.Rejected, 1, "must reject on timeout (fail-closed)")
	assert.Contains(t, result.Rejected[0].Reason, "timeout")
	assert.GreaterOrEqual(t, elapsed, 100*time.Millisecond,
		"must wait at least the configured timeout before failing closed")
	assert.Less(t, elapsed, 500*time.Millisecond,
		"must NOT wait significantly longer than timeout (within scheduling slop)")
}

// Mixed verdicts in a single batch: order must be preserved across
// approve and reject so the downstream serial dispatcher sees calls in
// the same order the model emitted them.
func TestDispatcher_EnforceModeMixedVerdictsPreserveOrder(t *testing.T) {
	t.Parallel()

	pub := &raceTestPublisher{}
	d := NewGovernanceDispatcher(
		ToolCallGovernanceConfig{Mode: ToolCallGovernanceModeEnforce, Timeout: "2s"},
		pub, slog.Default(), nil,
	)

	calls := []agentic.ToolCall{
		governanceTestCall("c1", "bash"),
		governanceTestCall("c2", "http_request"),
		governanceTestCall("c3", "bash"),
	}

	pub.onPublish = func() {
		if len(pub.published) != len(calls) {
			return
		}
		// Reverse actual publication order to test routing independently of arrival.
		for i := len(pub.published) - 1; i >= 0; i-- {
			payload := verdictForPublishedProposal(publishedGovernanceProposal(t, pub.published[i].data), "approved")
			if i == 1 {
				payload.Decision, payload.Reason = "rejected", "blocked"
			}
			decision, err := d.HandleVerdict(payload)
			require.NoError(t, err)
			require.Equal(t, natsclient.DeliveryDecisionAck, decision)
		}
	}

	result, err := d.Propose(context.Background(), "loop-1", "", calls)
	require.NoError(t, err)

	require.Len(t, result.Approved, 2)
	require.Len(t, result.Rejected, 1)
	assert.Equal(t, "c1", result.Approved[0].ID, "approved ordering preserved")
	assert.Equal(t, "c3", result.Approved[1].ID)
	assert.Equal(t, "c2", result.Rejected[0].Call.ID)
}

// spec: agentic-governance / Governance publications are durably at-least-once
// A partial publication is retryable source work, never a policy rejection.
func TestDispatcher_EnforceModePartialPublishFailure(t *testing.T) {
	t.Parallel()

	var d GovernanceDispatcher
	pub := proposalTestPublisher(func(_ context.Context, _ string, data []byte) error {
		proposal := publishedGovernanceProposal(t, data)
		if proposal.CallID == "c2" {
			return errors.New("selective publish failure")
		}
		decision, err := d.HandleVerdict(verdictForPublishedProposal(proposal, "approved"))
		require.NoError(t, err)
		require.Equal(t, natsclient.DeliveryDecisionAck, decision)
		return nil
	})
	d = NewGovernanceDispatcher(
		ToolCallGovernanceConfig{Mode: ToolCallGovernanceModeEnforce, Timeout: "1s"},
		pub, slog.Default(), nil,
	)

	calls := []agentic.ToolCall{
		governanceTestCall("c1", "bash"),
		governanceTestCall("c2", "bash"),
	}

	result, err := d.Propose(context.Background(), "loop-1", "", calls)
	require.ErrorIs(t, err, ErrGovernancePublishFailed)
	require.Empty(t, result.Approved)
	require.Empty(t, result.Rejected)
	require.Empty(t, d.(*enforceDispatcher).waiters)
}

// Race-condition fix: verdict arriving BEFORE Propose enters its select
// (i.e., the buffered channel absorbs it) must still resolve correctly.
// This is the canonical subscribe-before-publish race in process form.
func TestDispatcher_EnforceModeVerdictBeforeSelectArrival(t *testing.T) {
	t.Parallel()

	pub := &raceTestPublisher{}
	d := NewGovernanceDispatcher(
		ToolCallGovernanceConfig{Mode: ToolCallGovernanceModeEnforce, Timeout: "2s"},
		pub, slog.Default(), nil,
	)

	calls := []agentic.ToolCall{governanceTestCall("fast-call", "bash")}

	// raceTestPublisher fires the verdict from INSIDE PublishToStream —
	// before Propose returns from publish and enters the select. The
	// buffered waiter channel must absorb this.
	pub.onPublish = func() {
		payload := verdictForPublishedProposal(publishedGovernanceProposal(t, pub.published[0].data), "approved")
		payload.RuleID = "fast-rule"
		decision, err := d.HandleVerdict(payload)
		require.NoError(t, err)
		require.Equal(t, natsclient.DeliveryDecisionAck, decision)
	}

	result, err := d.Propose(context.Background(), "loop-1", "", calls)
	require.NoError(t, err)
	require.Len(t, result.Approved, 1, "race-fix: verdict during publish must still resolve approved")
	assert.Equal(t, "fast-call", result.Approved[0].ID)
}

// Late verdict (after Propose returned via timeout) must not panic or
// leak. The waiter map is released by defer; HandleVerdict logs at
// Debug and returns. Pins the no-leak invariant.
func TestDispatcher_EnforceModeLateVerdictIsNoOp(t *testing.T) {
	t.Parallel()

	pub := &mockVerdictPublisher{}
	d := NewGovernanceDispatcher(
		ToolCallGovernanceConfig{Mode: ToolCallGovernanceModeEnforce, Timeout: "50ms"},
		pub, slog.Default(), nil,
	)

	// Propose returns via timeout (no verdict sent inside).
	result, err := d.Propose(context.Background(), "loop-1", "",
		[]agentic.ToolCall{governanceTestCall("late-call", "bash")})
	require.NoError(t, err)
	require.Len(t, result.Rejected, 1)

	// Now fire a late verdict — must not panic.
	d.HandleVerdict(governanceTestVerdict("approved", "execution-late-call"))
}

// --- metrics integration --------------------------------------------

// mockDispatcherMetrics captures metric calls so tests can assert the
// dispatcher fires them on the right transitions.
type mockDispatcherMetrics struct {
	mu                 sync.Mutex
	verdicts           []recordedVerdict
	missingWaiterCalls int
}

type recordedVerdict struct {
	decision string
	mode     string
	duration float64
}

func (m *mockDispatcherMetrics) RecordGovernanceVerdict(decision, mode string, duration float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.verdicts = append(m.verdicts, recordedVerdict{decision: decision, mode: mode, duration: duration})
}

func (m *mockDispatcherMetrics) RecordGovernanceVerdictMissingWaiter() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.missingWaiterCalls++
}

func (m *mockDispatcherMetrics) Verdicts() []recordedVerdict {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]recordedVerdict, len(m.verdicts))
	copy(out, m.verdicts)
	return out
}

func TestDispatcher_EnforceModeRecordsApprovedVerdictMetric(t *testing.T) {
	t.Parallel()

	pub := &raceTestPublisher{}
	mx := &mockDispatcherMetrics{}
	d := NewGovernanceDispatcher(
		ToolCallGovernanceConfig{Mode: ToolCallGovernanceModeEnforce, Timeout: "2s"},
		pub, slog.Default(), mx,
	)

	calls := []agentic.ToolCall{governanceTestCall("c1", "bash")}
	pub.onPublish = func() {
		payload := verdictForPublishedProposal(publishedGovernanceProposal(t, pub.published[0].data), "approved")
		decision, err := d.HandleVerdict(payload)
		require.NoError(t, err)
		require.Equal(t, natsclient.DeliveryDecisionAck, decision)
	}

	_, err := d.Propose(context.Background(), "loop-1", "", calls)
	require.NoError(t, err)

	recorded := mx.Verdicts()
	require.Len(t, recorded, 1)
	assert.Equal(t, "approved", recorded[0].decision)
	assert.Equal(t, ToolCallGovernanceModeEnforce, recorded[0].mode)
	assert.Greater(t, recorded[0].duration, 0.0)
}

func TestDispatcher_EnforceModeRecordsTimeoutVerdictMetric(t *testing.T) {
	t.Parallel()

	pub := &mockVerdictPublisher{}
	mx := &mockDispatcherMetrics{}
	d := NewGovernanceDispatcher(
		ToolCallGovernanceConfig{Mode: ToolCallGovernanceModeEnforce, Timeout: "50ms"},
		pub, slog.Default(), mx,
	)

	calls := []agentic.ToolCall{governanceTestCall("c1", "bash")}
	_, err := d.Propose(context.Background(), "loop-1", "", calls)
	require.NoError(t, err)

	recorded := mx.Verdicts()
	require.Len(t, recorded, 1)
	assert.Equal(t, "timeout", recorded[0].decision, "timeout decision is its own label, not rejected")
	assert.Equal(t, ToolCallGovernanceModeEnforce, recorded[0].mode)
}

// Late verdict (after Propose timeout already fired) increments the
// missing-waiter counter — this is the canonical signal that the
// subscribe-before-publish race-fix regressed.
func TestDispatcher_LateVerdictIncrementsMissingWaiterMetric(t *testing.T) {
	t.Parallel()

	pub := &mockVerdictPublisher{}
	mx := &mockDispatcherMetrics{}
	d := NewGovernanceDispatcher(
		ToolCallGovernanceConfig{Mode: ToolCallGovernanceModeEnforce, Timeout: "30ms"},
		pub, slog.Default(), mx,
	)

	// Propose returns via timeout first.
	_, err := d.Propose(context.Background(), "loop-1", "",
		[]agentic.ToolCall{governanceTestCall("late-call", "bash")})
	require.NoError(t, err)

	// Late verdict — waiter already released by defer. Must increment
	// the missing-waiter counter, not panic.
	d.HandleVerdict(governanceTestVerdict("approved", "execution-late-call"))

	assert.Equal(t, 1, mx.missingWaiterCalls,
		"late verdict for released waiter must increment subscribe-before-publish counter")
}

// VerdictPayload supports two on-the-wire shapes. The approve-action
// writes top-level fields; the publish-action shape (used by ADR-039's
// rejection example rules) nests fields under "properties". Both must
// resolve through the same helpers so consumers don't write a custom
// parser per shape.
func TestVerdictPayload_EffectiveAccessors(t *testing.T) {
	t.Parallel()

	t.Run("top-level shape (approve action)", func(t *testing.T) {
		t.Parallel()
		p := VerdictPayload{
			Decision: "approved",
			CallID:   "call-1",
			Reason:   "policy permits",
		}
		assert.Equal(t, "approved", p.EffectiveDecision())
		assert.Equal(t, "call-1", p.EffectiveCallID())
		assert.Equal(t, "policy permits", p.EffectiveReason())
	})

	t.Run("nested shape (publish action)", func(t *testing.T) {
		t.Parallel()
		p := VerdictPayload{
			Properties: map[string]any{
				"decision": "rejected",
				"call_id":  "call-2",
				"reason":   "blocked",
			},
		}
		assert.Equal(t, "rejected", p.EffectiveDecision())
		assert.Equal(t, "call-2", p.EffectiveCallID())
		assert.Equal(t, "blocked", p.EffectiveReason())
	})

	t.Run("top-level wins over nested", func(t *testing.T) {
		t.Parallel()
		p := VerdictPayload{
			CallID: "top-level",
			Properties: map[string]any{
				"call_id": "nested",
			},
		}
		assert.Equal(t, "top-level", p.EffectiveCallID())
	})

	t.Run("empty payload yields empty effectives", func(t *testing.T) {
		t.Parallel()
		p := VerdictPayload{}
		assert.Empty(t, p.EffectiveDecision())
		assert.Empty(t, p.EffectiveCallID())
		assert.Empty(t, p.EffectiveReason())
	})
}

// --- config validation -----------------------------------------------

func TestToolCallGovernanceConfigValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     ToolCallGovernanceConfig
		wantErr bool
	}{
		{"empty defaults are valid", ToolCallGovernanceConfig{}, false},
		{"disabled", ToolCallGovernanceConfig{Mode: ToolCallGovernanceModeDisabled}, false},
		{"audit", ToolCallGovernanceConfig{Mode: ToolCallGovernanceModeAudit}, false},
		{"enforce", ToolCallGovernanceConfig{Mode: ToolCallGovernanceModeEnforce, Timeout: "500ms"}, false},
		{"unknown mode rejected", ToolCallGovernanceConfig{Mode: "permit"}, true},
		{"malformed timeout rejected", ToolCallGovernanceConfig{Mode: "enforce", Timeout: "5x"}, true},
		{"zero timeout rejected", ToolCallGovernanceConfig{Mode: "enforce", Timeout: "0s"}, true},
		{"negative timeout rejected", ToolCallGovernanceConfig{Mode: "enforce", Timeout: "-1s"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.cfg.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestToolCallGovernanceConfigEnsureDefaults(t *testing.T) {
	t.Parallel()

	cfg := ToolCallGovernanceConfig{}
	cfg.EnsureDefaults()
	assert.Equal(t, ToolCallGovernanceModeDisabled, cfg.Mode)
	assert.Equal(t, DefaultToolCallGovernanceTimeout, cfg.Timeout)
}

func TestToolCallGovernanceConfigIsEnabled(t *testing.T) {
	t.Parallel()

	assert.False(t, ToolCallGovernanceConfig{}.IsEnabled())
	assert.False(t, ToolCallGovernanceConfig{Mode: "disabled"}.IsEnabled())
	assert.True(t, ToolCallGovernanceConfig{Mode: "audit"}.IsEnabled())
	assert.True(t, ToolCallGovernanceConfig{Mode: "enforce"}.IsEnabled())
}

func TestToolCallGovernanceConfigIsEnforcing(t *testing.T) {
	t.Parallel()

	assert.False(t, ToolCallGovernanceConfig{Mode: "audit"}.IsEnforcing())
	assert.True(t, ToolCallGovernanceConfig{Mode: "enforce"}.IsEnforcing())
}

// --- helpers -------------------------------------------------------

// raceTestPublisher invokes onPublish from within PublishToStream BEFORE
// returning. This simulates the worst-case race where the verdict
// arrives at the dispatcher before Propose's select runs.
type raceTestPublisher struct {
	mu        sync.Mutex
	published []publishedVerdict
	onPublish func()
}

func (m *raceTestPublisher) PublishToStream(_ context.Context, subject string, data []byte) error {
	m.mu.Lock()
	cpy := make([]byte, len(data))
	copy(cpy, data)
	m.published = append(m.published, publishedVerdict{subject: subject, data: cpy})
	cb := m.onPublish
	m.mu.Unlock()
	if cb != nil {
		cb()
	}
	return nil
}
