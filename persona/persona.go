// Package persona provides storage and CRUD for agent-role prompt fragments.
//
// A Persona is the stored form of a role's prompt fragment — the static
// bits a human or LLM authors and a product might persist per-flow.
// Runtime-only aspects of prompt composition (dynamic ContentFunc,
// Condition predicates) stay in processor/agentic-loop/prompt.Fragment;
// Persona round-trips cleanly through JSON and KV.
//
// Per ADR-029, Persona sits in Pattern B (KV-backed CRUD Manager). See
// docs/adr/029-instance-type-patterns.md for the framework/product split:
// semstreams ships the primitive, products (semteams, semspec,
// soul.md-style BMAD adopters) compose their own persona registries.
package persona

// Persona is a serialisable prompt-fragment definition.
//
// Only static content is stored; any dynamic generation (ContentFunc)
// or conditional inclusion (Condition) remains a code-level concern on
// prompt.Fragment. When the assembler consumes a Persona it converts to
// a prompt.Fragment with those runtime hooks nil — documented in the
// assembler integration (ADR-029 step 3b).
type Persona struct {
	// ID is the unique key within the PERSONAS bucket. Kebab-case
	// convention aligns with rule IDs (e.g. "role-researcher",
	// "domain-finance").
	ID string `json:"id"`

	// Category maps to prompt.Category values (system=0, role=100,
	// tools=200, domain=300, constraints=400, context=500). Stored as
	// int so persona records don't leak the prompt package's enum type
	// into the Pattern-B storage surface.
	Category int `json:"category"`

	// Priority orders fragments inside a Category (lower = earlier).
	Priority int `json:"priority,omitempty"`

	// Content is the static prompt text.
	Content string `json:"content"`

	// Roles restricts this persona to specific agent roles. An empty
	// list means all roles — same semantics as prompt.Fragment.Roles.
	Roles []string `json:"roles,omitempty"`

	// Description is optional free-text context for operators who manage
	// personas via the CRUD tools — not included in the assembled prompt.
	Description string `json:"description,omitempty"`
}

// Validate checks the minimum invariants every stored persona must
// satisfy. Called by Manager.Create / Manager.Update before writing to
// KV so the bucket never holds obviously-broken records.
func (p *Persona) Validate() error {
	if p.ID == "" {
		return errInvalidPersona{reason: "id is required"}
	}
	if p.Content == "" {
		return errInvalidPersona{reason: "content is required"}
	}
	return nil
}

// errInvalidPersona keeps the package's validation errors distinct from
// KV transport errors so callers can decide whether to retry or surface.
type errInvalidPersona struct {
	reason string
}

func (e errInvalidPersona) Error() string {
	return "invalid persona: " + e.reason
}
