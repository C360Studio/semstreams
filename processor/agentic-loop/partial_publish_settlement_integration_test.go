//go:build integration

package agenticloop

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/require"
)

// A handler result whose publish phase fails partway is commit-UNKNOWN: the
// messages before the failure already have PubAck and their consumers are
// running. Retrying that re-runs effects that already happened, and until
// deterministic tool-call identity lands the re-run mints fresh CallIDs, so
// TOOL_CALL_OUTCOMES cannot dedupe the twins. The lane must quarantine and
// stop, not blind-retry.
//
// The stream here accepts agent.> only, so the second of three publishes is
// the one that fails — the partial case, not an all-or-nothing one.
//
// spec: agentic-loop / Loop input classes settle after owner-specific durable done
func TestIntegrationPartialPublishQuarantinesRatherThanRetrying(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV(), natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: "AGENT", Subjects: []string{"agent.>"}},
	))
	ctx := t.Context()

	handler := NewMessageHandler(DefaultConfig())
	loopID, err := handler.loopManager.CreateLoop("task-partial", "general", "model", 3)
	require.NoError(t, err)
	c := releaseTestComponent(t, handler)
	c.natsClient = testClient.Client
	require.NoError(t, c.initializeKVBuckets(ctx))

	result := HandlerResult{
		LoopID: loopID,
		State:  agentic.LoopStateExploring,
		PublishedMessages: []PublishedMessage{
			{Subject: "agent.first." + loopID, Data: []byte(`{"n":1}`)},
			// No stream is bound to tool.> in this fixture: this publish fails
			// after the first one is already durable.
			{Subject: "tool.execute." + loopID, Data: []byte(`{"n":2}`)},
			{Subject: "agent.third." + loopID, Data: []byte(`{"n":3}`)},
		},
	}

	persistErr := c.persistHandlerResult(ctx, result)
	require.Error(t, persistErr)
	require.Contains(t, persistErr.Error(), "unknown durability",
		"a partial publish must be declared commit-unknown, not a bare publish error")

	// And the production lane built on it settles that way: Quarantine,
	// owner-fatal latched, negative health, exact handle drained, and the
	// message neither ACKed nor NAKed.
	var latched natsclient.DeliveryResult
	admission := newDeliveryLaneAdmission(func(r natsclient.DeliveryResult) {
		latched = r
		c.recordDeliveryOwnerFatal(r)
	})
	policy, err := newLoopHeartbeatDeliveryPolicy(ctx, natsclient.StreamConsumerConfig{
		AckWait: 2 * time.Minute, BackOff: []time.Duration{30 * time.Second, 2 * time.Minute}, MaxDeliver: 2,
	}, 15*time.Second, "agent.response", func(workCtx context.Context, _ []byte) error {
		return c.persistHandlerResult(workCtx, result)
	})
	require.NoError(t, err)

	msg := &loopDeliveryOwnerMsg{data: []byte("{}")}
	settled, admitted := consumeAdmittedDelivery(ctx, msg, policy, admission)
	require.True(t, admitted)
	require.Equal(t, natsclient.DeliveryDecisionQuarantine, settled.Decision())
	require.True(t, settled.OwnerStopRequired(), "commit-unknown must stop the exact owner")
	require.Zero(t, msg.acks.Load()+msg.naks.Load()+msg.terms.Load(),
		"a quarantined delivery attempts no terminal method at all")
	require.Contains(t, latched.Err().Error(), "unknown durability")

	health := c.Health()
	require.False(t, health.Healthy)
	require.Contains(t, health.LastError, "unknown durability")

	// The other half, corrected in round 1: a failure BEFORE the first publish
	// is safe to re-WRITE — every stamp is a whole-value Put — but the delivery
	// that would re-run it is not safe to re-deliver. The handler has already
	// moved the loop, so the redelivery is answered from its new state and the
	// result the first attempt built cannot be rebuilt. Same partial effect,
	// same quarantine, different reason; only the cause text separates them.
	c.loopsBucket = failingLoopBucket{err: errors.New("kv unavailable")}
	preMsg := &loopDeliveryOwnerMsg{data: []byte("{}")}
	prePublish, admitted := consumeAdmittedDelivery(ctx, preMsg, policy, newDeliveryLaneAdmission(nil))
	require.True(t, admitted)
	require.Equal(t, natsclient.DeliveryDecisionQuarantine, prePublish.Decision())
	require.Zero(t, preMsg.acks.Load()+preMsg.naks.Load()+preMsg.terms.Load())
	require.Contains(t, prePublish.Err().Error(), "after the loop was already mutated")
	require.NotContains(t, prePublish.Err().Error(), "published results",
		"the stamp phase and the publish phase must stay distinguishable by cause")
}
