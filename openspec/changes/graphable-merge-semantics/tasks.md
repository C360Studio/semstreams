# Tasks — Graphable merge is predicate-level (gh#466)

> Scoping change (Proposed). Tasks unchecked; implementation follows approval.

## 1. The fix

- [ ] 1.1 `MergeEntity` existing-entity branch (`component.go:1802`): replace
      `existing.Triples = append(existing.Triples, entity.Triples...)` with
      `existing.Triples = graph.MergeTriples(existing.Triples, entity.Triples)`.
- [ ] 1.2 Confirm the surrounding merge (MessageType refresh, StorageRef,
      `reconcileIndexingProfile`, Version++, UpdatedAt) is unchanged and still
      correct after the triple-merge swap.

## 2. Contract + doc correction

- [ ] 2.1 Correct the `graph.MergeTriples` doc comment (`graph/helpers.go:98-100`):
      it replaces per `(subject, predicate)` (full-set semantics), NOT "all unique
      relationships preserved". State the multi-valued-predicate behavior
      explicitly so it is not a trap.

## 3. Producer/consumer safety sweep

- [ ] 3.1 Grep Graphable producers and `ENTITY_STATES` readers for any reliance on
      accumulated/duplicated triples (reading a predicate's history from repeated
      triples rather than KV revisions/streams). If found, it needs the
      explicit-opt-in path, not this lane's default — surface before landing.

## 4. Tests

- [ ] 4.1 Reconcile existing tests: confirm `batch_integration_test.go` `base+N`
      assertions (AddTriples, distinct predicates) are unaffected; find + fix any
      test asserting `MergeEntity` re-arrival growth.
- [ ] 4.2 Regression (gh#466): publish the same entity N times via the Graphable
      lane → exactly one triple per `(subject, predicate)`, not N (mirror
      semboids' `TestSnapshotsLandAndReplace`).
- [ ] 4.3 gh#177 half: a non-conflicting predicate written by a prior arrival (or
      other writer) survives a later Graphable arrival that does not carry it.
- [ ] 4.4 Multi-valued predicate: an arrival with a full `flock.neighbor` set
      replaces the prior set (not union); assert full-set-replace.

## 5. Spec + close

- [ ] 5.1 `openspec validate --strict`; gates green (`go test -race`,
      `-tags=integration` for `processor/graph-ingest`, `task lint`, schema
      no-drift); semstreams-reviewer; archive → promote `graph-ingest` into
      `openspec/specs/`.
- [ ] 5.2 Confirm the fix back to semboids on gh#466 (unskip
      `TestSnapshotsLandAndReplace`); note the full-set-replace contract.
- [ ] 5.3 If the safety sweep (3.1) finds an accumulation consumer, file the
      explicit-opt-in follow-up referencing this change.
