package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/storage"
	"github.com/c360studio/semstreams/storage/storeregistry"
	"github.com/c360studio/semstreams/types"
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

type stopObservingProvider struct {
	*barrierStoreProvider
	onStop  func() error
	stopped bool
}

func (p *stopObservingProvider) Stop(time.Duration) error {
	p.stopped = true
	if p.onStop == nil {
		return nil
	}
	return p.onStop()
}

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
func registerStarted(cm *ComponentManager, name string, comp component.Discoverable) error {
	cm.components[name] = &component.ManagedComponent{Component: comp, State: component.StateStarted}
	return cm.registerProvidedStores(name, comp)
}

func TestRegisterProvidedStores_PopulatesRegistry(t *testing.T) {
	cm := newStoreRegistryTestManager()
	if err := registerStarted(cm, "objectstore", provider("objectstore")); err != nil {
		t.Fatalf("register provided store: %v", err)
	}

	if _, ok := cm.storeRegistry.Streamable("objectstore"); !ok {
		t.Fatal("store not registered after start")
	}
	// A non-provider component is a silent no-op (not every component owns a store).
	if err := registerStarted(cm, "plain", &mockDiscoverableComponent{}); err != nil {
		t.Fatalf("register non-provider: %v", err)
	}
	if got := len(cm.storeRegistry.Instances()); got != 1 {
		t.Fatalf("registry has %d instances, want 1", got)
	}
}

func TestRegisterProvidedStores_SkipsWhenNotLive(t *testing.T) {
	cm := newStoreRegistryTestManager()
	// A component that started but was already removed/halted by a concurrent
	// stop/reconfig (not tracked as StateStarted) must NOT register — otherwise a
	// late register shadows a teardown that already deregistered (ADR-063 M1).
	if err := cm.registerProvidedStores("ghost", provider("ghost")); err != nil {
		t.Fatalf("untracked provider returned an error: %v", err)
	}
	if _, ok := cm.storeRegistry.Streamable("ghost"); ok {
		t.Fatal("registered a store for an untracked/non-live component")
	}
}

func TestDeregisterProvidedStores_ClearsRegistry(t *testing.T) {
	cm := newStoreRegistryTestManager()
	if err := registerStarted(cm, "objectstore", provider("objectstore")); err != nil {
		t.Fatalf("register provided store: %v", err)
	}
	cm.deregisterProvidedStores("objectstore")

	if _, ok := cm.storeRegistry.Streamable("objectstore"); ok {
		t.Fatal("store still registered after stop")
	}
	// Tracking is cleared, so a redundant deregister is a no-op.
	cm.deregisterProvidedStores("objectstore")
}

func TestStopLifecycleComponent_DeregistersBeforeProviderStop(t *testing.T) {
	cm := newStoreRegistryTestManager()
	provider := &stopObservingProvider{
		barrierStoreProvider: &barrierStoreProvider{
			barrierTestComponent: newBarrierTestComponent("objectstore"),
			provided: map[string]storage.StreamableStore{
				"objectstore": &fakeStreamable{id: "objectstore"},
			},
		},
	}
	provider.onStop = func() error {
		if _, ok := cm.storeRegistry.Streamable("objectstore"); ok {
			return errors.New("store remained registered when provider Stop began")
		}
		return nil
	}
	if err := registerStarted(cm, "objectstore", provider); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	if err := cm.stopLifecycleComponent(context.Background(), "objectstore", provider); err != nil {
		t.Fatalf("stop lifecycle provider: %v", err)
	}
}

