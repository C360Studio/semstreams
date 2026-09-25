//go:build integration

package agenticdispatch

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

// TestIntegrationEveryLaneDeclaresBufferedRefusal is the wiring test for this
// component's three refusal declarers (#1342, obligation inherited from #1341):
// one per NewAdmission call site in setupSubscriptions. Every other refusal
// test calls recordDeliveryRefused directly or builds its own Admission, so
// passing nil as onRefused at any of the three call sites survives them all.
//
// Per lane, the production callback bound by Start over real NATS latches on
// its first delivery while the server has already sent it a second; the
// drained handle flushes that second delivery into the closed lane, and only
// that call site's own declarer can move the counter under that lane's label.
//
// spec: jetstream-consumer-policy / control loss shuts down through the existing exact owner
func TestIntegrationEveryLaneDeclaresBufferedRefusal(t *testing.T) {
	rows := []struct {
		lane    string
		subject string
		// latch builds the first delivery the production callback sees. The
		// user.message lane settles its own work, so it latches on the
		// production handler's panic; the terminal lanes run the typed
		// heartbeat helper, which answers unreadable delivery metadata with
		// owner stop before any work runs.
		latch func(jetstream.Msg) jetstream.Msg
		data  func(t *testing.T) []byte
	}{
		{
			lane: "user.message", subject: "user.message.http",
			latch: func(msg jetstream.Msg) jetstream.Msg { return msg },
			data:  unknownCommandForRefusal,
		},
		{
			lane: "agent.complete", subject: "agent.complete.refusal",
			latch: func(msg jetstream.Msg) jetstream.Msg { return metadataUnavailableMsg{Msg: msg} },
			data:  func(*testing.T) []byte { return []byte(`{}`) },
		},
		{
			lane: "agent.failed", subject: "agent.failed.refusal",
			latch: func(msg jetstream.Msg) jetstream.Msg { return metadataUnavailableMsg{Msg: msg} },
			data:  func(*testing.T) []byte { return []byte(`{}`) },
		},
	}
	for _, row := range rows {
		t.Run(row.lane, func(t *testing.T) {
			ctx := t.Context()
			tc := natsclient.NewTestClient(t,
				natsclient.WithKVBuckets(defaultAgentLoopsBucket(t)),
				natsclient.WithStreams(
					natsclient.TestStreamConfig{Name: "REFUSAL_AGENT", Subjects: []string{"agent.>"}},
					natsclient.TestStreamConfig{Name: "REFUSAL_INPUT_USER", Subjects: []string{"user.message.>"}},
				),
			)
			var (
				laneCfg    natsclient.StreamConsumerConfig
				laneHandle jetstream.ConsumeContext
			)
			c := startProductionTerminalDispatch(
				t, ctx, tc, "REFUSAL_AGENT", "REFUSAL_INPUT_USER", "MISSING", "refusal",
				func(c *Component) {
					// An unknown command is answered through sendResponse; a
					// panic there is recovered by the settlement lane as
					// Quarantine, which latches user.message.
					c.sendResponseFn = func(agentic.UserResponse) {
						panic("injected: user response unavailable")
					}
					c.consumeStream = func(
						consumeCtx context.Context,
						owner natsclient.PortConsumerContext,
						cfg natsclient.StreamConsumerConfig,
						callback func(context.Context, jetstream.Msg),
					) (jetstream.ConsumeContext, error) {
						if owner.Port != row.lane {
							return tc.Client.ConsumeStreamWithConfig(consumeCtx, owner, cfg, callback)
						}
						laneCfg = cfg
						handle, err := tc.Client.ConsumeStreamWithConfig(consumeCtx, owner, cfg,
							holdFirstUntilDelivered(tc.Client, cfg, 2, row.latch, callback))
						laneHandle = handle
						return handle, err
					}
				},
			)
			require.NotNil(t, laneHandle, "Start did not bind %s", row.lane)
			counter := c.metrics.deliveryRefusals.WithLabelValues(row.lane)
			before := testutil.ToFloat64(counter)

			for range 2 {
				require.NoError(t, tc.Client.PublishToStream(ctx, row.subject, row.data(t)))
			}

			select {
			case <-laneHandle.Closed():
			case <-time.After(10 * time.Second):
				t.Fatalf("the latched %s handle was not drained", row.lane)
			}
			require.Equal(t, before+1, testutil.ToFloat64(counter),
				"the buffered delivery the drain flushed must reach this lane's own refusal declarer")

			// Neither delivery was settled: the latching one stopped the owner
			// without a terminal method, and the refused one attempted none.
			stream, err := tc.Client.GetStream(ctx, laneCfg.StreamName)
			require.NoError(t, err)
			consumer, err := stream.Consumer(ctx, laneCfg.ConsumerName)
			require.NoError(t, err)
			info, err := consumer.Info(ctx)
			require.NoError(t, err)
			require.Equal(t, uint64(2), info.Delivered.Consumer)
			require.Zero(t, info.AckFloor.Consumer)
			require.Equal(t, 2, info.NumAckPending)
		})
	}
}

func unknownCommandForRefusal(t *testing.T) []byte {
	t.Helper()
	msg := &agentic.UserMessage{
		MessageID: "refusal-" + time.Now().UTC().Format(time.RFC3339Nano), ChannelType: "http",
		ChannelID: "refusal-session", UserID: "refusal-user", Content: "/no-such-command",
		Timestamp: time.Now().UTC(),
	}
	data, err := json.Marshal(message.NewBaseMessage(msg.Schema(), msg, "refusal-test"))
	require.NoError(t, err)
	return data
}

// metadataUnavailableMsg is a real delivery whose server metadata cannot be
// read, which the typed heartbeat helper answers with Quarantine and owner stop
// before any work runs.
type metadataUnavailableMsg struct{ jetstream.Msg }

func (metadataUnavailableMsg) Metadata() (*jetstream.MsgMetadata, error) {
	return nil, errors.New("injected: delivery metadata unavailable")
}

// holdFirstUntilDelivered holds the lane's first delivery out of the
// production callback until the server reports want deliveries to this
// consumer, so every later delivery is already on its way to the client when
// the first one latches the lane, then hands the callback latch(first). That
// is what makes the flush of a buffered delivery into a drained lane
// deterministic rather than a race with the drain. A failed wait only releases
// the hold; the test's counter assertion is what fails.
func holdFirstUntilDelivered(
	client *natsclient.Client,
	cfg natsclient.StreamConsumerConfig,
	want uint64,
	latch func(jetstream.Msg) jetstream.Msg,
	callback func(context.Context, jetstream.Msg),
) func(context.Context, jetstream.Msg) {
	var once sync.Once
	return func(msgCtx context.Context, msg jetstream.Msg) {
		first := false
		once.Do(func() {
			first = true
			awaitServerDelivered(msgCtx, client, cfg, want)
		})
		if first {
			msg = latch(msg)
		}
		callback(msgCtx, msg)
	}
}

func awaitServerDelivered(ctx context.Context, client *natsclient.Client, cfg natsclient.StreamConsumerConfig, want uint64) {
	stream, err := client.GetStream(ctx, cfg.StreamName)
	if err != nil {
		return
	}
	consumer, err := stream.Consumer(ctx, cfg.ConsumerName)
	if err != nil {
		return
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		info, infoErr := consumer.Info(ctx)
		if infoErr == nil && info.Delivered.Consumer >= want {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
