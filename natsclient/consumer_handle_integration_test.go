//go:build integration

package natsclient

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// TestOutstandingWork_CountsDeliveredButUnacked is the caught-up-readiness-producers task
// 1.3 guard: a readiness producer must be able to read authoritative server-side
// backlog from the consumer it bound.
//
// It drives the PRODUCTION wire — a real consumer bound through
// ConsumeStreamWithConfig against a real server — because the claim is that the
// bookkeeping survives the framework's own lifecycle handling, which a fake cannot
// prove.
func TestOutstandingWork_CountsDeliveredButUnacked(t *testing.T) {
	tc := NewTestClient(t, WithJetStream(), WithStreams(TestStreamConfig{
		Name:     "HANDLE_PROBE",
		Subjects: []string{"handle.probe.>"},
	}))
	defer func() { _ = tc.Terminate() }()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const consumerName = "handle-probe-consumer"

	// Block every delivery so messages stay delivered-but-unacked. That is the state
	// NumPending alone cannot see, and the reason OutstandingWork sums the two
	// server-side counters internally rather than exposing either.
	release := make(chan struct{})
	delivered := make(chan struct{}, 16)
	err := tc.Client.ConsumeStreamWithConfig(ctx, StreamConsumerConfig{
		StreamName:    "HANDLE_PROBE",
		ConsumerName:  consumerName,
		FilterSubject: "handle.probe.>",
		DeliverPolicy: "all",
		AckPolicy:     "explicit",
		AutoCreate:    true,
	}, func(_ context.Context, msg jetstream.Msg) {
		select {
		case delivered <- struct{}{}:
		default:
		}
		<-release
		_ = msg.Ack()
	})
	require.NoError(t, err, "bind consumer")

	outstanding, err := tc.Client.OutstandingWork(ctx, "HANDLE_PROBE", consumerName)
	require.NoError(t, err, "outstanding work must be readable right after binding")
	require.Zero(t, outstanding, "nothing published yet")

	for i := 0; i < 5; i++ {
		_, err := tc.Client.PublishToStreamWithAck(ctx,
			fmt.Sprintf("handle.probe.%d", i), []byte("x"))
		require.NoError(t, err)
	}

	// Wait for at least one delivery so the outstanding sum is non-zero for a reason
	// the test caused, rather than sleeping and hoping.
	select {
	case <-delivered:
	case <-ctx.Done():
		t.Fatal("no message delivered")
	}

	// This is the assertion that fences a "simplify to NumPending" regression, and it
	// works through the collapsed signature precisely BECAUSE a delivery is blocked:
	// at least one of the 5 is delivered-but-unacked, so an implementation returning
	// only NumPending reports fewer than 5 and fails here. The blocked handler above
	// is load-bearing test setup, not incidental.
	require.Eventually(t, func() bool {
		outstanding, err := tc.Client.OutstandingWork(ctx, "HANDLE_PROBE", consumerName)
		return err == nil && outstanding == 5
	}, 20*time.Second, 100*time.Millisecond,
		"total must account for all 5 published messages while one is held unacked")

	close(release)

	require.Eventually(t, func() bool {
		outstanding, err := tc.Client.OutstandingWork(ctx, "HANDLE_PROBE", consumerName)
		return err == nil && outstanding == 0
	}, 20*time.Second, 100*time.Millisecond,
		"outstanding must drain to zero once deliveries are acked")
}

// TestOutstandingWork_UnboundConsumerIsAnError pins the fail-closed half: once a
// consumer is stopped, or was never bound, the call must ERROR rather than report 0.
// Unknown backlog must not be representable as empty backlog — that is the
// distinction the whole readiness signal rests on.
func TestOutstandingWork_UnboundConsumerIsAnError(t *testing.T) {
	tc := NewTestClient(t, WithJetStream(), WithStreams(TestStreamConfig{
		Name:     "HANDLE_STOP",
		Subjects: []string{"handle.stop.>"},
	}))
	defer func() { _ = tc.Terminate() }()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const consumerName = "handle-stop-consumer"
	err := tc.Client.ConsumeStreamWithConfig(ctx, StreamConsumerConfig{
		StreamName:    "HANDLE_STOP",
		ConsumerName:  consumerName,
		FilterSubject: "handle.stop.>",
		DeliverPolicy: "all",
		AckPolicy:     "explicit",
		AutoCreate:    true,
	}, func(_ context.Context, msg jetstream.Msg) { _ = msg.Ack() })
	require.NoError(t, err)

	_, err = tc.Client.OutstandingWork(ctx, "HANDLE_STOP", consumerName)
	require.NoError(t, err, "readable while bound")

	tc.Client.StopConsumer("HANDLE_STOP", consumerName)

	_, err = tc.Client.OutstandingWork(ctx, "HANDLE_STOP", consumerName)
	require.Error(t, err, "a stopped consumer must error, not report an empty backlog")

	_, err = tc.Client.OutstandingWork(ctx, "HANDLE_STOP", "never-bound")
	require.Error(t, err, "an unbound name must error, not report an empty backlog")
}
