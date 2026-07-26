# Tasks: Public Projection Mutation Client

## 1. API and Contract Validation

- [x] 1.1 Add failing `pkg/projection/contract_test.go` cases for optional group names, uniqueness, subject safety,
  birth-only contracts, canonical birth predicates, duplicates, and overlap with mutable groups.
- [x] 1.2 Extend `pkg/projection/contract.go`: add `PredicateGroup.Name` and `Contract.BirthPredicates`, keep existing
  JSON fields backward-compatible, and derive no ownership claim from birth predicates.
- [x] 1.3 Add failing mutation-client tests for binding, conditional heartbeat requirements, group selection,
  entity patterns, message type, subject checks, indexing profiles, and metadata conflicts.
- [x] 1.4 Extend `pkg/projection/mutation_types.go` with `ReplaceOwnedMutation.Group`; add public request, receipt,
  commit-state, error, and narrow interface types.
- [x] 1.5 Implement immutable contract indexing, selected-group lookup, and pre-transport validation in
  `pkg/projection/mutation_client.go`.
- [x] 1.6 Add error mapping that preserves existing classified and sentinel error inspection.
- [x] 1.7 Run focused tests and `go test -race` for the new public package.

## 2. Classified Transport and Authoritative Read-Back

- [x] 2.1 Add failing tests for the existing mutation response envelope, every graph error code, context cancellation,
  retry exhaustion, timeout ambiguity, and degraded success.
- [x] 2.2 Implement a private classified RPC adapter using the current subjects and graph wire types.
- [x] 2.3 Implement authoritative entity read-back through the existing graph-ingest query subject.
- [x] 2.4 Add wire-compatibility tests for subjects and serialized request/response shapes.
- [x] 2.5 Verify no handler, envelope, persisted schema, or `BaseMessage` change is introduced.

## 3. Atomic Create With Triples

- [x] 3.1 Add failing tests for token propagation, primary-subject atomic birth, existing-equivalent success,
  divergent conflict, lost response, absent read-back, unavailable read-back, and stable provenance.
- [x] 3.2 Add failing validation tests for cross-subject triples, including declared `ForeignEdgeClaim` triples.
- [x] 3.3 Add failing tests that reject non-empty `Entity.Triples` without mutation transport or input changes.
- [x] 3.4 Add failing tests for birth-only token-free create, create-authorized owning groups, append-only rejection,
  and immutable birth predicates excluded from append and replacement.
- [x] 3.5 Implement contract-validated `CreateWithTriples` with one existing graph request.
- [x] 3.6 Implement complete canonical-triple verification of every requested birth fact before any retry.
- [x] 3.7 Add graph-ingest integration tests for primary-subject atomicity, cross-subject rejection, fencing,
  degraded success, and conflict classification.

## 4. Schema-Derived Owned Replacement

- [x] 4.1 Add failing tests for named selection, single-group omission, ambiguous omission, unknown/non-replace
  selection, selected-group removal, sibling preservation, and unnamed backward compatibility.
- [x] 4.2 Add failing tests for delete-on-omit, birth/foreign predicate preservation, complete triple equality,
  stable token reuse, retry budget, and stale-token termination.
- [x] 4.3 Implement `ReplaceOwned` with the removal set derived only from the selected predicate group.
- [x] 4.4 Add graph-ingest integration tests for selected-group replacement, sibling preservation, and stale fencing.
- [x] 4.5 Run concurrency and race tests with one client shared by multiple goroutines.

## 5. Duplicate-Safe Append Evidence

- [x] 5.1 Add failing tests proving blind append is not unconditionally retried.
- [x] 5.2 Add failing lost-response tests for present, absent, and unavailable authoritative evidence.
- [x] 5.3 Implement single-entity `AppendEvidence` with exact canonical tuple verification before retry.
- [x] 5.4 Add graph-ingest integration tests proving an ambiguous successful append does not create a duplicate.

## 6. Internal Migration and Documentation

- [x] 6.1 Inventory in-repository raw mutation clients and assign each to a narrow public interface.
- [ ] 6.2 Name groups and birth predicates, then migrate rule replacement and duplicated owned-fact/create helpers.
- [ ] 6.3 Remove an old helper only after call-site and behavior-parity evidence is recorded.
- [x] 6.4 Document composition-root binding, heartbeat lifecycle, stale-token recovery, commit-state handling, and
  operation-specific retry behavior.
- [x] 6.5 Document the Semdragon migration path without changing downstream code in this proposal.
- [x] 6.6 Cross-link issue #683 and state which model decisions remain owned by that issue.

## 7. Quality Gates

- [ ] 7.1 Run formatting, lint, unit, integration, race, schema, and applicable end-to-end suites.
- [x] 7.2 Confirm all new critical paths and ambiguity branches have behavioral tests.
- [x] 7.3 Obtain SemStreams developer implementation sign-off.
- [x] 7.4 Obtain SemStreams reviewer approval for ownership, retry, wire compatibility, and public API stability.
- [x] 7.5 Update issue #313 with slice results and keep downstream migration gated on the reviewed public contract.

## Evidence

- Focused unit, scoped audit, and race suites passed, including the tagged real-stack integration/race run.
- Real graph-ingest committed integration mutations. Lost/degraded responses were altered test-side after the real
  commit; authoritative recovery still queried the real graph-ingest path. No internal handler fault was claimed.
- Full production audit reported 500 passed and 0 failed.
- Strict OpenSpec validation reported 32 passed and 0 failed.
- The mandatory SemStreams reviewer verdict was `APPROVE`; developer implementation evidence was accepted.
- Issue #313 records PR #687, its local verification evidence, and the still-gated internal migration scope.
- Unrelated whole-repository baselines remain outside this ledger, including the expected dependency tracked by
  issue #686. Live PR CI, schema, and applicable end-to-end gates remain open under task 7.1.
