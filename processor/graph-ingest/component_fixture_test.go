package graphingest

import (
	"context"
	"fmt"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
	semtypes "github.com/c360studio/semstreams/pkg/types"

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

// testDeploymentOrg / testDeploymentPlatform are the authority the fixtures in
// this package build a component under (ADR-102): graph-ingest refuses every
// candidate subject whose positions 1-2 differ, so a fixture's deployment pair
// and its entity-ID fixtures are one decision. They match the majority of this
// package's IDs; a file whose fixtures sit under a different pair passes
// withAuthority so the gate compares against the pair those IDs actually use.
const (
	testDeploymentOrg      = "c360"
	testDeploymentPlatform = "platform"
)

// testComponentOption customizes the Dependencies a fixture constructs with.
type testComponentOption func(*component.Dependencies)

// withAuthority points the fixture's authority gate at a different deployment
// pair. Naming it at the call site keeps "which deployment is this?" visible in
// the test rather than buried in a shared default.
func withAuthority(org, platform string) testComponentOption {
	return func(deps *component.Dependencies) {
		deps.Platform = component.PlatformMeta{Org: org, Platform: platform}
	}
}

// testDependencies builds the standard fixture Dependencies: a real payload
// registry, the caller's NATS client, and the deployment authority every
// graph-ingest now requires at construction.
func testDependencies(tb testing.TB, natsClient *natsclient.Client, opts ...testComponentOption) component.Dependencies {
	tb.Helper()
	deps := component.Dependencies{
		NATSClient:      natsClient,
		PayloadRegistry: newTestPayloadRegistry(tb),
		Platform:        component.PlatformMeta{Org: testDeploymentOrg, Platform: testDeploymentPlatform},
	}
	for _, opt := range opts {
		opt(&deps)
	}
	return deps
}

// authorityOfFixture is withAuthority read off a fixture entity ID. Read-path
// tests seed through the production write path, so the component must BE the
// deployment that owns the IDs the table declares; deriving the pair from the
// fixture keeps a heterogeneous table's rows and their component in lockstep
// instead of duplicating the pair in every row.
//
// It is a test convenience and nothing else: ADR-102 d2 forbids exactly this
// read-back on any minting path, which is why no production code does it.
func authorityOfFixture(tb testing.TB, entityID string) testComponentOption {
	tb.Helper()
	parsed, err := semtypes.ParseEntityID(entityID)
	if err != nil {
		tb.Fatalf("fixture entity ID %q is not canonical: %v", entityID, err)
	}
	return withAuthority(parsed.Org, parsed.Platform)
}

// seedEntityState writes an entity straight into ENTITY_STATES, as a mirror of
// what an import lane would have produced. A fixture needs it only for an
// entity under a FOREIGN authority: the in-process write paths are
// authority-bound (ADR-102 d5), and correctly so — an in-process create under a
// peer's pair would be the framework minting under a foreign authority. The
// import lane itself is exercised end-to-end by
// TestImportLaneAcceptsForeignRejectsLocalClaim; here it is only a seed.
func seedEntityState(tb testing.TB, c *Component, entity *graph.EntityState) {
	tb.Helper()
	encoded, err := graph.MarshalEntityState(entity)
	if err != nil {
		tb.Fatalf("marshal mirrored entity %q: %v", entity.ID, err)
	}
	if _, err := c.entityBucket.Put(context.Background(), entity.ID, encoded); err != nil {
		tb.Fatalf("seed mirrored entity %q: %v", entity.ID, err)
	}
}

// seedOwnedOrMirrored writes entity through the production create path when it
// carries this component's authority, and as a mirror when it does not — the
// shape a federated deployment actually holds.
func seedOwnedOrMirrored(tb testing.TB, c *Component, entity *graph.EntityState) {
	tb.Helper()
	if c.authorizeSubject(entity.ID, false) == nil {
		if err := c.CreateEntity(context.Background(), entity); err != nil {
			tb.Fatalf("create owned entity %q: %v", entity.ID, err)
		}
		return
	}
	seedEntityState(tb, c, entity)
}
