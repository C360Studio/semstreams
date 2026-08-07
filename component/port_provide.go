package component

import "fmt"

// StoreProvidePort declares that a storage component OWNS (provides) a store
// instance, addressable by its StorageInstance name (ADR-063). It is the
// flowgraph marker for store ownership and complements the StoreProvider
// interface the ComponentManager reads to populate the shared StoreRegistry.
//
// Non-exclusive by design. Duplicate-ownership detection lives at
// registry-population time (storeregistry.Register errors when two live
// components claim the same instance), NOT via port-conflict exclusivity:
// the ComponentManager clears cm.resources only on RemoveComponent, not on the
// reconfig/reconcile-stop paths, so an exclusive port would make a restarted
// owner collide with its own stale entry and silently fall back to old config
// (ADR-063 B1). Keeping this non-exclusive avoids that landmine.
type StoreProvidePort struct {
	Instance string `json:"instance"` // StorageInstance name this component owns
}

// ResourceID returns a unique identifier for store provide ports.
func (s StoreProvidePort) ResourceID() string {
	return fmt.Sprintf("store-provide:%s", s.Instance)
}

// IsExclusive returns false — ownership conflicts are caught at
// registry-population time, not here (see the type doc).
func (s StoreProvidePort) IsExclusive() bool {
	return false
}

// Kind returns the canonical port kind.
func (s StoreProvidePort) Kind() PortKind {
	return PortKindStoreProvide
}
