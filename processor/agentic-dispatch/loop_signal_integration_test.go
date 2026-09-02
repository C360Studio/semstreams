//go:build integration

package agenticdispatch

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const signalWireLoopID = "33333333-3333-4333-8333-333333333333"

// newSignalWireComponent wires a dispatch Component on a REAL NATS client with
// a real AGENT stream, so a publish lands on the subject instead of failing on
// a disconnected client. The loop is live and owned by user-a in both sources
// the gate merges.
func newSignalWireComponent(t *testing.T, tc *natsclient.TestClient) *Component {
	t.Helper()
	c := &Component{
		config:        DefaultConfig(),
		modelRegistry: newTestRegistry(),
		logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		loopTracker:   NewLoopTrackerWithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
		registry:      NewCommandRegistry(),
		metrics:       getMetrics(metric.NewMetricsRegistry()),
		natsClient:    tc.Client,
		decoder:       payloadbuiltins.NewTestDecoder(t),
	}
	c.registerBuiltinCommands()
	trackLoopOwnedBy(c, signalWireLoopID, "user-a")
	return c
}

// signalSubjectMessages drains everything currently on the loop's signal
// subject. It filters on the subject rather than on the stream so a test that
// asserts "nothing was published" is answering about THIS loop's lane.
func signalSubjectMessages(t *testing.T, ctx context.Context, tc *natsclient.TestClient, consumerName string) [][]byte {
	t.Helper()
	stream, err := tc.Client.GetStream(ctx, "AGENT_SIGNAL")
	require.NoError(t, err)
	info, err := stream.Info(ctx)
	require.NoError(t, err)
	if info.State.Msgs == 0 {
		return nil
	}
	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Name:          consumerName,
		FilterSubject: "agent.signal." + signalWireLoopID,
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	require.NoError(t, err)
	batch, err := consumer.Fetch(int(info.State.Msgs), jetstream.FetchMaxWait(5*time.Second))
	require.NoError(t, err)
	var out [][]byte
	for msg := range batch.Messages() {
		out = append(out, msg.Data())
		require.NoError(t, msg.Ack())
	}
	require.NoError(t, batch.Error())
	return out
}

// spec: agentic-dispatch / One control-signal payload travels the loop signal subject
// I10: every message on agent.signal.<loop_id>, FROM ANY LANE, decodes to
// exactly one payload type.
//
// It enumerates the class rather than the motivating instance: every lane that
// publishes on this subject in this tree is driven, and every byte slice that
// lands is decoded through the production decoder built from
// payloadbuiltins.Register, which is the same decode agentic-loop's signal
// handler performs. Since the HTTP signal endpoint was deleted, the /cancel
// chat command is the only such lane; a second producer added later without a
// driver here shows up as a message this test never counted, not as a passing
// test.
//
// This is also what catches the envelope half of the invariant. Before this
// change the chat lane published a BARE agentic.UserSignal — the right payload
// type with no BaseMessage envelope — which fails at the wire-format unmarshal
// with "cannot unmarshal string into Go struct field wireFormat.type", so the
// loop dropped it exactly as it dropped the deleted endpoint's message. A test
// that only asserted the Go type of the struct being published would have
// passed over both defects.
func TestSignalSubjectCarriesExactlyOnePayloadType(t *testing.T) {
	ctx := t.Context()
	tc := natsclient.NewTestClient(t, natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: "AGENT_SIGNAL", Subjects: []string{"agent.signal.>"}},
	))
	c := newSignalWireComponent(t, tc)
	c.config.Ports.Outputs = signalPortsOnStream(t, c.config.Ports.Outputs, "AGENT_SIGNAL")

	// The chat command. The requester arrives on a DIFFERENT channel from the
	// loop's own route, which is what makes the route assertion below
	// discriminating: with the two equal, taking the route from the request
	// instead of from the gate's merged facts is unobservable.
	msg := seamUserMessage("user-a")
	msg.ChannelType = "slack"
	msg.ChannelID = "C-999"
	resp, err := c.handleCancelCommand(ctx, msg, []string{signalWireLoopID}, "")
	require.NoError(t, err, "the chat lane must reach the subject")
	require.Equal(t, agentic.ResponseTypeStatus, resp.Type)

	messages := signalSubjectMessages(t, ctx, tc, "one-type-check")
	require.Len(t, messages, 1, "the one remaining lane published on the loop's signal subject")

	decoder := payloadbuiltins.NewTestDecoder(t)
	decoded, err := decoder.Decode(messages[0])
	require.NoErrorf(t, err, "the message must decode through the production decoder: %s", messages[0])
	signal, ok := decoded.Payload().(*agentic.UserSignal)
	require.Truef(t, ok, "the message is %T, not *agentic.UserSignal", decoded.Payload())
	assert.Equal(t, agentic.Domain, decoded.Type().Domain)
	assert.Equal(t, agentic.CategorySignal, decoded.Type().Category,
		"the retired signal_message category must not appear on this subject")
	assert.Equal(t, signalWireLoopID, signal.LoopID)
	assert.Equal(t, "user-a", signal.UserID, "the signal records the requester")
	assert.Equal(t, "http", signal.ChannelType,
		"the route is the LOOP's, taken from the gate's merged facts — the chat "+
			"requester arrived on slack")
	assert.Equal(t, "session-1", signal.ChannelID)
	assert.Equal(t, agentic.SignalCancel, signal.Type)
}

