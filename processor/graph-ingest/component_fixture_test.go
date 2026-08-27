package graphingest

import (
	"fmt"
	"testing"

	"github.com/c360studio/semstreams/agentic/research"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/payloadregistry"
)

// testStampKeys are the message types this package's tests stamp on the
// create seams (ADR-103: a stamp the registry does not hold is refused).
// Builtin and research types are registered by their own RegisterPayloads;
// these are the test-only types, registered as schema-less stubs with no floor.
var testStampKeys = []message.Type{
	{Domain: "test", Category: "entity", Version: "v1"},
	{Domain: "test", Category: "widget", Version: "v1"},
	{Domain: "test", Category: "fixture", Version: "v1"},
	{Domain: "test", Category: "mutation", Version: "v1"},
	{Domain: "test", Category: "nofloor", Version: "v1"},
	{Domain: "test", Category: "seed", Version: "v1"},
	{Domain: "test", Category: "merge", Version: "v1"},
	{Domain: "test", Category: "graphable", Version: "v1"},
	{Domain: "test", Category: "storable", Version: "v1"},
	{Domain: "test", Category: "poison", Version: "v1"},
	{Domain: "test", Category: "container", Version: "v1"},
	{Domain: "test", Category: "sensor", Version: "v1"},
	{Domain: "test", Category: "decode", Version: "v1"},
	{Domain: "test", Category: "noop", Version: "v1"},
	{Domain: "test", Category: "revision", Version: "v1"},
	{Domain: "metrictest", Category: "widget", Version: "v1"},
	{Domain: "boid", Category: "telemetry", Version: "v1"},
	{Domain: "workflow", Category: "task-unit", Version: "v1"},
	{Domain: "mission", Category: "command", Version: "v1"},
}

// testEntityType is the stamp for test entities born through CreateEntity.
func testEntityType() message.Type {
	return message.Type{Domain: "test", Category: "entity", Version: "v1"}
}

// registerTestStamps adds graph research and every test-only stamp through
// payloadregistry.RegisterTestType — the one stub-type spelling.
func registerTestStamps(tb testing.TB, reg *payloadregistry.Registry) {
	tb.Helper()
	if err := research.RegisterPayloads(reg); err != nil {
		tb.Fatalf("register research payloads: %v", err)
	}
	for _, mt := range testStampKeys {
		payloadregistry.RegisterTestType(tb, reg, mt)
	}
}

// newTestPayloadRegistry builds the registry graph-ingest tests inject:
// the framework builtin set, graph research, and every test-only stamp.
func newTestPayloadRegistry(tb testing.TB) *payloadregistry.Registry {
	tb.Helper()
	reg := payloadbuiltins.NewTestRegistry(tb)
	registerTestStamps(tb, reg)
	return reg
}

// panicTB is the panic-shaped testing.TB for helpers that have no test in
// scope (the shared lifecycle fixture): Fatalf panics, Helper is a no-op,
// everything else is unreachable from RegisterTestType and NewTestRegistry.
type panicTB struct{ testing.TB }

func (panicTB) Helper() {}

func (panicTB) Fatalf(format string, args ...any) {
	panic(fmt.Sprintf("graph-ingest test registry: "+format, args...))
}

// mustTestPayloadRegistry is newTestPayloadRegistry for helpers that have no
// testing.TB in scope.
func mustTestPayloadRegistry() *payloadregistry.Registry {
	return newTestPayloadRegistry(panicTB{})
}

// withTestRegistry gives a Component built as a literal (bypassing the
// factory) the registry and decoder the factory would have set, so the
// fail-closed create seam admits registered test stamps (O-15, tasks 5.4).
func withTestRegistry(tb testing.TB, c *Component) *Component {
	tb.Helper()
	c.payloadRegistry = newTestPayloadRegistry(tb)
	c.decoder = message.NewDecoder(c.payloadRegistry)
	return c
}
