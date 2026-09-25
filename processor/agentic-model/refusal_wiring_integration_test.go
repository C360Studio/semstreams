//go:build integration

package agenticmodel

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

// TestIntegrationLatchedLaneDeclaresRefusal is the wiring test for this
// component's refusal declarer (#1342, obligation inherited from #1341). Every
// other refusal test calls a recorder directly or builds its own Admission, so
// passing nil as onRefused at the setupConsumer call site survives them all.
// Here the production callback, bound by Start over real NATS, latches on a
// delivery whose metadata cannot be read, and a delivery then reaching the
// closed lane must move this component's own refusal counter.
//
// The request lane is pinned at one delivery in flight, so the server never
// sends a second delivery while the latching one stays unsettled and no
// buffered delivery can reach the closed admission. The test therefore hands
// the production callback the same real message a second time after the latch
// — the shape of a redelivery arriving at a lane that has already closed.
//
// spec: jetstream-consumer-policy / control loss shuts down through the existing exact owner
func TestIntegrationLatchedLaneDeclaresRefusal(t *testing.T) {
	const lane = "agent.request"
	tc := newProviderSettlementNATS(t)
	var calls atomic.Int32
	provider := successfulProvider(&calls)
	defer provider.Close()

	c := newProviderSettlementComponent(t, tc, provider.URL, "refusal-wiring")
	var (
		laneCfg    natsclient.StreamConsumerConfig
		laneHandle jetstream.ConsumeContext
	)
	c.consumeStream = func(
		ctx context.Context,
		owner natsclient.PortConsumerContext,
		cfg natsclient.StreamConsumerConfig,
		callback func(context.Context, jetstream.Msg),
	) (jetstream.ConsumeContext, error) {
		laneCfg = cfg
		var once sync.Once
		handle, err := tc.Client.ConsumeStreamWithConfig(ctx, owner, cfg,
			func(msgCtx context.Context, msg jetstream.Msg) {
				once.Do(func() {
					callback(msgCtx, metadataUnavailableMsg{Msg: msg})
				})
				callback(msgCtx, msg)
			})
		laneHandle = handle
		return handle, err
	}
	require.NoError(t, c.Start(t.Context()))
	t.Cleanup(func() { require.NoError(t, c.Stop(context.Background())) })
	require.NotNil(t, laneHandle, "Start did not bind the request lane")

	counter := c.metrics.deliveryRefusals.WithLabelValues(lane)
	before := testutil.ToFloat64(counter)
	publishProviderSettlementRequest(t, tc, "refusal-wiring")

	select {
	case <-laneHandle.Closed():
	case <-time.After(10 * time.Second):
		t.Fatal("the latched lane's exact handle was not drained")
	}
	require.Equal(t, before+1, testutil.ToFloat64(counter),
		"a delivery reaching the latched lane must reach this lane's own refusal declarer")
	require.Equal(t, "delivery ownership lost", c.Health().Status)
	require.Zero(t, calls.Load(), "neither delivery may reach the provider")

	stream, err := tc.Client.GetStream(t.Context(), laneCfg.StreamName)
	require.NoError(t, err)
	consumer, err := stream.Consumer(t.Context(), laneCfg.ConsumerName)
	require.NoError(t, err)
	info, err := consumer.Info(t.Context())
	require.NoError(t, err)
	require.Equal(t, uint64(1), info.Delivered.Consumer)
	require.Zero(t, info.AckFloor.Consumer)
	require.Equal(t, 1, info.NumAckPending)
}

// metadataUnavailableMsg is a real delivery whose server metadata cannot be
// read, which the typed heartbeat helper answers with Quarantine and owner stop
// before any work runs.
type metadataUnavailableMsg struct{ jetstream.Msg }

func (metadataUnavailableMsg) Metadata() (*jetstream.MsgMetadata, error) {
	return nil, errors.New("injected: delivery metadata unavailable")
}
