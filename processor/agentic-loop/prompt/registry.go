package prompt

import (
	"slices"
	"sort"
	"sync"
)

// Registry holds prompt fragments and filters them by context at assembly time.
type Registry struct {
	fragments []Fragment
	mu        sync.RWMutex
}

// NewRegistry creates an empty fragment registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// Add registers a fragment. Thread-safe.
func (r *Registry) Add(f Fragment) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fragments = append(r.fragments, f)
}

// AddAll registers multiple fragments.
func (r *Registry) AddAll(fragments []Fragment) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fragments = append(r.fragments, fragments...)
}

// Upsert replaces the fragment with the same ID or appends a new one.
// Callers layer KV-backed personas on top of DefaultFragments via this —
// a stored persona with ID "role-researcher" overrides the in-code
// researcher fragment without forcing a rebuild of the registry.
func (r *Registry) Upsert(f Fragment) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.upsertLocked(f)
}

// UpsertAll applies Upsert for each fragment under a single lock acquire.
// Preferred over a loop of Upsert calls when loading a batch of overrides.
func (r *Registry) UpsertAll(fragments []Fragment) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, f := range fragments {
		r.upsertLocked(f)
	}
}

func (r *Registry) upsertLocked(f Fragment) {
	for i := range r.fragments {
		if r.fragments[i].ID == f.ID {
			r.fragments[i] = f
			return
		}
	}
	r.fragments = append(r.fragments, f)
}

// GetForContext returns fragments matching the given context, ordered by
// category (ascending) then priority (ascending) within each category.
func (r *Registry) GetForContext(ctx *AssemblyContext) []Fragment {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var matched []Fragment
	for _, f := range r.fragments {
		if matchesContext(f, ctx) {
			matched = append(matched, f)
		}
	}

	sort.SliceStable(matched, func(i, j int) bool {
		if matched[i].Category != matched[j].Category {
			return matched[i].Category < matched[j].Category
		}
		return matched[i].Priority < matched[j].Priority
	})

	return matched
}

// matchesContext checks if a fragment should be included for the given context.
func matchesContext(f Fragment, ctx *AssemblyContext) bool {
	// Role filter
	if len(f.Roles) > 0 && !slices.Contains(f.Roles, ctx.Role) {
		return false
	}

	// Condition predicate
	if f.Condition != nil && !f.Condition(ctx) {
		return false
	}

	return true
}
