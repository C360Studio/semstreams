package fusion

import (
	"context"
	"fmt"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/storage"
)

// The hydration handle-resolution helper (ADR-062 increment 4, the "crux").
//
// A Lens.Hydrate returns a *message.StorageReference HANDLE, never bytes. This
// helper is what the engine uses to turn that handle into the verbatim body:
// resolve the handle's StorageInstance to a registered storage.Store and Get
// the Key. The contract binds to the backend-agnostic storage.Store INTERFACE +
// the StorageReference handle — never a concrete backend — so each
// deployment/content-type plugs in its own store (NATS ObjectStore for
// small/immutable text, a filestore / S3 for large binaries). This is what makes
// fusion deployment-independent: a remote caller of a standalone fusion service
// (semsource ADR-0006, headless mode removed) can't read the service worktree,
// so bodies must be addressable through a store, not a filesystem path.
//
// Bodies ride storage.Store.Get([]byte) — NOT the text StoredContent envelope
// (whose map[string]string fields corrupt non-UTF-8 to U+FFFD). Byte-exact by
// construction.

// StoreResolver maps a StorageReference.StorageInstance name to the
// storage.Store that holds its data. Wiring supplies it — typically
// MapStoreResolver over the deployment's registered stores. Kept as an interface
// so the engine never assumes one backend and tests can substitute fakes.
//
// Canonical key (gh#376 coordination with semsource): StorageInstance is the
// storage COMPONENT INSTANCE NAME (e.g. "objectstore", "filestore-media"), per
// StorageReference's own doc ("identifies which storage component holds the
// data … enables federation across multiple storage instances") — NOT the bucket
// name. Producers stamp the component instance name; wiring registers each store
// under that same name. (semstreams' own auto-stamp is inconsistent today —
// component.go uses the instance name, store.go uses the bucket — tracked
// separately; this helper, the first consumer, fixes the convention at
// instance-name.)
type StoreResolver interface {
	// Store returns the store registered under instance, and whether one exists.
	Store(instance string) (storage.Store, bool)
}

// MapStoreResolver is a static StoreResolver over a name→Store map. The zero
// value (nil map) resolves nothing. Safe for concurrent reads (Go map reads are
// safe without writers; the map is build-once at wiring time).
type MapStoreResolver map[string]storage.Store

// Store implements StoreResolver.
func (m MapStoreResolver) Store(instance string) (storage.Store, bool) {
	s, ok := m[instance]
	return s, ok
}

// BodyResolver dereferences a Lens.Hydrate handle to its verbatim bytes. It is
// the engine-side helper the assemble step uses to populate a node body from the
// handle a lens returned — backend-agnostic via the injected StoreResolver.
type BodyResolver struct {
	resolver StoreResolver
}

// NewBodyResolver builds a BodyResolver over the given StoreResolver. A nil
// resolver is permitted (ResolveBody then errors on any non-nil handle) so a
// deployment with no verbatim-body stores still constructs cleanly.
func NewBodyResolver(r StoreResolver) *BodyResolver {
	return &BodyResolver{resolver: r}
}

// ResolveBody returns the verbatim body bytes for a Hydrate handle.
//
// Key granularity (gh#376 coordination): the handle's Key addresses the EXACT
// verbatim body — one pre-sliced body blob per entity (keyed by entity ID or
// content hash) — so Get returns the body byte-for-byte with NO engine-side line
// math. The lens's Locator (path + line range) is for citation/display only; the
// body comes pre-sliced through the handle, not by trimming a whole file.
//
//   - A nil ref means "no verbatim body" — returns (nil, nil), NOT an error.
//     (Lens.Hydrate returns (nil, nil) for body-less entities; this preserves
//     that signal end-to-end.)
//   - A non-nil ref with an empty StorageInstance, or an instance with no
//     registered store, is a wiring/producer fault — returns an error so the
//     caller can degrade the node (the engine omits the body; hydration is
//     best-effort and does not fail the fuse — see Lens.Hydrate).
//   - The store's Get error is propagated wrapped.
func (b *BodyResolver) ResolveBody(ctx context.Context, ref *message.StorageReference) ([]byte, error) {
	if ref == nil {
		return nil, nil
	}
	if ref.StorageInstance == "" {
		return nil, fmt.Errorf("hydrate: storage reference has no StorageInstance (key=%q)", ref.Key)
	}
	if b.resolver == nil {
		return nil, fmt.Errorf("hydrate: no store resolver configured; cannot resolve instance %q", ref.StorageInstance)
	}
	store, ok := b.resolver.Store(ref.StorageInstance)
	if !ok {
		return nil, fmt.Errorf("hydrate: no storage.Store registered for instance %q", ref.StorageInstance)
	}
	data, err := store.Get(ctx, ref.Key)
	if err != nil {
		return nil, fmt.Errorf("hydrate: get %q from instance %q: %w", ref.Key, ref.StorageInstance, err)
	}
	return data, nil
}
