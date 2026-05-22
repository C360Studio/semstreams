package sensorml

import (
	"strings"
	"testing"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/vocabulary"
	"github.com/c360studio/semstreams/vocabulary/export"

	// Blank-import vocabulary/sosa so its prefixes are registered
	// with vocabulary/export before the Turtle smoke test runs.
	_ "github.com/c360studio/semstreams/vocabulary/sosa"
)

// TestAsset_TriplesEmitFromPhysicalSystemFixture covers the
// canonical "first user" of vocabulary/sosa: parse the SensorML
// fixture, wrap as an Asset with a 6-part entity ID, emit
// triples, and verify both the structural triples (type, hosts
// per component) and the SOSA/SSN IRI compaction through
// vocabulary/export.
func TestAsset_TriplesEmitFromPhysicalSystemFixture(t *testing.T) {
	defer vocabulary.SnapshotRegistry()()

	data := loadFixture(t, "drone-001.sensorml.json")
	process, err := UnmarshalProcess(data)
	if err != nil {
		t.Fatalf("UnmarshalProcess: %v", err)
	}

	const platformID = "acme.ops.robotics.gcs.drone.001"
	asset := NewAsset(platformID, process)
	// Child resolver: drone-001's components live under the
	// platform's 6-part ID.
	asset.ChildIDFn = func(localID string) string {
		return platformID + "/" + localID
	}

	triples := asset.Triples()
	if len(triples) == 0 {
		t.Fatal("expected non-empty triples")
	}

	// Look for the rdf:type triple and confirm it carries the
	// SOSA System IRI.
	if !containsTriple(triples, message.Triple{
		Subject:   platformID,
		Predicate: PredType,
		Object:    "http://www.w3.org/ns/ssn/System",
	}) {
		t.Errorf("expected rdf:type → ssn:System triple, got %v", triples)
	}

	// Each child must produce a hosts + isHostedBy pair.
	for _, childLocal := range []string{"battery-sensor", "gps-receiver"} {
		childID := platformID + "/" + childLocal
		if !containsTriple(triples, message.Triple{
			Subject:   platformID,
			Predicate: PredHosts,
			Object:    childID,
		}) {
			t.Errorf("expected sosa:hosts → %s triple, got %v", childID, triples)
		}
		if !containsTriple(triples, message.Triple{
			Subject:   childID,
			Predicate: PredIsHostedBy,
			Object:    platformID,
		}) {
			t.Errorf("expected sosa:isHostedBy → %s triple, got %v", platformID, triples)
		}
	}

	// Identifier values flow through.
	if !triplePredicateAndObjectMatch(triples, PredIdentifierValue, "SN-12345") {
		t.Errorf("expected identifier value SN-12345, got %v", triples)
	}
	if !triplePredicateAndObjectMatch(triples, PredIdentifierValue, "ALPHA-1") {
		t.Errorf("expected identifier value ALPHA-1, got %v", triples)
	}

	// RDF/Turtle export through vocabulary/export must compact
	// the dotted predicates to their sosa:/ssn: forms — proving
	// the predicates.go init() registration is live and
	// vocabulary/sosa's Register() is wired.
	out, err := export.SerializeToString(triples, export.Turtle)
	if err != nil {
		t.Fatalf("Turtle export: %v", err)
	}
	for _, want := range []string{
		"@prefix sosa: <http://www.w3.org/ns/sosa/>",
		"@prefix ssn: <http://www.w3.org/ns/ssn/>",
		"sosa:hosts",
		"sosa:isHostedBy",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected Turtle output to contain %q, got:\n%s", want, out)
		}
	}
}

func TestAsset_ComponentTriplesIncludeProcedure(t *testing.T) {
	defer vocabulary.SnapshotRegistry()()

	data := loadFixture(t, "drone-001.sensorml.json")
	process, err := UnmarshalProcess(data)
	if err != nil {
		t.Fatalf("UnmarshalProcess: %v", err)
	}
	sys := process.(*PhysicalSystem)
	component := sys.Components[0].(*PhysicalComponent)

	const componentID = "acme.ops.robotics.gcs.drone.001/battery-sensor"
	asset := NewAsset(componentID, component)
	triples := asset.Triples()

	// rdf:type → sosa:Sensor
	if !containsTriple(triples, message.Triple{
		Subject:   componentID,
		Predicate: PredType,
		Object:    "http://www.w3.org/ns/sosa/Sensor",
	}) {
		t.Errorf("expected rdf:type → sosa:Sensor for component, got %v", triples)
	}
	// sosa:usedProcedure → procedure href
	if !containsTriple(triples, message.Triple{
		Subject:   componentID,
		Predicate: PredUsedProcedure,
		Object:    "http://example.org/procedures/voltmeter",
	}) {
		t.Errorf("expected sosa:usedProcedure → voltmeter, got %v", triples)
	}
}

