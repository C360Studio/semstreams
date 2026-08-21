# Lifecycle simplification recovery ledger

This is the durable execution authority for `simplify-one-shot-lifecycle-ownership`. It exists because design approval,
a merged contract PR, strict OpenSpec validation, green tests for the old machinery, and runtime migration were
previously conflated. Compaction, a new agent session, or a later PR summary must start here rather than infer state
from proposal language.

## Binding truth

- The owner-approved simpler design was merged as documentation; the repository-wide runtime migration was not.
- No accepted implementation was lost in a merge. At recovery baseline `main`
  `9fcc841ee792a080a7b9998bfb51400cd81b24fe`, only one of the historical 42 production owners had been migrated away
  from the lifecycle framework.
- PR #987 removed zero owners. PR #988 removed one owner. Draft PR #990 is boot-only composition work and receives
  zero lifecycle-migration credit.
- Green tests currently demonstrate consistency of machinery that must be removed. They do not demonstrate that the
  target design has landed.
- The runtime migration is incomplete until every mechanical zero gate and every positive proof in this ledger passes.

## Freeze and authority

Until this ledger and its OpenSpec reconciliation are durable, no lifecycle implementation branch or PR #990 may
merge. The freeze lifts only in the ordered sequence below; it does not authorize parallel speculative lifecycle work.

This change is the sole tracker for:

- migration from `Generation`, `Operation`, `StopWithQuiesce`, and their manufactured lifecycle semantics;
- owner-local cancel, done/WaitGroup, native handle, failed-Start, and `startDone` ownership;
- removal of the old lifecycle helper surface and its behavior-preserving tests;
- controlled restart, dirty recovery, settlement ordering, and final deletion proof for that migration.

Other active changes have narrow, non-overlapping authority:

- `restore-go-lifecycle-ownership` owns stored-context and invented-root removal only;
- `require-restart-for-config-activation` owns boot-only component composition and rules-only hot reload only;
- suspended or future changes cannot claim lifecycle migration credit by cross-reference.

Do not create another lifecycle simplification spec or duplicate these tasks. A newly discovered defect may have an
issue or evidence record, but completion is recorded here. Changing this boundary requires explicit owner approval.

## Archetype-family execution authority — 2026-08-19

On 2026-08-19 the owner approved replacing steady-state one-owner-at-a-time execution with bounded archetype-family
waves. This changes execution granularity only; it does not change the approved lifecycle target, owner-migrated
definition, completion vocabulary, or any gate.

A family wave is authorized only when its reviewed inventory and design:

- freeze the exact owner membership, baseline commit, shared lifecycle contract, and member-specific exceptions before
  implementation;
- group only genuinely equivalent ownership shapes and make no opportunistic additions after review;
- isolate owners with distinct native protocols, context/root debt, manager admission, provider boundaries,
  observation collisions, or unresolved prerequisites into separately reviewed exceptions;
- preserve per-owner behavior proof, focused race evidence, source identities, and exact per-owner plus wave census
  movement;
- inherit the one independently passed global inventory and target-wave design while exact membership, contract,
  exceptions, API rulings, and measured premises remain unchanged; each wave still requires TDD evidence and
  independent implementation review before owner-migrated credit;
- trigger renewed inventory/design review only for a membership split, source/census drift that changes a premise, a
  new or changed exported surface, a new native/context/observation exception, or a prerequisite API-shape change;
  ordinary reviewed-ancestor commits are not drift; and
- withhold completion credit for a failed wave and block only its declared dependents. Any independent reviewed wave
  whose prerequisites are complete may proceed concurrently in an isolated worktree; there is no single global
  next-wave lock.

This historical process approval did not check the then-numbered tasks 2.1, 2.2, or 2.3; it did not grant owner,
Gate A, Gate B, Gate C,
runtime-migration, proof, release, archive, or tag credit; and does not weaken any Gate A/B/C, test-surface,
positive-proof, archive, or tag requirement. Unique and protocol-specific exceptions remain single coherent owner
slices when the reviewed inventory cannot establish a genuine family.

### Reviewed dependency authority

The reviewed global wave artifact records the full DAG. Execution status is dependency-based, not ordinal. R1 and ML1
have no implementation dependency. R1 births final
`internal/lifecyclecleanup.RollbackFailedStart(parent, rollback)`. I1/G1/M1/SM1/CM1 depend on R1; S1 depends on I1;
OT1 depends on I1; A1/O1/H1/OS1/RU1/GI1 depend on S1; N1 depends on every owner wave and all exported/adopter proof.
R1 is the selected first helper-birth family, but selection is not an exclusive repository-wide lock: ML1 may proceed
independently once the global design is finally accepted. A wave may be `blocked`, `ready`, `in progress`,
`implementation review`, or `complete`.

### Rejected standalone F0 worktree state — 2026-08-19

Owner ruling rejected the zero-owner F0, `lifecyclecleanup.Wait`, Background-root RollbackFailedStart signature, and
lifecyclejoin forwarding. Current uncommitted evidence is rejected and receives zero task/owner/gate/proof credit:

- untracked `internal/lifecyclecleanup/lifecyclecleanup.go`, SHA-256
  `fee8993472188a989f9444f5a325cd12366446e3ba73edbe4d760ab8481aac9e`;
- untracked `internal/lifecyclecleanup/lifecyclecleanup_test.go`, SHA-256
  `4822f3c209851babd446b03da5548ef8b26ea15c449b9cdd9a6092fb837eff82`;
- tracked diffs in `internal/lifecyclejoin/rollback.go` and `internal/lifecyclejoin/generation_test.go`, combined
  binary-diff SHA-256 `ea02c32427ff2ab79a20817e001010854081f106948c4d7ff845ef4a2cd02514`, 16 insertions/22
  deletions.

After this evidence is durable, cleanup restores only the two tracked lifecyclejoin files to baseline and removes only
the two untracked lifecyclecleanup files. It must not touch untracked `metrics-http-owner-inventory.md` or any other
change. No fragment is copied; R1 writes accepted parent-aware helper/tests from contract.

The hashes provide durable provenance only. Because the owner explicitly rejected this experiment, no recoverable
patch is required; after target-exact cleanup the bytes are intentionally discarded. Do not commit, stash, or copy
them into R1. If the owner wants recoverability despite rejection, the writer first materializes the actual patch—not
merely a hash.

### Corrected family-wave design authority checkpoint — 2026-08-19

Independent corrected-design review returned `DESIGN APPROVE` for
`remaining-owner-family-wave-design.md` at reviewed SHA-256
`4ceb09c9b98c7d1f5a250d95533814951d367538d9ef863c3116b7e6a97afadf`. The owner then stated
“agree - continue with recommendation.” That acceptance is limited to rejecting standalone F0 and a shared Wait
helper, accepting parent-aware `internal/lifecyclecleanup.RollbackFailedStart(parent, rollback)` born with R1, and
selecting R1 as the first wave.

The unrelated exported API rulings in the reviewed design remain unapproved. At this design-authority checkpoint, R1
implementation was still under review and no implementation verdict had yet been recorded. This checkpoint grants no
implementation, owner-migrated, Gate A/B/C, runtime-migration, proof, release, archive, or tag credit. Tasks and gates
remain unchecked.

### R1 research-five owner-family implementation checkpoint — 2026-08-19

Independent `semstreams-reviewer` verdict `APPROVE` applies to the R1 dirty worktree based on full commit
`7a14e4ab2c1ce7b9815555d1bd40eb79776a2a09`. Owner-migrated credit is granted only to these five frozen production
owner files:

- `processor/research-graph-assess/component.go`;
- `processor/research-graph-classify/component.go`;
- `processor/research-graph-execute/component.go`;
- `processor/research-graph-route/component.go`;
- `processor/research-graph-synthesize/component.go`.

R1 also births final parent-aware `internal/lifecyclecleanup.RollbackFailedStart(parent, rollback)` with five real
owner consumers. The helper receives no owner credit. The legacy lifecyclejoin implementation is unchanged, and no
unrelated exported API ruling is approved by this checkpoint.

The developer RED transcript is recorded conservatively as causal TDD evidence, not as a later green result:

1. The first R1 test run failed to build because the accepted parent-aware helper did not yet exist.
2. After helper compilation, the research packages exposed same-instance restart expectations that fail under the
   accepted one-shot contract.
3. The classify fixture required correction to remove unrelated invalid setup before it could test lifecycle behavior.
4. Each new assess/classify/route expiry test was later mutation-proved RED before the accepted implementation was
   restored.

Independent all-six race evidence:

| Package | Result | Elapsed |
|---|---|---:|
| `./internal/lifecyclecleanup` | PASS | 1.209s |
| `./processor/research-graph-assess` | PASS | 6.488s |
| `./processor/research-graph-classify` | PASS | 6.783s |
| `./processor/research-graph-execute` | PASS | 6.871s |
| `./processor/research-graph-route` | PASS | 7.424s |
| `./processor/research-graph-synthesize` | PASS | 7.114s |

The new `TestFailedStartRollbackExpiryRetainsPartialSubscription` case passed three times under the race detector in
assess, classify, and route, at approximately 17 seconds per package. `task lint`, `git diff --check`, and strict
OpenSpec validation also passed.

The production-only recovery census moved exactly as reviewed:

| Measurement | HEAD `7a14e4ab` | R1 worktree | Delta |
|---|---:|---:|---:|
| Production owner files importing `internal/lifecyclejoin` | 36 | 31 | -5 |
| `lifecyclejoin.NewGeneration` | 38 | 33 | -5 |
| `Generation.Stop` | 43 | 38 | -5 |
| External `RunPartialStartRollback` calls | 20 | 15 | -5 |
| Final parent-aware `RollbackFailedStart` calls | 0 | 5 | +5 |
| External `Generation.Cancel` | 4 | 4 | 0 |
| `Generation.StopWithQuiesce` | 8 | 8 | 0 |
| `lifecyclejoin.NewOperation` | 3 | 3 | 0 |

`git diff --quiet -- internal/lifecyclejoin` returned success. The unrelated Metrics inventory remained byte-identical
at SHA-256 `8a3b74786df6098aa053edd5c5c5e68f42f817ebd44008cdb75b8dece9eb2fc5`.

Stable R1 source identities for this checkpoint are:

- `internal/lifecyclecleanup/lifecyclecleanup.go`:
  `c0e93a428a0e1fcf4de670be87aaab08d3b56c5d5b40f23d3e10a9d2b0035001`;
- `internal/lifecyclecleanup/lifecyclecleanup_test.go`:
  `ebca025fc6aeb9fe856620c4f4f97e0e5a91f664886a97f0738c9194c51db7b8`;
- `processor/research-graph-assess/component.go`:
  `d41c24a88838f7d6bac0517b90e297617d45eba0a19635bac931982ddfeded3e`;
- `processor/research-graph-assess/component_test.go`:
  `6ad899332b5016510b6a93af33650dbddbdae8026fbb7c57b7a60a7c821df817`;
- `processor/research-graph-assess/lifecycle_test.go`:
  `6c7e3ddd98c351acd0a5f8dd72b7bb48216a8648edcbd688f1c8be63a938975d`;
- `processor/research-graph-classify/component.go`:
  `8b2c139b7cb54bf24530b4f7f5a640148f01d00795f3526cd835ccb148899e5a`;
- `processor/research-graph-classify/component_test.go`:
  `4e50b7d7ffc13c5464e173eee1717224c4b6f6109c84e27b762679a8a2866958`;
- `processor/research-graph-classify/lifecycle_test.go`:
  `7dd9809706ed00ab5d2c7d40a6568d782045ba9a9a7f27e4eb2f5bae642f1c71`;
- `processor/research-graph-execute/component.go`:
  `49bb6cc8045847bba95df37937423058e7d9ee8e00fd0245e3e387c2aae85c33`;
- `processor/research-graph-execute/component_test.go`:
  `817b88f3234eab260eb3508a62f2a22e2326e9e2bbd33d7fc960e12e1591aceb`;
- `processor/research-graph-execute/lifecycle_test.go`:
  `358ecb2b4ad1e24a8f77d35b99e62292156399201b264a53ed0df98f86ecc85d`;
- `processor/research-graph-route/component.go`:
  `f1f85d18a4ff662ce6e1a714f6d1869de506f17069e2a8c570d76ec21e1ab550`;
- `processor/research-graph-route/component_test.go`:
  `bf853432b79968938ec996f35b6d7842ae38b2853caaee88f3343d57223991d6`;
- `processor/research-graph-route/lifecycle_test.go`:
  `49320f99bdd969097aeb9ba345420a84a2e02c76738a43de343c9ed3bf5f4bca`;
- `processor/research-graph-synthesize/component.go`:
  `94f838e1680f6de6689df14b98b0f265b749e863d576c0e780f29aff794fe852`;
- `processor/research-graph-synthesize/component_test.go`:
  `e9bb57caabc4bc5b16996229e5b23ac4cf311688d2a8f9d7cffb0f02bcbacc7b`;
- `processor/research-graph-synthesize/lifecycle_test.go`:
  `3ae02a9ed63fddf5ee194b5194bcf58b4de3abcd0eaa9d4530e9f4e9f41ddb0b`.

Task 2.3 and Gate A/B/C remain unchecked and incomplete. This checkpoint grants no runtime-migration, proof, release,
archive, or tag credit.

### SM1 dependency and failed-Start authority correction — 2026-08-19

Architect binding interpretation corrects SM1 from an independent root to a child of R1. Every migrated owner with a
post-acquisition Start failure uses final parent-aware
`internal/lifecyclecleanup.RollbackFailedStart(parent, rollback)`. `Manager.StartAll` must locally attempt bounded
synchronous rollback before returning a child Start, main listener bind, or publisher failure. Process-root `StopAll`
is defense-in-depth, not a substitute for Manager-owned failed-Start cleanup.

This correction changes no SM1 membership or exported surface and leaves its planned owner -1, NewGeneration -3, and
StopWithQuiesce -3 deltas unchanged. After R1, SM1 is expected to move final helper calls 5→6 while removing no old
rollback call. At this correction checkpoint, independent design re-review and later SM1 implementation review were
still pending. It grants no SM1 owner, task, Gate A/B/C, runtime-migration, proof, release, archive, or tag credit; all
tasks and gates remain unchecked and incomplete.

### SM1 implementation checkpoint — 2026-08-19

Independent review of the narrow corrected SM1 design returned `DESIGN APPROVE`. Independent
`semstreams-reviewer` implementation verdict then returned `APPROVE` after all corrections for the dirty worktree
based on full commit `509cf8b21bfb76a9bfc3196baaef14836a8dd934`. Owner-migrated credit is granted only to
`service/service_manager.go`. Adjacent service tests and the process-root comment receive no owner credit.

The developer RED sequence is recorded conservatively:

1. Initial lifecycle tests exposed asynchronous bind and same-instance rebind behavior.
2. Canceled-Stop behavior was proved RED before the one-shot terminal correction.
3. Manager-local failed-Start rollback cases were proved RED before `StartAll` owned that cleanup.
4. A `cleanupFailedStart` compile RED preceded the final helper integration.

Final focused evidence:

| Evidence | Result | Elapsed |
|---|---|---:|
| Independent focused service race | PASS | 6.772s |
| Focused lifecycle matrix, 20 repetitions | PASS | 1.845s |
| `task lint` | PASS | — |
| `git diff --check` | PASS | — |
| Strict OpenSpec validation | PASS | — |

Full repository race is not claimed green for two distinct reasons:

1. Two user-owned `.claude/worktrees` entered repository-wide scanners, causing duplicate-census failures and failures
   against an old graph-ingest target.
2. Separate stale policy-baseline entries still expect two removed sleeps in root `service/base_test.go`.

The focused service surface passed, but the repository-wide race gate remains outstanding and receives no waiver.

The production-only recovery census moved exactly as reviewed:

| Measurement | HEAD `509cf8b2` | SM1 worktree | Delta |
|---|---:|---:|---:|
| Production owner files importing `internal/lifecyclejoin` | 31 | 30 | -1 |
| `lifecyclejoin.NewGeneration` | 33 | 30 | -3 |
| `Generation.Stop` | 38 | 38 | 0 |
| External `Generation.Cancel` | 4 | 4 | 0 |
| `Generation.StopWithQuiesce` | 8 | 5 | -3 |
| `lifecyclejoin.NewOperation` | 3 | 3 | 0 |
| External `RunPartialStartRollback` calls | 15 | 15 | 0 |
| Final parent-aware `RollbackFailedStart` production owner calls | 5 | 6 | +1 |

`git diff --quiet -- internal/lifecyclejoin` returned success. The unrelated Metrics inventory remained byte-identical
at SHA-256 `8a3b74786df6098aa053edd5c5c5e68f42f817ebd44008cdb75b8dece9eb2fc5`.

Stable SM1 source identities for this checkpoint are:

- `service/service_manager.go`:
  `4813e385eb311a128a437280e9c375611dd9388b3c52ba5ebd531737405f60c7`;
- `service/service_manager_test.go`:
  `4d6c081210982d857cd011ba77fab16ba685a1953a9f03e19adbb143081e5d6f`;
- `service/service_manager_health_listener_test.go`:
  `232713a773f9b04dd7e25ff53a339917b22f722f51256d195ec2ccc503b4b410`;
- `service/middleware_test.go`:
  `dff52587818a47b6450757b349c8f589ad90744ec22f16f15b2c40c712b60694`;
- `cmd/semstreams/main.go`:
  `dd5a20c6c306267959841f63daf0b21141aaa1cf042b8e0f6226a261c70823b1`.

The status-document hashes reported with the handoff are provenance, not additional implementation evidence. This
checkpoint grants no approval to unrelated exported API rulings. Task 2.3 and Gate A/B/C remain unchecked and
incomplete, and it grants no runtime-migration, proof, release, archive, or tag credit.

### G1 graph-read five-owner implementation checkpoint — 2026-08-19

Independent `semstreams-reviewer` verdict `APPROVE` applies to the G1 dirty worktree based on full commit
`0d22802c363d0c283219f895c74267ec2913f16a`. Owner-migrated credit is granted only to these five frozen production
owner files:

- `processor/graph-query/component.go`;
- `processor/graph-clustering/component.go`;
- `processor/graph-embedding/component.go`;
- `processor/graph-index-spatial/component.go`;
- `processor/graph-index-temporal/component.go`.

Adjacent query files and test files are supporting implementation/evidence surfaces and receive no separate owner
credit.

The TDD transcript is recorded conservatively:

1. The original no-action Stop contract was RED in all five owners.
2. The graph-query Start-path callback lock RED timed out in `Health`, proving the lock-order failure.
3. The accepted corrections restored the focused lifecycle surface to green.

Final evidence:

| Evidence | Result |
|---|---|
| Full five-package race run | PASS |
| Integration-tag lifecycle run | PASS |
| `TestLifecycleOwner` under race, five repetitions | PASS |
| `task lint` | PASS |
| `git diff --check` | PASS |

The production-only recovery census moved exactly as reviewed:

| Measurement | HEAD `0d22802c` | G1 worktree | Delta |
|---|---:|---:|---:|
| Production owner files importing `internal/lifecyclejoin` | 30 | 25 | -5 |
| `lifecyclejoin.NewGeneration` | 30 | 25 | -5 |
| `Generation.Stop` | 38 | 33 | -5 |
| Final parent-aware `RollbackFailedStart` production owner calls | 6 | 11 | +5 |
| External `Generation.Cancel` | 4 | 4 | 0 |
| `Generation.StopWithQuiesce` | 5 | 5 | 0 |
| `lifecyclejoin.NewOperation` | 3 | 3 | 0 |
| External `RunPartialStartRollback` calls | 15 | 15 | 0 |

`git diff --quiet -- internal/lifecyclejoin` and `git diff --quiet -- natsclient` both returned success. The unrelated
Metrics inventory remained byte-identical at SHA-256
`8a3b74786df6098aa053edd5c5c5e68f42f817ebd44008cdb75b8dece9eb2fc5`.

Stable G1 source identities for this checkpoint are:

- graph-query:
  - `component.go`: `e8994dd1254d66da97bb93323b3395b0e1c6a938cef38014aa2cb835c7d601c4`;
  - `component_test.go`: `9631e1832eb9384f510a7eb7f92487d0e8372763f21d26f1f1539f718ad83e8c`;
  - `graph_query_lifecycle_test.go`:
    `c62a710777d98725bb290726d5bfc5aeb6c946368a2ec72d91b65ad548efb464`;
  - `query.go`: `3c2f11e1c81246e32888fd9cdfddf685890fa0eddfc8a152e2e802b1c1bc8c40`;
  - `lifecycle_owner_test.go`: `14befd76b13c6333bf92f96315e6ae46e9874e8ba5e6953e3c15164ba3dc672a`.
