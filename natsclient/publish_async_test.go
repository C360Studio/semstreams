package natsclient

import (
	"context"
	"errors"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPublishToStreamAsync_NotConnected verifies the async enqueue honors the
// same connection guard as the synchronous path: a disconnected client returns
// ErrNotConnected and a nil future, without touching JetStream.
func TestPublishToStreamAsync_NotConnected(t *testing.T) {
	client, err := NewClient("nats://localhost:4222")
	require.NoError(t, err)

	future, err := client.PublishToStreamAsync(context.Background(), "test.subject", []byte("data"))
	assert.Equal(t, ErrNotConnected, err)
	assert.Nil(t, future)
}

// TestPublishToStreamAsync_CancelledContext verifies a cancelled context is
// honored before enqueue (PublishMsgAsync takes no ctx), returning the context
// error and a nil future. The breaker gate still takes precedence over ctx.
func TestPublishToStreamAsync_CancelledContext(t *testing.T) {
	client, err := NewClient("nats://localhost:4222")
	require.NoError(t, err)
	// Force Connected so the ctx check (which sits after the connection gate) is
	// the one that fires, not ErrNotConnected.
	client.setStatus(StatusConnected)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	future, err := client.PublishToStreamAsync(ctx, "test.subject", []byte("data"))
	assert.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, future)
	// A cancelled ctx is caller intent, not a connection fault: no failure recorded.
	assert.Equal(t, int32(0), client.Failures())
}

// TestPublishToStreamAsync_CircuitOpen verifies an open circuit rejects the async
// enqueue with ErrCircuitOpen and a nil future (no publish).
func TestPublishToStreamAsync_CircuitOpen(t *testing.T) {
	client, err := NewClient("nats://localhost:4222")
	require.NoError(t, err)

	// Drive the breaker open (threshold is 15).
	for i := 0; i < 15; i++ {
		client.recordFailure()
	}
	require.Equal(t, StatusCircuitOpen, client.Status())

	future, err := client.PublishToStreamAsync(context.Background(), "test.subject", []byte("data"))
	assert.Equal(t, ErrCircuitOpen, err)
	assert.Nil(t, future)
}

// TestAsyncPublishErrHandler_RecordsFailure verifies the connection-level async
// error handler bridges a failed async ack into the circuit breaker: enough
// failures through the handler open the breaker exactly as failed sync publishes
// would. This is the ack-failure → breaker path (gh#470 design §4).
func TestAsyncPublishErrHandler_RecordsFailure(t *testing.T) {
	client, err := NewClient("nats://localhost:4222")
	require.NoError(t, err)

	msg := &nats.Msg{Subject: "test.subject"}
	// One handler call short of the threshold: not yet open.
	for i := 0; i < 14; i++ {
		client.asyncPublishErrHandler(nil, msg, errors.New("ack failed"))
	}
	assert.NotEqual(t, StatusCircuitOpen, client.Status())

	// The 15th failed ack opens the breaker.
	client.asyncPublishErrHandler(nil, msg, errors.New("ack failed"))
	assert.Equal(t, StatusCircuitOpen, client.Status())
}

// TestAsyncPublishErrHandler_NilMsg verifies the handler's defensive nil-guard on
// the message argument. jetstream-go currently always passes a non-nil paf.msg,
// but the handler must not panic if that ever changes.
func TestAsyncPublishErrHandler_NilMsg(t *testing.T) {
	client, err := NewClient("nats://localhost:4222")
	require.NoError(t, err)

	assert.NotPanics(t, func() {
		client.asyncPublishErrHandler(nil, nil, errors.New("ack failed"))
	})
	assert.Equal(t, int32(1), client.Failures())
}

// TestPublishAsyncComplete_JetStreamUnavailable verifies the drain accessor
// returns an already-closed channel (not a blocking one) when JetStream is
// unavailable, so a drain loop does not hang.
func TestPublishAsyncComplete_JetStreamUnavailable(t *testing.T) {
	client, err := NewClient("nats://localhost:4222")
	require.NoError(t, err)

	select {
	case <-client.PublishAsyncComplete():
		// closed immediately — correct
	default:
		t.Fatal("PublishAsyncComplete must return a closed channel when JetStream is unavailable")
	}

	assert.Equal(t, 0, client.PublishAsyncPending())
}

// TestPublishBatchToStream_Empty verifies an empty batch is a no-op returning nil
// without touching the connection.
func TestPublishBatchToStream_Empty(t *testing.T) {
	client, err := NewClient("nats://localhost:4222")
	require.NoError(t, err)

	assert.NoError(t, client.PublishBatchToStream(context.Background(), "test.subject", nil))
}

// TestPublishBatchToStream_NotConnected verifies a non-empty batch on a
// disconnected client surfaces the enqueue guard error.
func TestPublishBatchToStream_NotConnected(t *testing.T) {
	client, err := NewClient("nats://localhost:4222")
	require.NoError(t, err)

	err = client.PublishBatchToStream(context.Background(), "test.subject", [][]byte{[]byte("a")})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNotConnected)
}
