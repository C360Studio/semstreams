// Package fusion is the generic deterministic-fusion engine: fan-out
// sub-query dispatch, dedup, rank, and budget enforcement over a
// GraphQueryClient, plus the product-supplied Lens SPI (lens.go). It is a
// near-leaf package — its only domain dependencies are the message package (for
// Triple in Entity and StorageReference as the Hydrate handle) and the leaf
// storage interface package (the Hydrate backend-deref contract); no NATS, no
// research-domain types (Intent, ExecutionOutput, RouteAction). Domain-coupled
// callers (processor/research-graph-execute) compose this engine and wrap the
// result in their own payload types; products supply a Lens.
package fusion
