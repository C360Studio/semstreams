// Package prompt provides fragment-based system prompt assembly for agentic loops.
// Prompts are composed from ordered fragments filtered by role, provider, and
// runtime conditions. Inspired by semdragon's promptmanager but simplified for
// semteams' architecture.
package prompt

// Category determines the order of fragments in the assembled prompt.
// Lower values appear earlier. Categories provide semantic grouping without
// requiring knowledge of specific fragment content.
type Category int

const (
	// CategorySystem contains core identity and behavioral constraints.
	CategorySystem Category = 0
	// CategoryRole contains role-specific instructions (architect, editor, reviewer, etc.).
	CategoryRole Category = 100
	// CategoryTools contains tool usage guidance and restrictions.
	CategoryTools Category = 200
	// CategoryDomain contains domain context (project knowledge, entity relationships).
	CategoryDomain Category = 300
	// CategoryConstraints contains guardrails, budget limits, safety rules.
	CategoryConstraints Category = 400
	// CategoryContext contains runtime context (prior conversation, dependencies).
	CategoryContext Category = 500
)

// Fragment is an atomic unit of prompt content that can be conditionally included
// and ordered by category during assembly.
type Fragment struct {
	ID       string   // Unique identifier for observability
	Category Category // Determines ordering (lower = earlier)
	Priority int      // Ordering within category (lower = earlier)
	Content  string   // Static content (used when ContentFunc is nil)

	// ContentFunc generates content dynamically at assembly time.
	// Takes precedence over Content when non-nil.
	ContentFunc func(ctx *AssemblyContext) string

	// Condition determines whether this fragment is included.
	// When nil, the fragment is always included.
	Condition func(ctx *AssemblyContext) bool

	// Roles restricts this fragment to specific agent roles.
	// Empty means all roles.
	Roles []string
}

// AssemblyContext provides runtime information for fragment filtering and
// dynamic content generation.
type AssemblyContext struct {
	// Agent identity
	Role     string // Agent role (general, architect, editor, reviewer, explorer, etc.)
	LoopID   string
	Depth    int // Nesting depth (0 = top-level)
	MaxDepth int

	// Task info
	Prompt       string   // User's original prompt
	WorkflowSlug string   // Optional workflow identifier
	WorkflowStep string   // Current step in workflow
	Tools        []string // Available tool names

	// Runtime state
	Iteration     int
	MaxIterations int
	ParentLoopID  string // Non-empty if this is a child agent

	// Provider hint for formatting
	Provider string // "anthropic", "openai", "ollama", etc.
}
