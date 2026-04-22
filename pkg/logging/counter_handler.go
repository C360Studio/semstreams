package logging

import (
	"context"
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"
)

// CounterHandler is an slog.Handler that increments a Prometheus counter
// for every record at WARN or above, keyed by (component, level). It is a
// pass-through: it never emits output, so callers compose it alongside the
// real output handlers (stdout, NATS) in a MultiHandler.
//
// Downstream consumers compute windowed views (1m, 5m) via PromQL's rate()
// or increase() — we do not maintain windowed state in-process.
type CounterHandler struct {
	counter *prometheus.CounterVec
	attrs   []slog.Attr
}

// componentAttrKey is the slog attribute key components use to identify
// themselves (e.g., logger := slog.Default().With("component", "udp-input")).
// Records without this attribute get counted under "unknown".
const componentAttrKey = "component"
const unknownComponent = "unknown"

// NewCounterHandler wraps a Prometheus CounterVec with labels
// ["component", "level"]. A nil counter returns a handler that counts
// nothing — useful for tests that don't care about the counter.
func NewCounterHandler(counter *prometheus.CounterVec) *CounterHandler {
	return &CounterHandler{counter: counter}
}

// Enabled returns true only for WARN and above. Debug/Info traffic skips the
// counter path entirely — keeps the hot log path cheap.
func (h *CounterHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelWarn
}

// Handle increments the counter for the record's (component, level). It
// always returns nil; a failing counter increment must not break the
// logging chain. The precedence for component resolution is: inline record
// attrs first, then chain attrs from WithAttrs, then "unknown" — so a
// deliberate slog.String("component", ...) at the call site overrides the
// logger's pre-bound component.
func (h *CounterHandler) Handle(_ context.Context, r slog.Record) error {
	if h.counter == nil {
		return nil
	}
	component := findComponentInRecord(r)
	if component == "" {
		component = findComponentInAttrs(h.attrs)
	}
	if component == "" {
		component = unknownComponent
	}
	h.counter.WithLabelValues(component, levelString(r.Level)).Inc()
	return nil
}

// WithAttrs clones the handler with additional attrs appended to the chain.
// The chain is consulted by Handle when a record has no inline "component"
// attr — matches the standard slog.Handler contract used by NATSLogHandler.
func (h *CounterHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newAttrs := make([]slog.Attr, len(h.attrs)+len(attrs))
	copy(newAttrs, h.attrs)
	copy(newAttrs[len(h.attrs):], attrs)
	return &CounterHandler{counter: h.counter, attrs: newAttrs}
}

// WithGroup is a noop for this handler: groups namespace attrs visually in
// output handlers, but we only care about the flat "component" key for the
// counter label. Returning the receiver unchanged is safe and matches
// Prometheus-label semantics (no dots/slashes in labels).
func (h *CounterHandler) WithGroup(_ string) slog.Handler {
	return h
}

// findComponentInRecord returns the "component" attr value from the
// record's inline attrs, or "" if absent.
func findComponentInRecord(r slog.Record) string {
	var found string
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == componentAttrKey {
			found = a.Value.String()
			return false
		}
		return true
	})
	return found
}

// findComponentInAttrs returns the "component" attr value from the chain
// (built up via WithAttrs), or "" if absent. The last occurrence wins,
// matching slog's "latest With wins" semantics.
func findComponentInAttrs(attrs []slog.Attr) string {
	var found string
	for _, a := range attrs {
		if a.Key == componentAttrKey {
			found = a.Value.String()
		}
	}
	return found
}

// levelString maps slog.Level to the short label value used on the metric.
// Non-standard levels fall through to the stdlib string form.
func levelString(l slog.Level) string {
	switch {
	case l >= slog.LevelError:
		return "error"
	case l >= slog.LevelWarn:
		return "warn"
	default:
		return l.String()
	}
}