- graph-clustering:
  - `component.go`: `778b1a8428ab7ba1ea22eb39ac007250f2410f3e0d5a342f5dc9e2afa430ba0d`;
  - `component_test.go`: `114e7c875507b75344ab2e5fba486fe3ae33bf96093fb95892067656c74eaaa4`;
  - `lifecycle_integration_test.go`:
    `e2256f1a1812652dda9f8274e3b896adbe51d32ccdc7217610b6ae2cf4a0fcdc`;
  - `query.go`: `02e5296cff7f4f7dd39719a1bb1065f3a189a645f46d477b0203a1ac76be75b8`;
  - `lifecycle_owner_test.go`: `83fb0055e5ff9219e2005bdb5c68aefc91e4469246a818d7121e826645a7bd51`.
- graph-embedding:
  - `component.go`: `3a22b15ec8370bc35269b85693d49a6e909668290ed0467440549643cbc235cb`;
  - `component_test.go`: `4286f69268070f7ca4711ae71146a1b39c759ef685a6c87ae6dbd16ec4884a06`;
  - `lifecycle_integration_test.go`:
    `c197723e953c82131662601a4f7e0dc63be0cc10c4f72ba20a8927fef8e92f16`;
  - `query.go`: `2c19e340306874db92923763c6bca16ed77c3313ef31b8d9aeb73af99560efe5`;
  - `lifecycle_owner_test.go`: `fd94be423bad50a82c02072b0be8732659026f656e3ce81de3b565454ac7441f`.
- graph-index-spatial:
  - `component.go`: `b5314085265e0b62f16dee4458aa9dc25211ab0359d23816497a1b3a5825ca5f`;
  - `component_test.go`: `a0028ccc161aca30a8c86d9fd4303a2b8bcf4c657070611b21ca409905972831`;
  - `lifecycle_integration_test.go`:
    `7ec5d860edfc207eff4568701a54f660979192c0dfdb6aac56162b771395ca97`;
  - `query.go`: `07e2d25c93e1e28856ef793827e1744b253bc8aeb4c311ee51883c6ad67b8410`;
  - `lifecycle_owner_test.go`: `ddc5be339e8a5124e6e66b41a65ebe962d77c656158107f26ef569f4d4f6a2a6`.
- graph-index-temporal:
  - `component.go`: `2e74f93695372237c29206ae2348523cb5e1593bf32b6faa322f7573d41ac816`;
  - `component_test.go`: `aa4fbfd0a0c3549c67c33de1fce163be019e842a7645829628d6055154bfc9f8`;
  - `lifecycle_integration_test.go`:
    `b5825c6b87b552ad86fc247a90bb5467003c687f9bdea08f0d0f459698fa7a92`;
  - `query.go`: `4d6cda7198fefa8704ccbce7dc4742101795868ec85dbfd0b840d02de440e0d9`;
  - `lifecycle_owner_test.go`: `c94a7c62f68187da90cf6c7992ded4394a4be0dec2b26665757344e01319c5aa`.

This checkpoint grants no approval to unrelated exported API rulings. Task 2.3 and Gate A/B/C remain unchecked and
incomplete, and it grants no runtime-migration, proof, release, archive, or tag credit.

### I1 native non-port reviewed owner-wave checkpoint — 2026-08-20

The owner explicitly approved only this breaking I1 surface: change canonical
`ConsumeInternalStreamWithConfig` to return its exact `jetstream.ConsumeContext`, reject duplicate live ownership of a
fixed internal durable, and remove zero-present-consumer `Registry.SubscribeCapabilities`. The approval does not
include `ConsumeDurable`, either port consumption method, `natsclient.Subscription`, Metrics APIs, or any later N1
retirement. Known sister repositories have no current caller of the changed internal method or removed Registry
method; unknown adopters receive a compile error instead of silently retaining hidden lifecycle ownership.

Independent `semstreams-reviewer` verdict `APPROVE I1 IMPLEMENTATION` applies to the dirty worktree based on full
commit `4b01d09e`. It confirms exact native ownership for `service/milestone_service.go` and
`agentic/agentrun/agentrun.go`, the supporting MaxDeliver observer, and atomic natsclient/Registry changes. With both
required breaking-change E2E tiers green, owner-migrated credit is granted exactly to
`agentic/agentrun/agentrun.go` and `service/milestone_service.go`. Supporting natsclient, component, and MaxDeliver
files receive no owner credit.

The TDD RED sequence is exact:

1. `go test ./natsclient -run '^TestConsumerPolicyExportedClientAPICensus$' -count=1` failed because the test expected
   the approved `(jetstream.ConsumeContext, error)` return while production still exposed the legacy error-only
   signature. The corrected test passed in 1.367s.
2. `go test ./service -run '^TestMilestoneServiceStartRejectsInvalidContextWithoutConsumingAuthority$' -count=1
   -timeout=20s -v` passed the nil subcase but failed the pre-canceled subcase at
   `service/milestone_service_test.go:83`: `svc.used` became true even though the subscriber was not called. The
   corrected implementation rejects nil and pre-canceled Start before consuming one-shot authority.

The corrected MaxDeliver contract is explicit: only agentrun may treat its missing optional `AGENT` stream as a
no-op. MaxDeliver requires the provisioned capture stream and fails loud when it is absent, returning a nil stop
closure and an error satisfying `errors.Is(err, jetstream.ErrStreamNotFound)`. The new real-NATS
`TestStartFailsLoudlyWhenCaptureStreamIsMissing` passed with package elapsed 2.027s and test elapsed 0.68s.

Final developer and independent-review evidence:

| Evidence | Result | Elapsed |
|---|---|---:|
| Focused I1 lifecycle race | PASS | 1.497s |
| Full service race | PASS | 6.766s |
| Real-NATS service integration | PASS | 2.228s |
| Real-NATS agentrun integration | PASS | 3.690s |
| Behavioral natsclient race excluding repository scanners | PASS | 3.217s |
| Focused lifecycle race, 20 repetitions | PASS | reviewer-confirmed |
| Real-NATS I1 matrix | PASS | reviewer-confirmed |
| Causal stress, 5 repetitions | PASS | reviewer-confirmed |
| `task lint` | PASS | — |
| `git diff --check` | PASS | — |
| Strict change validation and all 48 strict spec validations | PASS | — |

The full natsclient race is not claimed green. Its only failures were three repository-scanner tests that traversed
two user-owned `.claude/worktrees`; the behavioral natsclient race excluding those scanners passed. The reviewer also
confirmed that `ConsumeDurable`, `internal/lifecyclejoin`, and `metric` were unchanged.

The production-only recovery census moved exactly as reviewed in the dirty worktree. The two-owner delta is credited
only to the two frozen I1 owner files:

| Measurement | HEAD `4b01d09e` | I1 worktree | Delta |
|---|---:|---:|---:|
| Production owner files importing `internal/lifecyclejoin` | 23 | 21 | -2 |
| `lifecyclejoin.NewGeneration` | 22 | 21 | -1 |
| `Generation.Stop` | 31 | 30 | -1 |
| `lifecyclejoin.NewOperation` / lifecycle `Operation.Run` | 3 | 2 | -1 |
| External `RunPartialStartRollback` calls | 15 | 14 | -1 |
| Final parent-aware `RollbackFailedStart` production owner calls | 12 | 14 | +2 |
| `Generation.StopWithQuiesce` | 3 | 3 | 0 |
| External `Generation.Cancel` | 4 | 4 | 0 |

Stable I1 source identities for this reviewed checkpoint are:

- `agentic/agentrun/agentrun.go`:
  `e8d26f3b43843897c889c90d56966ed921928c7caed0fe8d7d83acb1765ab864`;
- `agentic/agentrun/agentrun_integration_test.go`:
  `bdff5bb78e1e5125c70f620d1fd8901e9ffa7ad2e352125b872f125e8cbea1c7`;
- `component/registry.go`: `ddb96938afb194aa185647e04e07728deb69e126a662656ce0947b6196e45a08`;
- `component/registry_integration_test.go`:
  `3052762be61eba32b202bc7e0b122cac8ffb2455da460a4842dc4334ff773bab`;
- `internal/maxdelivery/observer.go`:
  `319f55b0a8d64d60cfc5bb051b953aca801ee6b1e2b6863f7d7bbf3a8c401669`;
- `internal/maxdelivery/observer_integration_test.go`:
  `8dcd94cb33d7c80fbdd5701f5ec46ed8e1d4363dcb6ea5c848f9360ab6153b6f`;
- `internal/maxdelivery/observer_test.go`:
  `4ee284b93a5365ced973f7a79414819b2e1441bc5b7d2374933dafc4260e057c`;
- `natsclient/client.go`: `e46585cf32227b3d8eddd23e63ae5f6108037951f198d88f49c39707351be0d1`;
- `natsclient/client_integration_test.go`:
  `090bc022d15f6c056279aad9d8b977e0699f7c243f00f9f5d17cd8e8e4fe474f`;
- `natsclient/client_test.go`:
  `cecd3a19133b905e85a26509746593c0b7f294a36f9b006d60a73d4c7eef9f93`;
- `natsclient/consumer_policy.go`:
  `3c2732be1ef02ccad882a45608800983246cdab418822f262f4506043f52cd1a`;
- `natsclient/consumer_policy_callsite_test.go`:
  `73a0fe33d501e0f8b29066ef3dcec781c992c3f9c25d33f31e3e23b7c5af510a`;
- `natsclient/doc.go`: `843d7fcfcbdfc49b714fc48376b62cc733af2088ef8a2d7b24a0ec886230f9d6`;
- `natsclient/integration_test.go`:
  `dba5b71b492cf16983bfc6e4ea537e518da20214dd489acd82a9d4444504ec38`;
- `natsclient/jetstream_metrics.go`:
  `a48a40f6e0e594c472b045cf1d3e6417b071224f31e75fb75ea85a89d87a31b8`;
- `natsclient/jetstream_metrics_test.go`:
  `a91948d51e7c02acbb0d3cc41f8b37aa2fa63dc4dc0e00c1497d40fe7299a6e0`;
- `natsclient/stream.go`: `10bf4a494daf7c65f966e9978f333e9a7ee8dd258a84a246067a8b596ac0b431`;
- `natsclient/internal_consumer_lifecycle_integration_test.go`:
  `8c0d9776178ade6c09d3879ebd26bc060e71b48a20350504913ec40b7720cb7c`;
- `service/milestone_service.go`:
  `6dd3bf7e47e75503714ab0803c900598d14f67db75bccffa9b2ac4ab6ad851fd`;
- `service/milestone_service_test.go`:
  `2418da58894ae9e4df238e75f203b2f4b3615733016ee003f52ed895d3f298f3`.

After explicit elevation approval, both required breaking-change E2E tiers passed:

| Evidence | Result | Exact outcome |
|---|---|---|
| `task e2e:agentic` | PASS, exit 0 | Scenario success in 45.149536625s |
| Durable tool replay | PASS | executor invocations 1; tool executions 1; trajectory facts 10 |
| `task e2e:core` | PASS, exit 0 | 3/3 scenarios: health, dataflow, graph roundtrip |
| Core dataflow settlement | PASS | `max_delivery_deliveries=1` |
| Core storage path | PASS | ObjectStore/raw path evidence present |

The disposable review copy `/private/tmp/semstreams-i1-review.2COr7F` was removed and independently verified absent.
I1 landed as commit `07c37f7319a65c5109fe31bc36136661bc6e9243`. Task 2.3, Gate A/B/C, runtime migration,
proof, release, archive, and tag readiness remain unchecked and incomplete.

### OT1 OTEL pull-loop implementation checkpoint — 2026-08-20

Independent `semstreams-reviewer` verdict `APPROVE` applies to the OT1 dirty worktree based on full commit
`07c37f7319a65c5109fe31bc36136661bc6e9243`. Owner-migrated credit is granted only to
`output/otel/component.go`. `output/otel/component_test.go` and the new
`output/otel/component_lifecycle_integration_test.go` are supporting evidence surfaces and receive no owner credit.

OT1 binds process-global lifecycle identity with an opaque `(stream, durable)` claim. A duplicate local owner is
rejected without replacement or disturbance of the incumbent. The owner fences new fetches, cancels and joins its
pull loops, flushes the exporter, removes policy observers, performs context-bound exporter Shutdown, and only then
releases its exact claims. Completed repeated Stop returns nil without replay; no native Consumer deletion is part of
the contract.

The causal TDD RED replaced the legacy `lifecyclejoin.Operation` replay expectation: after exporter Shutdown returned
an injected error, the new one-shot test expected a completed second Stop to return nil, but the old Operation replayed
the prior error. The accepted implementation removes that retained-result behavior.

Final independent evidence:

| Evidence | Result | Elapsed |
|---|---|---:|
| Full OTEL package race | PASS | 1.378s |
| Real-NATS lifecycle integration | PASS | 3.948s |
| Blocked Start/Stop overlap, 5 repetitions | PASS | 3.264s |
| Focused lifecycle matrix, 10 repetitions | PASS | 1.306s |
| `task lint` | PASS | — |
| `git diff --check` | PASS | — |
| Strict OpenSpec validations | PASS, 52/52 | — |

The production-only recovery census moved exactly as reviewed:

| Measurement | HEAD `07c37f73` | OT1 worktree | Delta |
|---|---:|---:|---:|
| Production owner files importing `internal/lifecyclejoin` | 21 | 20 | -1 |
| `lifecyclejoin.NewGeneration` | 21 | 20 | -1 |
| `Generation.Stop` | 30 | 29 | -1 |
| External `Generation.Cancel` | 4 | 3 | -1 |
| `lifecyclejoin.NewOperation` / lifecycle `Operation.Run` | 2 | 1 | -1 |
| `Generation.StopWithQuiesce` | 3 | 3 | 0 |
| External `RunPartialStartRollback` calls | 14 | 14 | 0 |
| Final parent-aware `RollbackFailedStart` production owner calls | 14 | 14 | 0 |

Stable OT1 source identities for this checkpoint are:

- `output/otel/component.go`:
  `e1235cc252e6194269c762f5d727914995a48ddfe31b81bc3dd491e164004c19`;
- `output/otel/component_test.go`:
  `c109d81e80108cb3d8853a267c1ac5ea943ef6874308bc752eb4617ca4ea38eb`;
- `output/otel/component_lifecycle_integration_test.go`:
  `f7d00429c4bae897f8b6f40ee91b90984a54fe2c0747a13477482fbf6e9a4987`.

OT1 introduces no outward API or configuration change and no natsclient or consumer-deletion drift. The unrelated
Metrics inventory remains byte-identical at SHA-256
`8a3b74786df6098aa053edd5c5c5e68f42f817ebd44008cdb75b8dece9eb2fc5`. Task 2.3, Gate A/B/C, runtime migration,
proof, release, archive, and tag readiness remain unchecked and incomplete.

### S1 fixed-port six-owner implementation checkpoint — 2026-08-20

The owner explicitly approved only temporary `ConsumeStreamWithConfigHandle` for the five S1 JetStream owners under
the branch no-release/no-tag invariant. The canonical port method, split-context bridge or method,
`ConsumeDurable`, `natsclient.Subscription`, Metrics APIs, and N1 retirements remain excluded. The split-context bridge
is deferred to A1, its first real caller.

Independent `semstreams-reviewer` verdict `APPROVE` applies to the S1 dirty worktree based on full commit
`1fd214afd32ca5fcbfab5657f3cfdd68fd84afa1`. Owner-migrated credit is granted only to these six frozen production
owners:

- `examples/processors/document/component.go`;
- `examples/processors/iot_sensor/component.go`;
- `examples/processors/weather_station/component.go`;
- `processor/json_filter/json_filter.go`;
- `processor/json_generic/json_generic.go`;
- `processor/json_map/json_map.go`.

Supporting natsclient and test files receive no owner credit.

The bridge review initially returned a blocker because the shared managed-consumer helper called native Consume and
then checked `setupCtx.Err()`. Cancellation in that post-commit branch could force Stop, forget the consumer, and
release its claim before native Closed. The corrected bridge-specific commit path completes every fallible observation
and context check before native Consume, returns the exact handle with no post-commit fallible branch, and releases the
claim and metrics only after exact Closed. A controlled cancellation-during-Consume test proves that boundary. The
corrected controlled race passed 10 repetitions in 1.339s; the real-NATS bridge test passed in 2.392s.

The initial proof review returned HIGH because tests manually seeded `cleanupPending`, did not exercise actual
Start/Stop overlap or a production second-acquisition failure, and lacked JetStream normal-path Closed-before-callback
cancellation evidence. The correction added owner-private seams that drive actual Start through one exact acquired
handle, block the second acquisition, overlap Stop while proving unlocked `startDone` waiting, then fail and exercise
real rollback. All five JetStream owners prove successful Start keeps native Closed and callback contexts live until
Stop; Weather proves the equivalent real core-NATS path.

The causal RED sequence was:

1. The exported API census failed because the approved temporary handle signature did not exist.
2. Four untouched Stop-before-Start cases failed against the one-shot terminal contract.
3. The controlled bridge test did not compile because `startPortConsumerHandle` did not exist.
4. The causal JSON Map lifecycle tests did not compile because the required private wait/consume seams did not exist.

Final independent evidence:

| Surface | Overlap + native-close race x10 | Full unit race | Full integration race |
|---|---:|---:|---:|
| JSON filter | 1.324s | 1.242s | 5.203s |
| JSON generic | 1.532s | 1.556s | 1.972s |
| JSON map | 1.793s | 1.384s | 5.715s |
| Document | 1.856s | 1.722s | 2.711s |
| IoT | 2.060s | 1.893s | 2.128s |
| Weather | real-NATS overlap x10: 4.925s | 1.979s | 4.107s |

`task lint`, `git diff --check`, and all 52/52 strict OpenSpec validations passed.

The authoritative tracked-production census moved exactly as reviewed:

| Measurement | HEAD `1fd214af` | S1 worktree | Delta |
|---|---:|---:|---:|
| Production owner files importing `internal/lifecyclejoin` | 20 | 14 | -6 |
| `lifecyclejoin.NewGeneration` | 20 | 14 | -6 |
| `Generation.Stop` | 29 | 17 | -12 |
| External `RunPartialStartRollback` calls | 14 | 8 | -6 |
| Final parent-aware `RollbackFailedStart` production owner calls | 14 | 20 | +6 |
| `Generation.StopWithQuiesce` | 3 | 3 | 0 |
| `Client.StopConsumer` production calls | 5 | 0 | -5 |
| Temporary bridge production owner callers | 0 | 5 | +5 |

Repository-root scanners that traverse user-owned `.claude/worktrees` are polluted and are not ledger evidence. The
table above is the authoritative tracked-production measurement.

Stable S1 source identities for this checkpoint are:

- `examples/processors/document/component.go`:
  `d0ec2b817238515cf6172822fb46670d443198228f26b52c6357d6b20890794f`;
- `examples/processors/document/lifecycle_owner_test.go`:
  `1402aaee9eb25d611b4c08e8714136fe009e9b2c229ef7f1bf751e508243d2bb`;
- `examples/processors/iot_sensor/component.go`:
  `2b04e36a9b714803cd60cf40e29b9cc10726c9ab96d4e09332b851609b9cbf88`;
- `examples/processors/iot_sensor/lifecycle_owner_test.go`:
  `4641607e8a472a2f1259ead186c326b8854d7e3d0b3b0dd3473a8199dcb197d9`;
- `examples/processors/weather_station/component.go`:
  `0c31902a00260c01988e96126fa3673d83ceebd7937a37242c544c6a9c3546bb`;
- `examples/processors/weather_station/lifecycle_integration_test.go`:
  `ccf18ab955a072e7a398726090d508bb1e62ded992d4f29e30f228029cfd9c29`;
- `examples/processors/weather_station/lifecycle_owner_test.go`:
  `fccfec1ab953d27d8d7cfdc40576dfcea258a840159f5574678a72a44e40007a`;
