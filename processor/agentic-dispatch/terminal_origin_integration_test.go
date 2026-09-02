//go:build integration

package agenticdispatch

// gh#1094 — real-NATS proofs for workflow terminal delivery:
//
//  1. Origin resolution is restart-safe: with an EMPTY process tracker, the
//     route comes from persisted AGENT_LOOPS ancestry, and two deliveries of
//     the same terminal leave exactly one message on the origin's subject.
//  2. The AGENT_LOOPS bucket name is OBSERVED from the declared agent_loops
//     read port, never predicted by a constant.

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
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

// dispatchConfigWithAgentLoopsBucket applies an operator-shaped JSON port
// override through the production merge path, so the test binds the bucket
// exactly as a deployment would.
func dispatchConfigWithAgentLoopsBucket(t *testing.T, bucket string) Config {
	t.Helper()
	var override component.PortConfig
	require.NoError(t, json.Unmarshal([]byte(
		`{"inputs":[{"name":"`+agentLoopsPortName+`","config":{"kind":"kv-read","bucket":"`+bucket+`"}}]}`,
	), &override))
	merged, err := component.MergePortConfig(*DefaultConfig().Ports, override)
	require.NoError(t, err)
	config := DefaultConfig()
	config.Ports = &merged
	return config
}

// defaultAgentLoopsBucket observes the default loops bucket from the declared
// port instead of restating the literal in test fixtures.
func defaultAgentLoopsBucket(t *testing.T) string {
	t.Helper()
	bucket, err := loopsBucketFromPorts(DefaultConfig().Ports)
	require.NoError(t, err)
	return bucket
}

func putLoopRecord(t *testing.T, ctx context.Context, kv jetstream.KeyValue, record agentic.LoopEntity) {
	t.Helper()
	data, err := json.Marshal(record)
	require.NoError(t, err)
	_, err = kv.Put(ctx, record.ID, data)
	require.NoError(t, err)
}

func TestIntegrationWorkflowTerminalResolvesOriginFromAgentLoopsAfterRestart(t *testing.T) {
	ctx := t.Context()
	bucket := defaultAgentLoopsBucket(t)
	tc := natsclient.NewTestClient(t,
		natsclient.WithKVBuckets(bucket),
		natsclient.WithStreams(
			natsclient.TestStreamConfig{Name: "USER_TERMINAL", Subjects: []string{"user.>"}},
		),
	)
	reg := payloadregistry.NewWithSubset(t, agentic.RegisterPayloads)
	c := &Component{
		config: DefaultConfig(), decoder: message.NewDecoder(reg), natsClient: tc.Client,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)), loopTracker: NewLoopTracker(), metrics: getMetrics(nil),
	}
	c.config.Ports.Outputs[2].Config = component.JetStreamPort{Subjects: []string{"user.response.>"}, StreamName: "USER_TERMINAL"}

	kv, err := tc.GetKVBucket(ctx, bucket)
	require.NoError(t, err)
	// The chain as a rule-spawned workflow persists it: only the root owns a
	// route; every descendant carries ancestry and nothing else.
	putLoopRecord(t, ctx, kv, agentic.LoopEntity{
		ID: "chain-root", TaskID: "task-chain-root", State: agentic.LoopStateComplete,
		ChannelType: "http", ChannelID: "origin-1", UserID: "user-1",
	})
	putLoopRecord(t, ctx, kv, agentic.LoopEntity{
		ID: "chain-mid", TaskID: "task-chain-mid", State: agentic.LoopStateComplete,
		ParentLoopID: "chain-root", RunID: "chain-root",
	})
	putLoopRecord(t, ctx, kv, agentic.LoopEntity{
		ID: "chain-terminal", TaskID: "task-chain-terminal", State: agentic.LoopStateComplete,
		ParentLoopID: "chain-mid", RunID: "chain-root",
	})

	terminal := &agentic.LoopCompletedEvent{
		LoopID: "chain-terminal", TaskID: "task-chain-terminal", Outcome: agentic.OutcomeSuccess,
		Role:        "coordinator",
		Result:      `{"action":"respond_direct","reason":"Optimized the flight plan."}`,
		CompletedAt: time.Now().UTC(),
		Decision: &agentic.CoordinatorDecision{
			Action: agentic.DecideActionRespondDirect,
			Reason: "Optimized the flight plan.",
		},
	}
	data := terminalEnvelopeForDispatch(t, terminal)
	var source struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(data, &source))

	require.Empty(t, c.loopTracker.Get("chain-terminal"), "the tracker is empty, as after a restart")
	require.NoError(t, c.settleAgentTerminal(ctx, data), "origin must be recovered from AGENT_LOOPS ancestry alone")
	require.NoError(t, c.settleAgentTerminal(ctx, data), "redelivery must reuse the same response identity")

	stream, err := tc.Client.GetStream(ctx, "USER_TERMINAL")
	require.NoError(t, err)
	info, err := stream.Info(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), info.State.Msgs, "two deliveries must leave one response identity")

	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Name: "origin-check", FilterSubject: "user.response.http.origin-1", AckPolicy: jetstream.AckExplicitPolicy,
	})
	require.NoError(t, err)
	batch, err := consumer.Fetch(1, jetstream.FetchMaxWait(5*time.Second))
	require.NoError(t, err)
	msg := <-batch.Messages()
	require.NotNil(t, msg, "the response must be published on the ROOT's channel subject")
	require.Equal(t, terminalResponseIDPrefix+source.ID, msg.Headers().Get("Nats-Msg-Id"))

	decoded, err := c.decoder.Decode(msg.Data())
	require.NoError(t, err)
	response, ok := decoded.Payload().(*agentic.UserResponse)
	require.True(t, ok, "expected *agentic.UserResponse, got %T", decoded.Payload())
	require.Equal(t, agentic.ResponseTypeResult, response.Type)
	require.Equal(t, "Optimized the flight plan.", response.Content)
	require.Equal(t, "chain-terminal", response.InReplyTo)
	require.Equal(t, "http", response.ChannelType)
	require.Equal(t, "origin-1", response.ChannelID)
	require.Equal(t, "user-1", response.UserID)
	require.NoError(t, msg.Ack())
}