func TestDuplicateOwnership_RefusedNotClobbered(t *testing.T) {
	cm := newStoreRegistryTestManager()
	first := provider("objectstore")
	if err := registerStarted(cm, "owner-a", first); err != nil {
		t.Fatalf("register incumbent: %v", err)
	}

	// A second live component claims the same instance — refused, incumbent kept.
	err := registerStarted(cm, "owner-b", provider("objectstore"))
	if err == nil {
		t.Fatal("duplicate ownership did not return a provider startup error")
	}

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

func TestDuplicateOwnership_RollsBackOnlyRivalRegistrations(t *testing.T) {
	cm := newStoreRegistryTestManager()
	incumbent := provider("shared")
	if err := registerStarted(cm, "owner-a", incumbent); err != nil {
		t.Fatalf("register incumbent: %v", err)
	}
	rival := &storeProviderComponent{provided: map[string]storage.StreamableStore{
		"a-rival-only": &fakeStreamable{id: "rival-only"},
		"shared":       &fakeStreamable{id: "rival-shared"},
	}}

	if err := registerStarted(cm, "owner-b", rival); err == nil {
		t.Fatal("duplicate ownership did not return an error")
	}
	if _, ok := cm.storeRegistry.Streamable("a-rival-only"); ok {
		t.Fatal("failed rival left an earlier successful claim registered")
	}
	got, ok := cm.storeRegistry.Streamable("shared")
	if !ok || got != incumbent.provided["shared"] {
		t.Fatalf("rival rollback removed or replaced incumbent: (%v, %v)", got, ok)
	}
}

func TestReconfigSwapsHandle(t *testing.T) {
	cm := newStoreRegistryTestManager()

	oldComp := provider("objectstore")
	if err := registerStarted(cm, "objectstore", oldComp); err != nil {
		t.Fatalf("register old store: %v", err)
	}

	// Reconfig: deregister the old instance, then register the fresh one under the
	// same StorageInstance. No collision, and the new handle wins.
	cm.deregisterProvidedStores("objectstore")
	freshComp := provider("objectstore")
	if err := registerStarted(cm, "objectstore", freshComp); err != nil {
		t.Fatalf("register fresh store: %v", err)
	}

	got, _ := cm.storeRegistry.Streamable("objectstore")
	if got != freshComp.provided["objectstore"] {
		t.Fatal("post-reconfig resolve did not return the fresh handle")
	}
}

func TestRegisterProvidedStores_InvalidClaimFailsAtomically(t *testing.T) {
	cm := newStoreRegistryTestManager()
	valid := &fakeStreamable{id: "valid"}
	comp := &storeProviderComponent{provided: map[string]storage.StreamableStore{
		"valid": valid,
		"":      &fakeStreamable{id: "invalid"},
	}}
	cm.components["invalid-provider"] = &component.ManagedComponent{
		Component: comp,
		State:     component.StateStarted,
	}

	err := cm.registerProvidedStores("invalid-provider", comp)
	if err == nil {
		t.Fatal("empty StorageInstance did not return an error")
	}
	if _, ok := cm.storeRegistry.Streamable("valid"); ok {
		t.Fatal("invalid provider left a partially registered store")
	}
	if got := cm.storeProvided["invalid-provider"]; len(got) != 0 {
		t.Fatalf("invalid provider left registration tracking: %v", got)
	}
}

func TestRegisterProvidedStores_NilClaimFails(t *testing.T) {
	var typedNil *fakeStreamable
	for _, tc := range []struct {
		name  string
		store storage.StreamableStore
	}{
		{name: "nil interface", store: nil},
		{name: "typed nil", store: typedNil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cm := newStoreRegistryTestManager()
			comp := &storeProviderComponent{
				provided: map[string]storage.StreamableStore{"nil-store": tc.store},
			}
			cm.components["nil-provider"] = &component.ManagedComponent{
				Component: comp,
				State:     component.StateStarted,
			}

			if err := cm.registerProvidedStores("nil-provider", comp); err == nil {
				t.Fatal("nil store did not return an error")
			}
			if _, ok := cm.storeRegistry.Streamable("nil-store"); ok {
				t.Fatal("nil store was registered")
			}
		})
	}
}