- `natsclient/client.go`: `883fabf1391770035c4245fa41daa6f5c4136a737c3bd2b23b52f5bb529fa154`;
- `natsclient/consumer_policy_callsite_test.go`:
  `0f6db12208f7bc03be79a21cbd178681263e795fca26831fade2b6170b1a1429`;
- `natsclient/internal_consumer_lifecycle_integration_test.go`:
  `23ab086c70f0eac3332b32f048d665a844f1fb2a7de5840357c0c010690ee58b`;
- `natsclient/stream.go`: `44d6c6a170e126d2f0753d4ff1f5d984df6e010b273f308f5d627c7ce004f669`;
- `natsclient/stream_handle_test.go`:
  `6a50a5c8540723e407f2bae8335b84ee6bba212df6fb6034ac90eed09b30dd5c`;
- `processor/json_filter/json_filter.go`:
  `f28b6bd96a74752851374adee9eb2795be18e81cd6b3e950f3fb784396dacd41`;
- `processor/json_filter/lifecycle_integration_test.go`:
  `e6e263ad41da6675e52348622802e4c0f368ee7a62a6b7f9e260ce7527a782ce`;
- `processor/json_filter/lifecycle_owner_test.go`:
  `d2340aa4a931d2afc6230b87f1ad068f8dcc74f44b64d581b2fc78361a35fa63`;
- `processor/json_generic/json_generic.go`:
  `48e57d9d8a2e83c74635e1e6da78d1e561e9f0177178cff0d26fb6f6f745c3fa`;
- `processor/json_generic/lifecycle_integration_test.go`:
  `e0c7425143197ecbddd2e7787b1ee6da521554ea2694dd2d6c173743e5990321`;
- `processor/json_generic/lifecycle_owner_test.go`:
  `c4e79816158943a8d15fd82582ad3c11c806bfbb2e51f2cc7630b9517edb3046`;
- `processor/json_map/json_map.go`:
  `e7fd6c50e279c069667bdb9bd5a497c2f8ed41484ce91c2cf430296f4f35e04e`;
- `processor/json_map/lifecycle_integration_test.go`:
  `6323b735a7ee1fc01020cc219243158d1a6f6ac46cad56c74045d6ea8a115857`;
- `processor/json_map/lifecycle_owner_test.go`:
  `c5660fdc0d8b3605c50b019b79e68a375344105da82893f93faf135e27eec0ca`.

There is no outward API or configuration change beyond the explicitly approved temporary bridge, no canonical or
split-context signature change, and no `ConsumeDurable`, `natsclient.Subscription`, Metrics, or consumer-deletion
drift. The unrelated Metrics inventory remains byte-identical at SHA-256
`8a3b74786df6098aa053edd5c5c5e68f42f817ebd44008cdb75b8dece9eb2fc5`. Task 2.3, Gate A/B/C, runtime migration,
proof, release, archive, and tag readiness remain unchecked and incomplete.

### A1 agentic five-owner implementation checkpoint — 2026-08-20

S1's owner-approved conditional split bridge is exercised now by Loop, its first real caller. Temporary
`ConsumeStreamWithConfigContextsHandle` preserves the exact setup context for fallible setup and the exact handler
context for delivery, returns the exact native handle, and retains the opaque claim and metrics until exact Closed.
It remains branch-only under the no-release/no-tag invariant.

Independent `semstreams-reviewer` verdict `APPROVE` applies to the A1 dirty worktree based on full commit
`34caf623f0074b9aa2b50e5d4d76d0cf6ccca865`. Owner-migrated credit is granted only to these five frozen production
owners:

- `processor/agentic-dispatch/component.go`;
- `processor/agentic-governance/component.go`;
- `processor/agentic-loop/component.go`;
- `processor/agentic-model/component.go`;
- `processor/agentic-tools/component.go`.

Supporting natsclient, `http_activity`, `inflight`, and test files receive no owner credit.

The initial implementation review returned HIGH because the submitted tests did not causally drive real Start/Stop
overlap and production acquisition failures through the five owners. The correction introduced private owner seams,
blocked real acquisition after an exact first handle, overlapped Stop while proving unlocked `startDone` waiting, and
exercised real failed-Start rollback and later cleanup. Five owner causal tests also prove Drain, exact Closed, and
callback-context cancellation order rather than manually seeding terminal state.

A later review returned HIGH on core-NATS rejoin authority. A core Drain can return a caller-context error while its
native drain remains in progress. The correction retains the exact request subscription in `cleanupPending`; a later
Stop retries the wrapper wait and clears it only after the same native drain completes. This is retained acquired
cleanup authority, not running Stop result replay. The corrected core retry/rejoin test passed 10 race repetitions in
1.304s.

The causal RED sequence included the missing split-context handle API, missing private owner seams, and missing core
cleanup retry. The split API census failed before the bridge existed; causal owner tests did not compile until their
private wait/consume seams existed; and the core cleanup case failed until the exact request subscription remained
reachable for a later bounded cleanup attempt.

Owner-specific corrections preserve these orders and boundaries:

- Dispatch starts its lazy GraphView under the Start-derived control goroutine instead of
  `context.Background`, and terminal Stop prevents recreation.
- Loop reads `Consumer.Info` directly for outstanding-work observation rather than using Client lifecycle authority;
  its core trajectory/inflight subscriptions drain before JetStream Closed, then the sweeper and run context stop.
- Model drains JetStream and awaits Closed, cancels runtime work, then closes cached clients outside `clientMu`.
- Tools drains and retains the exact core tool-list subscription as needed, drains JetStream and awaits Closed, then
  cancels callback contexts.

Final independent focused and full-package race evidence:

| Evidence | Result | Elapsed |
|---|---|---:|
| Loop lifecycle matrix, 10 repetitions | PASS | 1.657s |
| Tools lifecycle matrix, 10 repetitions | PASS | 1.414s |
| Core-NATS cleanup retry/rejoin, 10 repetitions | PASS | 1.304s |
| Full dispatch race | PASS | 1.820s |
| Full governance race | PASS | 1.666s |
| Full loop race | PASS | 3.338s |
| Full model race | PASS | 8.444s |
| Full tools race | PASS | 2.051s |

Developer sequential real-NATS integration evidence:

| Package | Result | Elapsed |
|---|---|---:|
| dispatch | PASS | 41.776s |
| governance | PASS | 2.949s |
| loop | PASS | 28.523s |
| model | PASS | 14.896s |
| tools | PASS | 59.292s |

An earlier combined Loop/Tools command timed out in pre-test reaper cleanup before either package test began; it earns
no test result claim. The sequential package runs above are the integration evidence. Final `task e2e:agentic` passed
in 45.155538667s. `task lint`, `git diff --check`, and strict OpenSpec validation passed.

The authoritative tracked-production census moved exactly as reviewed:

| Measurement | HEAD `34caf623` | A1 worktree | Delta |
|---|---:|---:|---:|
| Production owner files importing `internal/lifecyclejoin` | 14 | 9 | -5 |
| `lifecyclejoin.NewGeneration` | 14 | 9 | -5 |
| `Generation.Stop` | 17 | 7 | -10 |
| External `RunPartialStartRollback` calls | 8 | 3 | -5 |
| Final parent-aware `RollbackFailedStart` production owner calls | 20 | 25 | +5 |
| `Generation.StopWithQuiesce` | 3 | 3 | 0 |
| External `Generation.Cancel` | 3 | 3 | 0 |
| `lifecyclejoin.NewOperation` / lifecycle `Operation.Run` | 1 | 1 | 0 |

Stable A1 source identities for this checkpoint are:

- `natsclient/consumer_policy_callsite_test.go`:
  `bb033d1dda8ab26201ecbd93da4e6ecbaa10ae22ba1970711a9f193e42e197b3`;
- `natsclient/stream.go`: `2826ecc3d9e8aed5f280204b4fa3c268ccde8d71f3d647d74059ec4a3a7548bf`;
- `natsclient/stream_handle_test.go`:
  `1377f013904a859c8faca5d3bc2363ecce28192711a704a1412349b815923d50`;
- `processor/agentic-dispatch/component.go`:
  `dbd9764ee9bed8feec46f0162249f9748179e9c77ccbcbc57d288c7b784e3737`;
- `processor/agentic-dispatch/http_activity.go`:
  `3da64397a7746feac46eeebc27103b735fe0a73ad53cc31a8bda3a50c68488c0`;
- `processor/agentic-dispatch/http_activity_test.go`:
  `85033e03dc5c7e09beb124b53976132231a96a6ef73bfc23987dcf4c7efaa776`;
- `processor/agentic-dispatch/lifecycle_integration_test.go`:
  `2c66844c969f4e3920fc2cd5f8f977a4b77cfa0f36be16ce5ba588f50200f46c`;
- `processor/agentic-dispatch/terminal_settlement_integration_test.go`:
  `d71e26d32561e8cac7a3d7fc7d38e18534f3cd672d2adb6f8af4839b30cc161e`;
- `processor/agentic-dispatch/lifecycle_causal_test.go`:
  `1921e4a709ac8c7267c5b9edc40c669aeed1e9b11c577c6a225d54874f43df81`;
- `processor/agentic-governance/component.go`:
  `95a67516381f50e9249289dccd9e14711d709f62a0769b9519fe152939ae070c`;
- `processor/agentic-governance/lifecycle_integration_test.go`:
  `ed577c9387e6e6b65c26fc6993aa6c15299456151b77ff1f13b6e63d9204e1fb`;
- `processor/agentic-governance/lifecycle_causal_test.go`:
  `c38d123726cb0c1d9f46f2bc9c92dec2f578ef40b0b094f497f45d86b96f638d`;
- `processor/agentic-loop/component.go`:
  `1bc9565cd676675a5882e6fae6abb1cd606648a21d3eff2ba27bb7dabe7848e7`;
- `processor/agentic-loop/inflight.go`:
  `4158b23af3070474f5ea3752787d69694ffeb55a4e9df10bc5a2ac404a76fe1e`;
- `processor/agentic-loop/inflight_test.go`:
  `ec9ee0358faa62b0b62abccc5026ea55ed5bce90fc34353b09d4e7a57af4d4b2`;
- `processor/agentic-loop/lifecycle_integration_test.go`:
  `2bd3cbb041548f1c3f5cf5d4f6184efc1c91a5a5360e0f289cf0a53de74b1d5b`;
- `processor/agentic-loop/spawn_identity_failure_test.go`:
  `69e337a6e0352ff6626a4741d4a681642406f4ccd37a39360050e72ccbb7fec0`;
- `processor/agentic-loop/lifecycle_causal_test.go`:
  `a66ede0027f2c74bae49a5d2046226bbd8a01cfa26bfe26f664febaaa81d377c`;
- `processor/agentic-model/component.go`:
  `3c7f605d196602af518f8443312b9a5f1d283b1e1bbfcb1a0e4a352e4de3a485`;
- `processor/agentic-model/lifecycle_integration_test.go`:
  `7f307ea84a870109feb474721b0f2d738f505b41b9e1d63b9473cae07658c1b2`;
- `processor/agentic-model/lifecycle_causal_test.go`:
  `e70ff139786653af1f4170bd6749923da68c50a401ff6bab5ef6ff38e2ea065f`;
- `processor/agentic-tools/component.go`:
  `3f5f68dcd9f232da93e61c680b9fcf14a613cf37b19a6a8ed6b08893184807f6`;
- `processor/agentic-tools/lifecycle_integration_test.go`:
  `37051abec8ecd213e950abd435bca3eec4d52a0c98dd75db67f920705bc5fe06`;
- `processor/agentic-tools/startup_atomic_integration_test.go`:
  `c06c1bcb1370495a7e16ee2aff26fee03ddec597835c540858ab67a96b630da4`;
- `processor/agentic-tools/lifecycle_causal_test.go`:
  `7009296c1baa3ae58e7006e757620a26d4c492bc334ad9e33dae0db722e05b2c`.

A1 removes the Dispatch GraphView invented context root and preserves read-only Loop observation separate from native
lifecycle authority. It preserves the reviewed Model client and Tools core/JetStream ordering. No name-routed
lifecycle or consumer deletion was added, and there is no outward change beyond the conditionally approved temporary
split bridge. The unrelated Metrics inventory remains byte-identical at SHA-256
`8a3b74786df6098aa053edd5c5c5e68f42f817ebd44008cdb75b8dece9eb2fc5`. Task 2.3, Gate A/B/C, runtime migration,
proof, release, archive, and tag readiness remain unchecked and incomplete.

### H1 standalone HTTP three-owner implementation checkpoint — 2026-08-20

Independent `semstreams-reviewer` verdict `APPROVE` applies to the H1 dirty worktree based on full commit
`e7f58aa1807e258793cf0015d5b2433ac619764d`. Owner-migrated credit is granted only to these three frozen production
owners:

- `gateway/graph-gateway/component.go`;
- `input/websocket/websocket_input.go`;
- `output/websocket/websocket.go`.

`gateway/graph-gateway/readiness_surface.go` and all test files are supporting implementation/evidence surfaces and
receive no owner credit.

The causal RED occupied each configured listen address before Start. The old asynchronous bind path returned success
before `Serve` reported the collision. The corrected owners bind synchronously, install exact Start-derived
`http.Server.BaseContext`, publish no readiness before successful setup, and retain the exact listener/server/serveDone
authority for caller-bounded Shutdown and join.

Independent review required five causal correction stages:

1. **Gateway inference registration and admission fence — HIGH.** Inference was prepared after the standalone handler
   registered, so the route could be absent, while shared handlers mounted directly and bypassed admission. The
   correction prepares a private inference submux before Serve and mounts it only through `admittedHTTP`. A causal
   shared admitted request blocks, Stop waits, a late request receives 503, and release precedes context cancellation.
2. **Input client-mode proof — MEDIUM.** The initial evidence did not exercise a real steady-state client socket. The
   corrected local-peer test proves the exact socket closes before callback cancellation and both client loops join.
3. **Output failed-Start/startDone proof — MEDIUM.** The initial failed-Start test did not overlap Stop. The
   correction gates the Nth real acquisition, proves incremental exact handles and `startDone`/`cleanupPending`, and
   proves Stop waits outside lifecycle locks. It exercises real helper retention, later retry, and no Drain replay.
4. **Gateway late-success resurrection — HIGH.** Start closed `startDone` before publishing `running`, `startTime`,
   and its success log, allowing Stop to terminalize before late Start success resurrected the owner. Success
   publication and logging now precede `startDone` close. A pre-close seam proves Stop waits and the final state remains
   terminal.
5. **Input reconnect late dial — HIGH.** A non-context Dial could publish a reconnect after Stop snapshotted client
   authority. The correction fences `clientOpen` before the snapshot, uses `DialContext`, atomically rejects and
   self-closes a post-dial socket, and suppresses reconnect. A real late-dial test proves no publication, peer close,
   Stop join, and final authority clear.

Final independent evidence:

| Package | Focused six-test lifecycle race x10 | Full race | Integration race |
|---|---:|---:|---:|
| graph gateway | 1.774s | 1.456s | 3.318s |
| input WebSocket | 1.525s | 1.461s | 5.108s |
| output WebSocket | 1.334s | 3.372s | 18.690s |

`gofmt`, `go vet`, `git diff --check`, and strict OpenSpec validation passed. Developer `task e2e:core` passed all
3/3 scenarios. This is H1 wave evidence only; it does not complete a broader proof or gate.

The authoritative tracked-production census moved exactly as reviewed:

| Measurement | HEAD `e7f58aa1` | H1 worktree | Delta |
|---|---:|---:|---:|
| Production owner files importing `internal/lifecyclejoin` | 9 | 6 | -3 |
| `lifecyclejoin.NewGeneration` | 9 | 6 | -3 |
| `Generation.Stop` | 7 | 7 | 0 |
| `Generation.StopWithQuiesce` | 3 | 0 | -3 |
| External `RunPartialStartRollback` calls | 3 | 2 | -1 |
| Final parent-aware `RollbackFailedStart` production owner calls | 25 | 26 | +1 |
| External `Generation.Cancel` | 3 | 3 | 0 |
| `lifecyclejoin.NewOperation` / lifecycle `Operation.Run` | 1 | 1 | 0 |

Stable H1 source identities for this checkpoint are:

- `gateway/graph-gateway/component.go`:
  `030096155f44e80d95af9317f2c99a618e04f95f58a0989c623b1928d35fa8a4`;
- `gateway/graph-gateway/component_test.go`:
  `e6d81e897ce2095372f4b401d2b8d13d56209b9c42c9ef4df5363e6fe70f8c5f`;
- `gateway/graph-gateway/readiness_surface.go`:
  `50d6c43cef1a9791eb95a0311a74de1623486469f180a8de1e96a9987acb4627`;
- `gateway/graph-gateway/lifecycle_owner_test.go`:
  `4c13bb533ce99c2ef10b359ebb97c1d4ed1263bf994c4eba1e33d6305c9ce765`;
- removed supporting `gateway/graph-gateway/lifecycle_integration_test.go`, baseline identity:
  `da8312b658cc45a2438e61fec4db4010649d5d6cd262ab1cb33ed4599945ab93`;
- `input/websocket/websocket_input.go`:
  `973d99294ef789e1d0d32b3a7fce4beac29e79d1e2ba57fe4fadb9ca597e0112`;
- `input/websocket/websocket_input_lifecycle_test.go`:
  `db88a35c6d5764e31e6a830505050bbc21ea6cf7a5e449e9bfdd844e115f1cb4`;
- `input/websocket/lifecycle_owner_test.go`:
  `717179780f05e6947878ef1575f6df7ed277f316219dad386854b362e14bfb41`;
- `output/websocket/constructor_test.go`:
  `36561ac7f3a10580c0752aa17c0c9e5f180896ab26008e2710a0e9c8b85910b1`;
- `output/websocket/lifecycle_authority_test.go`:
  `2257ec39b30122041beb8f1acc71aec3fe2c474bf74ef7e4769a7be2c0986e00`;
- `output/websocket/path_integration_test.go`:
  `cd63949c25f6dfd98fc4ff4d261c257cfb927450622874979284d6718679098e`;
- `output/websocket/websocket.go`:
  `87acf98df590a41c02c9ce41d89a3837b1e048ce56f39ddd7cdb8b76de5a0810`;
- `output/websocket/websocket_test.go`:
  `7ed43d2ea58e8f6082a0c06093eaff2383a1d853c55b2f68a234fc4b7282ce62`;
- `output/websocket/lifecycle_owner_test.go`:
  `29c54503853dc6a948c14a9bb609be80cf3387cf6449f3077be5a90038123e66`.

H1 introduces no outward API, configuration, or context surface change and no name-routed lifecycle or consumer
deletion. M1, ServiceManager HTTP, and pprof remain excluded. The temporary port bridges keep the branch under the
no-release/no-tag invariant. The unrelated Metrics inventory remains byte-identical at SHA-256
`8a3b74786df6098aa053edd5c5c5e68f42f817ebd44008cdb75b8dece9eb2fc5`. Task 2.3, Gate A/B/C, runtime migration,
proof, release, archive, and tag readiness remain unchecked and incomplete.

### O1 static output two-owner implementation checkpoint — 2026-08-20

Independent `semstreams-reviewer` verdict `APPROVE` applies to the O1 dirty worktree based on full commit
`29d4e1706a59dee037d764cf2cd6aa2ced26565d`. Owner-migrated credit is granted only to:

- `output/file/file.go`;
- `output/httppost/httppost.go`.

Lifecycle tests and package documentation are supporting evidence/adopter surfaces and receive no owner credit.

The initial TDD RED was compile-time causal evidence: the new lifecycle tests could not compile while owner-local
one-shot state, exact native handle seams, and HTTPPost Start-owned ACME authority were absent. The accepted
implementation serializes Start/Stop, drains exact core and JetStream callback authority before cancellation, retains
bounded failed-Start cleanup authority, makes completed repeated Stop nil without replay, and rejects same-instance
restart.

Independent review then found a blocker in HTTP transport cleanup. Terminal cleanup omitted
`CloseIdleConnections`, allowing pooled TCP connections to survive component Stop. The reviewer-correction RED was:

```text
go test ./output/httppost \
  -run TestLifecycleOwnerClosesHTTPIdleConnectionsAfterCallbacksAndACMEJoinOnce \
  -count=1
```

It failed with terminal order `acme-join`, wanted `acme-join,idle-close`. The correction waits for native callbacks to
close, cancels runtime work, joins ACME cleanup, then closes idle connections exactly once after successful cleanup.
It does not close them while cleanup remains pending, and completed repeated Stop does not replay the close.

