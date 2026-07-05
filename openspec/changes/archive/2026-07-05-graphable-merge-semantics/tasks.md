# Tasks — Graphable merge is predicate-level (gh#466)

> Scoping change (Proposed). Tasks unchecked; implementation follows approval.

## 1. The fix

- [x] 1.1 `MergeEntity` existing-entity branch (`component.go:1802`): replace
      `existing.Triples = append(existing.Triples, entity.Triples...)` with
      `existing.Triples = graph.MergeTriples(existing.Triples, entity.Triples)`.
- [x] 1.2 Confirm the surrounding merge (MessageType refresh, StorageRef,
      `reconcileIndexingProfile`, Version++, UpdatedAt) is unchanged and still
      correct after the triple-merge swap.
- [x] 1.3 Preserve the create-time indexing profile across the merge (ADR-054
      immutability vs MergeTriples newer-wins): strip the incoming profile before
      merging when the existing entity already has one; a profile-less stub keeps
      its first arrival's declared profile. Helpers `hasIndexingProfileTriple` /
      `triplesWithoutPredicate`. Surfaced by
      `TestIndexingProfile_ReArrival_DoesNotAccumulateOrReProfile`. Reviewer
      verified the profile is the ONLY immutable predicate reachable on this lane
      (hierarchy/stub markers never arrive on a Graphable payload).

## 2. Contract + doc correction

- [x] 2.1 Correct the `graph.MergeTriples` doc comment (`graph/helpers.go:98-100`):
      it replaces per `(subject, predicate)` (full-set semantics), NOT "all unique
      relationships preserved". State the multi-valued-predicate behavior
      explicitly so it is not a trap.

## 3. Producer/consumer safety sweep

- [x] 3.1 (done) Grep Graphable producers and `ENTITY_STATES` readers for any reliance on
      accumulated/duplicated triples (reading a predicate's history from repeated
      triples rather than KV revisions/streams). If found, it needs the
      explicit-opt-in path, not this lane's default — surface before landing.

## 4. Tests

- [x] 4.1 Reconcile existing tests: confirm `batch_integration_test.go` `base+N`
      assertions (AddTriples, distinct predicates) are unaffected; find + fix any
      test asserting `MergeEntity` re-arrival growth.
- [x] 4.2 Regression (gh#466): publish the same entity N times via the Graphable
      lane → exactly one triple per `(subject, predicate)`, not N (mirror
      semboids' `TestSnapshotsLandAndReplace`).
- [x] 4.3 (covered by existing TestIntegration_MergeEntity_SecondWriteMergesTriples: mission.phase survives a mission.command arrival) gh#177 half: a non-conflicting predicate written by a prior arrival (or
      other writer) survives a later Graphable arrival that does not carry it.
- [x] 4.4 Multi-valued predicate: an arrival with a full `flock.neighbor` set
      replaces the prior set (not union); assert full-set-replace.
- [x] 4.5 Profile immutability across re-arrival is exercised by
      `TestIndexingProfile_ReArrival_DoesNotAccumulateOrReProfile` (create-time
      profile survives a re-arrival declaring a different profile).
- [ ] 4.6 (review LOW, deferred) Explicit-declared stub-birth test: a profile-less
      stub whose first real arrival declares `entity.indexing.profile=content`
      keeps that declared profile (not the floor). Correct-by-reading; zero real
      `IndexingProfiler` implementers today, so unreachable in production —
      conscious gap.

## 5. Spec + close

- [x] 5.1 `openspec validate --strict`; gates green (`go test -race`,
      `-tags=integration` for `processor/graph-ingest`, `task lint`, schema
      no-drift); semstreams-reviewer (APPROVE — crux verified complete, MEDIUM
      spec-doc addressed); archive → promote `graph-ingest` into `openspec/specs/`.
- [x] 5.2 Confirm the fix back to semboids on gh#466 (unskip
      `TestSnapshotsLandAndReplace`); note the full-set-replace contract (on merge).
- [x] 5.3 Safety sweep (3.1) found no accumulation consumer → no opt-in follow-up
      needed.
