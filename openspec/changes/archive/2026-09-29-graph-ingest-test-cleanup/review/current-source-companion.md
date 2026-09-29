# Graph-ingest cleanup current-source companion

base: c9870a71e425445c5147a017c50c2f55b56e6f20

This is a bounded refresh of changed implementation source, not a replacement inventory or approval.
The working tree is uncommitted relative to base; the exact 25-file content hashes identify the inspected snapshot.
Version 3 refreshes only four audit-annotation coordinates in two source files after the context-first correction.
The fixed 32/44/17 mapping and all source pins are unchanged. Review authority remains with the independent reviewer.
The preceding v2 companion review (SHA256
`8a0937a8289fe66b0915aa4e5b1e40d2dd0eed6310eca4c825cd6dacf46358df`) is superseded by this current-source record.
Accepted pre-change evidence remains verbatim at `0085323e:openspec/changes/graph-ingest-test-cleanup/inventory.md`,
SHA256 `c7b4178dc00f4fdab4fad2f39a21db7969aa56132e3df9873ba232eb39e55e12`; its baseline/reference companions remain historical.

Frozen source manifest: `evidence/final-source.json`, SHA256 `3a9b31e446490985399d5e8af0ac36111fe9b03a475239ffe88b6342af953c7e`.
Machine-readable mapping: `current-source-companion.json`, SHA256 `9a8b16c6e5cb472e3725e493c24102a9b05c1042f6454b4e11cf8ef29fd35057`.
All 25 manifest hashes matched before and after this refresh. JSON includes every source hash, original identity,
fingerprint and relocated owner/Initialize/Start/finalizer line, plus all 44 accepted typed caller locations.

## Original 32 identities: current ownership sites

Each following pin maps one original approval in the JSON, in original record order. These are 29 integration-tagged
sites and three still-skipped default bodies. SiblingEdges keeps its distinct ordinal 1/2 mapping.
The replay mapping is the replay owner; its additional seed owner and explicit fence are listed separately.
Six helper pins are provisional finalizers; their successful transfers and caller defers are accounted for below.

- `processor/graph-ingest/authority_gate_integration_test.go:174` — `defer owner.provisionalFinish(ctx, t)`
- `processor/graph-ingest/batch_integration_test.go:43` — `defer owner.provisionalFinish(ctx, t)`
- `processor/graph-ingest/cas_integration_test.go:33` — `defer owner.finish(ctx, t)`
- `processor/graph-ingest/component_test.go:547` — `defer owner.finish(ctx, t)`
- `processor/graph-ingest/component_test.go:631` — `defer owner.finish(ctx, t)`
- `processor/graph-ingest/component_test.go:607` — `defer owner.finish(ctx, t)`
- `processor/graph-ingest/hierarchy_replay_integration_test.go:145` — `defer replayOwner.finish(ctx, t)`
- `processor/graph-ingest/hierarchy_sync_integration_test.go:219` — `defer owner.finish(ctx, t)`
- `processor/graph-ingest/hierarchy_sync_integration_test.go:614` — `defer owner.finish(ctx, t)`
- `processor/graph-ingest/hierarchy_sync_integration_test.go:459` — `defer owner.finish(ctx, t)`
- `processor/graph-ingest/hierarchy_sync_integration_test.go:56` — `defer owner.finish(ctx, t)`
- `processor/graph-ingest/hierarchy_sync_integration_test.go:551` — `defer owner.finish(ctx, t)`
- `processor/graph-ingest/hierarchy_sync_integration_test.go:280` — `defer owner.finish(ctx, t)`
- `processor/graph-ingest/hierarchy_sync_integration_test.go:442` — `defer owner.finish(ctx, t)`
- `processor/graph-ingest/hierarchy_sync_integration_test.go:664` — `defer owner.finish(ctx, t)`
- `processor/graph-ingest/hierarchy_sync_integration_test.go:764` — `defer owner.finish(ctx, t)`
- `processor/graph-ingest/hierarchy_sync_integration_test.go:138` — `defer owner.finish(ctx, t)`
- `processor/graph-ingest/keyed_ingest_integration_test.go:60` — `defer owner.finish(ctx, t)`
- `processor/graph-ingest/keyed_ingest_integration_test.go:210` — `defer owner.provisionalFinish(ctx, t)`
- `processor/graph-ingest/merge_entity_integration_test.go:276` — `defer owner.finish(ctx, t)`
- `processor/graph-ingest/poison_scoping_integration_test.go:133` — `defer owner.finish(ctx, t)`
- `processor/graph-ingest/poison_scoping_integration_test.go:57` — `defer owner.finish(ctx, t)`
- `processor/graph-ingest/query_integration_test.go:50` — `defer owner.finish(ctx, t)`
- `processor/graph-ingest/query_prefix_integration_test.go:44` — `defer owner.provisionalFinish(ctx, t)`
- `processor/graph-ingest/query_wire_contract_integration_test.go:54` — `defer owner.finish(ctx, t)`
- `processor/graph-ingest/readiness_gauges_integration_test.go:51` — `defer owner.finish(ctx, t)`
- `processor/graph-ingest/readiness_integration_test.go:181` — `defer owner.finish(ctx, t)`
- `processor/graph-ingest/readiness_integration_test.go:259` — `defer owner.finish(ctx, t)`
- `processor/graph-ingest/readiness_integration_test.go:341` — `defer owner.finish(ctx, t)`
- `processor/graph-ingest/readiness_integration_test.go:78` — `defer owner.provisionalFinish(ctx, t)`
- `processor/graph-ingest/registered_type_gate_integration_test.go:49` — `defer owner.provisionalFinish(ctx, t)`
- `processor/graph-ingest/resident_stamp_integration_test.go:57` — `defer owner.finish(ctx, t)`