Final developer and independent evidence:

| Evidence | File output | HTTP POST output |
|---|---:|---:|
| Causal lifecycle race, 10 repetitions | PASS, 1.309s | PASS, 1.360s |
| Full package race | PASS, 1.247s | PASS, 3.355s |
| Integration race | PASS, 15.154s | PASS, 10.801s |
| Reviewer corrected HTTP causal, 20 repetitions | — | PASS, 1.352s |
| Reviewer full HTTP race | — | PASS, 3.251s |
| Reviewer HTTP integration race | — | PASS, 11.219s |

`task lint`, `task build`, `git diff --check`, and strict OpenSpec validation passed. Repository-wide race is not
claimed fully green because repository-root scanners traverse user-owned `.claude/worktrees`; that known scanner
pollution is not behavioral O1 evidence. The focused, full-package, and integration O1 surfaces above are green.

The authoritative tracked-production census moved exactly as reviewed:

| Measurement | HEAD `29d4e170` | O1 worktree | Delta |
|---|---:|---:|---:|
| Production owner files importing `internal/lifecyclejoin` | 6 | 4 | -2 |
| `lifecyclejoin.NewGeneration` | 6 | 4 | -2 |
| `Generation.Stop` | 7 | 5 | -2 |
| External `RunPartialStartRollback` calls | 2 | 2 | 0 |
| Final parent-aware `RollbackFailedStart` production owner calls | 26 | 28 | +2 |
| `Generation.StopWithQuiesce` | 0 | 0 | 0 |
| External `Generation.Cancel` | 3 | 3 | 0 |
| `lifecyclejoin.NewOperation` / lifecycle `Operation.Run` | 1 | 1 | 0 |

Stable O1 implementation and evidence identities are:

- `output/file/file.go`:
  `2e63b349ee7b6e88efaf6916c97c779355c343c00da2568a32a4c0c5a6e6197c`;
- `output/file/lifecycle_integration_test.go`:
  `3b0274d8f70163a897c0463200bfdd0e80f057a08e5964e616e2f9d9095fad11`;
- `output/file/lifecycle_owner_test.go`:
  `cb991b1508bd5db47d3a3198a13e0cf6cb10009003b2a34d4e6d4c8e6aeae528`;
- `output/httppost/httppost.go`:
  `9f85d95ca9d50518a01b4f1fe2b630c805d48a5ffb7f86f1ed99282438d2414c`;
- `output/httppost/lifecycle_integration_test.go`:
  `ab0d8e39794c1534a6c28bf8592bdf5540999c0b7bcac4ab669223e2ef779c98`;
- `output/httppost/lifecycle_owner_test.go`:
  `1660385565bf76079572a8370d2571e70cd266195992691345b3eb96c8b93b5f`.

The adopter-facing package documentation now states the existing lifecycle contract without changing signatures or
configuration: each instance is one-shot, lifecycle transitions are serialized, Stop is caller-bounded, a completed
repeat Stop is nil, and reuse requires a fresh component instance. Stable package-document identities are:

- `output/file/README.md`: `bb6f13fb866b1944ecb54e6818e259f56f42f2d6e193e53fb950de51920b2ee3`;
- `output/file/doc.go`: `f7c73d9fcf6e8f3f099e2c2cd6c7d552a7e7d6ee3c14bf81df27a1d77eaf889b`;
- `output/httppost/README.md`:
  `9661503aa7ba9b7bea45abc01a57ed38218d328c96f164a5e5805efc61ee9ed2`;
- `output/httppost/doc.go`:
  `652c401f0b4b9ae4c0bf65ef2f8900ebc8427926b5799f94c242d5d3e9679bf1`.

O1 introduces no public signature or configuration change and no context, name-routed lifecycle, or consumer-deletion
surface. M1, OS1, RU1, GI1, N1, unrelated API, and broader gate credit remain excluded. The temporary port bridges keep
the branch under the no-release/no-tag invariant. The unrelated Metrics inventory remains byte-identical at SHA-256
`8a3b74786df6098aa053edd5c5c5e68f42f817ebd44008cdb75b8dece9eb2fc5`. Task 2.3, Gate A/B/C, runtime migration,
proof, release, archive, and tag readiness remain unchecked and incomplete.

### OS1 ObjectStore implementation checkpoint — 2026-08-20

Independent `semstreams-reviewer` verdict `APPROVE` applies to the OS1 dirty worktree based on full commit
`5cd94cee2addd02677f1dc048a200020f0af2776`. Owner-migrated credit is granted only to
`storage/objectstore/component.go`; lifecycle tests and package documentation are supporting surfaces and receive no
owner credit.

The TDD RED was exact:

```text
go test ./storage/objectstore -run '^TestLifecycleOwner' -count=1
```

It failed to compile because the owner-local exact-handle seams `newStore`, `closeStore`, `subscribeCore`,
`consumeStream`, and `waitConsumerClosed` did not exist. The accepted implementation publishes `startDone` before
fallible acquisition, owns each exact core subscription and JetStream handle, drains callbacks while Store authority
remains live, cancels only after native closure, then closes the Store. Failed-Start cleanup retains exact handles and
Store authority for a later caller-bounded Stop; completed repeated Stop is nil without replay.

Final developer and independent evidence:

| Evidence | Developer | Reviewer |
|---|---:|---:|
| Lifecycle race, 10 repetitions | PASS, 1.367s | — |
| Lifecycle race, 100 repetitions | — | PASS, 1.452s |
| Full package race | PASS, 1.315s | PASS, 1.237s |
| Full integration race | PASS, 65.033s | PASS, 65.248s |
| Real-NATS one-shot lifecycle, 3 repetitions | — | PASS, 1.891s |
| Stable-consumer integration, one run | — | PASS, 1.962s |

`task lint`, `task build`, schema generation, `git diff --check`, and strict OpenSpec validation passed. A combined
integration `count=3` run that included the pre-existing stable-consumer test failed on later repetitions because its
fixture deliberately retains the same durable identity and does not reset the namespace. The stable-consumer test is
green at `count=1`, consistent with the no-delete contract. The repeated-fixture failure earns no regression or
repeated-run claim.

Repository-wide race is not claimed fully green because repository-root scanners traverse unrelated user-owned
`.claude/worktrees`, and four stale testinfra baseline rows remain. The ordinary ObjectStore package race is green.

The authoritative tracked-production census moved exactly as reviewed:

| Measurement | HEAD `5cd94cee` | OS1 worktree | Delta |
|---|---:|---:|---:|
| Production owner files importing `internal/lifecyclejoin` | 4 | 3 | -1 |
| `lifecyclejoin.NewGeneration` | 4 | 3 | -1 |
| `Generation.Stop` | 5 | 3 | -2 |
| External `RunPartialStartRollback` calls | 2 | 1 | -1 |
| Final parent-aware `RollbackFailedStart` production owner calls | 28 | 29 | +1 |
| `Generation.StopWithQuiesce` | 0 | 0 | 0 |
| External `Generation.Cancel` | 3 | 3 | 0 |
| `lifecyclejoin.NewOperation` / lifecycle `Operation.Run` | 1 | 1 | 0 |
| ObjectStore owner-side `Client.StopConsumer` calls | 1 | 0 | -1 |

OS1 preserves the configured ObjectStore bucket and durable consumer identity; Stop does not delete either. The live
StoreProvider remains available until admitted callbacks drain, then terminal cleanup clears local Store authority.
JetStream writes retain their existing settlement contract: ACK only after the store commit and required
StorageReference publication succeed; transient/cancellation failures retain their existing NAK behavior and
structurally invalid work retains its existing Term behavior.

Stable OS1 implementation and evidence identities are:

- `storage/objectstore/component.go`:
  `b095db2546a9e8bc8e0cdab17d42199c721ad949c0754ead930f4920c230097f`;
- `storage/objectstore/lifecycle_integration_test.go`:
  `b39028f55da8fabe8721e8bf900627ca98b5568aeab0762efb57414bafd061c6`;
- `storage/objectstore/lifecycle_owner_test.go`:
  `9ce6d9a51892ccbbab7d008f215096de042953d1bfef74618bd2a7e0c388d384`.

The package README and doc comment said all operations were concurrent, which overbroadly included lifecycle
transitions. Their narrow correction preserves the Store/data/handler concurrency claim while stating serialized
one-shot lifecycle, nil completed repeat Stop, and fresh-instance reuse. Stable package-document identities are:

- `storage/objectstore/README.md`:
  `c9d341ec3fb0ea79145ba4d167b87a1b12fee5a243b4afdef448ed07af0691aa`;
- `storage/objectstore/doc.go`:
  `4356b0296df47e08ae0966b955181b3c4731a14c3b91516aae590ccd61a8d54c`.

OS1 introduces no outward API, configuration, context, or consumer-deletion surface. It removes the internal
name-routed `Client.StopConsumer` lifecycle call in favor of the exact native handle; no name-routed lifecycle API was
added. RU1, GI1, M1, N1, and all broader gate credit remain excluded. Temporary bridges keep the branch under the
no-release/no-tag invariant.
The unrelated Metrics inventory remains byte-identical at SHA-256
`8a3b74786df6098aa053edd5c5c5e68f42f817ebd44008cdb75b8dece9eb2fc5`. Task 2.3, Gate A/B/C, runtime migration,
proof, release, archive, and tag readiness remain unchecked and incomplete.

### GI1 graph-ingest implementation checkpoint — 2026-08-20

Independent `semstreams-reviewer` first-pass verdict `APPROVE` applies to the GI1 dirty worktree based on full commit
`f5532e3d4986593c7482e70f754aa9ea82a558f6`. Owner-migrated credit is granted only to
`processor/graph-ingest/component.go`. `processor/graph-ingest/keyed_ingest.go`,
`processor/graph-ingest/readiness.go`, and tests are supporting implementation/evidence surfaces and receive no owner
credit.

The causal TDD RED was:

```text
go test ./processor/graph-ingest -run TestLifecycleOwner -count=1
```

It failed to compile before production changed because the owner-local lifecycle fields and exact native binding seams
did not exist. The accepted implementation publishes cleanup and `startDone` authority before acquisition escapes,
owns exact core and JetStream handles, and removes both retained production contexts and both unauthorized roots.

Stop preserves the existing settlement boundary in this order: fence new JetStream delivery; issue Drain and await
exact Closed while submission, keyed-pool, KV/cache, and readiness-label authority remain live; cancel new submission;
join admitted keyed work; drain remaining core callbacks; cancel runtime/status work; join status publication; then
close caches. Failed-Start cleanup retains exact handles for a later caller-bounded Stop. Running deadline Stop is
terminal with no later Drain/result replay.

Final developer and independent evidence:

| Evidence | Developer | Reviewer |
|---|---:|---:|
| Lifecycle race, 10 repetitions | PASS, 1.347s | — |
| Lifecycle + settlement race, 10 repetitions | — | PASS, 1.610s |
| Full package race | PASS, 1.603s | PASS, 1.568s |
| Full integration | PASS, 31.900s | PASS, 31.266s |
| Focused one-shot/failed-Start integration, 3 repetitions | — | PASS, 2.109s |
| Isolated contract race | — | PASS, 8.025s |
| `task e2e:core` | PASS, 3/3 | PASS, 3/3 including graph roundtrip |

`task lint`, `task build`, schema generation with zero drift, `git diff --check`, strict change validation, and all
52/52 strict spec validations passed. Repository-wide race is not claimed green because repository-root scanners
traverse unrelated user-owned `.claude/worktrees`, and four stale testinfra baseline rows remain. The isolated contract
scanner passed; isolated testinfra reported only those four known rows. The ordinary graph-ingest package and
integration surfaces are green.

The authoritative tracked-production census moved exactly as reviewed:

| Measurement | HEAD `f5532e3d` | GI1 worktree | Delta |
|---|---:|---:|---:|
| Production owner files importing `internal/lifecyclejoin` | 3 | 2 | -1 |
| `lifecyclejoin.NewGeneration` | 3 | 2 | -1 |
| `Generation.Stop` | 3 | 2 | -1 |
| External `Generation.Cancel` | 3 | 2 | -1 |
| `lifecyclejoin.NewOperation` / lifecycle `Operation.Run` | 1 | 0 | -1 |
| External `RunPartialStartRollback` calls | 1 | 1 | 0 |
| Final parent-aware `RollbackFailedStart` production owner calls | 29 | 30 | +1 |
| Production stored `context.Context` fields | 2 | 0 | -2 |
| Unauthorized production `context.Background` roots | 2 | 0 | -2 |
| `Generation.StopWithQuiesce` | 0 | 0 | 0 |

GI1 preserves the existing subjects, configuration, generated schema, readiness semantics, and read-only bound
consumer labels. It preserves effect → durable ingest guard → ACK and the existing poison/NAK/Term dispositions. No
name-routed lifecycle or consumer deletion was added, and no outward surface changed.

Stable GI1 implementation and evidence identities are:

- `processor/graph-ingest/component.go`:
  `e0948db7c0dbfd97ab9c49370d1d0d6c519775cd3e32037f71845c32d718645f`;
- `processor/graph-ingest/component_test.go`:
  `7844499285e133b164bb8a5fa67aebe5863df1117a0fffab9bd6d70a4259773d`;
- `processor/graph-ingest/keyed_ingest.go`:
  `8e856ba23498fa38b3b7ac7aa6a5b46a3576847bc3c4ef06d57f7d0ec0371fb3`;
- `processor/graph-ingest/lifecycle_integration_test.go`:
  `a0976d5150f22f142682debf49bee9fc931f007370832c5ec2cee0ecf81e3f52`;
- `processor/graph-ingest/readiness.go`:
  `fd8a3ae4d716594efa04a7648875471696bfc0732ee0bd8b32491bd20f486bb6`;
- `processor/graph-ingest/readiness_integration_test.go`:
  `1edbbf3a1cdc3d214f42b1aab01b94e8d4f234bbd93d450e84392201fd4f369d`;
- `processor/graph-ingest/lifecycle_owner_test.go`:
  `648d1ad3ccf27e49fa161a5dce39f01293bd443f3a8a153c5debf53906b368cc`.

`processor/graph-ingest/README.md` and `processor/graph-ingest/doc.go` were inspected and contain no stale
lifecycle/restart/context-ownership claim directly implicated by GI1, so they remain unchanged at SHA-256
`6d66c73dc1de2f554b6d2d9166df9b565c60f4be418daa8db91bcc060e0d2cc2` and
`96d2749b526255e67e12ae91c731029e1598d9496b6304094071a8c0bbfbc4a4` respectively.

GI1 introduces no API, configuration, context, name-routed lifecycle, or consumer-deletion surface. RU1, M1, N1, and
all broader gate credit remain excluded. Temporary bridges keep the branch under the no-release/no-tag invariant. The
unrelated Metrics inventory remains byte-identical at SHA-256
`8a3b74786df6098aa053edd5c5c5e68f42f817ebd44008cdb75b8dece9eb2fc5`. Task 2.3, Gate A/B/C, runtime migration,
proof, release, archive, and tag readiness remain unchecked and incomplete.

### RU1 Rule package implementation checkpoint — 2026-08-20

The owner's `approved` is binding only for the coherent R1-R5 source contract:

1. Rule lifecycle-dependent public methods become context-first, with no compatibility overload.
2. Rule KV initialization is immediate and receives the owning context.
3. Standalone cron work enters one internal Start-owned dispatcher rather than inventing callback roots.
4. `Matches` rejects nil context instead of silently accepting contextless evaluation.
5. Stop closes update, watcher-borrow, entity-dispatch, and cron admission before snapshot or native teardown.

Independent `semstreams-reviewer` verdict `APPROVE` applies to the final RU1 dirty worktree based on full commit
`049cbbced433738620247c99aab7a48309b6e418`. Owner-migrated credit is granted only to
`processor/rule/processor.go`; every adjacent Rule file, composition caller, package document, migration note, and test
is supporting only.

The causal RED sequence was:

1. Initial reflection tests found retained production contexts and `Generation` lifecycle ownership.
2. Context-first Rule API and `InitializeKVStore` call-site tests failed to compile before the signatures changed.
3. `Matches` accepted nil context.
4. A watcher prepared late enough to escape the Stop snapshot.
5. Entity dispatch made Stop exceed its one-second caller deadline.
6. Cron `Register` succeeded and mutated state after Stop.
7. A duplicate `ConfigManager.Start` succeeded.
8. The production update seam was absent and its causal test failed to compile.
9. A mutated ConfigManager Stop path failed to acquire its cancel authority.

The first independent review blocked late command ordering, contextless behavior, lazy KV initialization, and
standalone cron callbacks that could silently fire outside lifecycle authority. After R1-R5 approval and correction,
the second review blocked a lifecycle gate held across I/O, terminal cron registration, and duplicate/racing
ConfigManager Start. It also required HIGH causal proof of the actual update-watch path and exact migration guidance,
plus a MEDIUM correction to stale `ENTITY_STATES` bucket ownership documentation. Those code findings are now cleared:
barriers precede snapshots, fallible I/O runs outside lifecycle gates, cron registration is terminally fenced,
ConfigManager Start is one-shot/race-safe, actual update-watch work is joined, and contexts remain lexical.

The final review then found one more ConfigManager late-Watch defect: a context-insensitive watcher acquisition could
return a successful native watcher after Stop canceled and snapshotted the published acquisition authority. The
correction checks the exact run context immediately after Watch returns, stops a successful late handle exactly once,
never publishes it as running state, joins Start before Stop completes, and leaves terminal one-shot state. The causal
test blocks acquisition until Stop has canceled it, returns the late handle, proves Stop waits while that handle's Stop
is blocked, then proves exact-once disposal, no running publication, terminal state, rejected restart, and nil repeated
Stop.

Final developer and independent evidence is:

| Evidence | Developer | Reviewer |
|---|---:|---:|
| Full Rule package race | PASS, 4.791s | PASS, 5.977s |
| Focused corrected lifecycle race, 50 repetitions | — | PASS, 5.033s |
| Full Rule integration | PASS, 41.945s | PASS, 43.823s |
| Fresh isolated structural E2E from final production identity | — | PASS, 38/38 |

`task lint`, `task build`, schema generation with zero drift, strict change/spec validation, and `git diff --check`
passed. The reviewer ran the fresh structural E2E in an isolated environment from the final production-file identity;
all 38 scenarios passed, and cleanup removed only that isolated stack and volume. Repository-wide race is not claimed
green because repository-root scanners traverse two unrelated user-owned `.claude/worktrees`, and four stale testinfra
baseline rows remain. The ordinary RU1 package and integration surfaces are green.

The authoritative tracked-production census moved exactly as reviewed:

| Measurement | HEAD `049cbbce` | RU1 worktree | Delta |
|---|---:|---:|---:|
| Production owner files importing `internal/lifecyclejoin` | 2 | 1 | -1 |
| `lifecyclejoin.NewGeneration` | 2 | 1 | -1 |
| `Generation.Stop` | 2 | 1 | -1 |
| External `Generation.Cancel` | 2 | 1 | -1 |
| External `RunPartialStartRollback` calls | 1 | 1 | 0 |
| Final parent-aware `RollbackFailedStart` production owner calls | 30 | 31 | +1 |
| `lifecyclejoin.NewOperation` / lifecycle `Operation.Run` | 0 | 0 | 0 |
| `Generation.StopWithQuiesce` | 0 | 0 | 0 |
| Rule production stored contexts | 3 | 0 | -3 |
| Unauthorized Rule production roots | 9 | 0 | -9 |
| Bounded durability `WithoutCancel` exception | 1 | 1 | 0 |

The sole retained `WithoutCancel` use remains the bounded persistence/finalization exception; it is not a continuing
runtime root. Rule settlement, readiness, subjects, configuration, schemas, dedicated rule hot reload, and effect/ACK
semantics remain unchanged. Graph-ingest exclusively creates and owns `ENTITY_STATES` with history 1 and no TTL; Rule
waits for and opens that bucket read-only without applying retention. No name-routed lifecycle or consumer deletion was
added.

The exact downstream source migration is recorded only in the SemStreams-owned
`docs/operations/migration-restart-safe-nats-client.md`, SHA-256
`5992afa019fb6bb4508cf5167fe67bc67718b968fa176fd9bdcca0568eb496b5`. Read-only census found one SemTeams
`cmd/semteams/main.go` call requiring `InitializeKVStore(ctx, natsClient)`, two SemSpec
`workflow/execrules/rulepack_test.go` calls requiring `SubstituteVariables(ctx, template)`, and 15 SemSpec
`workflow/intakerules/rulepack_test.go` calls requiring `EvaluateEntityState(ctx, entityState)`. SemStreams made no
sister-repository change.

