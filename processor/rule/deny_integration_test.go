//go:build integration

// Integration tests for the deny/approve actions against real NATS + JetStream.
//
// These exercise the full verdict machinery end-to-end: CallerContext
// substitution, action short-circuit, and the governance verdict audit
// (ADR-055 §3a) — a registered verdict event published to the append-only
// GOVERNANCE_VERDICT_AUDIT stream, replacing the prior rule-ID audit triple.
//
// Per-rule revision guard against cascade-fires is unit-tested separately in
// revision_tracking_test.go (shouldSkipRule, ownRevisions injection).
//
// Build tag "integration" — requires Docker via testcontainers.
// Run with: go test -race -tags=integration ./processor/rule/...
package rule_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/governance"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/processor/rule"
)

// natsPublisher is a test-only implementation of rule.Publisher that publishes
// to a real NATS connection so the test can count deliveries via a subscription.
type natsPublisher struct {
	natsClient *natsclient.Client
}

func (p *natsPublisher) Publish(ctx context.Context, subject string, data []byte) error {
	return p.natsClient.Publish(ctx, subject, data)
}

// denyTestVerdictAuditor is a test rule.VerdictAuditor that publishes verdict
// events to the real GOVERNANCE_VERDICT_AUDIT stream via PublishToStream — the
// same wire (subject + BaseMessage envelope) the production auditor uses
// (ADR-055 §3a).
type denyTestVerdictAuditor struct {
	nc *natsclient.Client
}

func (a *denyTestVerdictAuditor) EmitVerdict(ctx context.Context, ev governance.VerdictEvent) error {
	baseMsg := message.NewBaseMessage(ev.Schema(), &ev, "rule_engine")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		return err
	}
	return a.nc.PublishToStream(ctx, governance.VerdictSubject(ev.Decision, ev.RuleID), data)
}

// publishActionForTest returns a publish action scoped to subject so test
// assertions can count NATS deliveries independently of NATS routing.
func publishActionForTest(subject string) rule.Action {
	return rule.Action{
		Type:    "publish",
		Subject: subject,
	}
}

// makeEC builds an ExecutionContext with a populated MatchState (so
// ec.RuleID() is non-empty) and the given CallerContext.
func makeEC(ruleID, entityID string, caller *rule.CallerContext) *rule.ExecutionContext {
	return &rule.ExecutionContext{
		EntityID: entityID,
		State: &rule.MatchState{
			RuleID: ruleID,
		},
		Caller: caller,
	}
}

// ----- Test 1: end-to-end deny flow ----------------------------------------

