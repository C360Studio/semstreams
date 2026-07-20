package agenticdispatch

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestComponent creates a minimal Component for testing HTTP handlers
func newTestComponent(t *testing.T) *Component {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &Component{
		config:      DefaultConfig(),
		loopTracker: NewLoopTrackerWithLogger(logger),
		registry:    NewCommandRegistry(),
		logger:      logger,
		metrics:     getMetrics(nil), // Use default metrics for tests
		natsClient:  nil,             // Will be nil for unit tests
	}
}

func TestHandleListLoops(t *testing.T) {
	comp := newTestComponent(t)

	// Add some test loops
	comp.loopTracker.Track(&LoopInfo{
		LoopID:      "loop-1",
		TaskID:      "task-1",
		UserID:      "user-1",
		ChannelType: "http",
		ChannelID:   "chan-1",
		State:       "executing",
		Iterations:  3,
		CreatedAt:   time.Now(),
	})
	comp.loopTracker.Track(&LoopInfo{
		LoopID:      "loop-2",
		TaskID:      "task-2",
		UserID:      "user-2",
		ChannelType: "http",
		ChannelID:   "chan-2",
		State:       "pending",
		CreatedAt:   time.Now(),
	})

	t.Run("list all loops", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/loops", nil)
		rec := httptest.NewRecorder()

		comp.handleListLoops(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

		var loops []*LoopInfo
		err := json.Unmarshal(rec.Body.Bytes(), &loops)
		require.NoError(t, err)
		assert.Len(t, loops, 2)
	})

	t.Run("filter by user_id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/loops?user_id=user-1", nil)
		rec := httptest.NewRecorder()

		comp.handleListLoops(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var loops []*LoopInfo
		err := json.Unmarshal(rec.Body.Bytes(), &loops)
		require.NoError(t, err)
		assert.Len(t, loops, 1)
		assert.Equal(t, "user-1", loops[0].UserID)
	})

	t.Run("filter by state", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/loops?state=pending", nil)
		rec := httptest.NewRecorder()

		comp.handleListLoops(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var loops []*LoopInfo
		err := json.Unmarshal(rec.Body.Bytes(), &loops)
		require.NoError(t, err)
		assert.Len(t, loops, 1)
		assert.Equal(t, "pending", loops[0].State)
	})

	t.Run("filter by user_id and state", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/loops?user_id=user-1&state=executing", nil)
		rec := httptest.NewRecorder()

		comp.handleListLoops(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var loops []*LoopInfo
		err := json.Unmarshal(rec.Body.Bytes(), &loops)
		require.NoError(t, err)
		assert.Len(t, loops, 1)
		assert.Equal(t, "user-1", loops[0].UserID)
		assert.Equal(t, "executing", loops[0].State)
	})

	t.Run("empty result with non-matching filter", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/loops?user_id=nonexistent", nil)
		rec := httptest.NewRecorder()

		comp.handleListLoops(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var loops []*LoopInfo
		err := json.Unmarshal(rec.Body.Bytes(), &loops)
		require.NoError(t, err)
		assert.Len(t, loops, 0)
	})
}

