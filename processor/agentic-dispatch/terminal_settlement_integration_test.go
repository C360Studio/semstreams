//go:build integration

package agenticdispatch

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

func TestIntegrationTerminalSettlementRestartRouteStableDedupAndUnlimitedAttempts(t *testing.T) {
	ctx := t.Context()
	tc := natsclient.NewTestClient(t,
		natsclient.WithKVBuckets(defaultAgentLoopsBucket(t)),
		natsclient.WithStreams(
			natsclient.TestStreamConfig{Name: "AGENT_TERMINAL", Subjects: []string{"agent.>"}},
			natsclient.TestStreamConfig{Name: "USER_TERMINAL", Subjects: []string{"user.>"}},
		),
	)
	reg := payloadregistry.NewWithSubset(t, agentic.RegisterPayloads)
	c := &Component{
		config: DefaultConfig(), decoder: message.NewDecoder(reg), natsClient: tc.Client,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)), loopTracker: NewLoopTracker(), metrics: getMetrics(nil),
	}
	// The checked production response output is USER; this fixture binds that
	// existing port to the independently named test stream without changing its
	// subject contract.
	c.config.Ports.Outputs[2].Config = component.JetStreamPort{Subjects: []string{"user.response.>"}, StreamName: "USER_TERMINAL"}

	kv, err := tc.GetKVBucket(ctx, defaultAgentLoopsBucket(t))
	require.NoError(t, err)
	loop := agentic.LoopEntity{
		ID: "restart-loop", TaskID: "restart-task", State: agentic.LoopStateComplete, MaxIterations: 3,
		ChannelType: "http", ChannelID: "restart-channel",
	}
	loopData, err := json.Marshal(loop)
	require.NoError(t, err)
	_, err = kv.Put(ctx, loop.ID, loopData)
	require.NoError(t, err)

	at := time.Now().UTC()
	terminal := &agentic.LoopCompletedEvent{
		LoopID: loop.ID, TaskID: loop.TaskID, Outcome: agentic.OutcomeSuccess, Result: "restart result", CompletedAt: at,
	}
	terminalData := terminalEnvelopeForDispatch(t, terminal)
	require.NoError(t, c.settleAgentTerminal(ctx, terminalData), "empty process tracker must recover route from AGENT_LOOPS")
	require.NoError(t, c.settleAgentTerminal(ctx, terminalData), "redelivery must reuse stable response MsgID")

	userStream, err := tc.Client.GetStream(ctx, "USER_TERMINAL")
	require.NoError(t, err)
	userInfo, err := userStream.Info(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), userInfo.State.Msgs, "stable Nats-Msg-Id must deduplicate a redelivery inside USER window")

	agentStream, err := tc.Client.GetStream(ctx, "AGENT_TERMINAL")
	require.NoError(t, err)
	consumer, err := agentStream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Name: "unlimited", FilterSubject: "agent.complete.unlimited", AckPolicy: jetstream.AckExplicitPolicy,
		AckWait: time.Second, MaxDeliver: 0,
	})
	require.NoError(t, err)
	require.NoError(t, tc.Client.PublishToStream(ctx, "agent.complete.unlimited", terminalData))
	for attempt := 1; attempt <= 4; attempt++ {
		batch, fetchErr := consumer.Fetch(1, jetstream.FetchMaxWait(3*time.Second))
		require.NoError(t, fetchErr)
		msg := <-batch.Messages()
		require.NotNil(t, msg, "attempt %d", attempt)
		metadata, metadataErr := msg.Metadata()
		require.NoError(t, metadataErr)
		require.Equal(t, uint64(attempt), metadata.NumDelivered)
		if attempt < 4 {
			require.NoError(t, msg.Nak())
		} else {
			require.NoError(t, msg.Ack())
		}
	}
}