// TestIntegration_DenyFlow exercises the full deny action pipeline against
// real NATS + JetStream. Two sub-cases:
//
//   - Case A (admin caller): action list [publish, publish, publish] — no deny.
//     All 3 reach NATS, zero verdict events emitted.
//
//   - Case B (viewer caller): action list [publish, deny, publish]. The deny is
//     the second action. It fires unconditionally, short-circuits the third
//     publish, returns *DenyVerdict with $caller.id substituted into the reason,
//     and emits exactly one deny verdict event to GOVERNANCE_VERDICT_AUDIT.
//
// The audit stream is provisioned via the real config.StreamsManager, which also
// live-validates the GOVERNANCE_VERDICT_AUDIT stream config (createStream must
// accept it). Verdict events are checked two ways: a core subscription on the
// stream subject proves live delivery + decode (and lets the admin case assert
// NO verdict), and a JetStream GetLastMsgForSubject read-back proves the deny
// verdict is durably persisted and replayable — the actual audit guarantee, not
// just that a publish went out.
//
// Why two different action lists instead of a single list with a When-guarded
// deny: ActionExecutor.Execute does not evaluate When clauses — that is the
// StatefulEvaluator.runActions responsibility. Calling Execute directly bypasses
// When evaluation. Using two purpose-built action lists keeps the integration
// boundary clean and the assertions precise.
func TestIntegration_DenyFlow(t *testing.T) {
	nc := getTestNATSClient(t)
	ctx := context.Background()

	// ADR-055 §3a: provision the audit stream and wire the framework verdict
	// auditor so deny/approve emit verdict events. EnsureStreams is idempotent
	// and live-validates the GOVERNANCE_VERDICT_AUDIT stream config.
	sm := config.NewStreamsManager(nc, slog.Default())
	require.NoError(t, sm.EnsureStreams(ctx, &config.Config{}))

	pub := &natsPublisher{natsClient: nc}
	executor := rule.NewActionExecutorFull(nil, nil, pub)
	executor.SetVerdictAuditor(&denyTestVerdictAuditor{nc: nc})

	// Capture verdict events landing on the audit stream subject.
	verdictCh := make(chan *governance.VerdictEvent, 8)
	decoder := payloadbuiltins.NewTestDecoder(t)
	_, err := nc.Subscribe(ctx, "governance.verdict.>", func(_ context.Context, m *nats.Msg) {
		bm, derr := decoder.Decode(m.Data)
		if derr != nil {
			return
		}
		if ev, ok := bm.Payload().(*governance.VerdictEvent); ok {
			verdictCh <- ev
		}
	})
	require.NoError(t, err)

	// Track publish dispatches via a NATS subscription.
	var publishCount atomic.Int64
	_, err = nc.Subscribe(ctx, "deny.test.publish", func(_ context.Context, _ *nats.Msg) {
		publishCount.Add(1)
	})
	require.NoError(t, err)

	// ---- Case A: admin caller — no deny in chain ----------------------------
	t.Run("admin_caller_all_actions_run", func(t *testing.T) {
		publishCount.Store(0)

		ecAdmin := makeEC("role-gate-rule", "acme.ops.test.svc.entity.001", &rule.CallerContext{
			ID:   "admin-user-1",
			Role: "admin",
			Org:  "acme",
		})

		adminActions := []rule.Action{
			publishActionForTest("deny.test.publish"),
			publishActionForTest("deny.test.publish"),
			publishActionForTest("deny.test.publish"),
		}

		for _, act := range adminActions {
			require.NoError(t, executor.Execute(ctx, act, ecAdmin),
				"admin publish actions must not error")
		}

		// All 3 publish actions must reach NATS.
		require.Eventually(t, func() bool {
			return publishCount.Load() == 3
		}, 2*time.Second, 25*time.Millisecond, "expected 3 NATS publish deliveries for admin caller")

		// No verdict event must be emitted for an allowed (admin) caller. There
		// is no positive signal to wait for, so a bounded negative window is the
		// correct technique here.
		select {
		case ev := <-verdictCh:
			t.Fatalf("admin caller must not emit a verdict event, got %+v", ev)
		case <-time.After(300 * time.Millisecond):
		}
	})

	// ---- Case B: viewer caller — deny short-circuits in middle of chain -----
	t.Run("viewer_caller_deny_short_circuits", func(t *testing.T) {
		publishCount.Store(0)

		ecViewer := makeEC("role-gate-rule", "acme.ops.test.svc.entity.002", &rule.CallerContext{
			ID:   "viewer-user-1",
			Role: "viewer",
			Org:  "acme",
		})

		viewerActions := []rule.Action{
			publishActionForTest("deny.test.publish"),
			{Type: rule.ActionTypeDeny, Reason: "user $caller.id is not admin"},
			publishActionForTest("deny.test.publish"),
		}

		var (
			actionIdx int
			deniedErr error
		)
		for i, act := range viewerActions {
			if err := executor.Execute(ctx, act, ecViewer); err != nil {
				actionIdx = i
				deniedErr = err
				break
			}
		}

		// Must be a DenyVerdict.
		require.Error(t, deniedErr, "deny action must return an error")
		require.True(t, errors.Is(deniedErr, rule.ErrDenyVerdict),
			"error must satisfy errors.Is(err, ErrDenyVerdict): %v", deniedErr)

		var dv *rule.DenyVerdict
		require.True(t, errors.As(deniedErr, &dv), "error must be extractable as *DenyVerdict")
		assert.Equal(t, "role-gate-rule", dv.RuleID,
			"DenyVerdict.RuleID must carry originating rule ID")
		assert.Equal(t, "user viewer-user-1 is not admin", dv.Reason,
			"$caller.id must be substituted in the deny reason")

		// Deny is the second action (index 1); the loop must have broken there.
		assert.Equal(t, 1, actionIdx, "loop must break at the deny action (index 1)")

		// Only the first publish ran before the deny.
		require.Eventually(t, func() bool {
			return publishCount.Load() >= 1
		}, 2*time.Second, 25*time.Millisecond, "first publish must reach NATS before deny")

		// Deliberate negative-assertion sleep: prove the third publish did NOT fire
		// after the deny short-circuit. There is no positive signal to wait for,
		// so a bounded window is the correct technique here (not an anti-pattern).
		time.Sleep(300 * time.Millisecond)
		assert.Equal(t, int64(1), publishCount.Load(),
			"exactly one publish must run; the action after deny must be skipped")

		// Exactly one deny verdict event must be delivered live on the audit
		// subject, carrying the substituted reason + the originating rule and
		// entity (ADR-055 §3a).
		select {
		case ev := <-verdictCh:
			assert.Equal(t, governance.DecisionDeny, ev.Decision,
				"verdict event decision must be deny")
			assert.Equal(t, "role-gate-rule", ev.RuleID,
				"verdict event must carry the originating rule ID")
			assert.Equal(t, "user viewer-user-1 is not admin", ev.Reason,
				"verdict event reason must carry the substituted reason")
			assert.Equal(t, "acme.ops.test.svc.entity.002", ev.EntityID,
				"verdict event must carry the entity ID")
		case <-time.After(2 * time.Second):
			t.Fatal("deny action must emit exactly one verdict event to the audit stream")
		}

		// Persistence gate: the verdict must be DURABLY recorded on the stream,
		// not merely published. PublishToStream returns only after the PubAck, so
		// by now the record is persisted; read the last message on the deny
		// subject back and decode it. Robust to testcontainer reuse —
		// GetLastMsgForSubject returns the most recent (this run's) event, and
		// the reason is deterministic.
		js, jerr := nc.JetStream()
		require.NoError(t, jerr)
		stream, serr := js.Stream(ctx, "GOVERNANCE_VERDICT_AUDIT")
		require.NoError(t, serr)
		raw, gerr := stream.GetLastMsgForSubject(ctx, governance.VerdictSubject(governance.DecisionDeny, "role-gate-rule"))
		require.NoError(t, gerr, "deny verdict must be persisted + replayable on the audit stream")
		persisted, derr := decoder.Decode(raw.Data)
		require.NoError(t, derr, "persisted record must decode through the production registry")
		pev, ok := persisted.Payload().(*governance.VerdictEvent)
		require.True(t, ok, "persisted payload must be a *VerdictEvent, got %T", persisted.Payload())
		assert.Equal(t, governance.DecisionDeny, pev.Decision)
		assert.Equal(t, "user viewer-user-1 is not admin", pev.Reason,
			"the persisted (replayable) record must carry this run's deny reason")
	})
}
