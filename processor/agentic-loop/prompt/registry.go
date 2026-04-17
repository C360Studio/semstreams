package prompt

import (
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
	if len(f.Roles) > 0 {
		found := false
		for _, r := range f.Roles {
			if r == ctx.Role {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Condition predicate
	if f.Condition != nil && !f.Condition(ctx) {
		return false
	}

	return true
}
