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
// W0 4a landed (Decision 4 T2-seam reject, OBSERVE-ONLY):
//
//   - The inverse-gate is now ENFORCED at registration: RegisterOwner runs
//     CheckInverseGate over the registration's foreign edges using an injected
//     InverseResolver (set via EnsureBuckets; nil → skip-with-warn-once).
//   - FE-claim-only owners are EXEMPT from liveness compaction (see compactStale)
//     — they are not leases. KNOWN 4b/4c follow-up: a dead FE-only owner now
//     persists, so the cross-type overlap check could strand a live owning-claim
//     registrant against it (not reachable today; see the compactStale comment).
//   - ClaimReader is the read-only view graph-ingest uses to classify foreign
//     edges at the T2-seam (observe-only: meter unclaimed + route anyway). The
//     graph-ingest seam wiring + the foreign_edge_unclaimed_total metric live in
//     processor/graph-ingest.
//
// W0 OwnerToken write-lease landed (Decision 2 write seam — the full PR-1..PR-5 arc):
//
//   - PR-1 wire field + incarnation fence; PR-2 ClaimReader.OwnerOf lease reader;
//     PR-3 observe-only lease check at the graph-ingest create_with_triples /
//     update_with_triples handlers (meter owner_lease_mismatch_total + Warn,
//     write commits); PR-3.5 typed opaque OwnerToken; PR-4 post-registration
//     Watch-revival quiesce (WatchRevival quiesces the lifecycle Manager producer
//     — quiesce-not-HALT, availability over strict).
//   - PR-5 (the reject flip) is the closing move: a write whose OwnerToken does
//     not match the live owner of a contested predicate is REJECTED with
//     ErrorCodeOwnerLeaseStale — but ONLY when graph-ingest's enforce_owner_lease
//     config is set. Default false keeps the observe-only bake posture; operators
//     flip it on per-deploy once owner_lease_mismatch_total reads zero. The reject
//     protects the rule-pack replace_owned producer that PR-4's Manager quiesce
//     does not cover (a superseded pack holds a cached token, so its stale
//     incarnation fails the lease check at the write seam). All fail-open cases
//     (empty token, no claim reader, legacy/pre-fence owner, reader blip) stay
//     fail-open even under enforcement.
//
// What is deliberately NOT here yet (later W0 increments, each with its own
// review):
//
//   - The HARD T2-seam reject (4c) — 4a routes unclaimed foreign edges
//     deprecated-on-arrival; the hard reject is gated on the metric reading zero
//     over a bake window.
//   - The PENDING_EDGES Conditional-edge buffer + delete-after-apply drain +
//     boot re-drain + the fourth-path (ensureReferencedEntityExists) fold + the
//     counting crash-recovery flip-gate test (Decision 4 / 4b). EnsureBuckets
//     does NOT create PENDING_EDGES yet (no consumer).
//   - The hard-fail-on-overlap flip + observability metric for the Manager embed
//     (gated behind explicit enforcement, alongside the write-lease above).
//   - The projection-contract auto-derivation wiring into graph-ingest boot
//     (Decision 6): pkg/projection.Bind exists; graph-ingest does not yet call
//     it.
//
// See docs/adr/056-authoritative-semantic-state.md.
package ownership
