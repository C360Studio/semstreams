## 0. Governance Prerequisites

- [x] 0.1 Correct `graph-index-hardening` to record shipped codec/catalog behavior without backdating the canonical
      predicate contract; architect review approved the current-truth correction
- [x] 0.2 Complete or explicitly re-scope and archive `graph-index-hardening` before this change modifies
      production index behavior; benchmark-only work may proceed independently
- [x] 0.3 Record PR #532 as the framework grammar prerequisite. Require its local zero-violation source/configuration/
      fixture gates before framework current-layout activation. Keep every owned-reference update, clean NATS
      wipe/reseed, product e2e, and predicate archive gate as coordinated pre-v1 release requirements without adding
      persisted-state migration, compatibility readers, or rollback
- [x] 0.4 Record the archived `nats-kv-keys` baseline and helper availability: literal validators, stable errors, and
      budgets. The `x1_` opaque codec is available only for a separately authorized new or changed axis; it does not
      re-encode current axes, and the shipped untagged predicate hex remains unchanged
- [x] 0.5 Make benchmark/proof builders and the inactive reconciliation helper call the shared literal key/filter
      validators and return stable classified errors before NATS I/O; zero-I/O tests cover lister, Put, and Delete
- [ ] 0.5a Apply the same preflight contract to any future framework activation call sites before wiring them; this
      checkpoint activates no production reader, writer, configuration, or lifecycle path
- [x] 0.6 Prove worst-case current PREDICATE, PREDICATE_CATALOG, NAME, CONTEXT, INCOMING, and OUTGOING keys and filters
      against shared budgets using canonical `E = 256`; unit maxima are 321/710/902/256/451 bytes through the shared
      validators. Audit ALIAS's representative raw key separately and hand its missing identity bound or raw/opaque/
      owner-discovery decision to the owning ALIAS change without blocking unrelated reconciliation. Real-NATS maxima
      and the remaining local entity-contract gates in task 0.7 still block activation
- [ ] 0.7 Complete the `entity-id-contract` local activation prerequisites: tasks 1.1-4.3 (including completed
      authoritative write task 3.2 and open independent replay/direct-poison task 3.2a), local tasks 5.1, 5.2-5.3 and
      5.6, and tasks
      6.1-6.4a (including final 6.2b and 6.4a reruns). Evidence includes the canonical `E <= 256` API, explicit final
      entity/triple-subject and `@id` reference validation, local zero-violation source corpus, ObjectStore zero-I/O
      guard, malformed current-write/direct-NATS fail-fast proof, clean local NATS wipe/reseed, and final breaking e2e.
      PR #534 (`c8f0b92e`, branch head `6ef169dd`) supplies task 3.2 evidence only. This blocks framework
      current-layout reconciliation but does not require the entity-ID change to archive. Owned-product tasks 5.1a,
      5.4-5.5 and 6.5a remain coordinated pre-v1 release/archive gates

## 1. PR #524 Store and Query Contract

- [ ] 1.1 Inventory every derived-index layout, fixed or variable token arity, semantic owner, literal forward
      filter or explicit non-filterability, literal owner filter or alternate authority, value-overwrite policy,
      lifecycle behavior, reset rule, and readiness consequence in the ADR-075 selected framework composition.
      Product-adapter projections remain in their owner's index and retention inventory rather than the framework
      matrix; graph-research graph state remains framework-owned
- [ ] 1.2 Encode literal filter strings through the shared NATS KV validators and pin the complete PR #524 ownership
      matrix plus maximum token/key/filter formulas in table-driven tests, including ALIAS, PREDICATE_CATALOG, every
      current layout, and the raw PREDICATE candidate. Physical layouts, filters, and formulas are pinned, but the
      table-driven test does not yet encode the complete semantic owner, overwrite, lifecycle, reset, and readiness
      matrix required by tasks 1.1-1.2
- [ ] 1.2a Add malformed-axis and complete-key/filter pre-I/O controls for production CONTEXT, OUTGOING, and every
      remaining graph-index path, covering watcher, Get, lister, Put, Delete, and any additional I/O before activation.
      This is the graph-index-owned obligation moved from `entity-id-contract`; it is not waived
- [ ] 1.3 Define and test the INCOMING source-ownership contract, including source fact replacement, source
      removal/tombstone, target retirement, and authorized cascade behavior, without production wiring
- [ ] 1.4 Pin shipped framework behavior: OUTGOING and CONTEXT reconcile; PREDICATE, NAME, ALIAS, and source-owned
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
- [x] 2.2 Construct and validate literal exact-arity forward and owner filters through `nats-kv-keys`; prove unit-level
      maximum current PREDICATE, PREDICATE_CATALOG, NAME, CONTEXT, INCOMING, and OUTGOING keys/filters at `E = 256`
      against shared token, key, filter, byte, and arity budgets. Audit ALIAS's current raw exact key in the matrix and
      hand its unbounded result to the separate ALIAS owner rather than blocking unrelated stores
