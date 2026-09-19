package natsclient

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockMsg implements jetstream.Msg for testing heartbeat behavior.
type mockMsg struct {
	subject         string
	data            []byte
	dataCount       atomic.Int32
	metadata        *jetstream.MsgMetadata
	metadataErr     error
	metadataNil     bool
	metadataCount   atomic.Int32
	ackCalled       atomic.Bool
	nakCalled       atomic.Bool
	ackCount        atomic.Int32
	nakCount        atomic.Int32
	nakDelay        atomic.Int64 // stored as nanoseconds
	inProgressCount atomic.Int32
	termCalled      atomic.Bool
	termCount       atomic.Int32
	order           *atomic.Int64
	metadataOrder   atomic.Int64
	dataOrder       atomic.Int64
	settlementOrder atomic.Int64

	mu            sync.Mutex
	inProgressErr error
	ackErr        error
	nakErr        error
	termErr       error
}

func (m *mockMsg) Data() []byte {
	m.dataCount.Add(1)
	if m.order != nil {
		m.dataOrder.CompareAndSwap(0, m.order.Add(1))
	}
	return m.data
}
func (m *mockMsg) Subject() string      { return m.subject }
func (m *mockMsg) Reply() string        { return "" }
func (m *mockMsg) Headers() nats.Header { return nil }
func (m *mockMsg) Metadata() (*jetstream.MsgMetadata, error) {
	m.metadataCount.Add(1)
	if m.order != nil {
		m.metadataOrder.CompareAndSwap(0, m.order.Add(1))
	}
	if m.metadataErr != nil {
		return nil, m.metadataErr
	}
	if m.metadataNil {
		return nil, nil
	}
	if m.metadata != nil {
		return m.metadata, nil
	}
	return &jetstream.MsgMetadata{NumDelivered: 1}, nil
}

func (m *mockMsg) Ack() error {
	m.ackCalled.Store(true)
	m.ackCount.Add(1)
	m.recordSettlement()
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ackErr
}

func (m *mockMsg) DoubleAck(_ context.Context) error { return nil }

func (m *mockMsg) Nak() error {
	m.nakCalled.Store(true)
	m.nakCount.Add(1)
	m.recordSettlement()
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.nakErr
}

func (m *mockMsg) NakWithDelay(delay time.Duration) error {
	m.nakCalled.Store(true)
	m.nakCount.Add(1)
	m.recordSettlement()
	m.nakDelay.Store(int64(delay))
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.nakErr
}

func (m *mockMsg) InProgress() error {
	m.inProgressCount.Add(1)
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.inProgressErr
}

func (m *mockMsg) Term() error {
	m.termCalled.Store(true)
	m.termCount.Add(1)
	m.recordSettlement()
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.termErr
}

func TestConsumeWithHeartbeatSurfacesSettlementErrors(t *testing.T) {
	t.Run("transient NAK", func(t *testing.T) {
		settleErr := errors.New("nak failed")
		msg := &mockMsg{nakErr: settleErr}
		err := ConsumeWithHeartbeat(context.Background(), msg, time.Second,
			func(context.Context) error { return errors.New("work failed") })
		require.ErrorIs(t, err, settleErr)
	})

	t.Run("shutdown NAK", func(t *testing.T) {
		settleErr := errors.New("shutdown nak failed")
		msg := &mockMsg{nakErr: settleErr}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := ConsumeWithHeartbeat(ctx, msg, time.Second,
			func(workCtx context.Context) error { <-workCtx.Done(); return workCtx.Err() })
		require.ErrorIs(t, err, settleErr)
	})

	t.Run("permanent Term", func(t *testing.T) {
		settleErr := errors.New("term failed")
		msg := &mockMsg{termErr: settleErr}
		err := ConsumeWithHeartbeat(context.Background(), msg, time.Second,
			func(context.Context) error { return TerminateDelivery(errors.New("bad payload")) })
		require.ErrorIs(t, err, settleErr)
	})
}

func (m *mockMsg) TermWithReason(_ string) error {
	m.termCalled.Store(true)
	m.termCount.Add(1)
	m.recordSettlement()
	return nil
}

func (m *mockMsg) recordSettlement() {
	if m.order != nil {
		m.settlementOrder.CompareAndSwap(0, m.order.Add(1))
	}
}

