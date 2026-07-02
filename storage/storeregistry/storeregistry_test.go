package storeregistry_test

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/c360studio/semstreams/pkg/fusion"
	"github.com/c360studio/semstreams/storage"
	"github.com/c360studio/semstreams/storage/storeregistry"
)

// fakeStore is a minimal StreamableStore for registry tests. Pointer-backed, so
// two fakes are distinct instances.
type fakeStore struct{ id string }

func (f *fakeStore) Put(context.Context, string, []byte) error      { return nil }
func (f *fakeStore) Get(context.Context, string) ([]byte, error)    { return []byte(f.id), nil }
func (f *fakeStore) List(context.Context, string) ([]string, error) { return nil, nil }
func (f *fakeStore) Delete(context.Context, string) error           { return nil }
func (f *fakeStore) Open(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader([]byte(f.id))), nil
}

var _ storage.StreamableStore = (*fakeStore)(nil)

// The registry must satisfy fusion's StoreResolver so fusion consumes it with
// no adapter (ADR-063 type reconcile).
var _ fusion.StoreResolver = (*storeregistry.Registry)(nil)

func TestRegisterAndResolve(t *testing.T) {
	r := storeregistry.New()
	s := &fakeStore{id: "objectstore"}

	if err := r.Register("objectstore", s); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Streamable resolves the same handle.
	got, ok := r.Streamable("objectstore")
	if !ok || got != s {
		t.Fatalf("Streamable: got (%v,%v), want (%v,true)", got, ok, s)
	}

	// Store() (fusion path) resolves the same underlying store.
	gotStore, ok := r.Store("objectstore")
	if !ok || gotStore != storage.Store(s) {
		t.Fatalf("Store: got (%v,%v), want the same store", gotStore, ok)
	}

	// Unknown instance resolves to (nil,false), not a panic.
	if _, ok := r.Streamable("nope"); ok {
		t.Fatal("Streamable(unknown) returned ok=true")
	}
}

func TestRegisterDuplicateOwnershipErrors(t *testing.T) {
	r := storeregistry.New()
	if err := r.Register("objectstore", &fakeStore{id: "a"}); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	// A second, different component claiming the same instance is a wiring fault.
	err := r.Register("objectstore", &fakeStore{id: "b"})
	if err == nil {
		t.Fatal("duplicate ownership did not error")
	}
	// The first owner is unchanged (no silent clobber).
	got, _ := r.Streamable("objectstore")
	if fs, _ := got.(*fakeStore); fs == nil || fs.id != "a" {
		t.Fatalf("duplicate register clobbered the entry: %v", got)
	}
}

func TestDeregisterThenReregister_Reconfig(t *testing.T) {
	r := storeregistry.New()
	old := &fakeStore{id: "old"}
	if err := r.Register("objectstore", old); err != nil {
		t.Fatalf("Register old: %v", err)
	}

	// Reconfig: Stop deregisters, Start of the fresh instance re-registers. No
	// collision, and the new handle wins.
	r.Deregister("objectstore")
	fresh := &fakeStore{id: "fresh"}
	if err := r.Register("objectstore", fresh); err != nil {
		t.Fatalf("re-register after deregister must succeed, got: %v", err)
	}
	got, _ := r.Streamable("objectstore")
	if got != fresh {
		t.Fatalf("post-reconfig resolve got %v, want the fresh handle", got)
	}
}

func TestDeregisterIsIdempotentAndDoesNotClose(t *testing.T) {
	r := storeregistry.New()
	// Deregistering an absent instance is a no-op, not a panic.
	r.Deregister("absent")
	if len(r.Instances()) != 0 {
		t.Fatalf("registry not empty after no-op deregister: %v", r.Instances())
	}
	// The registry stays usable after a no-op deregister.
	if err := r.Register("x", &fakeStore{id: "x"}); err != nil {
		t.Fatalf("Register after no-op deregister: %v", err)
	}
}

func TestRegisterGuards(t *testing.T) {
	r := storeregistry.New()
	if err := r.Register("", &fakeStore{}); err == nil {
		t.Fatal("empty instance name should error")
	}
	if err := r.Register("x", nil); err == nil {
		t.Fatal("nil store should error")
	}
}

func TestInstancesSnapshot(t *testing.T) {
	r := storeregistry.New()
	_ = r.Register("a", &fakeStore{id: "a"})
	_ = r.Register("b", &fakeStore{id: "b"})
	got := r.Instances()
	if len(got) != 2 {
		t.Fatalf("Instances len = %d, want 2 (%v)", len(got), got)
	}
	// Mutating the snapshot must not affect the registry.
	got[0] = "mutated"
	if len(r.Instances()) != 2 {
		t.Fatal("Instances snapshot is not a copy")
	}
}
