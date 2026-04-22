package logging

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// newTestCounter returns a fresh CounterVec on an isolated registry so
// parallel tests don't collide on the default Prometheus registry.
func newTestCounter(t *testing.T) *prometheus.CounterVec {
	t.Helper()
	cv := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "test",
			Subsystem: "log",
			Name:      "entries_total",
		},
		[]string{"component", "level"},
	)
	return cv
}

// readCounter returns the current value for the given (component, level)
// combination, or 0 if the combination has not been observed yet.
func readCounter(t *testing.T, cv *prometheus.CounterVec, component, level string) float64 {
	t.Helper()
	c, err := cv.GetMetricWithLabelValues(component, level)
	if err != nil {
		t.Fatalf("GetMetricWithLabelValues(%q, %q): %v", component, level, err)
	}
	var m dto.Metric
	if err := c.Write(&m); err != nil {
		t.Fatalf("Write metric: %v", err)
	}
	return m.GetCounter().GetValue()
}

func TestCounterHandler_Enabled(t *testing.T) {
	h := NewCounterHandler(newTestCounter(t))
	cases := []struct {
		level slog.Level
		want  bool
	}{
		{slog.LevelDebug, false},
		{slog.LevelInfo, false},
		{slog.LevelWarn, true},
		{slog.LevelError, true},
	}
	for _, tc := range cases {
		if got := h.Enabled(context.Background(), tc.level); got != tc.want {
			t.Errorf("Enabled(%v) = %v, want %v", tc.level, got, tc.want)
		}
	}
}

func TestCounterHandler_HandleIncrementsCorrectLabel(t *testing.T) {
	cv := newTestCounter(t)
	h := NewCounterHandler(cv)

	rec := slog.NewRecord(time.Now(), slog.LevelWarn, "broke", 0)
	rec.AddAttrs(slog.String("component", "udp-input"))

	if err := h.Handle(context.Background(), rec); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := readCounter(t, cv, "udp-input", "warn"); got != 1 {
		t.Errorf("counter[udp-input,warn] = %v, want 1", got)
	}
}

func TestCounterHandler_MissingComponentIsUnknown(t *testing.T) {
	cv := newTestCounter(t)
	h := NewCounterHandler(cv)

	rec := slog.NewRecord(time.Now(), slog.LevelError, "boom", 0)
	if err := h.Handle(context.Background(), rec); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := readCounter(t, cv, unknownComponent, "error"); got != 1 {
		t.Errorf("counter[unknown,error] = %v, want 1", got)
	}
}

// TestCounterHandler_WithAttrsChain verifies the common production pattern:
// a package binds the component once via logger.With(...) and every
// subsequent Warn/Error call inherits that label through the handler chain.
func TestCounterHandler_WithAttrsChain(t *testing.T) {
	cv := newTestCounter(t)
	h := NewCounterHandler(cv)

	bound := h.WithAttrs([]slog.Attr{slog.String("component", "agentic-loop")})

	rec := slog.NewRecord(time.Now(), slog.LevelWarn, "retry exhausted", 0)
	if err := bound.Handle(context.Background(), rec); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := readCounter(t, cv, "agentic-loop", "warn"); got != 1 {
		t.Errorf("counter[agentic-loop,warn] = %v, want 1", got)
	}
}

// TestCounterHandler_InlineOverridesChain documents precedence: an inline
// slog.String("component", ...) at the call site wins over a component
// bound earlier via WithAttrs. Matches "most specific caller wins."
func TestCounterHandler_InlineOverridesChain(t *testing.T) {
	cv := newTestCounter(t)
	h := NewCounterHandler(cv)
	bound := h.WithAttrs([]slog.Attr{slog.String("component", "chain-X")})

	rec := slog.NewRecord(time.Now(), slog.LevelWarn, "msg", 0)
	rec.AddAttrs(slog.String("component", "inline-Y"))

	if err := bound.Handle(context.Background(), rec); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := readCounter(t, cv, "inline-Y", "warn"); got != 1 {
		t.Errorf("counter[inline-Y,warn] = %v, want 1 (inline should win)", got)
	}
	if got := readCounter(t, cv, "chain-X", "warn"); got != 0 {
		t.Errorf("counter[chain-X,warn] = %v, want 0 (chain should lose to inline)", got)
	}
}

// recordingHandler captures every record its Handle() method sees. Used to
// prove CounterHandler is pass-through when composed under MultiHandler —
// the counter fires AND the output handlers still see the record.
type recordingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}
func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

func (h *recordingHandler) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.records)
}

// TestCounterHandler_PassThroughInMultiHandler confirms the counter doesn't
// swallow records — the real output handler still observes every Warn+
// record while the counter increments in parallel.
func TestCounterHandler_PassThroughInMultiHandler(t *testing.T) {
	cv := newTestCounter(t)
	counter := NewCounterHandler(cv)
	recorder := &recordingHandler{}
	multi := NewMultiHandler(counter, recorder)

	logger := slog.New(multi).With("component", "test-svc")
	logger.Warn("w1")
	logger.Error("e1")
	logger.Info("skipped") // below Warn threshold for the counter

	if got := readCounter(t, cv, "test-svc", "warn"); got != 1 {
		t.Errorf("counter[test-svc,warn] = %v, want 1", got)
	}
	if got := readCounter(t, cv, "test-svc", "error"); got != 1 {
		t.Errorf("counter[test-svc,error] = %v, want 1", got)
	}
	// recordingHandler accepts all levels, so it sees all 3.
	if got := recorder.count(); got != 3 {
		t.Errorf("recordingHandler saw %d records, want 3 (counter must not drop records for other handlers)", got)
	}
}

func TestCounterHandler_NilCounterIsNoop(t *testing.T) {
	h := NewCounterHandler(nil)
	rec := slog.NewRecord(time.Now(), slog.LevelWarn, "msg", 0)
	rec.AddAttrs(slog.String("component", "x"))
	if err := h.Handle(context.Background(), rec); err != nil {
		t.Errorf("Handle with nil counter should return nil, got %v", err)
	}
}

func TestLevelString(t *testing.T) {
	cases := []struct {
		in   slog.Level
		want string
	}{
		{slog.LevelWarn, "warn"},
		{slog.LevelError, "error"},
		{slog.LevelError + 4, "error"}, // Fatal-ish custom level clamps to error
		{slog.LevelInfo, slog.LevelInfo.String()},
	}
	for _, tc := range cases {
		if got := levelString(tc.in); got != tc.want {
			t.Errorf("levelString(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
