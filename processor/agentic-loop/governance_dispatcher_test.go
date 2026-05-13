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
		{ID: "c1", Name: "bash", Arguments: map[string]any{"command": "ls /tmp"}},
		{ID: "c2", Name: "http_request", Arguments: map[string]any{"url": "https://example.com"}},
	}
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

	calls := []agentic.ToolCall{{ID: "c1", Name: "bash"}}
	result, err := d.Propose(context.Background(), "loop-1", "", calls)
	require.NoError(t, err, "audit publish failure must not propagate")
	assert.Equal(t, calls, result.Approved, "audit must still pass calls through even when publish fails")
}

// --- enforce mode ---------------------------------------------------

func TestDispatcher_EnforceModeWaitsForApproveVerdict(t *testing.T) {
	t.Parallel()

	pub := &mockVerdictPublisher{}
	d := NewGovernanceDispatcher(
		ToolCallGovernanceConfig{Mode: ToolCallGovernanceModeEnforce, Timeout: "2s"},
		pub, slog.Default(), nil,
	)

	calls := []agentic.ToolCall{{ID: "call-001", Name: "bash"}}

	// Simulate an approve verdict arriving 50ms after Propose starts.
	// HandleVerdict runs on a separate goroutine (in production, the
	// JetStream callback). The buffered waiter channel absorbs the
	// send even if Propose hasn't entered its select yet.
	go func() {
		time.Sleep(50 * time.Millisecond)
		payload, _ := json.Marshal(VerdictPayload{
			Decision: "approved", RuleID: "rule-allow", Reason: "policy permits",
		})
		d.HandleVerdict("approved", "call-001", payload)
	}()

	result, err := d.Propose(context.Background(), "loop-1", "", calls)
	require.NoError(t, err)

	assert.Len(t, result.Approved, 1)
	assert.Empty(t, result.Rejected)
	assert.Equal(t, "call-001", result.Approved[0].ID)
}

func TestDispatcher_EnforceModeRejectsOnDenyVerdict(t *testing.T) {
	t.Parallel()

	pub := &mockVerdictPublisher{}
	d := NewGovernanceDispatcher(
		ToolCallGovernanceConfig{Mode: ToolCallGovernanceModeEnforce, Timeout: "2s"},
		pub, slog.Default(), nil,
	)

	calls := []agentic.ToolCall{{ID: "call-001", Name: "bash"}}

	go func() {
		time.Sleep(50 * time.Millisecond)
		payload, _ := json.Marshal(VerdictPayload{
			Decision: "rejected", RuleID: "block-bash", Reason: "bash disallowed",
		})
		d.HandleVerdict("rejected", "call-001", payload)
	}()

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

	calls := []agentic.ToolCall{{ID: "call-001", Name: "bash"}}

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

	pub := &mockVerdictPublisher{}
	d := NewGovernanceDispatcher(
		ToolCallGovernanceConfig{Mode: ToolCallGovernanceModeEnforce, Timeout: "2s"},
		pub, slog.Default(), nil,
	)

	calls := []agentic.ToolCall{
		{ID: "c1", Name: "bash"},
		{ID: "c2", Name: "http_request"},
		{ID: "c3", Name: "bash"},
	}

	// Race-fix: send the verdicts AFTER Propose has registered all
	// waiters. 50ms slop is sufficient because Propose pre-registers
	// every waiter synchronously before publish completes.
	go func() {
		time.Sleep(50 * time.Millisecond)
		// Reverse order on purpose to confirm the dispatcher
		// re-orders by request, not by arrival.
		approvedPayload, _ := json.Marshal(VerdictPayload{Decision: "approved"})
		rejectedPayload, _ := json.Marshal(VerdictPayload{Decision: "rejected", Reason: "blocked"})
		d.HandleVerdict("approved", "c3", approvedPayload)
		d.HandleVerdict("rejected", "c2", rejectedPayload)
		d.HandleVerdict("approved", "c1", approvedPayload)
	}()

	result, err := d.Propose(context.Background(), "loop-1", "", calls)
	require.NoError(t, err)

	require.Len(t, result.Approved, 2)
	require.Len(t, result.Rejected, 1)
	assert.Equal(t, "c1", result.Approved[0].ID, "approved ordering preserved")
	assert.Equal(t, "c3", result.Approved[1].ID)
	assert.Equal(t, "c2", result.Rejected[0].Call.ID)
}

