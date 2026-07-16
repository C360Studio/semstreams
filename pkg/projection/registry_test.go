package projection

import (
	"errors"
	"testing"

	"github.com/c360studio/semstreams/pkg/ownership"
)

func TestRegistry(t *testing.T) {
	resetRegistry()
	t.Cleanup(resetRegistry)

	if err := Register(csapiSystem(t)); err != nil {
		t.Fatalf("register: %v", err)
	}

	got, ok := Lookup("cs-api.system")
	if !ok || got.EntityPattern != sysPat {
		t.Errorf("lookup after register failed: ok=%v pattern=%q", ok, got.EntityPattern)
	}

	// Duplicate name is rejected.
	if err := Register(csapiSystem(t)); !errors.Is(err, ErrInvalidContract) {
		t.Errorf("duplicate registration should error, got %v", err)
	}

	// Invalid contract is rejected at registration time.
	if err := Register(Contract{Name: "bad"}); !errors.Is(err, ErrInvalidContract) {
		t.Errorf("invalid contract registration should error, got %v", err)
	}

	if got := Registered(); len(got) != 1 || got[0].Name != "cs-api.system" {
		t.Errorf("Registered() = %d contracts, want exactly cs-api.system", len(got))
	}
}

func TestRegistered_SortedDeterministic(t *testing.T) {
	resetRegistry()
	t.Cleanup(resetRegistry)

	mk := func(name, typeSeg string) Contract {
		return Contract{
			Name:          name,
			EntityPattern: "c360.semconnect.systems.csapi." + typeSeg + ".*",
			Groups:        []PredicateGroup{{Mode: ownership.ModeReplaceOwned, Predicates: []string{"test.value.p"}}},
		}
	}
	for _, c := range []Contract{mk("z", "ztype"), mk("a", "atype"), mk("m", "mtype")} {
		if err := Register(c); err != nil {
			t.Fatal(err)
		}
	}
	got := Registered()
	want := []string{"a", "m", "z"}
	for i, c := range got {
		if c.Name != want[i] {
			t.Errorf("Registered()[%d] = %q, want %q (name-sorted)", i, c.Name, want[i])
		}
	}
}

func TestMustRegister_PanicsOnDuplicate(t *testing.T) {
	resetRegistry()
	t.Cleanup(resetRegistry)

	MustRegister(csapiSystem(t))
	defer func() {
		if recover() == nil {
			t.Error("MustRegister should panic on a duplicate name")
		}
	}()
	MustRegister(csapiSystem(t))
}
