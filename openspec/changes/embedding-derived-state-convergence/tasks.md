# Tasks: embedding-derived-state-convergence

## 1. Verification (DONE pre-change, 2026-07-28 — recorded for the ledger)

- [x] 1.1 Premises re-verified at `f7965f0e`: no repair loop in graph-embedding; `SavePending`/
  `SavePendingWithStorageRef` unconditional `Put`; graph-index precedent intact
  (`processor/graph-index/component.go:965,1056-1123`).
- [x] 1.2 Coalescer consumer sweep: **semsource enables `coalesce_ms` by DEFAULT at 200ms**
  (`../semsource/cmd/semsource/run.go:742,761,800,821`) — issue #629's "dormant/opt-in" framing
  falsified ecosystem-wide; deletion off the table. semspec's `coalesce_ms:500` is graph-INDEX's knob
  (irrelevant). No semstreams config sets it (the e2e coverage gap task 4.2 closes).
- [x] 1.3 Restart-recovery legs for #625 verified: `WatchAll` no-options delivers tombstones
  last-per-subject; zero `PurgeDeletes` calls repo-wide; `ENTITY_STATES` retention-guard-asserted.

## 2. #629 — single-writer hop-1 seam (TDD; drive tests via a KV decorator on the unexported bucket
fields from in-package tests — no production hooks; precedent `graph/embedding/memkv_test.go`)

- [x] 2.1 T1 failing first — the issue's resurrection repro: fake `entityStatesBucket.Get` does the
  real Get, signals, blocks, returns the captured pre-delete entry; tombstone applied mid-window;
  assert `GetEmbedding(K) == nil` post-join AND the tombstone's delete did not complete before
  release (ordering leg). Must be RED on current code.
  FINDING (2026-07-28): observed RED on pre-change code on BOTH legs in one run (ordering leg
  `t.Error`, not Fatal, so both discriminators surface): "tombstone delete completed while the
  coalesced flush's authoritative read was in flight" AND resurrection (record present post-join at
  `SourceRevision:0x4` — the stale flush recreated the key AFTER the delete). GREEN post-seam.
  Test: `TestHop1Seam_CoalescedFlushCannotResurrectTombstonedEntity`
  (`processor/graph-embedding/derived_state_convergence_test.go`).
- [x] 2.2 T2 failing first — coalesced lane converges on authoritative absence (tombstone before
  flush): assert the derived record is DELETED (today: silent drain, never deletes).
  FINDING: observed RED pre-change on BOTH absence sentinels (subtests ErrKeyNotFound and
  ErrKeyDeleted): record remained (`Status:"pending"`) after the flush. GREEN post-change; watermark
  drain asserted unchanged. Test: `TestCoalescedFlush_AuthoritativeAbsenceDeletesDerivedRecord`.
- [x] 2.3 Implement `hop1Mu` + `reconcileEntity` per design (full JetStream sentinel set via
  `errors.Is`; max-rev drain idiom; present → `queueEntityForEmbedding` only). Collapse
  `processEntityBatch` to the reconcile loop. Watcher update/tombstone paths take the seam.
  FINDING: sentinel set covered via `natsclient.IsKVNotFoundError` (errors.Is over BOTH
  ErrKeyNotFound and ErrKeyDeleted — the graph-index reconcileEntity precedent, a strict superset of
  the two-sentinel check). Tombstone path extracted to `applyEntityTombstone` (seam-held); immediate-
  mode watcher update takes the seam inline. One design elaboration: reconcile's absence-branch
  delete FAILURE also marks `ReasonDeleteFailed` — required because the branch's max-rev
  `OutcomeDeleted` drain clears the map entry, so without the re-mark ONE failed repair pass would
  permanently clear the mark and recreate the #625 leak (commented at the site).
- [x] 2.4 Lock-order invariant in code comments: `hop1Mu` → `failedMu`, never reverse; no `c.mu`
  under `hop1Mu`. T7 immediate-mode regression green (`coalesce_ms:0` semantics unchanged).
  FINDING: invariant stated on the `hop1Mu` field doc and on `repairTargets`. T7
  (`TestImmediateMode_SemanticsUnchanged`) green; pins pending-at-delivered-revision, hop-2-owns-
  terminal, tombstone-deletes-and-drains.
- [x] 2.5 `Stop` hardening: `c.cancel()` before `entityCoalescer.Close()`.
  FINDING: cancel moved to the top of Stop's teardown (before coalescer Close, unsubscribes, worker
  stop); the trailing duplicate cancel removed. Full unit+integration suites green under -race with
  the new ordering.

