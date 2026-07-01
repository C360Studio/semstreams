package fusionvocab

import (
	"testing"

	"github.com/c360studio/semstreams/vocabulary"
	"github.com/c360studio/semstreams/vocabulary/bfo"
	"github.com/c360studio/semstreams/vocabulary/cco"
)

func TestClassSpecificity(t *testing.T) {
	s := New()
	cases := []struct {
		iri  string
		want float64
	}{
		{"", 0},                             // unset
		{"http://example.com/NotAClass", 0}, // unknown
		{bfo.Entity, 0},                     // root: no ancestors
		{bfo.Object, 4},                     // Object→MaterialEntity→IndependentContinuant→Continuant→Entity
		{cco.Sensor, 5},                     // Sensor→Artifact→MaterialEntity→IndependentContinuant→Continuant→Entity
		{cco.Requirement, 5},                // Requirement→DirectiveICE→ICE→GDC→Continuant→Entity
	}
	for _, c := range cases {
		if got := s.ClassSpecificity(c.iri); got != c.want {
			t.Errorf("ClassSpecificity(%q) = %v, want %v", c.iri, got, c.want)
		}
	}
	// Monotonicity: a subclass is strictly more specific than its ancestor.
	if s.ClassSpecificity(cco.Sensor) <= s.ClassSpecificity(cco.Artifact) {
		t.Error("Sensor should be more specific than its ancestor Artifact")
	}
}

func TestPredicateSalience(t *testing.T) {
	// Save/restore the global registry so the test is isolated.
	t.Cleanup(vocabulary.SnapshotRegistry())

	vocabulary.Register("test.identity.serial", vocabulary.WithWeight(2.5))
	vocabulary.Register("test.meta.updated") // no weight → 0

	s := New()
	if got := s.PredicateSalience("test.identity.serial"); got != 2.5 {
		t.Errorf("PredicateSalience(weighted) = %v, want 2.5", got)
	}
	if got := s.PredicateSalience("test.meta.updated"); got != 0 {
		t.Errorf("PredicateSalience(unweighted) = %v, want 0", got)
	}
	if got := s.PredicateSalience("test.unregistered.field"); got != 0 {
		t.Errorf("PredicateSalience(unregistered) = %v, want 0", got)
	}
}
