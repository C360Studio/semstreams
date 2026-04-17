package operatingmodel

import (
	"time"

	"github.com/c360studio/semstreams/message"
)

// ProfileRef identifies a user's operating-model profile in the 6-part
// entity-ID namespace. It is the minimum information needed to emit triples.
type ProfileRef struct {
	// Org is the organization part of the entity ID.
	Org string
	// Platform is the platform part of the entity ID.
	Platform string
	// UserID is the instance part for the profile entity.
	UserID string
	// Version is the current operating-model profile version (starts at 1).
	Version int
}

// LayerTriples returns the triples that describe an approved layer checkpoint
// and all of its entries, plus the profile-root links that connect the layer
// into the user's profile.
//
// The caller is responsible for deduplicating profile-root links across
// multiple layer writes (e.g. by using an "upsert" graph operation).
// entries is treated as approved content; each entry's Validate() should have
// passed before this call.
//
// now is captured in each triple's Timestamp field so all triples from a
// single approval share the same time.
func LayerTriples(
	ref ProfileRef,
	layer string,
	checkpointSummary string,
	entries []Entry,
	now time.Time,
) []message.Triple {
	profileID := ProfileEntityID(ref.Org, ref.Platform, ref.UserID)
	layerID := LayerEntityID(ref.Org, ref.Platform, ref.UserID, layer)

	triples := profileRootTriples(profileID, layerID, ref.Version, now)
	triples = append(triples, layerBodyTriples(layerID, layer, checkpointSummary, ref.Version, now)...)
	triples = append(triples, entryTriplesForLayer(ref, layerID, entries, now)...)
	return triples
}

// profileRootTriples emits the profile-version, last-updated, and the
// has-layer relationship.
func profileRootTriples(profileID, layerID string, version int, now time.Time) []message.Triple {
	return []message.Triple{
		{
			Subject:    profileID,
			Predicate:  PredicateProfileVersion,
			Object:     int64(version),
			Source:     TripleSource,
			Timestamp:  now,
			Confidence: 1.0,
		},
		{
			Subject:    profileID,
			Predicate:  PredicateProfileLastUpdated,
			Object:     now.UTC().Format(time.RFC3339),
			Source:     TripleSource,
			Timestamp:  now,
			Confidence: 1.0,
			Datatype:   "xsd:dateTime",
		},
		{
			Subject:    profileID,
			Predicate:  PredicateProfileHasLayer,
			Object:     layerID,
			Source:     TripleSource,
			Timestamp:  now,
			Confidence: 1.0,
		},
	}
}

// layerBodyTriples emits the per-layer checkpoint triples.
func layerBodyTriples(layerID, layer, summary string, version int, now time.Time) []message.Triple {
	return []message.Triple{
		{
			Subject:    layerID,
			Predicate:  PredicateLayerName,
			Object:     layer,
			Source:     TripleSource,
			Timestamp:  now,
			Confidence: 1.0,
		},
		{
			Subject:    layerID,
			Predicate:  PredicateLayerCheckpointSummary,
			Object:     summary,
			Source:     TripleSource,
			Timestamp:  now,
			Confidence: 1.0,
		},
		{
			Subject:    layerID,
			Predicate:  PredicateLayerVersion,
			Object:     int64(version),
			Source:     TripleSource,
			Timestamp:  now,
			Confidence: 1.0,
		},
	}
}

// entryTriplesForLayer emits has_entry links from the layer and the full body
// of each entry.
func entryTriplesForLayer(ref ProfileRef, layerID string, entries []Entry, now time.Time) []message.Triple {
	if len(entries) == 0 {
		return nil
	}
	out := make([]message.Triple, 0, len(entries)*8)
	for _, e := range entries {
		entryID := EntryEntityID(ref.Org, ref.Platform, e.EntryID)
		out = append(out, message.Triple{
			Subject:    layerID,
			Predicate:  PredicateLayerHasEntry,
			Object:     entryID,
			Source:     TripleSource,
			Timestamp:  now,
			Confidence: 1.0,
		})
		out = append(out, entryBodyTriples(entryID, e, now)...)
	}
	return out
}

// entryBodyTriples emits one triple per populated field of an entry using the
// predicate-per-field convention from semstreams' agentic graph writer.
func entryBodyTriples(entryID string, e Entry, now time.Time) []message.Triple {
	base := func(pred string, obj any) message.Triple {
		return message.Triple{
			Subject:    entryID,
			Predicate:  pred,
			Object:     obj,
			Source:     TripleSource,
			Timestamp:  now,
			Confidence: 1.0,
		}
	}

	triples := []message.Triple{
		base(PredicateEntryTitle, e.Title),
		base(PredicateEntrySummary, e.Summary),
		base(PredicateEntrySourceConfidence, e.ResolvedSourceConfidence()),
		base(PredicateEntryStatus, e.ResolvedStatus()),
	}
	if e.Cadence != "" {
		triples = append(triples, base(PredicateEntryCadence, e.Cadence))
	}
	if e.Trigger != "" {
		triples = append(triples, base(PredicateEntryTrigger, e.Trigger))
	}
	if len(e.Inputs) > 0 {
		triples = append(triples, base(PredicateEntryInputs, toAnySlice(e.Inputs)))
	}
	if len(e.Stakeholders) > 0 {
		triples = append(triples, base(PredicateEntryStakeholders, toAnySlice(e.Stakeholders)))
	}
	if len(e.Constraints) > 0 {
		triples = append(triples, base(PredicateEntryConstraints, toAnySlice(e.Constraints)))
	}
	return triples
}

// toAnySlice copies a []string into []any so it JSON-encodes as a list
// without forcing downstream consumers to reflect on concrete types.
func toAnySlice(in []string) []any {
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}