func TestAsset_AggregateProcess_HasSubSystem(t *testing.T) {
	defer vocabulary.SnapshotRegistry()()

	const raw = `{
        "type": "AggregateProcess",
        "id": "ndvi-pipeline",
        "components": [
            {"type": "SimpleProcess", "id": "downsample", "method": {"href": "http://example.org/methods/downsample"}},
            {"type": "SimpleProcess", "id": "compute-ndvi", "method": {"href": "http://example.org/methods/ndvi"}}
        ]
    }`
	process, err := UnmarshalProcess([]byte(raw))
	if err != nil {
		t.Fatalf("UnmarshalProcess: %v", err)
	}
	const aggID = "acme.ops.imagery.satellite.pipeline.ndvi"
	asset := NewAsset(aggID, process)
	asset.ChildIDFn = func(localID string) string { return aggID + "/" + localID }

	triples := asset.Triples()

	for _, childLocal := range []string{"downsample", "compute-ndvi"} {
		if !containsTriple(triples, message.Triple{
			Subject:   aggID,
			Predicate: PredHasSubSystem,
			Object:    aggID + "/" + childLocal,
		}) {
			t.Errorf("expected ssn:hasSubSystem → %s, got %v", childLocal, triples)
		}
	}
}

func TestAsset_NilProcess(t *testing.T) {
	asset := &Asset{entityID: "x.y.z.a.b.c"}
	if got := asset.Triples(); got != nil {
		t.Errorf("expected nil triples on nil process, got %v", got)
	}
}

func TestAsset_PositionRoundTripsAsTriple(t *testing.T) {
	// Issue #114: SensorML position field used to be silently dropped
	// by the parser's type model and triple emitter. The fix:
	// preserve the raw GeoJSON-shaped JSON on AbstractProcess.Position
	// and emit a sensorml.process.position triple (→ sosa:hasLocation).
	defer vocabulary.SnapshotRegistry()()

	const sml = `{
		"type": "PhysicalSystem",
		"id": "platform-001",
		"label": "Sensor Platform",
		"position": {"type": "Point", "coordinates": [-122.4194, 37.7749, 10.0]}
	}`

	process, err := UnmarshalProcess([]byte(sml))
	if err != nil {
		t.Fatalf("UnmarshalProcess: %v", err)
	}

	const platformID = "acme.ops.robotics.gcs.platform.001"
	asset := NewAsset(platformID, process)
	triples := asset.Triples()

	// The parser must preserve the raw GeoJSON on the type model.
	base := process.Base()
	if base.Position == nil {
		t.Fatal("expected AbstractProcess.Position to be populated; SensorML position field was silently dropped")
	}
	wantGeoJSON := `{"type": "Point", "coordinates": [-122.4194, 37.7749, 10.0]}`
	if got := string(base.Position.Raw); got != wantGeoJSON {
		t.Errorf("Position.Raw mismatch:\n got: %q\nwant: %q", got, wantGeoJSON)
	}

	// And emit a position triple under PredPosition (→ sosa:hasLocation).
	if !triplePredicateAndObjectMatch(triples, PredPosition, wantGeoJSON) {
		t.Errorf("expected position triple %q → %q; got %v", PredPosition, wantGeoJSON, triples)
	}

	// Turtle export must compact PredPosition to sosa:hasLocation
	// (proving the predicate registration in init() is live).
	out, err := export.SerializeToString(triples, export.Turtle)
	if err != nil {
		t.Fatalf("Turtle export: %v", err)
	}
	if !strings.Contains(out, "sosa:hasLocation") {
		t.Errorf("expected Turtle output to contain sosa:hasLocation, got:\n%s", out)
	}
}

func TestAsset_PositionAbsent_NoTripleEmitted(t *testing.T) {
	// Regression: a process WITHOUT a position field must not
	// emit a position triple. Guards against Object: "" or
	// Object: "null" leaks.
	const sml = `{
		"type": "PhysicalSystem",
		"id": "no-position-platform",
		"label": "Indoor sensor — no geometry"
	}`

	process, err := UnmarshalProcess([]byte(sml))
	if err != nil {
		t.Fatalf("UnmarshalProcess: %v", err)
	}

	asset := NewAsset("acme.ops.indoor.gcs.platform.001", process)
	triples := asset.Triples()
	for _, tr := range triples {
		if tr.Predicate == PredPosition {
			t.Errorf("unexpected position triple emitted when SensorML has no position field: %v", tr)
		}
	}
}

func containsTriple(triples []message.Triple, want message.Triple) bool {
	for _, t := range triples {
		if t.Subject == want.Subject && t.Predicate == want.Predicate && t.Object == want.Object {
			return true
		}
	}
	return false
}

func triplePredicateAndObjectMatch(triples []message.Triple, predicate string, object any) bool {
	for _, t := range triples {
		if t.Predicate == predicate && t.Object == object {
			return true
		}
	}
	return false
}