func TestDynamicProviderDuplicateFailsAndRemainsVisible(t *testing.T) {
	cm := newStoreRegistryTestManager()
	client, err := natsclient.NewClient("nats://localhost:4222")
	if err != nil {
		t.Fatalf("new NATS client: %v", err)
	}
	cm.natsClient = client
	cm.resources = make(map[string][]string)
	cm.started.Store(true)
	cm.initialized.Store(true)

	incumbentStore := &fakeStreamable{id: "incumbent"}
	incumbent := &barrierStoreProvider{
		barrierTestComponent: newBarrierTestComponent("incumbent"),
		provided:             map[string]storage.StreamableStore{"shared": incumbentStore},
	}
	if err := registerStarted(cm, "incumbent", incumbent); err != nil {
		t.Fatalf("register incumbent: %v", err)
	}

	if err := cm.registry.RegisterFactory("dynamic-provider", &component.Registration{
		Name: "dynamic-provider",
		Type: string(types.ComponentTypeStorage),
		Factory: func(json.RawMessage, component.Dependencies) (component.Discoverable, error) {
			return &stopObservingProvider{
				barrierStoreProvider: &barrierStoreProvider{
					barrierTestComponent: newBarrierTestComponent("rival"),
					provided: map[string]storage.StreamableStore{
						"rival-only": &fakeStreamable{id: "rival-only"},
						"shared":     &fakeStreamable{id: "rival"},
					},
				},
				onStop: func() error { return errDynamicProviderRollback },
			}, nil
		},
	}); err != nil {
		t.Fatalf("register dynamic provider factory: %v", err)
	}
	cfg := types.ComponentConfig{
		Type:    types.ComponentTypeStorage,
		Name:    "dynamic-provider",
		Enabled: true,
		Config:  json.RawMessage(`{}`),
	}

	err = cm.createAndStartComponent(context.Background(), "rival", cfg)
	if err == nil {
		t.Fatal("dynamic duplicate provider start did not return an error")
	}
	if !errors.Is(err, errDynamicProviderRollback) {
		t.Fatalf("dynamic provider error did not join teardown failure: %v", err)
	}
	status := cm.GetComponentStatus()
	if got := status["rival"].State; got != component.StateFailed {
		t.Fatalf("dynamic rival state = %s, want failed", got)
	}
	if status["rival"].LastError == nil {
		t.Fatal("dynamic rival did not retain its registration error")
	}
	if !strings.Contains(status["rival"].LastError.Error(), "duplicate ownership") {
		t.Fatalf("dynamic rival LastError lost registration failure: %v", status["rival"].LastError)
	}
	if !errors.Is(status["rival"].LastError, errDynamicProviderRollback) {
		t.Fatalf("dynamic rival LastError did not join teardown failure: %v", status["rival"].LastError)
	}
	if err := cm.performDetailedHealthCheck(); err == nil {
		t.Fatal("dynamic rival registration failure was absent from manager health")
	}
	rivalComp, ok := cm.components["rival"].Component.(*stopObservingProvider)
	if !ok {
		t.Fatalf("dynamic rival component type = %T", cm.components["rival"].Component)
	}
	if !rivalComp.stopped {
		t.Fatal("dynamic rejected rival lifecycle Stop was not called")
	}
	if cm.components["rival"].Context != nil || cm.components["rival"].Cancel != nil {
		t.Fatal("dynamic rejected rival component context was not cleared")
	}
	select {
	case <-rivalComp.startReturned:
	default:
		t.Fatal("dynamic rival lifecycle Start did not return")
	}
	if _, ok := cm.storeRegistry.Streamable("rival-only"); ok {
		t.Fatal("dynamic rejected rival left a store claim registered")
	}
	got, ok := cm.storeRegistry.Streamable("shared")
	if !ok || got != incumbentStore {
		t.Fatalf("dynamic rival replaced incumbent: (%v, %v)", got, ok)
	}
}

