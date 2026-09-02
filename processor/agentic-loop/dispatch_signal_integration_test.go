//go:build integration

package agenticloop

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	agenticdispatch "github.com/c360studio/semstreams/processor/agentic-dispatch"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// spec: agentic-dispatch / One control-signal payload travels the loop signal subject
// The endpoint actually stops the loop.
//
// This is the joint the unification exists to close, so it is driven end to
// end and nothing at the seam is reconstructed:
//
//	POST /loops/{id}/signal  →  dispatch's REAL route (RegisterHTTPHandlers)
//	                         →  a REAL NATS subject
//	                         →  the exact bytes off the wire
//	                         →  agentic-loop's REAL handleSignalMessage
//	                         →  the loop's cancellation event on agent.complete
//
// The dispatch component is built through its own NewComponent, and the loop it
// is asked to cancel exists only in the durable AGENT_LOOPS record — this
// process never tracked it — so the gate's merged read is exercised too.
//
// The requester is deliberately NOT the owner: a cancel_any operator cancels
// a loop owned by someone else. That is what makes the identity assertion
// discriminating — with requester == owner the two candidate sources for the
// signal's user field are indistinguishable, and a mutation swapping the
// requester for the owner survives (measured: it did, before this fixture
// separated them).
//
// Before this change the endpoint published a dispatch-local payload the loop's
// handler dropped as an unexpected type, answered 200, and cancelled nothing.
// What fails here if that returns is the completion assertion, not a type
// assertion on a struct nobody put on a wire.
func TestHTTPSignalEndpointCancelsTheLoop(t *testing.T) {
	ctx := t.Context()
	const loopID = "44444444-4444-4444-8444-444444444444"

	tc := natsclient.NewTestClient(t,
		natsclient.WithKVBuckets("AGENT_LOOPS"),
		natsclient.WithStreams(natsclient.TestStreamConfig{
			Name: "AGENT", Subjects: []string{"agent.>"},
		}),
	)

	// The loop side: a real loop in a real agentic-loop component.
	loopComp := &Component{
		config:     DefaultConfig(),
		handler:    NewMessageHandler(DefaultConfig()),
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		decoder:    payloadbuiltins.NewTestDecoder(t),
		natsClient: tc.Client,
	}
	_, err := loopComp.handler.loopManager.CreateLoopWithID(loopID, "task-1", "general", "test-model", 5)
	require.NoError(t, err)
	entity, err := loopComp.handler.GetLoop(loopID)
	require.NoError(t, err)
	require.False(t, entity.State.IsTerminal(), "the loop is running before the signal")

	// The durable record the dispatch gate reads. Dispatch never tracked this
	// loop, so admission depends entirely on the merged read.
	kv, err := tc.GetKVBucket(ctx, "AGENT_LOOPS")
	require.NoError(t, err)
	record, err := json.Marshal(agentic.LoopEntity{
		ID: loopID, TaskID: "task-1", UserID: "loop-owner",
		ChannelType: "http", ChannelID: "session-1",
		State: agentic.LoopStateExecuting, MaxIterations: 5,
	})
	require.NoError(t, err)
	_, err = kv.Put(ctx, loopID, record)
	require.NoError(t, err)

	// The dispatch side: its own constructor, its own route table.
	mux := http.NewServeMux()
	dispatchHTTPHandlers(t, tc, mux)

	body := strings.NewReader(`{"type":"cancel","reason":"operator asked"}`)
	req := httptest.NewRequest(http.MethodPost, "/loops/"+loopID+"/signal", body)
	req = req.WithContext(agenticdispatch.WithIdentity(ctx, "cancel-operator"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code,
		"a cancel_any holder is admitted on a loop they do not own: %s", rec.Body.String())

	// The exact bytes dispatch put on the subject — not a reconstruction.
	data := fetchOne(t, ctx, tc, "AGENT", "signal-e2e", "agent.signal."+loopID)

	loopComp.handleSignalMessage(ctx, data)

	// The loop cancelled. Its in-process entity is released on settlement
	// (the terminal-release contract), so the observable outcome is the
	// cancellation event the loop published, which also carries the identity
	// that travelled the whole way from the HTTP request.
	completion := fetchOne(t, ctx, tc, "AGENT", "complete-e2e", "agent.complete."+loopID)
	decoded, err := loopComp.decoder.Decode(completion)
	require.NoError(t, err)
	cancelled, ok := decoded.Payload().(*agentic.LoopCancelledEvent)
	require.Truef(t, ok, "expected *agentic.LoopCancelledEvent, got %T", decoded.Payload())
	assert.Equal(t, loopID, cancelled.LoopID)
	assert.Equal(t, agentic.OutcomeCancelled, cancelled.Outcome)
	assert.Equal(t, "cancel-operator", cancelled.CancelledBy,
		"the REQUESTER travelled from the HTTP request to the cancellation record — "+
			"not the loop's owner, which is loop-owner")
}

// dispatchHTTPHandlers builds a dispatch component through its own constructor
// and registers its real routes. Nothing here reaches into dispatch internals:
// if the endpoint's wiring changes, this fails at the route, which is the point.
func dispatchHTTPHandlers(t *testing.T, tc *natsclient.TestClient, mux *http.ServeMux) {
	t.Helper()
	config := agenticdispatch.DefaultConfig()
	config.Permissions.CancelAny = []string{"cancel-operator"}
	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)
	dispatch, err := agenticdispatch.NewComponent(rawConfig, component.Dependencies{
		NATSClient:      tc.Client,
		PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
		ModelRegistry: &model.Registry{
			Endpoints: map[string]*model.EndpointConfig{"test-model": {Model: "test-model", MaxTokens: 128000}},
			Defaults:  model.DefaultsConfig{Model: "test-model"},
		},
	})
	require.NoError(t, err)
	registrar, ok := dispatch.(interface {
		RegisterHTTPHandlers(prefix string, mux *http.ServeMux)
	})
	require.True(t, ok, "dispatch must still register HTTP handlers")
	registrar.RegisterHTTPHandlers("/", mux)
}

// fetchOne pulls exactly one message off a subject and fails if none arrives.
func fetchOne(t *testing.T, ctx context.Context, tc *natsclient.TestClient, streamName, consumerName, subject string) []byte {
	t.Helper()
	stream, err := tc.Client.GetStream(ctx, streamName)
	require.NoError(t, err)
	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Name: consumerName, FilterSubject: subject, AckPolicy: jetstream.AckExplicitPolicy,
	})
	require.NoError(t, err)
	batch, err := consumer.Fetch(1, jetstream.FetchMaxWait(10*time.Second))
	require.NoError(t, err)
	msg, ok := <-batch.Messages()
	require.NoError(t, batch.Error())
	require.Truef(t, ok && msg != nil, "no message arrived on %s", subject)
	require.NoError(t, msg.Ack())
	return msg.Data()
}
