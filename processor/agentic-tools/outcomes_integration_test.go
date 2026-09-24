//go:build integration

package agentictools

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type injectedCreateFailureStore struct {
	completedOutcomeStore
	err error
}

func (s injectedCreateFailureStore) Create(context.Context, string, []byte) error { return s.err }

func correlatedIntegrationToolCall(call agentic.ToolCall) agentic.ToolCall {
	call.RequestID = "request-" + call.ID
	call.ExecutionID = "execution-" + call.ID
	call.CallOrdinal = 1
	return call
}

func TestIntegrationPostEffectCreateFailureIsAmbiguousAndLeavesNoAuthority(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: "AMBIGUOUS_CREATE", Subjects: []string{"tool.result.>"}},
	))
	ctx := t.Context()
	bucket, err := graph.EnsureCatalogBucket(ctx, testClient.Client, graph.BucketToolCallOutcomes)
	require.NoError(t, err)
	executor := &countingExecutor{}
	var logs bytes.Buffer
	componentUnderTest := &Component{
		config: DefaultConfig(), registry: NewExecutorRegistry(), decoder: payloadbuiltins.NewTestDecoder(t),
		logger: slog.New(slog.NewJSONHandler(&logs, nil)),
		outcomes: injectedCreateFailureStore{
			completedOutcomeStore: jetStreamCompletedOutcomeStore{bucket: bucket}, err: context.DeadlineExceeded,
		},
		publishStream: testClient.Client.PublishToStreamWithMsgID,
		metrics:       newToolsMetrics(),
	}
	require.NoError(t, componentUnderTest.registry.RegisterTool("count", executor))
	call := correlatedIntegrationToolCall(agentic.ToolCall{
		ID: "ambiguous-create", Name: "count", LoopID: "loop", TraceID: "trace",
	})
	wireMessage := message.NewBaseMessage(call.Schema(), &call, "integration")
	wire, err := json.Marshal(wireMessage)
	require.NoError(t, err)
	decision, err := componentUnderTest.handleToolDelivery(ctx, wire)
	require.Equal(t, natsclient.DeliveryDecisionQuarantine, decision)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, int32(1), executor.calls.Load(), "effect occurred before injected Create failure")
	_, err = bucket.Get(ctx, toolCallOutcomeKey(call.ExecutionID))
	assert.ErrorIs(t, err, jetstream.ErrKeyNotFound, "failed Create must leave no false completion authority")
	assert.Equal(t, float64(1), testutil.ToFloat64(
		componentUnderTest.metrics.ambiguousRedeliveries.WithLabelValues(string(ambiguousCauseStoreFailure))))
	assert.Contains(t, logs.String(), `"ambiguous_effect":true`)
}

func TestIntegrationExecutorPanicCompletesWithCorrelatedInternalResult(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: "PANIC_RESULT", Subjects: []string{"tool.result.>"}},
	))
	ctx := t.Context()
	bucket, err := graph.EnsureCatalogBucket(ctx, testClient.Client, graph.BucketToolCallOutcomes)
	require.NoError(t, err)
	componentUnderTest := &Component{
		config: DefaultConfig(), registry: NewExecutorRegistry(), decoder: payloadbuiltins.NewTestDecoder(t),
		logger: slog.Default(), outcomes: jetStreamCompletedOutcomeStore{bucket: bucket},
		publishStream: testClient.Client.PublishToStreamWithMsgID, metrics: newToolsMetrics(),
	}
	require.NoError(t, componentUnderTest.registry.RegisterTool("panic", panicExecutor{}))
	call := correlatedIntegrationToolCall(agentic.ToolCall{
		ID: "panic-integration", Name: "panic", LoopID: "loop", TraceID: "trace",
	})
	wireMessage := message.NewBaseMessage(call.Schema(), &call, "integration")
	wire, err := json.Marshal(wireMessage)
	require.NoError(t, err)
	require.NoError(t, componentUnderTest.handleToolCall(ctx, wire))
	entry, err := bucket.Get(ctx, toolCallOutcomeKey(call.ExecutionID))
	require.NoError(t, err)
	outcome, err := decodeCompletedOutcome(entry.Value(), call)
	require.NoError(t, err)
	assert.Equal(t, call.ID, outcome.Result.CallID)
	assert.Equal(t, call.RequestID, outcome.Result.RequestID)
	assert.Equal(t, call.ExecutionID, outcome.Result.ExecutionID)
	assert.Equal(t, call.CallOrdinal, outcome.Result.CallOrdinal)
	assert.Equal(t, call.LoopID, outcome.Result.LoopID)
	assert.Equal(t, call.TraceID, outcome.Result.TraceID)
	assert.Equal(t, agentic.ToolErrorInternal, outcome.Result.ErrorKind)
	assert.NotContains(t, outcome.Result.Error, "secret")
	assert.Equal(t, float64(1), testutil.ToFloat64(
		componentUnderTest.metrics.ambiguousRedeliveries.WithLabelValues(string(ambiguousCausePanic))))
}

