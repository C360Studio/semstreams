package websocket

import (
	"context"
	"encoding/json"
	"testing"

	natspkg "github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestOutput builds an Output with pass-through set as requested, running, and
// with no connected clients — enough to exercise the transform/broadcast decision
// without a server or NATS.
func newTestOutput(t *testing.T, passthrough bool) *Output {
	t.Helper()
	// A valid port satisfies validateConfig; the server is never Start()ed so
	// nothing binds it (no net.Listen → no fixed-port lint concern).
	ws := NewOutputFromConfig(ConstructorConfig{
		Name:        "test-passthrough",
		Port:        8099,
		Path:        "/ws",
		Subjects:    []string{"test.subject"},
		Passthrough: passthrough,
	})
	require.NoError(t, ws.Initialize())
	ws.mu.Lock()
	ws.running = true
	ws.mu.Unlock()
	return ws
}

// TestTransformPayload_PassthroughValidJSON: pass-through returns the producer's
// ORIGINAL bytes unchanged — key order preserved, no timestamp/subject injected.
func TestTransformPayload_PassthroughValidJSON(t *testing.T) {
	ws := newTestOutput(t, true)

	// Key order chosen so that a map round-trip would reorder it (Go marshals map
	// keys sorted), proving no decode/re-encode happened.
	original := []byte(`{"z":1,"a":2,"payload":"already complete"}`)

	out := ws.transformPayload("test.subject", original)

	assert.Equal(t, string(original), string(out), "pass-through must broadcast original bytes byte-for-byte")
	// Explicitly: no envelope fields injected.
	assert.NotContains(t, string(out), "timestamp")
	assert.NotContains(t, string(out), "subject")
}

// TestTransformPayload_PassthroughDoesNotInjectMissingFields: the documented
// contract — with pass-through on, even JSON lacking timestamp/subject is NOT
// injected (producer owns its envelope).
func TestTransformPayload_PassthroughDoesNotInjectMissingFields(t *testing.T) {
	ws := newTestOutput(t, true)

	original := []byte(`{"entity_id":"123","status":"active"}`)
	out := ws.transformPayload("graph.updates", original)

	assert.Equal(t, string(original), string(out))
	assert.NotContains(t, string(out), "timestamp")
	assert.NotContains(t, string(out), "\"subject\"")
}

// TestTransformPayload_PassthroughNonJSONFallsBackToRawData: pass-through is safe
// on a mixed subject — non-JSON still becomes a raw_data envelope.
func TestTransformPayload_PassthroughNonJSONFallsBackToRawData(t *testing.T) {
	ws := newTestOutput(t, true)

	out := ws.transformPayload("graph.raw", []byte("not json"))

	var wrapped map[string]any
	require.NoError(t, json.Unmarshal(out, &wrapped), "non-JSON must be wrapped in a JSON raw_data envelope")
	assert.Equal(t, "raw_data", wrapped["type"])
	assert.Equal(t, "not json", wrapped["data"])
	assert.Equal(t, "graph.raw", wrapped["subject"])
	assert.NotEmpty(t, wrapped["timestamp"])
}

// TestTransformPayload_DefaultInjectsMissingFields: default mode (pass-through
// off) is unchanged — JSON without timestamp/subject gets them injected.
func TestTransformPayload_DefaultInjectsMissingFields(t *testing.T) {
	ws := newTestOutput(t, false)

	out := ws.transformPayload("graph.updates.entity", []byte(`{"entity_id":"123"}`))

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(out, &decoded))
	assert.Equal(t, "123", decoded["entity_id"])
	assert.Equal(t, "graph.updates.entity", decoded["subject"], "default mode must inject the subject")
	assert.NotEmpty(t, decoded["timestamp"], "default mode must inject a timestamp")
}

// TestTransformPayload_DefaultPreservesPresentFields: default mode must not
// overwrite a timestamp/subject the producer already set.
func TestTransformPayload_DefaultPreservesPresentFields(t *testing.T) {
	ws := newTestOutput(t, false)

	out := ws.transformPayload("actual.subject", []byte(`{"subject":"producer.set","timestamp":"2020-01-01T00:00:00Z"}`))

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(out, &decoded))
	assert.Equal(t, "producer.set", decoded["subject"], "must not overwrite a present subject")
	assert.Equal(t, "2020-01-01T00:00:00Z", decoded["timestamp"], "must not overwrite a present timestamp")
}

// TestTransformPayload_DefaultNonJSONRawData: default mode wraps non-JSON as
// raw_data (regression guard on the existing behavior).
func TestTransformPayload_DefaultNonJSONRawData(t *testing.T) {
	ws := newTestOutput(t, false)

	out := ws.transformPayload("x.raw", []byte("plain text"))

	var wrapped map[string]any
	require.NoError(t, json.Unmarshal(out, &wrapped))
	assert.Equal(t, "raw_data", wrapped["type"])
	assert.Equal(t, "plain text", wrapped["data"])
}

// TestBroadcastPayload_BothHandlerEntrypoints verifies both inbound handlers route
// through the shared transform, so pass-through behavior cannot drift between them.
// With no connected clients this exercises the decision + metrics path without a
// server; it must not panic and must update lastActivity.
func TestBroadcastPayload_BothHandlerEntrypoints(t *testing.T) {
	ctx := context.Background()
	payload := []byte(`{"z":1,"a":2}`)

	for _, passthrough := range []bool{false, true} {
		ws := newTestOutput(t, passthrough)

		// Data entrypoint.
		ws.handleNATSMessageData(ctx, payload, "test.subject")
		assert.NotZero(t, ws.lastActivity.Load(), "handleNATSMessageData must update lastActivity")

		// *nats.Msg entrypoint.
		ws2 := newTestOutput(t, passthrough)
		ws2.handleNATSMessage(ctx, &natspkg.Msg{Data: payload, Subject: "test.subject"})
		assert.NotZero(t, ws2.lastActivity.Load(), "handleNATSMessage must update lastActivity")
	}
}