// Publish failure on a single call in enforce mode becomes a per-call
// rejection — the rest of the batch continues with their waiters
// intact. This is the architectural commitment that publish failure
// degrades to fail-closed gracefully rather than wedging the loop.
func TestDispatcher_EnforceModePartialPublishFailure(t *testing.T) {
	t.Parallel()

	// Custom publisher that fails on the second call only.
	pub := &selectiveFailPublisher{failCallIDPredicate: func(callID string) bool {
		return callID == "c2"
	}}
	d := NewGovernanceDispatcher(
		ToolCallGovernanceConfig{Mode: ToolCallGovernanceModeEnforce, Timeout: "1s"},
		pub, slog.Default(), nil,
	)

	calls := []agentic.ToolCall{
		{ID: "c1", Name: "bash"},
		{ID: "c2", Name: "bash"},
	}

	go func() {
		time.Sleep(30 * time.Millisecond)
		payload, _ := json.Marshal(VerdictPayload{Decision: "approved"})
		// Only c1 will have a verdict subscribe path — c2's publish failed.
		d.HandleVerdict("approved", "c1", payload)
	}()

	result, err := d.Propose(context.Background(), "loop-1", "", calls)
	require.NoError(t, err)

	require.Len(t, result.Approved, 1)
	assert.Equal(t, "c1", result.Approved[0].ID)
	require.Len(t, result.Rejected, 1)
	assert.Equal(t, "c2", result.Rejected[0].Call.ID)
	assert.Contains(t, result.Rejected[0].Reason, "publish failed")
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

	calls := []agentic.ToolCall{{ID: "fast-call", Name: "bash"}}

	// raceTestPublisher fires the verdict from INSIDE PublishToStream —
	// before Propose returns from publish and enters the select. The
	// buffered waiter channel must absorb this.
	pub.onPublish = func() {
		payload, _ := json.Marshal(VerdictPayload{Decision: "approved", RuleID: "fast-rule"})
		d.HandleVerdict("approved", "fast-call", payload)
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
		[]agentic.ToolCall{{ID: "late-call", Name: "bash"}})
	require.NoError(t, err)
	require.Len(t, result.Rejected, 1)

	// Now fire a late verdict — must not panic.
	payload, _ := json.Marshal(VerdictPayload{Decision: "approved"})
	d.HandleVerdict("approved", "late-call", payload)
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

	pub := &mockVerdictPublisher{}
	mx := &mockDispatcherMetrics{}
	d := NewGovernanceDispatcher(
		ToolCallGovernanceConfig{Mode: ToolCallGovernanceModeEnforce, Timeout: "2s"},
		pub, slog.Default(), mx,
	)

	calls := []agentic.ToolCall{{ID: "c1", Name: "bash"}}
	go func() {
		time.Sleep(30 * time.Millisecond)
		payload, _ := json.Marshal(VerdictPayload{Decision: "approved"})
		d.HandleVerdict("approved", "c1", payload)
	}()

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

	calls := []agentic.ToolCall{{ID: "c1", Name: "bash"}}
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
		[]agentic.ToolCall{{ID: "late-call", Name: "bash"}})
	require.NoError(t, err)

	// Late verdict — waiter already released by defer. Must increment
	// the missing-waiter counter, not panic.
	payload, _ := json.Marshal(VerdictPayload{Decision: "approved"})
	d.HandleVerdict("approved", "late-call", payload)

	assert.Equal(t, 1, mx.missingWaiterCalls,
		"late verdict for released waiter must increment subscribe-before-publish counter")
}

// --- subject parsing -------------------------------------------------

func TestDecisionFromVerdictSubject(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		subject string
		want    string
	}{
		{"canonical approved", "agent.toolcall.approved.loop-1.call-001", "approved"},
		{"canonical rejected", "agent.toolcall.rejected.loop-1.call-001", "rejected"},
		{"unrelated subject — empty", "agent.task.review", ""},
		{"wrong verb — empty", "agent.toolcall.observed.loop-1.call-1", ""},
		{"three segments still parses", "agent.toolcall.approved", "approved"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := decisionFromVerdictSubject(tt.subject)
			assert.Equal(t, tt.want, got)
		})
	}
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

// selectiveFailPublisher fails on calls whose call_id matches the
// predicate. The mock builds the proposed subject as
// "agent.toolcall.proposed.<loop>" — the publisher parses the payload
// to extract call_id and decide whether to fail.
type selectiveFailPublisher struct {
	mu                  sync.Mutex
	published           []publishedVerdict
	failCallIDPredicate func(callID string) bool
}

func (m *selectiveFailPublisher) PublishToStream(_ context.Context, subject string, data []byte) error {
	// Bytes are BaseMessage-wrapped (wire envelope around GenericJSONPayload).
	// Extract the proposed-call payload via the wire envelope so the
	// per-call_id predicate can decide whether to fail.
	var envelope struct {
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(data, &envelope); err == nil {
		var generic struct {
			Data ProposedToolCallPayload `json:"data"`
		}
		if err := json.Unmarshal(envelope.Payload, &generic); err == nil {
			if m.failCallIDPredicate != nil && m.failCallIDPredicate(generic.Data.CallID) {
				return errors.New("selective publish failure")
			}
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cpy := make([]byte, len(data))
	copy(cpy, data)
	m.published = append(m.published, publishedVerdict{subject: subject, data: cpy})
	return nil
}

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