func TestIntegrationConcurrentReplicasConvergeOnOneCompletedOutcome(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream())
	ctx := t.Context()
	bucket, err := graph.EnsureCatalogBucket(ctx, testClient.Client, graph.BucketToolCallOutcomes)
	require.NoError(t, err)
	status, err := bucket.Status(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), status.History())
	assert.Zero(t, status.TTL())
	streamInfo := status.(*jetstream.KeyValueBucketStatus).StreamInfo()
	assert.Equal(t, 1, streamInfo.Config.Replicas)
	assert.LessOrEqual(t, streamInfo.Config.MaxBytes, int64(0), "non-positive MaxBytes is unlimited in NATS")

	store := jetStreamCompletedOutcomeStore{bucket: bucket}
	replicas := []*Component{
		{outcomes: store, logger: slog.Default()},
		{outcomes: store, logger: slog.Default()},
	}
	call := correlatedIntegrationToolCall(agentic.ToolCall{ID: "integration-concurrent-call", Name: "external-write"})
	start := make(chan struct{})
	winners := make(chan completedOutcome, len(replicas))
	errs := make(chan error, len(replicas))
	var wg sync.WaitGroup
	for i, replica := range replicas {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			candidate := correlateToolResult(call, agentic.ToolResult{Name: call.Name, Content: string(rune('a' + i))})
			winner, _, persistErr := replica.persistCompletedOutcome(context.Background(), call, candidate, outcomePathNew, false, true)
			winners <- winner
			errs <- persistErr
		}()
	}
	close(start)
	wg.Wait()
	close(winners)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	var authoritative string
	for winner := range winners {
		if authoritative == "" {
			authoritative = winner.Result.Content
		}
		assert.Equal(t, authoritative, winner.Result.Content)
	}

	restarted := &Component{outcomes: store, logger: slog.Default()}
	loaded, found, err := restarted.loadCompletedOutcome(ctx, call, storeOperationGet)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, authoritative, loaded.Result.Content)
}

// ackReplayStopBudget is a backstop on each Stop in the ACK-failure restart
// test, not an expected wait: both Stops are local drains that should finish
// far inside it, and five seconds is slack for a loaded host. If a Stop ever
// reaches it, the test fails with the deadline instead of hanging until the
// package timeout (#1370).
const ackReplayStopBudget = 5 * time.Second

// errorLeaves flattens errors.Join trees so an assertion can require an exact
// set of causes; errors.Is on a join matches any member, so it would let an
// unexpected extra cause through.
func errorLeaves(err error) []error {
	if err == nil {
		return nil
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var leaves []error
		for _, member := range joined.Unwrap() {
			leaves = append(leaves, errorLeaves(member)...)
		}
		return leaves
	}
	return []error{err}
}

