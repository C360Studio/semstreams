package natsclient

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeConsumeContext struct {
	drainCalls atomic.Int32
	stopCalls  atomic.Int32
	drained    chan struct{}
	closed     chan struct{}
	drainOnce  sync.Once
}

func newFakeConsumeContext() *fakeConsumeContext {
	return &fakeConsumeContext{
		drained: make(chan struct{}),
		closed:  make(chan struct{}),
	}
}

func (c *fakeConsumeContext) Stop() {
	c.stopCalls.Add(1)
}

func (c *fakeConsumeContext) Drain() {
	c.drainCalls.Add(1)
	c.drainOnce.Do(func() { close(c.drained) })
}

func (c *fakeConsumeContext) Closed() <-chan struct{} { return c.closed }

func TestStopConsumerWaitsForNativeDrainAndIsRepeatable(t *testing.T) {
	consumeCtx := newFakeConsumeContext()
	client := &Client{consumers: map[string]consumerBinding{
		"STREAM:consumer": newConsumerBinding(consumeCtx, nil, consumerPolicyKey{}),
	}}
	result := make(chan error, 1)
	go func() { result <- client.StopConsumer(context.Background(), "STREAM", "consumer") }()

	<-consumeCtx.drained
	select {
	case err := <-result:
		t.Fatalf("StopConsumer returned before native drain closed: %v", err)
	default:
	}
	close(consumeCtx.closed)
	require.NoError(t, <-result)
	require.NoError(t, client.StopConsumer(context.Background(), "STREAM", "consumer"))
	require.Equal(t, int32(1), consumeCtx.drainCalls.Load())
	require.Zero(t, consumeCtx.stopCalls.Load())
}

func TestStopConsumerCanceledCallerDoesNotStartOrDetachDrain(t *testing.T) {
	consumeCtx := newFakeConsumeContext()
	client := &Client{consumers: map[string]consumerBinding{
		"STREAM:consumer": newConsumerBinding(consumeCtx, nil, consumerPolicyKey{}),
	}}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	require.ErrorIs(t, client.StopConsumer(canceled, "STREAM", "consumer"), context.Canceled)
	require.Zero(t, consumeCtx.drainCalls.Load())

	firstCtx, cancelFirst := context.WithCancel(context.Background())
	firstResult := make(chan error, 1)
	go func() { firstResult <- client.StopConsumer(firstCtx, "STREAM", "consumer") }()
	<-consumeCtx.drained
	cancelFirst()
	require.ErrorIs(t, <-firstResult, context.Canceled)

	rejoined := make(chan error, 1)
	go func() { rejoined <- client.StopConsumer(context.Background(), "STREAM", "consumer") }()
	select {
	case err := <-rejoined:
		t.Fatalf("later StopConsumer did not rejoin native drain: %v", err)
	default:
	}
	close(consumeCtx.closed)
	require.NoError(t, <-rejoined)
	require.Equal(t, int32(1), consumeCtx.drainCalls.Load())
}

func TestStopConsumerConcurrentCallersShareDrain(t *testing.T) {
	consumeCtx := newFakeConsumeContext()
	client := &Client{consumers: map[string]consumerBinding{
		"STREAM:consumer": newConsumerBinding(consumeCtx, nil, consumerPolicyKey{}),
	}}
	const callers = 8
	results := make(chan error, callers)
	start := make(chan struct{})
	var ready sync.WaitGroup
	ready.Add(callers)
	for range callers {
		go func() {
			ready.Done()
			<-start
			results <- client.StopConsumer(context.Background(), "STREAM", "consumer")
		}()
	}
	ready.Wait()
	close(start)
	<-consumeCtx.drained
	close(consumeCtx.closed)
	for range callers {
		require.NoError(t, <-results)
	}
	require.Equal(t, int32(1), consumeCtx.drainCalls.Load())
}

func TestStopConsumerInFlightCallbackCanFinishThroughCallerStateLock(t *testing.T) {
	consumeCtx := newFakeConsumeContext()
	client := &Client{consumers: map[string]consumerBinding{
		"STREAM:consumer": newConsumerBinding(consumeCtx, nil, consumerPolicyKey{}),
	}}
	var stateMu sync.Mutex
	callbackDone := make(chan struct{})
	go func() {
		<-consumeCtx.drained
		stateMu.Lock()
		stateMu.Unlock()
		close(callbackDone)
		close(consumeCtx.closed)
	}()

	// Component Stop implementations snapshot handles under their state lock,
	// then release it before entering the shared native drain.
	stateMu.Lock()
	streamName, consumerName := "STREAM", "consumer"
	stateMu.Unlock()
	require.NoError(t, client.StopConsumer(context.Background(), streamName, consumerName))
	<-callbackDone
}
