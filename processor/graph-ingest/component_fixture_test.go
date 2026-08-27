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
// these are the test-only keys, registered as schema-less stubs with no floor.
var testStampKeys = []string{
	"test.entity.v1",
	"test.widget.v1",
	"test.fixture.v1",
	"test.mutation.v1",
	"test.nofloor.v1",
	"test.seed.v1",
	"test.merge.v1",
	"test.graphable.v1",
	"test.storable.v1",
	"test.poison.v1",
	"test.container.v1",
	"test.sensor.v1",
	"test.decode.v1",
	"test.noop.v1",
	"test.revision.v1",
	"metrictest.widget.v1",
	"boid.telemetry.v1",
	"workflow.task-unit.v1",
	"mission.command.v1",
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
	for _, key := range testStampKeys {
		payloadregistry.RegisterTestType(tb, reg, key)
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