func TestIntegrationAckFailureRestartReplaysWithoutSecondExecution(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: "TOOL_ACK_REPLAY", Subjects: []string{"tool.execute.>", "tool.result.>"}},
	))
	ctx := t.Context()
	publisher, err := natsclient.NewClient(testClient.URL)
	require.NoError(t, err)
	require.NoError(t, publisher.Connect(ctx))
	t.Cleanup(func() { require.NoError(t, publisher.Close(context.Background())) })

	config := DefaultConfig()
	config.ConsumerNameSuffix = "ack-replay"
	for index := range config.Ports.Inputs {
		if config.Ports.Inputs[index].Name == "tool.execute" {
			config.Ports.Inputs[index].Config = component.JetStreamPort{
				StreamName: "TOOL_ACK_REPLAY", Subjects: []string{"tool.execute.>"},
				AckWait: "500ms", HeartbeatInterval: "250ms",
			}
		}
	}
	for index := range config.Ports.Outputs {
		if config.Ports.Outputs[index].Name == "tool.result" {
			config.Ports.Outputs[index].Config = component.JetStreamPort{
				StreamName: "TOOL_ACK_REPLAY", Subjects: []string{"tool.result.*"},
			}
		}
	}
	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)
	executor := &countingExecutor{}
	newRunning := func(client *natsclient.Client) *Component {
		discoverable, createErr := NewComponent(rawConfig, component.Dependencies{
			NATSClient: client, PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
		})
		require.NoError(t, createErr)
		componentUnderTest := discoverable.(*Component)
		require.NoError(t, componentUnderTest.RegisterToolExecutor(executor))
		require.NoError(t, componentUnderTest.Start(ctx))
		return componentUnderTest
	}
	first := newRunning(testClient.Client)
	realPublish := first.publishStream
	first.publishStream = func(publishCtx context.Context, subject string, data []byte, msgID string) error {
		if publishErr := realPublish(publishCtx, subject, data, msgID); publishErr != nil {
			return publishErr
		}
		// PubAck has arrived; sever the request consumer's connection before
		// the delivery's settlement contract can ACK the request.
		testClient.GetNativeConnection().Close()
		return nil
	}
	call := correlatedIntegrationToolCall(agentic.ToolCall{
		ID: "ack-failure-call", Name: "count", LoopID: "loop", TraceID: "trace",
	})
	envelope := message.NewBaseMessage(call.Schema(), &call, "integration")
	wire, err := json.Marshal(envelope)
	require.NoError(t, err)
	require.NoError(t, publisher.PublishToStream(ctx, "tool.execute."+call.ID, wire))
	require.Eventually(t, func() bool { return executor.calls.Load() == 1 }, 5*time.Second, 25*time.Millisecond)
	// Wait for the sever to land before stopping. Racing Stop against the
	// close made its outcome depend on where the close fell relative to the
	// tool.list drain. A close while that drain is pending leaves
	// natsclient.Subscription.Drain waiting for a SubscriptionClosed status
	// that a closed connection never emits (#1372), so Stop could only return
	// at its ctx deadline. That is the likely cause of #1370's 20-minute hang
	// under context.Background(): the natural stack resolved only to
	// component.go:632, and the Drain frame beneath it comes from a forced
	// reproduction.
	require.Eventually(t, func() bool { return testClient.GetNativeConnection().IsClosed() },
		5*time.Second, 5*time.Millisecond, "the wrapped publish must sever the first component's connection")
	firstStopCtx, cancelFirstStop := context.WithTimeout(context.Background(), ackReplayStopBudget)
	firstStopErr := first.Stop(firstStopCtx)
	cancelFirstStop()
	// With the connection already closed, the tool.list subscription is
	// invalid and its closed status never fired, so the drain reports
	// nats.ErrBadSubscription (natsclient/client.go:789-796). The JetStream
	// consumer's Closed does fire on connection loss, so the lane adds nothing.
	// That one cause is the whole result; any other member of the join fails.
	require.Equal(t, []error{nats.ErrBadSubscription}, errorLeaves(firstStopErr),
		"Stop after the connection closed must report exactly the invalid tool.list subscription; got %v", firstStopErr)
	replayBefore := testutil.ToFloat64(first.metrics.outcomeTotal.WithLabelValues(string(outcomePathReplay)))

	secondClient, err := natsclient.NewClient(testClient.URL)
	require.NoError(t, err)
	require.NoError(t, secondClient.Connect(ctx))
	t.Cleanup(func() { require.NoError(t, secondClient.Close(context.Background())) })
	second := newRunning(secondClient)
	defer func() {
		secondStopCtx, cancelSecondStop := context.WithTimeout(context.Background(), ackReplayStopBudget)
		defer cancelSecondStop()
		assert.NoError(t, second.Stop(secondStopCtx), "Stop on a live connection must drain cleanly")
	}()
	require.Eventually(t, func() bool {
		return testutil.ToFloat64(second.metrics.outcomeTotal.WithLabelValues(string(outcomePathReplay))) > replayBefore
	}, 20*time.Second, 100*time.Millisecond, "redelivery must traverse durable replay after configured 15s backoff")
	assert.Equal(t, int32(1), executor.calls.Load(), "ACK failure redelivery must not execute again")
}

