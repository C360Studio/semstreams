package gateddagexec_test

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadbuiltins"
	gateddagexec "github.com/c360studio/semstreams/processor/gated-dag"
	"github.com/stretchr/testify/require"
)

// TestDispatchMessage_ProductionDecoderRoundTrip drives the real publish wrap
// (NewBaseMessage) through the production decoder (every builtin registered) to
// prove the dispatch envelope is registered + decodable end-to-end — not a
// shape-cast against an anonymous struct (production-decoder round-trip
// discipline). Lives in an external _test package to avoid the
// payloadbuiltins → gated-dag import cycle.
func TestDispatchMessage_ProductionDecoderRoundTrip(t *testing.T) {
	msg := &gateddagexec.DispatchMessage{
		UnitEntityID:   "acme.ops.plan.fanout.unit.7",
		FanOutWorkflow: gateddagexec.FanOutWorkflow,
	}
	base := message.NewBaseMessage(msg.Schema(), msg, "test")
	data, err := json.Marshal(base)
	require.NoError(t, err)

	decoded, err := payloadbuiltins.NewTestDecoder(t).Decode(data)
	require.NoError(t, err)

	got, ok := decoded.Payload().(*gateddagexec.DispatchMessage)
	require.Truef(t, ok, "decoded payload must be *DispatchMessage, got %T", decoded.Payload())
	require.Equal(t, "acme.ops.plan.fanout.unit.7", got.UnitEntityID)
	require.Equal(t, gateddagexec.FanOutWorkflow, got.FanOutWorkflow)
}
