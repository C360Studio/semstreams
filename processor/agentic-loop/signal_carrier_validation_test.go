package agenticloop

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// A signal published straight onto the subject never passed dispatch's gate,
// and the decoder does not validate, so before #1238 its malformed token went
// to the cancel path, missed memory, found no record, and was acknowledged as
// a stale drop — the carrier's own refusal was unreachable on this lane. The
// token here is the uppercase form of a live loop's token: close enough that a
// case-folding lookup could cancel the wrong loop, and not a minted token.
//
// Driven through the production agent.signal callback that setupSubscriptions
// wires, so the disposition observed is the one the consumer applies.
//
// spec: agentic-loop / Loop input classes settle after owner-specific durable done
func TestMalformedSignalCarrierIsTerminatedOnReceipt(t *testing.T) {
	discoverable, err := NewComponent([]byte(`{}`), component.Dependencies{
		NATSClient: &natsclient.Client{}, PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
	})
	require.NoError(t, err)
	c := discoverable.(*Component)
	c.started = true
	c.startTime = time.Now()
	var logs bytes.Buffer
	c.logger = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	// No record for any token: without the refusal, the missing-loop branch
	// classifies the signal stale and acknowledges it.
	c.loopsBucket = recordLoopBucket{records: map[string]agentic.LoopEntity{}}

	loopID, err := c.handler.loopManager.CreateLoop("task-signal", "general", "model", 3)
	require.NoError(t, err)
	c.waitForStreamInput = func(context.Context, string) error { return nil }
	callbacks := make(map[string]func(context.Context, jetstream.Msg))
	handles := make(map[string]*loopPolicyHandle)
	c.consumeStream = func(_ context.Context, _ context.Context, owner natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig, callback func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
		handle := &loopPolicyHandle{closed: make(chan struct{})}
		callbacks[owner.Port] = callback
		handles[owner.Port] = handle
		return handle, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	require.NoError(t, c.setupSubscriptions(ctx, ctx))

	// BaseMessage.MarshalJSON validates, so the in-tree publishers cannot
	// produce these bytes; a direct publisher writing its own JSON can. Mint a
	// valid envelope and swap the token in the wire bytes.
	valid := baseMessageBytes(t, &agentic.UserSignal{
		SignalID: "signal-malformed", Type: agentic.SignalCancel, LoopID: loopID,
		UserID: "operator", Timestamp: time.Now().UTC(),
	})
	malformed := bytes.ReplaceAll(valid, []byte(loopID), []byte(strings.ToUpper(loopID)))
	require.NotEqual(t, valid, malformed)
	msg := &loopSettlementMsg{data: malformed}
	callbacks["agent.signal"](ctx, msg)

	require.Equal(t, int32(1), msg.terms.Load(), "a malformed carrier is permanently invalid")
	require.Zero(t, msg.acks.Load(), "a malformed carrier was acknowledged as if it were a stale drop")
	require.Zero(t, msg.naks.Load())
	require.Contains(t, logs.String(), "is not a framework-minted loop token",
		"the refusal must name its cause where the operator reads the lane's errors")
	require.Zero(t, handles["agent.signal"].drains.Load(), "an invalid input is not an owner failure")

	entity, err := c.handler.GetLoop(loopID)
	require.NoError(t, err, "the live loop was released by a signal that never named it")
	require.False(t, entity.State.IsTerminal(), "the live loop was cancelled by a malformed token")

	cancel()
	for _, binding := range c.consumers {
		<-binding.Done()
	}
}
