package cco

import (
	"testing"

	"github.com/c360studio/semstreams/vocabulary/bfo"
)

func TestSubClassOf_EveryClassReachesBFORoot(t *testing.T) {
	// Every CCO class must transitively reach bfo.Entity through its BFO anchor,
	// and must not be its own ancestor.
	for child := range SubClassOf {
		parents := Parents(child)
		if len(parents) == 0 {
			t.Errorf("%s has no parents", child)
			continue
		}
		for _, p := range parents {
			if p == child {
				t.Errorf("%s is its own ancestor (cycle)", child)
			}
		}
		if last := parents[len(parents)-1]; last != bfo.Entity {
			t.Errorf("Parents(%s) does not terminate at bfo.Entity; got root %s", child, last)
		}
	}
}

func TestParents_CrossesIntoBFO(t *testing.T) {
	// A directive ICE walks up through CCO then anchors into BFO.
	got := Parents(Requirement)
	want := []string{
		DirectiveInformationContentEntity,
		InformationContentEntity,
		bfo.GenericallyDependentContinuant,
		bfo.Continuant,
		bfo.Entity,
	}
	if len(got) != len(want) {
		t.Fatalf("Parents(Requirement) = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Parents(Requirement)[%d] = %s, want %s (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestIsA_WithinCCO(t *testing.T) {
	cases := []struct {
		child, parent string
		want          bool
	}{
		{Requirement, Requirement, true},                       // reflexive
		{Requirement, DirectiveInformationContentEntity, true}, // direct
		{Requirement, InformationContentEntity, true},          // transitive
		{Person, Agent, true},                                  // agent branch
		{Sensor, Artifact, true},                               // artifact branch
		{Document, InformationBearingArtifact, true},
		{ActOfObserving, IntentionalAct, true}, // act branch
		{ActOfObserving, Act, true},
		{Requirement, Agent, false},               // cross-branch
		{Sensor, InformationContentEntity, false}, // artifact is not an ICE
	}
	for _, c := range cases {
		if got := IsA(c.child, c.parent); got != c.want {
			t.Errorf("IsA(%s, %s) = %v, want %v", c.child, c.parent, got, c.want)
		}
	}
}

func TestIsA_AcrossCCOtoBFO(t *testing.T) {
	// CCO classes IsA their BFO anchors (the whole point of the cross-ontology
	// walk): an ICE is a generically dependent continuant, an agent is a material
	// entity, an act is a process, an artifact is a material entity.
	cases := []struct {
		child, bfoParent string
	}{
		{Requirement, bfo.GenericallyDependentContinuant},
		{Requirement, bfo.Continuant},
		{Requirement, bfo.Entity},
		{Person, bfo.MaterialEntity},
		{ActOfObserving, bfo.Process},
		{ActOfObserving, bfo.Occurrent},
		{Sensor, bfo.MaterialEntity},
		{Sensor, bfo.IndependentContinuant},
	}
	for _, c := range cases {
		if !IsA(c.child, c.bfoParent) {
			t.Errorf("IsA(%s, %s) = false, want true (CCO→BFO)", c.child, c.bfoParent)
		}
	}
	// And NOT across branches: an agent (a material entity / continuant) is not a
	// process, and NOT an Object — CCO anchors Agent at MaterialEntity, the
	// parent of Object (a group of agents is an ObjectAggregate, an Object
	// sibling), so the agent branch must not subsume under Object.
	if IsA(Person, bfo.Process) {
		t.Error("IsA(Person, bfo.Process) = true, want false")
	}
	if IsA(Person, bfo.Object) {
		t.Error("IsA(Person, bfo.Object) = true, want false (Agent anchors at MaterialEntity, not Object)")
	}
}

func TestUnknownIRI(t *testing.T) {
	const unknown = "http://example.com/NotACCOClass"
	if got := Parents(unknown); got != nil {
		t.Errorf("Parents(unknown) = %v, want nil", got)
	}
	if IsA(unknown, bfo.Entity) {
		t.Error("IsA(unknown, bfo.Entity) = true, want false")
	}
}
