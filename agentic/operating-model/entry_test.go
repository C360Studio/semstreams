package operatingmodel

import (
	"strings"
	"testing"
)

func validEntry() Entry {
	return Entry{
		EntryID: "entry-001",
		Title:   "Weekly planning block",
		Summary: "Mondays 9-10am calendar-blocked for planning",
	}
}

func TestEntry_Validate_Success(t *testing.T) {
	e := validEntry()
	if err := e.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestEntry_Validate_AllOptionalFields(t *testing.T) {
	e := validEntry()
	e.Cadence = "weekly"
	e.Trigger = "Monday 9am"
	e.Inputs = []string{"retro notes"}
	e.Stakeholders = []string{"self"}
	e.Constraints = []string{"no meetings"}
	e.SourceConfidence = ConfidenceConfirmed
	e.Status = StatusActive
	if err := e.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestEntry_Validate_RequiredFields(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Entry)
		wantIn string
	}{
		{"missing entry_id", func(e *Entry) { e.EntryID = "" }, "entry_id required"},
		{"whitespace entry_id", func(e *Entry) { e.EntryID = "   " }, "entry_id required"},
		{"dot in entry_id", func(e *Entry) { e.EntryID = "entry.001" }, "must not contain dots"},
		{"missing title", func(e *Entry) { e.Title = "" }, "title required"},
		{"missing summary", func(e *Entry) { e.Summary = "" }, "summary required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := validEntry()
			tc.mutate(&e)
			err := e.Validate()
			if err == nil {
				t.Fatalf("Validate() = nil, want error")
			}
			if !strings.Contains(err.Error(), tc.wantIn) {
				t.Errorf("Validate() err = %q, want substring %q", err, tc.wantIn)
			}
		})
	}
}

func TestEntry_Validate_EnumFields(t *testing.T) {
	e := validEntry()
	e.SourceConfidence = "bogus"
	if err := e.Validate(); err == nil {
		t.Error("Validate() = nil for invalid source_confidence")
	}

	e = validEntry()
	e.Status = "bogus"
	if err := e.Validate(); err == nil {
		t.Error("Validate() = nil for invalid status")
	}
}

func TestEntry_Defaults(t *testing.T) {
	e := validEntry()
	if got := e.ResolvedSourceConfidence(); got != ConfidenceConfirmed {
		t.Errorf("ResolvedSourceConfidence default = %q, want %q", got, ConfidenceConfirmed)
	}
	if got := e.ResolvedStatus(); got != StatusActive {
		t.Errorf("ResolvedStatus default = %q, want %q", got, StatusActive)
	}

	e.SourceConfidence = ConfidenceSynthesized
	e.Status = StatusUnresolved
	if got := e.ResolvedSourceConfidence(); got != ConfidenceSynthesized {
		t.Errorf("ResolvedSourceConfidence explicit = %q, want %q", got, ConfidenceSynthesized)
	}
	if got := e.ResolvedStatus(); got != StatusUnresolved {
		t.Errorf("ResolvedStatus explicit = %q, want %q", got, StatusUnresolved)
	}
}
