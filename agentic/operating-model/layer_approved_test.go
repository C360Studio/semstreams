package operatingmodel

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func validLayerApproved() *LayerApproved {
	return &LayerApproved{
		UserID:            "coby",
		LoopID:            "loop-abc",
		Layer:             LayerOperatingRhythms,
		ProfileVersion:    1,
		CheckpointSummary: "Weekly rhythms established",
		Entries:           []Entry{{EntryID: "e-1", Title: "t", Summary: "s"}},
		ApprovedAt:        fixedTime(),
	}
}

func TestLayerApproved_Schema(t *testing.T) {
	p := validLayerApproved()
	got := p.Schema()
	if got.Domain != Domain || got.Category != CategoryLayerApproved || got.Version != SchemaVersion {
		t.Errorf("Schema = %+v", got)
	}
}

func TestLayerApproved_Validate_Success(t *testing.T) {
	p := validLayerApproved()
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate = %v, want nil", err)
	}
}

func TestLayerApproved_Validate_Errors(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*LayerApproved)
		wantIn string
	}{
		{"missing user", func(p *LayerApproved) { p.UserID = "" }, "user_id"},
		{"dot in user", func(p *LayerApproved) { p.UserID = "a.b" }, "dots"},
		{"missing loop", func(p *LayerApproved) { p.LoopID = "" }, "loop_id"},
		{"invalid layer", func(p *LayerApproved) { p.Layer = "bogus" }, "canonical layer name"},
		{"bad version", func(p *LayerApproved) { p.ProfileVersion = 0 }, "profile_version"},
		{"missing summary", func(p *LayerApproved) { p.CheckpointSummary = "" }, "checkpoint_summary"},
		{"empty entries", func(p *LayerApproved) { p.Entries = nil }, "at least one entry"},
		{"invalid entry", func(p *LayerApproved) { p.Entries[0].Title = "" }, "entries[0]"},
		{"duplicate entry_id", func(p *LayerApproved) {
			p.Entries = []Entry{
				{EntryID: "x", Title: "a", Summary: "a"},
				{EntryID: "x", Title: "b", Summary: "b"},
			}
		}, "duplicated"},
		{"missing approved_at", func(p *LayerApproved) { p.ApprovedAt = time.Time{} }, "approved_at required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := validLayerApproved()
			tc.mutate(p)
			err := p.Validate()
			if err == nil {
				t.Fatalf("Validate = nil, want error containing %q", tc.wantIn)
			}
			if !strings.Contains(err.Error(), tc.wantIn) {
				t.Errorf("Validate error = %q, want substring %q", err, tc.wantIn)
			}
		})
	}
}

func TestLayerApproved_JSONRoundTrip(t *testing.T) {
	p := validLayerApproved()
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var p2 LayerApproved
	if err := json.Unmarshal(data, &p2); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if p2.UserID != p.UserID || p2.Layer != p.Layer || len(p2.Entries) != len(p.Entries) {
		t.Errorf("round-tripped payload mismatch: got=%+v want=%+v", p2, *p)
	}
	if !p2.ApprovedAt.Equal(p.ApprovedAt) {
		t.Errorf("approved_at round-trip: got=%v want=%v", p2.ApprovedAt, p.ApprovedAt)
	}
}

func TestLayerApproved_Triples_IncludesAllLayers(t *testing.T) {
	p := validLayerApproved()
	triples := p.Triples("c360", "ops")

	profileID := ProfileEntityID("c360", "ops", "coby")
	layerID := LayerEntityID("c360", "ops", "coby", LayerOperatingRhythms)

	if !containsTriple(triples, profileID, PredicateProfileVersion) {
		t.Errorf("Triples missing profile version")
	}
	if !containsTriple(triples, layerID, PredicateLayerCheckpointSummary) {
		t.Errorf("Triples missing checkpoint summary")
	}
}

func TestLayerApproved_Triples_UsesApprovedAt(t *testing.T) {
	p := validLayerApproved()
	p.ApprovedAt = fixedTime()
	triples := p.Triples("c360", "ops")
	for _, tr := range triples {
		if !tr.Timestamp.Equal(p.ApprovedAt) {
			t.Errorf("triple timestamp = %v, want %v", tr.Timestamp, p.ApprovedAt)
		}
	}
}

func TestLayerApproved_Triples_TotalCount(t *testing.T) {
	// Asserts cardinality for a known input: 1 layer with 1 fully-populated
	// entry. Changing the triple surface should be an explicit decision that
	// also updates this test.
	p := validLayerApproved()
	p.Entries = []Entry{{
		EntryID:          "e-1",
		Title:            "t",
		Summary:          "s",
		Cadence:          "weekly",
		Trigger:          "Monday",
		Inputs:           []string{"a"},
		Stakeholders:     []string{"b"},
		Constraints:      []string{"c"},
		SourceConfidence: ConfidenceConfirmed,
		Status:           StatusActive,
	}}
	triples := p.Triples("c360", "ops")

	// Profile root: 3 (version, last_updated, has_layer)
	// Layer body: 3 (name, checkpoint_summary, version)
	// Entry link: 1 (has_entry)
	// Entry body: 9 (title, summary, source_confidence, status, cadence, trigger, inputs, stakeholders, constraints)
	const want = 3 + 3 + 1 + 9
	if got := len(triples); got != want {
		t.Errorf("len(triples) = %d, want %d", got, want)
	}
}
