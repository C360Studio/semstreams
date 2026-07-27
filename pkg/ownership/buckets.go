package ownership

// KV bucket names for the ownership substrate (ADR-056 Decision 2/4). These
// are framework-owned buckets, created at graph-ingest boot (a later W0
// increment wires creation; the names are fixed here so the registry and the
// boot wiring agree on one source of truth).
const (
	// BucketOwnerClaims holds the single `_registry` epoch key — the union of
	// every registered owner's claims, advanced under UpdateWithRetry CAS.
	BucketOwnerClaims = "OWNER_CLAIMS"

	// BucketOwnerPresence holds `heartbeat.<owner>` keys only for registrations
	// containing replace/CAS claims. Its bucket-level TTL lets the next
	// registrant compact a crashed owner's whole atomic entry. Non-owning
	// append/foreign-edge-only registrations have no key and persist.
	BucketOwnerPresence = "OWNER_PRESENCE"

	// BucketPendingEdges buffers Conditional foreign edges whose target has not
	// yet been born (Decision 4). Declared here for the boot wiring; the buffer
	// itself lands in a later W0 increment.
	BucketPendingEdges = "PENDING_EDGES"
)

// registryKey is the single epoch key in BucketOwnerClaims. The bare
// `_registry` (no slashes — NATS KV keys permit only alphanumerics + `-` `_`
// `=` `.`, and the rest of the codebase keys with dots).
const registryKey = "_registry"

// presenceKeyPrefix prefixes owning-lease heartbeat keys in
// BucketOwnerPresence. The suffix in `heartbeat.<owner>` is the canonical,
// subject-safe owner id.
const presenceKeyPrefix = "heartbeat."
