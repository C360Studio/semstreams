//go:build integration

package agentrun_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/agentic/agentrun"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/lifecycle"
)

// TestIntegration_MilestoneLanesDeclareFiniteMaxDeliver is invariant I6, read
// off the consumers NATS actually holds rather than off a config literal a test
// wrote for itself.
//
// MaxDeliver 0 means unlimited in JetStream, so drift back to it is a one-token
// edit that turns a poison milestone into an infinite redelivery loop with no
// exhaustion advisory — the loop is silent, and the counter the owner chose as
// the stuck-milestone signal (semstreams_nats_max_delivery_exhaustions_total,
// ruling Q2) never fires. The bound is 5 rather than the port default 3 because
// each attempt re-runs every registered handler against one 30s deadline.
//
// The heartbeat ceiling is proven by construction on the same path: Start
// validates a 10s heartbeat against each lane's AckWait through
// ValidateHeartbeatDeliveryPolicy BEFORE acquiring the consumer, so a heartbeat
// above AckWait/2 fails Start rather than letting a lease expire mid-fanout.
// A successful Start here is that check passing for both lanes.
func TestIntegration_MilestoneLanesDeclareFiniteMaxDeliver(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithKVBuckets(graph.BucketEntityStates))
	ctx := t.Context()

	_, err := tc.CreateStream(ctx, agentrun.AgentStreamName, []string{"agent.>"})
	require.NoError(t, err, "create AGENT stream")

	mgr := lifecycle.NewManager(tc.Client, nil)
	require.NoError(t, agentrun.Register(mgr))
	sub := agentrun.NewMilestoneSubscriber(mgr, nil, "acme", "ops", nil)

	stop, err := sub.Start(ctx, tc.Client, agentrun.StartConfig{
		StreamName:         agentrun.AgentStreamName,
		ConsumerNameSuffix: "policy",
	})
	require.NoError(t, err, "Start validates both lanes' heartbeat against their AckWait")
	require.NotNil(t, stop)
	defer func() { require.NoError(t, stop(ctx)) }()

	js, err := tc.Client.JetStream()
	require.NoError(t, err)

	for _, lane := range []string{"complete", "failed"} {
		t.Run(lane, func(t *testing.T) {
			consumer, consumerErr := js.Consumer(ctx, agentrun.AgentStreamName, "agentrun-milestone-"+lane+"-policy")
			require.NoError(t, consumerErr, "the %s lane declared no durable consumer", lane)
			info, infoErr := consumer.Info(ctx)
			require.NoError(t, infoErr)

			assert.Positive(t, info.Config.MaxDeliver,
				"MaxDeliver 0 is unlimited: a poison milestone would redeliver forever, unsignalled")
			assert.Equal(t, 5, info.Config.MaxDeliver, "the ruled redelivery bound (Q2, 2026-09-18)")
			assert.Equal(t, 30*time.Second, info.Config.AckWait, "the fanout deadline one attempt runs against")
			assert.Empty(t, info.Config.BackOff,
				"the retry delay is semantic (DelayedDeliveryRetry), not a consumer BackOff ladder")
		})
	}
}