// spec: agentic-dispatch / The ownership model binds the user lane, and approval is deliberately not owner-scoped
// I11 on the wire: for a refused cancel, NOTHING is published on the loop's
// signal subject. The unit form asserts the answer the requester gets; this one
// counts the messages that actually arrived.
func TestIntegrationRefusedCancelPublishesNothingOnTheSubject(t *testing.T) {
	ctx := t.Context()
	tc := natsclient.NewTestClient(t, natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: "AGENT_SIGNAL", Subjects: []string{"agent.signal.>"}},
	))
	c := newSignalWireComponent(t, tc)
	c.config.Ports.Outputs = signalPortsOnStream(t, c.config.Ports.Outputs, "AGENT_SIGNAL")

	resp, err := c.handleCancelCommand(ctx, seamUserMessage("user-b"), []string{signalWireLoopID}, "")
	require.NoError(t, err)

	// The wire is asserted BEFORE the answer, and with assert rather than
	// require, so the count is the assertion that reports a publish-then-refuse
	// rather than being skipped by an earlier fatal on the response.
	stream, err := tc.Client.GetStream(ctx, "AGENT_SIGNAL")
	require.NoError(t, err)
	info, err := stream.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(0), info.State.Msgs,
		"a refused request puts nothing on the loop's signal subject")
	assert.Empty(t, signalSubjectMessages(t, ctx, tc, "refusal-check"),
		"nor anything the subject filter would pick up")

	assert.Equal(t, agentic.ResponseTypeError, resp.Type, "and the caller is told why")
	assert.Contains(t, resp.Content, "does not own")
}

// signalPortsOnStream rebinds the declared signal output port to the
// independently named test stream WITHOUT changing its subject contract — the
// same fixture shape terminal settlement uses. It rebinds by port NAME rather
// than by index so a reordering of the default port list cannot silently point
// this at some other lane.
func signalPortsOnStream(t *testing.T, ports []component.PortDefinition, streamName string) []component.PortDefinition {
	t.Helper()
	found := false
	for i := range ports {
		if ports[i].Name != signalOutputPortName {
			continue
		}
		ports[i].Config = component.JetStreamPort{Subjects: []string{"agent.signal.*"}, StreamName: streamName}
		found = true
	}
	require.True(t, found, "the %q output port must be declared", signalOutputPortName)
	return ports
}