func TestHandleGetLoop(t *testing.T) {
	comp := newTestComponent(t)

	// Add a test loop
	comp.loopTracker.Track(&LoopInfo{
		LoopID:        "loop-1",
		TaskID:        "task-1",
		UserID:        "user-1",
		ChannelType:   "http",
		ChannelID:     "chan-1",
		State:         "executing",
		Iterations:    3,
		MaxIterations: 10,
		CreatedAt:     time.Now(),
	})

	t.Run("get existing loop", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/loops/loop-1", nil)
		req.SetPathValue("id", "loop-1")
		rec := httptest.NewRecorder()

		comp.handleGetLoop(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

		var loop LoopInfo
		err := json.Unmarshal(rec.Body.Bytes(), &loop)
		require.NoError(t, err)
		assert.Equal(t, "loop-1", loop.LoopID)
		assert.Equal(t, "task-1", loop.TaskID)
		assert.Equal(t, "executing", loop.State)
		assert.Equal(t, 3, loop.Iterations)
	})

	t.Run("loop not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/loops/nonexistent", nil)
		req.SetPathValue("id", "nonexistent")
		rec := httptest.NewRecorder()

		comp.handleGetLoop(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)

		var resp HTTPMessageResponse
		err := json.Unmarshal(rec.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, "error", resp.Type)
		assert.Contains(t, resp.Content, "not found")
	})

	t.Run("missing loop ID", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/loops/", nil)
		req.SetPathValue("id", "")
		rec := httptest.NewRecorder()

		comp.handleGetLoop(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

func TestHandleLoopSignal(t *testing.T) {
	comp := newTestComponent(t)

	// Add a test loop
	comp.loopTracker.Track(&LoopInfo{
		LoopID:      "loop-1",
		TaskID:      "task-1",
		UserID:      "user-1",
		ChannelType: "http",
		ChannelID:   "chan-1",
		State:       "executing",
		CreatedAt:   time.Now(),
	})

	t.Run("loop not found", func(t *testing.T) {
		body := `{"type":"cancel","reason":"test"}`
		req := httptest.NewRequest(http.MethodPost, "/loops/nonexistent/signal", strings.NewReader(body))
		req.SetPathValue("id", "nonexistent")
		rec := httptest.NewRecorder()

		comp.handleLoopSignal(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("invalid request body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/loops/loop-1/signal", strings.NewReader("invalid json"))
		req.SetPathValue("id", "loop-1")
		rec := httptest.NewRecorder()

		comp.handleLoopSignal(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("invalid signal type", func(t *testing.T) {
		body := `{"type":"invalid","reason":"test"}`
		req := httptest.NewRequest(http.MethodPost, "/loops/loop-1/signal", strings.NewReader(body))
		req.SetPathValue("id", "loop-1")
		rec := httptest.NewRecorder()

		comp.handleLoopSignal(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)

		var resp HTTPMessageResponse
		err := json.Unmarshal(rec.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Contains(t, resp.Content, "invalid signal type")
	})

	t.Run("missing loop ID", func(t *testing.T) {
		body := `{"type":"cancel"}`
		req := httptest.NewRequest(http.MethodPost, "/loops//signal", strings.NewReader(body))
		req.SetPathValue("id", "")
		rec := httptest.NewRecorder()

		comp.handleLoopSignal(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

func TestSignalRequestValidation(t *testing.T) {
	tests := []struct {
		name         string
		signal       string
		expectBadReq bool // Expect 400 Bad Request (validation error)
		expectIntErr bool // Expect 500 Internal Error (NATS error, meaning validation passed)
	}{
		{"pause is valid", "pause", false, true},       // Valid signal, but no NATS client
		{"resume is valid", "resume", false, true},     // Valid signal, but no NATS client
		{"cancel is valid", "cancel", false, true},     // Valid signal, but no NATS client
		{"empty is invalid", "", true, false},          // Validation fails
		{"unknown is invalid", "stop", true, false},    // Validation fails
		{"uppercase is invalid", "PAUSE", true, false}, // Validation fails
	}

	comp := newTestComponent(t)
	comp.loopTracker.Track(&LoopInfo{
		LoopID: "loop-1",
		State:  "executing",
	})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `{"type":"` + tt.signal + `"}`
			req := httptest.NewRequest(http.MethodPost, "/loops/loop-1/signal", strings.NewReader(body))
			req.SetPathValue("id", "loop-1")
			rec := httptest.NewRecorder()

			comp.handleLoopSignal(rec, req)

			if tt.expectBadReq {
				assert.Equal(t, http.StatusBadRequest, rec.Code)
			} else if tt.expectIntErr {
				// Valid signals will fail with 500 due to no NATS client in test
				// but this proves validation passed
				assert.Equal(t, http.StatusInternalServerError, rec.Code)
			}
		})
	}
}

func TestHandleActivityStream_NoClient(t *testing.T) {
	// The activity stream requires a NATS client with a KV bucket.
	// Full SSE flow testing requires integration tests.
	// Skip actual execution - NATS client is nil and would panic
	// Full SSE testing should be done in integration tests with real NATS
	t.Skip("Requires integration test with real NATS infrastructure")
}

func TestActivityEventSerialization(t *testing.T) {
	loop := &Loop{
		LoopID: "loop-123",
		State:  "pending",
	}
	event := ActivityEvent{
		Type:      "loop_created",
		LoopID:    "loop-123",
		Timestamp: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		Data:      loop,
	}

	data, err := json.Marshal(event)
	require.NoError(t, err)

	var decoded ActivityEvent
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, "loop_created", decoded.Type)
	assert.Equal(t, "loop-123", decoded.LoopID)
	require.NotNil(t, decoded.Data)
	assert.Equal(t, "loop-123", decoded.Data.LoopID)
	assert.Equal(t, "pending", decoded.Data.State)
}

// TestActivityEventTypeAndID table-tests the activityEventTypeAndID pure
// function — the single decision point for the isCompletion / bareLoopID /
// eventType triple that handleActivityStream emits onto the /activity wire.
// Locking the function prevents handler refactors from silently regressing
// the correlation without a failing test.
func TestActivityEventTypeAndID(t *testing.T) {
	const bare = "loop_abc123"

	cases := []struct {
		name       string
		key        string
		op         jetstream.KeyValueOp
		revision   uint64
		wantType   string
		wantLoopID string
	}{
		{
			name:       "COMPLETE_ key always emits loop_completed with bare id",
			key:        "COMPLETE_" + bare,
			op:         jetstream.KeyValuePut,
			revision:   1,
			wantType:   "loop_completed",
			wantLoopID: bare,
		},
		{
			name:       "COMPLETE_ key revision > 1 still emits loop_completed",
			key:        "COMPLETE_" + bare,
			op:         jetstream.KeyValuePut,
			revision:   5,
			wantType:   "loop_completed",
			wantLoopID: bare,
		},
		{
			name:       "non-terminal put revision 1 emits loop_created",
			key:        bare,
			op:         jetstream.KeyValuePut,
			revision:   1,
			wantType:   "loop_created",
			wantLoopID: bare,
		},
		{
			name:       "non-terminal put revision > 1 emits loop_updated",
			key:        bare,
			op:         jetstream.KeyValuePut,
			revision:   7,
			wantType:   "loop_updated",
			wantLoopID: bare,
		},
		{
			name:       "non-terminal delete emits loop_deleted",
			key:        bare,
			op:         jetstream.KeyValueDelete,
			revision:   3,
			wantType:   "loop_deleted",
			wantLoopID: bare,
		},
		{
			// Folded from the retired mapKVOperation (production-dead helper
			// deleted; activityEventTypeAndID owns the mapping).
			name:       "unknown operation emits unknown",
			key:        bare,
			op:         jetstream.KeyValueOp(99),
			revision:   1,
			wantType:   "unknown",
			wantLoopID: bare,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotType, gotLoopID := activityEventTypeAndID(tc.key, tc.op, tc.revision)
			assert.Equal(t, tc.wantType, gotType, "event type")
			assert.Equal(t, tc.wantLoopID, gotLoopID, "bare loop ID")
		})
	}
}

// TestActivityEventLoopIDCorrelation verifies that for a COMPLETE_<id>-keyed
// terminal entry the ActivityEvent envelope loop_id equals data.loop_id (no
// COMPLETE_ prefix leak), and that terminal-ness is signalled by event.Type.
// Tests drive the PRODUCTION decoder (loopFromCompletion) against real
// agentic event types — not synthetic JSON shapes — to lock the wire contract.
//
// This is a regression test for gh#226: previously LoopID was set from
// entry.Key() so terminal events carried "COMPLETE_<id>" in the envelope
// while data.loop_id held the bare "<id>".
func TestActivityEventLoopIDCorrelation(t *testing.T) {
	const bareID = "loop_abc123"
	const taskID = "task-1"
	ts := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	// marshalEvent marshals a real agentic terminal event via its own
	// MarshalJSON method — this is the payload agentic-loop actually writes
	// to the COMPLETE_<id> KV key.
	marshalCompleted := func(t *testing.T) []byte {
		t.Helper()
		ev := &agentic.LoopCompletedEvent{
			LoopID:      bareID,
			TaskID:      taskID,
			Role:        "researcher",
			Outcome:     agentic.OutcomeSuccess,
			Result:      "42",
			Iterations:  3,
			TokensIn:    100,
			TokensOut:   200,
			CompletedAt: ts,
		}
		b, err := json.Marshal(ev)
		require.NoError(t, err)
		return b
	}

	marshalFailed := func(t *testing.T) []byte {
		t.Helper()
		ev := &agentic.LoopFailedEvent{
			LoopID:     bareID,
			TaskID:     taskID,
			Role:       "researcher",
			Outcome:    agentic.OutcomeFailed,
			Reason:     "tool_error",
			Error:      "dial timeout",
			Iterations: 2,
			FailedAt:   ts,
		}
		b, err := json.Marshal(ev)
		require.NoError(t, err)
		return b
	}

	marshalCancelled := func(t *testing.T) []byte {
		t.Helper()
		ev := &agentic.LoopCancelledEvent{
			LoopID:      bareID,
			TaskID:      taskID,
			Outcome:     agentic.OutcomeCancelled,
			CancelledBy: "user",
			CancelledAt: ts,
		}
		b, err := json.Marshal(ev)
		require.NoError(t, err)
		return b
	}

	t.Run("LoopCompletedEvent: loopFromCompletion decodes outcome, no state", func(t *testing.T) {
		payload := marshalCompleted(t)
		loop, ok := loopFromCompletion(payload)
		require.True(t, ok, "loopFromCompletion must succeed on a real LoopCompletedEvent")
		assert.Equal(t, bareID, loop.LoopID)
		// outcome must carry the verdict
		assert.Equal(t, agentic.OutcomeSuccess, loop.Outcome)
		// state is NOT populated by production terminal events — the field is absent
		// from LoopCompletedEvent/LoopFailedEvent/LoopCancelledEvent.
		assert.Empty(t, loop.State,
			"data.state must be empty for production terminal events (no state field on the event type)")
	})

	t.Run("LoopFailedEvent: loopFromCompletion decodes outcome, no state", func(t *testing.T) {
		payload := marshalFailed(t)
		loop, ok := loopFromCompletion(payload)
		require.True(t, ok)
		assert.Equal(t, bareID, loop.LoopID)
		assert.Equal(t, agentic.OutcomeFailed, loop.Outcome)
		assert.Empty(t, loop.State)
	})

	t.Run("LoopCancelledEvent: loopFromCompletion decodes outcome, no state", func(t *testing.T) {
		payload := marshalCancelled(t)
		loop, ok := loopFromCompletion(payload)
		require.True(t, ok)
		assert.Equal(t, bareID, loop.LoopID)
		assert.Equal(t, agentic.OutcomeCancelled, loop.Outcome)
		assert.Empty(t, loop.State)
	})

	t.Run("COMPLETE_ key: envelope loop_id == data.loop_id, type == loop_completed", func(t *testing.T) {
		payload := marshalCompleted(t)
		rawKey := "COMPLETE_" + bareID

		// Drive through the production function, not a reimplementation.
		eventType, bareLoopID := activityEventTypeAndID(rawKey, jetstream.KeyValuePut, 1)
		assert.Equal(t, "loop_completed", eventType)
		assert.Equal(t, bareID, bareLoopID)

		loop, ok := loopFromCompletion(payload)
		require.True(t, ok)

		event := ActivityEvent{
			Type:      eventType,
			LoopID:    bareLoopID,
			Timestamp: ts,
			Data:      &loop,
		}

		assert.Equal(t, event.Data.LoopID, event.LoopID,
			"envelope loop_id must equal data.loop_id so consumers can correlate")
		assert.Equal(t, bareID, event.LoopID,
			"envelope loop_id must be the bare ID without COMPLETE_ prefix")
		assert.Equal(t, "loop_completed", event.Type,
			"COMPLETE_ key must emit loop_completed so terminal-ness is wire-observable from event.Type")
		// data.outcome carries the verdict; data.state is absent on real terminal events.
		assert.Equal(t, agentic.OutcomeSuccess, event.Data.Outcome,
			"data.outcome must carry the terminal verdict")
		assert.Empty(t, event.Data.State,
			"data.state must be empty — production terminal events have no state field")
	})

	t.Run("ActivityEvent round-trips through JSON with bare loop_id and outcome", func(t *testing.T) {
		payload := marshalCompleted(t)
		loop, ok := loopFromCompletion(payload)
		require.True(t, ok)

		event := ActivityEvent{
			Type:      "loop_completed",
			LoopID:    bareID,
			Timestamp: ts,
			Data:      &loop,
		}

		data, err := json.Marshal(event)
		require.NoError(t, err)

		var decoded ActivityEvent
		err = json.Unmarshal(data, &decoded)
		require.NoError(t, err)

		assert.Equal(t, "loop_completed", decoded.Type)
		assert.Equal(t, bareID, decoded.LoopID)
		require.NotNil(t, decoded.Data)
		assert.Equal(t, bareID, decoded.Data.LoopID,
			"data.loop_id must survive JSON round-trip as bare ID")
		assert.Equal(t, decoded.LoopID, decoded.Data.LoopID,
			"envelope loop_id must equal data.loop_id after JSON round-trip")
		assert.Equal(t, agentic.OutcomeSuccess, decoded.Data.Outcome,
			"data.outcome must survive JSON round-trip")
		assert.Empty(t, decoded.Data.State,
			"data.state must remain empty after JSON round-trip")
	})
}

func TestSignalResponseSerialization(t *testing.T) {
	resp := SignalResponse{
		LoopID:    "loop-123",
		Signal:    "cancel",
		Accepted:  true,
		Message:   "Signal accepted",
		Timestamp: "2024-01-15T10:30:00Z",
	}

	data, err := json.Marshal(resp)
	require.NoError(t, err)

	var decoded SignalResponse
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, "loop-123", decoded.LoopID)
	assert.Equal(t, "cancel", decoded.Signal)
	assert.True(t, decoded.Accepted)
}
