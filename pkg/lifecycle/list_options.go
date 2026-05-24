package lifecycle

import "time"

// ListOptions parameterizes Manager.List queries against a registered
// workflow type. All fields are optional; the zero value lists every
// instance in the workflow's KV bucket (subject to Limit / Offset).
//
// Match semantics:
//   - Phase filters to entries whose Participant.Phase() equals the
//     given value. Empty string means any phase.
//   - Active=true filters to entries whose Participant.IsTerminal()
//     returns false. Active=false (the zero value) does not filter on
//     terminal status — to list only terminal entries, set
//     ListOptions.Phase to a specific terminal phase OR add a Match
//     entry on a field that distinguishes them.
//   - UpdatedAfter filters to entries whose KV revision timestamp is
//     strictly later than the given time. Useful for incremental
//     pagination of large active sets.
//   - Match is a field-equality map; key is the JSON field name (per
//     `json:` struct tag), value is the expected value. Multi-tenancy
//     is expressed app-side via Match{"org_id": "acme"} — the
//     framework knows nothing about org/tenant semantics. Reflection
//     is used to read field values at match time; this is acceptable
//     for v1 linear-scan but flag fields as `lifecycle:"indexable"`
//     for v2 secondary-index migration once an operator demonstrates a
//     scaling bottleneck.
//   - Limit caps the returned slice size. 0 means unlimited (default).
//   - Offset is a pagination cursor — skip the first Offset matching
//     entries. Combine with Limit for paged operator-API responses.
//
// v1 implementation is linear scan over the bucket; complexity is
// O(N) per List call where N is the total instance count. Operators
// hitting this cliff (~10K active instances is a reasonable guideline
// — file when an operator demonstrates the bottleneck) trigger the
// v2 secondary index work; the API stays stable across the migration.
type ListOptions struct {
	// Phase filters to instances whose current phase equals this
	// value. Empty string means any phase.
	Phase string

	// Active=true filters to non-terminal instances. Active=false
	// (default) does not filter on terminal status. To list only
	// terminal instances, use Phase with a specific terminal phase
	// or Match on a discriminating field.
	Active bool

	// UpdatedAfter filters to instances whose last KV write happened
	// strictly after this time. Zero value disables the filter.
	UpdatedAfter time.Time

	// Match is a field-equality map (JSON field name → expected value).
	// Multi-tenancy and other app-specific filters live here. Field
	// resolution is reflection-based against the registered state
	// struct.
	Match map[string]any

	// Limit caps the returned slice size. 0 (default) means unlimited.
	Limit int

	// Offset skips the first Offset matching entries before applying
	// Limit. Use for paged operator-API responses.
	Offset int
}
