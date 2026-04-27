package payloadregistry

import (
	"testing"
)

// NewForTest returns a fresh empty Registry suitable for unit tests
// that want full isolation from other tests in the same process.
// Registers nothing — caller adds whatever payloads the test needs.
//
// For tests that want the full first-party builtin set, the
// payloadbuiltins package provides a Register helper; tests in higher
// layers can call it after constructing a registry here. We don't
// import payloadbuiltins from this package because doing so would
// re-introduce the import cycle this package is structured to avoid.
func NewForTest(tb testing.TB) *Registry {
	tb.Helper()
	return New()
}

// NewWithSubset returns a fresh Registry populated by calling each
// supplied registration function in order. Useful when a test needs a
// subset of builtin payloads — e.g., only `agentic` types — without
// pulling in the full first-party set.
//
//	reg := payloadregistry.NewWithSubset(t, agentic.RegisterPayloads)
//
// Aggregates registration errors via errors.Join; calls tb.Fatal on
// any error so the test terminates immediately.
func NewWithSubset(tb testing.TB, regs ...func(*Registry) error) *Registry {
	tb.Helper()
	reg := New()
	for _, fn := range regs {
		if err := fn(reg); err != nil {
			tb.Fatalf("payloadregistry.NewWithSubset: %v", err)
		}
	}
	return reg
}