Stable final RU1 implementation and evidence identities are:

- `cmd/e2e-semstreams/main.go`:
  `ff942d8511ac576cd753480138713169ab20e93b75e0eaec71abd127e3108336`;
- `cmd/semstreams/main.go`:
  `49124d9769ee339d961273221e2fb192fec03486dc3c2c18908743206e8a24f9`;
- `processor/rule/actions.go`:
  `d059b66ce07f12f2d3e7a0bff7c5edf05e23b46aebffec95b26beea7e7f35f37`;
- `processor/rule/actions_lifecycle.go`:
  `bf80de4ef8284274f3a7f8f96bb9fc442186d57e73d834fd15f26b7ce8fc051d`;
- `processor/rule/actions_subject_override_test.go`:
  `6baeb69f010b15e506e967a324f5f6b5544f6b919fe58e80ceb196062fe75206`;
- `processor/rule/actions_test.go`:
  `5c67f00c1d2eb6f7fe220c40d923f9f8b7d86a05c742f2c6274232367344182c`;
- `processor/rule/caller_substitution_test.go`:
  `44c56b00e73c3c48037b79a3765944f2a2a344c27f977dfb32726039a92c7214`;
- `processor/rule/cron_scheduler.go`:
  `be11067f3e49b0e0e79ca5271b34411f4b6f0b165a911ee55424d7f8f15a8f8d`;
- `processor/rule/cron_scheduler_test.go`:
  `cee9d706683e7000ccc63f854ed870bc598c289bc39d9617d373e7f8c2088659`;
- `processor/rule/cron_substitution_test.go`:
  `f79f70cb6365dbccfc4d7e9d00dd5d7a778d6b6cb70b5d331d7cb34f7ca5079f`;
- `processor/rule/docs/custom-rules.md`:
  `9367e7a252e484cb2e253cdb6e24dedcfd4b5a224ed53df8c9c3a0b212162beb`;
- `processor/rule/docs/entity-watching.md`:
  `8d5d5ba590b5c13905788655aaa5b5f8149869f1719b01e5d1d6a3d558bd243b`;
- `processor/rule/entity_evaluation_fence.go`:
  `ea28d0a98a2718a3ccdf9f20b1e08057fedaec1f391092a134e2ca80bc6b0fe3`;
- `processor/rule/entity_rule_pattern_selection_test.go`:
  `b3cb78821249bdb37e2cde207773bef2b97e606f1eda23e2343f5543737c4646`;
- `processor/rule/entity_substitution_test.go`:
  `bcfd7f00e607fbd2a81b11262c0cd3a075a8ea1d1e42239e95d9349112b65a48`;
- `processor/rule/entity_watcher.go`:
  `1cae91c2e357e676d3c89a27605f2b20918835fdc882800563779bcff795a36a`;
- `processor/rule/entity_watcher_generation_test.go`:
  `b5a459a66c7c641e185a008669ded53c28627134fd356640f0ff7dab1be0a7d6`;
- `processor/rule/entity_watcher_hardening_integration_test.go`:
  `9d1e58990536d0550aecd8343595c34a136f85c2bbb207a0bd360e0f636ec356`;
- `processor/rule/example_fan_out_integration_test.go`:
  `6dfd97d34f992d950b53525aa99c5079e2d436936a08955984eae3ca44161abf`;
- `processor/rule/execution_context.go`:
  `b8d834bc268a9d0b36c1036c42439c018791f4b2c1b4fb48d85c08546df0879b`;
- `processor/rule/expression_factory.go`:
  `7ca9507b97a03bbfef5b1e76365221f27338dcb335620dff2307c796f1c5d867`;
- `processor/rule/expression_factory_test.go`:
  `d545c2cc57836d7ec2a0052e9f5e5d91d7d85febdf77934ae074f47a0339baa5`;
- `processor/rule/for_each_substitution_test.go`:
  `60e017748de24424b6a7ed42169034388a9699cf8b08eba99b3804eabb6cb4fe`;
- `processor/rule/interfaces.go`:
  `f9c9d759f949e92ced8e9ef17444802bcd730ff678e1f796f3f5c70a416e2d49`;
- `processor/rule/kv_config_integration.go`:
  `920c8c8369569286880b6991a22dbfe0b3e8b5fb7c2279f4ff4a6ee20ada78f8`;
- `processor/rule/kv_config_list_test.go`:
  `989b5c88c9d1cabbbc36c6c9e51f049e904789b80a737e38025cb0e9bf89a774`;
- `processor/rule/kv_hot_reload_integration_test.go`:
  `110c6fa86301483be59c56d1eee3c950518bf7ba689a30484f82c7f96f1f23ea`;
- `processor/rule/lifecycle_substitution.go`:
  `4614926fc1f8c91a37663072c8064c3e49438b74942632330fb5cc47710fb0a2`;
- `processor/rule/lifecycle_substitution_test.go`:
  `6b5ca94bdf23e7a0a6014cce663400e7b561a1773567431d6589fcfa3b36c241`;
- `processor/rule/matches.go`:
  `70a12cb17e0d8ac013e6b7833eb4d4bfb5efbcefeaa1b6cfccad117f87ec982c`;
- `processor/rule/matches_test.go`:
  `23294ee2d58803a21c32c745111a6e137e1989623fe3b09fb5c398082ec45568`;
- `processor/rule/message_handler.go`:
  `f4cad9705b03388cda3c70ebeb2f657bd773ed9e4e58b696f3a903801e1122db`;
- `processor/rule/message_substitution_test.go`:
  `c083ccd8bc5c68216fd1b0169f19c2ff2bdcc921fbbd132ea22e023045788d4e`;
- `processor/rule/processor.go`:
  `1d1dc7cad6f0af470753a1d59f2f7fe3ad93dd59c3723d6a43d4bda4635496f1`;
- `processor/rule/research_graph_pipeline_integration_test.go`:
  `e69fd1d3ae88365d16323a3b48b5ef2fee9f35681fc04ab26110e0724579c1af`;
- `processor/rule/rule_lifecycle_test.go`:
  `ab8857509634514e8aa7e9cc5f1d218082edefaf4fb522a52eb05808b607406a`;
- `processor/rule/runtime_config.go`:
  `a649914a93b3ba82a9daf0b60a1421f9afd80990531378f4f6efa45db0ebd402`;
- `processor/rule/runtime_config_lifecycle_test.go`:
  `7d83593ff8574c79ccfd28da9d5a26ef0df4dd9b718fa3b207b411960b545625`;
- `processor/rule/schedule_tracker_test.go`:
  `e5c7a98f9caf5bc542cd9ce08d2c054ecfb5272f4fe4ca5538e59e128123d2c3`;
- `processor/rule/stateful_evaluator.go`:
  `125741e9571b64d7c7b1d26b586040ea0339ccffb60076c4ff60dc0faaced0f3`;
- `processor/rule/stateful_evaluator_persist_test.go`:
  `9706addb2f888a170b296ce942398bf4b6406a2bdbbdd3f5d80bfaf0c2a5b8aa`;
- `processor/rule/test_rule_factory.go`:
  `d90def898c89c7e7cab7d43607e9b9fde9e9567b62b2b6c0f04244a930afdb75`;
- `processor/rule/triple_length_substitution_test.go`:
  `17aa3766e43f1f87f4a74ba0da48d25f41d6896346b11eac655a72a159d2a37d`;
- `processor/rule/triple_triples_substitution_test.go`:
  `69309fd4373c613d9a7af8fa854cb21cd56c5c29dca1de6d90d0ea8e0ed65de4`;
- `processor/rule/triple_value_substitution_test.go`:
  `4c074966134ae23516c21840d7c151eb8c3a030da1fda0a1649d85d6afa81b4d`;
- `processor/rule/typed_substitution_test.go`:
  `4c01c86691d166b95fa03a8ccf36ca8fb6d0e58cce3e5790b3fc382a316ae6e9`;
- `test/e2e/scenarios/crud-tools/fire_every_n_fixture_test.go`:
  `144774e99b866ea67a042490baafb8cb9357dcd3c0cd919e066f248977a38f8d`;
- `processor/rule/lifecycle_owner_test.go`:
  `626dd1534c8a0cc63f5ba92b9e80e963fbf7a4af2a41b206c6002c9aa44ccd02`.

The unrelated Metrics inventory remains byte-identical at SHA-256
`8a3b74786df6098aa053edd5c5c5e68f42f817ebd44008cdb75b8dece9eb2fc5`. RU1 owner-migrated credit remains limited to
`processor/rule/processor.go`; supporting production files, composition callers, docs, migration guidance, and tests
receive no owner credit. RU1 grants no M1 or N1 credit. Temporary bridges keep the branch under the no-release/no-tag
invariant. Task 2.3, Gate A/B/C, runtime migration, proof, release, archive, and tag readiness remain unchecked and
incomplete.

### M1 Metrics HTTP implementation checkpoint — 2026-08-20

The owner approved the exact existing-surface break to `metric.Server.Start(context.Context)` and
`metric.Server.Stop(context.Context)`, with no aliases, shims, options, or second lifecycle surface. Each Server and
Metrics service instance is one-shot; restart constructs a fresh instance. pprof remains the explicit
process-lifetime exception and is outside M1.

Independent final `semstreams-reviewer` verdict `APPROVE` applies to the M1 dirty worktree based on full commit
`c7ca5d0f8e23c8e2deb516a6e10121705e291e7a`. Owner-migrated credit is granted only to `service/metrics.go`.
`metric/handler.go`, all tests, package/adopter documentation, and migration guidance are supporting implementation,
evidence, or discovery surfaces and receive no owner credit.

M1 binds the provider before BaseService commits running state and passes the exact Start-derived context through
`http.Server.BaseContext`. It stores no context. Owner-local `startDone` and cleanupPending authority make Stop wait
outside locks for Start finalization and preserve the exact provider after failed-Start cleanup failure. Running Stop
keeps the runtime context live while invoking the provider, then cancels and joins BaseService work. Only failed-Start
cleanupPending may retry cleanup; a running Stop attempt is terminal, and a completed repeated Stop returns nil
without replaying a prior result. Concurrent Stop is unsupported and returns a typed transient error rather than
sharing a result.

`metric.Server.Stop` attempts graceful `http.Server.Shutdown` within the caller's context. If graceful shutdown fails
or that budget expires, it force-closes the exact HTTP server and listener, then observes the original serveDone under
a separately and immediately bounded one-second context. It joins the original graceful, force-close, serve, and join
errors, clears exact handles once, and makes repeated Stop a nil no-op.

Exact M1 conformance mapping:

| Owner ruling | Result | File:line evidence |
|---|---|---|
| Replace existing API; add no second surface | PASS | E1 |
| One-shot instance; fresh instance restarts | PASS | E2 |
| Synchronous bind; exact Start `BaseContext` | PASS | E3 |
| Caller-bounded grace; terminal force/join | PASS | E4 |
| Owner `startDone`; failed-Start retry authority | PASS | E5 |
| Repeat nil; concurrent Stop typed transient | PASS | E6 |
| Retain no `context.Context` | PASS | E7 |
| Exclude pprof; leave it unchanged | PASS | E8 |

- E1: `metric/handler.go:56` and `metric/handler.go:155` replace the two methods;
  `service/metrics.go:43-46` consumes only that surface.
- E2: `metric/handler.go:63-71` enforces use once; `metric/handler_test.go:43-68` proves restart with a new instance.
- E3: `metric/handler.go:113-146` binds before returning and injects the exact context;
  `metric/handler_test.go:48-51` proves both facts.
- E4: `metric/handler.go:151-223` implements graceful shutdown, force-close, and immediate bounded join;
  `metric/handler_test.go:126-170` proves terminal deadline cleanup.
- E5: `service/metrics.go:125-180` publishes and finalizes Start; `service/metrics.go:199-276` owns cleanup order and
  retry; `service/metrics_owner_test.go:73-194` proves failed-Start authority and Start/Stop overlap.
- E6: `metric/handler.go:159-169` and `service/metrics.go:218-253` define terminal/concurrent behavior;
  `metric/handler_test.go:70-109` and `service/metrics_owner_test.go:46-70` prove it.
- E7: the production state records at `metric/handler.go:23-34` and `service/metrics.go:20-40` retain only native
  handles, phase state, and private cancellation.
- E8: `service/pprof.go:8-38` records the process-lifetime exception; `git diff -- service/pprof.go` is empty.

The TDD and review correction sequence is recorded conservatively:

1. Initial owner tests failed before the context-bearing one-shot provider and owner-local lifecycle state existed.
2. The causal deadline RED proved a canceled Stop retained a non-nil server handle and could later rejoin the same
   serving result.
3. The first independent review blocked that behavior because Metrics terminalized and discarded the provider after
   the deadline, allowing the retained server to leak. The correction added terminal force-close and the separate
   bounded join while preserving failed-Start retry authority only in Metrics.
4. The second independent review raised HIGH for missing causal Start/Stop overlap proof. The added test blocks Start
   after exact provider publication but before BaseService commit, proves Stop waits outside `lifecycleMu`, proves a
   canceled waiter neither steals nor closes provider authority, then releases Start and proves one terminal cleanup.

Final developer and independent evidence is:

| Evidence | Developer | Reviewer |
|---|---:|---:|
| Focused metric race matrix, 10 repetitions | PASS, 1.788s | PASS |
| Focused service M1 race matrix, 10 repetitions | PASS, 33.894s | PASS |
| Causal Start/Stop overlap race, 50 repetitions | — | PASS |
| Full metric + service race | PASS | PASS |
| Metrics lifecycle integration, 10 repetitions | PASS, 2.199s | — |
| Isolated lifecycle contract race | PASS, 2.135s | — |
| `task e2e:core` | PASS, 3/3 | — |

`task lint`, `task build`, schema generation with zero drift, `git diff --check`, strict OpenSpec validation, gofmt,
and vet passed on their recorded developer or reviewer runs. A root independent focused provider/service run passed in
1.290s/4.672s before the later overlap-only service test addition; final reviewer evidence covers the final service
identity. Repository-wide race is not claimed green because repository-root scanners traverse unrelated user-owned
`.claude/worktrees` and stale policy-baseline rows remain. The broad integration parent also retains unrelated stale
MessageLogger and ComponentManager assertions. The isolated contract surface passed, and ordinary metric/service M1
race surfaces are green.

The authoritative tracked-production census moved exactly as reviewed:

| Measurement | HEAD `c7ca5d0f` | M1 worktree | Delta |
|---|---:|---:|---:|
| Production owner files importing `internal/lifecyclejoin` | 1 | 0 | -1 |
| `lifecyclejoin.NewGeneration` | 1 | 0 | -1 |
| `Generation.Stop` | 1 | 0 | -1 |
| External `Generation.Cancel` | 1 | 0 | -1 |
| External `RunPartialStartRollback` calls | 1 | 0 | -1 |
| Final parent-aware `RollbackFailedStart` production owner calls | 31 | 32 | +1 |

The read-only sister-repository census found zero direct `metric.NewServer`, `service.NewMetrics`, or metrics-server
lifecycle callers. External direct consumers discover the source break at compile time and pass their existing
runtime and shutdown contexts. Configuration, endpoint paths, scrape behavior, and schemas are unchanged. SemStreams
made no sister-repository change.

Stable final M1 implementation and evidence identities are:

- `service/metrics.go`:
  `58987b4066057e2c0e2dd3be52036ee40e20d6079bc5913fadd8282c68295132`;
- `metric/handler.go`:
  `6053bdc41483ac0a5bc0b16a17b905e096b3b566abcdfa23beb0af6c366b8ad6`;
- `service/metrics_owner_test.go`:
  `a4230d7ab26bc2745c454d34d3863f1fe5dad114e6cd5ef1d1f6f069d8d90a34`;
- `metric/handler_test.go`:
  `5851cc90d663ea19bfaaf64134598bf3871297b4e0f0342fd7c3232464aac1aa`;
- `service/lifecycle_context_contract_test.go`:
  `e295b48b35218c1d0c9ebd7ef9f4c5f3b8bf9e3f82230190e18558cc7525463a`;
- `service/lifecycle_integration_test.go`:
  `0cd2032649e6b0741eb746c936f59168fadaed1d4300db73d5cc588a78690719`;
- `metric/doc.go`:
  `3457d31c269cf52a9faca7d5cd705d8ca2028421a35df21a9bcd9c78f2d0b0d0`;
- `metric/README.md`:
  `7d3aff2eae369112ecc68c7f6f2d7cc1084aa4b193f5f2a8b9bb104693ed69f3`;
- `docs/operations/migration-restart-safe-nats-client.md`:
  `430dec09f7916e9a72b320cd63296851ca4d92db2490f3a2dba6a2277d93143e`.

The M1 inventory is preserved byte-identical at SHA-256
`8a3b74786df6098aa053edd5c5c5e68f42f817ebd44008cdb75b8dece9eb2fc5`; it remains an untracked inventory artifact
and is not implementation credit. Temporary bridges keep the branch under the no-release/no-tag invariant. Task 2.3,
Gate A/B/C, N1, runtime migration, proof, release, archive, and tag readiness remain unchecked and incomplete.

### Pre-inventory N1 approval checkpoint — 2026-08-20

After M1 completed the 36th production owner migration, the owner approved a six-ruling N1 package: separate N1a
lifecyclejoin deletion; canonical exact-handle port methods and bridge removal; same-signature one-shot Subscription
Drain; a stateless durable handler and unaliased ConsumeDurable removal; Client child/name/OutstandingWork removal; and
five-field/schema removal with private exact-identity fixture cleanup.

That approval preceded the refreshed inventory gate and corrected design review. It remains historical evidence of
owner intent but is superseded for execution. In particular, it did not yet account completely for BackOff minimum
semantics, stable WARN observability, ADR-095 precedence, the binding gated-DAG durable-consume contract, every
preserved claim/observation authority, or complete downstream generated configuration impact.

This historical checkpoint marked no task and granted no implementation, Gate A/B/C, runtime-proof, release, archive,
or tag credit. It did not authorize SemStreams to edit sister repositories.

### Accepted N1 inventory and corrected-design checkpoint — 2026-08-20

The inventory-only artifact [`n1-convergence-inventory.md`](n1-convergence-inventory.md) was measured at baseline
`2f974bdb7f22efb39ac5136e9c0b719b711249c2`, has SHA-256
`2a95a0f5fd6683aeed585c8dca43d65ff662f32b2b046ce2262f6b97f74612e9`, and received independent verdict
`INVENTORY PASS`. It selects no target.

The corrected design records genuine do-nothing, incumbent-extension, and recommended options for six rulings. It
binds ADR-095 over ADR-094's superseded lifecycle mechanics and retains
`openspec/specs/gated-dag-dispatch/spec.md:43-77` as current durable-consume/heartbeat authority. It adds BackOff
minimum-positive validation, overflow-safe heartbeat arithmetic, stable WARN fields, separately preserved
claims/metrics/observation/internal-creator/readiness/inflight mechanisms, and complete local/downstream
configuration impact.

Independent review returned pre-owner `DESIGN APPROVE` with no findings for reviewed design SHA-256
`a9de5bd5cd86c484466eadee0947b8afe3d5dffb17249c2a8a48eeeba42faa0a`. The accepted N1 inventory identity remains
byte-identical. At this historical checkpoint owner reconfirmation of all six rulings was the sole design-authority
blocker; the working-system-first checkpoint below supersedes it. Tasks 2.1, 2.4, 2.5, 3.1, and 3.2 remained
unchecked. N1 implementation, Gate A/B/C, runtime-proof,
release, archive, and tag credit remain absent. N1a and N1b share the no-release/no-tag invariant.

### Working-system-first reset and N1a landed checkpoint — 2026-08-20

This checkpoint supersedes the six-ruling N1 execution target immediately above while preserving it as historical
evidence. The owner directed the project to restore a system that works well and can be understood, then decide from
evidence whether further improvement is needed. That direction approves the simplification reset; it does not approve
speculative `Subscription.Drain` semantics. Current Drain behavior and tests remain unchanged and deferred.

