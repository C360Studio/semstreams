package objectstore

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
)

// #727 ack decision table: a JetStream write delivery must only be positively
// acked when the store commit (and any required StorageReference publication)
// succeeded. These tests pin classifyWriteDisposition (the pure decision) and
// drive the production handler (handleJetStreamWriteRequest ->
// processWriteMessage -> settleJetStreamWrite) with a fake jetstream.Msg and a
// fake ObjectStore backend to prove each class reaches the right ack verb.

func TestClassifyWriteDisposition(t *testing.T) {
	t.Parallel()

	cancelled := context.Canceled

	tests := []struct {
		name    string
		ctxErr  error
		procErr error
		want    writeDisposition
	}{
		{"nil error acks", nil, nil, dispositionAck},
		{"nil error acks even under cancellation", cancelled, nil, dispositionAck},
		{
			"invalid terms",
			nil,
			errs.WrapInvalid(errors.New("bad entity id"), "Store", "StoreContent", "validate entity ID"),
			dispositionTerm,
		},
		{
			"transient naks with transient delay",
			nil,
			errs.WrapTransient(errors.New("backend down"), "Store", "Store", "store message"),
			dispositionNakTransient,
		},
		{
			"unclassified naks with transient delay (retry is the safe default)",
			nil,
			errors.New("kaboom"),
			dispositionNakTransient,
		},
		{
			"cancellation owns a transient error (shutdown nak)",
			cancelled,
			errs.WrapTransient(context.Canceled, "Store", "Store", "store message"),
			dispositionNakShutdown,
		},
		{
			// select-race-on-pre-cancelled-ctx discipline: an error observed
			// under a cancelled context must NEVER Term — the invalid class may
			// be an artifact of the shutdown, and Term permanently drops the
			// delivery.
			"cancellation owns even an invalid-classified error (must not term)",
			cancelled,
			errs.WrapInvalid(errors.New("bad entity id"), "Store", "StoreContent", "validate entity ID"),
			dispositionNakShutdown,
		},
		{
			"deadline exceeded owns the outcome (shutdown nak)",
			context.DeadlineExceeded,
			errs.WrapTransient(context.DeadlineExceeded, "Store", "Store", "store message"),
			dispositionNakShutdown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, classifyWriteDisposition(tt.ctxErr, tt.procErr))
		})
	}
}

// ackRecordingMsg is a fake jetstream.Msg recording which ack verb the handler
// applied. Only the methods the write handler touches are implemented; any
// other call panics via the embedded nil interface (loud, not silent).
type ackRecordingMsg struct {
	jetstream.Msg
	data []byte

	acks      int
	terms     int
	nakDelays []time.Duration
}

func (m *ackRecordingMsg) Data() []byte    { return m.data }
func (m *ackRecordingMsg) Subject() string { return "unit.write" }
func (m *ackRecordingMsg) Ack() error      { m.acks++; return nil }
func (m *ackRecordingMsg) Term() error     { m.terms++; return nil }
func (m *ackRecordingMsg) NakWithDelay(delay time.Duration) error {
	m.nakDelays = append(m.nakDelays, delay)
	return nil
}

// fakeObjectStore injects a PutBytes outcome underneath the REAL Store type so
// handleJetStreamWriteRequest runs the production processWriteMessage path
// (decode attempt -> raw-bytes Store fallback) against a controlled backend.
type fakeObjectStore struct {
	jetstream.ObjectStore
	putErr error
}

func (f *fakeObjectStore) PutBytes(context.Context, string, []byte) (*jetstream.ObjectInfo, error) {
	if f.putErr != nil {
		return nil, f.putErr
	}
	return &jetstream.ObjectInfo{}, nil
}

// newAckTestComponent builds a Component whose store writes to the fake
// backend. No ports are configured, so the "stored" emit is a by-design skip;
// the decoder has no registry, so every payload takes the raw Store path.
func newAckTestComponent(putErr error) *Component {
	return &Component{
		instanceName: "unit",
		decoder:      message.NewDecoder(nil),
		logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		store: &Store{
			bucketName:   "UNIT",
			instanceName: "unit",
			store:        &fakeObjectStore{putErr: putErr},
			keyGenerator: &DefaultKeyGenerator{},
		},
	}
}

func TestHandleJetStreamWriteRequest_SuccessAcks(t *testing.T) {
	c := newAckTestComponent(nil)
	msg := &ackRecordingMsg{data: []byte(`{"raw":"payload"}`)}

	c.handleJetStreamWriteRequest(context.Background(), msg)

	assert.Equal(t, 1, msg.acks, "successful store must positively ack")
	assert.Zero(t, msg.terms)
	assert.Empty(t, msg.nakDelays)
}

