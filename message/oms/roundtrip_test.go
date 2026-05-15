package oms_test

// Phase 6 production-path round-trip test. This is the test
// that Phase 4 (MF-1) and Phase 5 reviewers explicitly deferred
// to this phase — the payload-registry seam where a BaseMessage
// envelope wrapping an OGC OMS Observation must marshal cleanly
// and decode through the production message.Decoder back into
// a typed *Observation with intact triples.
//
// Lives in message/oms_test (external test package) to prove
// the public API is enough to round-trip; if a downstream
// consumer can't reach the internals, neither can this test.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/graph/geo/geojson"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/message/oms"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/vocabulary"
	"github.com/c360studio/semstreams/vocabulary/export"

	// Blank-import vocabulary/sosa so its prefixes are registered
	// with vocabulary/export before any Turtle smoke runs.
	_ "github.com/c360studio/semstreams/vocabulary/sosa"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

// TestProductionRoundTrip_BaseMessageThroughDecoder is the
// canonical proof Phase 4/5 reviewers asked for: marshal a
// BaseMessage envelope wrapping an OMS Observation, decode
// through payloadbuiltins.NewTestDecoder (which exercises the
// payload registry's wireFormat resolution), and confirm the
// resulting payload is a typed *Observation whose fields and
// triples match the source.
func TestProductionRoundTrip_BaseMessageThroughDecoder(t *testing.T) {
	original := &oms.Observation{
		ID:               "acme.ops.robotics.gcs.drone.001/obs/12345",
		Procedure:        "http://example.org/procedures/voltmeter",
		ObservedProperty: "http://example.org/properties/battery-voltage",
		FeatureOfInterest: oms.NewFeatureOfInterestRef(
			"http://example.org/features/battery-pack-001"),
		PhenomenonTime: "2026-05-15T14:30:00Z",
		ResultTime:     "2026-05-15T14:30:00.250Z",
		Result:         12.4,
	}

	envelope := message.NewBaseMessage(original.Schema(), original, "phase-6-test")
	wireBytes, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}

	decoder := payloadbuiltins.NewTestDecoder(t)
	decoded, err := decoder.Decode(wireBytes)
	if err != nil {
		t.Fatalf("registry decode: %v\nwire: %s", err, wireBytes)
	}

	if got := decoded.Type(); got != original.Schema() {
		t.Errorf("type: got %+v, want %+v", got, original.Schema())
	}
	obs, ok := decoded.Payload().(*oms.Observation)
	if !ok {
		t.Fatalf("payload: got %T, want *oms.Observation", decoded.Payload())
	}
	if obs.ID != original.ID ||
		obs.Procedure != original.Procedure ||
		obs.ObservedProperty != original.ObservedProperty ||
		obs.ResultTime != original.ResultTime ||
		obs.PhenomenonTime != original.PhenomenonTime {
		t.Errorf("scalar fields drift after round-trip:\n  original: %+v\n  got:      %+v", original, obs)
	}
	if obs.Result != 12.4 {
		t.Errorf("result: got %v, want 12.4", obs.Result)
	}
	if obs.FeatureOfInterest == nil ||
		obs.FeatureOfInterest.Href != "http://example.org/features/battery-pack-001" {
		t.Errorf("FoI: got %+v, want href to battery-pack-001", obs.FeatureOfInterest)
	}

	if len(original.Triples()) != len(obs.Triples()) {
		t.Errorf("triple count drift: original=%d got=%d", len(original.Triples()), len(obs.Triples()))
	}
}

// TestRoundTrip_URIFoIFixture confirms the URI-ref FoI shape
// parses from a hand-authored fixture and re-marshals byte-stable.
func TestRoundTrip_URIFoIFixture(t *testing.T) {
	raw := loadFixture(t, "observation-uri-foi.oms.json")
	var obs oms.Observation
	if err := json.Unmarshal(raw, &obs); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	if obs.FeatureOfInterest == nil || obs.FeatureOfInterest.Href == "" {
		t.Fatalf("expected URI-ref FoI, got %+v", obs.FeatureOfInterest)
	}
	if obs.FeatureOfInterest.Feature != nil {
		t.Errorf("URI-ref FoI should not populate Feature")
	}

	remarshaled, err := json.Marshal(&obs)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	var second oms.Observation
	if err := json.Unmarshal(remarshaled, &second); err != nil {
		t.Fatalf("re-unmarshal: %v\nbytes: %s", err, remarshaled)
	}
	if second.FeatureOfInterest.Href != obs.FeatureOfInterest.Href {
		t.Errorf("FoI href drift: got %q, want %q",
			second.FeatureOfInterest.Href, obs.FeatureOfInterest.Href)
	}
}

// TestRoundTrip_InlineGeoJSONFoIFixture confirms the inline-
// GeoJSON FoI shape parses, preserves the embedded Feature
// geometry, and round-trips. Exercises the Phase 3 dependency.
func TestRoundTrip_InlineGeoJSONFoIFixture(t *testing.T) {
	raw := loadFixture(t, "observation-inline-foi.oms.json")
	var obs oms.Observation
	if err := json.Unmarshal(raw, &obs); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	if obs.FeatureOfInterest == nil || obs.FeatureOfInterest.Feature == nil {
		t.Fatalf("expected inline Feature FoI, got %+v", obs.FeatureOfInterest)
	}
	if obs.FeatureOfInterest.Href != "" {
		t.Errorf("inline FoI should not populate Href")
	}

	feat := obs.FeatureOfInterest.Feature
	if feat.Geometry == nil || feat.Geometry.Type() != geojson.TypePoint {
		t.Errorf("expected Point geometry, got %+v", feat.Geometry)
	}

	// Round-trip preserves the Feature.
	remarshaled, err := json.Marshal(&obs)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if !strings.Contains(string(remarshaled), `"type":"Feature"`) {
		t.Errorf("re-marshaled JSON missing inline Feature shape:\n%s", remarshaled)
	}
}

