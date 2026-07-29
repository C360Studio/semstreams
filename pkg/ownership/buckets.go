package ownership

import "github.com/c360studio/semstreams/graph"

// KV bucket names for the ownership substrate (ADR-056 Decision 2/4). Both
// live buckets are framework-owned catalog members; the names re-export
// graph's constants (the catalog's single source of truth) so the registry,
// the boot wiring, and the write guard can never drift.
const (
	// BucketOwnerClaims holds the single `_registry` epoch key — the union of
	// every registered owner's claims, advanced under UpdateWithRetry CAS.
	BucketOwnerClaims = graph.BucketOwnerClaims

	// BucketOwnerPresence holds `heartbeat.<owner>` keys only for registrations
	// containing replace/CAS claims. Its bucket-level TTL lets the next
	// registrant compact a crashed owner's whole atomic entry. Non-owning
	// append/foreign-edge-only registrations have no key and persist.
	BucketOwnerPresence = graph.BucketOwnerPresence

	// BucketPendingEdges buffers Conditional foreign edges whose target has not
	// yet been born (Decision 4). Declared here for the boot wiring; the buffer
	// itself lands in a later W0 increment. NOT a catalog member: no consumer
	// exists yet, so the framework guarantees nothing about it.
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
