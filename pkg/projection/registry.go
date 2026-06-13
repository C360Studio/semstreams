package projection

import (
	"fmt"
	"sort"
	"sync"
)

// The global projection-contract registry. Types/components declare their
// contracts here (typically from init(), co-located with payload registration);
// component boot enumerates and derives them per owner. Mirrors the
// payloadregistry idiom the design references.
var (
	registryMu sync.RWMutex
	registry   = make(map[string]Contract)
)

// Register adds a contract to the global registry, keyed by Name. Validates the
// contract and rejects a duplicate name. Co-locate the call with the payload
// type's registration so the projection and the type are declared together.
func Register(c Contract) error {
	if err := c.Validate(); err != nil {
		return err
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, dup := registry[c.Name]; dup {
		return fmt.Errorf("%w: contract %q already registered", ErrInvalidContract, c.Name)
	}
	registry[c.Name] = c
	return nil
}

// MustRegister is Register that panics on error — for init()-time registration
// where a malformed or duplicate contract is a programming error.
func MustRegister(c Contract) {
	if err := Register(c); err != nil {
		panic(fmt.Sprintf("projection.MustRegister(%q): %v", c.Name, err))
	}
}

// Lookup returns a registered contract by name.
func Lookup(name string) (Contract, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	c, ok := registry[name]
	return c, ok
}

// Registered returns every registered contract, name-sorted for deterministic
// enumeration.
func Registered() []Contract {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]Contract, 0, len(registry))
	for _, c := range registry {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// resetRegistry clears the global registry — test-only, to isolate cases that
// exercise registration without leaking state across tests.
func resetRegistry() {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry = make(map[string]Contract)
}
