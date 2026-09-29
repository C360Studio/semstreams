# Preliminary implementation wiring review — #1428 / #1429

Mode: implementation review, migration-wiring slice only. Read-only; no tests, Docker, repository writes, or commits.

HEAD: `0d888375e332ac915ba55af50668cd6c0cad699b`; frozen source base: `caa98f5acae60efbc669ad1e1795ab6e903abd42`.
Accepted design SHA256: `c2206a3471e0fc5c3f21f966422856b4ee1adc7fa9b02b0352f361ac9cb82e78`.
Accepted ledger SHA256: `1b74ac431316ec3d945780bbab0768b0b6a3072c856960609d931623effbb1b6`.
Initial source manifest: `/private/tmp/gh1428-wiring-source-snapshot.json`, SHA256 `c504940ecf341f8ec4ef8d71649eb62aa0ca9e7d08ac8e2bb77f68b41c12453f`.
Eleven source files still match that snapshot. The two external integration files were refreshed after the developer's observation-subscription changes; exact reviewed bytes follow below.

## Findings

### HIGH processor/rule/rule_integration_test.go:138 — Cleanup observes a wider authority than the accepted Start

Mechanism: B18 starts with `owner.startContext(testCtx)` at line152, where testCtx has a10-second deadline, but defers `owner.finish(ctx,t)` against its30-second ancestor. B16 repeats this at549/561 with15seconds, and B20 at856/868 with15seconds. The external owner checks only its supplied operation context's Err (`test_owner_external_integration_test.go:45`). If the narrower accepted Start parent expires while outer ctx is live, cleanup can report success without detecting lost controlled authority.

Attempted refutation: native Processor Stop does not guarantee an error for an ended Start parent. `owner_lane.go:139–172` settles a fence with nil after that runtime has already ended; `processor.go:1247–1348` uses the fresh terminal context and resource results, not the accepted Start parent's Err. The existing abort test `readiness_integration_test.go:171–220` explicitly accepts native nil or accurate cleanup errors after accepted-parent cancellation. Private cancel deferral is correctly ordered, but cannot detect this independent earlier parent expiry.

Smallest correction: make finalization observe the actual narrower accepted authority as well as any separate outer operation authority, without storing Context or replacing an expired authority. Retain separate outer KV/I/O authority and synchronous bounded Stop. Verify the shorter-authority-ended/wider-authority-live case is classified as a failed controlled shutdown.

### HIGH processor/rule/rule_integration_test.go:74 — Existing execution budgets now include substrate/setup time

Mechanism: all six B15–B20 cases move their existing10/15-second Start/work context creation from after Initialize to the test entry, before `getTestNATSClient`. Examples: B18 at74 before76; B15 at222 before224; B16 at498 before500; B19 at404; B17 at651; B20 at765 before767. `getTestNATSClient` permits30seconds for shared substrate startup (line39). Consequently valid substrate/setup latency can exhaust a previously independent execution budget before Start, or substantially shrink the existing behavioral probe window. This is a scope change despite retaining the timeout constants.

Attempted refutation: the accepted design104–108 preserves existing10/15/20/30-second scopes and separately introduces bounded setup scopes for previously unbounded work. Ledger B15–B20 and the original deleted diff lines place these six execution clocks immediately before Start, after Initialize. No measured or accepted change authorizes consuming them during NATS startup. Existing contextless native construction/setup does not itself shorten startup to these limits.

Smallest correction: retain early provisional owner protection while preserving the execution-phase deadline boundary. Bound previously unbounded setup independently; do not lengthen the existing work budgets to compensate. Ensure lexical cancellation still follows owner Stop and resolve the narrower-authority observation above.

## Wiring checked

The complete13-file migration diff and three concrete-owner implementations were reviewed against the already accepted B00–B23/H01–H37 ledger; no new census was performed. All24 baseline-root migrations and37 physical helper callers are represented:24 scheduler,6 cron Processor,5 run-scope harness,2 revision harness calls. These populations overlap; they are not61 independent owners.

