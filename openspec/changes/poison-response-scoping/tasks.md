# Tasks — poison-response-scoping

## 1. Gates before implementation

- [x] 1.1 Adversarial review (5-lens) of proposal + design + spec deltas; verdicts + dispositions
  recorded in `adversarial-review.md`; artifacts revised to the post-review shape
- [x] 1.2 Audit `predicate-contract-enforcement`'s spec deltas, checked tasks, and proposal (not
  just open tasks) for latch/readiness assertions — found the `predicate-contract` beta-cutover
  conflict; resolved via our MODIFIED delta + hard ordering dependency (theirs syncs first)
- [x] 1.3 Finalize ADR-079 (Proposed → Accepted) and file the `graph/query/client.go`
  watcher-tax follow-up issue it names

## 2. Contract plumbing (graph package)

- [x] 2.1 Add optional `EntityID` field to `graph.StateContractError`; fix the doc comment at
  `graph/state_contract.go:68-70` ("permanent until restart" is now class-conditional); unit
  tests for Error() rendering with/without ID
- [x] 2.2 Stamp EntityID inside the closures/goroutines where identity lives (sweep list from
  design D4): MergeEntity both branches, AddTriple, AddTriples (fix the `casErr.Error()`
  stringification to propagate the typed error), RemoveTriple, `update_with_triples` closure,
  `query.go` batch-fetch goroutine, `fetchEntityState` callers, boot sweep `entry.Key()`
- [x] 2.3 Round-trip tests: EntityID does not ride `Detail` (in-process typed error only, wire
  carries class/code); `StateContractError` given explicit JSON tags (`Err` excluded) + marshal
  shape test; DebugStatus inventory shape round-trip test

## 3. Per-entity poison inventory (graph-ingest)

- [x] 3.1 Replace global `entityStatePoison` with a mutex-guarded revision-stamped map +
  `atomic.Int64` size; **atomic emptiness fast-path on the commit path** (required, design D2);
  once-per-entity ERROR (map as dedup); boot-sweep log cap (first 100, then count-only WARN)
- [x] 3.2 Gauge (single, no per-entity labels); Health: `Healthy=false`, `Status="degraded"`,
  count + first-10 IDs + bounded reasons; `DebugStatus()` full-inventory enumeration
- [x] 3.3 Clear paths: DeleteEntity; successful commit with revision > recorded;
  **any successful validating read of the key**; verify no read/write path consults the map
  (observability-only invariant); record-side cache invalidation (`entityCache.Delete` on
  inventory-record)
- [x] 3.4 Wire RMW classification into the inventory at every closure caller of
  `classifyStoredStateRMWError` (MergeEntity, AddTriple, AddTriples, RemoveTriple,
  update_with_triples) — recorded at the shared classification seam itself, so every closure
  caller inherits it

## 4. Guard lifecycle (graph-ingest snapshot-then-stop)

- [x] 4.1 Rework `startEntityStateGuard`: synchronous drain → validate into inventory
  (last-revision-wins per key) → `watcher.Stop()` → **drain updates channel to close,
  discarding, never marking watch-lost** (design D1 mandatory shape); delete
  `runEntityStateGuard`; make the watcher a local (delete the `entityStateWatcher` field and
  its Stop() teardown lines)
- [x] 4.2 Preserve snapshot transport-failure semantics; document the History=1 assumption at
  the drain site

## 5. Query, mutation, and ingest scoping (graph-ingest)

- [x] 5.1 Delete the dead latch protocol outright: `entityQueryMu` ceremony,
  `finalizeEntityQueryResponse`, `checkEntityQueryReady` lock discipline,
  `latchEntityStatePoison`, `beforeEntityQueryResponse` hook; readiness = atomic check at
  handler entry
- [x] 5.2 Aggregate reads: collect ALL poisoned entities at the batch merge point, fail with
  the typed error naming the bounded list, inventory all in the same attempt; sweep every
  multi-entity read path (batch, prefix), not just `fetchEntitiesConcurrent`
