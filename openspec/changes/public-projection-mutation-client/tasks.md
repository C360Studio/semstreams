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
- [x] 1.8 Specify one successful owner registration per Registry across direct `RegisterOwner`, `Bind`,
  `BindAndHeartbeat`, and `BindMutationClient`, including identical and concurrent second attempts.
- [x] 1.9 Clarify that birth predicates are create-only through this client, derive no lease, and are not
  graph-enforced write-once facts.
- [x] 1.10 Implement `ErrOwnerAlreadyBound` before heartbeat/claim mutation, release the identity after a failed
  first attempt, preserve the sentinel through mutation errors, and cover the global invariant with tests.
- [x] 1.11 Aggregate static built-in contracts by owner, validate the complete set, and bind it once during ownership
  wiring; keep birth-only/no-claim clients outside the registration-identity guard.

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
  and create-only birth predicates excluded from append and replacement.
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

## 5. Duplicate-Resistant Append Evidence

- [x] 5.1 Add failing tests proving blind append is not unconditionally retried.
- [x] 5.2 Add failing lost-response tests for present, absent, and unavailable authoritative evidence.
- [x] 5.3 Implement single-entity `AppendEvidence` with exact canonical tuple verification before retry.
- [x] 5.4 Add graph-ingest integration coverage for the observed commit-before-read-back ambiguity path without
  claiming absence of the late-commit double-apply race.
- [x] 5.5 Document the timeout/read-absence/late-commit race, prohibit generic outer retry, and require
  `Retry.MaxRetries=0` where strict no-retry behavior is required until #697.

## 6. Internal Migration and Documentation

- [x] 6.1 Inventory in-repository raw mutation clients and assign each to a narrow public interface.
- [ ] 6.2 Name groups and birth predicates, then migrate rule replacement and duplicated owned-fact/create helpers.
- [ ] 6.3 Remove an old helper only after call-site and behavior-parity evidence is recorded.
- [x] 6.4 Document composition-root binding, heartbeat lifecycle, stale-token recovery, commit-state handling, and
  operation-specific retry behavior.
- [x] 6.5 Document the Semdragon migration path without changing downstream code in this proposal.
- [x] 6.6 Cross-link issue #683 and state which model decisions remain owned by that issue.
- [x] 6.7 Document the fail-closed `enforce_owner_lease=true` serving-fleet gate, claim reader, live heartbeat, zero
  mismatch evidence, and the resulting Semdragon #313 adoption block.
- [x] 6.8 Keep PR #696 explicitly outside this public API change as later internal adoption.

## 7. Quality Gates

- [ ] 7.1 Run formatting, lint, unit, integration, race, schema, and applicable end-to-end suites.
- [x] 7.2 Confirm all new critical paths and ambiguity branches have behavioral tests.
- [x] 7.3 Obtain SemStreams developer implementation sign-off.
- [x] 7.4 Complete the initial mandatory Fable review for ownership, retry, wire compatibility, and public API
  stability; record its `REQUEST CHANGES` findings and obtain user approval for the internal remediation.
- [x] 7.5 Update issue #313 with slice results and keep downstream migration gated on the reviewed public contract.

## 8. Issue #700 Registration and Lease-Liveness Amendment

- [x] 8.1 Add failing table tests for birth-only, foreign-edge-only, append-only, foreign-edge-plus-append,
  owning-only, and mixed owning/non-owning contract sets.
- [x] 8.2 Separate Registry registration identity from owner-presence lease liveness. Preserve
  `ErrOwnerAlreadyBound` for persistent and liveness-managed same-Registry repeats even when presence is absent.
- [x] 8.3 Make birth-only/no-claim binding perform no registration, presence write, token mint, or heartbeater
  enrollment.
- [x] 8.4 Make foreign-edge-only, append-only, and foreign-edge-plus-append owners register persistently as one
  atomic entry with zero token, no presence, and no enrollment.
- [x] 8.5 Make any replace-owned or CAS claim classify the complete atomic owner entry as liveness-managed, with one
  non-zero token, presence record, and heartbeater enrollment covering every owner, append, and foreign-edge claim.
- [x] 8.6 Add Registry and mutation-client integration tests for persistence without presence, complete-entry lease
  expiry, token posture, enrollment posture, failed-first retry, and identical/concurrent same-owner rejection.