Every mapped finalizer precedes its Initialize/Start assertion in the inspected declaration. The six provisional
helpers transfer only after setup, so helper assertion exits retain local ownership. Direct callers hold lexical
ownership immediately after construction. This is source-order evidence; runtime behavior is separately reviewed.

## Six returning helpers and accepted caller relocation

| Helper | Relocated accepted calls | Provisional finalizer | Transfer |
|---|---:|---|---|
| startAuthorityGateComponent | 10 | `processor/graph-ingest/authority_gate_integration_test.go:174` | `processor/graph-ingest/authority_gate_integration_test.go:180` |
| startBatchTestComponent | 21 | `processor/graph-ingest/batch_integration_test.go:43` | `processor/graph-ingest/batch_integration_test.go:48` |
| startKeyedWireComponent | 2 | `processor/graph-ingest/keyed_ingest_integration_test.go:210` | `processor/graph-ingest/keyed_ingest_integration_test.go:214` |
| startPrefixTestComponent | 5 | `processor/graph-ingest/query_prefix_integration_test.go:44` | `processor/graph-ingest/query_prefix_integration_test.go:50` |
| startIngestForReadiness | 2 | `processor/graph-ingest/readiness_integration_test.go:78` | `processor/graph-ingest/readiness_integration_test.go:89` |
| startGateTestComponent | 4 | `processor/graph-ingest/registered_type_gate_integration_test.go:49` | `processor/graph-ingest/registered_type_gate_integration_test.go:53` |

All 44 calls from the accepted typed-reference companion were relocated within the same enclosing declarations.
Each is immediately followed by its caller lexical finish defer, with an earlier finite t.Context-derived operation
context and deferred cancellation. No additional callers or missing callers are inferred from this textual refresh.
The extra caller files beyond the original 16 are included in the exact frozen manifest and JSON.

- `processor/graph-ingest/authority_gate_integration_test.go:137` — `func startAuthorityGateComponent(ctx context.Context, t *testing.T, enableHierarchy bool) *authorityGateHarness {`
- `processor/graph-ingest/batch_integration_test.go:23` — `func startBatchTestComponent(ctx context.Context, t *testing.T) (*Component, *graphIngestTestOwner) {`
- `processor/graph-ingest/keyed_ingest_integration_test.go:195` — `func startKeyedWireComponent(ctx context.Context, t *testing.T) (*Component, *natsclient.TestClient, *graphIngestTestOwner) {`
- `processor/graph-ingest/query_prefix_integration_test.go:25` — `func startPrefixTestComponent(ctx context.Context, t *testing.T, opts ...testComponentOption) (*Component, *natsclient.Client, *graphIngestTestOwner) {`
- `processor/graph-ingest/readiness_integration_test.go:62` — `func startIngestForReadiness(ctx context.Context, t *testing.T) (*natsclient.TestClient, *Component, *graphIngestTestOwner) {`
- `processor/graph-ingest/registered_type_gate_integration_test.go:32` — `func startGateTestComponent(ctx context.Context, t *testing.T, reg *payloadregistry.Registry, enableHierarchy bool, opts ...testComponentOption) (*Component, *natsclient.Client, *graphIngestTestOwner) {`

