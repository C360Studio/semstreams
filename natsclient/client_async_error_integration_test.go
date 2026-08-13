//go:build integration

package natsclient

import (
	"context"
	stderrors "errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type asyncErrorCallbackObservation struct {
	conn *nats.Conn
	sub  *nats.Subscription
	err  error
}

func TestIntegration_ClientHandleErrorLogsObservedSlowConsumerDropCount(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	testClient := NewTestClient(t, WithMinimalFeatures())
	handler := newAsyncErrorLogHandler()
	client, err := NewClient(testClient.URL, WithLogger(slog.New(handler)))
	require.NoError(t, err)
	require.NoError(t, client.Connect(ctx))
	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		require.NoError(t, client.Close(closeCtx))
	})

	conn := client.GetConnection()
	require.NotNil(t, conn)

	callbackEntered := make(chan asyncErrorCallbackObservation, 1)
	releaseCallback := make(chan struct{})
	callbackHandled := make(chan struct{})
	conn.SetErrorHandler(func(callbackConn *nats.Conn, sub *nats.Subscription, callbackErr error) {
		callbackEntered <- asyncErrorCallbackObservation{
			conn: callbackConn,
			sub:  sub,
			err:  callbackErr,
		}
		select {
		case <-releaseCallback:
			client.handleError(callbackConn, sub, callbackErr)
			close(callbackHandled)
		case <-ctx.Done():
		}
	})

	const subject = "diagnostics.slow-consumer"
	handlerEntered := make(chan struct{})
	releaseHandler := make(chan struct{})
	sub, err := conn.Subscribe(subject, func(_ *nats.Msg) {
		close(handlerEntered)
		<-releaseHandler
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		close(releaseHandler)
		_ = sub.Unsubscribe()
	})
	require.NoError(t, sub.SetPendingLimits(1, -1))
	require.NoError(t, conn.FlushWithContext(ctx))

	require.NoError(t, conn.Publish(subject, []byte("block handler")))
	require.NoError(t, conn.FlushWithContext(ctx))
	select {
	case <-handlerEntered:
	case <-ctx.Done():
		t.Fatalf("message handler did not block before overflow: %v", ctx.Err())
	}

	const additionalMessages = 8
	for i := range additionalMessages {
		require.NoError(t, conn.Publish(subject, []byte(fmt.Sprintf("overflow-%d", i))))
	}
	require.NoError(t, conn.FlushWithContext(ctx))

	var callback asyncErrorCallbackObservation
	select {
	case callback = <-callbackEntered:
	case <-ctx.Done():
		t.Fatalf("real NATS error callback was not invoked: %v", ctx.Err())
	}
	require.Same(t, conn, callback.conn)
	require.Same(t, sub, callback.sub)
	require.ErrorIs(t, callback.err, nats.ErrSlowConsumer)

	observedDropped := waitForExactDroppedCount(t, ctx, sub, additionalMessages)
	require.Greater(t, observedDropped, 1)

	close(releaseCallback)
	select {
	case <-callbackHandled:
	case <-ctx.Done():
		t.Fatalf("production error handler did not return: %v", ctx.Err())
	}

	record := waitForAsyncErrorLog(t, ctx, handler)
	assert.Equal(t, slog.LevelError, record.level)
	assert.Equal(t, "NATS error", record.message)
	require.Contains(t, record.attrs, "error")
	loggedErr, ok := record.attrs["error"].Any().(error)
	require.True(t, ok)
	assert.True(t, stderrors.Is(loggedErr, nats.ErrSlowConsumer))
	require.Contains(t, record.attrs, "subject")
	assert.Equal(t, subject, record.attrs["subject"].String())
	assert.NotContains(t, record.attrs, "queue")
	require.Contains(t, record.attrs, "dropped")
	assert.Equal(t, int64(observedDropped), record.attrs["dropped"].Int64())
}

func waitForExactDroppedCount(
	t *testing.T,
	ctx context.Context,
	sub *nats.Subscription,
	want int,
) int {
	t.Helper()

	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		dropped, err := sub.Dropped()
		require.NoError(t, err)
		if dropped == want {
			return dropped
		}
		if dropped > want {
			t.Fatalf("subscription dropped %d messages, want exact fixed-count result %d", dropped, want)
		}

		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatalf("subscription dropped %d messages before timeout, want %d: %v", dropped, want, ctx.Err())
		}
	}
}

func waitForAsyncErrorLog(
	t *testing.T,
	ctx context.Context,
	handler *asyncErrorLogHandler,
) capturedAsyncErrorLog {
	t.Helper()

	select {
	case record := <-handler.records:
		return record
	case <-ctx.Done():
		t.Fatalf("async NATS error log was not emitted: %v", ctx.Err())
		return capturedAsyncErrorLog{}
	}
}