N1a landed as commit `8da1b83ae9c2f323bf484dc28e0574d81504bef9`
(`refactor(lifecycle): remove unused lifecyclejoin`) from baseline
`2f974bdb7f22efb39ac5136e9c0b719b711249c2`. Its exact scope is:

- deleted `internal/lifecyclejoin/generation.go` (baseline SHA prefix `2de9a3c`);
- deleted `internal/lifecyclejoin/generation_test.go` (`495c27f`);
- deleted `internal/lifecyclejoin/operation.go` (`afdc25e`);
- deleted `internal/lifecyclejoin/rollback.go` (`a814efe`); and
- replaced one diagnostic-only string in `processor/rule/lifecycle_owner_test.go`.

The commit contains 1 insertion and 749 deletions, net -748, with zero production additions. Production and test
imports, qualified symbols, and declarations are zero; the package directory is empty; and
`internal/lifecyclecleanup` has no diff. Independent implementation and merge review returned `APPROVE` with no
findings.

| Check | Result |
|---|---|
| Focused `processor/rule` race | PASS, 5.915s |
| Targeted ownership tests, ten race repetitions | PASS, 1.836s |
| `task lint` | PASS |
| `task build` and `go build ./...` | PASS |
| `git diff --check` | PASS |
| Strict change validation | PASS |
| All OpenSpec strict validation | PASS, 52/52 |

Repository-wide race and contract are not claimed green. User-owned `.claude/worktrees` scanner pollution, stale
natsclient census, and four stale testinfra rows reproduce on the clean baseline; candidate packages are green. This
is a baseline limitation, not broader N1 proof.

Protected evidence remained byte-identical: Metrics inventory SHA-256
`8a3b74786df6098aa053edd5c5c5e68f42f817ebd44008cdb75b8dece9eb2fc5`, N1 inventory SHA-256
`2a95a0f5fd6683aeed585c8dca43d65ff662f32b2b046ce2262f6b97f74612e9`.

N1a receives task 2.4 and its mechanical gate credit only. The remaining sequence is canonical exact-handle port
convergence; hidden Client catalog/name/OutstandingWork removal; five inert Go/schema field removals with private
exact-identity fixture cleanup; and `ConsumeDurable` replacement by stateless `NewDurableHandler` while preserving
`ConsumeWithHeartbeat` settlement, redelivery, and WARN behavior. The hard remaining budget is seven exports deleted,
one added (net -6), five fields/schema properties removed, catalogs/state deleted, and zero new lifecycle structs,
interfaces, maps, mutexes, goroutines, contexts, or configuration.

The existing Client-local `internalClaims` implementation remains unchanged: reject rather than replace, use an
opaque pointer token, release on precommit failure or exact native Closed, and retain no owner label. Canonical sealed
pre-Start validation and an error naming both owners move out of current N1 to deferred future improvements. This adds
no fifth boundary or claim state, and N1 does not claim complete ADR-095 conformance.

All remaining N1 tasks, Gate A/B/C, controlled/dirty proof, release, archive, and tag readiness remain unchecked. The
branch remains under the no-release/no-tag invariant.

### CM1 ComponentManager implementation checkpoint — 2026-08-19

Independent `semstreams-reviewer` verdict `APPROVE` applies to the CM1 dirty worktree based on full commit
`01ec3bed0be5a517065befcb878f4a90efbb14de`. Owner-migrated credit is granted only to
`service/component_manager.go`; supporting test files receive no separate owner credit.

The TDD RED sequence is recorded conservatively:

1. The initial callback-borrow fence test expected an error and received nil.
2. The health terminal-projection RED returned the child map instead of the required terminal projection.
3. The accepted corrections restored the focused ComponentManager lifecycle surface to green.

Final independent evidence:

| Evidence | Result | Elapsed |
|---|---|---:|
| Focused service race | PASS | 7.868s |
| CM lifecycle matrix, 10 repetitions | PASS | 6.740s |
| Integration ComponentManager/framework bucket | PASS | 8.950s |
| `gofmt` | PASS | — |
| `go vet` | PASS | — |
| `revive` | PASS | — |
| `git diff --check` | PASS | — |

The production-only recovery census moved exactly as reviewed:

| Measurement | HEAD `01ec3bed` | CM1 worktree | Delta |
|---|---:|---:|---:|
| Production owner files importing `internal/lifecyclejoin` | 25 | 24 | -1 |
| `lifecyclejoin.NewGeneration` | 25 | 23 | -2 |
| `Generation.Stop` | 33 | 32 | -1 |
| `Generation.StopWithQuiesce` | 5 | 3 | -2 |
| Final parent-aware `RollbackFailedStart` production owner calls | 11 | 12 | +1 |
| External `Generation.Cancel` | 4 | 4 | 0 |
| `lifecyclejoin.NewOperation` | 3 | 3 | 0 |
| External `RunPartialStartRollback` calls | 15 | 15 | 0 |

`git diff --quiet -- internal/lifecyclejoin` and `git diff --quiet -- natsclient` both returned success. The unrelated
Metrics inventory remained byte-identical at SHA-256
`8a3b74786df6098aa053edd5c5c5e68f42f817ebd44008cdb75b8dece9eb2fc5`.

Stable CM1 source identities for this checkpoint are:

- `service/component_manager.go`:
  `08a2a5bcb41fa393ee09e2043442982362dfdd1add6611d008e7894214dc76d0`;
- `service/component_manager_start_barrier_test.go`:
  `02f7227c468c9946deaa3930d7eb0a7c764fc0189708fd6d3767e6abc136a3dc`;
- `service/component_manager_storeregistry_test.go`:
  `f8956f5925587a52337ec9c796ccae2bfc3bf40ee1cca3ee2fdfb9a4d708f466`;
- `service/lifecycle_context_contract_test.go`:
  `b1434c3bafc475eefa9a519d261e02cc10e3b44e98ddc85d190a6dd4ebc87c25`;
- `service/component_manager_owner_test.go`:
  `4fb256e20a2cb9baf55a14ffe7373f8cd4c88a4cee5246e64507abdda135e9dc`.

This checkpoint grants no approval to unrelated exported API rulings. Task 2.3 and Gate A/B/C remain unchecked and
incomplete, and it grants no runtime-migration, proof, release, archive, or tag credit.

### ML1 MessageLogger implementation checkpoint — 2026-08-19

Independent `semstreams-reviewer` verdict `APPROVE` applies to the ML1 dirty worktree based on full commit
`c825f0e9e5736201e44dc22329e1cfb6e4a50c81`. Owner-migrated credit is granted only to
`service/message_logger.go`. Adjacent `service/message_logger_http.go` and test files are supporting
implementation/evidence surfaces and receive no separate owner credit. `service/message_logger_kv_watch.go` remains
unchanged; its SSE lifecycle remains request-owned.

The TDD RED sequence is recorded conservatively:

1. The initial one-shot test showed a completed second Stop replaying the first teardown error.
2. The three-test admission/Drain race run exposed double drain of one obsolete subscription, premature snapshot and
   drain after reconciliation-admission expiry, and duplicate claim of one obsolete subscription.
3. The Start/Stop commit-race test showed Stop completing first while Start later logged `MessageLogger started` and
   returned nil.
4. The accepted corrections restored the focused lifecycle surface to green without retained Stop-result replay.

Final independent evidence:

| Evidence | Result | Elapsed |
|---|---|---:|
| Full service race | PASS | 6.709s |
| MessageLogger lifecycle/HTTP race matrix, 10 repetitions | PASS | 7.146s |
| Real-NATS MessageLogger integration | PASS | 3.578s |
| `gofmt` | PASS | — |
| `go vet` | PASS | — |
| `revive` | PASS | — |
| `git diff --check` | PASS | — |
| Strict OpenSpec validation | PASS | — |

The production-only recovery census and HTTP-context search moved exactly as reviewed:

| Measurement | HEAD `c825f0e9` | ML1 worktree | Delta |
|---|---:|---:|---:|
| Production owner files importing `internal/lifecyclejoin` | 24 | 23 | -1 |
| `lifecyclejoin.NewGeneration` | 23 | 22 | -1 |
| `Generation.Stop` | 32 | 31 | -1 |
| MessageLogger HTTP KV invented roots | 2 | 0 | -2 |
| `Generation.StopWithQuiesce` | 3 | 3 | 0 |
| Final parent-aware `RollbackFailedStart` production owner calls | 12 | 12 | 0 |
| External `RunPartialStartRollback` calls | 15 | 15 | 0 |
| External `Generation.Cancel` | 4 | 4 | 0 |
| `lifecyclejoin.NewOperation` | 3 | 3 | 0 |

`git diff --quiet -- internal/lifecyclejoin`, `git diff --quiet -- natsclient`, and
`git diff --quiet -- service/message_logger_kv_watch.go` all returned success. The unrelated Metrics inventory remained
byte-identical at SHA-256 `8a3b74786df6098aa053edd5c5c5e68f42f817ebd44008cdb75b8dece9eb2fc5`.

Stable ML1 source identities for this checkpoint are:

- `service/message_logger.go`:
  `40710cb1de84ac543854e08f60445cd20dffb47b7c1bcd84c9aa951d52594316`;
- `service/message_logger_http.go`:
  `a2435cb9171f81474a008e33ab447f7932b4a21341250be84b75a2fe9ef267bc`;
- `service/message_logger_registry_test.go`:
  `a0ab2689eebf7ecb91f2a2df965622f3eebe2f81b81d4ca74b77ff59bdf87ba9`;
- `service/message_logger_subscription_integration_test.go`:
  `a1e351cb02ab4c5e579ea4fe6abf478d54f5a93aa517be46883ae8b0c01a8468`;
- `service/message_logger_http_kv_query_test.go`:
  `9698e4b63434254fb3c15e7baba9a9ad9d9764c6c0e0e75cb5addb05fd3582bb`;
- `service/message_logger_lifecycle_test.go`:
  `ded9c16bc12d854757f129602c7592f5f4fbb33f9551c2bf253e63470bef4674`.

This checkpoint grants no approval to unrelated exported API rulings. Task 2.3 and Gate A/B/C remain unchecked and
incomplete, and it grants no runtime-migration, proof, release, archive, or tag credit.

## Completion vocabulary

Use only these terms in status reports, PR descriptions, and handoffs:

- **Contract complete:** the approved design text is merged. This says nothing about production code.
- **Checkpoint measured:** counts were reproduced at an exact commit with the searches defined below.
- **Owner migrated:** one production owner has no prohibited lifecycle dependency, follows its classified native
  ownership order, and passes focused race tests. This does not complete a gate or the runtime migration.
- **Implementation gate complete:** every owner and test assigned to that gate passes its stated exit criteria at an
  exact commit. It does not imply restart proof or release readiness.
- **Runtime migration complete:** all implementation gates and all production and test-surface zero gates pass.
- **Proof complete:** required controlled-process, dirty-process, settlement, race, integration, and relevant E2E
  evidence is recorded against the exact candidate commit.
- **Spec complete:** runtime migration and proof are complete and every remaining task is truthfully checked.
- **Tag ready:** spec complete, archive gates pass, CI is green, downstream migration notes are current, and the
  candidate tag commit is the proven commit.

Do not report unqualified status such as “done,” “closed,” “migrated,” “green,” or “validated.”
Use a qualified meaning above and name its commit.

## Pinned recovery checkpoint

Recovery baseline: merged `main` at `9fcc841ee792a080a7b9998bfb51400cd81b24fe`.

These are commit-qualified Git object searches executed from a clean measuring worktree; `main` was clean. They do not
include or describe the separate dirty keyed-dispatcher experiment recorded under the workspace checkpoint.

| Measurement | Count |
|---|---:|
| Production owner files importing `internal/lifecyclejoin` | 41 |
| `lifecyclejoin.NewGeneration` | 44 |
| `Generation.Stop` | 49 |
| External `Generation.Cancel` | 10 |
| External `Generation.Signal` | 0 |
| `Generation.StopWithQuiesce` | 8 |
| `lifecyclejoin.NewOperation` | 3 |
| `Operation.Run` | 3 |
| External `RunPartialStartRollback` calls | 21 |

The historical 42-owner census at `63a733a2378dff9f09c74c461ba776d352f79221` remains immutable in
[`inventory.md`](inventory.md). These recovery counts are a later checkpoint, not a correction to that census.

The checkpoint was reproduced with these production-only searches:

```text
git grep -l 'internal/lifecyclejoin' 9fcc841ee792a080a7b9998bfb51400cd81b24fe -- '*.go' ':!*_test.go'
git grep -n 'lifecyclejoin.NewGeneration' 9fcc841ee792a080a7b9998bfb51400cd81b24fe -- '*.go' ':!*_test.go'
git grep -n -E '(generation|Generation)\.Stop\(' 9fcc841ee792a080a7b9998bfb51400cd81b24fe -- '*.go' ':!*_test.go'
git grep -n '\.StopWithQuiesce(' 9fcc841ee792a080a7b9998bfb51400cd81b24fe -- '*.go' ':!*_test.go'
git grep -n -E '(generation|Generation)\.Cancel\(\)' 9fcc841ee792a080a7b9998bfb51400cd81b24fe -- '*.go' ':!*_test.go'
git grep -n 'generation.Signal(' 9fcc841ee792a080a7b9998bfb51400cd81b24fe -- '*.go' ':!*_test.go'
git grep -n 'lifecyclejoin.NewOperation' 9fcc841ee792a080a7b9998bfb51400cd81b24fe -- '*.go' ':!*_test.go'
git grep -n -E '(shutdownOp|poolStop|stopOp)\.Run\(' 9fcc841ee792a080a7b9998bfb51400cd81b24fe -- '*.go' ':!*_test.go'
git grep -n 'lifecyclejoin.RunPartialStartRollback(' 9fcc841ee792a080a7b9998bfb51400cd81b24fe -- '*.go' ':!*_test.go'
```

Count complete output lines for each command; count unique lines for the owner-file search. The variable-name patterns
are a pinned-baseline reproduction aid, not a future-proof substitute for type-aware inspection.

### Measurement definitions

- **Production** means tracked `*.go` files excluding `*_test.go`, generated fixtures, vendored code, and archived
  OpenSpec artifacts.
- **Production owner file** means a production Go file importing `internal/lifecyclejoin`, counted once regardless of
  the number of symbols used.
- **External** means a call outside the defining `internal/lifecyclejoin` package. Tests are counted separately.
- **Generation.Stop** includes calls through variables or fields whose receiver is a `Generation`; the raw text count
  must be reconciled with type-aware inspection when naming differs.
- **Operation.Run** likewise includes every call on a lifecycle `Operation`, not unrelated methods named `Run`.
- **Rollback calls** count external uses of the old symbol. The approved bounded failed-Start invariant may retain an
  equivalent stateless helper, but the old package and symbol receive no exemption from the final zero gates.
- A checkpoint is valid only when it records the full commit, clean/dirty state, exact search commands, and results.
  Counts copied from a prior report are not a new checkpoint.

## Post-PR #997 merged-main checkpoint — 2026-08-18

Checkpoint measured on clean merged `main` at
`8117858367e1cc9d1dc434d211989e7a2ed1e552`. The measuring worktree had an empty porcelain status before this ledger
entry was added.

| Measurement | Count |
|---|---:|
| Production owner files importing `internal/lifecyclejoin` | 41 |
| `lifecyclejoin.NewGeneration` | 43 |
| `Generation.Stop` | 48 |
| External `Generation.Cancel` | 5 |
| External `Generation.Signal` | 0 |
| `Generation.StopWithQuiesce` | 8 |
| `lifecyclejoin.NewOperation` | 3 |
| `Operation.Run` | 3 |
| External `RunPartialStartRollback` calls | 20 |

These counts were reproduced from the merged commit with the same production-only searches defined by this ledger:

```text
git grep -l 'internal/lifecyclejoin' 8117858367e1cc9d1dc434d211989e7a2ed1e552 -- '*.go' ':!*_test.go'
git grep -n 'lifecyclejoin.NewGeneration' 8117858367e1cc9d1dc434d211989e7a2ed1e552 -- '*.go' ':!*_test.go'
git grep -n -E '(generation|Generation)\.Stop\(' 8117858367e1cc9d1dc434d211989e7a2ed1e552 -- '*.go' ':!*_test.go'
git grep -n '\.StopWithQuiesce(' 8117858367e1cc9d1dc434d211989e7a2ed1e552 -- '*.go' ':!*_test.go'
git grep -n -E '(generation|Generation)\.Cancel\(\)' 8117858367e1cc9d1dc434d211989e7a2ed1e552 -- '*.go' ':!*_test.go'
git grep -n 'generation.Signal(' 8117858367e1cc9d1dc434d211989e7a2ed1e552 -- '*.go' ':!*_test.go'
git grep -n 'lifecyclejoin.NewOperation' 8117858367e1cc9d1dc434d211989e7a2ed1e552 -- '*.go' ':!*_test.go'
git grep -n -E '(shutdownOp|poolStop|stopOp)\.Run\(' 8117858367e1cc9d1dc434d211989e7a2ed1e552 -- '*.go' ':!*_test.go'
git grep -n 'lifecyclejoin.RunPartialStartRollback(' 8117858367e1cc9d1dc434d211989e7a2ed1e552 -- '*.go' ':!*_test.go'
```

PR #997 removed zero production owner files and earns zero lifecycle-migration, proof, release, archive, or tag credit.
Lower helper-call counts without a lower production-owner count are not owner migration. The next authorized action is
implementation Gate A; this checkpoint introduces no design change or completion claim.

## Gate A two-input implementation worktree checkpoint — 2026-08-18

Checkpoint measured in a dirty implementation worktree based on full commit
`cd6f570ec9fc8e0fed43eabb2c353b4de36a6d29`. The runtime/test slice is limited to
`input/file/file.go`, `input/file/file_lifecycle_test.go`, `input/http/http.go`, and the new
`input/http/http_lifecycle_test.go`; this ledger and `tasks.md` are the only task-truth additions. This is not a clean
candidate-commit checkpoint and earns no Gate A, proof, release, archive, or tag completion credit.

The two architect-approved S-owner candidates replace `Generation` with one private owner-local cancel function and
the existing lifecycle mutex, running flag, and wait group. Start publishes cancel/join authority before launching
work. Stop consumes cancel once, cancels before the caller-context-bounded join, reports a deadline honestly, and
makes a completed repeated Stop nil/no-op without concurrent execution, rejoin, or result replay. Redundant shutdown
and done channels are absent. `input/file` no longer invokes `component.StandardLifecycleTests`; both owners instead
have focused deterministic Start/Stop, parent-cancellation, blocked-join deadline, and completed-repeat tests.

Stable worktree source identities for this review are:

- `input/file/file.go`: `cba90b467715767bedcd26b21d15b31f04b20ba37832447fd171fd809c75d802`
- `input/file/file_lifecycle_test.go`: `8d55ae618e8abc9319d99aeb3e90b6abfd243c0538010017471646d44d2491db`
- `input/http/http.go`: `914a4171ac3011fa98ca7ca4f70db36f5b4563497113f17a35d5389bd15ed86b`
- `input/http/http_lifecycle_test.go`: `67c78b1e2970272880efd126bd25389c34d1f02b550dcbf8125bfce005db78ed`

| Ruling | Evidence | Checkpoint result |
|---|---|---|
| Owner-local allowed state only | `A01` | Independently approved for this slice. |
| Start publishes authority before launch | `A02` | Independently approved for this slice. |
| One-shot cancel precedes caller-bounded join | `A03` | Independently approved for this slice. |
| Honest timeout, no rejoin, completed repeat nil | `A04` | Independently approved for this slice. |
| Prohibited mechanisms absent | `Z01` | Independently approved for this slice. |
| Focused behavior and race tests | `T01` | Independently approved for this slice. |
| Required census movement | `C01` | Independently approved delta; no gate credit. |
| Adjacent outputs remain out of slice | `U01` | Approved Q-primary/F classification; zero credit. |

Independent `semstreams-reviewer` verdict on 2026-08-18: `CORRECTIONS CONFIRMED`. The verdict approves `A01`-`A04`,
`Z01`, `T01`, `C01`, and `U01` for this exact two-owner slice and the stable source identities above. It does not
approve Gate A or any broader runtime-migration or proof claim.

Exact anchor definitions:

- `A01`: owner state at `input/file/file.go:104-110` and `input/http/http.go:78-83` is limited to the lifecycle
  mutex, private cancel function, running flag, and WaitGroup; no context is stored.
