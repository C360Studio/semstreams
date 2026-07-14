## 0. Governance Prerequisites

- [ ] 0.1 Correct `graph-index-hardening` to record shipped codec/catalog behavior without backdating the
      canonical predicate contract
- [ ] 0.2 Complete or explicitly re-scope and archive `graph-index-hardening` before this change modifies
      production index behavior; benchmark-only work may proceed independently
- [ ] 0.3 Record PR #532 as the framework grammar prerequisite and keep remaining owned-producer migrations as
      release gates before any raw-key cutover
- [ ] 0.4 Complete and archive `nats-kv-key-contract`; consume its exported validators, stable errors, opaque-codec
      contract, and budgets before any fixed-position filter proof or raw-key decision
- [ ] 0.5 Prove worst-case current PREDICATE, PREDICATE_CATALOG, NAME, CONTEXT, INCOMING, OUTGOING, and ALIAS keys and
      filters against the shared budgets; if unbounded entity IDs or aliases prevent proof, make the separately
      approved semantic-bound or physical-codec change an activation dependency without choosing it here

## 1. PR #524 Store and Query Contract

- [ ] 1.1 Inventory every derived-index layout, fixed or variable token arity, semantic owner, literal forward
      filter or explicit non-filterability, literal owner filter or alternate authority, value-overwrite policy,
      lifecycle behavior, reset rule, and readiness consequence
- [ ] 1.2 Encode literal filter strings through the shared NATS KV validators and pin the complete PR #524 ownership
      matrix plus maximum token/key/filter formulas in table-driven tests, including ALIAS, PREDICATE_CATALOG, every
      current layout, and the raw PREDICATE candidate
- [ ] 1.3 Define and test the INCOMING source-ownership contract, including source fact replacement, source
      removal/tombstone, target retirement, and authorized cascade behavior, without production wiring
- [ ] 1.4 Pin shipped production behavior: OUTGOING and CONTEXT reconcile; PREDICATE, NAME, ALIAS, and source-owned
      INCOMING remain additive; target-prefixed INCOMING deletion remains legacy behavior
- [ ] 1.5 Record ALIAS, spatial/geohash, embedding, blob/ObjectStore, cascade, and global GC as separately specified
      work outside this change
- [ ] 1.6 Freeze public acceptance fixtures for `[A] -> [B] -> []` at the graph-index status watermark across
      OUTGOING, exact/value predicate, list, stats, compound, by-name, incoming, and traversal queries; track
      clustering only after its independently completed next detection cycle

## 2. Real-NATS Current-Layout Owner Spike

- [ ] 2.1 Freeze and version the 5k CI and 21k full datasets, environment, numeric/resource-headroom budgets,
      convergence watermark, maximum memberships per owner, maximum-supported-worker candidate, and benchmark-only
      manifest baseline
- [ ] 2.2 Construct and validate literal exact-arity forward and owner filters through `nats-kv-key-contract`, then
      prove maximum current PREDICATE, PREDICATE_CATALOG, NAME, CONTEXT, INCOMING, OUTGOING, and ALIAS keys/filters
      against the shared token, key, filter, byte, and arity budgets and real NATS, including malformed shorter and
      longer keys; representative corpus success MUST NOT substitute for governed maxima
- [ ] 2.3 Test concurrent Put/Delete, duplicate observation and exact-key deduplication, cancellation, empty buckets,
      error classification, and convergence to a declared final ENTITY_STATES revision
- [ ] 2.4 Prove `[A] -> [B] -> []` stale deletion, desired insertion, and stable-key value overwrite for every
      membership store in isolated reconciliation tests without activating production paths
- [ ] 2.5 Run five warmups and 30 measured repetitions, then full replay and sustained churn at representative one-
      and four-worker shapes, a 16-worker stress shape, and the maximum-supported-worker candidate
- [ ] 2.6 Record latency, ingest throughput, queue growth, catch-up time, key/byte volume, client allocations, server
      CPU/RSS, temporary-consumer high-water/return-to-baseline, and end-to-end reconciliation time
- [ ] 2.7 Compare the shipped PR #524 path, filtered reconciliation, and benchmark-only manifest across the full
      lifecycle; passing isolated latency MUST NOT predetermine the owner-discovery decision

## 3. Current-Layout Reconciliation Decision and Implementation

- [ ] 3.1 Write and approve the owner-discovery and INCOMING-ownership ADR, selecting mechanisms per store from the
      complete correctness and resource evidence; block activation on every failed filter mechanism and unresolved
      current-layout entity-ID/alias bound or physical-codec dependency
