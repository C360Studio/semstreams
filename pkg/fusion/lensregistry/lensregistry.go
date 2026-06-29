// Package lensregistry holds the explicit, instance-scoped registry of fusion
// Lens factories (ADR-062 increment 3).
//
// Registration is EXPLICIT — a bootstrap aggregator calls Register — and is
// NOT an init()-global like the vocabulary registry. Per ADR-062: lenses are
// factories that need deps/config (like components and payloads), not stateless
// metadata like predicates; and the vocabulary registry's last-write-wins
// override semantics would be exactly wrong for lenses. You want explicit
// per-product registration that ERRORS on a name collision, never a silent
// override. The registry is an instance (New) threaded through wiring, not a
// package global, so two independently-configured engines don't share state.
package lensregistry

import (
	"fmt"
	"sort"
	"sync"

	"github.com/c360studio/semstreams/pkg/fusion"
)

// Factory constructs a configured Lens. Products supply one per lens; the
// explicit bootstrap aggregator captures the lens's deps/config in the closure,
// so construction is deferred to Build (when wiring is ready) without leaking
// those deps into the registry.
type Factory func() (fusion.Lens, error)

// Registry is an explicit, instance-scoped set of named Lens factories. The
// zero value is not usable — call New.
type Registry struct {
	mu        sync.RWMutex
	factories map[string]Factory
}

// New returns an empty registry ready for explicit Register calls.
func New() *Registry {
	return &Registry{factories: make(map[string]Factory)}
}

// Register adds a lens factory under name. Returns an error on an empty name, a
// nil factory, or a duplicate name — explicit per-product registration, never a
// silent last-write-wins override (the property that makes this NOT the
// vocabulary init()-global pattern; ADR-062).
func (r *Registry) Register(name string, f Factory) error {
	if name == "" {
		return fmt.Errorf("lensregistry: empty lens name")
	}
	if f == nil {
		return fmt.Errorf("lensregistry: nil factory for %q", name)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.factories[name]; exists {
		return fmt.Errorf("lensregistry: lens %q already registered (explicit registration only — no silent override)", name)
	}
	r.factories[name] = f
	return nil
}

// Build constructs the Lens registered under name. Returns an error when name is
// unregistered, the factory fails, or the factory returns a nil Lens.
func (r *Registry) Build(name string) (fusion.Lens, error) {
	r.mu.RLock()
	f, ok := r.factories[name]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("lensregistry: no lens registered under %q", name)
	}
	lens, err := f()
	if err != nil {
		return nil, fmt.Errorf("lensregistry: build lens %q: %w", name, err)
	}
	if lens == nil {
		return nil, fmt.Errorf("lensregistry: factory for %q returned a nil lens", name)
	}
	return lens, nil
}

// Names returns the registered lens names, sorted for deterministic iteration.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.factories))
	for n := range r.factories {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
