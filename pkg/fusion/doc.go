// Package fusion is the generic deterministic-fusion engine: fan-out
// sub-query dispatch, dedup, rank, and budget enforcement over a
// GraphQueryClient. It is a pure leaf package — no NATS, no research-domain
// types (Intent, ExecutionOutput, RouteAction). Domain-coupled callers
// (processor/research-graph-execute) compose this engine and wrap the result
// in their own payload types.
package fusion