## Private owner and proof source

The private owner stores a concrete component, cancellation and attempt/transfer booleans. It creates a cancellable
Start child, marks a terminal attempt before the real Stop call, supplies a fresh detached five-second context,
checks concrete and context errors, and cancels Start after the synchronous call. finish reports with Errorf and
does not retry. Provisional ownership stays local until transfer. The private CancelFunc edge has one installed manual
resolution; independent approval and its finish dependency refresh are recorded in `review/manifest-review.md`.

- `processor/graph-ingest/test_owner_support_test.go:11` — `type graphIngestTestOwner struct {`
- `processor/graph-ingest/test_owner_support_test.go:23` — `ctx, cancel := context.WithCancel(parent)`
- `processor/graph-ingest/test_owner_support_test.go:32` — `o.attempted = true // A returned error or panic never grants an implicit second attempt.`
- `processor/graph-ingest/test_owner_support_test.go:34` — `defer o.cancelStart() // Accepted Start authority stays live through concrete Stop.`
- `processor/graph-ingest/test_owner_support_test.go:36` — `stopCtx, cancelStop := context.WithTimeout(context.WithoutCancel(operationCtx), 5*time.Second)`
- `processor/graph-ingest/test_owner_support_test.go:38` — `stopErr := o.component.Stop(stopCtx)`
- `processor/graph-ingest/test_owner_support_test.go:39` — `if err := stopCtx.Err(); err != nil {`
- `processor/graph-ingest/test_owner_support_test.go:42` — `if err := operationCtx.Err(); err != nil {`
- `processor/graph-ingest/test_owner_support_test.go:54` — `t.Errorf("graph-ingest terminal cleanup: %v", err)`
- `processor/graph-ingest/test_owner_support_test.go:60` — `if !o.transferred {`
- `processor/graph-ingest/test_owner_support_test.go:66` — `o.transferred = true`

Named source examples cover controlled authority and operation usability, retained failed-start handles that expose
an erroneous retry, canceled operation with fresh terminal authority, and observed consumer Closed. The child matrix
covers setup exit, transfer exit, cleanup error and failed explicit fence; it uses selected self-exec with one owned
process and synchronous wait per case. Its exact TestMain selector avoids shared NATS only for that child shape.
Source presence and witness assertions are not claims that these tests or mutations passed at this snapshot.

- `processor/graph-ingest/test_owner_test.go:24` — `func TestGraphIngestTestOwnerControlledStopPreservesAuthority(t *testing.T) {`
- `processor/graph-ingest/test_owner_test.go:57` — `func TestGraphIngestTestOwnerFailedAttemptIsNotRetried(t *testing.T) {`
- `processor/graph-ingest/test_owner_test.go:75` — `func TestGraphIngestTestOwnerCanceledOperationKeepsFreshTerminalAuthority(t *testing.T) {`
- `processor/graph-ingest/test_owner_test.go:102` — `func TestGraphIngestTestOwnerWaitsForObservedClosure(t *testing.T) {`
- `processor/graph-ingest/test_owner_child_test.go:26` — `if len(args) != 2 || args[0] != "-test.run=^TestGraphIngestOwnerChildFixture$" || args[1] != "-test.count=1" {`
- `processor/graph-ingest/test_owner_child_test.go:54` — `func TestGraphIngestOwnerAssertionExitAndCleanupError(t *testing.T) {`
- `processor/graph-ingest/test_owner_child_test.go:70` — `output, err := cmd.CombinedOutput() // One owned process and one synchronous wait.`
- `processor/graph-ingest/test_owner_child_test.go:98` — `func TestGraphIngestOwnerChildFixture(t *testing.T) {`
- `processor/graph-ingest/lifecycle_integration_test.go:26` — `child, childErr := graphIngestOwnerChildRequest(os.Getenv(graphIngestOwnerChildEnv), os.Args[1:])`

## Adjacent fences, workers and preserved probes

Seed finalization is owned before setup and checked before fresh replay. Readiness immediately checks producer
completion after successful Stop (a default assertion, with no extra wait) and checks
continued operation authority before Purge. Worker release/join in the stale-cache regression is a later defer,
therefore precedes component finish on assertion exits; timeout reports failure, not observed completion.
The unit closure proof now has once-only release and a separate fresh finite Cleanup join, reporting incomplete
cleanup if its Stop goroutine does not finish. Existing unchanged lifecycle-owner tests retain keyed/core join coverage.

