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
	ackCalled       atomic.Bool
	nakCalled       atomic.Bool
	nakDelay        atomic.Int64 // stored as nanoseconds
	inProgressCount atomic.Int32
	termCalled      atomic.Bool

	mu            sync.Mutex
	inProgressErr error
	ackErr        error
}

func (m *mockMsg) Data() []byte                              { return m.data }
func (m *mockMsg) Subject() string                           { return m.subject }
func (m *mockMsg) Reply() string                             { return "" }
func (m *mockMsg) Headers() nats.Header                      { return nil }
func (m *mockMsg) Metadata() (*jetstream.MsgMetadata, error) { return nil, nil }

func (m *mockMsg) Ack() error {
	m.ackCalled.Store(true)
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ackErr
}

func (m *mockMsg) DoubleAck(_ context.Context) error { return nil }

func (m *mockMsg) Nak() error {
	m.nakCalled.Store(true)
	return nil
}

func (m *mockMsg) NakWithDelay(delay time.Duration) error {
	m.nakCalled.Store(true)
	m.nakDelay.Store(int64(delay))
	return nil
}

func (m *mockMsg) InProgress() error {
	m.inProgressCount.Add(1)
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.inProgressErr
}

func (m *mockMsg) Term() error {
	m.termCalled.Store(true)
	return nil
}

func (m *mockMsg) TermWithReason(_ string) error {
	m.termCalled.Store(true)
	return nil
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

func TestConsumeWithHeartbeat_NaksOnContextCancel(t *testing.T) {
	msg := &mockMsg{subject: "test.subject"}
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel context after a short delay
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	err := ConsumeWithHeartbeat(
		ctx,
		msg,
		10*time.Millisecond,
		func(workCtx context.Context) error {
			<-workCtx.Done()
			// Simulate work noticing cancellation but taking time to clean up
			time.Sleep(50 * time.Millisecond)
			return workCtx.Err()
		},
	)

	require.ErrorIs(t, err, context.Canceled)
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
	msg := &mockMsg{
		subject:       "test.subject",
		inProgressErr: errors.New("connection lost"),
	}

	var workCancelled atomic.Bool
	err := ConsumeWithHeartbeat(
		context.Background(),
		msg,
		10*time.Millisecond,
		func(workCtx context.Context) error {
			<-workCtx.Done()
			workCancelled.Store(true)
			return workCtx.Err()
		},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to send InProgress")
	// Give the goroutine a moment to observe cancellation
	time.Sleep(20 * time.Millisecond)
	assert.True(t, workCancelled.Load(), "work goroutine should be cancelled on heartbeat failure")
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
