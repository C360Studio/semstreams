package natsclient

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
)

type fakeNativeSubscription struct {
	valid        atomic.Bool
	drainCalls   atomic.Int32
	drainErr     error
	drainCalled  chan struct{}
	status       chan nats.SubStatus
	drainOnce    sync.Once
	statusOnce   sync.Once
	closeOnDrain bool
	statusesMu   sync.Mutex
	statuses     []nats.SubStatus
}

func newFakeNativeSubscription() *fakeNativeSubscription {
	sub := &fakeNativeSubscription{
		drainCalled: make(chan struct{}),
		status:      make(chan nats.SubStatus),
	}
	sub.valid.Store(true)
	return sub
}

func (s *fakeNativeSubscription) Drain() error {
	s.drainCalls.Add(1)
	s.drainOnce.Do(func() { close(s.drainCalled) })
	if s.closeOnDrain {
		s.valid.Store(false)
		s.statusOnce.Do(func() { close(s.status) })
	}
	return s.drainErr
}

func (s *fakeNativeSubscription) IsValid() bool {
	return s.valid.Load()
}

func (s *fakeNativeSubscription) StatusChanged(statuses ...nats.SubStatus) <-chan nats.SubStatus {
	s.statusesMu.Lock()
	s.statuses = append([]nats.SubStatus(nil), statuses...)
	s.statusesMu.Unlock()
	return s.status
}

func (s *fakeNativeSubscription) Unsubscribe() error {
	s.valid.Store(false)
	s.statusOnce.Do(func() { close(s.status) })
	return nil
}

func TestSubscriptionDrainWaitsForNativeClosure(t *testing.T) {
	native := newFakeNativeSubscription()
	sub := newSubscription(native)
	result := make(chan error, 1)
	go func() { result <- sub.Drain(context.Background()) }()

	<-native.drainCalled
	select {
	case err := <-result:
		t.Fatalf("Drain returned before native subscription closed: %v", err)
	default:
	}
	native.status <- nats.SubscriptionClosed
	require.NoError(t, <-result)
	require.Equal(t, int32(1), native.drainCalls.Load())
	require.Equal(t, []nats.SubStatus{nats.SubscriptionClosed}, native.statuses)
}

func TestSubscriptionDrainExternallyUnsubscribedIsSuccessfulAndRepeatable(t *testing.T) {
	native := newFakeNativeSubscription()
	sub := newSubscription(native)
	require.NoError(t, native.Unsubscribe())

	require.NoError(t, sub.Drain(context.Background()))
	require.NoError(t, sub.Drain(context.Background()))
	require.Zero(t, native.drainCalls.Load())
}

func TestSubscriptionDrainInvalidWithoutClosedAuthorityPreservesErrBadSubscription(t *testing.T) {
	native := newFakeNativeSubscription()
	native.valid.Store(false)
	sub := newSubscription(native)

	require.ErrorIs(t, sub.Drain(context.Background()), nats.ErrBadSubscription)
	require.Zero(t, native.drainCalls.Load())
}

func TestSubscriptionDrainClosedErrBadSubscriptionIsSuccessful(t *testing.T) {
	native := newFakeNativeSubscription()
	native.drainErr = nats.ErrBadSubscription
	native.closeOnDrain = true
	sub := newSubscription(native)

	require.NoError(t, sub.Drain(context.Background()))
	require.NoError(t, sub.Drain(context.Background()))
	require.Equal(t, int32(1), native.drainCalls.Load())
}

func TestSubscriptionDrainOtherNativeErrorSurvivesClosure(t *testing.T) {
	native := newFakeNativeSubscription()
	native.drainErr = errors.New("native drain failed")
	native.closeOnDrain = true
	sub := newSubscription(native)

	require.EqualError(t, sub.Drain(context.Background()), "native drain failed")
}

func TestSubscriptionDrainCanceledCallerDoesNotConsumeOrDetachAuthority(t *testing.T) {
	native := newFakeNativeSubscription()
	sub := newSubscription(native)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	require.ErrorIs(t, sub.Drain(canceled), context.Canceled)
	<-native.drainCalled
	require.Equal(t, int32(1), native.drainCalls.Load(), "canceled authority still initiates native drain")

	firstCtx, cancelFirst := context.WithCancel(context.Background())
	firstResult := make(chan error, 1)
	go func() { firstResult <- sub.Drain(firstCtx) }()
	cancelFirst()
	require.ErrorIs(t, <-firstResult, context.Canceled)
	require.Equal(t, int32(1), native.drainCalls.Load())

	joined := make(chan error, 1)
	go func() { joined <- sub.Drain(context.Background()) }()
	select {
	case err := <-joined:
		t.Fatalf("later Drain did not rejoin native completion: %v", err)
	default:
	}
	close(native.status)
	require.NoError(t, <-joined)
	require.Equal(t, int32(1), native.drainCalls.Load())
}

func TestSubscriptionDrainDeadlineWinsAfterNativeClosure(t *testing.T) {
	native := newFakeNativeSubscription()
	sub := newSubscription(native)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- sub.Drain(ctx) }()
	<-native.drainCalled
	cancel()
	close(native.status)
	require.ErrorIs(t, <-result, context.Canceled)
	require.NoError(t, sub.Drain(context.Background()))
}

func TestSubscriptionDrainConcurrentCallersShareNativeDrain(t *testing.T) {
	native := newFakeNativeSubscription()
	sub := newSubscription(native)
	const callers = 8
	results := make(chan error, callers)
	var ready sync.WaitGroup
	ready.Add(callers)
	start := make(chan struct{})
	for range callers {
		go func() {
			ready.Done()
			<-start
			results <- sub.Drain(context.Background())
		}()
	}
	ready.Wait()
	close(start)
	<-native.drainCalled
	close(native.status)
	for range callers {
		require.NoError(t, <-results)
	}
	require.NoError(t, sub.Drain(context.Background()))
	require.Equal(t, int32(1), native.drainCalls.Load())
}
