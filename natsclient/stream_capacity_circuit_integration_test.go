//go:build integration

package natsclient

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

func TestIntegration_StreamCapacityRejectionIsCircuitNeutral(t *testing.T) {
	t.Run("sync maximum messages", testSyncMaxMessagesCircuitNeutral)
	t.Run("with ack maximum bytes", testWithAckMaxBytesCircuitNeutral)
	t.Run("async maximum messages per subject", testAsyncMaxMsgsPerSubjectCircuitNeutral)
	t.Run("batch aggregate preserves capacity error", testBatchCapacityCircuitNeutral)
}

func testSyncMaxMessagesCircuitNeutral(t *testing.T) {
	ctx, client, js := newCapacityTestClient(t)
	_, err := js.CreateStream(ctx, jetstream.StreamConfig{
		Name: "CAP_SYNC", Subjects: []string{"cap.sync"}, Storage: jetstream.MemoryStorage,
		Discard: jetstream.DiscardNew, MaxMsgs: 1,
	})
	require.NoError(t, err)
	_, err = js.CreateStream(ctx, jetstream.StreamConfig{
		Name: "CAP_HEALTHY", Subjects: []string{"cap.healthy"}, Storage: jetstream.MemoryStorage,
		Discard: jetstream.DiscardNew, MaxMsgs: 10,
	})
	require.NoError(t, err)
	require.NoError(t, client.PublishToStream(ctx, "cap.sync", []byte("first")))
	seedCircuitFailures(t, client, 14)

	err = client.PublishToStream(ctx, "cap.sync", []byte("rejected"))
	requireCapacityAPIError(t, err, "maximum messages exceeded")
	require.Equal(t, int32(14), client.Failures())
	require.Equal(t, int32(14), client.circuitFailures.Load())
	require.Equal(t, StatusConnected, client.Status())
	require.NoError(t, client.PublishToStream(ctx, "cap.healthy", []byte("still works")))
}

func testWithAckMaxBytesCircuitNeutral(t *testing.T) {
	ctx, client, js := newCapacityTestClient(t)
	_, err := js.CreateStream(ctx, jetstream.StreamConfig{
		Name: "CAP_BYTES", Subjects: []string{"cap.bytes"}, Storage: jetstream.MemoryStorage,
		Discard: jetstream.DiscardNew, MaxBytes: 1,
	})
	require.NoError(t, err)
	seedCircuitFailures(t, client, 14)

	_, err = client.PublishToStreamWithAck(ctx, "cap.bytes", []byte("rejected"))
	requireCapacityAPIError(t, err, "maximum bytes exceeded")
	require.Equal(t, int32(14), client.Failures())
	require.Equal(t, int32(14), client.circuitFailures.Load())
	require.Equal(t, StatusConnected, client.Status())
}

func testAsyncMaxMsgsPerSubjectCircuitNeutral(t *testing.T) {
	ctx, client, js := newCapacityTestClient(t)
	_, err := js.CreateStream(ctx, jetstream.StreamConfig{
		Name: "CAP_ASYNC", Subjects: []string{"cap.async.*"}, Storage: jetstream.MemoryStorage,
		Discard: jetstream.DiscardNew, DiscardNewPerSubject: true, MaxMsgsPerSubject: 1,
	})
	require.NoError(t, err)
	require.NoError(t, client.PublishToStream(ctx, "cap.async.one", []byte("first")))
	seedCircuitFailures(t, client, 14)

	future, err := client.PublishToStreamAsync(ctx, "cap.async.one", []byte("rejected"))
	require.NoError(t, err)
	require.NotNil(t, future)
	select {
	case ackErr := <-future.Err():
		requireCapacityAPIError(t, ackErr, "maximum messages per subject exceeded")
	case <-future.Ok():
		t.Fatal("capacity-limited async publish unexpectedly succeeded")
	case <-ctx.Done():
		t.Fatalf("async capacity future did not resolve: %v", ctx.Err())
	}
	select {
	case <-client.PublishAsyncComplete():
	case <-ctx.Done():
		t.Fatalf("async completion did not close: %v", ctx.Err())
	}
	// Successful enqueue is still the async liveness reset; its later capacity
	// nack is neutral and therefore cannot add a failure back.
	require.Zero(t, client.Failures())
	require.Zero(t, client.circuitFailures.Load())
	require.Equal(t, StatusConnected, client.Status())
}

func testBatchCapacityCircuitNeutral(t *testing.T) {
	ctx, client, js := newCapacityTestClient(t)
	_, err := js.CreateStream(ctx, jetstream.StreamConfig{
		Name: "CAP_BATCH", Subjects: []string{"cap.batch"}, Storage: jetstream.MemoryStorage,
		Discard: jetstream.DiscardNew, MaxMsgs: 1,
	})
	require.NoError(t, err)
	require.NoError(t, client.PublishToStream(ctx, "cap.batch", []byte("first")))
	seedCircuitFailures(t, client, 14)

	err = client.PublishBatchToStream(ctx, "cap.batch", [][]byte{[]byte("a"), []byte("b")})
	require.Error(t, err)
	requireCapacityAPIError(t, err, "maximum messages exceeded")
	require.Contains(t, err.Error(), "2 of 2 publishes failed")
	require.Zero(t, client.Failures())
	require.Zero(t, client.circuitFailures.Load())
	require.Equal(t, StatusConnected, client.Status())
}

func newCapacityTestClient(t *testing.T) (context.Context, *Client, jetstream.JetStream) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(cancel)
	testClient := NewTestClient(t, WithJetStream())
	client, err := NewClient(testClient.URL, WithMaxReconnects(0))
	require.NoError(t, err)
	require.NoError(t, client.Connect(ctx))
	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		_ = client.Close(closeCtx)
	})
	js, err := client.JetStream()
	require.NoError(t, err)
	return ctx, client, js
}

func seedCircuitFailures(t *testing.T, client *Client, count int) {
	t.Helper()
	for range count {
		client.recordFailure()
	}
	require.Equal(t, int32(count), client.Failures())
	require.Equal(t, int32(count), client.circuitFailures.Load())
	require.Equal(t, StatusConnected, client.Status())
}

func requireCapacityAPIError(t *testing.T, err error, description string) {
	t.Helper()
	var apiErr *jetstream.APIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, jetstream.ErrorCode(10077), apiErr.ErrorCode)
	require.Equal(t, description, apiErr.Description)
}
