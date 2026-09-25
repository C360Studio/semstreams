//go:build integration

package agenticloop

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

// The two tests below are the wiring tests for this component's two refusal
// declarers (#1342, obligation inherited from #1341): one per NewAdmission call
// site in setupConsumer. Every other refusal test calls a recorder directly or
// builds its own Admission, so passing nil as onRefused at either call site
// survives them all. Each test drives the production callback setupConsumer
// builds, over real NATS, into a latched lane and reads this component's own
// refusal counter.

// The settlement-only call site: its lanes allow ten deliveries in flight, so
// the refused delivery is a genuinely buffered one — the server has sent it
// before the first delivery latches the lane, and the drained handle flushes it.
//
// spec: jetstream-consumer-policy / control loss shuts down through the existing exact owner
func TestIntegrationSettlementLaneDeclaresBufferedRefusal(t *testing.T) {
	const lane = "agent.approval_response"
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: "AGENT", Subjects: []string{"agent.>"}},
	))
	c := newRefusalWiringLoop(t, tc)
	loopID := c.handler.loopManager.GenerateLoopID()
	// Without a loop manager the approval handler panics, which the lane
	// settles as Quarantine: the first delivery is the one that latches.
	c.handler.loopManager = nil

	var (
		laneCfg    natsclient.StreamConsumerConfig
		laneHandle jetstream.ConsumeContext
	)
	c.consumeStream = func(
		setupCtx, handlerCtx context.Context,
		owner natsclient.PortConsumerContext,
		cfg natsclient.StreamConsumerConfig,
		callback func(context.Context, jetstream.Msg),
	) (jetstream.ConsumeContext, error) {
		laneCfg = cfg
		handle, err := tc.Client.ConsumeStreamWithConfigContexts(
			setupCtx, handlerCtx, owner, cfg, holdFirstUntilDelivered(tc.Client, cfg, 2, callback))
		laneHandle = handle
		return handle, err
	}
	ctx := setupRefusalWiringLane(t, c, lane, nil, c.handleApprovalResponseMessage)
	counter := c.metrics.deliveryRefusals.WithLabelValues(lane)
	before := testutil.ToFloat64(counter)

	for _, callID := range []string{"call-latching", "call-buffered"} {
		response := &agentic.ApprovalResponse{
			LoopID: loopID, CallID: callID, Decision: agentic.ApprovalDecisionApprove,
			ApprovedBy: "operator", DecidedAt: time.Now().UTC(),
		}
		data, err := json.Marshal(message.NewBaseMessage(response.Schema(), response, "test"))
		require.NoError(t, err)
		require.NoError(t, tc.Client.PublishToStream(ctx, lane+"."+callID, data))
	}

	awaitClosed(t, laneHandle)
	require.Equal(t, before+1, testutil.ToFloat64(counter),
		"the buffered delivery the drain flushed must reach this lane's own refusal declarer")
	requireUnsettled(t, tc, laneCfg, 2)
}

