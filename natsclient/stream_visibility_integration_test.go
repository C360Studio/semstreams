//go:build integration

package natsclient

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/pkg/errs"
)

// TestConsumerSetupWaitsForStreamThatBecomesVisible proves the wait ENDS on
// visibility rather than on a timer: setup begins while the stream provably does
// not exist, the stream appears while setup is still waiting, and setup succeeds.
//
// A single node cannot reproduce clustered metadata propagation, so a late
// creation stands in for a non-leader node applying the meta assignment. The
// synchronization is the production wait's own wire traffic, not a delay: every
// probe publishes $JS.API.STREAM.INFO.<stream>, and a SECOND probe exists only
// because the first was answered "stream not found". Creating the stream once
// two probes have been observed therefore proves setup was still waiting when
// the stream appeared, without any test-side timing standing in for that proof.
func TestConsumerSetupWaitsForStreamThatBecomesVisible(t *testing.T) {
	testClient := NewTestClient(t, WithJetStream())
	defer func() { _ = testClient.Terminate() }()

	for _, entry := range consumeEntryPoints() {
		t.Run(entry.name, func(t *testing.T) {
			// A deadline, not a delay: nats.Conn.FlushWithContext refuses a
			// deadline-free context, and every wait below is bounded by its own
			// synchronization long before this expires.
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			streamName := "LATE_VISIBLE_" + strings.ToUpper(entry.name)
			subject := "late.visible." + entry.name + ".>"

			_, err := testClient.GetStream(ctx, streamName)
			require.ErrorIs(t, err, jetstream.ErrStreamNotFound,
				"precondition: the stream must be absent when consumer setup begins")

			conn := testClient.GetNativeConnection()
			probes, err := conn.SubscribeSync("$JS.API.STREAM.INFO." + streamName)
			require.NoError(t, err)
			t.Cleanup(func() { _ = probes.Unsubscribe() })
			require.NoError(t, conn.FlushWithContext(ctx))

			created := make(chan error, 1)
			go func() {
				// Bounded far above the probe interval and far below the
				// visibility budget, so a setup that stopped re-probing reports
				// itself here instead of stalling the test until its deadline.
				probeCtx, probeCancel := context.WithTimeout(ctx, 2*time.Second)
				defer probeCancel()
				for range 2 {
					if _, waitErr := probes.NextMsgWithContext(probeCtx); waitErr != nil {
						created <- fmt.Errorf(
							"consumer setup stopped probing for the absent stream: %w", waitErr)
						return
					}
				}
				_, createErr := testClient.CreateStream(ctx, streamName, []string{subject})
				created <- createErr
			}()

			handle, err := entry.consume(ctx, testClient.Client, StreamConsumerConfig{
				StreamName:    streamName,
				ConsumerName:  "late-visible",
				FilterSubject: subject,
				AckPolicy:     "explicit",
				DeliverPolicy: "all",
			}, func(_ context.Context, msg jetstream.Msg) { _ = msg.Ack() })

			require.NoError(t, <-created)
			require.NoError(t, err,
				"consumer setup must absorb the window where the stream is not yet visible")
			require.NotNil(t, handle)
			handle.Drain()
			<-handle.Closed()
		})
	}
}

// TestConsumerSetupFailsLoudlyWhenStreamNeverBecomesVisible is the guard that
// keeps the wait from becoming retry-until-green. A stream that never appears
// must still fail the caller, with the absent classification reachable through
// the transient wrap so callers and tests can branch on it.
func TestConsumerSetupFailsLoudlyWhenStreamNeverBecomesVisible(t *testing.T) {
	testClient := NewTestClient(t, WithJetStream())
	defer func() { _ = testClient.Terminate() }()

	for _, entry := range consumeEntryPoints() {
		t.Run(entry.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			streamName := "NEVER_VISIBLE_" + strings.ToUpper(entry.name)

			handle, err := entry.consume(ctx, testClient.Client, StreamConsumerConfig{
				StreamName:    streamName,
				ConsumerName:  "never-visible",
				FilterSubject: "never.visible." + entry.name + ".>",
				AckPolicy:     "explicit",
				DeliverPolicy: "all",
			}, func(context.Context, jetstream.Msg) {})

			require.Nil(t, handle)
			require.ErrorIs(t, err, jetstream.ErrStreamNotFound,
				"an exhausted visibility budget must still report the stream as absent")
			require.ErrorIs(t, err, ErrStreamNotVisible,
				"a spent budget is the durable evidence of absence callers branch on")
			require.True(t, errs.IsTransient(err),
				"the failure keeps its existing transient classification")

			_, lookupErr := testClient.GetStream(ctx, streamName)
			require.ErrorIs(t, lookupErr, jetstream.ErrStreamNotFound,
				"the wait must not have created the stream it was waiting for")
		})
	}
}
