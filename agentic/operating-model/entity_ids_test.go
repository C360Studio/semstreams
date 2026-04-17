package operatingmodel

import (
	"strings"
	"testing"

	"github.com/c360studio/semstreams/message"
)

func TestProfileEntityID_Valid(t *testing.T) {
	id := ProfileEntityID("c360", "ops", "user-abc")
	if want := "c360.ops.user.teams.profile.user-abc"; id != want {
		t.Fatalf("ProfileEntityID = %q, want %q", id, want)
	}
	if !message.IsValidEntityID(id) {
		t.Fatalf("ProfileEntityID %q fails IsValidEntityID", id)
	}
}

func TestLayerEntityID_Valid(t *testing.T) {
	id := LayerEntityID("c360", "ops", "user-abc", LayerFriction)
	if want := "c360.ops.user.teams.om-layer.user-abc-friction"; id != want {
		t.Fatalf("LayerEntityID = %q, want %q", id, want)
	}
	if !message.IsValidEntityID(id) {
		t.Fatalf("LayerEntityID %q fails IsValidEntityID", id)
	}
}

func TestEntryEntityID_Valid(t *testing.T) {
	id := EntryEntityID("c360", "ops", "entry-123")
	if want := "c360.ops.user.teams.om-entry.entry-123"; id != want {
		t.Fatalf("EntryEntityID = %q, want %q", id, want)
	}
	if !message.IsValidEntityID(id) {
		t.Fatalf("EntryEntityID %q fails IsValidEntityID", id)
	}
}

func TestEntityID_AllCanonicalLayers(t *testing.T) {
	for _, layer := range Layers() {
		id := LayerEntityID("c360", "ops", "u1", layer)
		if !message.IsValidEntityID(id) {
			t.Errorf("layer %q produced invalid entity ID %q", layer, id)
		}
	}
}

func TestEntityID_PanicsOnEmpty(t *testing.T) {
	cases := []struct {
		name string
		call func()
	}{
		{"profile empty org", func() { ProfileEntityID("", "ops", "u1") }},
		{"profile empty platform", func() { ProfileEntityID("c360", "", "u1") }},
		{"profile empty userID", func() { ProfileEntityID("c360", "ops", "") }},
		{"layer empty layer", func() { LayerEntityID("c360", "ops", "u1", "") }},
		{"entry empty entryID", func() { EntryEntityID("c360", "ops", "") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Fatalf("expected panic, got nil")
				}
			}()
			tc.call()
		})
	}
}

func TestEntityID_PanicsOnDotInPart(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic for dot in userID")
		} else if s, _ := r.(string); !strings.Contains(s, "dot") {
			t.Fatalf("expected panic message to mention dot, got %q", s)
		}
	}()
	ProfileEntityID("c360", "ops", "user.with.dot")
}

func TestLayerEntityID_PanicsOnInvalidLayer(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic for invalid layer name")
		}
	}()
	LayerEntityID("c360", "ops", "u1", "not_a_real_layer")
}

func TestLayers_CanonicalOrderAndCount(t *testing.T) {
	got := Layers()
	want := []string{
		LayerOperatingRhythms,
		LayerRecurringDecisions,
		LayerDependencies,
		LayerInstitutionalKnowledge,
		LayerFriction,
	}
	if len(got) != len(want) {
		t.Fatalf("Layers() len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Layers()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestLayers_IsCopy(t *testing.T) {
	a := Layers()
	a[0] = "mutated"
	b := Layers()
	if b[0] == "mutated" {
		t.Fatalf("Layers() returned a shared slice; mutation leaked")
	}
}

func TestIsValidLayer(t *testing.T) {
	for _, layer := range Layers() {
		if !IsValidLayer(layer) {
			t.Errorf("IsValidLayer(%q) = false, want true", layer)
		}
	}
	if IsValidLayer("") {
		t.Error("IsValidLayer(\"\") = true, want false")
	}
	if IsValidLayer("bogus_layer") {
		t.Error("IsValidLayer(\"bogus_layer\") = true, want false")
	}
}