// spec: agentic-tools / Tool-result publication is durably at-least-once
func TestIntegrationResultPublishFailureRestartReplaysStoredOutcome(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: "TOOL_PUBLISH_REPLAY", Subjects: []string{"tool.execute.>"}},
	))
	ctx := t.Context()
	config := DefaultConfig()
	config.ConsumerNameSuffix = "publish-replay"
	for index := range config.Ports.Inputs {
		if config.Ports.Inputs[index].Name == "tool.execute" {
			config.Ports.Inputs[index].Config = component.JetStreamPort{
				StreamName: "TOOL_PUBLISH_REPLAY", Subjects: []string{"tool.execute.>"},
				AckWait: "500ms", HeartbeatInterval: "250ms",
			}
		}
	}
	for index := range config.Ports.Outputs {
		if config.Ports.Outputs[index].Name == "tool.result" {
			// The declared result stream deliberately does not exist for the first
			// component. The real synchronous JetStream publish therefore fails
			// after COMPLETED persistence rather than through an injected stub.
			config.Ports.Outputs[index].Config = component.JetStreamPort{
				StreamName: "TOOL_PUBLISH_RESULTS", Subjects: []string{"tool.result.*"},
			}
		}
	}
	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)
	executor := &countingExecutor{}
	newRunning := func(client *natsclient.Client) *Component {
		discoverable, createErr := NewComponent(rawConfig, component.Dependencies{
			NATSClient: client, PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
		})
		require.NoError(t, createErr)
		componentUnderTest := discoverable.(*Component)
		require.NoError(t, componentUnderTest.RegisterToolExecutor(executor))
		require.NoError(t, componentUnderTest.Start(ctx))
		return componentUnderTest
	}

	first := newRunning(testClient.Client)
	publishFailures := make(chan error, 1)
	realPublish := first.publishStream
	first.publishStream = func(publishCtx context.Context, subject string, data []byte, msgID string) error {
		publishErr := realPublish(publishCtx, subject, data, msgID)
		if publishErr != nil {
			select {
			case publishFailures <- publishErr:
			default:
			}
		}
		return publishErr
	}
	call := correlatedIntegrationToolCall(agentic.ToolCall{
		ID: "publish-failure-call", Name: "count", LoopID: "loop", TraceID: "trace",
	})
	envelope := message.NewBaseMessage(call.Schema(), &call, "integration")
	wire, err := json.Marshal(envelope)
	require.NoError(t, err)
	require.NoError(t, testClient.Client.PublishToStream(ctx, "tool.execute."+call.ID, wire))
	select {
	case publishErr := <-publishFailures:
		require.Error(t, publishErr, "real synchronous publication must fail while no result stream exists")
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for real synchronous result publication failure")
	}
	require.Equal(t, int32(1), executor.calls.Load())
	bucket, err := graph.EnsureCatalogBucket(ctx, testClient.Client, graph.BucketToolCallOutcomes)
	require.NoError(t, err)
	entry, err := bucket.Get(ctx, toolCallOutcomeKey(call.ExecutionID))
	require.NoError(t, err, "COMPLETED must precede the failed result publication")
	authority, err := decodeCompletedOutcome(entry.Value(), call)
	require.NoError(t, err)
	require.Equal(t, "executed", authority.Result.Content)
	require.NoError(t, first.Stop(context.Background()))

	_, err = testClient.Client.EnsureStream(ctx, jetstream.StreamConfig{
		Name: "TOOL_PUBLISH_RESULTS", Subjects: []string{"tool.result.>"},
		MaxAge: time.Hour, MaxBytes: 1 << 20, Discard: jetstream.DiscardNew,
	})
	require.NoError(t, err)
	secondClient, err := natsclient.NewClient(testClient.URL)
	require.NoError(t, err)
	require.NoError(t, secondClient.Connect(ctx))
	t.Cleanup(func() { require.NoError(t, secondClient.Close(context.Background())) })
	second := newRunning(secondClient)
	defer second.Stop(context.Background())

	js, err := secondClient.JetStream()
	require.NoError(t, err)
	resultStream, err := js.Stream(ctx, "TOOL_PUBLISH_RESULTS")
	require.NoError(t, err)
	var replayed agentic.ToolResult
	decoder := payloadbuiltins.NewTestDecoder(t)
	require.Eventually(t, func() bool {
		raw, getErr := resultStream.GetLastMsgForSubject(ctx, "tool.result."+call.ExecutionID)
		if getErr != nil {
			return false
		}
		base, decodeErr := decoder.Decode(raw.Data)
		if decodeErr != nil {
			return false
		}
		result, ok := base.Payload().(*agentic.ToolResult)
		if !ok {
			return false
		}
		replayed = *result
		return true
	}, 35*time.Second, 100*time.Millisecond, "redelivery must replay after the transient 30s NAK delay")
	assert.Equal(t, authority.Result, replayed, "replay must publish the exact stored authority")
	assert.Equal(t, int32(1), executor.calls.Load(), "publication-failure redelivery must not execute again")

	consumer, err := js.Consumer(ctx, "TOOL_PUBLISH_REPLAY", "agentic-tools-tool-execute-all-publish-replay")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		info, infoErr := consumer.Info(ctx)
		return infoErr == nil && info.NumAckPending == 0 && info.AckFloor.Consumer >= 1
	}, 5*time.Second, 50*time.Millisecond, "successful replay publication must permit request ACK")
}

