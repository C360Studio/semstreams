package component

import "fmt"

// StoreProvidePort declares that a storage component OWNS (provides) a store
// instance, addressable by its StorageInstance name (ADR-063). It is the
// flowgraph marker for store ownership and complements the StoreProvider
// interface the ComponentManager reads to populate the shared StoreRegistry.
//
// Non-exclusive by design. Duplicate live Store ownership is owner-local
// runtime state and remains enforced by storeregistry.Register; it is not a
// declaration-derived exclusive-resource claim.
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
