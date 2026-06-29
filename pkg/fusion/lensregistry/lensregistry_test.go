package lensregistry_test

import (
	"context"
	"errors"
	"testing"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/fusion"
	"github.com/c360studio/semstreams/pkg/fusion/lensregistry"
)

// stubLens is a do-nothing Lens for registry tests — the registry stores and
// builds factories; it does not care what the lens does.
type stubLens struct{ name string }

func (s stubLens) Name() string                         { return s.name }
func (stubLens) ResolveMode(string) fusion.ResolveMode  { return fusion.ResolveModeSymbol }
func (stubLens) Edges() []fusion.EdgeSpec               { return nil }
func (stubLens) Label(*fusion.Entity) string            { return "" }
func (stubLens) Kind(*fusion.Entity) string             { return "" }
func (stubLens) Location(*fusion.Entity) fusion.Locator { return fusion.Locator{} }
func (stubLens) Hydrate(context.Context, *fusion.Entity) (*message.StorageReference, error) {
	return nil, nil
}

func TestRegistry_RegisterAndBuild(t *testing.T) {
	r := lensregistry.New()
	if err := r.Register("code", func() (fusion.Lens, error) { return stubLens{name: "code"}, nil }); err != nil {
		t.Fatalf("Register: %v", err)
	}
	lens, err := r.Build("code")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if lens.Name() != "code" {
		t.Errorf("built lens Name = %q, want code", lens.Name())
	}
}

func TestRegistry_DuplicateRegistrationErrors(t *testing.T) {
	r := lensregistry.New()
	if err := r.Register("code", func() (fusion.Lens, error) { return stubLens{name: "code"}, nil }); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	// Second registration under the same name must ERROR — no silent
	// last-write-wins override (the property that distinguishes this from the
	// vocabulary init()-global registry; ADR-062).
	err := r.Register("code", func() (fusion.Lens, error) { return stubLens{name: "other"}, nil })
	if err == nil {
		t.Fatal("expected duplicate registration to error, got nil")
	}
	// The original registration must be intact (override refused, not applied).
	lens, buildErr := r.Build("code")
	if buildErr != nil {
		t.Fatalf("Build after refused override: %v", buildErr)
	}
	if lens.Name() != "code" {
		t.Errorf("override leaked: built lens Name = %q, want the original code", lens.Name())
	}
}

func TestRegistry_RejectsEmptyNameAndNilFactory(t *testing.T) {
	r := lensregistry.New()
	if err := r.Register("", func() (fusion.Lens, error) { return stubLens{}, nil }); err == nil {
		t.Error("expected error on empty name")
	}
	if err := r.Register("x", nil); err == nil {
		t.Error("expected error on nil factory")
	}
}

func TestRegistry_BuildUnregistered(t *testing.T) {
	r := lensregistry.New()
	if _, err := r.Build("missing"); err == nil {
		t.Error("expected error building an unregistered lens")
	}
}

func TestRegistry_BuildSurfacesFactoryError(t *testing.T) {
	r := lensregistry.New()
	sentinel := errors.New("boom")
	_ = r.Register("bad", func() (fusion.Lens, error) { return nil, sentinel })
	_, err := r.Build("bad")
	if err == nil || !errors.Is(err, sentinel) {
		t.Errorf("Build should wrap the factory error, got %v", err)
	}
}

func TestRegistry_BuildRejectsNilLens(t *testing.T) {
	r := lensregistry.New()
	_ = r.Register("nilly", func() (fusion.Lens, error) { return nil, nil })
	if _, err := r.Build("nilly"); err == nil {
		t.Error("expected error when factory returns a nil lens with no error")
	}
}

func TestRegistry_NamesSorted(t *testing.T) {
	r := lensregistry.New()
	for _, n := range []string{"docs", "code", "spec"} {
		name := n
		_ = r.Register(name, func() (fusion.Lens, error) { return stubLens{name: name}, nil })
	}
	got := r.Names()
	want := []string{"code", "docs", "spec"}
	if len(got) != len(want) {
		t.Fatalf("Names = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Names[%d] = %q, want %q (must be sorted)", i, got[i], want[i])
		}
	}
}
