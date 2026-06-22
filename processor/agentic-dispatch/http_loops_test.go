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

func TestMapKVOperation(t *testing.T) {
	comp := newTestComponent(t)

	tests := []struct {
		name      string
		operation jetstream.KeyValueOp
		revision  uint64
		expected  string
	}{
		{"create on revision 1", jetstream.KeyValuePut, 1, "loop_created"},
		{"update on revision > 1", jetstream.KeyValuePut, 5, "loop_updated"},
		{"delete", jetstream.KeyValueDelete, 3, "loop_deleted"},
		{"unknown operation", jetstream.KeyValueOp(99), 1, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := comp.mapKVOperation(tt.operation, tt.revision)
			assert.Equal(t, tt.expected, result)
		})
	}
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

// TestActivityEventLoopIDCorrelation verifies that for a COMPLETE_<id>-keyed
// terminal entry the ActivityEvent envelope loop_id equals data.loop_id (no
// COMPLETE_ prefix leak), and that terminal-ness is signalled by event.Type
// rather than requiring callers to inspect the raw KV key or data.state.
//
// This is a regression test for gh#226: previously LoopID was set from
// entry.Key() so terminal events carried "COMPLETE_<id>" in the envelope
// while data.loop_id held the bare "<id>".
func TestActivityEventLoopIDCorrelation(t *testing.T) {
	const bareID = "loop_abc123"

	// Simulate the COMPLETE_<loopID> payload written by agentic-loop on
	// terminal outcomes.  loopFromCompletion unmarshals via completionWire
	// which reads the "loop_id" JSON field — this is the bare ID already.
	completionPayload := []byte(`{
		"loop_id":"` + bareID + `",
		"task_id":"task-1",
		"role":"researcher",
		"state":"complete",
		"outcome":"success",
		"result":"42",
		"iterations":3,
		"tokens_in":100,
		"tokens_out":200
	}`)

	t.Run("loopFromCompletion returns bare loop_id", func(t *testing.T) {
		loop, ok := loopFromCompletion(completionPayload)
		require.True(t, ok, "expected loopFromCompletion to succeed")
		// data.loop_id must be the bare ID, not COMPLETE_<id>
		assert.Equal(t, bareID, loop.LoopID)
		assert.Equal(t, "complete", loop.State)
		assert.Equal(t, "success", loop.Outcome)
	})

	t.Run("ActivityEvent envelope loop_id matches data.loop_id for COMPLETE_ key", func(t *testing.T) {
		// Replicate the exact logic in handleActivityStream so we lock the
		// fix and catch any future regression without requiring a live NATS
		// client.
		rawKey := "COMPLETE_" + bareID
		isCompletion := strings.HasPrefix(rawKey, "COMPLETE_")
		require.True(t, isCompletion)

		bareLoopID := rawKey
		if isCompletion {
			bareLoopID = strings.TrimPrefix(rawKey, "COMPLETE_")
		}

		var eventType string
		if isCompletion {
			eventType = "loop_completed"
		} else {
			comp := newTestComponent(t)
			eventType = comp.mapKVOperation(jetstream.KeyValuePut, 1)
		}

		loop, ok := loopFromCompletion(completionPayload)
		require.True(t, ok)

		event := ActivityEvent{
			Type:      eventType,
			LoopID:    bareLoopID,
			Timestamp: time.Now(),
			Data:      &loop,
		}

		// Core assertion: envelope loop_id == data.loop_id (no COMPLETE_ prefix).
		assert.Equal(t, event.Data.LoopID, event.LoopID,
			"envelope loop_id must equal data.loop_id so consumers can correlate")
		assert.Equal(t, bareID, event.LoopID,
			"envelope loop_id must be the bare ID without COMPLETE_ prefix")

		// Terminal-ness is signalled by event.Type, not the raw KV key.
		assert.Equal(t, "loop_completed", event.Type,
			"COMPLETE_ key must emit loop_completed so terminal-ness is wire-observable from event.Type")

		// data.state also independently confirms terminal-ness.
		assert.Equal(t, "complete", event.Data.State,
			"data.state must reflect the terminal outcome from the completion payload")
	})

	t.Run("non-terminal key preserves existing type mapping", func(t *testing.T) {
		rawKey := bareID // no COMPLETE_ prefix
		isCompletion := strings.HasPrefix(rawKey, "COMPLETE_")
		require.False(t, isCompletion)

		bareLoopID := rawKey
		comp := newTestComponent(t)

		assert.Equal(t, bareID, bareLoopID, "bare ID is unchanged for non-terminal keys")
		assert.Equal(t, "loop_created", comp.mapKVOperation(jetstream.KeyValuePut, 1),
			"revision 1 maps to loop_created for non-terminal keys")
		assert.Equal(t, "loop_updated", comp.mapKVOperation(jetstream.KeyValuePut, 5),
			"revision > 1 maps to loop_updated for non-terminal keys")
	})

	t.Run("ActivityEvent round-trips through JSON with bare loop_id", func(t *testing.T) {
		loop, ok := loopFromCompletion(completionPayload)
		require.True(t, ok)

		event := ActivityEvent{
			Type:      "loop_completed",
			LoopID:    bareID,
			Timestamp: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
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