func TestIntegrationDispatchPersistedLoopReadUsesDeclaredAgentLoopsPort(t *testing.T) {
	ctx := t.Context()
	const altBucket = "AGENT_LOOPS_ALT"
	tc := natsclient.NewTestClient(t,
		natsclient.WithKVBuckets(altBucket),
		natsclient.WithStreams(
			natsclient.TestStreamConfig{Name: "USER_TERMINAL", Subjects: []string{"user.>"}},
		),
	)
	reg := payloadregistry.NewWithSubset(t, agentic.RegisterPayloads)
	c := &Component{
		config: dispatchConfigWithAgentLoopsBucket(t, altBucket), decoder: message.NewDecoder(reg),
		natsClient: tc.Client, logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		loopTracker: NewLoopTracker(), metrics: getMetrics(nil),
	}
	c.config.Ports.Outputs[2].Config = component.JetStreamPort{Subjects: []string{"user.response.>"}, StreamName: "USER_TERMINAL"}

	// The record exists ONLY in the non-default bucket. A predicted bucket
	// name reads AGENT_LOOPS, finds nothing, and NAKs forever.
	kv, err := tc.GetKVBucket(ctx, altBucket)
	require.NoError(t, err)
	putLoopRecord(t, ctx, kv, agentic.LoopEntity{
		ID: "alt-loop", TaskID: "task-alt-loop", State: agentic.LoopStateComplete,
		ChannelType: "http", ChannelID: "alt-origin", UserID: "alt-user",
	})

	data := terminalEnvelopeForDispatch(t, &agentic.LoopCompletedEvent{
		LoopID: "alt-loop", TaskID: "task-alt-loop", Outcome: agentic.OutcomeSuccess,
		Result: "the alt result", CompletedAt: time.Now().UTC(),
	})
	require.NoError(t, c.settleAgentTerminal(ctx, data))

	stream, err := tc.Client.GetStream(ctx, "USER_TERMINAL")
	require.NoError(t, err)
	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Name: "alt-check", FilterSubject: "user.response.http.alt-origin", AckPolicy: jetstream.AckExplicitPolicy,
	})
	require.NoError(t, err)
	batch, err := consumer.Fetch(1, jetstream.FetchMaxWait(5*time.Second))
	require.NoError(t, err)
	msg := <-batch.Messages()
	require.NotNil(t, msg, "the record's route must be read from the BOUND bucket")

	decoded, err := c.decoder.Decode(msg.Data())
	require.NoError(t, err)
	response, ok := decoded.Payload().(*agentic.UserResponse)
	require.True(t, ok, "expected *agentic.UserResponse, got %T", decoded.Payload())
	require.Equal(t, agentic.ResponseTypeResult, response.Type)
	require.Equal(t, "the alt result", response.Content)
	require.Equal(t, "alt-origin", response.ChannelID)
	require.Equal(t, "alt-user", response.UserID)
	require.NoError(t, msg.Ack())
}
