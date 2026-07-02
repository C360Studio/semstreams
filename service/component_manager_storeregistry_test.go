package service

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/storage"
	"github.com/c360studio/semstreams/storage/storeregistry"
)

// fakeStreamable is a minimal StreamableStore for manager-wiring tests.
type fakeStreamable struct{ id string }

func (f *fakeStreamable) Put(context.Context, string, []byte) error      { return nil }
func (f *fakeStreamable) Get(context.Context, string) ([]byte, error)    { return []byte(f.id), nil }
func (f *fakeStreamable) List(context.Context, string) ([]string, error) { return nil, nil }
func (f *fakeStreamable) Delete(context.Context, string) error           { return nil }
func (f *fakeStreamable) Open(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader([]byte(f.id))), nil
}

// storeProviderComponent is a Discoverable that also owns a store (ADR-063).
type storeProviderComponent struct {
	mockDiscoverableComponent
	provided map[string]storage.StreamableStore
}

func (s *storeProviderComponent) ProvidedStores() map[string]storage.StreamableStore {
	return s.provided
}

var _ component.StoreProvider = (*storeProviderComponent)(nil)

func newStoreRegistryTestManager() *ComponentManager {
	return &ComponentManager{
		BaseService:   NewBaseServiceWithOptions("component-manager", nil),
		components:    make(map[string]*component.ManagedComponent),
		registry:      component.NewRegistry(),
		storeRegistry: storeregistry.New(),
		storeProvided: make(map[string][]string),
	}
}

func provider(instance string) *storeProviderComponent {
	return &storeProviderComponent{
		provided: map[string]storage.StreamableStore{instance: &fakeStreamable{id: instance}},
	}
}

// registerStarted mirrors the real start path: the component is tracked as
// StateStarted (the liveness precondition registerProvidedStores enforces)
// before its stores are registered.
func registerStarted(cm *ComponentManager, name string, comp component.Discoverable) {
	cm.components[name] = &component.ManagedComponent{Component: comp, State: component.StateStarted}
	cm.registerProvidedStores(name, comp)
}

func TestRegisterProvidedStores_PopulatesRegistry(t *testing.T) {
	cm := newStoreRegistryTestManager()
	registerStarted(cm, "objectstore", provider("objectstore"))

	if _, ok := cm.storeRegistry.Streamable("objectstore"); !ok {
		t.Fatal("store not registered after start")
	}
	// A non-provider component is a silent no-op (not every component owns a store).
	registerStarted(cm, "plain", &mockDiscoverableComponent{})
	if got := len(cm.storeRegistry.Instances()); got != 1 {
		t.Fatalf("registry has %d instances, want 1", got)
	}
}

func TestRegisterProvidedStores_SkipsWhenNotLive(t *testing.T) {
	cm := newStoreRegistryTestManager()
	// A component that started but was already removed/halted by a concurrent
	// stop/reconfig (not tracked as StateStarted) must NOT register — otherwise a
	// late register shadows a teardown that already deregistered (ADR-063 M1).
	cm.registerProvidedStores("ghost", provider("ghost"))
	if _, ok := cm.storeRegistry.Streamable("ghost"); ok {
		t.Fatal("registered a store for an untracked/non-live component")
	}
}

func TestDeregisterProvidedStores_ClearsRegistry(t *testing.T) {
	cm := newStoreRegistryTestManager()
	registerStarted(cm, "objectstore", provider("objectstore"))
	cm.deregisterProvidedStores("objectstore")

	if _, ok := cm.storeRegistry.Streamable("objectstore"); ok {
		t.Fatal("store still registered after stop")
	}
	// Tracking is cleared, so a redundant deregister is a no-op.
	cm.deregisterProvidedStores("objectstore")
}

func TestDuplicateOwnership_RefusedNotClobbered(t *testing.T) {
	cm := newStoreRegistryTestManager()
	first := provider("objectstore")
	registerStarted(cm, "owner-a", first)

	// A second live component claims the same instance — refused, incumbent kept.
	registerStarted(cm, "owner-b", provider("objectstore"))

	got, _ := cm.storeRegistry.Streamable("objectstore")
	if fs, _ := got.(*fakeStreamable); fs == nil || fs != first.provided["objectstore"] {
		t.Fatalf("duplicate ownership clobbered the incumbent store: %v", got)
	}
	// owner-b registered nothing, so deregistering it must not evict owner-a's store.
	cm.deregisterProvidedStores("owner-b")
	if _, ok := cm.storeRegistry.Streamable("objectstore"); !ok {
		t.Fatal("deregistering the refused owner evicted the incumbent")
	}
}

func TestReconfigSwapsHandle(t *testing.T) {
	cm := newStoreRegistryTestManager()

	oldComp := provider("objectstore")
	registerStarted(cm, "objectstore", oldComp)

	// Reconfig: deregister the old instance, then register the fresh one under the
	// same StorageInstance. No collision, and the new handle wins.
	cm.deregisterProvidedStores("objectstore")
	freshComp := provider("objectstore")
	registerStarted(cm, "objectstore", freshComp)

	got, _ := cm.storeRegistry.Streamable("objectstore")
	if got != freshComp.provided["objectstore"] {
		t.Fatal("post-reconfig resolve did not return the fresh handle")
	}
}
