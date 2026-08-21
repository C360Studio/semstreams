//go:build integration

package natsclient

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_PublishToStreamAsync_PipelinesAndDrains verifies the async
// publish path: N messages enqueue without blocking on individual acks, every
// returned future resolves via Ok() (no Err()), PublishAsyncComplete closes once
// drained, PublishAsyncPending returns to 0, and all N are stored (gh#470).
func TestIntegration_PublishToStreamAsync_PipelinesAndDrains(t *testing.T) {
	ctx := context.Background()

	natsContainer, natsURL := startNATSContainerWithJS(ctx, t)
	defer natsContainer.Terminate(ctx)

	client, err := NewClient(natsURL)
	require.NoError(t, err)
	require.NoError(t, client.Connect(ctx))
	defer client.Close(ctx)

	stream, err := client.EnsureStream(ctx, jetstream.StreamConfig{
		Name:     "ASYNC_STREAM",
		Subjects: []string{"async.>"},
		Storage:  jetstream.MemoryStorage,
		MaxAge:   testStreamMaxAge,
		MaxBytes: testStreamMaxBytes,
	})
	require.NoError(t, err)

	const n = 500
	futures := make([]jetstream.PubAckFuture, 0, n)
	for i := 0; i < n; i++ {
		f, perr := client.PublishToStreamAsync(ctx, "async.test", []byte(strconv.Itoa(i)))
		require.NoError(t, perr)
		require.NotNil(t, f)
		futures = append(futures, f)
	}

	// Drain: block until every outstanding async publish is acked.
	select {
	case <-client.PublishAsyncComplete():
	case <-time.After(10 * time.Second):
		t.Fatalf("PublishAsyncComplete did not close; %d still pending", client.PublishAsyncPending())
	}

	assert.Equal(t, 0, client.PublishAsyncPending(), "no async publishes should remain pending after drain")

	// Every future resolved successfully.
	for i, f := range futures {
		select {
		case <-f.Ok():
		case ackErr := <-f.Err():
			t.Fatalf("future %d resolved with error: %v", i, ackErr)
		default:
			t.Fatalf("future %d did not resolve after drain", i)
		}
	}

	info, err := stream.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(n), info.State.Msgs, "all async publishes must be stored")
}

// TestIntegration_PublishToStreamAsync_Ordering verifies per-subject ordering from
// a single caller/connection: an async-published monotonic sequence is stored (and
// consumed) in publish order.
func TestIntegration_PublishToStreamAsync_Ordering(t *testing.T) {
	ctx := context.Background()

	natsContainer, natsURL := startNATSContainerWithJS(ctx, t)
	defer natsContainer.Terminate(ctx)

	client, err := NewClient(natsURL)
	require.NoError(t, err)
	require.NoError(t, client.Connect(ctx))
	defer client.Close(ctx)

	_, err = client.EnsureStream(ctx, jetstream.StreamConfig{
		Name:     "ASYNC_ORDER_STREAM",
		Subjects: []string{"asyncorder.>"},
		Storage:  jetstream.MemoryStorage,
		MaxAge:   testStreamMaxAge,
		MaxBytes: testStreamMaxBytes,
	})
	require.NoError(t, err)

	const n = 200
	for i := 0; i < n; i++ {
		_, perr := client.PublishToStreamAsync(ctx, "asyncorder.seq", []byte(strconv.Itoa(i)))
		require.NoError(t, perr)
	}
	select {
	case <-client.PublishAsyncComplete():
	case <-time.After(10 * time.Second):
		t.Fatal("drain timeout")
	}

	// Consume in stream order and assert the sequence matches publish order.
	got := make([]int, 0, n)
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(n)
	handle, err := client.ConsumeStreamWithConfig(ctx, PortConsumerContext{Component: "integration", Port: "input"}, StreamConsumerConfig{
		StreamName:    "ASYNC_ORDER_STREAM",
		ConsumerName:  "order-consumer",
		FilterSubject: "asyncorder.>",
		DeliverPolicy: "all",
		AckPolicy:     "explicit",
	}, func(_ context.Context, msg jetstream.Msg) {
		v, _ := strconv.Atoi(string(msg.Data()))
		mu.Lock()
		got = append(got, v)
		mu.Unlock()
		msg.Ack()
		wg.Done()
	})
	require.NoError(t, err)
	defer drainNativeConsume(t, handle)

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("consume timeout, got %d/%d", len(got), n)
	}

	want := make([]int, n)
	for i := range want {
		want[i] = i
	}
	assert.Equal(t, want, got, "async publishes must be stored/consumed in publish order")
}