func TestIntegrationLowMaxPayloadStoresAndPublishesCompactAuthority(t *testing.T) {
	ctx := t.Context()
	testClient := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithTestMaxPayload(2048),
		natsclient.WithStreams(natsclient.TestStreamConfig{
			Name: "LOW_PAYLOAD_TOOL", Subjects: []string{"tool.result.>"},
		}),
	)
	client := testClient.Client
	bucket, err := graph.EnsureCatalogBucket(ctx, client, graph.BucketToolCallOutcomes)
	require.NoError(t, err)
	component := &Component{
		config: DefaultConfig(), logger: slog.Default(), outcomes: jetStreamCompletedOutcomeStore{bucket: bucket},
		publishStream: client.PublishToStreamWithMsgID,
	}
	call := correlatedIntegrationToolCall(agentic.ToolCall{
		ID: "low-max-payload", Name: "large", LoopID: "loop", TraceID: "trace",
	})
	full := correlateToolResult(call, agentic.ToolResult{Name: call.Name, Content: strings.Repeat("sensitive", 1024)})
	require.NoError(t, component.persistAndPublishOutcome(ctx, call, full, outcomePathNew, true))

	entry, err := bucket.Get(ctx, toolCallOutcomeKey(call.ExecutionID))
	require.NoError(t, err)
	authority, err := decodeCompletedOutcome(entry.Value(), call)
	require.NoError(t, err)
	assert.Equal(t, "too_large", authority.Result.Error)
	assert.Empty(t, authority.Result.Content)
	stream, err := client.JetStream()
	require.NoError(t, err)
	toolStream, err := stream.Stream(ctx, "LOW_PAYLOAD_TOOL")
	require.NoError(t, err)
	raw, err := toolStream.GetLastMsgForSubject(ctx, "tool.result."+call.ExecutionID)
	require.NoError(t, err)
	var envelope struct {
		Payload agentic.ToolResult `json:"payload"`
	}
	require.NoError(t, json.Unmarshal(raw.Data, &envelope))
	assert.Equal(t, authority.Result, envelope.Payload)
}
