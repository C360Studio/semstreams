package swe

import (
	"strings"
	"testing"
)

// TestIRIsCoverConstants catches drift between the iris.go
// constants and the predicates.go lookup map.
func TestIRIsCoverConstants(t *testing.T) {
	wantPairs := map[string]string{
		Prefix + ":Quantity":      Quantity,
		Prefix + ":Category":      Category,
		Prefix + ":Time":          Time,
		Prefix + ":Count":         Count,
		Prefix + ":Boolean":       Boolean,
		Prefix + ":Text":          Text,
		Prefix + ":QuantityRange": QuantityRange,
		Prefix + ":DataRecord":    DataRecord,
		Prefix + ":DataArray":     DataArray,
		Prefix + ":DataChoice":    DataChoice,
		Prefix + ":Vector":        Vector,

		Prefix + ":label":          Label,
		Prefix + ":definition":     Definition,
		Prefix + ":uom":            UoM,
		Prefix + ":value":          Value,
		Prefix + ":nilValue":       NilValue,
		Prefix + ":referenceFrame": ReferenceFrame,
	}
	got := IRIs()
	if len(got) != len(wantPairs) {
		t.Fatalf("IRIs(): want %d entries, got %d", len(wantPairs), len(got))
	}
	for compact, iri := range wantPairs {
		if gotIRI, ok := got[compact]; !ok {
			t.Errorf("IRIs() missing %q", compact)
		} else if gotIRI != iri {
			t.Errorf("IRIs()[%q] = %q, want %q", compact, gotIRI, iri)
		}
	}
}

func TestConstantsLiveInDeclaredNamespace(t *testing.T) {
	all := []string{
		Quantity, Category, Time, Count, Boolean, Text, QuantityRange,
		DataRecord, DataArray, DataChoice, Vector,
		Label, Definition, UoM, Value, NilValue, ReferenceFrame,
	}
	for _, c := range all {
		if !strings.HasPrefix(c, Namespace) {
			t.Errorf("%q does not start with SWE namespace %q", c, Namespace)
		}
	}
}

func TestIsKnown(t *testing.T) {
	cases := []struct {
		iri  string
		want bool
	}{
		{Quantity, true},
		{Label, true},
		{Namespace + "unmappedButValidSWE", false},
		{"http://example.org/not-swe", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsKnown(c.iri); got != c.want {
			t.Errorf("IsKnown(%q) = %v, want %v", c.iri, got, c.want)
		}
	}
}

func TestLocalName(t *testing.T) {
	cases := []struct {
		iri  string
		want string
	}{
		{Quantity, "Quantity"},
		{UoM, "uom"},
		{Namespace + "unmappedButValidSWE", "unmappedButValidSWE"},
		{"http://example.org/not-swe", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := LocalName(c.iri); got != c.want {
			t.Errorf("LocalName(%q) = %q, want %q", c.iri, got, c.want)
		}
	}
}