// TestIntegration_PublishToStreamAsyncWithMsgID_Dedup verifies the async WithMsgID
// variant carries the same ADR-055 T1 dedup contract as the synchronous one: the
// same Nats-Msg-Id within the window collapses to one stored message.
func TestIntegration_PublishToStreamAsyncWithMsgID_Dedup(t *testing.T) {
	ctx := context.Background()

	natsContainer, natsURL := startNATSContainerWithJS(ctx, t)
	defer natsContainer.Terminate(ctx)

	client, err := NewClient(natsURL)
	require.NoError(t, err)
	require.NoError(t, client.Connect(ctx))
	defer client.Close(ctx)

	stream, err := client.EnsureStream(ctx, jetstream.StreamConfig{
		Name:       "ASYNC_MSGID_STREAM",
		Subjects:   []string{"asyncmsgid.>"},
		Storage:    jetstream.MemoryStorage,
		Duplicates: 2 * time.Minute,
		MaxAge:     testStreamMaxAge,
		MaxBytes:   testStreamMaxBytes,
	})
	require.NoError(t, err)

	f1, err := client.PublishToStreamAsyncWithMsgID(ctx, "asyncmsgid.test", []byte("first"), "evt-1")
	require.NoError(t, err)
	f2, err := client.PublishToStreamAsyncWithMsgID(ctx, "asyncmsgid.test", []byte("first-again"), "evt-1")
	require.NoError(t, err)
	<-client.PublishAsyncComplete()

	for _, f := range []jetstream.PubAckFuture{f1, f2} {
		select {
		case <-f.Ok():
		case ackErr := <-f.Err():
			t.Fatalf("dedup publish future errored: %v", ackErr)
		}
	}

	info, err := stream.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), info.State.Msgs,
		"same Nats-Msg-Id within the window must dedup to one stored message on the async path")
}

// TestIntegration_PublishBatchToStream verifies the batch helper stores every
// message in publish order and returns nil on all-acked.
func TestIntegration_PublishBatchToStream(t *testing.T) {
	ctx := context.Background()

	natsContainer, natsURL := startNATSContainerWithJS(ctx, t)
	defer natsContainer.Terminate(ctx)

	client, err := NewClient(natsURL)
	require.NoError(t, err)
	require.NoError(t, client.Connect(ctx))
	defer client.Close(ctx)

	_, err = client.EnsureStream(ctx, jetstream.StreamConfig{
		Name:     "BATCH_STREAM",
		Subjects: []string{"batch.>"},
		Storage:  jetstream.MemoryStorage,
		MaxAge:   testStreamMaxAge,
		MaxBytes: testStreamMaxBytes,
	})
	require.NoError(t, err)

	const m = 300
	msgs := make([][]byte, m)
	for i := range msgs {
		msgs[i] = []byte(fmt.Sprintf("msg-%d", i))
	}

	require.NoError(t, client.PublishBatchToStream(ctx, "batch.test", msgs))
	assert.Equal(t, 0, client.PublishAsyncPending(), "batch must fully drain before returning")

	// Consume and assert order.
	got := make([]string, 0, m)
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(m)
	handle, err := client.ConsumeStreamWithConfig(ctx, PortConsumerContext{Component: "integration", Port: "input"}, StreamConsumerConfig{
		StreamName:    "BATCH_STREAM",
		ConsumerName:  "batch-consumer",
		FilterSubject: "batch.>",
		DeliverPolicy: "all",
		AckPolicy:     "explicit",
	}, func(_ context.Context, msg jetstream.Msg) {
		mu.Lock()
		got = append(got, string(msg.Data()))
		mu.Unlock()
		msg.Ack()
		wg.Done()
	})
	require.NoError(t, err)
	defer drainNativeConsume(t, handle)

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("consume timeout, got %d/%d", len(got), m)
	}

	want := make([]string, m)
	for i := range want {
		want[i] = fmt.Sprintf("msg-%d", i)
	}
	assert.Equal(t, want, got, "batch must be stored/consumed in publish order")
}

