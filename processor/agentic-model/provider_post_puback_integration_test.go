//go:build integration

package agenticmodel

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// postPubAckInterruptedMessage preserves the real delivery and heartbeat methods.
// Only terminal ACK is interrupted, without sending anything to the server.
type postPubAckInterruptedMessage struct {
	jetstream.Msg
	ackEntered chan<- jetstream.Msg
	ackRelease <-chan struct{}
}

func (m postPubAckInterruptedMessage) Ack() error {
	select {
	case m.ackEntered <- m.Msg:
	default:
	}
	<-m.ackRelease
	return errors.New("test replacement interrupted source ACK after response PubAck")
}

// spec: agentic-model / Model request settlement is bound to a durable response
func TestIntegrationPostResponsePubAckReplacementReusesLiveProviderResult(t *testing.T) {
	tc := newProviderSettlementNATS(t)
	var calls atomic.Int32
	provider := successfulProvider(&calls)
	t.Cleanup(provider.Close)

	const suffix = "post-response-puback"
	const consumerName = "agentic-model-agent-request-all-" + suffix
	newModel := func() *Component {
		model := newProviderSettlementComponent(t, tc, provider.URL, suffix)
		// Use the existing port configuration and normal policy validation to
		// bound actual server redelivery after the withheld ACK. No test NAK.
		port := model.config.Ports.Inputs[0].Config.(component.JetStreamPort)
		port.AckWait = "2s"
		port.HeartbeatInterval = "500ms"
		model.config.Ports.Inputs[0].Config = port
		var err error
		model.inputPorts, model.outputPorts, err = resolveConfiguredPorts(model.config)
		require.NoError(t, err)
		return model
	}

	ackEntered := make(chan jetstream.Msg, 1)
	ackRelease := make(chan struct{})
	releaseAck := sync.OnceFunc(func() { close(ackRelease) })
	callbackReturned := make(chan struct{}, 1)
	drainIssued := make(chan struct{})
	signalDrain := sync.OnceFunc(func() { close(drainIssued) })
	var firstHandle jetstream.ConsumeContext
	first := newModel()
	first.consumeStream = func(
		ctx context.Context, owner natsclient.PortConsumerContext, cfg natsclient.StreamConsumerConfig,
		handler func(context.Context, jetstream.Msg),
	) (jetstream.ConsumeContext, error) {
		handle, err := tc.Client.ConsumeStreamWithConfig(ctx, owner, cfg, func(msgCtx context.Context, msg jetstream.Msg) {
			handler(msgCtx, postPubAckInterruptedMessage{Msg: msg, ackEntered: ackEntered, ackRelease: ackRelease})
			select {
			case callbackReturned <- struct{}{}:
			default:
			}
		})
		firstHandle = handle
		return handle, err
	}
	first.waitConsumerClosed = func(ctx context.Context, closed <-chan struct{}) error {
		signalDrain()
		select {
		case <-closed:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	t.Cleanup(func() {
		releaseAck()
		require.NoError(t, first.Stop(context.Background()))
	})
	require.NoError(t, first.Start(t.Context()))
	ack, req := publishProviderSettlementRequest(t, tc, suffix)

	var firstDelivery jetstream.Msg
	select {
	case firstDelivery = <-ackEntered:
	case <-time.After(10 * time.Second):
		t.Fatal("first model did not reach source ACK after its live provider response")
	}
	firstMetadata, err := firstDelivery.Metadata()
	require.NoError(t, err)
	require.Equal(t, providerSettlementRequestStream, firstMetadata.Stream)
	require.Equal(t, consumerName, firstMetadata.Consumer)
	require.Equal(t, ack.Sequence, firstMetadata.Sequence.Stream)
	require.Equal(t, uint64(1), firstMetadata.NumDelivered)
	require.Equal(t, int32(1), calls.Load())

	responseStream, err := tc.Client.GetStream(t.Context(), providerSettlementResponseStream)
	require.NoError(t, err)
	committed, err := responseStream.GetLastMsgForSubject(t.Context(), "agent.response."+req.RequestID)
	require.NoError(t, err, "the first model must persist its actual response before source ACK")
	decoded, err := payloadbuiltins.NewTestDecoder(t).Decode(committed.Data)
	require.NoError(t, err)
	response, ok := decoded.Payload().(*agentic.AgentResponse)
	require.True(t, ok)
	require.Equal(t, req.RequestID, response.RequestID)
	require.Equal(t, agentic.StatusComplete, response.Status)
	require.Equal(t, "done", response.Message.Content)

	sourceStream, err := tc.Client.GetStream(t.Context(), providerSettlementRequestStream)
	require.NoError(t, err)
	sourceConsumer, err := sourceStream.Consumer(t.Context(), consumerName)
	require.NoError(t, err)
	info, err := sourceConsumer.Info(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, info.NumAckPending)
	require.Less(t, info.AckFloor.Stream, ack.Sequence)

	stopDone := make(chan error, 1)
	go func() { stopDone <- first.Stop(t.Context()) }()
	select {
	case <-drainIssued:
	case <-time.After(5 * time.Second):
		t.Fatal("first model did not drain its actual consumer")
	}
	select {
	case <-firstHandle.Closed():
		t.Fatal("consumer closed while its source ACK callback was still held")
	default:
	}
	releaseAck()
	select {
	case stopErr := <-stopDone:
		require.NoError(t, stopErr)
	case <-time.After(5 * time.Second):
		t.Fatal("first model did not join after the interrupted ACK returned")
	}
	select {
	case <-callbackReturned:
	default:
		t.Fatal("first model Stop returned before its delivery callback joined")
	}
	info, err = sourceConsumer.Info(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, info.NumAckPending, "replacement must inherit a genuinely unacknowledged source")
	require.Less(t, info.AckFloor.Stream, ack.Sequence)

	redelivered := make(chan jetstream.Msg, 1)
	replacement := newModel()
	replacement.consumeStream = func(
		ctx context.Context, owner natsclient.PortConsumerContext, cfg natsclient.StreamConsumerConfig,
		handler func(context.Context, jetstream.Msg),
	) (jetstream.ConsumeContext, error) {
		return tc.Client.ConsumeStreamWithConfig(ctx, owner, cfg, func(msgCtx context.Context, msg jetstream.Msg) {
			select {
			case redelivered <- msg:
			default:
			}
			handler(msgCtx, msg)
		})
	}
	t.Cleanup(func() { require.NoError(t, replacement.Stop(context.Background())) })
	require.NoError(t, replacement.Start(t.Context()))
	select {
	case delivery := <-redelivered:
		metadata, metadataErr := delivery.Metadata()
		require.NoError(t, metadataErr)
		require.Equal(t, firstMetadata.Stream, metadata.Stream)
		require.Equal(t, firstMetadata.Consumer, metadata.Consumer)
		require.Equal(t, firstMetadata.Sequence.Stream, metadata.Sequence.Stream)
		require.Greater(t, metadata.NumDelivered, firstMetadata.NumDelivered)
		require.Equal(t, firstDelivery.Data(), delivery.Data())
	case <-time.After(10 * time.Second):
		t.Fatal("replacement did not receive the real unacknowledged source redelivery")
	}
	requireProviderSourceAck(t, tc, consumerName, ack.Sequence)
	require.NoError(t, replacement.Stop(t.Context()))
	require.Equal(t, int32(1), calls.Load(), "replacement must reuse the first model's committed response")
	retained, err := responseStream.GetLastMsgForSubject(t.Context(), "agent.response."+req.RequestID)
	require.NoError(t, err)
	require.Equal(t, committed.Sequence, retained.Sequence, "reuse must not publish another response")
	require.Equal(t, committed.Data, retained.Data)
}