func TestIntegrationUnlimitedAttemptsDoNotPreventAgeOrCapacityEviction(t *testing.T) {
	ctx := t.Context()
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream())
	js, err := tc.Client.JetStream()
	require.NoError(t, err)

	ageStream, err := tc.Client.EnsureStream(ctx, jetstream.StreamConfig{
		Name: "TERMINAL_AGE", Subjects: []string{"terminal.age"}, MaxAge: 150 * time.Millisecond, MaxBytes: 1 << 20, Discard: jetstream.DiscardOld,
	})
	require.NoError(t, err)
	_, err = ageStream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{Name: "unlimited-age", AckPolicy: jetstream.AckExplicitPolicy, MaxDeliver: 0})
	require.NoError(t, err)
	require.NoError(t, tc.Client.PublishToStream(ctx, "terminal.age", []byte("unsettled")))
	require.Eventually(t, func() bool {
		info, infoErr := ageStream.Info(ctx)
		return infoErr == nil && info.State.Msgs == 0
	}, 3*time.Second, 25*time.Millisecond, "MaxAge must evict even with MaxDeliver=0")

	capacityStream, err := js.CreateStream(ctx, jetstream.StreamConfig{
		Name: "TERMINAL_CAPACITY", Subjects: []string{"terminal.capacity"}, MaxAge: time.Minute, MaxBytes: 160, Discard: jetstream.DiscardOld,
	})
	require.NoError(t, err)
	_, err = capacityStream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{Name: "unlimited-capacity", AckPolicy: jetstream.AckExplicitPolicy, MaxDeliver: 0})
	require.NoError(t, err)
	require.NoError(t, tc.Client.PublishToStream(ctx, "terminal.capacity", make([]byte, 100)))
	require.NoError(t, tc.Client.PublishToStream(ctx, "terminal.capacity", make([]byte, 100)))
	info, err := capacityStream.Info(ctx)
	require.NoError(t, err)
	require.Less(t, info.State.Msgs, uint64(2), "DiscardOld capacity must evict unsettled source despite MaxDeliver=0")
}

func TestIntegrationPersistedLoopMalformedJSONAndIDMismatchArePermanent(t *testing.T) {
	ctx := t.Context()
	tc := natsclient.NewTestClient(t, natsclient.WithKVBuckets(defaultAgentLoopsBucket(t)))
	c := terminalTestComponent(t)
	c.natsClient = tc.Client
	kv, err := tc.GetKVBucket(ctx, defaultAgentLoopsBucket(t))
	require.NoError(t, err)

	_, err = kv.Put(ctx, "malformed-loop", []byte(`{"not valid"`))
	require.NoError(t, err)
	_, err = c.loadPersistedLoop(ctx, "malformed-loop")
	require.Error(t, err)
	require.True(t, isPermanentTerminal(err))
	require.ErrorContains(t, err, "malformed AGENT_LOOPS/malformed-loop")

	mismatch, err := json.Marshal(agentic.LoopEntity{ID: "other-loop", TaskID: "task"})
	require.NoError(t, err)
	_, err = kv.Put(ctx, "expected-loop", mismatch)
	require.NoError(t, err)
	_, err = c.loadPersistedLoop(ctx, "expected-loop")
	require.Error(t, err)
	require.True(t, isPermanentTerminal(err))
	require.ErrorContains(t, err, `contains loop id "other-loop"`)
}

