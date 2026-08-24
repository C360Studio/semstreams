package natsclient

import (
	"context"
	"errors"
	"testing"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// consumeEntryPoint is one production consumer-setup path that resolves a stream
// handle before it can create a consumer.
type consumeEntryPoint struct {
	name    string
	consume func(
		ctx context.Context,
		c *Client,
		cfg StreamConsumerConfig,
		handler func(context.Context, jetstream.Msg),
	) (jetstream.ConsumeContext, error)
}

// consumeEntryPoints enumerates every production path through a pre-consumer
// stream lookup, so a guard written against one lookup cannot silently leave the
// other unguarded:
//
//   - ConsumeInternalStreamWithConfig owns its own lookup (stream.go).
//   - consumePortStreamWithConfigContexts owns the other, and is the shared body
//     of BOTH exported port operations — ConsumeStreamWithConfig and
//     ConsumeStreamWithConfigContexts differ only in which context authorizes
//     delivered messages, which is settled after the lookup. Driving
//     ConsumeStreamWithConfig therefore covers both.
//
// ensureStreamForConsumer's lookup is deliberately absent: its not-found answer
// is not a failure there, it is the branch that creates the stream.
func consumeEntryPoints() []consumeEntryPoint {
	return []consumeEntryPoint{
		{
			name: "internal",
			consume: func(
				ctx context.Context,
				c *Client,
				cfg StreamConsumerConfig,
				handler func(context.Context, jetstream.Msg),
			) (jetstream.ConsumeContext, error) {
				return c.ConsumeInternalStreamWithConfig(ctx, cfg, handler)
			},
		},
		{
			name: "port",
			consume: func(
				ctx context.Context,
				c *Client,
				cfg StreamConsumerConfig,
				handler func(context.Context, jetstream.Msg),
			) (jetstream.ConsumeContext, error) {
				return c.ConsumeStreamWithConfig(
					ctx,
					PortConsumerContext{Component: "stream-visibility", Port: "in"},
					cfg,
					handler,
				)
			},
		},
	}
}

// TestConsumerSetupReturnsNonAbsentStreamFailureWithoutProbingAgain pins the
// scope of the visibility wait by COUNTING lookups rather than timing them: a
// failure that is not jetstream.ErrStreamNotFound is the server's real answer —
// a permission denial, a transport fault, a cancelled caller — and waiting on it
// would turn a bounded propagation tolerance into retry-until-green.
//
// The real-wire counterpart, that an absent stream is retried and then reported
// as absent, is TestConsumerSetupWaitsForStreamThatBecomesVisible and
// TestConsumerSetupFailsLoudlyWhenStreamNeverBecomesVisible.
func TestConsumerSetupReturnsNonAbsentStreamFailureWithoutProbingAgain(t *testing.T) {
	for _, entry := range consumeEntryPoints() {
		t.Run(entry.name, func(t *testing.T) {
			transportErr := errors.New("write tcp 127.0.0.1:4222: connection reset by peer")
			fake := &fakeJetStream{streamErr: transportErr}
			client := newConnectedClientWithFakeJS(t, fake)

			handle, err := entry.consume(t.Context(), client, StreamConsumerConfig{
				StreamName:    "STREAM_LOOKUP_FAILS",
				ConsumerName:  "visibility-scope",
				FilterSubject: "stream.lookup.fails.>",
				AckPolicy:     "explicit",
				DeliverPolicy: "all",
			}, func(context.Context, jetstream.Msg) {})

			require.Nil(t, handle)
			require.ErrorIs(t, err, transportErr)
			require.Equal(t, int64(1), fake.streamCalls.Load(),
				"only jetstream.ErrStreamNotFound is a metadata-propagation window; "+
					"every other lookup failure is the answer and is returned on first observation")
		})
	}
}
