## 0. Governance Prerequisites

- [ ] 0.1 Correct `graph-index-hardening` to record shipped codec/catalog behavior without backdating the
      future predicate contract
- [ ] 0.2 Complete or explicitly re-scope its remaining tasks and archive it as shipped graph-index truth
- [ ] 0.3 Wait for `predicate-contract-enforcement` grammar and owned-producer cutover before raw predicate keys

## 1. Store Ownership and Filter Contract

- [ ] 1.1 Inventory every derived index key layout, token arity, semantic owner, read filter, and delete behavior
- [ ] 1.2 Encode the PR #524 owner-filter matrix in table-driven tests
- [ ] 1.3 Make INCOMING source-owned retraction authoritative and remove target-prefix hard-delete behavior
- [ ] 1.4 Identify ALIAS/spatial/embedding/blob stores that remain outside key-filter reconciliation

## 2. Real-NATS Fixed-Position Spike

- [ ] 2.1 Freeze/version the CI and 21k full benchmark datasets, environment, numeric budgets, and manifest baseline
- [ ] 2.2 Prove exact owner filters for PREDICATE, NAME, CONTEXT, INCOMING, and OUTGOING against real NATS
- [ ] 2.3 Test concurrent Put/Delete, duplicate results, cancellation, empty buckets, and error classification
- [ ] 2.4 Prove stale-row diff/retraction including `[A] -> []` for every membership store
- [ ] 2.5 Prove selected buckets are deleted/recreated empty and no old-format reader exists
- [ ] 2.6 Run five warmups and 30 measured repetitions per candidate on the registered profiles
- [ ] 2.7 Record latency, key/byte volume, allocations, server CPU/RSS, consumer cost, and reconciliation time

## 3. Predicate Representation Decision

- [ ] 3.1 Benchmark current hash+catalog and raw fixed-nine-token candidates with identical datasets
- [ ] 3.2 Compare exact lookup, namespace enumeration, membership watch, owner cleanup, bytes, and failure modes
- [ ] 3.3 Decide catalog retention/atomicity and verify raw-key compatibility with the canonical grammar
- [ ] 3.4 Write and approve a superseding ADR naming the affected ADR-065/068/073 clauses

## 4. Reconciliation and Cutover

- [ ] 4.1 Implement deduplicated owner enumeration and desired-versus-stored diff behind bounded budgets
- [ ] 4.2 Preserve keyed ordering, current-state reconciliation, bounded repair, and failure-held readiness
- [ ] 4.3 Implement source-axis INCOMING retraction without erasing live sources on target retirement
- [ ] 4.4 Apply the selected PREDICATE key/catalog format with no steady-state dual writes
- [ ] 4.5 Delete/recreate selected buckets, replay freshly reingested canonical state, and expose a watermark
- [ ] 4.6 Prove exact/namespace query, traversal, clustering, restart, and repair parity across cutover

## 5. Retention and Documentation Closeout

- [ ] 5.1 Narrow manifest/tombstone-payload requirements to stores that fail or cannot use owner filtering
- [ ] 5.2 Correct gh#527 and cross-link remaining gh#433 cleanup scope
- [ ] 5.3 Supersede ADR-065/068/073 via new records without rewriting historical decisions
- [ ] 5.4 Update KV Twofer, knowledge-graph, vocabulary, index-reference, and breaking-reset documentation
- [ ] 5.5 Run lint, race, contracts, real-NATS integration, structural e2e, semantic e2e, and affected product suites
- [ ] 5.6 Archive this change so graph-index/query/retention deltas become current truth