func TestIntegrationInvalidTerminalIsTerminated(t *testing.T) {
	ctx := t.Context()
	tc := natsclient.NewTestClient(t,
		natsclient.WithKVBuckets(defaultAgentLoopsBucket(t)),
		natsclient.WithStreams(
			natsclient.TestStreamConfig{Name: "INVALID_TERMINAL", Subjects: []string{"agent.>"}},
			natsclient.TestStreamConfig{Name: "INVALID_TERMINAL_USER", Subjects: []string{"user.message.>"}},
		),
	)
	_ = startProductionTerminalDispatch(
		t, ctx, tc, "INVALID_TERMINAL", "INVALID_TERMINAL_USER", "MISSING", "invalid", nil)
	stream, err := tc.Client.GetStream(ctx, "INVALID_TERMINAL")
	require.NoError(t, err)
	consumer, err := stream.Consumer(ctx, "agentic-dispatch-agent-complete-invalid")
	require.NoError(t, err)
	advisory, err := tc.Client.GetConnection().SubscribeSync(
		"$JS.EVENT.ADVISORY.CONSUMER.MSG_TERMINATED.INVALID_TERMINAL.agentic-dispatch-agent-complete-invalid")
	require.NoError(t, err)
	t.Cleanup(func() { _ = advisory.Unsubscribe() })
	require.NoError(t, tc.Client.PublishToStream(ctx, "agent.complete.invalid", []byte(`{"not":"a BaseMessage"}`)))
	_, err = advisory.NextMsg(10 * time.Second)
	require.NoError(t, err, "production callback must Term invalid terminal")
	require.Eventually(t, func() bool {
		info, infoErr := consumer.Info(ctx)
		return infoErr == nil && info.NumAckPending == 0 && info.NumRedelivered == 0
	}, 2*time.Second, 25*time.Millisecond)
}

func TestIntegrationProductionCallbackRetriesKVThenAcksAfterPubAck(t *testing.T) {
	ctx := t.Context()
	tc := natsclient.NewTestClient(t,
		natsclient.WithKVBuckets(defaultAgentLoopsBucket(t)),
		natsclient.WithStreams(
			natsclient.TestStreamConfig{Name: "CALLBACK_AGENT", Subjects: []string{"agent.>"}},
			natsclient.TestStreamConfig{Name: "CALLBACK_INPUT_USER", Subjects: []string{"user.message.>"}},
			natsclient.TestStreamConfig{Name: "CALLBACK_USER", Subjects: []string{"callback.response.>"}},
		),
	)
	deliveryDone := make(chan error, 2)
	c := startProductionTerminalDispatch(
		t, ctx, tc, "CALLBACK_AGENT", "CALLBACK_INPUT_USER", "CALLBACK_USER", "retry",
		func(c *Component) { c.terminalDeliveryDoneFn = func(err error) { deliveryDone <- err } },
	)
	routingReadBefore := terminalReasonValue(c, "routing_read_transient")
	responseSettledBefore := terminalReasonValue(c, "response_settled")

	kv, err := tc.GetKVBucket(ctx, defaultAgentLoopsBucket(t))
	require.NoError(t, err)
	persist := func(loopID, taskID, channelID string) {
		data, marshalErr := json.Marshal(agentic.LoopEntity{
			ID: loopID, TaskID: taskID, State: agentic.LoopStateComplete, MaxIterations: 3,
			ChannelType: "http", ChannelID: channelID,
		})
		require.NoError(t, marshalErr)
		_, putErr := kv.Put(ctx, loopID, data)
		require.NoError(t, putErr)
	}
	stream, err := tc.Client.GetStream(ctx, "CALLBACK_AGENT")
	require.NoError(t, err)
	consumer, err := stream.Consumer(ctx, "agentic-dispatch-agent-complete-retry")
	require.NoError(t, err)
	publishTerminal := func(loopID, taskID string) {
		data := terminalEnvelopeForDispatch(t, &agentic.LoopCompletedEvent{
			LoopID: loopID, TaskID: taskID, Outcome: agentic.OutcomeSuccess,
			Result: loopID + " result", CompletedAt: time.Now().UTC(),
		})
		require.NoError(t, tc.Client.PublishToStream(ctx, "agent.complete."+loopID, data))
	}
	publishTerminal("kv-loop", "kv-task")
	select {
	case callbackErr := <-deliveryDone:
		require.Error(t, callbackErr, "proven pre-publish failure must retry")
	case <-time.After(5 * time.Second):
		t.Fatal("transient production callback did not finish")
	}
	info, err := consumer.Info(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), info.Delivered.Consumer)
	require.Zero(t, info.AckFloor.Consumer, "transient KV failure must not ACK")
	require.Equal(t, 1, info.NumAckPending)
	require.Equal(t, routingReadBefore+1, terminalReasonValue(c, "routing_read_transient"))

	persist("kv-loop", "kv-task", "kv-channel")
	select {
	case callbackErr := <-deliveryDone:
		require.NoError(t, callbackErr, "recovered production callback")
	case <-time.After(35 * time.Second):
		t.Fatal("recovered production callback did not finish")
	}
	require.Eventually(t, func() bool {
		settled, infoErr := consumer.Info(ctx)
		return infoErr == nil && settled.Delivered.Consumer == 2 &&
			settled.AckFloor.Consumer == 2 && settled.AckFloor.Stream == 1 && settled.NumAckPending == 0
	}, 3*time.Second, 25*time.Millisecond, "source ACK must follow successful synchronous response PubAck")
	require.Equal(t, responseSettledBefore+1, terminalReasonValue(c, "response_settled"))
	userStream, err := tc.Client.GetStream(ctx, "CALLBACK_USER")
	require.NoError(t, err)
	userInfo, err := userStream.Info(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), userInfo.State.Msgs)
}

