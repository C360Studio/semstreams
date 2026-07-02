// Package storeregistry provides a concurrency-safe registry mapping a
// message.StorageReference.StorageInstance to the live store that backs it
// (ADR-063).
//
// StorageInstance is a logical federation NAME (the producing storage
// component's instance name, gh#400), not a physical address — a reader holding
// a StorageRef cannot derive a handle on its own (it does not know the bucket,
// backend, or connection, and the owner may change them at runtime). This
// registry is the single place the owner publishes "instance X = this live
// store, right now"; readers resolve current truth instead of guessing or
// caching a stale derivation.
//
// Lifecycle contract (enforced by the populator, the ComponentManager):
//   - Register on component Start, Deregister on Stop — so a reconfig
//     (Stop→Start of a fresh instance) swaps the entry.
//   - Consumers resolve LAZILY per-fetch and MUST NOT cache the handle, so a
//     stale/closed store is never held.
//
// Ownership: the registrant (the storage component that owns the store's
// lifecycle) owns Close. Borrowers (graph-embedding, fusion) MUST NOT Close a
// resolved store.
//
// The Registry keys on storage.StreamableStore (the superset — objectstore.Store
// implements it) and also exposes Store(), so it satisfies fusion's
// StoreResolver (Store(instance) (storage.Store, bool)) structurally while
// serving graph-embedding's streaming Open() — one registry, both consumers.
// Kept a leaf: imports only storage.
package storeregistry

import (
	"fmt"
	"sort"
	"sync"

	"github.com/c360studio/semstreams/storage"
)

// Registry maps a StorageInstance name to the live StreamableStore that backs
// it. Safe for concurrent use. The zero value is not usable — construct with
// New.
type Registry struct {
	mu     sync.RWMutex
	stores map[string]storage.StreamableStore
}

// New returns an empty Registry.
func New() *Registry {
	return &Registry{stores: make(map[string]storage.StreamableStore)}
}

// Register binds instance to s. It errors on an empty name, a nil store, or a
// duplicate: an instance already held by a live component. Duplicate ownership
// (two components claiming the same StorageInstance) is a wiring fault surfaced
// loud at Start rather than a silent registry clobber — the ADR-063 replacement
// for exclusive-port conflict detection, which is reconfig-fragile (B1). The
// populator Deregisters on Stop, so a normal reconfig (Stop→Start) never
// collides here; a collision means two live owners.
func (r *Registry) Register(instance string, s storage.StreamableStore) error {
	if instance == "" {
		return fmt.Errorf("storeregistry: cannot register an empty instance name")
	}
	if s == nil {
		return fmt.Errorf("storeregistry: cannot register a nil store for instance %q", instance)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.stores[instance]; ok {
		return fmt.Errorf("storeregistry: instance %q already registered by a live component (duplicate ownership)", instance)
	}
	r.stores[instance] = s
	return nil
}

// Deregister removes instance from the registry. Idempotent: deregistering an
// absent instance is a no-op. Deregister does NOT Close the store — the owning
// component Closes it on its own Stop.
func (r *Registry) Deregister(instance string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.stores, instance)
}

// Streamable returns the streaming store registered under instance, and whether
// one exists right now. Consumers MUST call this per-fetch and MUST NOT cache
// the returned handle (see the lifecycle contract).
func (r *Registry) Streamable(instance string) (storage.StreamableStore, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.stores[instance]
	return s, ok
}

// Store returns the store registered under instance as a storage.Store (Get),
// and whether one exists. This method makes *Registry satisfy fusion's
// StoreResolver interface structurally (no fusion import here — keeps this a
// leaf). Same lazy, no-cache contract as Streamable.
func (r *Registry) Store(instance string) (storage.Store, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.stores[instance]
	return s, ok
}

// Instances returns the currently-registered instance names in sorted order.
// For observability and tests; the slice is a snapshot and safe to mutate.
func (r *Registry) Instances() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.stores))
	for name := range r.stores {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
