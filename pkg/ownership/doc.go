// Package ownership implements the ADR-056 authoritative-semantic-state
// owner registry: the framework substrate that names exactly one owner per
// (entity-ID pattern, predicate group) and rejects two owners selecting the
// same cell in an owning write mode.
//
// This is the W0 spine. It provides:
//
//   - The claim types — OwnerClaim (owned current state) and ForeignEdgeClaim
//     (a relationship-producer claim), plus CoordinationWaiver — modelling the
//     Decision-1 tuple (entity-ID pattern, predicate set, write mode, owner id).
//   - A SINGLE-EPOCH-KEY registry (the bare `_registry` key in the OWNER_CLAIMS
//     bucket) advanced under UpdateWithRetry CAS, so cross-process overlap is
//     detected: every registrant of ANY claim serializes through one key
//     (Decision 2).
//   - Glob-vs-glob entity-ID-pattern intersection × exact-string predicate
//     intersection (the overlap algorithm), including the Owner×ForeignEdge
//     cross-type check (Decision 2 MEDIUM).
//   - Stale-owner compaction via a separate OWNER_PRESENCE heartbeat bucket
//     (liveness over a dead owner's stale claim — the same call NATS KV TTLs
//     make), and OwnerOf, the write-time lease lookup the graph-ingest
//     mutation handlers consult.
//
// What is deliberately NOT here yet (later W0 increments, each with its own
// review):
//
//   - The ForeignEdgeClaim T2-regroup-seam reject + inverse-gate in
//     graph-ingest (Decision 4 BLOCKING-B).
//   - The PENDING_EDGES Conditional-edge buffer + delete-after-apply drain +
//     boot re-drain + the counting crash-recovery flip-gate test (Decision 4).
//   - The OwnerToken wire field on the graph mutation requests + the handler
//     lease check returning ErrorCodeOwnerLeaseStale (Decision 2 write seam).
//   - graph-ingest boot wiring (bucket creation, Registry instantiation) and
//     the lifecycle.Manager embedding (Decision 5).
//
// See docs/adr/056-authoritative-semantic-state.md.
package ownership
