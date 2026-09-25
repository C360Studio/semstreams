package natsclient

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
)

// fakeNativeSubscription models the nats.go closed handler: it fires once,
// after the delivery goroutine exits, on every terminal path.
type fakeNativeSubscription struct {
	valid        atomic.Bool
	drainCalls   atomic.Int32
	drainErr     error
	drainCalled  chan struct{}
	drainOnce    sync.Once
	closeOnDrain bool

	closedMu   sync.Mutex
	closed     func(string)
	closedOnce sync.Once
}

func newFakeNativeSubscription() *fakeNativeSubscription {
	sub := &fakeNativeSubscription{drainCalled: make(chan struct{})}
	sub.valid.Store(true)
	return sub
}

func (s *fakeNativeSubscription) Drain() error {
	s.drainCalls.Add(1)
	s.drainOnce.Do(func() { close(s.drainCalled) })
	if s.closeOnDrain {
		s.valid.Store(false)
		s.fireClosed()
	}
	return s.drainErr
}

func (s *fakeNativeSubscription) IsValid() bool {
	return s.valid.Load()
}

func (s *fakeNativeSubscription) SetClosedHandler(handler func(string)) {
	s.closedMu.Lock()
	s.closed = handler
	s.closedMu.Unlock()
}

// fireClosed stands in for the delivery goroutine exiting.
func (s *fakeNativeSubscription) fireClosed() {
	s.closedMu.Lock()
	handler := s.closed
	s.closedMu.Unlock()
	if handler != nil {
		s.closedOnce.Do(func() { handler("fake") })
	}
}

func (s *fakeNativeSubscription) Unsubscribe() error {
	s.valid.Store(false)
	s.fireClosed()
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
	native.fireClosed()
	require.NoError(t, <-result)
	require.Equal(t, int32(1), native.drainCalls.Load())
}

func TestSubscriptionDrainExternallyUnsubscribedIsSuccessfulAndRepeatable(t *testing.T) {
	native := newFakeNativeSubscription()
	sub := newSubscription(native)
	require.NoError(t, native.Unsubscribe())

	require.NoError(t, sub.Drain(context.Background()))
	require.NoError(t, sub.Drain(context.Background()))
	require.Equal(t, int32(1), native.drainCalls.Load(), "native Drain on an unsubscribed sub is a nil no-op")
}

func TestSubscriptionDrainBornInvalidIsSuccessful(t *testing.T) {
	native := newFakeNativeSubscription()
	native.valid.Store(false)
	sub := newSubscription(native)

	require.NoError(t, sub.Drain(context.Background()))
	require.NoError(t, sub.Drain(context.Background()))
}

func TestSubscriptionDrainNativeErrBadSubscriptionIsSticky(t *testing.T) {
	native := newFakeNativeSubscription()
	native.drainErr = nats.ErrBadSubscription
	native.closeOnDrain = true
	sub := newSubscription(native)

	require.ErrorIs(t, sub.Drain(context.Background()), nats.ErrBadSubscription)
	require.ErrorIs(t, sub.Drain(context.Background()), nats.ErrBadSubscription)
	require.Equal(t, int32(1), native.drainCalls.Load())
}

func TestSubscriptionDrainOtherNativeErrorSurvivesClosure(t *testing.T) {
	native := newFakeNativeSubscription()
	native.drainErr = errors.New("native drain failed")
	native.closeOnDrain = true
	sub := newSubscription(native)

	require.EqualError(t, sub.Drain(context.Background()), "native drain failed")
}

func TestSubscriptionDrainConnectionClosesMidDrain(t *testing.T) {
	native := newFakeNativeSubscription()
	sub := newSubscription(native)

	// A canceled ctx proves Drain is still waiting once native Drain has
	// returned nil and the connection has gone.
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, sub.Drain(canceled), context.Canceled)
	native.valid.Store(false)
	require.ErrorIs(t, sub.Drain(canceled), context.Canceled)

	native.fireClosed()
	ctx, cancelBounded := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelBounded()
	require.NoError(t, sub.Drain(ctx))
	require.Equal(t, int32(1), native.drainCalls.Load())
}

func TestSubscriptionDrainNativeConnectionClosedWaitsForClosure(t *testing.T) {
	native := newFakeNativeSubscription()
	sub := newSubscription(native)
	// The connection closes after construction, so native Drain refuses.
	native.valid.Store(false)
	native.drainErr = nats.ErrConnectionClosed

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, sub.Drain(canceled), context.Canceled,
		"ErrConnectionClosed must wait for the closed handler, not return early")

	native.fireClosed()
	ctx, cancelBounded := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelBounded()
	require.NoError(t, sub.Drain(ctx))
	require.Equal(t, int32(1), native.drainCalls.Load())
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
	native.fireClosed()
	require.NoError(t, <-joined)
	require.Equal(t, int32(1), native.drainCalls.Load())
}

func TestSubscriptionDrainContextEndsFirst(t *testing.T) {
	native := newFakeNativeSubscription()
	sub := newSubscription(native)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- sub.Drain(ctx) }()
	<-native.drainCalled
	cancel()
	require.ErrorIs(t, <-result, context.Canceled)

	native.fireClosed()
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
	native.fireClosed()
	for range callers {
		require.NoError(t, <-results)
	}
	require.NoError(t, sub.Drain(context.Background()))
	require.Equal(t, int32(1), native.drainCalls.Load())
}
