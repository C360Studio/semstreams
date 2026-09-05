package rule

import (
	"context"
	"log/slog"
	"sync"
)

// The slog capture used by the tagged run-scope tests and the untagged skip-reason
// test alike. It lives in this untagged file for the same reason
// foreignFiringSkipTestMetrics does (actions_test.go): both suites assert the
// same operator surface — the one Info line per declined dispatch — and a
// helper that only compiles under the integration tag would leave the unit test
// asserting it a second way.

// capturedRecord is one slog record as an OPERATOR would read it: the level, the
// message, and the attributes flattened to strings.
type capturedRecord struct {
	level slog.Level
	msg   string
	attrs map[string]string
}

// capturingHandler records what the executor logged. The log is a promised
// operator surface here ("ONE Info log per dispatch naming EVERY write that dispatch skipped"), so it
// is asserted like any other output rather than discarded. Locking keeps -race
// clean regardless of who calls the logger.
type capturingHandler struct {
	mu      sync.Mutex
	records []capturedRecord
}

func (h *capturingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *capturingHandler) Handle(_ context.Context, r slog.Record) error {
	captured := capturedRecord{level: r.Level, msg: r.Message, attrs: make(map[string]string)}
	r.Attrs(func(a slog.Attr) bool {
		captured.attrs[a.Key] = a.Value.String()
		return true
	})
	h.mu.Lock()
	h.records = append(h.records, captured)
	h.mu.Unlock()
	return nil
}

func (h *capturingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *capturingHandler) WithGroup(string) slog.Handler      { return h }

// withMessage returns every record carrying exactly this message.
func (h *capturingHandler) withMessage(msg string) []capturedRecord {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]capturedRecord, 0, len(h.records))
	for _, record := range h.records {
		if record.msg == msg {
			out = append(out, record)
		}
	}
	return out
}
