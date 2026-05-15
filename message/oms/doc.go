// Package oms provides the OGC OMS v3.0 Observation document
// payload type for SemStreams. It is the bidirectional mapper
// between OMS-natural JSON (the wire shape every Connected
// Systems API consumer exchanges) and the SemStreams
// [message.BaseMessage] envelope that downstream processors
// route through the payload registry.
//
// # What it ships
//
//   - [Observation] — the OGC OMS v3.0 Observation Go struct.
//     Implements [message.Payload] (Schema / Validate /
//     MarshalJSON / UnmarshalJSON) and [graph.Graphable]
//     (EntityID / Triples).
//   - [FeatureOfInterest] — accepts either a URI reference or
//     an inline GeoJSON Feature (via Phase 3
//     graph/geo/geojson).
//   - [RegisterPayloads] — registers ogc.oms.v3 with the
//     payload registry. Aggregated into payloadbuiltins so
//     every production binary picks it up automatically.
//
// # Schema identity
//
// The payload Schema is fixed at Type{Domain: "ogc", Category:
// "oms", Version: "v3"} — abbreviated as "ogc.oms.v3" in
// registry diagnostics and reactive-rule predicate matching.
//
// # Wire shape
//
// [Observation.MarshalJSON] emits the OMS-natural JSON document
// per OGC 20-082r4 bundled with CS API v1.0:
//
//	{
//	    "type": "Observation",
//	    "id": "observation-7f3a",
//	    "procedure": "http://example.org/procedures/voltmeter",
//	    "observedProperty": "http://example.org/properties/voltage",
//	    "featureOfInterest": "http://example.org/features/battery-001",
//	    "phenomenonTime": "2026-05-15T14:30:00Z",
//	    "resultTime": "2026-05-15T14:30:00Z",
//	    "result": 12.4
//	}
//
// The BaseMessage envelope around an Observation places that JSON
// in the "payload" field of the SemStreams [message.wireFormat],
// so the same OMS-natural bytes flow through internal NATS
// publishes via [message.NewDecoder] without re-encoding.
//
// # Scope
//
// MVP coverage targets the CS API v1.0 critical path:
//
//   - Result as a simple JSON value (number, string, boolean).
//     Quantity (value + uom), Category, and TimeSeries result
//     shapes are deferred — operators producing typed results
//     today should bind their UoM via the SensorML side
//     (Phase 5) or via downstream processors.
//   - PhenomenonTime / ResultTime as ISO 8601 instants only.
//     Time intervals ({begin, end}) deferred.
//   - FeatureOfInterest as either a URI reference or an inline
//     GeoJSON Feature. Other OGC reference shapes deferred.
//   - Parameter, ValidTime, ResultQuality, RelatedObservation —
//     all deferred. Operators needing these should file a
//     follow-up.
//
// See [ADR-044] for the framework / sister-repo split rationale
// and the dependency chain that places this package in Phase 6.
//
// # External references
//
//   - OGC 20-082r4 OMS v3.0: https://docs.ogc.org/as/20-082r4/20-082r4.html
//   - CS API Part 2 (dynamic data):
//     https://docs.ogc.org/DRAFTS/23-002r0.html
//
// [graph.Graphable]: ../../graph
// [ADR-044]: ../../docs/adr/044-ogc-connected-systems-framework-split.md
package oms