func TestIntegrationProductionCallbackUnknownPublishQuarantinesExactLane(t *testing.T) {
	ctx := t.Context()
	tc := natsclient.NewTestClient(t,
		natsclient.WithKVBuckets(defaultAgentLoopsBucket(t)),
		natsclient.WithStreams(
			natsclient.TestStreamConfig{Name: "QUARANTINE_AGENT", Subjects: []string{"agent.>"}},
			natsclient.TestStreamConfig{Name: "QUARANTINE_INPUT_USER", Subjects: []string{"user.message.>"}},
		),
	)
	deliveryDone := make(chan error, 1)
	c := startProductionTerminalDispatch(
		t, ctx, tc, "QUARANTINE_AGENT", "QUARANTINE_INPUT_USER", "MISSING", "quarantine",
		func(c *Component) { c.terminalDeliveryDoneFn = func(err error) { deliveryDone <- err } },
	)
	kv, err := tc.GetKVBucket(ctx, defaultAgentLoopsBucket(t))
	require.NoError(t, err)
	loop := agentic.LoopEntity{
		ID: "quarantine-loop", TaskID: "quarantine-task", State: agentic.LoopStateComplete, MaxIterations: 3,
		ChannelType: "http", ChannelID: "channel",
	}
	data, err := json.Marshal(loop)
	require.NoError(t, err)
	_, err = kv.Put(ctx, loop.ID, data)
	require.NoError(t, err)
	payload := terminalEnvelopeForDispatch(t, &agentic.LoopCompletedEvent{
		LoopID: loop.ID, TaskID: loop.TaskID, Outcome: agentic.OutcomeSuccess, CompletedAt: time.Now(),
	})
	require.NoError(t, tc.Client.PublishToStream(ctx, "agent.complete."+loop.ID, payload))
	select {
	case callbackErr := <-deliveryDone:
		require.Error(t, callbackErr)
		require.True(t, isUnknownTerminalPublication(callbackErr))
	case <-time.After(5 * time.Second):
		t.Fatal("unknown publication result was not observed")
	}
	require.Len(t, c.consumers, 5)
	completeClosed := c.consumers[1].handle.Closed()
	select {
	case <-completeClosed:
	case <-time.After(5 * time.Second):
		t.Fatal("agent.complete exact handle was not drained")
	}
	stream, err := tc.Client.GetStream(ctx, "QUARANTINE_AGENT")
	require.NoError(t, err)
	consumer, err := stream.Consumer(ctx, "agentic-dispatch-agent-complete-quarantine")
	require.NoError(t, err)
	info, err := consumer.Info(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), info.Delivered.Consumer)
	require.Zero(t, info.AckFloor.Consumer)
	require.Equal(t, 1, info.NumAckPending)
	require.Zero(t, info.NumRedelivered)
}

