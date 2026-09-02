//go:build integration

package agenticdispatch

import (
	"context"
	"io"
	"log/slog"
	"net/http"
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
// It enumerates the class rather than the motivating instance: both lanes that
// publish on this subject in this tree — the /cancel chat command and the HTTP
// signal endpoint — are driven, and every byte slice that lands is decoded
// through the production decoder built from payloadbuiltins.Register, which is
// the same decode agentic-loop's signal handler performs. A third producer
// added later without a driver here shows up as a message this test never
// counted, not as a passing test.
//
// This is also what catches the envelope half of the invariant. Before this
// change the chat lane published a BARE agentic.UserSignal — the right payload
// type with no BaseMessage envelope — which fails at the wire-format unmarshal
// with "cannot unmarshal string into Go struct field wireFormat.type", so the
// loop dropped it exactly as it dropped the HTTP lane's message. A test that
// only asserted the Go type of the struct being published would have passed
// over both defects.
func TestSignalSubjectCarriesExactlyOnePayloadType(t *testing.T) {
	ctx := t.Context()
	tc := natsclient.NewTestClient(t, natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: "AGENT_SIGNAL", Subjects: []string{"agent.signal.>"}},
	))
	c := newSignalWireComponent(t, tc)
	c.config.Ports.Outputs = signalPortsOnStream(t, c.config.Ports.Outputs, "AGENT_SIGNAL")

	// Lane 1 — the chat command.
	msg := seamUserMessage("user-a")
	resp, err := c.handleCancelCommand(ctx, msg, []string{signalWireLoopID}, "")
	require.NoError(t, err, "the chat lane must reach the subject")
	require.Equal(t, agentic.ResponseTypeStatus, resp.Type)

	// Lane 2 — the HTTP signal endpoint.
	rec := seamHTTPCall(t, c.handleLoopSignal, http.MethodPost,
		"/loops/"+signalWireLoopID+"/signal", signalWireLoopID, `{"type":"pause","reason":"operator asked"}`, "user-a")
	require.Equal(t, http.StatusOK, rec.Code, "the endpoint must reach the subject")

	messages := signalSubjectMessages(t, ctx, tc, "one-type-check")
	require.Len(t, messages, 2, "both lanes published on the loop's signal subject")

	decoder := payloadbuiltins.NewTestDecoder(t)
	verbs := map[string]bool{}
	for i, data := range messages {
		decoded, err := decoder.Decode(data)
		require.NoErrorf(t, err, "message %d must decode through the production decoder: %s", i, data)
		signal, ok := decoded.Payload().(*agentic.UserSignal)
		require.Truef(t, ok, "message %d is %T, not *agentic.UserSignal", i, decoded.Payload())
		assert.Equal(t, agentic.Domain, decoded.Type().Domain)
		assert.Equal(t, agentic.CategorySignal, decoded.Type().Category,
			"the retired signal_message category must not appear on this subject")
		assert.Equal(t, signalWireLoopID, signal.LoopID)
		assert.Equal(t, "user-a", signal.UserID, "the signal records the requester")
		verbs[signal.Type] = true
	}
	assert.Equal(t, map[string]bool{"cancel": true, "pause": true}, verbs,
		"one message per lane, carrying the verb that lane asked for")
}

// spec: agentic-dispatch / One control-signal payload travels the loop signal subject
// I11 on the wire: for a refused signal request, NOTHING is published on the
// loop's signal subject. The unit form asserts the status that proves the
// ordering; this one counts the messages that actually arrived.
func TestIntegrationRefusedSignalPublishesNothingOnTheSubject(t *testing.T) {
	ctx := t.Context()
	tc := natsclient.NewTestClient(t, natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: "AGENT_SIGNAL", Subjects: []string{"agent.signal.>"}},
	))
	c := newSignalWireComponent(t, tc)
	c.config.Ports.Outputs = signalPortsOnStream(t, c.config.Ports.Outputs, "AGENT_SIGNAL")

	rec := seamHTTPCall(t, c.handleLoopSignal, http.MethodPost,
		"/loops/"+signalWireLoopID+"/signal", signalWireLoopID, `{"type":"cancel"}`, "user-b")
	require.Equal(t, http.StatusForbidden, rec.Code)

	assert.Empty(t, signalSubjectMessages(t, ctx, tc, "refusal-check"),
		"a refused request puts nothing on the loop's signal subject")

	stream, err := tc.Client.GetStream(ctx, "AGENT_SIGNAL")
	require.NoError(t, err)
	info, err := stream.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(0), info.State.Msgs, "the stream is empty, not merely the filtered view")
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