- [ ] 3.2 For each failed query-visible owner filter, first complete its approved dependent bounded replacement
      mechanism. Then recreate PREDICATE_INDEX, PREDICATE_CATALOG, NAME_INDEX, and INCOMING_INDEX behind typed
      not-ready responses after atomically starting a rebuild generation that resets sticky
      readiness/watermark/enumeration state; rebuild from canonical ENTITY_STATES, activate filtered reconciliation
      only for passing stores and the approved alternate mechanism for each failed store, preserve CONTEXT
      replacement, and remove rejected helpers
- [ ] 3.3 Preserve keyed ordering, execution-time current-state reads, bounded repair, exact status watermarks, and
      failure-held readiness; enforce the benchmark-selected maximum worker count in configuration
- [ ] 3.4 Replace source-owned INCOMING rows on source fact changes/removal and remove target-prefix deletion only
      after the selected source-owned replacement mechanism, filtered or alternate, is approved, implemented, and
      covered by readiness; preserve live-source assertions on target logical retirement or target removal/tombstone
- [ ] 3.5 Sort and deduplicate complete exact, value-filtered, compound, list, namespace-list, and stats results;
      apply limits/samples only on wire surfaces that expose them and preserve existing INCOMING/NAME total orders
- [ ] 3.6 Make predicate-list and namespace-list expose only current non-zero memberships; keep vocabulary history in
      the registry even if PREDICATE_CATALOG remains physically monotonic
- [ ] 3.7 Prove graph-index query parity after update, empty projection, source removal, target retirement, repair,
      restart, and shuffled replay only after every affected query-visible store has an approved and implemented
      bounded replacement mechanism; separately prove the next completed clustering cycle consumes reconciled truth

## 4. Independent PREDICATE Representation Decision

- [ ] 4.1 Confirm an existing governed maximum entity-ID length and prove the complete raw key against the shared
      literal-token, literal-key, arity, and byte budgets; if no entity bound exists, fail the raw candidate and open
      a separate breaking entity-ID contract change rather than expanding it here
- [ ] 4.2 Pre-register eligible required queries/consumers and a numeric or mechanically decidable
      material-improvement threshold for each before measuring either representation
- [ ] 4.3 Benchmark current `hash(predicate).entity6` plus PREDICATE_CATALOG against raw fixed-nine-token keys with
      identical datasets, current-layout reconciliation, and public query fixtures
- [ ] 4.4 Compare exact/namespace lookup, bytes, resource headroom, failure convergence, operational inspection, and
      catalog dependency; retain hash+catalog unless raw crosses a pre-registered threshold
- [ ] 4.5 Identify any current predicate-membership-watch consumer; if one exists, define add/remove-only semantics,
      otherwise record watch behavior as a non-public operational property
- [ ] 4.6 Write and approve the ADR-065 representation decision independently of the already-approved current-layout
      reconciliation decision
- [ ] 4.7 If raw wins, delete/recreate only selected derived buckets, rebuild from canonical ENTITY_STATES behind a
      typed not-ready watermark, remove every old-format reader, and retire PREDICATE_CATALOG
- [ ] 4.8 If hash+catalog remains, remove raw-candidate scaffolding and avoid an unnecessary storage cutover
- [ ] 4.9 Prove exact, namespace, list, stats, compound, traversal, restart, repair, and limited-result parity for the
      selected representation; prove clustering parity after its next independently completed detection cycle

## 5. Governance and Documentation Closeout

- [ ] 5.1 Publish the measured owner-discovery matrix as an input to the separate retention epic without selecting
      ObjectStore reachability, tombstone payload, TTL, stream-limit, cascade, or global GC policy here
- [ ] 5.2 Correct gh#527 and cross-link remaining gh#433 cleanup scope without absorbing those changes
- [ ] 5.3 Supersede ADR-065/068 through new records without rewriting historical decisions
- [ ] 5.4 Update KV Twofer, knowledge-graph, vocabulary, index-reference, reset, and query-ordering documentation
- [ ] 5.5 Run lint, race, contracts, real-NATS integration, structural e2e, semantic e2e, and affected product suites
- [ ] 5.6 Archive this change only after every affected query-visible store satisfies `[A] -> [B] -> []` through
      an approved and implemented bounded replacement mechanism, including mechanisms delivered by explicit
      dependent changes, so graph-index and graph-query deltas become current truth
