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

type capturedAsyncErrorLog struct {
	level   slog.Level
	message string
	attrs   map[string]slog.Value
}

type asyncErrorLogHandler struct {
	records chan capturedAsyncErrorLog
}

func newAsyncErrorLogHandler() *asyncErrorLogHandler {
	return &asyncErrorLogHandler{records: make(chan capturedAsyncErrorLog, 8)}
}

func (h *asyncErrorLogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelError
}

func (h *asyncErrorLogHandler) Handle(_ context.Context, record slog.Record) error {
	attrs := make(map[string]slog.Value, record.NumAttrs())
	record.Attrs(func(attr slog.Attr) bool {
		attrs[attr.Key] = attr.Value
		return true
	})
	h.records <- capturedAsyncErrorLog{
		level:   record.Level,
		message: record.Message,
		attrs:   attrs,
	}
	return nil
}

func (h *asyncErrorLogHandler) WithAttrs(_ []slog.Attr) slog.Handler {
	return h
}

func (h *asyncErrorLogHandler) WithGroup(_ string) slog.Handler {
	return h
}

func (h *asyncErrorLogHandler) next(t *testing.T) capturedAsyncErrorLog {
	t.Helper()

	select {
	case record := <-h.records:
		return record
	default:
		t.Fatal("expected an async NATS error log record")
		return capturedAsyncErrorLog{}
	}
}

func TestClientHandleErrorAttributesSubscription(t *testing.T) {
	tests := []struct {
		name       string
		sub        *nats.Subscription
		err        error
		wantFields map[string]any
	}{
		{
			name: "nil subscription preserves the generic shape",
			err:  stderrors.New("connection asynchronous error"),
		},
		{
			name: "ordinary subscription error names the subject without a queue",
			sub:  &nats.Subscription{Subject: "agent.loop.>"},
			err:  stderrors.New("permissions error"),
			wantFields: map[string]any{
				"subject": "agent.loop.>",
			},
		},
		{
			name: "ordinary queue subscription error names the queue",
			sub: &nats.Subscription{
				Subject: "agent.loop.>",
				Queue:   "workers",
			},
			err: stderrors.New("permissions error"),
			wantFields: map[string]any{
				"subject": "agent.loop.>",
				"queue":   "workers",
			},
		},
		{
			name: "wrapped slow-consumer error reports an unavailable drop count",
			sub:  &nats.Subscription{Subject: "agent.loop.>"},
			err:  fmt.Errorf("subscription delivery failed: %w", nats.ErrSlowConsumer),
			wantFields: map[string]any{
				"subject":           "agent.loop.>",
				"dropped_available": false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := newAsyncErrorLogHandler()
			client, err := NewClient("nats://localhost:4222", WithLogger(slog.New(handler)))
			require.NoError(t, err)

			client.handleError(nil, tt.sub, tt.err)
			record := handler.next(t)

			assert.Equal(t, slog.LevelError, record.level)
			assert.Equal(t, "NATS error", record.message)
			require.Contains(t, record.attrs, "error")
			loggedErr, ok := record.attrs["error"].Any().(error)
			require.True(t, ok, "error attribute must retain an error value")
			assert.Same(t, tt.err, loggedErr)

			wantFieldCount := 1 + len(tt.wantFields)
			assert.Len(t, record.attrs, wantFieldCount)
			for key, want := range tt.wantFields {
				require.Contains(t, record.attrs, key)
				assert.Equal(t, want, record.attrs[key].Any())
			}
			for _, forbidden := range []string{
				"pending", "pending_bytes", "max_pending", "max_pending_bytes", "pending_limit", "pending_bytes_limit",
			} {
				assert.NotContains(t, record.attrs, forbidden)
			}
		})
	}
}

func TestClientHandleErrorDoesNotMutateRuntimeStateOrCallbacks(t *testing.T) {
	handler := newAsyncErrorLogHandler()
	client, err := NewClient("nats://localhost:4222", WithLogger(slog.New(handler)))
	require.NoError(t, err)

	client.setStatus(StatusConnected)
	client.failures.Store(4)
	client.circuitFailures.Store(3)
	lastFailure := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	client.lastFailure.Store(lastFailure)
	client.backoff.Store(4 * time.Second)

	wantStatus := client.Status()
	wantHealthy := client.IsHealthy()
	wantFailures := client.Failures()
	wantCircuitFailures := client.circuitFailures.Load()
	wantLastFailure := client.lastFailure.Load().(time.Time)
	wantBackoff := client.Backoff()

	client.handleError(nil, &nats.Subscription{Subject: "agent.loop.>"}, nats.ErrSlowConsumer)
	_ = handler.next(t)

	assert.Equal(t, wantStatus, client.Status())
	assert.Equal(t, wantHealthy, client.IsHealthy())
	assert.Equal(t, wantFailures, client.Failures())
	assert.Equal(t, wantCircuitFailures, client.circuitFailures.Load())
	assert.Equal(t, wantLastFailure, client.lastFailure.Load().(time.Time))
	assert.Equal(t, wantBackoff, client.Backoff())
}

var _ slog.Handler = (*asyncErrorLogHandler)(nil)