func TestConsumeWithHeartbeat_AcksOnSuccess(t *testing.T) {
	msg := &mockMsg{subject: "test.subject"}

	err := ConsumeWithHeartbeat(
		context.Background(),
		msg,
		50*time.Millisecond,
		func(_ context.Context) error {
			return nil
		},
	)

	require.NoError(t, err)
	assert.True(t, msg.ackCalled.Load(), "expected Ack to be called")
	assert.False(t, msg.nakCalled.Load(), "expected Nak to not be called")
}

func TestConsumeWithHeartbeat_NaksWithDelayOnWorkError(t *testing.T) {
	msg := &mockMsg{subject: "test.subject"}
	workErr := errors.New("work failed")

	err := ConsumeWithHeartbeat(
		context.Background(),
		msg,
		50*time.Millisecond,
		func(_ context.Context) error {
			return workErr
		},
	)

	require.ErrorIs(t, err, workErr)
	assert.True(t, msg.nakCalled.Load(), "expected NakWithDelay to be called")
	assert.Equal(t, int64(30*time.Second), msg.nakDelay.Load(), "expected 30s delay")
	assert.False(t, msg.ackCalled.Load(), "expected Ack to not be called")
}

func TestConsumeWithHeartbeatTermsPermanentWorkError(t *testing.T) {
	msg := &mockMsg{subject: "test.subject"}
	workErr := errors.New("permanent invalid payload")

	err := ConsumeWithHeartbeat(
		context.Background(),
		msg,
		50*time.Millisecond,
		func(context.Context) error { return TerminateDelivery(workErr) },
	)

	require.ErrorIs(t, err, workErr)
	assert.True(t, msg.termCalled.Load(), "permanent delivery must be terminated")
	assert.False(t, msg.ackCalled.Load(), "permanent delivery must not be acked as success")
	assert.False(t, msg.nakCalled.Load(), "permanent delivery must not be retried")
}

func TestConsumeWithHeartbeat_NaksOnContextCancel(t *testing.T) {
	var order atomic.Int64
	msg := &mockMsg{subject: "test.subject", order: &order}
	ctx, cancel := context.WithCancel(t.Context())
	workStarted := make(chan struct{})
	workCancelled := make(chan struct{})
	releaseCleanup := make(chan struct{})
	returned := make(chan error, 1)
	var workExitOrder atomic.Int64
	var helperReturnOrder atomic.Int64

	go func() {
		err := ConsumeWithHeartbeat(
			ctx,
			msg,
			time.Second,
			func(workCtx context.Context) error {
				close(workStarted)
				<-workCtx.Done()
				close(workCancelled)
				<-releaseCleanup
				workExitOrder.Store(order.Add(1))
				return workCtx.Err()
			},
		)
		helperReturnOrder.Store(order.Add(1))
		returned <- err
	}()

	<-workStarted
	cancel()
	<-workCancelled
	close(releaseCleanup)
	var err error
	select {
	case err = <-returned:
	case <-time.After(time.Second):
		require.FailNow(t, "ConsumeWithHeartbeat did not return after work cleanup")
	}

	require.ErrorIs(t, err, context.Canceled)
	assert.Positive(t, workExitOrder.Load())
	assert.Greater(t, msg.settlementOrder.Load(), workExitOrder.Load(), "work exit must precede settlement")
	assert.Greater(t, helperReturnOrder.Load(), msg.settlementOrder.Load(), "settlement must precede helper return")
	assert.True(t, msg.nakCalled.Load(), "expected NakWithDelay to be called")
	assert.Equal(t, int64(5*time.Second), msg.nakDelay.Load(), "expected 5s delay")
}

func TestConsumeWithHeartbeat_SendsInProgressBeforeAckWait(t *testing.T) {
	msg := &mockMsg{subject: "test.subject"}
	heartbeatInterval := 20 * time.Millisecond

	err := ConsumeWithHeartbeat(
		context.Background(),
		msg,
		heartbeatInterval,
		func(_ context.Context) error {
			// Work takes long enough for multiple heartbeats
			time.Sleep(70 * time.Millisecond)
			return nil
		},
	)

	require.NoError(t, err)
	assert.True(t, msg.ackCalled.Load())
	// Should have sent at least 2 heartbeats (70ms / 20ms = ~3)
	count := msg.inProgressCount.Load()
	assert.GreaterOrEqual(t, count, int32(2), "expected at least 2 InProgress calls, got %d", count)
}