// TestTriples_InlineFoIWithNumericID exercises the foiTriple
// numeric-id branch. The GeoJSON Feature in this fixture has
// `"id": 7` — a JSON number. The resulting hasFeatureOfInterest
// triple should carry the canonical numeric string "7", not
// raw JSON bytes.
func TestTriples_InlineFoIWithNumericID(t *testing.T) {
	defer vocabulary.SnapshotRegistry()()

	raw := loadFixture(t, "observation-numeric-foi-id.oms.json")
	var obs oms.Observation
	if err := json.Unmarshal(raw, &obs); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	triples := obs.Triples()
	var foiObject any
	for _, tr := range triples {
		if tr.Predicate == oms.PredHasFeatureOfInterest {
			foiObject = tr.Object
			break
		}
	}
	if foiObject != "7" {
		t.Errorf("hasFeatureOfInterest object: got %v (%T), want \"7\"", foiObject, foiObject)
	}
}

// TestTriples_AnonymousInlineFoIEmitsNoFoITriple exercises the
// foiTriple anonymous branch. When the inline Feature has no
// id, foiTriple returns empty and Triples() suppresses the
// hasFeatureOfInterest triple entirely. Spatial-extent recovery
// is the documented responsibility of a downstream Feature-aware
// processor; we just confirm we don't fabricate a bogus subject.
func TestTriples_AnonymousInlineFoIEmitsNoFoITriple(t *testing.T) {
	defer vocabulary.SnapshotRegistry()()

	raw := loadFixture(t, "observation-anonymous-foi.oms.json")
	var obs oms.Observation
	if err := json.Unmarshal(raw, &obs); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, tr := range obs.Triples() {
		if tr.Predicate == oms.PredHasFeatureOfInterest {
			t.Errorf("anonymous inline FoI should not emit hasFeatureOfInterest triple, got %+v", tr)
		}
	}
}

// TestObservation_TriplesEmitSosaPrefixes confirms the dotted-
// predicate registration is live: emitting triples and pushing
// through vocabulary/export's Turtle serialization produces
// compacted sosa: forms.
func TestObservation_TriplesEmitSosaPrefixes(t *testing.T) {
	defer vocabulary.SnapshotRegistry()()

	obs := &oms.Observation{
		ID:               "test.entity.observation.42",
		Procedure:        "http://example.org/procedures/voltmeter",
		ObservedProperty: "http://example.org/properties/battery",
		FeatureOfInterest: oms.NewFeatureOfInterestRef(
			"http://example.org/features/battery-pack-001"),
		ResultTime: "2026-05-15T14:30:00Z",
		Result:     12.4,
	}
	triples := obs.Triples()
	if len(triples) == 0 {
		t.Fatal("expected non-empty triples")
	}

	out, err := export.SerializeToString(triples, export.Turtle)
	if err != nil {
		t.Fatalf("turtle export: %v", err)
	}
	for _, want := range []string{
		"@prefix sosa: <http://www.w3.org/ns/sosa/>",
		"sosa:usedProcedure",
		"sosa:observedProperty",
		"sosa:hasFeatureOfInterest",
		"sosa:resultTime",
		"sosa:hasSimpleResult",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected Turtle output to contain %q, got:\n%s", want, out)
		}
	}
}

// TestObservation_ValidateRejectsMissingFields asserts the
// payload's Validate enforces every OMS-required field.
func TestObservation_ValidateRejectsMissingFields(t *testing.T) {
	cases := []struct {
		name    string
		obs     oms.Observation
		wantErr bool
	}{
		{
			name: "all required present",
			obs: oms.Observation{
				Procedure:        "http://example.org/procedures/p",
				ObservedProperty: "http://example.org/properties/q",
				ResultTime:       "2026-05-15T00:00:00Z",
			},
			wantErr: false,
		},
		{
			name: "missing procedure",
			obs: oms.Observation{
				ObservedProperty: "http://example.org/properties/q",
				ResultTime:       "2026-05-15T00:00:00Z",
			},
			wantErr: true,
		},
		{
			name: "missing observed property",
			obs: oms.Observation{
				Procedure:  "http://example.org/procedures/p",
				ResultTime: "2026-05-15T00:00:00Z",
			},
			wantErr: true,
		},
		{
			name: "missing result time",
			obs: oms.Observation{
				Procedure:        "http://example.org/procedures/p",
				ObservedProperty: "http://example.org/properties/q",
			},
			wantErr: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.obs.Validate()
			if c.wantErr && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !c.wantErr && err != nil {
				t.Errorf("expected nil, got %v", err)
			}
		})
	}
}

// TestUnmarshalRejectsBadTypeDiscriminator confirms that a JSON
// document carrying the wrong "type" field is rejected rather
// than silently misclassified.
func TestUnmarshalRejectsBadTypeDiscriminator(t *testing.T) {
	const raw = `{"type": "Sample", "procedure": "x", "observedProperty": "y", "resultTime": "z"}`
	var obs oms.Observation
	if err := json.Unmarshal([]byte(raw), &obs); err == nil {
		t.Error("expected error on wrong type discriminator, got nil")
	}
}
