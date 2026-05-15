package oms

import (
	"encoding/json"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/vocabulary/sosa"
)

// EntityID implements [graph.Graphable]. Returns the
// Observation's local ID — operators routing through the graph
// pipeline are expected to either populate the ID field with a
// 6-part SemStreams entity ID or rebind via a wrapping payload.
// Returns the empty string when ID is unset; downstream
// graph-ingest treats that as "no entity," which is the right
// fall-through for ingest pipelines doing entity resolution
// from triples instead of the local ID.
func (o *Observation) EntityID() string { return o.ID }

// Triples implements [graph.Graphable]. Emits the SOSA-aligned
// triple set for this observation:
//
//   - rdf:type → sosa:Observation
//   - sosa:usedProcedure → procedure IRI
//   - sosa:observedProperty → property IRI
//   - sosa:hasFeatureOfInterest → FoI IRI (URI ref case) or
//     a synthetic inline-Feature subject (deferred)
//   - sosa:resultTime → ISO 8601 timestamp
//   - sosa:phenomenonTime → ISO 8601 timestamp (when present)
//   - sosa:hasSimpleResult → the literal result value
//
// Inline-GeoJSON FoIs emit a single hasFeatureOfInterest triple
// pointing at the GeoJSON Feature's id (or empty when the
// Feature has no id). Richer per-Feature triples would require
// a second-pass entity emission and are deferred — the graph
// pipeline can pick up the inline Feature via a dedicated
// processor.
func (o *Observation) Triples() []message.Triple {
	if o == nil || o.ID == "" {
		return nil
	}
	out := make([]message.Triple, 0, 8)
	out = append(out, message.Triple{
		Subject:   o.ID,
		Predicate: PredType,
		Object:    sosa.Observation,
	})
	if o.Procedure != "" {
		out = append(out, message.Triple{Subject: o.ID, Predicate: PredUsedProcedure, Object: o.Procedure})
	}
	if o.ObservedProperty != "" {
		out = append(out, message.Triple{Subject: o.ID, Predicate: PredObservedProperty, Object: o.ObservedProperty})
	}
	if foi := foiTriple(o); foi != "" {
		out = append(out, message.Triple{Subject: o.ID, Predicate: PredHasFeatureOfInterest, Object: foi})
	}
	if o.ResultTime != "" {
		out = append(out, message.Triple{Subject: o.ID, Predicate: PredResultTime, Object: o.ResultTime})
	}
	if o.PhenomenonTime != "" {
		out = append(out, message.Triple{Subject: o.ID, Predicate: PredPhenomenonTime, Object: o.PhenomenonTime})
	}
	if o.Result != nil {
		out = append(out, message.Triple{Subject: o.ID, Predicate: PredHasSimpleResult, Object: o.Result})
	}
	return out
}

// foiTriple resolves the FeatureOfInterest to a single triple
// object. URI-shaped FoIs return the URI; inline-GeoJSON FoIs
// return the Feature's id (when present) or empty (when not —
// inline anonymous features don't graph cleanly until the
// graph pipeline adds a Feature-aware sibling emitter).
func foiTriple(o *Observation) string {
	if o.FeatureOfInterest == nil {
		return ""
	}
	if o.FeatureOfInterest.Href != "" {
		return o.FeatureOfInterest.Href
	}
	if o.FeatureOfInterest.Feature != nil {
		if rawID := o.FeatureOfInterest.Feature.RawID; len(rawID) > 0 {
			// Use json.Unmarshal to canonicalize the id rather
			// than byte-level quote-trimming — string ids with
			// escaped characters (e.g. "scan\"cell" embedded
			// quote, JSON-legal) would otherwise survive the
			// trim with the backslash intact.
			var asString string
			if err := json.Unmarshal(rawID, &asString); err == nil {
				return asString
			}
			var asNumber json.Number
			if err := json.Unmarshal(rawID, &asNumber); err == nil {
				return asNumber.String()
			}
			// Fallback: emit raw bytes for any other JSON
			// shape (booleans, nulls in odd corpora).
			return string(rawID)
		}
	}
	return ""
}
