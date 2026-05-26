package lifecycle

// ListOptions parameterizes Manager.List queries against a registered
// workflow type. All fields are optional; the zero value lists every
// instance in the workflow's KV bucket (subject to Limit / Offset).
//
// Filter semantics:
//
//   - Phase filters to entries whose Participant.Phase() equals the
//     given value. Empty string means any phase.
//
//   - Active=true filters to entries whose Participant.IsTerminal()
//     returns false. Active=false (the zero value) does not filter on
//     terminal status — to list only terminal entries, set
//     ListOptions.Phase to a specific terminal phase OR add a Match
//     entry on a field that distinguishes them.
//
//   - Match is a field-equality map; key is the JSON field name (per
//     `json:` struct tag), value is the expected value. Multi-tenancy
//     is expressed app-side via Match{"org_id": "acme"} — the
//     framework knows nothing about org/tenant semantics. Reflection
//     is used to read field values at match time; this is acceptable
//     for v1 linear-scan but flag fields as `lifecycle:"indexable"`
//     for v2 secondary-index migration once an operator demonstrates a
//     scaling bottleneck.
//
//     Match comparison semantics are STRICT reflect.DeepEqual against
//     the target field's concrete type. This means:
//
//   - Numeric literals are type-strict: Match{"replica_count": 3}
//     (Go int) does NOT match a field typed uint32(3). Callers
//     constructing Match maps in Go code must use the exact field
//     type (e.g. Match{"replica_count": uint32(3)}).
//
//   - The zero value matches an unset omitempty field. Match
//     {"deployed_to": ""} matches every instance whose DeployedTo
//     field was never set. To skip a filter entirely, omit the key
//     — there is no "match anything" sentinel.
//
//   - String comparison is case-sensitive byte-equal. No fold,
//     no trim, no glob.
//
//     HTTP gateway components (PR 3 of the ADR-047 bundle) handle
//     query-string-to-Go-type coercion at the gateway boundary — that
//     is the right place for permissive parsing of operator-supplied
//     filter values, NOT inside Manager.List. Keeping Manager strict
//     makes its behavior predictable from Go callers AND keeps the
//     wire-level coercion layer's responsibility cleanly separated.
//
//   - Limit caps the returned slice size. 0 means unlimited (default).
//
//   - Offset is a pagination cursor — skip the first Offset matching
//     entries. Combine with Limit for paged operator-API responses.
//
// v1 implementation is linear scan over the bucket; complexity is
// O(N) per List call where N is the total instance count. Operators
// hitting this cliff (~10K active instances is a reasonable guideline
// — file when an operator demonstrates the bottleneck) trigger the
// v2 secondary index work; the API stays stable across the migration.
//
// # Dropped from v1 surface — UpdatedAfter time.Time
//
// An earlier draft exposed UpdatedAfter for incremental pagination
// of large active sets ("show me changes since T"). It was removed
// before v1 ship because plumbing the per-entity revision timestamp
// from the kvStore layer into the filter chain required widening
// kvStore.Get's return signature to leak store-layer concerns
// (CreatedAt) into the Participant model. Apps that need
// since-timestamp pagination today should store their own
// LastUpdatedAt unix-timestamp field on the state struct and Match
// against it. v2 secondary indexing will add the filter back as a
// first-class API when an operator demonstrates the need; the API
// stays additive across that migration.
type ListOptions struct {
	// Phase filters to instances whose current phase equals this
	// value. Empty string means any phase.
	Phase string

	// Active=true filters to non-terminal instances. Active=false
	// (default) does not filter on terminal status. To list only
	// terminal instances, use Phase with a specific terminal phase
	// or Match on a discriminating field.
	Active bool

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