- [ ] 2.2a Prove those governed maxima and exact match sets in real NATS with Put/Get/Delete/ListKeysFiltered/Watch,
      including malformed shorter/longer, neighboring-owner, and reversed-axis controls; representative corpus
      success MUST NOT substitute for governed maxima. This owns the maximum graph-index conformance moved from
      `entity-id-contract`, whose unit `E <= 256` bound is a shared prerequisite rather than duplicate proof
- [ ] 2.3 Test concurrent Put/Delete, duplicate observation and exact-key deduplication, cancellation, empty buckets,
      error classification, and convergence to a declared final ENTITY_STATES revision. Cancellation, stable error
      classification, and duplicate-result deduplication are proven; concurrency, empty-bucket recreation, and final
      revision convergence remain
- [x] 2.4 Prove `[A] -> [B] -> []` stale deletion and desired insertion for PREDICATE, NAME, CONTEXT, and source-owned
      INCOMING, plus NAME stable-key value overwrite, in isolated tests without activating production paths
- [ ] 2.5 Run five warmups and 30 measured repetitions, then full replay and sustained churn at representative one-
      and four-worker shapes, a 16-worker stress shape, and the maximum-supported-worker candidate
- [ ] 2.6 Record latency, ingest throughput, queue growth, catch-up time, key/byte volume, client allocations, server
      CPU/RSS, temporary-consumer high-water/return-to-baseline, and end-to-end reconciliation time
- [ ] 2.7 Compare the shipped PR #524 path, filtered reconciliation, and benchmark-only manifest across the full
      lifecycle; passing isolated latency MUST NOT predetermine the owner-discovery decision

## 3. Current-Layout Reconciliation Decision and Implementation

- [ ] 3.1 Write and approve the owner-discovery and INCOMING-ownership ADR, selecting mechanisms per store from the
      complete correctness and resource evidence; block activation on every failed filter mechanism or incomplete
      production-path pre-I/O control or maximum real-NATS key/filter proof. Keep activation blocked until task 0.7
      and this change's tasks 1.2a and 2.2a pass. Record ALIAS risk for its separate owning change without making it a
      gate for unrelated current-layout reconciliation
- [ ] 3.2 For each failed query-visible owner filter, first complete its approved dependent bounded replacement
      mechanism. After the announced pre-v1 wipe/reseed, initialize PREDICATE_INDEX, PREDICATE_CATALOG, NAME_INDEX,
      and INCOMING_INDEX with readiness false before canonical reseed; remain not-ready until initial replay reaches
      the authoritative watermark, then activate passing filtered reconciliation and each approved alternate
      mechanism. Leave generation-based maintenance rebuild to `bounded-storage-operability` tasks 5.1-5.4
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
- [ ] 4.7 If raw wins, include the selected derived buckets in the announced pre-v1 incompatible-NATS wipe, initialize
      them from freshly reseeded canonical ENTITY_STATES behind a typed not-ready watermark, remove every old-format
      reader, and retire PREDICATE_CATALOG; add no in-place migration or persisted-state preservation path
- [ ] 4.8 If hash+catalog remains, remove raw-candidate scaffolding and avoid an unnecessary storage cutover
- [ ] 4.9 Prove exact, namespace, list, stats, compound, traversal, restart, repair, and limited-result parity for the
      selected representation; prove clustering parity after its next independently completed detection cycle

## 5. Governance and Documentation Closeout

- [ ] 5.1 Publish the measured owner-discovery matrix as an input to the separate retention epic without selecting
      ObjectStore reachability, tombstone payload, TTL, stream-limit, cascade, or global GC policy here. Name only
      selected framework stores; hand product-adapter derived records to the owner notices from ADR-075
- [ ] 5.2 Correct gh#527 and cross-link remaining gh#433 cleanup scope without absorbing those changes
- [ ] 5.3 Supersede ADR-065/068 through new records without rewriting historical decisions
- [ ] 5.4 Update KV Twofer, knowledge-graph, vocabulary, index-reference, reset, and query-ordering documentation
- [ ] 5.5 Run lint, race, contracts, real-NATS integration, structural e2e, semantic e2e, and affected product suites
- [ ] 5.6 Archive this change only after every affected query-visible store satisfies `[A] -> [B] -> []` through
      an approved and implemented bounded replacement mechanism, including mechanisms delivered by explicit
      dependent changes, so graph-index and graph-query deltas become current truth
