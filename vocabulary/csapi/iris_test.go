package csapi

import (
	"strings"
	"testing"
)

func TestIRIsCoverConstants(t *testing.T) {
	wantPairs := map[string]string{
		Prefix + ":Datastream": Datastream,

		Prefix + ":producedBy":          ProducedBy,
		Prefix + ":resultTimeRange":     ResultTimeRange,
		Prefix + ":phenomenonTimeRange": PhenomenonTimeRange,
		Prefix + ":resultType":          ResultType,
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
		Datastream,
		ProducedBy, ResultTimeRange, PhenomenonTimeRange, ResultType,
	}
	for _, c := range all {
		if !strings.HasPrefix(c, Namespace) {
			t.Errorf("%q does not start with CS API namespace %q", c, Namespace)
		}
	}
}

func TestIsKnown(t *testing.T) {
	cases := []struct {
		iri  string
		want bool
	}{
		{Datastream, true},
		{ProducedBy, true},
		{ResultTimeRange, true},
		{Namespace + "unmappedButValidCSAPI", false},
		{"http://example.org/not-csapi", false},
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
		{Datastream, "Datastream"},
		{ProducedBy, "producedBy"},
		{ResultTimeRange, "resultTimeRange"},
		{Namespace + "unmappedButValidCSAPI", "unmappedButValidCSAPI"},
		{"http://example.org/not-csapi", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := LocalName(c.iri); got != c.want {
			t.Errorf("LocalName(%q) = %q, want %q", c.iri, got, c.want)
		}
	}
}
