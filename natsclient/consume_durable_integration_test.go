//go:build integration

package natsclient

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_ConsumeDurable_ReceivesAndAcks drives the ASSEMBLED durable
// consume path against a real server: publish via PublishToStreamWithMsgID, and
// assert ConsumeDurable's func(ctx,[]byte)error handler receives the payload and
// (returning nil) acks it — closing the "tests the pieces, not the system" gap.
func TestIntegration_ConsumeDurable_ReceivesAndAcks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	natsContainer, natsURL := startTestNATSContainerWithJS(ctx, t)
	defer natsContainer.Terminate(context.Background())

	client, err := NewClient(natsURL, WithMaxReconnects(0))
	require.NoError(t, err)
	require.NoError(t, client.Connect(ctx))
	defer client.Close(context.Background())

	const subject = "cd.test.recv"
	_, err = client.EnsureStream(ctx, jetstream.StreamConfig{
		Name: "CD_RECV", Subjects: []string{subject}, Duplicates: 2 * time.Minute,
		MaxAge:   testStreamMaxAge,
		MaxBytes: testStreamMaxBytes,
	})
	require.NoError(t, err)

	got := make(chan []byte, 1)
	err = client.ConsumeDurable(ctx, StreamConsumerConfig{
		StreamName: "CD_RECV", ConsumerName: "cd-recv", FilterSubject: subject, AckWait: 2 * time.Second,
	}, 500*time.Millisecond, func(_ context.Context, data []byte) error {
		got <- data
		return nil
	})
	require.NoError(t, err)

	require.NoError(t, client.PublishToStreamWithMsgID(ctx, subject, []byte("hello-durable"), "id1"))

	select {
	case data := <-got:
		assert.Equal(t, []byte("hello-durable"), data)
	case <-time.After(10 * time.Second):
		t.Fatal("handler did not receive the published message within 10s")
	}
}

// TestIntegration_ConsumeDurable_MsgIDDedup validates the ADR-070 B1 fix: two
// publishes of the same Nats-Msg-Id within the stream's Duplicates window collapse
// to ONE stored message, so the consumer sees the unit exactly once — this is what
// makes the claim-rollback safe against an ack-timeout re-dispatch.
func TestIntegration_ConsumeDurable_MsgIDDedup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	natsContainer, natsURL := startTestNATSContainerWithJS(ctx, t)
	defer natsContainer.Terminate(context.Background())

	client, err := NewClient(natsURL, WithMaxReconnects(0))
	require.NoError(t, err)
	require.NoError(t, client.Connect(ctx))
	defer client.Close(context.Background())

	const subject = "cd.test.dedup"
	_, err = client.EnsureStream(ctx, jetstream.StreamConfig{
		Name: "CD_DEDUP", Subjects: []string{subject}, Duplicates: 2 * time.Minute,
		MaxAge:   testStreamMaxAge,
		MaxBytes: testStreamMaxBytes,
	})
	require.NoError(t, err)

	var deliveries int32
	err = client.ConsumeDurable(ctx, StreamConsumerConfig{
		StreamName: "CD_DEDUP", ConsumerName: "cd-dedup", FilterSubject: subject, AckWait: 2 * time.Second,
	}, 500*time.Millisecond, func(_ context.Context, _ []byte) error {
		atomic.AddInt32(&deliveries, 1)
		return nil
	})
	require.NoError(t, err)

	// Same msg-id twice (the ack-timeout re-dispatch shape).
	require.NoError(t, client.PublishToStreamWithMsgID(ctx, subject, []byte("unit-a"), "unit-a"))
	require.NoError(t, client.PublishToStreamWithMsgID(ctx, subject, []byte("unit-a"), "unit-a"))

	// Wait past any delivery, then assert exactly one.
	time.Sleep(2 * time.Second)
	assert.Equal(t, int32(1), atomic.LoadInt32(&deliveries), "same msg-id within the dedup window must deliver once")
}