// TestIntegration_PublishToStreamAsync_StampsTraceAndMsgID verifies the async path
// preserves the same header invariants as the synchronous path: a trace context is
// injected (auto-generated when absent) and the WithMsgID variant stamps
// Nats-Msg-Id. Asserted on the raw consumed message headers.
func TestIntegration_PublishToStreamAsync_StampsTraceAndMsgID(t *testing.T) {
	ctx := context.Background()

	natsContainer, natsURL := startNATSContainerWithJS(ctx, t)
	defer natsContainer.Terminate(ctx)

	client, err := NewClient(natsURL)
	require.NoError(t, err)
	require.NoError(t, client.Connect(ctx))
	defer client.Close(ctx)

	_, err = client.EnsureStream(ctx, jetstream.StreamConfig{
		Name:     "ASYNC_HDR_STREAM",
		Subjects: []string{"asynchdr.>"},
		Storage:  jetstream.MemoryStorage,
		MaxAge:   testStreamMaxAge,
		MaxBytes: testStreamMaxBytes,
	})
	require.NoError(t, err)

	_, err = client.PublishToStreamAsyncWithMsgID(ctx, "asynchdr.test", []byte("payload"), "evt-hdr-1")
	require.NoError(t, err)
	<-client.PublishAsyncComplete()

	var hdr map[string][]string
	var wg sync.WaitGroup
	wg.Add(1)
	handle, err := client.ConsumeStreamWithConfig(ctx, PortConsumerContext{Component: "integration", Port: "input"}, StreamConsumerConfig{
		StreamName:    "ASYNC_HDR_STREAM",
		ConsumerName:  "hdr-consumer",
		FilterSubject: "asynchdr.>",
		DeliverPolicy: "all",
		AckPolicy:     "explicit",
	}, func(_ context.Context, msg jetstream.Msg) {
		hdr = msg.Headers()
		msg.Ack()
		wg.Done()
	})
	require.NoError(t, err)
	defer drainNativeConsume(t, handle)

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("consume timeout")
	}

	require.NotNil(t, hdr)
	assert.NotEmpty(t, hdr[TraceIDHeader], "async publish must inject a trace ID header")
	assert.NotEmpty(t, hdr[TraceparentHeader], "async publish must inject a W3C traceparent header")
	assert.Equal(t, "evt-hdr-1", hdr[nats.MsgIdHdr][0], "WithMsgID variant must stamp Nats-Msg-Id")
}

// TestIntegration_PublishToStreamAsync_EnqueueResetsCircuit verifies the documented
// breaker semantic (design §4): a successful async enqueue resets the failure
// count, giving a pure-async producer a reset path.
func TestIntegration_PublishToStreamAsync_EnqueueResetsCircuit(t *testing.T) {
	ctx := context.Background()

	natsContainer, natsURL := startNATSContainerWithJS(ctx, t)
	defer natsContainer.Terminate(ctx)

	client, err := NewClient(natsURL)
	require.NoError(t, err)
	require.NoError(t, client.Connect(ctx))
	defer client.Close(ctx)

	_, err = client.EnsureStream(ctx, jetstream.StreamConfig{
		Name:     "ASYNC_RESET_STREAM",
		Subjects: []string{"asyncreset.>"},
		Storage:  jetstream.MemoryStorage,
		MaxAge:   testStreamMaxAge,
		MaxBytes: testStreamMaxBytes,
	})
	require.NoError(t, err)

	// Accumulate failures below the open threshold.
	for i := 0; i < 5; i++ {
		client.recordFailure()
	}
	require.Equal(t, int32(5), client.Failures())

	// A successful async enqueue must reset the failure count.
	f, err := client.PublishToStreamAsync(ctx, "asyncreset.test", []byte("ok"))
	require.NoError(t, err)
	require.NotNil(t, f)
	assert.Equal(t, int32(0), client.Failures(),
		"a successful async enqueue must reset the circuit breaker failure count")
	<-client.PublishAsyncComplete()
}

// TestIntegration_PublishBatchToStream_CtxCancel verifies a batch whose context is
// already cancelled returns a context error rather than hanging.
func TestIntegration_PublishBatchToStream_CtxCancel(t *testing.T) {
	ctx := context.Background()

	natsContainer, natsURL := startNATSContainerWithJS(ctx, t)
	defer natsContainer.Terminate(ctx)

	client, err := NewClient(natsURL)
	require.NoError(t, err)
	require.NoError(t, client.Connect(ctx))
	defer client.Close(ctx)

	_, err = client.EnsureStream(ctx, jetstream.StreamConfig{
		Name:     "BATCH_CANCEL_STREAM",
		Subjects: []string{"batchcancel.>"},
		Storage:  jetstream.MemoryStorage,
		MaxAge:   testStreamMaxAge,
		MaxBytes: testStreamMaxBytes,
	})
	require.NoError(t, err)

	cancelledCtx, cancel := context.WithCancel(ctx)
	cancel()

	msgs := [][]byte{[]byte("a"), []byte("b"), []byte("c")}
	err = client.PublishBatchToStream(cancelledCtx, "batchcancel.test", msgs)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}
