package bfo

import (
	"testing"
)

func TestSubClassOf_RootHasNoParent(t *testing.T) {
	if _, ok := SubClassOf[Entity]; ok {
		t.Fatal("Entity is the root and must not appear in SubClassOf")
	}
	if got := Parents(Entity); got != nil {
		t.Fatalf("Parents(Entity) = %v, want nil", got)
	}
}

func TestSubClassOf_EveryParentIsReachableAndAcyclic(t *testing.T) {
	// Every class in the map must transitively reach Entity, and no class may be
	// its own ancestor (the walk's cycle guard would otherwise loop).
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
		if last := parents[len(parents)-1]; last != Entity {
			t.Errorf("Parents(%s) does not terminate at Entity; got root %s", child, last)
		}
	}
}

func TestParents_NearestFirstToRoot(t *testing.T) {
	got := Parents(Object)
	want := []string{MaterialEntity, IndependentContinuant, Continuant, Entity}
	if len(got) != len(want) {
		t.Fatalf("Parents(Object) = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Parents(Object)[%d] = %s, want %s (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestIsA(t *testing.T) {
	cases := []struct {
		child, parent string
		want          bool
	}{
		{Object, Object, true},             // reflexive
		{Object, MaterialEntity, true},     // direct
		{Object, Continuant, true},         // transitive
		{Object, Entity, true},             // to root
		{Function, RealizableEntity, true}, // Function → Disposition → RealizableEntity
		{Function, SpecificallyDependentContinuant, true},
		{Object, Occurrent, false},   // cross-branch (continuant vs occurrent)
		{Process, Continuant, false}, // occurrent is not a continuant
		{History, Process, true},     // history is a process (BFO 2020)
		{History, Occurrent, true},
		{Entity, Object, false}, // root is not a subclass of a leaf
	}
	for _, c := range cases {
		if got := IsA(c.child, c.parent); got != c.want {
			t.Errorf("IsA(%s, %s) = %v, want %v", c.child, c.parent, got, c.want)
		}
	}
}

func TestUnknownIRI(t *testing.T) {
	const unknown = "http://example.com/NotABFOClass"
	if got := Parents(unknown); got != nil {
		t.Errorf("Parents(unknown) = %v, want nil", got)
	}
	if IsA(unknown, Entity) {
		t.Error("IsA(unknown, Entity) = true, want false")
	}
	if !IsA(unknown, unknown) {
		t.Error("IsA(unknown, unknown) = false, want true (reflexive)")
	}
}
