//go:build integration

// Integration tests for the deny action against real NATS + KV.
//
// These tests exercise the full deny machinery end-to-end: CallerContext
// substitution, action short-circuit, and audit triple write.
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
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/processor/rule"
)

// natsTripleMutator is a test-only implementation of rule.TripleMutator
// that performs real NATS request/reply against a test responder.
type natsTripleMutator struct {
	natsClient *natsclient.Client
}

// natsPublisher is a test-only implementation of rule.Publisher that
// publishes to a real NATS connection. Used in TestIntegration_DenyFlow to
// let publish actions actually reach the NATS broker so the test can count
// deliveries via a subscription.
type natsPublisher struct {
	natsClient *natsclient.Client
}

func (p *natsPublisher) Publish(ctx context.Context, subject string, data []byte) error {
	return p.natsClient.Publish(ctx, subject, data)
}

func (m *natsTripleMutator) AddTriple(ctx context.Context, _ string, triple message.Triple) (uint64, error) {
	req := gtypes.AddTripleRequest{Triple: triple}
	data, err := json.Marshal(req)
	if err != nil {
		return 0, err
	}

	respData, err := m.natsClient.RequestWithRetry(
		ctx,
		rule.SubjectTripleAdd,
		data,
		5*time.Second,
		natsclient.DefaultRetryConfig(),
	)
	if err != nil {
		return 0, err
	}

	var resp gtypes.AddTripleResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return 0, err
	}
	if !resp.Success {
		return 0, errors.New(resp.Error)
	}

	return resp.KVRevision, nil
}

func (m *natsTripleMutator) RemoveTriple(ctx context.Context, _ string, subject, predicate string) (uint64, error) {
	req := gtypes.RemoveTripleRequest{Subject: subject, Predicate: predicate}
	data, err := json.Marshal(req)
	if err != nil {
		return 0, err
	}

	respData, err := m.natsClient.RequestWithRetry(
		ctx,
		rule.SubjectTripleRemove,
		data,
		5*time.Second,
		natsclient.DefaultRetryConfig(),
	)
	if err != nil {
		return 0, err
	}

	var resp gtypes.RemoveTripleResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return 0, err
	}
	if !resp.Success {
		return 0, errors.New(resp.Error)
	}
	return resp.KVRevision, nil
}

// startGraphResponder registers NATS request/reply responders for both
// triple mutation subjects. The add-responder stores triples in collector
// and returns a synthetic KVRevision (auto-incrementing). The remove-
// responder simply acknowledges. Both reply with Success=true.
//
// Returns a channel that receives every triple added (for assertions).
// Cleanup is handled by testcontainer teardown at test end.
func startGraphResponder(t *testing.T, nc *natsclient.Client) (added chan message.Triple) {
	t.Helper()
	ctx := context.Background()

	ch := make(chan message.Triple, 64)
	var revision atomic.Uint64

	addHandler := func(_ context.Context, data []byte) ([]byte, error) {
		var req gtypes.AddTripleRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return nil, err
		}
		rev := revision.Add(1)
		ch <- req.Triple
		resp := gtypes.AddTripleResponse{
			MutationResponse: gtypes.MutationResponse{
				Success:    true,
				KVRevision: rev,
				Timestamp:  time.Now().UnixNano(),
			},
		}
		return json.Marshal(resp)
	}

	removeHandler := func(_ context.Context, data []byte) ([]byte, error) {
		var req gtypes.RemoveTripleRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return nil, err
		}
		rev := revision.Add(1)
		resp := gtypes.RemoveTripleResponse{
			MutationResponse: gtypes.MutationResponse{
				Success:    true,
				KVRevision: rev,
				Timestamp:  time.Now().UnixNano(),
			},
		}
		return json.Marshal(resp)
	}

	_, err := nc.SubscribeForRequests(ctx, rule.SubjectTripleAdd, addHandler)
	require.NoError(t, err, "failed to subscribe to triple add subject")

	_, err = nc.SubscribeForRequests(ctx, rule.SubjectTripleRemove, removeHandler)
	require.NoError(t, err, "failed to subscribe to triple remove subject")

	return ch
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

// drainTriples reads up to n triples from ch within timeout, returning
// however many arrived. Used to assert both "some triples arrived" and
// "no triples arrived" without relying on arbitrary sleeps.
func drainTriples(ch chan message.Triple, n int, timeout time.Duration) []message.Triple {
	var out []message.Triple
	deadline := time.After(timeout)
	for {
		if len(out) >= n {
			return out
		}
		select {
		case t := <-ch:
			out = append(out, t)
		case <-deadline:
			return out
		}
	}
}

// ----- Test 1: end-to-end deny flow ----------------------------------------

// TestIntegration_DenyFlow exercises the full deny action pipeline against
// real NATS + JetStream. Two sub-cases:
//
//   - Case A (admin caller): action list [publish, publish, publish] — no deny.
//     All 3 reach NATS, zero audit triples written. Verifies the executor +
//     publisher + triple mutator are all correctly wired to real NATS.
//
//   - Case B (viewer caller): action list [publish, deny, publish]. The deny is
//     the second action. It fires unconditionally, short-circuits the third
//     publish, returns *DenyVerdict with $caller.id substituted into the reason,
//     and writes exactly one rule.deny audit triple to the graph responder.
//
// "Graph integration" is provided by a lightweight in-process NATS responder
// (startGraphResponder) — no graph-gateway process is needed.
//
// Why two different action lists instead of a single list with a When-guarded
// deny: ActionExecutor.Execute does not evaluate When clauses — that is the
// StatefulEvaluator.runActions responsibility. Calling Execute directly (Option
// A from the chunk spec) bypasses When evaluation. Using two purpose-built
// action lists keeps the integration boundary clean and the assertions precise.
func TestIntegration_DenyFlow(t *testing.T) {
	nc := getTestNATSClient(t)
	ctx := context.Background()

	addedCh := startGraphResponder(t, nc)

	mut := &natsTripleMutator{natsClient: nc}
	pub := &natsPublisher{natsClient: nc}
	executor := rule.NewActionExecutorFull(nil, mut, pub)

	// Track publish dispatches via a NATS subscription.
	var publishCount atomic.Int64
	_, err := nc.Subscribe(ctx, "deny.test.publish", func(_ context.Context, _ *nats.Msg) {
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

		// No audit triple must have been written.
		written := drainTriples(addedCh, 1, 200*time.Millisecond)
		assert.Empty(t, written, "admin caller must not produce a rule.deny audit triple")
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

		// Exactly one rule.deny audit triple must reach the graph responder.
		auditTriples := drainTriples(addedCh, 1, 2*time.Second)
		require.Len(t, auditTriples, 1,
			"deny action must write exactly one audit triple")

		at := auditTriples[0]
		assert.Equal(t, rule.PredicateRuleDeny, at.Predicate,
			"audit triple predicate must be %q", rule.PredicateRuleDeny)
		assert.Equal(t, "user viewer-user-1 is not admin", at.Object,
			"audit triple object must carry the substituted reason")
		assert.Equal(t, "role-gate-rule", at.Subject,
			"audit triple subject must be the rule ID (from ec.RuleID())")
	})
}