func TestConsumeWithHeartbeat_ReturnsErrorOnInProgressFailure(t *testing.T) {
	msg := &mockMsg{
		subject:       "test.subject",
		inProgressErr: errors.New("connection lost"),
	}

	err := ConsumeWithHeartbeat(
		context.Background(),
		msg,
		10*time.Millisecond,
		func(_ context.Context) error {
			// Work takes longer than heartbeat interval
			time.Sleep(50 * time.Millisecond)
			return nil
		},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to send InProgress")
}

func TestConsumeWithHeartbeat_CancelsWorkOnInProgressFailure(t *testing.T) {
	var order atomic.Int64
	msg := &mockMsg{
		subject:       "test.subject",
		inProgressErr: errors.New("connection lost"),
		order:         &order,
	}
	workStarted := make(chan struct{})
	workCancelled := make(chan struct{})
	releaseCleanup := make(chan struct{})
	returned := make(chan error, 1)
	var workExitOrder atomic.Int64
	var helperReturnOrder atomic.Int64

	go func() {
		err := ConsumeWithHeartbeat(
			t.Context(),
			msg,
			time.Millisecond,
			func(workCtx context.Context) error {
				close(workStarted)
				<-workCtx.Done()
				close(workCancelled)
				<-releaseCleanup
				workExitOrder.Store(order.Add(1))
				return workCtx.Err()
			},
		)
		helperReturnOrder.Store(order.Add(1))
		returned <- err
	}()

	<-workStarted
	<-workCancelled
	close(releaseCleanup)
	var err error
	select {
	case err = <-returned:
	case <-time.After(time.Second):
		require.FailNow(t, "ConsumeWithHeartbeat did not return after work cleanup")
	}

	require.ErrorIs(t, err, ErrHeartbeatFailed)
	assert.Contains(t, err.Error(), "failed to send InProgress")
	assert.Positive(t, workExitOrder.Load())
	assert.Greater(t, helperReturnOrder.Load(), workExitOrder.Load(), "work exit must precede helper return")
	assert.Zero(t, msg.ackCount.Load(), "heartbeat failure must not ACK")
	assert.Zero(t, msg.nakCount.Load(), "heartbeat failure must remain unsettled")
	assert.Zero(t, msg.termCount.Load(), "heartbeat failure must not terminate")
}
func TestConsumeWithHeartbeat_JoinsCleanupErrorOnInProgressFailure(t *testing.T) {
	cleanupErr := errors.New("cleanup failed")
	heartbeatErr := errors.New("connection lost")
	msg := &mockMsg{
		subject:       "test.subject",
		inProgressErr: heartbeatErr,
	}

	err := ConsumeWithHeartbeat(
		t.Context(),
		msg,
		time.Millisecond,
		func(workCtx context.Context) error {
			<-workCtx.Done()
			return cleanupErr
		},
	)

	require.ErrorIs(t, err, ErrHeartbeatFailed)
	require.ErrorIs(t, err, heartbeatErr)
	require.ErrorIs(t, err, cleanupErr)
}

func TestConsumeWithHeartbeat_RetainsCleanupErrorJoinedWithCancellation(t *testing.T) {
	cleanupErr := errors.New("cleanup failed")
	heartbeatErr := errors.New("connection lost")
	msg := &mockMsg{subject: "test.subject", inProgressErr: heartbeatErr}

	err := ConsumeWithHeartbeat(t.Context(), msg, time.Millisecond, func(workCtx context.Context) error {
		<-workCtx.Done()
		return errors.Join(workCtx.Err(), cleanupErr)
	})

	require.ErrorIs(t, err, ErrHeartbeatFailed)
	require.ErrorIs(t, err, heartbeatErr)
	require.ErrorIs(t, err, cleanupErr)
}

func TestConsumeWithHeartbeat_FastWorkNoHeartbeat(t *testing.T) {
	msg := &mockMsg{subject: "test.subject"}

	err := ConsumeWithHeartbeat(
		context.Background(),
		msg,
		time.Second, // heartbeat interval much longer than work
		func(_ context.Context) error {
			return nil // instant completion
		},
	)

	require.NoError(t, err)
	assert.True(t, msg.ackCalled.Load())
	assert.Equal(t, int32(0), msg.inProgressCount.Load(), "no heartbeats expected for fast work")
}