- [x] 5.3 Fix the three mutation read seams (`mutations.go` entity.update read,
  update_with_triples CAS read, create_with_triples restamp read-back): typed fatal
  `graph_state_reset_required`, not `rejectInternal`
- [x] 5.4 `processIngest`: resident-poison (`StateContractError`) → **Nak** (MaxDeliver-bounded)
  + inventory record; candidate-invalid stays Term; Nak backoff decided: plain Nak, the
  consumer's existing delivery policy owns backoff (documented at the disposition split)

## 6. Rule + clustering guard retirement, agentic-loop rescope

- [x] 6.1 rule: remove `startGraphStateGuard` full-firehose watcher; prove the sticky
  evaluation kill switch still latches on consumed poison via the input path (test); zero
  entity patterns → zero ENTITY_STATES watchers (test)
- [x] 6.2 clustering: remove `startEntityContractWatch`; prove input-path validation drives the
  sticky projection latch with coverage equivalence (test) before deletion; one watcher total
  (wire-level assertion)
- [x] 6.3 agentic-loop: rescope `graph.IsStateContractError` reaction from component-wide
  latch + hold-until-restart to per-loop failure; task intake continues; remove the
  hold-until-restart machinery for this class

## 7. Tests (graph-ingest)

- [x] 7.1 Rewrite `query_contract_guard_test.go` + named siblings (`keyed_ingest_test.go`,
  `merge_entity_write_gate_test.go` poison assertions): per-entity refusal; **invert**
  `TestQueryDiscoveredPoisonBlocksConcurrentReadyResponse` (concurrent valid response SERVES —
  now `TestQueryDiscoveredPoisonServesConcurrentReadyResponse`);
  fix the mock watcher's Stop() to close its updates channel (real nats.go semantics —
  drain-to-close hangs against the current mock)
- [x] 7.2 Wire-level integration: no graph-ingest guard consumer on the ENTITY_STATES stream
  after Start; post-Start write delivered to no graph-ingest watcher
  (`poison_scoping_integration_test.go`)
- [x] 7.3 Boot sweep: resident poison → inventoried + ERROR + gauge, boots; last-revision-wins
  (poisoned then valid pre-marker revision → no entry)
- [x] 7.4 Repair recovery: delete → gauge 0, Health recovers, fresh create serves;
  out-of-band valid overwrite → clears on next successful read; concurrent
  record-vs-newer-commit → no stale entry (revision guard); re-poison → re-inventoried +
  re-logged
- [x] 7.5 Aggregate: [A,C poisoned, B valid] → typed error names A and C, both inventoried in
  one attempt, no silent omission; mutation seams return typed fatal (three seams + the RMW
  closure path)
- [x] 7.6 Nak disposition: resident-poison arrival Nak'd, applies on redelivery after repair;
  candidate-invalid still Term'd (`TestProcessIngest_InvalidGraphableTerminatesBeforeGuardIO`)
- [x] 7.7 Full gates: `task lint`, `go test -race ./...`, branch integration sweep
  (`go test -race -tags=integration ./...` — graph package touched), contract tests, schema
  no-drift; slog-capturing tests non-parallel

## 8. Docs, issue, rollout

- [x] 8.1 Runbook: alert on the gauge + Health message (NOT `/components/health` status code —
  binary 503); **capture-before-delete** (History=1 destroys forensic bytes); no out-of-band
  purge; guard-bucket reset on stream reseed; mass-poison escalation to the clean-wipe/reseed
  contract (docs/operations/17) above a threshold; co-resident sticky consumers (rule kill
  switch once poison consumed, lifecycle manager, projection owners) still restart per their
  contracts
- [x] 8.2 Bench + e2e evidence: ingest-lane before/after on this branch; `task e2e:structural`
  green pre-tag
- [x] 8.3 gh#562 reply: three-watcher localization presented as OUR inference on their
  measurement (not their conclusion), the consumer-report discriminator, candidate build
  offered for the beta.146-vs-candidate A/B
- [ ] 8.4 After semboids A/B: record macro recovery on gh#562; any shortfall now indicts a
  non-watcher contributor — file separately, do not widen this change
