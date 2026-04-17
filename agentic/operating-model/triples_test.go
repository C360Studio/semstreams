package operatingmodel

import (
	"testing"
	"time"

	"github.com/c360studio/semstreams/message"
)

func fixedTime() time.Time {
	return time.Date(2026, 4, 15, 10, 0, 0, 0, time.UTC)
}

func sampleRef() ProfileRef {
	return ProfileRef{
		Org:      "c360",
		Platform: "ops",
		UserID:   "coby",
		Version:  1,
	}
}

func sampleEntries() []Entry {
	return []Entry{
		{
			EntryID:      "e-001",
			Title:        "Weekly planning block",
			Summary:      "Mondays 9-10am",
			Cadence:      "weekly",
			Trigger:      "Monday 9am",
			Inputs:       []string{"retro notes"},
			Stakeholders: []string{"self"},
			Constraints:  []string{"no meetings"},
		},
		{
			EntryID: "e-002",
			Title:   "Daily standup",
			Summary: "15-min team sync",
		},
	}
}

func containsTriple(triples []message.Triple, subject, predicate string) bool {
	for _, tr := range triples {
		if tr.Subject == subject && tr.Predicate == predicate {
			return true
		}
	}
	return false
}

func findObject(triples []message.Triple, subject, predicate string) any {
	for _, tr := range triples {
		if tr.Subject == subject && tr.Predicate == predicate {
			return tr.Object
		}
	}
	return nil
}

func countTriples(triples []message.Triple, subject, predicate string) int {
	n := 0
	for _, tr := range triples {
		if tr.Subject == subject && tr.Predicate == predicate {
			n++
		}
	}
	return n
}

func TestLayerTriples_ContainsProfileAndLayerCores(t *testing.T) {
	ref := sampleRef()
	triples := LayerTriples(ref, LayerOperatingRhythms, "Weekly rhythms established", sampleEntries(), fixedTime())

	profileID := ProfileEntityID(ref.Org, ref.Platform, ref.UserID)
	layerID := LayerEntityID(ref.Org, ref.Platform, ref.UserID, LayerOperatingRhythms)

	cases := []struct {
		subject   string
		predicate string
	}{
		{profileID, PredicateProfileVersion},
		{profileID, PredicateProfileLastUpdated},
		{profileID, PredicateProfileHasLayer},
		{layerID, PredicateLayerName},
		{layerID, PredicateLayerCheckpointSummary},
		{layerID, PredicateLayerVersion},
	}
	for _, c := range cases {
		if !containsTriple(triples, c.subject, c.predicate) {
			t.Errorf("missing triple: subject=%q predicate=%q", c.subject, c.predicate)
		}
	}
}

func TestLayerTriples_HasEntryLinkPerEntry(t *testing.T) {
	ref := sampleRef()
	entries := sampleEntries()
	triples := LayerTriples(ref, LayerFriction, "2 friction items", entries, fixedTime())

	layerID := LayerEntityID(ref.Org, ref.Platform, ref.UserID, LayerFriction)
	got := countTriples(triples, layerID, PredicateLayerHasEntry)
	if got != len(entries) {
		t.Errorf("has_entry links = %d, want %d", got, len(entries))
	}
}

func TestLayerTriples_EntryBodyPredicates(t *testing.T) {
	ref := sampleRef()
	entries := sampleEntries()
	triples := LayerTriples(ref, LayerOperatingRhythms, "summary", entries, fixedTime())

	e1ID := EntryEntityID(ref.Org, ref.Platform, "e-001")
	e2ID := EntryEntityID(ref.Org, ref.Platform, "e-002")

	e1Expect := []string{
		PredicateEntryTitle, PredicateEntrySummary,
		PredicateEntrySourceConfidence, PredicateEntryStatus,
		PredicateEntryCadence, PredicateEntryTrigger,
		PredicateEntryInputs, PredicateEntryStakeholders, PredicateEntryConstraints,
	}
	for _, pred := range e1Expect {
		if !containsTriple(triples, e1ID, pred) {
			t.Errorf("e-001 missing predicate %q", pred)
		}
	}

	e2Required := []string{
		PredicateEntryTitle, PredicateEntrySummary,
		PredicateEntrySourceConfidence, PredicateEntryStatus,
	}
	for _, pred := range e2Required {
		if !containsTriple(triples, e2ID, pred) {
			t.Errorf("e-002 missing predicate %q", pred)
		}
	}
	e2Absent := []string{
		PredicateEntryCadence, PredicateEntryTrigger,
		PredicateEntryInputs, PredicateEntryStakeholders, PredicateEntryConstraints,
	}
	for _, pred := range e2Absent {
		if containsTriple(triples, e2ID, pred) {
			t.Errorf("e-002 should not have predicate %q when field is empty", pred)
		}
	}
}

