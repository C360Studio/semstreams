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

// errProbeTransport is a failure that has nothing to do with the stream: the
// class the wait must never reclassify as absence.
var errProbeTransport = errors.New("probe transport failure")

// absentThenBlockedTransport answers the FIRST Stream() with the absent
// classification, then blocks until the wait context ends and returns a
// transport failure of its own — so the failure and the wait's ending arrive
// together, which is the only sequence in which a stale absence could overwrite
// a real answer.
type absentThenBlockedTransport struct {
	*fakeJetStream
	transport error
}

func (f *absentThenBlockedTransport) Stream(ctx context.Context, _ string) (jetstream.Stream, error) {
	if f.streamCalls.Add(1) == 1 {
		return nil, jetstream.ErrStreamNotFound
	}
	<-ctx.Done()
	return nil, f.transport
}

// TestConsumerSetupDoesNotReclassifyALaterProbeFailureAsAbsence pins the scope of
// the wait's one tolerance from the other side: an earlier probe's "not found"
// must not decide what a LATER probe's failure means. A transport or permission
// fault that lands as the wait ends is an answer about that probe, and returning
// ErrStreamNotVisible for it would hand a caller durable evidence of absence for
// a fault the stream had nothing to do with — which agentrun's skip would read as
// "no agentic components in this deployment".
func TestConsumerSetupDoesNotReclassifyALaterProbeFailureAsAbsence(t *testing.T) {
	for _, entry := range consumeEntryPoints() {
		t.Run(entry.name, func(t *testing.T) {
			fake := &absentThenBlockedTransport{
				fakeJetStream: &fakeJetStream{},
				transport:     errProbeTransport,
			}
			client := newConnectedClientWithFakeJS(t, fake)
			// The caller's deadline ends the wait. The budget is a constant, and
			// which bound ends it is not what this test is about.
			ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
			defer cancel()

			handle, err := entry.consume(ctx, client, StreamConsumerConfig{
				StreamName:    "LATE_PROBE_FAULT",
				ConsumerName:  "late-probe-fault",
				FilterSubject: "late.probe.fault.>",
				AckPolicy:     "explicit",
				DeliverPolicy: "all",
			}, func(context.Context, jetstream.Msg) {})

			require.Nil(t, handle)
			require.ErrorIs(t, err, errProbeTransport,
				"the failure this probe actually returned is the answer")
			require.NotErrorIs(t, err, ErrStreamNotVisible,
				"a probe that failed for its own reason measured no absence")
		})
	}
}

// absentThenLostReply answers the FIRST Stream() with the absent classification,
// then never replies: the next probe blocks until the context it was given ends
// and returns that context's own error, which is what nats.go's request path
// produces when no reply arrives before the deadline.
type absentThenLostReply struct {
	*fakeJetStream
}

func (f *absentThenLostReply) Stream(ctx context.Context, _ string) (jetstream.Stream, error) {
	if f.streamCalls.Add(1) == 1 {
		return nil, jetstream.ErrStreamNotFound
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

// TestConsumerSetupDoesNotMintTheSentinelForALostReply is the sentinel's second
// scope guard, and the sharper one: the failing probe's error IS a context error,
// so a wait that decided "the budget ended, therefore absent" could not tell this
// apart from a spent budget. It must, because a probe that got no reply measured
// nothing — and agentrun would read the sentinel as "no agentic components in
// this deployment" and disable itself for the process lifetime over a lost reply.
//
// Absence is what COMPLETED probes said; the sentinel is minted only when the
// budget runs out between them.
func TestConsumerSetupDoesNotMintTheSentinelForALostReply(t *testing.T) {
	for _, entry := range consumeEntryPoints() {
		t.Run(entry.name, func(t *testing.T) {
			fake := &absentThenLostReply{fakeJetStream: &fakeJetStream{}}
			client := newConnectedClientWithFakeJS(t, fake)

			// The caller's context is deliberately NOT bounded here, so the probe
			// ends on the BUDGET while the caller is still alive. That is the only
			// arrangement in which the two endings are distinguishable: bound the
			// caller instead and its cancellation ends both at once, which any
			// implementation reports identically. The cost is one budget per entry
			// point, and it buys the only assertion that can fail if the sentinel
			// is minted from an unfinished probe.
			handle, err := entry.consume(t.Context(), client, StreamConsumerConfig{
				StreamName:    "LOST_REPLY",
				ConsumerName:  "lost-reply",
				FilterSubject: "lost.reply.>",
				AckPolicy:     "explicit",
				DeliverPolicy: "all",
			}, func(context.Context, jetstream.Msg) {})

			require.Nil(t, handle)
			require.ErrorIs(t, err, context.DeadlineExceeded,
				"the probe ended on its own context, and that is the answer")
			require.ErrorIs(t, err, jetstream.ErrStreamNotFound,
				"the last completed observation stays reachable for classification")
			require.NotErrorIs(t, err, ErrStreamNotVisible,
				"a probe that observed nothing cannot be evidence of absence")
		})
	}
}
