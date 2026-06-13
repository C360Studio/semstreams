package ownership

// KV bucket names for the ownership substrate (ADR-056 Decision 2/4). These
// are framework-owned buckets, created at graph-ingest boot (a later W0
// increment wires creation; the names are fixed here so the registry and the
// boot wiring agree on one source of truth).
const (
	// BucketOwnerClaims holds the single `_registry` epoch key — the union of
	// every registered owner's claims, advanced under UpdateWithRetry CAS.
	BucketOwnerClaims = "OWNER_CLAIMS"

	// BucketOwnerPresence holds per-owner heartbeat keys
	// (`heartbeat.<owner_token>`) with a bucket-level TTL backstop, so a
	// crashed owner's claims are compacted out of the epoch by the next
	// registrant (availability over a dead owner's stale claim).
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

// presenceKeyPrefix prefixes per-owner heartbeat keys in BucketOwnerPresence,
// dot-segmented as `heartbeat.<owner_token>` where owner_token is the
// subject-safe FNV-1a hex of the owner id (see Token).
const presenceKeyPrefix = "heartbeat."