- Direct Processor/scheduler owners are installed before Initialize/Register/Start and post-acquisition assertions. Owner attempts are recorded before native Stop; terminal contexts are fresh, detached only for bounded synchronous cleanup; private Start cancellation runs after native Stop even on panic. Finalizers use nonfatal error reporting and skip an already attempted owner.
- The returning cron and graph-ingest helpers provisionally own acquisitions through fallible setup, transfer immediately before return, and callers immediately defer finish. Graph-ingest construction asserts success before installing the owner, but this was refuted as a leak: `CreateGraphIngest`704–784 returns nil on every error and only a concrete Component on success, with no later fallible branch after allocation.
- Restart/hardening explicit Stops use the owner attempt fence; later operation authority is independent of the private Start cancellation. Deliberate nil, repeat, abort, and concurrency probes remain native. Scheduler real-robfig/second-Start cases remain intact.
- Shared substrate stays alive through lexical component Stop; its Terminate results are checked. Canonical NewTestClient retains sole termination ownership where duplicate caller cleanup was removed. Observation subscriptions are retained, checked, and cleaned by testing callbacks after lexical Stop. External tests remain package rule_test using their private concrete adapter.
- No production or exported API change is present in this slice. The residual retained adjacency population is not silently swept into this repair.

## Limits and pending stages

This is not final implementation approval. Newly appearing `test_owner_proof_test.go` is outside this frozen wiring slice and has not yet been reviewed. Focused native proofs, mutation sensitivity/restoration, real integration, baseline reconciliation, exact new native-cancellation classifications (if needed), evidence manifest and required gates remain explicitly pending. Their absence at this planned intermediate stage is not an additional defect. No test command was executed by this reviewer, and the reported focused race/compile results are not promoted to full proof here. The #1421 required-Test merge hold remains unchanged.

## Exact reviewed source identity

| File | SHA256 |
|---|---|
| processor/rule/actions_run_scope_integration_test.go | `340dc58fe59b7bef6033d0cb0e7f35755e3bef4763b19ebafd9cca22d4d8325a` |
| processor/rule/cron_scheduler_integration_test.go | `2139752f76b18eec014aed9f34016045b5086f7c94ba9c122509097a0b8d25d0` |
| processor/rule/cron_scheduler_test.go | `896eabe63815c9e11b2d873af6b92550fa74c4f7d6f2fb7f2b7a50dde3321308` |
| processor/rule/entity_watcher_debounce_integration_test.go | `bc209a216e37e0a8fae9bf63ba359f9c6aa85904388faf5d35e8c2ec8d6a3b25` |
| processor/rule/entity_watcher_hardening_integration_test.go | `b42b1c52ec86ebaa2d36e035e28afb324c2d24c19b88d12a5d07b833392d8a2b` |
| processor/rule/entity_watcher_integration_test.go | `3ad5e89b3c4130f9bb57274d131140f2fabccc68c30ee8464f362ff54b6b14b9` |
| processor/rule/rule_integration_test.go | `c8c1c3777c7821dd75bdc174807b9c41ef956227c10b537ee897cf4cfb3c6eec` |
| processor/rule/state_cleanup_integration_test.go | `7f7c6b98a9af8e83702245b479b74771e05ed0d97818dfba7bc938c9d78ae4f7` |
| processor/rule/stateful_integration_test.go | `1b4e66ae4eda5bce7447e06e5a5ed4acd427540a5019139feaf644085e90325d` |
| processor/rule/test_graph_ingest_owner_integration_test.go | `2fd41bfcddb83ffc3dbb4396d97e7b15004107c509b514e6182f51af431ea19c` |
| processor/rule/test_owner_external_integration_test.go | `56475ac98dfb6358374896611647d307208b7b328f9cedf4904826b117ca364d` |
| processor/rule/test_owner_support_test.go | `ffa9a2a8a1a0800f256982f260b973f08edc942b6fc1f9509c19d73a8c580bd8` |
| processor/rule/triple_mutator_revision_integration_test.go | `426f8c3616f12b9b09a1f5acee955b3900807914fd3b01305035a2e510dc0e3c` |

CHANGES REQUESTED for the two HIGH wiring findings above. Full implementation verdict withheld pending fixes and the complete evidence packet.
