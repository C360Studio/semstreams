//go:build integration

package natsclient

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_PublishToStreamWithMsgID_Dedup verifies the ADR-055 §5 "T1"
// contract: a deterministic Nats-Msg-Id collapses re-publishes of the same
// logical event to a single stored message within the stream's duplicate
// window, distinct IDs each store, and an empty ID opts out of dedup entirely
// (drop-in PublishToStream behavior).
func TestIntegration_PublishToStreamWithMsgID_Dedup(t *testing.T) {
	ctx := context.Background()
	const duplicateWindow = 250 * time.Millisecond

	natsContainer, natsURL := startNATSContainerWithJS(ctx, t)
	defer natsContainer.Terminate(ctx)

	client, err := NewClient(natsURL)
	require.NoError(t, err)
	require.NoError(t, client.Connect(ctx))
	defer client.Close(ctx)

	// Stream with an explicit duplicate-detection window.
	stream, err := client.EnsureStream(ctx, jetstream.StreamConfig{
		Name:       "MSGID_STREAM",
		Subjects:   []string{"msgid.>"},
		Storage:    jetstream.MemoryStorage,
		Duplicates: duplicateWindow,
		MaxAge:     testStreamMaxAge,
		MaxBytes:   testStreamMaxBytes,
	})
	require.NoError(t, err)

	msgCount := func() uint64 {
		info, ierr := stream.Info(ctx)
		require.NoError(t, ierr)
		return info.State.Msgs
	}

	// Same msgID twice → deduped to one stored message. PublishMsg returns a
	// PubAck with Duplicate=true (no error) on the second call.
	require.NoError(t, client.PublishToStreamWithMsgID(ctx, "msgid.test", []byte("first"), "evt-1"))
	require.NoError(t, client.PublishToStreamWithMsgID(ctx, "msgid.test", []byte("first-again"), "evt-1"))
	assert.Equal(t, uint64(1), msgCount(),
		"same Nats-Msg-Id within the window must dedup to one stored message")

	// Expiry itself is the wall-clock behavior under test. Poll the server's
	// authoritative stream state instead of sleeping for a guessed scheduler
	// margin: duplicate attempts remain collapsed until the configured window
	// actually expires, then the first accepted attempt advances the count.
	require.Eventually(t, func() bool {
		if err := client.PublishToStreamWithMsgID(ctx, "msgid.test", []byte("after-window"), "evt-1"); err != nil {
			return false
		}
		return msgCount() == 2
	}, 3*time.Second, 25*time.Millisecond,
		"the same Nats-Msg-Id must store again after the configured window")

	// Distinct msgID → a new message.
	require.NoError(t, client.PublishToStreamWithMsgID(ctx, "msgid.test", []byte("second"), "evt-2"))
	assert.Equal(t, uint64(3), msgCount(), "distinct Nats-Msg-Id must store a new message")

	// Empty msgID → no dedup; two identical publishes both store.
	require.NoError(t, client.PublishToStreamWithMsgID(ctx, "msgid.test", []byte("x"), ""))
	require.NoError(t, client.PublishToStreamWithMsgID(ctx, "msgid.test", []byte("x"), ""))
	assert.Equal(t, uint64(5), msgCount(), "empty msgID must not dedup")
}

// TestIntegration_PublishToStreamWithMsgID_NotConnected verifies the circuit
// guard mirrors PublishToStream / PublishToStreamWithAck.
func TestIntegration_PublishToStreamWithMsgID_NotConnected(t *testing.T) {
	client, err := NewClient("nats://localhost:4222")
	require.NoError(t, err)

	ctx := context.Background()
	err = client.PublishToStreamWithMsgID(ctx, "test.subject", []byte("data"), "id-1")
	assert.Equal(t, ErrNotConnected, err)
}
