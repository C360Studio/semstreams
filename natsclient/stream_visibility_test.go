package natsclient

import (
	"context"
	"errors"
	"testing"
	"time"

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

// TestConsumerAutoCreateBindsAStreamThatAlreadyExists covers the seam one step
// over from the visibility wait. On a lagging node the auto-create pre-check is
// answered "not found" for a stream that exists, so setup falls through to
// CreateStream and the server answers 10058 — and if this caller's auto-create
// config differs at all from the live declaration, that is exactly when it does.
// Returning that as transient would fail boot for a stream that is present, one
// seam away from the window natsclient just absorbed.
//
// The answer is to bind by name, matching the pre-check's own success path: a
// non-owner does not restamp a stream someone else declared. Proof that setup
// got PAST auto-create is that the failure it ultimately reports is the guarded
// lookup's absent answer, not the create's already-in-use one.
func TestConsumerAutoCreateBindsAStreamThatAlreadyExists(t *testing.T) {
	fake := &fakeJetStream{
		streamErr:       jetstream.ErrStreamNotFound,
		createStreamErr: jetstream.ErrStreamNameAlreadyInUse,
	}
	client := newConnectedClientWithFakeJS(t, fake)
	// A caller deadline, not a measurement: the fake never makes the stream
	// visible, so this bounds the wait that follows the bind. Which branch
	// auto-create took is what is asserted.
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()

	handle, err := client.ConsumeInternalStreamWithConfig(ctx, StreamConsumerConfig{
		StreamName:    "AUTO_CREATE_RACE",
		ConsumerName:  "auto-create-race",
		FilterSubject: "auto.create.race.>",
		AckPolicy:     "explicit",
		DeliverPolicy: "all",
		AutoCreate:    true,
		AutoCreateConfig: &StreamAutoCreateConfig{
			Subjects: []string{"auto.create.race.>"},
			MaxAge:   time.Hour,
			MaxBytes: 64 << 20,
			Discard:  jetstream.DiscardOld,
		},
	}, func(context.Context, jetstream.Msg) {})

	require.Nil(t, handle)
	require.Error(t, err)
	require.NotErrorIs(t, err, jetstream.ErrStreamNameAlreadyInUse,
		"a stream that already exists is not an auto-create failure; the caller binds by name")
	require.ErrorIs(t, err, jetstream.ErrStreamNotFound,
		"setup must continue past auto-create into the guarded lookup")
	require.Equal(t, int64(1), fake.createStreamCalls.Load(),
		"one create attempt, then bind — never a create loop")
	require.Greater(t, fake.streamCalls.Load(), int64(1),
		"the pre-check plus at least one visibility probe")
}

// TestStreamNotVisibleSeparatesASpentBudgetFromACancelledWait pins the sentinel's
// contract at the function that owns it: only a spent budget carries
// ErrStreamNotVisible, both endings keep the absent classification reachable, and
// a wait the caller cut short carries the caller's own cause instead — because it
// measured nothing about the stream.
func TestStreamNotVisibleSeparatesASpentBudgetFromACancelledWait(t *testing.T) {
	spent := streamNotVisible(context.Background(), jetstream.ErrStreamNotFound)
	require.ErrorIs(t, spent, ErrStreamNotVisible)
	require.ErrorIs(t, spent, jetstream.ErrStreamNotFound)

	ended, cancel := context.WithCancel(context.Background())
	cancel()
	cut := streamNotVisible(ended, jetstream.ErrStreamNotFound)
	require.ErrorIs(t, cut, context.Canceled)
	require.ErrorIs(t, cut, jetstream.ErrStreamNotFound)
	require.NotErrorIs(t, cut, ErrStreamNotVisible,
		"a wait the caller cut short is not evidence that the stream is absent")
}

// TestConsumerSetupCancelledWaitCarriesNoAbsenceEvidence is the same fact through
// the production entry points: a caller whose context ends the wait gets both
// causes and NOT the sentinel, so a lifetime decision branching on the sentinel
// fails closed on a cancelled boot without needing to order its conditions.
func TestConsumerSetupCancelledWaitCarriesNoAbsenceEvidence(t *testing.T) {
	for _, entry := range consumeEntryPoints() {
		t.Run(entry.name, func(t *testing.T) {
			fake := &fakeJetStream{streamErr: jetstream.ErrStreamNotFound}
			client := newConnectedClientWithFakeJS(t, fake)
			// The caller's deadline ends the wait long before the budget does.
			ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
			defer cancel()

			handle, err := entry.consume(ctx, client, StreamConsumerConfig{
				StreamName:    "CANCELLED_WAIT",
				ConsumerName:  "cancelled-wait",
				FilterSubject: "cancelled.wait.>",
				AckPolicy:     "explicit",
				DeliverPolicy: "all",
			}, func(context.Context, jetstream.Msg) {})

			require.Nil(t, handle)
			require.ErrorIs(t, err, context.DeadlineExceeded)
			require.ErrorIs(t, err, jetstream.ErrStreamNotFound)
			require.NotErrorIs(t, err, ErrStreamNotVisible,
				"the budget was never spent, so nothing durable was measured")
		})
	}
}

// notFoundConsumerInfo is a consumer whose initial observation answers with the
// absent classification — the third seam that can put jetstream.ErrStreamNotFound
// into a consumer-setup error chain.
type notFoundConsumerInfo struct{}

func (notFoundConsumerInfo) Info(context.Context) (*jetstream.ConsumerInfo, error) {
	return nil, jetstream.ErrStreamNotFound
}

// TestAbsentClassificationFromConsumerSeamsCarriesNoSentinel is why the sentinel
// exists. Consumer CREATION and the initial consumer OBSERVATION both preserve
// their cause through a transient wrap, so both can hand a caller
// jetstream.ErrStreamNotFound for a stream that is present — a consumer-level
// answer, not a statement about the stream. Neither may carry the sentinel, or a
// caller branching on absence disables itself for the process lifetime on a
// consumer fault.
func TestAbsentClassificationFromConsumerSeamsCarriesNoSentinel(t *testing.T) {
	created := ClassifyConsumerPolicyError(jetstream.ErrStreamNotFound, "ConsumeInternalStreamWithConfig")
	require.ErrorIs(t, created, jetstream.ErrStreamNotFound,
		"the cause is preserved for diagnosis")
	require.NotErrorIs(t, created, ErrStreamNotVisible,
		"consumer creation never measured the stream's visibility")

	client := newConnectedClientWithFakeJS(t, &fakeJetStream{})
	_, observeErr := client.observeInternalConsumer(t.Context(), notFoundConsumerInfo{})
	require.ErrorIs(t, observeErr, jetstream.ErrStreamNotFound)
	require.NotErrorIs(t, observeErr, ErrStreamNotVisible,
		"initial consumer observation never measured the stream's visibility")
}
