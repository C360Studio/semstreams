package oms

import (
	"strings"
	"testing"
)

func TestIRIsCoverConstants(t *testing.T) {
	wantPairs := map[string]string{
		Prefix + ":Observation":        Observation,
		Prefix + ":ObservableProperty": ObservableProperty,
		Prefix + ":Procedure":          Procedure,
		Prefix + ":FeatureOfInterest":  FeatureOfInterest,
		Prefix + ":Result":             Result,

		Prefix + ":resultTime":           ResultTime,
		Prefix + ":phenomenonTime":       PhenomenonTime,
		Prefix + ":hasResult":            HasResult,
		Prefix + ":hasFeatureOfInterest": HasFeatureOfInterest,
		Prefix + ":observedProperty":     ObservedProperty,
		Prefix + ":usedProcedure":        UsedProcedure,
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
		Observation, ObservableProperty, Procedure, FeatureOfInterest, Result,
		ResultTime, PhenomenonTime, HasResult, HasFeatureOfInterest,
		ObservedProperty, UsedProcedure,
	}
	for _, c := range all {
		if !strings.HasPrefix(c, Namespace) {
			t.Errorf("%q does not start with OMS namespace %q", c, Namespace)
		}
	}
}

func TestIsKnown(t *testing.T) {
	cases := []struct {
		iri  string
		want bool
	}{
		{Observation, true},
		{ResultTime, true},
		{Namespace + "unmappedButValidOMS", false},
		{"http://example.org/not-oms", false},
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
		{Observation, "Observation"},
		{ResultTime, "resultTime"},
		{Namespace + "unmappedButValidOMS", "unmappedButValidOMS"},
		{"http://example.org/not-oms", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := LocalName(c.iri); got != c.want {
			t.Errorf("LocalName(%q) = %q, want %q", c.iri, got, c.want)
		}
	}
}