## 3. #625 — markStranded + repair loop (TDD)

- [x] 3.1 T3 failing first — failed delete: record present, `FailedCount==1` reason `delete_failed`,
  `State==degraded`, watermark STILL drained at tombstone revision (#624 invariant asserted
  explicitly); then flip fake, call `repairStranded(ctx)` directly (no ticker/sleep) → record absent,
  `FailedCount==0`, `State==ready`.
  FINDING: staged TDD — the marking half (record present, count 1 reason delete_failed, watermark
  drained at rev 6, degraded) compiles pre-change and was observed RED ("expected 0x1, actual 0x0"
  on FailedCount); the repair half references `repairStranded`, which does not exist pre-change (its
  fails-first evidence is a compile failure, noted as the weaker form), and was appended
  post-implementation; full test GREEN + 3× stable. State degraded/ready asserted through
  `graph.ComputeIndexStatus` over the component's real `failedSnapshot`/watermark values — the exact
  projection `computeEmbeddingStatus` wires — because `natsclient.BucketLastSeq` requires the
  concrete `*jetstream.KeyValueBucketStatus` a fake cannot construct; the full production compute
  path's State is asserted in T8 against real NATS. Test: `TestFailedDelete_MarksStrandedAndRepairs`.
- [x] 3.2 T4 failing first — `SavePending` failure strands (reason `pending_write_failed`, watermark
  NOT completed per #613 F2) and repairs.
  FINDING: same staging as 3.1 — marking half observed RED pre-change ("expected 0x1, actual 0x0"
  on FailedCount; the #613 F2 watermark-not-completed leg holds on both sides, asserted explicitly);
  repair half appended post-implementation: repair re-queues the pending record at the FRESH
  authoritative revision (7), and the mark clears at the simulated hop-2 terminal (the production
  onTerminal path), not at repair time. Test: `TestSavePendingFailure_MarksStrandedForRepair`.
- [x] 3.3 Implement `markStranded` (floor revision 0 — comment WHY: the applyTerminalOutcome guard
  makes any higher pin unclearable), the three marking sites (tombstone-delete failure = complete at
  TRUE revision R then mark at 0 — two calls, different revisions, both load-bearing; SavePending
  failure; flush read failure), 3 in-memory reason consts.
  FINDING: consts are `ReasonDeleteFailed`/`ReasonPendingWriteFailed`/`ReasonEntityReadFailed` in
  `graph/embedding/derived_reasons.go` (exported, doc states in-memory-only + why). markStranded's
  doc also notes the guard's benign edge: an entity already failed at a real revision keeps that
  newer failure (already degraded; re-delivery recovery unchanged). SavePending marking added to
  BOTH hop-1 lanes (inline `SavePending` and `SavePendingWithStorageRef`). Drain-THEN-mark ordering
  is load-bearing at every site (the drain's map-clear runs at max-rev/tombstone-rev and would
  remove a mark made before it) — commented at the sites.
- [x] 3.4 Implement `repairTargets` (snapshot under `failedMu`, release before dispatch) +
  `repairStranded` (reason-scoped) + 12-line 30s `repairLoop` (empty-set short-circuit,
  `c.wg`-registered, launched with the existing goroutines).
  FINDING: `embeddingRepairInterval = 30s`; repairLoop launched in
  `waitForDependenciesAndStartWatcher` between the entity watcher and the status loop (post-
  dependency, wg-registered, ctx-cancelled). Dedicated-goroutine rationale (vs the ADR-083 heartbeat)
  in the repairLoop doc. Unbounded-flat-retry justification in the doc (reason-scoped set → no
  poison class).
- [x] 3.5 T5 — repair-set scoping: embedder-side reason (`connection_refused`) NOT re-driven.
  FINDING: asserted via the stub's recorded Gets (no authoritative re-read) AND the failure staying
  counted with its reason untouched. Test: `TestRepairScope_EmbedderSideReasonNotReDriven`.
- [x] 3.6 T6 — in-memory-only invariant: the three reasons never reach `SaveFailed`;
  `normalizeFailureReason` NOT extended.
  FINDING: two guards. Component-side `TestStrandedReasons_NeverPersistToStoredRecords` drives all
  three stranding sites with a write-recording index bucket: all three reasons present in the
  in-memory accounting (discriminating precondition) while NO attempted durable write carries
  `StatusFailed` or any derived-write reason. Package-side
  `graph/embedding/derived_reasons_test.go` (`TestDerivedReasons_NotInPersistedFailureEnum`) pins
  `normalizeFailureReason(reason) == "unknown"` for all three — goes red if anyone extends the
  persisted enum.
- [x] 3.7 Reviewer round (semstreams-reviewer, CHANGES-REQUESTED → addressed): HIGH class-scope
  catch — the no-text-transition delete failure in `queueEntityForEmbedding` was an UNMARKED member
  of the failed-derived-delete class the spec delta guarantees repair for (reached by immediate
  watcher, coalesced reconcile, AND repair). Two failure shapes: (a) live entity with a served
  StatusGenerated vector transitions to no-text, delete fails → FailedCount 0 / ready with the
  stale vector queryable, unrepaired because unmarked; (b) repair-masking — a repair re-drive's
  fresh-revision Skipped CLEARED a prior floor-0 mark, so degraded cleared without convergence.
  FIX (smallest, mirrors the reconcile absence branch): capture delErr; KEEP the Skipped completion
  (drain-THEN-mark — the watermark must drain, ADR-066 §3, and the ordering is what lets the
  re-mark survive the Skipped's clear); `markStranded(ReasonDeleteFailed)` on failure; site comment
  rewritten to name the StatusGenerated harm (the old comment named only StatusFailed residue).
  TDD: both legs observed RED pre-fix ("expected: 0x1, actual: 0x0" on FailedCount in each) —
  `TestNoTextTransition_FailedDeleteMarksStrandedAndRepairs` (mark + degraded + watermark drained
  at delivered revision + repairStranded converges to absent/ready) and
  `TestNoTextTransition_FailedDeleteDoesNotMaskPriorStranding` (pre-stranded
  pending_write_failed survives the failing no-text re-drive as ReasonDeleteFailed). GREEN + 3×
  stable post-fix; both packages -race green; lint/gofmt clean; go.mod/go.sum byte-clean. NIT also
  addressed: proposal complexity ledger corrected from 4 → 6 unexported methods (named).

## 4. Integration + fixtures

- [x] 4.1 T8 integration (testcontainers): `coalesce_ms:50`, update burst then tombstone,
  `require.Eventually` on state predicates only (record absent, `FailedCount==0`) — no timing bounds.
  FINDING: `TestIntegration_CoalescedLane_TombstoneConverges`
  (`processor/graph-embedding/derived_state_convergence_integration_test.go`) — Eventually predicate
  is record-absent AND `failedSnapshot()==0` AND `computeEmbeddingStatus(ctx).State == ready` (the
  ready leg proves the watermark drained through burst + tombstone over the FULL production compute,
  incl. BucketLastSeq — closing the unit-side gap noted at 3.1). Green in 0.79s; 3× stable under
  -race against real NATS.
- [x] 4.2 Config-only fixture edit: set `coalesce_ms` on graph-embedding in ONE statistical-tier e2e
  config (own commit inside the change so tier variance is attributable). tasks note: the
  deterministic tests are the REAL gate for the coalesced lane — e2e-green does not imply coalesced
  coverage beyond this fixture.
  FINDING: `configs/statistical.json` graph-embedding block gains `"coalesce_ms": 100` (the config
  `task e2e:statistical` mounts via `docker/compose/tiered.yml` → `/app/configs/statistical.json`).
  JSON carries no comments, so the rationale lives HERE: this makes the statistical tier boot the
  coalesced lane (previously uncovered by every semstreams e2e config while semsource defaults it on
  at 200ms); the deterministic unit/integration tests above are the REAL gate for the lane —
  e2e-green does not imply coalesced coverage beyond exercising this fixture. Own-commit split is
  the orchestrator's call at commit time (this change was implemented uncommitted by instruction).

## 5. Specs + docs

- [x] 5.1 Spec deltas are the contract — `openspec validate --strict` clean; implementation walked
  against every scenario.
  FINDING: `openspec validate embedding-derived-state-convergence --strict` → "is valid". Scenario
  walk: debounced-requeue-cannot-resurrect → T1 + seam; coalesced-absence-deletes → T2 +
  reconcile absence branch; failed-delete-repaired-until-absent + watermark-unchanged → T3;
  degraded-not-ready-without-pinning-watermark → T3 (#624 leg); derived-reasons-never-persist → T6
  pair; create-lane-cannot-resurrect → T1 + sole-writer-under-seam construction; the pre-existing
  newest-revision/CAS scenarios → untouched storage lanes, existing tests still green.
- [x] 5.2 Changelog notes: degraded-instead-of-ready for failed derived writes; coalesced flush now on
  the watcher goroutine (bounded, quantified in design).
  FINDING: the repo carries no CHANGELOG.md; the two behavior-change notes are recorded here for the
  PR body / release notes: (1) a failed derived write/delete/read now reports `degraded` (previously
  `ready` until restart) and self-heals via a 30s background repair loop; (2) with `coalesce_ms > 0`
  the coalesced flush's hop-1 work now serializes with the watcher (2E round-trips per window on the
  watcher goroutine vs the immediate mode's N; cheaper whenever debounce dedup N/E > 2, worst case
  ~2× at low dedup — design §Decision 1). NOT BREAKING: no exported API or record-shape change.

## 6. Gates (all under `GOFLAGS=-mod=readonly`; go.mod/go.sum byte-clean checked after every stage)

- [x] 6.1 `task lint` / `go vet` (+`-tags=integration`, `-tags=live_llm`) / `gofmt -l` clean.
  FINDING: all clean (revive included, zero warnings). NOTE for the record: mid-session go.mod/go.sum
  contamination appeared (semsource + oauth2 bump) DESPITE every command running under
  `GOFLAGS=-mod=readonly` — an external writer (editor tooling with the user-global `-mod=mod`) is
  the only consistent explanation; no Go file imports semsource and there is no go.work. Restored
  byte-clean from HEAD via `git show HEAD:go.mod/go.sum >` redirection (no working-tree git
  commands) and re-verified clean after every subsequent gate.
- [x] 6.2 Full `go test -race ./...` green; new tests 3× stability.
  FINDING: full suite exit 0, 135 packages ok, zero FAIL lines (grepped `^FAIL` explicitly, not the
  pipeline tail). All 8 new unit tests + the T8 integration test run `-count=3` green under -race.
- [x] 6.3 Framework-package branch integration sweep `go test -race -tags=integration ./...`.
  FINDING: full-repo sweep exit 0, 136 packages ok, zero FAIL (scoped
  `./graph/... ./processor/graph-embedding/...` pass first, then the full `./...` per this task's
  wording; Docker/testcontainers live).
- [x] 6.4 `task schema:generate` no drift expected (verify); `go test ./test/contract/...`.
  FINDING: schema:generate produced zero diff under `schemas/ specs/` (verified via git status +
  diff, both empty — expected: no config-schema surface changed). Contract tests ok.
- [x] 6.5 `task e2e:statistical` green (NOT BREAKING — gate is confirmatory; includes the 4.2 fixture).
  **GREEN with the coalesce_ms:100 fixture — first-ever e2e coverage of the coalesced lane: success:true,
  0 errors, 0 data loss, embeddings drained 0 failed; resolved_total 107 vs ~250s immediate-mode baseline
  at identical 68 fresh generations (the debounce visibly collapsing redundant re-resolutions). Run
  predates the round-6 HIGH fix, which is failure-path-only marking (unreachable in a green e2e run) —
  the deterministic tests are that path's gate, per 4.2's own note.**
  NOTE: left to the orchestrator's final-gate run per the implementation handoff (the developer-run
  verify list excluded the e2e tier); the 4.2 fixture means this tier now boots the coalesced lane.

## 7. PR + review + merge

- [ ] 7.1 Branch off main; PR references #625/#629 + the semsource default-on correction;
  conventional commit (fix scope, NOT breaking).
- [x] 7.2 `semstreams-reviewer` pre-merge — explicit lenses: T1's fails-first proof on the REAL
  concurrency shape; lock-order invariant; markStranded floor-revision trap; hop-2 must NOT take the
  seam; complexity ledger conformance (zero durable state).
  **Round 1 CHANGES-REQUESTED: one HIGH — the no-text-transition delete failure was an unmarked member
  of the spec's own "failed derived-record delete" class, with a repair-masking variant (Skipped clearing
  a prior mark without convergence); fixed per prescription with both legs red-first (task 3.7). All
  other lenses verified clean; ledger conforming; APPROVE per the reviewer's stated condition, fix
  verified at the site by the orchestrator.**
- [ ] 7.3 Owner-run Codex gate; merge on addressed + CI-green (`gh pr checks` + `mergeStateStatus`);
  archive + baton roll-forward (include the pending cache-class/e2e-ladder-comment baton notes).