- `processor/graph-ingest/readiness_integration_test.go:396` — `producerDone := comp.statusDone`
- `processor/graph-ingest/readiness_integration_test.go:400` — `case <-producerDone:`
- `processor/graph-ingest/readiness_integration_test.go:404` — `require.NoError(t, ctx.Err(), "operation authority must remain usable after the phase fence")`
- `processor/graph-ingest/cache_stale_repopulation_integration_test.go:53` — `release := func() { releaseOnce.Do(func() { close(releaseReader) }) }`
- `processor/graph-ingest/cache_stale_repopulation_integration_test.go:77` — `joinCtx, cancelJoin := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)`
- `processor/graph-ingest/cache_stale_repopulation_integration_test.go:82` — `t.Errorf("cache reader did not join before fixture Stop: %v", joinCtx.Err())`
- `processor/graph-ingest/cache_stale_repopulation_integration_test.go:90` — `t.Fatal("cache reader returned before the post-Get hook; race window was not exercised")`
- `processor/graph-ingest/test_owner_test.go:122` — `joinCtx, cancelJoin := context.WithTimeout(context.WithoutCancel(t.Context()), 5*time.Second)`
- `processor/graph-ingest/test_owner_test.go:127` — `t.Errorf("incomplete cleanup: concrete Stop did not join after native Closed release: %v", joinCtx.Err())`
- `processor/graph-ingest/component_test.go:650` — `err := comp.Stop(context.Background())`
- `processor/graph-ingest/component_test.go:659` — `err := comp.Stop(context.Background())`
- `processor/graph-ingest/component_test.go:673` — `err := comp.Stop(context.Background())`
- `processor/graph-ingest/component_test.go:922` — `err := comp.Stop(context.Background())`
- `processor/graph-ingest/hierarchy_replay_integration_test.go:138` — `require.NoError(t, seedOwner.stop(ctx))`
- `processor/graph-ingest/readiness_integration_test.go:398` — `require.NoError(t, owner.stop(ctx))`
- `processor/graph-ingest/lifecycle_integration_test.go:112` — `if err := comp.Stop(t.Context()); err != nil {`
- `processor/graph-ingest/lifecycle_integration_test.go:115` — `if err := comp.Stop(t.Context()); err != nil {`
- `processor/graph-ingest/lifecycle_integration_test.go:201` — `go func() { stopResult <- comp.Stop(stopCtx) }()`
- `processor/graph-ingest/lifecycle_owner_test.go:93` — `go func() { stopResult <- owner.Stop(t.Context()) }()`
- `processor/graph-ingest/lifecycle_owner_test.go:169` — `if err := owner.Stop(t.Context()); err != nil {`
- `processor/graph-ingest/lifecycle_owner_test.go:175` — `if err := owner.Stop(t.Context()); err != nil {`
- `processor/graph-ingest/lifecycle_owner_test.go:195` — `go func() { stopResult <- owner.Stop(stopCtx) }()`
- `processor/graph-ingest/lifecycle_owner_test.go:204` — `if err := owner.Stop(t.Context()); err != nil {`
- `processor/graph-ingest/lifecycle_owner_test.go:226` — `if err := owner.Stop(nil); err == nil {`
- `processor/graph-ingest/lifecycle_owner_test.go:232` — `if err := owner.Stop(t.Context()); err != nil {`
- `processor/graph-ingest/lifecycle_owner_test.go:238` — `if err := owner.Stop(t.Context()); err != nil {`

The JSON maps all 17 original adjacent calls. Fifteen deliberate/legacy API-probe spellings remain; the two phase
fences now use checked owner.stop. The lifecycle-owner file and runtime implementation are byte-identical to the
accepted checkpoint. Nil, expired, repeated Stop and failed-Start overlap contracts are not blanket-rewritten.

## Skips, substrate and evidence limits

All six prior skips remain. Three have repaired cleanup source and remain unexecuted; the one-shot AlreadyStarted
assertion now checks ErrAlreadyStarted. No runtime proof is claimed for removing their original source debt.

