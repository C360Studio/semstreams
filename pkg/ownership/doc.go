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
// W0 first consumer (landed — Decision 5 embed):
//
//   - EnsureBuckets creates the two framework buckets (OWNER_CLAIMS with audit
//     history; OWNER_PRESENCE with the PresenceTTL staleness backstop) and
//     returns a Registry. It is called EAGERLY in the framework boot path,
//     before lifecycle registration — NOT in graph-ingest, which boots after.
//   - Heartbeater is the substrate-owned liveness ticker an embedder drives over
//     its app-root context (no Close to adopt; ctx cancellation stops it).
//   - lifecycle.Manager embeds the registry via AttachOwnership: each registered
//     Workflow derives an owner claim (phase=cas-transition, audit+writable
//     fields=replace-owned) and registers through the shared epoch. The first
//     consumer's runtime posture is OBSERVE-ONLY — a cross-owner overlap is
//     logged, not bricked (Decision 5); the substrate still REJECTS it (the
//     Manager swallows the rejection deliberately).
//
// What is deliberately NOT here yet (later W0 increments, each with its own
// review):
//
//   - The ForeignEdgeClaim T2-regroup-seam reject in graph-ingest (Decision 4
//     BLOCKING-B). The registration-time inverse-gate (CheckInverseGate) is
//     pure-logic-present; the graph-ingest write-boundary wiring is deferred.
//   - The PENDING_EDGES Conditional-edge buffer + delete-after-apply drain +
//     boot re-drain + the counting crash-recovery flip-gate test (Decision 4).
//     EnsureBuckets does NOT create PENDING_EDGES yet (no consumer).
//   - The OwnerToken wire field on the graph mutation requests + the handler
//     lease check returning ErrorCodeOwnerLeaseStale (Decision 2 write seam),
//     and the post-registration Watch-revival fatal-halt. These two are the
//     write-gating half of the story; until they land, an evicted-then-revived
//     owner only churns the epoch (no dropped writes), which is why the embed
//     ships observe-only rather than hard-fail.
//   - The hard-fail-on-overlap flip + observability metric for the Manager embed
//     (gated behind explicit enforcement, alongside the write-lease above).
//   - The projection-contract auto-derivation wiring into graph-ingest boot
//     (Decision 6): pkg/projection.Bind exists; graph-ingest does not yet call
//     it.
//
// See docs/adr/056-authoritative-semantic-state.md.
package ownership
