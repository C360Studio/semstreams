//go:build integration

package agenticgovernance

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

// TestIntegrationLatchedLaneDeclaresBufferedRefusal is the wiring test for the
// refusal declarer (#1342, obligation inherited from #1341). Every other
// refusal test calls the recorder directly or builds its own Admission, so
// passing nil as onRefused at the setupConsumer call site survives them all.
// Here the production callback, built by setupInputConsumers over real NATS,
// latches on its first delivery while the server has already sent it a second;
// the drained handle flushes that second delivery into the closed lane, and
// only the call site's own declarer can move this component's counter.
//
// spec: jetstream-consumer-policy / control loss shuts down through the existing exact owner
func TestIntegrationLatchedLaneDeclaresBufferedRefusal(t *testing.T) {
	const lane = "task_validation"
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: "AGENT", Subjects: []string{"agent.>"}},
	))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	discoverable, err := NewComponent([]byte(`{}`), component.Dependencies{NATSClient: tc.Client})
	require.NoError(t, err)
	c := discoverable.(*Component)
	// With no chain every admitted delivery panics in work, which the lane
	// settles as Quarantine: the first delivery is the one that latches.
	c.chain = nil

	var (
		laneCfg    natsclient.StreamConsumerConfig
		laneHandle jetstream.ConsumeContext
	)
	c.consumeStream = func(
		consumeCtx context.Context,
		owner natsclient.PortConsumerContext,
		cfg natsclient.StreamConsumerConfig,
		callback func(context.Context, jetstream.Msg),
	) (jetstream.ConsumeContext, error) {
		if owner.Port != lane {
			return tc.Client.ConsumeStreamWithConfig(consumeCtx, owner, cfg, callback)
		}
		laneCfg = cfg
		handle, consumeErr := tc.Client.ConsumeStreamWithConfig(
			consumeCtx, owner, cfg, holdFirstUntilDelivered(tc.Client, cfg, 2, callback))
		laneHandle = handle
		return handle, consumeErr
	}
	require.NoError(t, c.setupInputConsumers(ctx))
	require.NotNil(t, laneHandle, "production setup did not bind %s", lane)
	t.Cleanup(func() {
		for _, binding := range c.consumers {
			binding.Drain()
			<-binding.Closed()
		}
		cancel()
		for _, binding := range c.consumers {
			<-binding.Done()
		}
	})

	counter := c.metrics.deliveryRefusals.WithLabelValues(lane)
	before := testutil.ToFloat64(counter)
	for _, id := range []string{"latching", "buffered"} {
		data, marshalErr := json.Marshal(Message{ID: id, Content: Content{Text: "clean"}})
		require.NoError(t, marshalErr)
		require.NoError(t, tc.Client.PublishToStream(ctx, "agent.task."+id, data))
	}

	select {
	case <-laneHandle.Closed():
	case <-time.After(10 * time.Second):
		t.Fatal("the latched lane's exact handle was not drained")
	}
	require.Equal(t, before+1, testutil.ToFloat64(counter),
		"the buffered delivery the drain flushed must reach this lane's own refusal declarer")

	// Neither delivery was settled: the latching one quarantined, the refused
	// one attempted no terminal method.
	stream, err := tc.Client.GetStream(ctx, laneCfg.StreamName)
	require.NoError(t, err)
	consumer, err := stream.Consumer(ctx, laneCfg.ConsumerName)
	require.NoError(t, err)
	info, err := consumer.Info(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(2), info.Delivered.Consumer)
	require.Zero(t, info.AckFloor.Consumer)
	require.Equal(t, 2, info.NumAckPending)
}

// holdFirstUntilDelivered holds the lane's first delivery out of the
// production callback until the server reports want deliveries to this
// consumer, so every later delivery is already on its way to the client when
// the first one latches the lane. That is what makes the flush of a buffered
// delivery into a drained lane deterministic rather than a race with the drain.
// A failed wait only releases the hold; the test's counter assertion is what
// fails.
func holdFirstUntilDelivered(
	client *natsclient.Client,
	cfg natsclient.StreamConsumerConfig,
	want uint64,
	callback func(context.Context, jetstream.Msg),
) func(context.Context, jetstream.Msg) {
	var once sync.Once
	return func(msgCtx context.Context, msg jetstream.Msg) {
		once.Do(func() { awaitServerDelivered(msgCtx, client, cfg, want) })
		callback(msgCtx, msg)
	}
}

func awaitServerDelivered(ctx context.Context, client *natsclient.Client, cfg natsclient.StreamConsumerConfig, want uint64) {
	stream, err := client.GetStream(ctx, cfg.StreamName)
	if err != nil {
		return
	}
	consumer, err := stream.Consumer(ctx, cfg.ConsumerName)
	if err != nil {
		return
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		info, infoErr := consumer.Info(ctx)
		if infoErr == nil && info.Delivered.Consumer >= want {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
