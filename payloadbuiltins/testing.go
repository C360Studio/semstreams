package payloadbuiltins

import (
	"testing"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadregistry"
)

// NewTestRegistry builds a *payloadregistry.Registry populated with
// the framework-core payloads via Register(reg). Convenience for tests
// that need to wire deps.PayloadRegistry on a component under test.
//
// The testing.TB argument forces this to compile only in test files —
// production code physically cannot reach this helper. Mirrors
// httptest.NewRecorder placement.
//
// For tests that want a tighter subset, build a registry directly via
// payloadregistry.NewWithSubset.
func NewTestRegistry(tb testing.TB) *payloadregistry.Registry {
	tb.Helper()
	reg := payloadregistry.New()
	if err := Register(reg); err != nil {
		tb.Fatalf("payloadbuiltins.NewTestRegistry: register builtins: %v", err)
	}
	return reg
}

// NewTestDecoder builds a registry populated with all first-party
// payloads and returns a *message.Decoder bound to it. Convenience
// for tests that decode wire bytes directly without going through a
// component.
//
// Equivalent to: message.NewDecoder(NewTestRegistry(tb)).
func NewTestDecoder(tb testing.TB) *message.Decoder {
	tb.Helper()
	return message.NewDecoder(NewTestRegistry(tb))
}
