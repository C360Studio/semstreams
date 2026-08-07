package websocket

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
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
	ws := mustNewOutputFromConfig(t, ConstructorConfig{
		Name:        "test-passthrough",
		Path:        "/ws",
		InputPorts:  natsInputDefinitions([]string{"test.subject"}),
		OutputPorts: websocketOutputDefinitions(8099),
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

	// transformPayload returns the producer bytes verbatim on the pass-through
	// branch (the downstream envelope marshal still compacts/escapes, but does
	// NOT reorder keys or reformat numbers — that is the gh#471 win). A map
	// round-trip would have sorted the keys to a,payload,z.
	assert.Equal(t, string(original), string(out), "pass-through must return the original bytes (no reorder/re-encode)")
}

// TestTransformPayload_PassthroughDoesNotInjectMissingFields: the documented
// contract — with pass-through on, even JSON lacking timestamp/subject is NOT
// injected (producer owns its envelope).
func TestTransformPayload_PassthroughDoesNotInjectMissingFields(t *testing.T) {
	ws := newTestOutput(t, true)

	original := []byte(`{"entity_id":"123","status":"active"}`)
	out := ws.transformPayload("graph.updates", original)

	// The Equal assertion fully covers "not injected": an injected timestamp or
	// subject would make out != original.
	assert.Equal(t, string(original), string(out), "pass-through must not inject missing envelope fields")
}

// TestCreateOutput_PassthroughConfigRoundTrip drives the operator config wire:
// raw JSON → SafeUnmarshal → Config.Passthrough → factory → Output.passthrough.
// The other tests set ConstructorConfig directly, bypassing this json seam; this
// locks it so a json-tag rename or a dropped factory assignment fails loudly
// (memory: feedback_polymorphic_config_needs_json_roundtrip_test).
func TestCreateOutput_PassthroughConfigRoundTrip(t *testing.T) {
	// CreateOutput only requires a non-nil NATSClient (it does not dial), so an
	// empty client keeps this a fast unit test with no container.
	deps := component.Dependencies{NATSClient: &natsclient.Client{}}

	t.Run("passthrough true reaches Output", func(t *testing.T) {
		out, err := CreateOutput(json.RawMessage(`{"passthrough":true}`), deps)
		require.NoError(t, err)
		require.True(t, out.(*Output).passthrough,
			`operator config {"passthrough":true} must reach Output.passthrough`)
	})

	t.Run("absent defaults to false", func(t *testing.T) {
		out, err := CreateOutput(json.RawMessage(`{}`), deps)
		require.NoError(t, err)
		require.False(t, out.(*Output).passthrough,
			"pass-through must default to false when absent from operator config")
	})
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

// TestPassthrough_EnvelopeMarshalCompactsAndEscapes documents the ACTUAL
// end-to-end guarantee (reviewer MEDIUM): the pass-through payload still flows
// through the shared MessageEnvelope marshal, which compacts insignificant
// whitespace and HTML-escapes < > & in the RawMessage. So the guarantee is NOT
// literal byte-identity — it is that key ORDER and numeric PRECISION are
// preserved (the gh#471 win: no map re-sort, no strconv.fmtF re-formatting).
func TestPassthrough_EnvelopeMarshalCompactsAndEscapes(t *testing.T) {
	ws := newTestOutput(t, true)

	// Pretty-printed, keys in non-sorted order, containing < > &, and a
	// high-precision float that the default map path would reformat.
	original := []byte(`{ "z": 1, "a": 2, "html": "<svg>&amp;</svg>", "f": 0.12345678901234567 }`)

	payload := ws.transformPayload("t", original)
	require.Equal(t, string(original), string(payload), "transformPayload returns bytes verbatim")

	// The envelope marshal is where compaction/escaping happens.
	_, envelopeData := ws.prepareMessageEnvelope(payload)
	var env MessageEnvelope
	require.NoError(t, json.Unmarshal(envelopeData, &env))
	got := string(env.Payload)

	// Semantically equal to the producer JSON (the escaped < decodes back to <)...
	assert.JSONEq(t, string(original), got)
	// ...but NOT byte-identical: whitespace is compacted and < > & are HTML-escaped
	// (each < becomes a < sequence), so no literal '<' survives even though it
	// decodes back for JSONEq.
	assert.NotEqual(t, string(original), got, "envelope marshal compacts/escapes — not literal byte-identity")
	assert.NotContains(t, got, "<", "the envelope marshal HTML-escapes < (no literal < survives)")
	assert.NotContains(t, got, "  ", "the envelope marshal compacts insignificant whitespace")
	// The actual win: key order preserved (z before a, not sorted) and the
	// high-precision float is NOT reformatted (default map path -> float64 would be).
	assert.Contains(t, got, `"z":1,"a":2`, "key order preserved (not sorted)")
	assert.Contains(t, got, `0.12345678901234567`, "numeric precision preserved (no float64 reformat)")
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
