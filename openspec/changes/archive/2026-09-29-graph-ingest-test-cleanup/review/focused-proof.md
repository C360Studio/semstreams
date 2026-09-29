# Graph-ingest focused proof

## Scope and source identity

The accepted requirement is the lexical fixture ownership delta in this change; the accepted design checkpoint is
`c9870a71`. Named examples are sufficient for this private, single-owner protocol: setup escape, transferred escape,
normal terminal cleanup, explicit phase fence, terminal error, operation cancellation and once-only attempt. The
oracle follows ownership, result and ordering obligations, not the private helper's field values. There is no new
input grammar or generated-history claim.

`evidence/focused-source-hashes.json` identifies the first real integration snapshot and nine selected cases.
`evidence/final-source-freeze-after-inline.json` identifies the later 25-file snapshot after replacing 61 helper
calls with the identical native `context.WithTimeout(t.Context(), 60*time.Second)` expression. The guard had reported
60 uncertain callback roots at those callers. The final review correction to the owned-closure proof is recorded
separately; older results below remain evidence for their recorded source, not an assertion that every later edit ran.

`evidence/focused-artifact-hashes.json` identifies the original byte-for-byte scratch copies. Raw records keep
their original scratch paths; the same basenames without the `gh1423-` prefix are retained beside this document.
Backup source files use `.go.txt` so they cannot participate in compilation or the cleanup census.

## Executed checks

- Pre-change default package: `go test -race -json -count=1 ./processor/graph-ingest`, at `0085323e`, passed in
  5.017 seconds wall time: 202 top-level passes and six skips. Compact terminals are retained in
  `evidence/pre-change-unit-baseline.json`. It is not repaired integration evidence.
- Initial private-owner RED/GREEN: `evidence/owner-red.log` reaches the finite terminal-context assertion; its
  additional nil-cancel panic in the stub does not prove the retry oracle. `evidence/owner-green.log` records the
  initial passing focused suite. Later mutation evidence supplies the retry oracle.
- Restored owner examples: `go test -race -json -count=1 ./processor/graph-ingest -run '^TestGraphIngest(TestOwner|Owner)'`
  passed in 1.499 seconds package time, retained as `evidence/owner-restored-final.jsonl`. The same selection after
  inline context adoption passed in 1.472 seconds, retained as `evidence/owner-after-inline.jsonl`.
- The integration-tagged binary compiled with `go test -c -tags=integration -o
  /private/tmp/gh1423-integration-compile.test ./processor/graph-ingest`. Compilation is not integration execution.
  Its selected assertion-exit child also produced the expected failure and cleanup witnesses with an invalid Docker
  host (`evidence/tagged-child-no-docker.log`), proving this child dispatch needs no second container.
- The focused canonical runner selection below passed first try in 7.707 seconds package / 10.16 seconds wall time
  (`evidence/focused-integration.log`). Each of the nine names exists once and contains no Skip at this snapshot.
  The runner supplied race detection, additive integration tags, `-p 2`, cached NATS 2.14 Alpine and its host lock.
- Restored stale-cache and readiness-fence cases passed through the same canonical runner with `-v`: 0.44 and 0.53
  seconds case time, 2.799 seconds package time (`evidence/restored-fences-green.log`). These cases include real
  production construction, Start and Stop. No second full suite was run during iteration.

```bash
scripts/run-integration-tests.sh -run '^(TestGraphIngestOwnerAssertionExitAndCleanupError|TestAuthorityGateRejectsForeignOnFactLane|TestIntegration_AddTriples_SingleSubjectIsOneCAS|TestIntegration_KeyedIngest_PublishedEntityIngestsThroughPool|TestIntegration_PrefixQuery_ErrorClassification|TestCreateAcceptsRegisteredMessageType|TestIntegration_ReadinessEnvelope_AbsentKeyIsNotAFailure|TestComponent_HierarchyReplay_UnchangedEntitiesAdvanceNoRevision|TestIntegration_CacheStaleRepopulationRace)$' ./processor/graph-ingest
```

The warm assertion-exit unit proof remains below its five-second ceiling. The focused integration log contains
package elapsed time, not a per-case or cleanup-duration distribution. No cleanup latency comparison is claimed.
The main Linux CI graph-ingest package time of 43.671 seconds is a different platform and broader selection, not a
like-for-like speedup baseline. No total native Stop wall-clock bound is established.

## Compositional completion evidence

The unchanged `TestLifecycleOwnerRunningStopPreservesEffectSettlementOrder` in
`processor/graph-ingest/lifecycle_owner_test.go:43` exercises real keyed work and observes consumer Closed,
`processDone`, core drain and final Stop in order. That file's SHA256 is
`d1ae852375a9ff9a46de7dba7f80b745f2bf23f94dc2055e87051e998c86836c`; the retained pre-change race terminals contain its
explicit PASS. New private-owner examples add synchronous wrapper return and cancellation-order evidence. Selected
real integration logs observe Component Stop before TestClient container teardown. These are separate complementary
witnesses; the new Closed-only example alone does not exercise keyed work or real substrate teardown.