func TestLayerTriples_DefaultsApplied(t *testing.T) {
	ref := sampleRef()
	entries := []Entry{{EntryID: "e-1", Title: "t", Summary: "s"}}
	triples := LayerTriples(ref, LayerDependencies, "summary", entries, fixedTime())

	e1ID := EntryEntityID(ref.Org, ref.Platform, "e-1")
	if got := findObject(triples, e1ID, PredicateEntrySourceConfidence); got != ConfidenceConfirmed {
		t.Errorf("default source_confidence = %v, want %q", got, ConfidenceConfirmed)
	}
	if got := findObject(triples, e1ID, PredicateEntryStatus); got != StatusActive {
		t.Errorf("default status = %v, want %q", got, StatusActive)
	}
}

func TestLayerTriples_SourceAndTimestamp(t *testing.T) {
	ref := sampleRef()
	now := fixedTime()
	triples := LayerTriples(ref, LayerOperatingRhythms, "summary", sampleEntries(), now)
	for _, tr := range triples {
		if tr.Source != TripleSource {
			t.Errorf("triple source = %q, want %q", tr.Source, TripleSource)
		}
		if !tr.Timestamp.Equal(now) {
			t.Errorf("triple timestamp = %v, want %v", tr.Timestamp, now)
		}
		if tr.Confidence <= 0 || tr.Confidence > 1 {
			t.Errorf("triple confidence = %f, want in (0,1]", tr.Confidence)
		}
	}
}

func TestLayerTriples_EmptyEntries(t *testing.T) {
	ref := sampleRef()
	triples := LayerTriples(ref, LayerFriction, "nothing to see", nil, fixedTime())
	layerID := LayerEntityID(ref.Org, ref.Platform, ref.UserID, LayerFriction)
	if countTriples(triples, layerID, PredicateLayerHasEntry) != 0 {
		t.Errorf("expected 0 has_entry links for empty entries")
	}
	profileID := ProfileEntityID(ref.Org, ref.Platform, ref.UserID)
	if !containsTriple(triples, profileID, PredicateProfileVersion) {
		t.Errorf("expected profile version triple even with empty entries")
	}
}

func TestLayerTriples_VersionIsInt64(t *testing.T) {
	// Downstream consumers type-switch on int64 for numeric objects; accidental
	// widening/narrowing changes the graph wire format silently.
	ref := sampleRef()
	triples := LayerTriples(ref, LayerOperatingRhythms, "s", sampleEntries(), fixedTime())
	profileID := ProfileEntityID(ref.Org, ref.Platform, ref.UserID)
	obj := findObject(triples, profileID, PredicateProfileVersion)
	if _, ok := obj.(int64); !ok {
		t.Errorf("PredicateProfileVersion object = %T, want int64", obj)
	}
	layerID := LayerEntityID(ref.Org, ref.Platform, ref.UserID, LayerOperatingRhythms)
	obj = findObject(triples, layerID, PredicateLayerVersion)
	if _, ok := obj.(int64); !ok {
		t.Errorf("PredicateLayerVersion object = %T, want int64", obj)
	}
}

func TestLayerTriples_InputsEncodedAsList(t *testing.T) {
	ref := sampleRef()
	entries := []Entry{{
		EntryID: "e-1", Title: "t", Summary: "s",
		Inputs: []string{"a", "b", "c"},
	}}
	triples := LayerTriples(ref, LayerDependencies, "summary", entries, fixedTime())
	e1ID := EntryEntityID(ref.Org, ref.Platform, "e-1")
	obj := findObject(triples, e1ID, PredicateEntryInputs)
	list, ok := obj.([]any)
	if !ok {
		t.Fatalf("inputs object = %T, want []any", obj)
	}
	if len(list) != 3 {
		t.Fatalf("inputs length = %d, want 3", len(list))
	}
}