- `A02`: `input/file/file.go:379-401` and `input/http/http.go:251-272` validate Start context, derive the run context,
  and publish cancel, running, and WaitGroup ownership before launching work.
- `A03`: `input/file/file.go:408-437` and `input/http/http.go:284-313` consume and clear cancel once under the
  lifecycle mutex, cancel before waiting, and bound only the join with the caller context.
- `A04`: production repeat/timeout branches are `input/file/file.go:413-436` and `input/http/http.go:289-312`.
  Behavior proof is `input/file/file_lifecycle_test.go:65-82,98-143` and
  `input/http/http_lifecycle_test.go:57-83,106-132`.
- `T01`: focused behavior proof spans `input/file/file_lifecycle_test.go:65-143` and
  `input/http/http_lifecycle_test.go:57-132`; the focused race command and result are recorded below.

The compact search evidence for the table is:

```text
Z01:
! git grep -n -E \
  -e 'internal/lifecyclejoin|lifecyclejoin\.|Generation|StopWithQuiesce' \
  -e 'NewOperation|RunPartialStartRollback|shutdown[[:space:]]+chan' \
  -e 'done[[:space:]]+chan|\.shutdown|\.done|rejoin' \
  -- input/file/file.go input/http/http.go
result: no matches

C01:
git grep -l 'internal/lifecyclejoin' -- '*.go' ':!*_test.go' | wc -l
=> 39
git grep -n 'lifecyclejoin.NewGeneration' -- '*.go' ':!*_test.go' | wc -l
=> 41
git grep -n -E '(generation|Generation)\.Stop\(' -- '*.go' ':!*_test.go' | wc -l
=> 46

U01: git diff --quiet -- output/file/file.go output/httppost/httppost.go
result: exit 0; both output files untouched
```

No deviation is recorded for this two-owner slice. The independently approved dirty-worktree findings grant
owner-migrated credit to `input/file` and `input/http` only; they do not complete Gate A.

Focused race evidence from this worktree:

```text
go test -race ./input/file ./input/http -count=1 -timeout=120s
ok github.com/c360studio/semstreams/input/file 1.302s
ok github.com/c360studio/semstreams/input/http 1.432s
```

The production-only recovery searches defined above report this exact delta from the post-PR #997 checkpoint:

| Measurement | Post-PR #997 | This worktree | Delta |
|---|---:|---:|---:|
| Production owner files importing `internal/lifecyclejoin` | 41 | 39 | -2 |
| `lifecyclejoin.NewGeneration` | 43 | 41 | -2 |
| `Generation.Stop` | 48 | 46 | -2 |
| External `Generation.Cancel` | 5 | 5 | 0 |
| External `Generation.Signal` | 0 | 0 | 0 |
| `Generation.StopWithQuiesce` | 8 | 8 | 0 |
| `lifecyclejoin.NewOperation` | 3 | 3 | 0 |
| `Operation.Run` | 3 | 3 | 0 |
| External `RunPartialStartRollback` calls | 20 | 20 | 0 |

Owner-migrated credit is granted to `input/file` and `input/http` only. Gate A remains incomplete because its remaining
assigned owners and gate-wide exit criteria are not complete. Runtime migration, proof, release, archive, and tag
completion remain incomplete.

The current architect reclassification is binding for the adjacent output owners and changes no historical inventory:

- `output/file/file.go` is Q-primary with an F facet, not S, pending exact native subscription/consumer handles,
  protocol-specific callback-admission ordering, and retained partial-Start cleanup authority.
- `output/httppost/httppost.go` is Q-primary with an F facet, not S, pending exact native subscription/consumer
  handles, protocol-specific callback-admission ordering, and retained partial-Start cleanup authority.

Neither output owner is edited or receives migration credit in this checkpoint.

## Graph-index parent and dispatcher implementation worktree checkpoint — 2026-08-18

Checkpoint measured in a dirty implementation worktree based on merged `main` at full commit
`e7789f6cf5714e5b5fb04c0221cb9b2def17d3a0`. The runtime/test slice is limited to the graph-index parent and its
private keyed dispatcher: `processor/graph-index/component.go`, `processor/graph-index/keyed_dispatcher.go`, focused
lifecycle proof in `processor/graph-index/lifecycle_order_test.go`, and the three existing dispatcher test-cleanup
call sites in `processor/graph-index/ordered_dispatch_test.go` and
`processor/graph-index/owner_filter_load_integration_test.go`. This ledger and `tasks.md` are the only task-truth
additions. This is not a clean candidate-commit checkpoint and earns no Gate A, proof, release, archive, or tag
completion credit.

Stable worktree source identities for this checkpoint are:

- `processor/graph-index/component.go`: `01d83a26fc2affc4136da86d60a5bb31a49e070716ba3d8ac6a8dca288711f5e`
- `processor/graph-index/keyed_dispatcher.go`: `45added4af11d00c1a4eae158c612f868648e3e0f54f802b169060468503ede1`
- `processor/graph-index/lifecycle_order_test.go`: `1689efb74f06d970b91b29b8a3b4355e2b4ace04829ced2f4a3cadaba5acad1f`
- `processor/graph-index/ordered_dispatch_test.go`: `0a337acf0e8ae9aab274acf8220c8a83a77bc93ca8e3a4d819077a77bcc0c7dc`
- `processor/graph-index/owner_filter_load_integration_test.go`:
  `3d3e1fb464bdd09a502a4b140ea464b6b7eec5623d273a2f346cd9655ffb4711`

| Ruling | Exact implementation evidence | Checkpoint result |
|---|---|---|
| Parent adds only failed-Start phase state | `processor/graph-index/component.go:241-253` | Existing lifecycle/resource fields remain; only private `cleanupPending` was added and no context is stored. |
| Authority precedes escaping work | `processor/graph-index/component.go:608-650,698-712` | Cancel/runDone/cleanupPending publish first; the one runDone waiter starts after all Add sites are sealed. |
| Failed Start uses bounded exact cleanup | `processor/graph-index/component.go:627-651,781-832` | Exact subscriptions drain before cancel; runDone, coalescer.done, and pool.done are awaited under the terminal bound. |
| Failed-Start authority alone is retryable | `processor/graph-index/component.go:589-595,725-752` | Start rejects retained cleanup; later Stop clears authority only after complete cleanup. |
| Normal Stop remains one-shot | `processor/graph-index/component.go:753-779` | Cancel authority is claimed once; timeout is honest and later normal Stop is a no-op. |
| Dispatcher is a parent-owned child | `processor/graph-index/keyed_dispatcher.go:12-74` | Generation and independent Stop are absent; parent cancellation plus exact done replaces them. |
| Channel-synchronized behavior proof | `processor/graph-index/lifecycle_order_test.go:127-214` | Failed cleanup retention/retry and dispatcher-child join are proved without sleep-based synchronization. |
| Existing dispatcher cleanup follows target | `processor/graph-index/ordered_dispatch_test.go:46-59,378-403`; `processor/graph-index/owner_filter_load_integration_test.go:392-397` | Tests cancel their exact parent context and await private done. |
| Census moves by the reviewed owner count | searches and table below | Dispatcher receives owner-migrated credit; the already lifecyclejoin-free parent receives none. |

Focused and package race evidence from this worktree:

```text
go test -race ./processor/graph-index -run \
  'TestComponent(StartFailure|FailedStart|Stop)|TestKeyedDispatcher' -count=1 -timeout=120s
ok github.com/c360studio/semstreams/processor/graph-index 1.341s

go test -race ./processor/graph-index -count=1 -timeout=120s
ok github.com/c360studio/semstreams/processor/graph-index 1.798s
```

The production-only recovery searches defined above report this exact delta from merged baseline `e7789f6c`:

| Measurement | Merged baseline | This worktree | Delta |
|---|---:|---:|---:|
| Production owner files importing `internal/lifecyclejoin` | 39 | 38 | -1 |
| `lifecyclejoin.NewGeneration` | 41 | 40 | -1 |
| `Generation.Stop` | 46 | 45 | -1 |
| External `Generation.Cancel` | 5 | 5 | 0 |
| External `Generation.Signal` | 0 | 0 | 0 |
| `Generation.StopWithQuiesce` | 8 | 8 | 0 |
| `lifecyclejoin.NewOperation` | 3 | 3 | 0 |
| `Operation.Run` | 3 | 3 | 0 |
| External `RunPartialStartRollback` calls | 20 | 20 | 0 |

Owner-migrated credit is granted to `processor/graph-index/keyed_dispatcher.go` only. The graph-index parent already
had no production `internal/lifecyclejoin` import at the merged baseline, so its failed-Start correction receives no
owner-count credit. Task 2.3, Gate A, runtime migration, proof, release, archive, and tag completion remain incomplete.

## Dispatcher Stop-error prerequisite worktree checkpoint — 2026-08-18

Dirty worktree baseline: merged `main` `0f7687a7f371be7e937b70fe27d2a1e9f5587eba`. Scope is limited to
`pkg/dispatch/{dispatcher.go,completion_watcher.go,doc.go}` and focused tests. This prerequisite corrects the exact
`BoundedDispatcher` handle needed by later owner migrations and grants zero owner, Gate, proof, release, archive, or
tag credit.

| Ruling | Exact evidence | Result |
|---|---|---|
| Nil Stop context rejects before action | `pkg/dispatch/dispatcher.go:196-199`; `pkg/dispatch/dispatcher_test.go:272-285` | Pool remains usable after rejection. |
| Finished caller context receives zero pool budget | `pkg/dispatch/dispatcher.go:200-230`; `pkg/dispatch/dispatcher_test.go:287-359` | Both canceled and expired contexts return promptly. |
| Pool and caller failures are returned | `pkg/dispatch/dispatcher.go:231-239`; `pkg/dispatch/dispatcher_test.go:287-359` | Error matches `worker.ErrStopTimeout` and the exact context cause. |
| Completion watcher is canceled and bounded | `pkg/dispatch/completion_watcher.go:180-195`; `pkg/dispatch/completion_watcher_test.go:84-132` | Unobserved callback join returns the caller context error. |
| Failed Stop is terminal; completed repeat is nil | `pkg/dispatch/dispatcher.go:23-26,183-195`; `pkg/dispatch/doc.go:94-102`; `pkg/dispatch/dispatcher_test.go:259-269` | No retry/rejoin/result state was added. |

Evidence:

```text
go test -race ./pkg/dispatch -run 'TestDispatcher_Stop|TestCompletionWatcherStop' -count=1 -timeout=120s
ok github.com/c360studio/semstreams/pkg/dispatch 1.446s

go test -race ./pkg/dispatch -count=1 -timeout=120s
ok github.com/c360studio/semstreams/pkg/dispatch 1.850s

go test -tags=integration -race ./pkg/dispatch -count=1 -timeout=120s
ok github.com/c360studio/semstreams/pkg/dispatch 4.847s

task lint
PASS
```

The real-NATS integration gate proves the existing successful completion-watcher path still joins. It does not claim
a real-NATS pool-timeout injection; channel-synchronized unit proof covers the failed boundary. Production lifecycle
counts remain unchanged from the baseline: owner files 38, NewGeneration 40, Generation.Stop 45, external Cancel 5,
StopWithQuiesce 8, NewOperation 3, Operation.Run 3, and old rollback calls 20.

## Gated-DAG one-shot owner worktree checkpoint — 2026-08-18

Dirty worktree baseline: merged `main` `a1a68a784d6f82c5f6cbfb81fe81c86945eeda67`. Runtime/test scope is limited
to `processor/gated-dag/{executor.go,component.go,executor_lifecycle_test.go,executor_integration_test.go}`; this ledger
and `tasks.md` are the only task-truth additions. This checkpoint grants owner-migrated credit only to
`processor/gated-dag/executor.go`; Gate A, runtime migration, proof, release, archive, and tag completion remain
unchecked and incomplete.

Stable worktree source identities:

- `processor/gated-dag/executor.go`: `b772ebc214679ab8a688b833b17428e1566fae2c83a85058a5aad9c57eded28e`
- `processor/gated-dag/component.go`: `1b867b93425165b4c7f6a2540fa38ede92465876a182da115b4b1be2e3a5062f`
- `processor/gated-dag/executor_lifecycle_test.go`:
  `2ab86ca28c0d78cc4bddaf47d5edd0ab1fade8d89093529e7757bc2a6d93e747`
- `processor/gated-dag/executor_integration_test.go`:
  `90f911716bea721046bc60d6a177319212cc143735c92fdaa8fa97dae20d352a`

| Ruling | Exact implementation evidence | Checkpoint result |
|---|---|---|
| Owner-local lifecycle only | `processor/gated-dag/executor.go:71-73,188-194` | `Generation` is replaced by one private cancel, one done channel, and the existing WaitGroup; no context is stored. |
| Exact dispatcher and failed-Watch rollback | `processor/gated-dag/executor.go:95-123`; `processor/gated-dag/executor_lifecycle_test.go:90-104` | The exact dispatcher is retained; failed lifecycle-Watch acquisition returns the joined Watch and dispatcher rollback result. |
| Goroutine-local KV watcher | `processor/gated-dag/executor.go:374-407` | The exact watcher stays local to its goroutine and LIFO defers stop it before WaitGroup completion; no watcher catalog or field is added. |
| One-shot Stop order | `processor/gated-dag/executor.go:207-227`; `processor/gated-dag/executor_lifecycle_test.go:14-40` | Stop consumes cancel once, cancels, stops the dispatcher, and bounds the existing done join; later executor Stop is nil/no-op. |
| Boot-only parent and honest failed Stop | `processor/gated-dag/component.go:136-149,272-274,297-320`; `processor/gated-dag/executor_lifecycle_test.go:42-88` | The successful executor pointer is retained as used-instance evidence; a claimed failed Stop cannot become later Component success or healthy runtime. |
| Fresh-component restart proof | `processor/gated-dag/executor_integration_test.go:189-218` | Same-instance restart rejects; a fresh Component boots against retained NATS and reconciles authoritative state. |
| Touched sleep removal | `processor/gated-dag/executor_integration_test.go:34-89,146-184` | Graph-ingest readiness is observed through request/reply and dedup is driven by explicit passes/submission counts; the touched file has no `time.Sleep`. |

TDD red evidence:

```text
go test -race ./processor/gated-dag -run \
  'TestExecutorStop|TestComponent(FailedStop|CompletedStop|RejectsSameInstance)|TestExecutorStartWatchFailure' \
  -count=1 -timeout=120s
=> build failed: executor had no owner-local cancel/done fields

go test -tags=integration -race ./processor/gated-dag -run TestIntegration_BootReconcile -count=1 -timeout=120s
=> failed: the old test attempted same-instance Start after completed Stop and received the required boot-only rejection
```

Green evidence:

```text
go test -race ./processor/gated-dag -count=1 -timeout=120s
ok github.com/c360studio/semstreams/processor/gated-dag 1.348s

go test -tags=integration -race ./processor/gated-dag -count=1 -timeout=180s
ok github.com/c360studio/semstreams/processor/gated-dag 8.869s
```

The production-only recovery searches report this exact delta from baseline `a1a68a78`:

| Measurement | Baseline | Worktree | Delta |
|---|---:|---:|---:|
| Production owner files importing `internal/lifecyclejoin` | 38 | 37 | -1 |
| `lifecyclejoin.NewGeneration` | 40 | 39 | -1 |
| `Generation.Stop` | 45 | 44 | -1 |
| External `Generation.Cancel` | 5 | 4 | -1 |
| External `Generation.Signal` | 0 | 0 | 0 |
| `Generation.StopWithQuiesce` | 8 | 8 | 0 |
| `lifecyclejoin.NewOperation` | 3 | 3 | 0 |
| `Operation.Run` | 3 | 3 | 0 |
| External `RunPartialStartRollback` calls | 20 | 20 | 0 |

## Gate A BaseService dirty-worktree checkpoint — 2026-08-19

This dirty implementation checkpoint is based on clean merged `main`
`c5953972bbc56f013bf4665674e99f03c11395f6`. The owner-selected handoff is
[`base-service-owner-slice.md`](base-service-owner-slice.md), SHA-256
`663094c67f65b444b2539ea9861cd51dd60f692b83eedb67817db68172c3f114`. The handoff limits production credit to
`service/base.go` and includes only BaseService-specific test correction propagation.

Exact reviewed file hashes:

- `service/base.go`: `470cddafe9ffbd5d48f655ca05ee178e0228fe3cafd437433435ac729d202856`
- `service/base_lifecycle_test.go`: `8b37f82a66a33e44c0cad2cfcac879c2657628a9f113d5dc00051f2d8cfad7ca`
- `service/base_test.go`: `44e283023ba248eacf09bf26be942397c9ea71b5a9b42f10623a72d78dbb4d5d`
- `service/lifecycle_context_contract_test.go`:
  `ce575266e5ba10a6dc3a7bc492e754171bb7b78e7053dbe8965e9cb64c19cfcb`
- `service/service_manager_stopall_test.go`:
  `761e9c06733a68c0c538918b98a3c1ecfcd6c8ec5eec95c10a6a6bc2307465e9`
- `openspec/changes/simplify-one-shot-lifecycle-ownership/base-service-owner-slice.md`:
  `663094c67f65b444b2539ea9861cd51dd60f692b83eedb67817db68172c3f114`

The final independent `semstreams-reviewer` verdict is `APPROVE` with no findings. Its conformance rulings are:

- `service/base.go:111-114,254-291` replaces `Generation` and retained terminal results with private cancel,
  done, and WaitGroup authority; no context is retained, and authority is published before owned goroutines escape.
- `service/base.go:232-247` rejects nil, already-canceled, and same-instance reused Start before starting new work.
- `service/base.go:296-349` makes Stop one-shot: it consumes cancel, stops ticker admission, cancels before the exact
  caller-context-bounded join, returns timeout honestly, and gives later calls no rejoin or result-replay path.
- `service/base.go:281-290,460-478` publishes `StatusStopped` only after both owned goroutines return; parent
  cancellation converges through that same exact owner join.
- `service/base_lifecycle_test.go:11-134` deterministically covers canceled Start, canceled/deadline Stop, no rejoin,
  same-instance rejection, Stop-before-Start terminal use, parent cancellation, and owner completion.
- `service/base_test.go:291-329` proves restart by fresh composition rather than same-instance generation reuse.
- `service/lifecycle_context_contract_test.go:55-68,89-101` retains nil-context and cancel-before-wait contract coverage
  while removing tests that required shared Stop completion or later rejoin.
- `service/service_manager_stopall_test.go:27-29,125-136` corrects wording to exact completion and proves completed
  repeated BaseService Stop without treating `StatusStopping` as completion. No Manager behavior changed.

The developer supplied the following historical TDD red transcript. The reviewer found the failures consistent with
the final change but marked TDD history `UNVERIFIED` because no in-tree or CI artifact preserves the runs. Record it
as qualified execution history, not durable proof.

```text
go test -race ./service -run \
  '^TestBaseService(StartRejectsCanceledContext|StopIsOneShotAfterCanceledJoin|'\
'CompletedStopRejectsSameInstanceRestart)$' \
  -count=1 -timeout=20s
=> FAIL: canceled Start wanted context.Canceled, got nil; repeated Stop timed out after 1s while rejoining blocked
   work; same-instance restart wanted an error, got nil; package failed in 1.509s

go test -race ./service -run '^TestBaseServiceStopIsOneShotAfterFailedJoin$' -count=1 -timeout=20s
=> FAIL: canceled and deadline subtests wanted StatusStopping (3), got StatusStopped (0):
   "repeat Stop must not predict owner completion"; package failed in 0.513s

go test -race ./service -run '^TestBaseServiceStopBeforeStartRejectsSameInstanceStart$' -count=1 -timeout=20s
=> FAIL: base_lifecycle_test.go:117 wanted an error, got nil; package failed in 0.500s
```

Final green evidence after the reviewer-required corrections:

```text
# Independent root/Program Manager verification, before the final comment-only correction
go test -race ./service -count=1 -timeout=180s
=> ok github.com/c360studio/semstreams/service 7.045s

go test -tags=integration -race ./service -run '^TestService_FreshInstanceRestart$' -count=1 -timeout=120s
=> ok github.com/c360studio/semstreams/service 3.599s

task lint
=> PASS

openspec validate simplify-one-shot-lifecycle-ownership --strict --no-interactive
=> Change 'simplify-one-shot-lifecycle-ownership' is valid

git diff --check
=> PASS

# Developer rerun after the final comment-only correction
go test -race ./service -count=1 -timeout=180s
=> ok github.com/c360studio/semstreams/service 6.703s

go test -race ./service -run \
  '^(TestServiceManager_StopAll_Idempotency|TestBaseService_CompletedStopIsIdempotent)$' \
  -count=1 -timeout=30s
=> ok github.com/c360studio/semstreams/service 1.472s

git diff --check
=> PASS
```