func TestRestartedProviderDuplicateFailsWithoutReplacingIncumbent(t *testing.T) {
	cm := newStoreRegistryTestManager()
	client, err := natsclient.NewClient("nats://localhost:4222")
	if err != nil {
		t.Fatalf("new NATS client: %v", err)
	}
	cm.natsClient = client
	cm.resources = make(map[string][]string)
	cm.started.Store(true)
	cm.initialized.Store(true)

	incumbentStore := &fakeStreamable{id: "incumbent"}
	incumbent := &barrierStoreProvider{
		barrierTestComponent: newBarrierTestComponent("incumbent"),
		provided:             map[string]storage.StreamableStore{"shared": incumbentStore},
	}
	if err := registerStarted(cm, "incumbent", incumbent); err != nil {
		t.Fatalf("register incumbent: %v", err)
	}

	old := &barrierStoreProvider{
		barrierTestComponent: newBarrierTestComponent("rival"),
		provided: map[string]storage.StreamableStore{
			"rival-old": &fakeStreamable{id: "old"},
		},
	}
	oldCfg := types.ComponentConfig{
		Type:    types.ComponentTypeStorage,
		Name:    "restart-provider",
		Enabled: true,
		Config:  json.RawMessage(`{"version":"old"}`),
	}
	cm.components["rival"] = &component.ManagedComponent{
		Component: old,
		State:     component.StateStarted,
		Config:    oldCfg,
	}
	if err := cm.registerProvidedStores("rival", old); err != nil {
		t.Fatalf("register old rival store: %v", err)
	}

	if err := cm.registry.RegisterFactory("restart-provider", &component.Registration{
		Name: "restart-provider",
		Type: string(types.ComponentTypeStorage),
		Factory: func(json.RawMessage, component.Dependencies) (component.Discoverable, error) {
			return &stopObservingProvider{
				barrierStoreProvider: &barrierStoreProvider{
					barrierTestComponent: newBarrierTestComponent("rival"),
					provided: map[string]storage.StreamableStore{
						"rival-only": &fakeStreamable{id: "replacement-only"},
						"shared":     &fakeStreamable{id: "replacement"},
					},
				},
				onStop: func() error { return nil },
			}, nil
		},
	}); err != nil {
		t.Fatalf("register restart provider factory: %v", err)
	}
	newCfg := oldCfg
	newCfg.Config = json.RawMessage(`{"version":"new"}`)

	err = cm.restartComponentWithNewConfig(context.Background(), "rival", newCfg, cm.components["rival"])
	if err == nil {
		t.Fatal("restarted duplicate provider did not return an error")
	}
	status := cm.GetComponentStatus()
	if got := status["rival"].State; got != component.StateFailed {
		t.Fatalf("restarted rival state = %s, want failed", got)
	}
	if status["rival"].LastError == nil || !strings.Contains(status["rival"].LastError.Error(), "duplicate ownership") {
		t.Fatalf("restarted rival did not retain registration failure: %v", status["rival"].LastError)
	}
	if err := cm.performDetailedHealthCheck(); err == nil {
		t.Fatal("restarted rival registration failure was absent from manager health")
	}
	rivalComp, ok := cm.components["rival"].Component.(*stopObservingProvider)
	if !ok {
		t.Fatalf("restarted rival component type = %T", cm.components["rival"].Component)
	}
	if !rivalComp.stopped {
		t.Fatal("restarted rejected rival lifecycle Stop was not called")
	}
	if cm.components["rival"].Context != nil || cm.components["rival"].Cancel != nil {
		t.Fatal("restarted rejected rival component context was not cleared")
	}
	if _, ok := cm.storeRegistry.Streamable("rival-old"); ok {
		t.Fatal("restart left the old provider registered after teardown")
	}
	if _, ok := cm.storeRegistry.Streamable("rival-only"); ok {
		t.Fatal("restarted rejected rival left a store claim registered")
	}
	got, ok := cm.storeRegistry.Streamable("shared")
	if !ok || got != incumbentStore {
		t.Fatalf("restarted rival replaced incumbent: (%v, %v)", got, ok)
	}
}

var errDynamicProviderRollback = errors.New("dynamic provider rollback failed")
