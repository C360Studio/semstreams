package persona

import (
	"context"

	"github.com/c360studio/semstreams/processor/agentic-loop/prompt"
)

// ToFragment converts a stored Persona into a prompt.Fragment.
//
// The dynamic hooks on prompt.Fragment — ContentFunc and Condition —
// stay nil: those are Go closures that can't round-trip through JSON.
// A stored Persona can only contribute static content and role-based
// gating; richer runtime behaviour remains a code-level concern on
// DefaultFragments. See ADR-029 step 3b for the framework/product split.
func (p *Persona) ToFragment() prompt.Fragment {
	return prompt.Fragment{
		ID:       p.ID,
		Category: prompt.Category(p.Category),
		Priority: p.Priority,
		Content:  p.Content,
		Roles:    p.Roles,
	}
}

// Fragments materialises every stored persona as prompt.Fragments so a
// caller can UpsertAll them onto a prompt.Registry seeded with
// DefaultFragments. Safe to call on a nil Manager — returns (nil, nil)
// so products can wire persona support conditionally without guarding
// every call site.
//
// Callers choose the cadence: once at startup for static personas, or
// again on KV-watch events if hot-reload of persona edits is required.
func (m *Manager) Fragments(ctx context.Context) ([]prompt.Fragment, error) {
	if m == nil {
		return nil, nil
	}
	personas, err := m.List(ctx)
	if err != nil {
		return nil, err
	}
	if len(personas) == 0 {
		return nil, nil
	}
	fragments := make([]prompt.Fragment, 0, len(personas))
	for _, p := range personas {
		fragments = append(fragments, p.ToFragment())
	}
	return fragments, nil
}