- [x] 8.7 Audit production and tests to prove #700 adds no implied expiry, fake presence, or selective reaping for
  permanent foreign-edge cross-type conflicts; retain that policy as a separate follow-up.
- [ ] 8.8 Run focused and repository-wide unit, race, integration, lint, schema, contract, and applicable end-to-end
  gates for the #700 implementation.
- [x] 8.9 Run strict validation for this change and the complete OpenSpec set, plus Markdown and diff hygiene.
- [x] 8.10 Obtain mandatory Fable re-review of the #700 public-contract amendment and resolve every finding before
  implementation acceptance.

## Evidence

- Focused unit, scoped audit, and race suites passed, including the tagged real-stack integration/race run.
- Real graph-ingest committed integration mutations. Lost/degraded responses were altered test-side after the real
  commit; authoritative recovery still queried the real graph-ingest path. No internal handler fault was claimed.
- Full production audit reported 500 passed and 0 failed.
- Strict OpenSpec validation reported 32 passed and 0 failed.
- The initial mandatory Fable review completed with a `REQUEST CHANGES` verdict. The completed
  [Fable re-review](https://github.com/C360Studio/semstreams/pull/687#issuecomment-5091179194) returned `APPROVE`
  with no P1 findings and no inline review threads.
- The user approved internal remediation of the recorded findings and cleared continuation of PR #687.
- B1 remediation uses duplicate-resistant language and explicitly discloses the late-commit double-apply race.
- B3 remediation adds fail-closed lease enforcement and rollout evidence on every serving graph-ingest instance.
- B4 remediation uses a Registry-wide `ErrOwnerAlreadyBound` invariant across direct and projection binding paths.
  It permits one successful registration per owner/Registry, releases failed first attempts, and aggregates static
  built-in contracts before binding. PR #696 remains later adoption.
- B5 remediation uses create-only birth-predicate language without a graph-enforced immutability claim.
- #700 materially changed the reviewed registration/liveness public contract, so Fable re-review was mandatory and
  is complete under task 8.10.
- Fable's P2 append-idempotency observation is non-gating and remains tracked in
  [#697](https://github.com/C360Studio/semstreams/issues/697#issuecomment-5092168779).
- Persistent non-owning registration retirement remains a separate follow-up under
  [#703](https://github.com/C360Studio/semstreams/issues/703).
- Issue #313 records PR #687, its local verification evidence, and the still-gated internal migration scope.
- Unrelated whole-repository baselines remain outside this ledger, including the expected dependency tracked by
  issue #686. Live PR CI, schema, and applicable end-to-end gates remain open under task 7.1.
- #700 implementation adds `Registration.ContainsOwningClaims`, owning-only presence/revival monitoring,
  compaction exemption for every non-empty non-owning entry, zero tokens for persistent append/foreign binding, and
  heartbeater enrollment only for non-zero owning tokens.
- `TestRegistrationContainsOwningClaims`, `TestEpoch_CompactStale_ExemptsEveryNonOwningNonEmptyEntry`,
  `TestRegistry_PresenceAndRevivalMonitoringFollowOwningSemantics`,
  `TestRegistry_NonOwningRepeatAndConcurrentHaveOneEpochWriteNoPresence`,
  `TestRegistry_PresenceRollbackOnlyForOwningRegistration`, and
  `TestRegistry_StaleMixedOwningEntryIsRemovedAtomically` cover Registry classification, persistence,
  same-Registry guards, side effects, and atomic stale cleanup.
- `TestBindAndHeartbeat_ModeMatrix` and `TestIntegration_BindMutationClientModeMatrix` cover birth-only,
  foreign-edge-only, append-only, foreign-edge-plus-append, owning, and mixed token/enrollment postures through
  projection binding and the public client.
- `TestRegistry_ValidNonOwningRegistrationsDoNotWarn` proves valid append-only and foreign-edge-plus-append
  registrations emit no misconfiguration warning solely for lacking owning claims.
- Architecture review required this final operational-ledger correction. Tasks 8.1 through 8.7 now reflect the
  implemented and reviewed #700 posture. Fable task 8.10 is approved; broad validation task 8.8 remains open.
- #700 documentation validation passes strict target validation, strict validation of all 32 OpenSpec items,
  Markdown lint, `git diff --check`, and the 120-column audit.
- PR CI statistical E2E passed. That focused success does not by itself close broad gate 8.8 or the pre-existing
  repository-wide gate 7.1.