func TestIntegrationProductionCallbackShutdownUsesSemanticRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	tc := natsclient.NewTestClient(t,
		natsclient.WithKVBuckets(defaultAgentLoopsBucket(t)),
		natsclient.WithStreams(
			natsclient.TestStreamConfig{Name: "CALLBACK_SHUTDOWN", Subjects: []string{"agent.>"}},
			natsclient.TestStreamConfig{Name: "CALLBACK_SHUTDOWN_USER", Subjects: []string{"user.message.>"}},
		),
	)
	entered := make(chan struct{})
	deliveryDone := make(chan error, 1)
	var enteredOnce sync.Once
	comp := startProductionTerminalDispatch(
		t, ctx, tc, "CALLBACK_SHUTDOWN", "CALLBACK_SHUTDOWN_USER", "MISSING", "shutdown",
		func(c *Component) {
			c.terminalDeliveryDoneFn = func(err error) { deliveryDone <- err }
			c.loadPersistedLoopFn = func(workCtx context.Context, _ string) (*agentic.LoopEntity, error) {
				enteredOnce.Do(func() { close(entered) })
				<-workCtx.Done()
				return nil, workCtx.Err()
			}
		},
	)
	stream, err := tc.Client.GetStream(ctx, "CALLBACK_SHUTDOWN")
	require.NoError(t, err)
	consumer, err := stream.Consumer(ctx, "agentic-dispatch-agent-complete-shutdown")
	require.NoError(t, err)
	payload := terminalEnvelopeForDispatch(t, &agentic.LoopCompletedEvent{
		LoopID: "shutdown-loop", TaskID: "shutdown-task", Outcome: agentic.OutcomeSuccess, CompletedAt: time.Now(),
	})
	require.NoError(t, tc.Client.PublishToStream(ctx, "agent.complete.shutdown-loop", payload))
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("production callback did not enter terminal work")
	}
	cancel()
	select {
	case callbackErr := <-deliveryDone:
		require.ErrorIs(t, callbackErr, context.Canceled)
	case <-time.After(3 * time.Second):
		t.Fatal("production callback did not finish its shutdown delayed-NAK")
	}
	cleanupCtx, cleanupCancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cleanupCancel()
	require.NoError(t, comp.Stop(cleanupCtx))
	redelivery, err := consumer.Fetch(1, jetstream.FetchMaxWait(35*time.Second))
	require.NoError(t, err)
	redelivered := <-redelivery.Messages()
	require.NotNil(t, redelivered, "shutdown must use the explicit 30-second semantic retry")
	metadata, err := redelivered.Metadata()
	require.NoError(t, err)
	require.Equal(t, uint64(2), metadata.NumDelivered)
	require.NoError(t, redelivered.Term())
}

func startProductionTerminalDispatch(
	t *testing.T,
	ctx context.Context,
	tc *natsclient.TestClient,
	agentStream, inputUserStream, outputUserStream, suffix string,
	configure func(*Component),
) *Component {
	t.Helper()
	config := DefaultConfig()
	config.ConsumerNameSuffix = suffix
	for i := range config.Ports.Inputs {
		// Only the JetStream lanes are rebound to the test streams; the
		// declared agent_loops KV read port keeps its bucket.
		port, isStream := config.Ports.Inputs[i].Config.(component.JetStreamPort)
		if !isStream {
			continue
		}
		if config.Ports.Inputs[i].Name == "user.message" {
			port.StreamName = inputUserStream
		} else {
			port.StreamName = agentStream
		}
		config.Ports.Inputs[i].Config = port
	}
	response := config.Ports.Outputs[2].Config.(component.JetStreamPort)
	response.StreamName = outputUserStream
	response.Subjects = []string{"callback.response.>"}
	config.Ports.Outputs[2].Config = response
	raw, err := json.Marshal(config)
	require.NoError(t, err)
	reg := payloadregistry.NewWithSubset(t, agentic.RegisterPayloads)
	discoverable, err := NewComponent(raw, component.Dependencies{
		NATSClient: tc.Client, ModelRegistry: newTestRegistry(), PayloadRegistry: reg,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	require.NoError(t, err)
	c := discoverable.(*Component)
	if configure != nil {
		configure(c)
	}
	require.NoError(t, c.Start(ctx))
	t.Cleanup(func() { _ = c.Stop(context.Background()) })
	return c
}