- `processor/graph-ingest/component_test.go:542` — `t.Skip("requires real NATS connection - move to integration tests")`
- `processor/graph-ingest/component_test.go:602` — `t.Skip("requires real NATS connection - move to integration tests")`
- `processor/graph-ingest/component_test.go:626` — `t.Skip("requires real NATS connection - move to integration tests")`
- `processor/graph-ingest/component_test.go:643` — `t.Skip("requires real NATS connection - move to integration tests")`
- `processor/graph-ingest/component_test.go:665` — `t.Skip("requires real NATS connection - move to integration tests")`
- `processor/graph-ingest/component_test.go:908` — `t.Skip("requires real NATS connection - move to integration tests")`
- `processor/graph-ingest/component_test.go:639` — `assert.ErrorIs(t, err, errs.ErrAlreadyStarted)`

The two inventoried direct TestClient.Terminate defers are absent in their exact test declarations. TestClient
remains the registered, checked substrate owner; normal lexical component defers run earlier. Its implementation,
the hierarchy construction helper, cache implementations and keyed pool match the accepted checkpoint byte for byte.
Native Drain and cache Close remain contextless; two independent five-second cache waits can outlive the supplied
Stop deadline. A pool bound can win before lane completion. No generic interruption, leak freedom or failed-Stop
join is inferred. This refresh runs no tests, Docker operations, guard or mutations.

Installed baseline `test/testinfra/cleanup_baseline.json`, SHA256
`909d254426e4b190d3fe10c86f936dcea742d8a6998b7b90a93bbe02ccab2615`, contains 297 debt
entries / 90 resolutions. Exact JSON comparison removes precisely the 32 original identities, preserves all other
entries and 89 original resolutions, and adds one CancelFunc resolution. `review/manifest-review.md` records independent
approval of the exact installed record, including its context-first finish dependency. This appendix does not itself approve it
or substitute for final guard/proof review. The earlier retained guard log reports test/testinfra ok (8.508s) at its
own pre-reordering snapshot, not this freeze. Final invocation/outcome evidence belongs to the coordinator.
Accepted 329/89 and original historical 334 remain distinct.

## Searches and mechanical refresh log

- Read `/private/tmp/gh1423-final-source-freeze-after-inline.json`; the first 25-file hash check passed. A later build
  stopped when the readiness hash changed. After coordinator freeze confirmation, refreshed only readiness and owner
  unit proof hashes into `gh1423-final-source-freeze-for-inventory.json`; all other 23 are unchanged. Final checks run twice.
- Read the first 100 lines of the accepted scratch baseline/reference JSON to confirm schema; the builder then
  reads both complete files and relocates only their explicit entries/references.
- Ran `git status --short` and `git rev-parse HEAD` to identify uncommitted source and the recorded base.
- Read owner support lines 1–240, owner tests 1–280 and child proof 1–300.
- Read authority helper 130–205, batch helper/caller 15–100, readiness helper 55–105 and readiness fence 370–440.
- Read accepted inventory 155–260, lifecycle integration 1–95, stale-cache worker 25–170 and replay 120–180.
- Read existing bounded caller/core review records solely for their stated checkpoint limits; do not inherit PASS
  across differing hashes. Current full implementation/proof review remains independent.
- Ran `git diff --` for lifecycle_integration_test.go, component_test.go and readiness_gauges_integration_test.go.
- A bounded Python extraction selected only the original baseline functions and printed owner, context, Start,
  Initialize and skip lines; no repo-wide structural discovery was made.
- Reproducible refresh script: `/private/tmp/gh1423-build-current-inventory-v3.py`. It uses `git show 0085323e:<path>`
  for the original helper-reference declarations and seven named unchanged dependencies, exact-name line relocation
  within those declarations, and exact `.Terminate(` zero checks in the two known duplicate-owner functions.
- Read final owner proof 95–190 and readiness fence 393–423, the last 25 guard-log lines, and current baseline JSON.
  Compared the exact baseline against `git show 0085323e:test/testinfra/cleanup_baseline.json` without editing it.
- V2 reads the coordinator-provided context-first final manifest and proposed baseline; verifies their supplied
  SHA256 values and all 25 source hashes; reruns only the same fixed relocation and exact baseline comparison.
  The original companion and builder remain unchanged. No live baseline was edited or admission rerun.
- V3 reads `gh1423-final-source.json`, verifies its supplied SHA256 and all 25 source hashes, and confirms the only
  changed hashes are component_test.go and hierarchy_sync_integration_test.go. Reads the exact four-replacement
  `gh1423-audit-annotations-correction.json` packet and `review/manifest-review.md`; verifies installed baseline909d.
  Reuses fixed mappings; original/v2 artifacts remain unchanged. Compares every v3 canonical pin with v2.
- No gopls re-sweep was needed: the accepted typed reference population is the fixed input, not a new caller census.