The independent reviewer accepted deferral of separate cleanup-duration measurement from the design plan: retained
failure latencies and completion observations support this bounded repair, and the report makes no cleanup-latency
distribution or comparative speedup claim. This is an explicit measurement limitation, not an unrecorded pass.

## Controlled violations

The owner implementation mutations are retained in `evidence/mutations.py` and `evidence/mutations.json`. Each used
`cp` backup/restoration, fixed test expectations, the exact focused race command in the record, a compiling mutant,
exit 1 at the named assertion, verified restored bytes and the passing restored owner selection above.

| Fault | Intended observation | Evidence |
|---|---|---|
| Remove provisional finalization | Setup-exit child lacks owned Drain witness | `mutation-provisional_removed.log` |
| Cancel accepted Start before Stop | Real concrete Stop boundary observes ended authority | `mutation-premature_start_cancel.log` |
| Discard concrete Stop error | Returned causes no longer include injected terminal failure | `mutation-discard_stop_error.log` |
| Retry after explicit failed attempt | Retained failed-Start subscription sees two Drain attempts | `mutation-implicit_retry.log` |
| Omit readiness Stop fence | Status producer has not joined before Purge | `mutation-readiness-fence.log` |

The readiness fault failed in 4.328 seconds wall time; the healthy baseline and restored run are retained. Its oracle
is an observed `statusDone` closure before Purge plus still-live operation authority. The implicit retry mutation
uses the existing failed-Start retained-handle contract: it does not infer visibility of a no-op second Stop after a
running component has already terminalized.

The separate stale-cache early-Fatal control injects an assertion while the reader is blocked. It exits 1 at that
assertion in 3.291 seconds wall time and records graceful component Stop and both container teardowns. This is a
failure-path control, not an implementation-mutation detection claim. The log has no readerDone marker; the reviewed
unconditional release/join path and runtime outcome together support the claim, rather than a fabricated marker.

## Limits and review

Keep all six existing skips. Three baseline entries are repaired skipped source and supply no runtime execution
claim; the other 29 entries are integration-tagged source. Census removal is distinct from production-path proof.
Controlled resource examples exercise real Component.Stop without successful real-NATS Start; selected integration
cases supply that complementary construction/Start evidence.

Native consumer Drain is contextless. HybridCache.Close and TTL.Close each have their own sequential five-second
wait outside the Stop context. KeyedPool.Stop can time out before all lanes finish. A finite supplied deadline does
not promise a total five-second cleanup or full join after failure. No production lifecycle contract is changed.

Independent implementation/proof review, final corrected proof, exact guard reconciliation, full canonical push
gates and archive/spec review remain separate completion records.

## Evidence provenance clarification

The final immediate-fence mutation in `evidence/readiness-immediate-mutant.json` is authoritative: it retains the
exact one-line omitted-Stop edit, before/mutant/restored SHA256, intended assertion and healthy/restored runs.
The earlier 4.328-second readiness mutation remains historical; its exact original edit transcript was not retained.
Its failure log alone is not represented as a byte-reproducible mutation record.

The stale-cache packet's `runner_completed: false` field was erroneous. Its log ends with the selected case FAIL,
both containers terminated, package FAIL and the canonical runner's failure exit; it was a completed expected
failure, not an aborted runner. The exact edit was reconstructed against retained source and matching mutant MD5:
after `addTripleLane`, replace `require.NoError(t, err)` with
`t.Fatal("injected early assertion while reader blocked")`. This is reconstructed edit evidence, not an original
mutation transcript. Raw evidence is preserved unchanged; this paragraph supplies the correction.

The four support-owner mutations captured support-source checksums during the run; they did not capture
contemporaneous test-file hashes. Their support-only driver, intended failures, checksum restoration and surrounding
source snapshots establish reconstructed lineage for the unchanged selected unit/child assertions. The reviewer
explicitly accepted this bounded evidence without a repeat solely to add timestamps. Do not describe it as
contemporaneous oracle hashing. The later join-containment correction changes a different selected test.

## Raw log retention

Seven original testcontainer logs contain trailing spaces; the final spec-check CLI log also contains trailing
spaces and an extra EOF blank line. Their reviewable `.log` copies normalize only those whitespace bytes so
repository whitespace checks remain clean. `evidence/raw-log-normalization.json` records both original and
retained-text SHA256 values; `evidence/raw-log-bytes.zip` preserves each exact original byte sequence under the same
basename. Original raw hashes in earlier evidence remain valid for those ZIP members. No failure, timestamp, phase
witness or other content was changed. All other retained logs remain byte-identical to their original copies.