The production-only recovery census has this exact baseline-to-worktree delta:

| Measurement | Baseline | Worktree | Delta |
|---|---:|---:|---:|
| Production owner files importing `internal/lifecyclejoin` | 37 | 36 | -1 |
| `lifecyclejoin.NewGeneration` | 39 | 38 | -1 |
| `Generation.Stop` | 44 | 43 | -1 |
| External `Generation.Cancel` | 4 | 4 | 0 |
| External `Generation.Signal` | 0 | 0 | 0 |
| `Generation.StopWithQuiesce` | 8 | 8 | 0 |
| `lifecyclejoin.NewOperation` | 3 | 3 | 0 |
| `Operation.Run` | 3 | 3 | 0 |
| External `RunPartialStartRollback` calls | 20 | 20 | 0 |

Owner-migrated credit is limited to `service/base.go`. This remains a dirty-worktree checkpoint: task 2.3 and Gate A
remain incomplete, and it grants no controlled/dirty proof, release, archive, or tag credit.

## PR #1001 integration-runner interrupt prerequisite inventory — 2026-08-19

The repeated CI failure in `TestIntegrationRunner_InterruptReapsPullBeforeReleasingLock` is inventoried at
[`testinfra-interrupt-inventory.md`](testinfra-interrupt-inventory.md), baseline
`6d9d754af2f13d0f09145ed34ce81f3d8b013885`, SHA-256
`b183233d955680d4f00fcb8749b7a4b09370fa24479f79d476f8427093024737`. This is an inventory-only testinfra
prerequisite. Independent review returned `INVENTORY PASS`. The narrowed D1 target is recorded at
[`testinfra-interrupt-design.md`](testinfra-interrupt-design.md), SHA-256
`c0f8754b241741c45523b00b2007a668e16f38b676a1c31138f960f766a3e764`. It grants zero lifecycle migration,
restart-proof, release, archive, or tag credit. Independent review returned `DESIGN PASS`, and the owner approved the
narrowed D1 implementation against that exact design hash.

Dirty implementation checkpoint at baseline `6d9d754af2f13d0f09145ed34ce81f3d8b013885` is limited to
`test/testinfra/integration_runner_contract_test.go`, SHA-256
`81b04d4bca469dfb6df79d221afd9a0b4fc03f3d51379e4f40814d6a950d995f`. The inventory, design, and this ledger are the
only test-truth additions. No production script, environment contract, exported API, Docker resource, or sister
repository changed.

| D1 ruling | Exact evidence | Checkpoint result |
|---|---|---|
| Stable termination case and causal helper | `test/testinfra/integration_runner_contract_test.go:206-245,355-399,688-691` | The renamed case re-execs only its fake pull. The helper installs `SIGTERM` notification before publishing its PID and gives the inherited release pipe one reader: pre-TERM release or EOF exits successfully; after TERM, the helper acknowledges TERM and joins that same release read before exit. |
| Parent-ready follows Bash PID retention | `test/testinfra/integration_runner_contract_test.go:228-234,263-276,301-306` | The private date wrapper signals only after the helper PID marker exists; this runner call occurs after Bash retained `$!`. |
| Exact four-pipe ownership | `test/testinfra/integration_runner_contract_test.go:247-299,401-465` | The runner case passes parent-ready, TERM-ack, release, and reap-check endpoints. Its waiter and release-first cleanup register immediately after successful Start, before fallible inherited-endpoint closes. The direct pre-TERM case uses only two `/dev/null` descriptor placeholders and the release pipe. |
| Lock remains while acknowledged child is blocked | `test/testinfra/integration_runner_contract_test.go:307-325` | The runner receives `SIGTERM`; exact lock-owner bytes remain unchanged, `kill -0` proves the PID exists, and the private `rmdir` mutation probe refuses removal. |
| Reap precedes lock removal | `test/testinfra/integration_runner_contract_test.go:235-245,327-352` | The wrapper refuses exact lock removal while the PID exists, including zombie state; only PID absence emits reap acknowledgement and delegates real `rmdir`. |
| Actual exit, never timeout-as-success | `test/testinfra/integration_runner_contract_test.go:333-348,441-464,779-830` | The sole waiter returns an actual `*exec.ExitError` code 130 with exited `ProcessState`; typed timeout is a hard failure and no second `Cmd.Wait` exists. The pre-TERM case exact-matches the helper PID and proves it is absent after Wait. |
| Early cleanup converges on the same waiter | `test/testinfra/integration_runner_contract_test.go:280-299,438-450,795-830` | Cleanup is registered before any post-Start failure can escape, releases the helper first, kills the runner only while still live, and joins the existing waiter. |
| Adjacent contracts remain intact | `test/testinfra/integration_runner_contract_test.go:401-490,519-574` | The pre-TERM release regression, typed timeout result, and holder's bounded contention/release behavior use the same single-Wait ownership contract. |

TDD red evidence:

```text
go test -race ./test/testinfra -run TestIntegrationRunner_TerminationReapsPullBeforeReleasingLock \
  -count=1 -timeout=30s
=> FAIL after 3.02s: wait for parent retained pull PID: i/o timeout
```

The failure is the intended missing causal milestone before the helper/date producer was wired. It is not accepted as
runner completion.

Correction red evidence after independent review found that early cleanup could close release before TERM:

```text
go test -race ./test/testinfra -run '^TestIntegrationRunnerFakePullHelper_PreTERMReleaseExits$' \
  -count=1 -timeout=20s
=> FAIL after 3.01s: helper did not accept pre-TERM release: command did not exit within 3s
```

The helper previously waited for TERM before reading release, so EOF could not converge early cleanup. The corrected
helper causally waits for either event and, if TERM wins, joins the same release read after acknowledging TERM.

Green focused evidence:

```text
go test -race ./test/testinfra -run \
  '^(TestIntegrationRunner_TerminationReapsPullBeforeReleasingLock|TestIntegrationRunnerFakePullHelper_PreTERMReleaseExits)$' \
  -count=20 -timeout=180s
PASS: both causal cases passed 20/20 under the race detector

go test -race ./test/testinfra -run \
  'TestIntegrationRunner_HostLockHasBoundedContentionDiagnostics|TestCommandWaiter' -count=1 -timeout=60s
ok github.com/c360studio/semstreams/test/testinfra 2.555s

go test -race ./test/testinfra -skip '^TestInfrastructurePolicyGuard$' -count=1 -timeout=120s
ok github.com/c360studio/semstreams/test/testinfra 6.964s

go test -race ./test/testinfra -count=1 -timeout=120s
LOCAL GATE NOT GREEN: TestInfrastructurePolicyGuard reported 742 findings under two excluded, user-owned
`.claude/worktrees/agent-*` trees. No reported finding points to this test file. The worktrees were not mutated or
removed; an isolated checkout/CI must run the full package gate.

task lint
PASS

openspec validate simplify-one-shot-lifecycle-ownership --strict --no-interactive
Change 'simplify-one-shot-lifecycle-ownership' is valid

git diff --check
PASS
```

The twenty termination runs each completed actual runner Wait, exit-code/state validation, post-reap acknowledgement,
PID absence, and lock absence. The twenty pre-TERM runs each completed the exact helper Wait and PID-absence proof.
Test cleanup left no owned process or FD endpoint. This prerequisite receives zero owner, lifecycle, Gate,
runtime-proof, release, archive, or tag credit. The full-package gate remains explicitly unverified until run without
the excluded user-owned worktrees in the repository scan root.

## Workspace recovery checkpoint

Authority collisions are inventoried in
[`authority-reconciliation-inventory.md`](authority-reconciliation-inventory.md) (SHA-256
`d495b4acb908bb846194eec0ce9c97076691bf3cba5d3143b8c2453335792f4e`); inventory only, with no completion credit.
The inventory-first reconciliation design is recorded in
[`authority-reconciliation-design.md`](authority-reconciliation-design.md) (SHA-256
`9af0ceeacae2dc0a854b0337f5eb6dd19710a90fde586944e7f9bca0b06df039`); design only, with no runtime or proof credit.
Applying the approved authority reconciliation changes no runtime-migration or proof completion credit.

PR #990 truth-reset inventory revision 1 was superseded before its first commit; its bytes are not a durable artifact.
This ledger retains only SHA-256 `a3e83b5843381b6d69183c959b0497be7c6d0f0d4538aade5f6604d637e69817`, measured from current-main baseline
`eb1f6d7758f75a2ff5598e2ca92af92e8c21d753` against historical PR head
`8f19ef3678a549913385b090e4de1766a7a43a27`, independent `semstreams-reviewer` verdict
`INVENTORY CHANGES REQUESTED`, and the zero implementation, lifecycle, proof, or release-credit outcome.

PR #990 truth-reset inventory revision 2:
[`pr990-truth-reset-inventory.md`](../require-restart-for-config-activation/pr990-truth-reset-inventory.md),
measured from the same baseline and historical head. SHA-256:
`5256057932030c7e854a3889ae2756fbec577870ee5e5c9c7c0e8ab86874541d`. Independent
`semstreams-reviewer` verdict: `INVENTORY PASS`. Revision 2
supersedes revision 1 only as the inventory submitted for re-review. PR #990
continues to receive zero implementation, lifecycle, proof, or release credit.

PR #990 binding boot-only disposition:
[`pr990-boot-only-disposition.md`](../require-restart-for-config-activation/pr990-boot-only-disposition.md), SHA-256
`40b2534b604a14f64aacbb8f4db86bdbc38129f3f114e0ac40118c9f7259fc41`, disposition baseline
`42f349b02bfa9517cff575a9c2a1af3094e591ce`, historical PR head
`8f19ef3678a549913385b090e4de1766a7a43a27`. Owner binding disposition: reject historical PR #990 as a merge,
rebase, commit-replay, or cherry-pick unit; permit only the recorded narrow reconstruction. This checkpoint grants
zero implementation, lifecycle, proof, release, archive, or tag credit.

The authoritative eight-worktree evidence, binding classifications, bounded preservation sets, and exact cleanup
scope are recorded in [`worktree-recovery-manifest.md`](worktree-recovery-manifest.md). No dirty lifecycle worktree may
be removed until that manifest is merged and its two bounded preservation artifacts are verified.

Known worktrees at approval time:

- **Draft PR #990:** `/private/tmp/semstreams-gh986-boot-only-flow-activation`, branch
  `codex/gh986-boot-only-flow-activation`, clean at `8f19ef36`.
- **Rejected cleanup experiment:** `/private/tmp/semstreams-generation-removal-1`, branch
  `codex/remove-generation-leaf-owners`, with uncommitted edits to
  `processor/graph-index/keyed_dispatcher.go` and a new lifecycle test.
- **Earlier rejected experiments:** separate legacy dirty worktrees. Inventory each before cleanup; none is
  implementation authority.

Workspace cleanup is ordered and evidence-preserving:

1. Enumerate every worktree with its path, branch or detached state, HEAD, porcelain status, changed-file list, and
   diff stat. Do not infer that two similarly named worktrees contain the same experiment.
2. For every dirty worktree, record a patch SHA-256 and classification: accepted, superseded, rejected, or unknown.
   Preserve the manifest before removing anything.
3. Treat the keyed-dispatcher experiment as rejected. It must not be committed, copied forward, or expanded into a
   generic admission/shutdown mechanism.
4. Remove rejected worktrees only after their manifest is durable. Do not touch unrelated Docker containers, sister
   repositories, or the clean PR #990 worktree.
5. Finish with one clean `main`, the clean PR #990 worktree, and one explicitly approved implementation worktree.
   Record the resulting `git worktree list` and porcelain status before implementation resumes.

## Ordered recovery sequence

### 1. Make the truth reset durable

- Land this ledger and reconcile the three active changes to the authority boundaries above.
- Keep all runtime tasks unchecked.
- Complete and preserve the workspace manifest and cleanup evidence.
- Reproduce the pinned checkpoint or record an explained delta before touching runtime code.

### 2. Re-review PR #990 as boot-only work

PR #990 is reviewed only after step 1. Its clean reference is worktree
`/private/tmp/semstreams-gh986-boot-only-flow-activation`, branch `codex/gh986-boot-only-flow-activation`, commit
`8f19ef36`.

The review must establish that it:

- preserves boot-only component topology and rules-only hot reload;
- does not introduce component hot reload, lifecycle wrappers, retained Stop results, or new restart machinery;
- remains understandable as ordinary composition code on the touched ComponentManager and Rule surfaces;
- is green and mergeable without relying on abandoned worktree changes.

If it passes, merge it before lifecycle owner migration because it changes owner surfaces. Assign it zero lifecycle
completion credit, then pin and reproduce a new full census on merged `main`. If it fails, narrow or close it; do not
repair it by adding lifecycle machinery.

### 3. Implementation gate A: simple and failed-Start owners

Migrate the rebased S and F owners, plus only leaf owners proven to have the same budget:

- direct owner-local cancel and done/WaitGroup joins;
- fence real local admission before cancellation when the owner actually has admission;
- preserve acquired native handles until cleanup succeeds;
- retain `cleanupPending` and reject Start when bounded failed-Start cleanup expires;
- The only final shared helper is `internal/lifecyclecleanup.RollbackFailedStart(parent, rollback)`, born with R1 and
  immediately bounded as `WithTimeout(WithoutCancel(parent), 5s)`. Exact completion waits remain owner-local. The old
  lifecyclejoin rollback implementation remains unchanged only for unmigrated owners; migrated owners never import it.

No generic generation, operation election, retained result, rejoin channel, concurrent-Stop coordinator, detached
cleanup, or bespoke shutdown state machine is allowed. A leaf does not independently solve shutdown ordering that its
parent composition boundary owns.

Gate A completes only when each assigned owner has focused behavior tests and race proof, the census decreases by the
reviewed owner count, and no prohibited helper or semantics were added elsewhere.

### 4. Implementation gate B: native protocol and manager owners

Migrate Q, P, and M owners against their exact protocols:

- await `startDone` before choosing running Stop or retained failed-Start cleanup;
- stop admission through the native NATS, HTTP, WebSocket, worker-pool, exporter, or subscriber handle;
- keep callback authority live while the native protocol drains or closes;
- cancel the run context only at the protocol's correct boundary, then join owner goroutines;
- use a caller-provided Stop context for waits; a bounded cleanup context is permitted only for the documented
  terminal-finalization exception;
- do not discover lifecycle authority by name, catalog children, replace same-name consumers, or mix backlog
  observation into lifecycle control.

Gate B completes only with protocol-specific tests, focused race proof, failed-Start proof where applicable, and an
updated census demonstrating that every assigned owner left the old framework.

### 5. Implementation gate C: deletion and end-to-end proof

- Remove `Generation`, `Operation`, `StopWithQuiesce`, their constructors/methods, and old helper files.
- Remove or rewrite every test that requires concurrent Stop result sharing, canceled or expired Stop rejoin, retained
  error replay, executor election, or StopWithQuiesce behavior.
- Retain the useful contract that a completed repeated Stop is nil/no-op, plus honest timeout behavior and
  failed-Start cleanup authority.
- Audit every production ACK site for effect-before-ACK and declared idempotency, durable progress/outbox, or explicit
  external at-most-once limits.
- Prove controlled shutdown and fresh boot with a real process.
- Prove dirty recovery by killing the process between effect and ACK and observing redelivery and convergence; also
  cover the required NATS interruption/restart path.
- Run focused race tests, repository race tests, integration tests, relevant E2E tiers, schema validation, lint, build,
  and CI against the same candidate commit.

There is no predeclared PR count. Keep reviews bounded by coherent ownership surfaces, but do not equate PR count,
merged documentation, or intermediate green CI with completion.

## Test-surface exit gate

Before runtime migration can be called complete, repository-wide production and test searches must show no dependency
on the rejected semantics. In particular, there must be no test whose required outcome is:

- concurrent Stop callers electing one executor or sharing a retained result;
- a Stop that returned on cancellation/deadline later rejoining the same terminal operation;
- replay of a previously retained Stop error;
- `Generation`, `Operation`, or `StopWithQuiesce` behavior;
- automatic same-name consumer replacement, lifecycle catalogs, or delete-on-Stop knobs.

Positive tests must instead prove owner-visible behavior: completed repeated Stop is nil/no-op, Start/Stop ordering is
race-free, accepted work follows the declared native drain semantics, timeouts return honestly, failed-Start authority
is retained, and all owned goroutines join. Arbitrary sleeps are prohibited; use explicit synchronization.

## Archive and tag gate

The change may not be archived, and no dependent tag may be cut, until one clean candidate commit satisfies all of the
following. “Expected,” “covered by another spec,” and “green before the final rebase” are not exemptions.

### Mechanical production zeros

| Search surface | Required count |
|---|---:|
| Production files importing `internal/lifecyclejoin` | 0 |
| `lifecyclejoin.NewGeneration` | 0 |
| Calls on `Generation.Stop` | 0 |
| External `Generation.Cancel` | 0 |
| External `Generation.Signal` | 0 |
| `Generation.StopWithQuiesce` | 0 |
| `lifecyclejoin.NewOperation` | 0 |
| Calls on lifecycle `Operation.Run` | 0 |
| External `RunPartialStartRollback` using the old symbol | 0 |
| Production declarations of `Generation`, `Operation`, or `StopWithQuiesce` | 0 |
| `Client.StopConsumer` declarations and calls | 0 |
| `Client.StopAndDeleteConsumer` declarations and calls | 0 |
| `Client.StopAllConsumers` declarations and calls | 0 |
| SemStreams `ManagedConsumer` wrapper/type declarations and uses | 0 |
| `DrainAndDelete` declarations and calls | 0 |
| Client lifecycle child catalogs, bindings, and `stopAllConsumers` | 0 |
| `DeleteConsumerOnStop` configuration fields | 0 |
| Automatic same-name consumer pre-stop or replacement paths | 0 |
| `ConsumeDurable` declarations and calls | 0 |
| Lifecycle-bound `OutstandingWork` or name-routed backlog calls | 0 |

Removal credit for `ConsumeDurable` belongs only to N1 and requires owner-approved `NewDurableHandler`, equivalent
heartbeat/AckWait and settlement proof, and a SemStreams migration map for ten sibling production calls. A lower local
call count without replacement earns zero credit.

The only justified final helper is `internal/lifecyclecleanup.RollbackFailedStart`. There is no shared Wait helper. N1
deletes the unchanged legacy rollback implementation and all lifecyclejoin declarations/imports after every owner
migration.

The five historical `DeleteConsumerOnStop` fields must all be absent; one renamed or generated-schema copy still
fails the zero gate. Exact-name searches are the first check for the client rows, followed by type-aware inspection for
renamed receivers, aliases, wrappers, catalogs, bindings, and equivalent same-name replacement or lifecycle-routed
backlog behavior. Read-only backlog observation may remain only when it is independent of lifecycle handles and bound
to a documented product contract.

### Mechanical test zeros

- Zero imports or direct uses of the removed lifecycle API in tests.
- Zero shared-suite cases enforcing executor election, concurrent result sharing, canceled/deadline rejoin, or retained
  result replay.
- Zero delete-on-Stop knob, managed-consumer, name-routed Stop/delete, or same-name replacement contract tests.

### Positive evidence

- Every production owner from the rebased census is mapped to a reviewed replacement and no owner is unaccounted for.
- Controlled shutdown, fresh boot, process exit, failed-Start cleanup, duplicate identity rejection, and owned-goroutine
  join evidence is green at the candidate commit.
- Every production ACK site has a recorded effect-before-ACK and recovery posture; dirty process and NATS interruption
  tests demonstrate redelivery/convergence for crash-critical paths.
- Focused race, repository race, integration, relevant E2E, schema, lint, build, and required CI checks are green at the
  candidate commit.
- Downstream impact is documented only in SemStreams migration notes; all sister repositories remain read-only.
- Every task in this change is truthfully complete. Strict OpenSpec validation passes after, not instead of, those
  runtime and proof gates.

Only after all three sections pass may this change be archived and the next tag be declared ready.