// The heartbeat call site: agent.task, agent.response and tool.result are
// pinned at one delivery in flight (agenticLoopConsumerPolicy), so the server
// never sends a second delivery while the latching one stays unsettled, and no
// buffered delivery can reach this lane's closed admission. The test therefore
// hands the production callback the same real message a second time after the
// latch — the shape of a redelivery arriving at a lane that has already closed.
// The latch itself is the delivery-metadata failure the typed heartbeat helper
// answers with owner stop.
//
// spec: jetstream-consumer-policy / control loss shuts down through the existing exact owner
func TestIntegrationHeartbeatLaneDeclaresRefusalAfterLatch(t *testing.T) {
	const lane = "agent.task"
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: "AGENT", Subjects: []string{"agent.>"}},
	))
	c := newRefusalWiringLoop(t, tc)

	var (
		laneCfg    natsclient.StreamConsumerConfig
		laneHandle jetstream.ConsumeContext
	)
	c.consumeStream = func(
		setupCtx, handlerCtx context.Context,
		owner natsclient.PortConsumerContext,
		cfg natsclient.StreamConsumerConfig,
		callback func(context.Context, jetstream.Msg),
	) (jetstream.ConsumeContext, error) {
		laneCfg = cfg
		var once sync.Once
		handle, err := tc.Client.ConsumeStreamWithConfigContexts(setupCtx, handlerCtx, owner, cfg,
			func(msgCtx context.Context, msg jetstream.Msg) {
				once.Do(func() {
					callback(msgCtx, metadataUnavailableMsg{Msg: msg})
				})
				callback(msgCtx, msg)
			})
		laneHandle = handle
		return handle, err
	}
	ctx := setupRefusalWiringLane(t, c, lane, c.taskInputHandler(30*time.Minute), nil)
	counter := c.metrics.deliveryRefusals.WithLabelValues(lane)
	before := testutil.ToFloat64(counter)

	require.NoError(t, tc.Client.PublishToStream(ctx, lane+".latching", []byte(`{}`)))

	awaitClosed(t, laneHandle)
	require.Equal(t, before+1, testutil.ToFloat64(counter),
		"a delivery reaching the latched lane must reach this lane's own refusal declarer")
	require.Equal(t, "delivery ownership lost", c.Health().Status)
	requireUnsettled(t, tc, laneCfg, 1)
}

func newRefusalWiringLoop(t *testing.T, tc *natsclient.TestClient) *Component {
	t.Helper()
	discoverable, err := NewComponent([]byte(`{}`), component.Dependencies{
		NATSClient: tc.Client, PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
	})
	require.NoError(t, err)
	c := discoverable.(*Component)
	c.started = true
	c.startTime = time.Now()
	return c
}

// setupRefusalWiringLane binds one input lane through the production
// setupConsumer — the function that holds both NewAdmission call sites — and
// registers the cleanup that drains every handle and joins every observer.
func setupRefusalWiringLane(
	t *testing.T,
	c *Component,
	lane string,
	handler inputHandler,
	settle func(context.Context, []byte) (natsclient.DeliveryDecision, error),
) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	port, err := (component.PortDefinition{
		Name:     lane,
		Config:   component.JetStreamPort{StreamName: "AGENT", Subjects: []string{lane + ".>"}},
		Required: true,
	}).Resolve(component.DirectionInput)
	require.NoError(t, err)
	require.NoError(t, c.setupConsumer(ctx, ctx, port, lane+".>", handler, settle))
	require.Len(t, c.consumers, 1)
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
	return ctx
}

func awaitClosed(t *testing.T, handle jetstream.ConsumeContext) {
	t.Helper()
	require.NotNil(t, handle, "production setup did not bind the lane")
	select {
	case <-handle.Closed():
	case <-time.After(10 * time.Second):
		t.Fatal("the latched lane's exact handle was not drained")
	}
}

// requireUnsettled asserts the lane settled nothing: the latching delivery
// stopped the owner without a terminal method, and the refused one attempted
// none either.
func requireUnsettled(t *testing.T, tc *natsclient.TestClient, cfg natsclient.StreamConsumerConfig, delivered int) {
	t.Helper()
	stream, err := tc.Client.GetStream(t.Context(), cfg.StreamName)
	require.NoError(t, err)
	consumer, err := stream.Consumer(t.Context(), cfg.ConsumerName)
	require.NoError(t, err)
	info, err := consumer.Info(t.Context())
	require.NoError(t, err)
	require.Equal(t, uint64(delivered), info.Delivered.Consumer)
	require.Zero(t, info.AckFloor.Consumer)
	require.Equal(t, delivered, info.NumAckPending)
}

// metadataUnavailableMsg is a real delivery whose server metadata cannot be
// read, which the typed heartbeat helper answers with Quarantine and owner stop
// before any work runs.
type metadataUnavailableMsg struct{ jetstream.Msg }

func (metadataUnavailableMsg) Metadata() (*jetstream.MsgMetadata, error) {
	return nil, errors.New("injected: delivery metadata unavailable")
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