func TestHandleJetStreamWriteRequest_TransientStoreFailureNaks(t *testing.T) {
	c := newAckTestComponent(errors.New("nats: insufficient storage"))
	msg := &ackRecordingMsg{data: []byte(`{"raw":"payload"}`)}

	c.handleJetStreamWriteRequest(context.Background(), msg)

	assert.Zero(t, msg.acks, "#727: a failed store must NOT be acked — that permanently loses the message")
	assert.Zero(t, msg.terms)
	require.Len(t, msg.nakDelays, 1)
	assert.Equal(t, transientNakDelay, msg.nakDelays[0],
		"transient failures use the 30s heartbeat-precedent delay")
}

func TestHandleJetStreamWriteRequest_CancelledContextNaksWithShutdownDelay(t *testing.T) {
	c := newAckTestComponent(context.Canceled)
	msg := &ackRecordingMsg{data: []byte(`{"raw":"payload"}`)}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancelled: the shutdown race shape

	c.handleJetStreamWriteRequest(ctx, msg)

	assert.Zero(t, msg.acks)
	assert.Zero(t, msg.terms, "cancellation must never Term (would drop a retryable delivery)")
	require.Len(t, msg.nakDelays, 1)
	assert.Equal(t, shutdownNakDelay, msg.nakDelays[0],
		"shutdown uses the 5s heartbeat-precedent delay")
}

// recordingLogHandler captures slog records so tests can pin log LEVELS.
// Instance-local (wired into one Component's logger) — it never touches
// slog.SetDefault or any process-global logger state.
type recordingLogHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *recordingLogHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *recordingLogHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r)
	return nil
}
func (h *recordingLogHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingLogHandler) WithGroup(string) slog.Handler      { return h }

func (h *recordingLogHandler) countAtLevel(level slog.Level) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, r := range h.records {
		if r.Level == level {
			n++
		}
	}
	return n
}

// TestSettleJetStreamWrite_ShutdownNakLogLevels pins the OBSERVABILITY split
// inside the NakShutdown disposition: a per-message processing DEADLINE expiry
// (hung backend — the message can park un-acked once MaxDeliver exhausts) must
// log at ERROR, while genuine shutdown CANCELLATION stays Debug. The NAK verb
// and 5s delay are identical for both causes — only the log level differs.
// Regressing the DeadlineExceeded record to Debug fails this test.
func TestSettleJetStreamWrite_ShutdownNakLogLevels(t *testing.T) {
	procErr := errs.WrapTransient(errors.New("put bytes stalled"), "Store", "Store", "store message")

	t.Run("deadline exceeded logs ERROR", func(t *testing.T) {
		h := &recordingLogHandler{}
		c := newAckTestComponent(nil)
		c.logger = slog.New(h)
		msg := &ackRecordingMsg{data: []byte(`{"raw":"payload"}`)}

		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		defer cancel()
		require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)

		c.settleJetStreamWrite(ctx, msg, procErr)

		require.Len(t, msg.nakDelays, 1)
		assert.Equal(t, shutdownNakDelay, msg.nakDelays[0])
		assert.Zero(t, msg.acks)
		assert.Zero(t, msg.terms)
		assert.Equal(t, 1, h.countAtLevel(slog.LevelError),
			"a hung-backend deadline expiry is a real failure and must be loud (ERROR)")
	})

	t.Run("cancellation logs Debug only", func(t *testing.T) {
		h := &recordingLogHandler{}
		c := newAckTestComponent(nil)
		c.logger = slog.New(h)
		msg := &ackRecordingMsg{data: []byte(`{"raw":"payload"}`)}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		require.ErrorIs(t, ctx.Err(), context.Canceled)

		c.settleJetStreamWrite(ctx, msg, procErr)

		require.Len(t, msg.nakDelays, 1)
		assert.Equal(t, shutdownNakDelay, msg.nakDelays[0])
		assert.Zero(t, msg.acks)
		assert.Zero(t, msg.terms)
		assert.Zero(t, h.countAtLevel(slog.LevelError),
			"genuine shutdown must not spam ERROR")
		assert.Equal(t, 1, h.countAtLevel(slog.LevelDebug))
	})
}

// TestSettleJetStreamWrite_InvalidTerms drives the Term branch through the
// production settle seam with the real classified error shape the store
// returns for an entity-ID contract violation. (Producing that error through
// handleJetStreamWriteRequest needs a registered ContentStorable payload —
// covered by the integration test; the disposition wiring is identical.)
func TestSettleJetStreamWrite_InvalidTerms(t *testing.T) {
	c := newAckTestComponent(nil)
	msg := &ackRecordingMsg{data: []byte(`{"raw":"payload"}`)}

	procErr := errs.WrapInvalid(errors.New("invalid entity ID contract input"),
		"Store", "StoreContent", "validate entity ID")
	c.settleJetStreamWrite(context.Background(), msg, procErr)

	assert.Zero(t, msg.acks, "invalid payloads must not be acked as if stored")
	assert.Empty(t, msg.nakDelays, "invalid payloads must not be retried (poison loop)")
	assert.Equal(t, 1, msg.terms, "structurally invalid payloads terminate the delivery")
}
