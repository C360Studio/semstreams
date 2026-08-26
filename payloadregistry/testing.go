package payloadregistry

import (
	"strings"
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

// testStubPayload is the schema-less stub RegisterTestType registers. It
// carries no Schema(), so validateSchemaConsistency skips it; it can never be
// decoded on the fact lane — it exists only so a unit test can stamp the key
// at graph-ingest's create seam without owning a payload type.
type testStubPayload struct{}

// RegisterTestType registers key ("domain.category.version") with a
// schema-less stub factory and no floor. tb.Fatalf on a malformed key or a
// registration error, so a test terminates at the fixture rather than at the
// first rejected create.
func RegisterTestType(tb testing.TB, reg *Registry, key string) {
	tb.Helper()
	parts := strings.Split(key, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		tb.Fatalf("payloadregistry.RegisterTestType: key %q is not domain.category.version", key)
	}
	if err := reg.Register(&Registration{
		Domain: parts[0], Category: parts[1], Version: parts[2],
		Description: "test stub type " + key,
		Factory:     func() any { return &testStubPayload{} },
	}); err != nil {
		tb.Fatalf("payloadregistry.RegisterTestType(%q): %v", key, err)
	}
}
