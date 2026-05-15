package export_test

// Phase 2 smoke test for ADR-044 — confirms that importing the
// new vocabulary/{sosa,swe,oms} sub-packages registers their
// prefixes via init() and that an end-to-end Turtle export of
// app-level predicates bound to SOSA / SSN / SWE / OMS IRIs
// produces compacted output with all four expected prefixes.
//
// This is the "first user" of the new packages that satisfies the
// PR-scope rule: the three packages plus a proximate exporter
// consumer ship together, so no dead code accumulates between the
// framework merge and the first sister-repo consumer.
//
// The pattern exercised here is the established one: app-level
// dotted predicates registered with vocabulary.Register(...,
// WithIRI(sosa.Observes)). The IRI constants in vocabulary/sosa,
// vocabulary/swe, and vocabulary/oms are designed to be passed
// through WithIRI; the exporter's prefix table (extended via
// each package's Register() init) handles the compaction.

import (
	"strings"
	"testing"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/vocabulary"
	"github.com/c360studio/semstreams/vocabulary/export"

	// Blank-import to trigger init() prefix registration.
	_ "github.com/c360studio/semstreams/vocabulary/oms"
	_ "github.com/c360studio/semstreams/vocabulary/sosa"
	_ "github.com/c360studio/semstreams/vocabulary/swe"

	"github.com/c360studio/semstreams/vocabulary/oms"
	"github.com/c360studio/semstreams/vocabulary/sosa"
	"github.com/c360studio/semstreams/vocabulary/swe"
)

// registerPhase2SmokePredicates wires app-level dotted predicates
// to the SOSA/SSN/SWE/OMS IRIs exposed by the new packages. In
// production code this binding lives in each consumer's
// domain-specific vocabulary file (per the existing pattern); the
// test inlines it because the binding IS the demonstration.
func registerPhase2SmokePredicates() {
	vocabulary.Register("smoke.platform.hasDeployment", vocabulary.WithIRI(sosa.SSNHasDeployment))
	vocabulary.Register("smoke.platform.hosts", vocabulary.WithIRI(sosa.Hosts))
	vocabulary.Register("smoke.sensor.madeObservation", vocabulary.WithIRI(sosa.MadeObservation))
	vocabulary.Register("smoke.observation.resultTime", vocabulary.WithIRI(sosa.ResultTime))
	vocabulary.Register("smoke.observation.uom", vocabulary.WithIRI(swe.UoM))
	vocabulary.Register("smoke.observation.observedProperty", vocabulary.WithIRI(oms.ObservedProperty))
}

func TestPhase2_SensorObservationExportsAllFourPrefixes(t *testing.T) {
	defer vocabulary.SnapshotRegistry()()
	registerPhase2SmokePredicates()

	const (
		platform    = "acme.ops.robotics.gcs.drone.001"
		sensor      = "acme.ops.robotics.gcs.drone.001/sensor/battery"
		observation = "acme.ops.robotics.gcs.drone.001/obs/123"
		deployment  = "acme.ops.robotics.gcs.drone.001/deployment/mission-7"
	)
	triples := []message.Triple{
		{Subject: platform, Predicate: "smoke.platform.hasDeployment", Object: deployment},
		{Subject: platform, Predicate: "smoke.platform.hosts", Object: sensor},
		{Subject: sensor, Predicate: "smoke.sensor.madeObservation", Object: observation},
		{Subject: observation, Predicate: "smoke.observation.resultTime", Object: "2026-05-15T12:00:00Z"},
		{Subject: observation, Predicate: "smoke.observation.uom", Object: "%"},
		{Subject: observation, Predicate: "smoke.observation.observedProperty", Object: "http://example.org/battery-level"},
	}

	out, err := export.SerializeToString(triples, export.Turtle)
	if err != nil {
		t.Fatalf("SerializeToString: %v", err)
	}

	// Prefix declarations should appear for every namespace touched.
	for _, want := range []string{
		"@prefix sosa: <http://www.w3.org/ns/sosa/>",
		"@prefix ssn: <http://www.w3.org/ns/ssn/>",
		"@prefix swe: <http://www.opengis.net/swe/2.0/>",
		"@prefix oms: <http://www.opengis.net/oms/3.0/>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got:\n%s", want, out)
		}
	}

	// And the compacted predicates themselves should appear.
	for _, want := range []string{
		"ssn:hasDeployment",
		"sosa:hosts",
		"sosa:madeObservation",
		"sosa:resultTime",
		"swe:uom",
		"oms:observedProperty",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain compacted predicate %q, got:\n%s", want, out)
		}
	}
}

func TestPhase2_RegisteredPrefixesSurviveExporterReuse(t *testing.T) {
	defer vocabulary.SnapshotRegistry()()
	registerPhase2SmokePredicates()

	triples := []message.Triple{
		{Subject: "acme.ops.robotics.gcs.drone.001", Predicate: "smoke.platform.hosts", Object: "sensor"},
	}

	first, err := export.SerializeToString(triples, export.Turtle)
	if err != nil {
		t.Fatalf("first export: %v", err)
	}
	if !strings.Contains(first, "sosa:hosts") {
		t.Errorf("first export missing sosa:hosts:\n%s", first)
	}

	// A custom base IRI overrides only the ss: prefix; sosa: should still resolve.
	second, err := export.SerializeToString(triples, export.Turtle, export.WithBaseIRI("https://example.org"))
	if err != nil {
		t.Fatalf("second export: %v", err)
	}
	if !strings.Contains(second, "sosa:hosts") {
		t.Errorf("second export missing sosa:hosts:\n%s", second)
	}
	// The override should also re-base subject IRIs under the custom
	// base, proving the snapshot-and-override interaction works
	// together rather than the prefix snapshot silently shadowing it.
	// (The @prefix ss: declaration only appears when an IRI actually
	// compacts through ss: — entity-shaped subjects have slashes in
	// their local part and bypass CURIE compaction, so we verify the
	// effect on the subject IRI directly.)
	if !strings.Contains(second, "<https://example.org/entities/") {
		t.Errorf("second export expected subject IRI under custom base, got:\n%s", second)
	}
}
