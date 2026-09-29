# Final implementation review — #1428 / draft #1429

Mode: implementation review, before archive. Read-only; reviewer ran no tests or Docker, made no repository edits, and committed nothing.

## Verdict

APPROVE the implementation at the exact source/baseline identities below. No open blocking or high finding remains. The bounded change repairs existing rule-test cleanup ownership, with no production behavior or exported API change.

This is not merge authorization. Final archive/spec reconciliation must still be reviewed separately as the last content commit, and the #1421 required-Test-job flake hold remains binding for #1429. The #1404-specific waiver does not transfer; a fresh green hosted run alone does not discharge that hold.

## Exact reviewed identity

Worktree: `/Users/coby/.codex/worktrees/gh1397-runner-tests/semstreams`.
Branch: `codex/gh1428-rule-test-cleanup`.
HEAD before implementation commit: `0d888375e332ac915ba55af50668cd6c0cad699b`, plus the reviewed uncommitted files.
Frozen original source: `caa98f5acae60efbc669ad1e1795ab6e903abd42`.

| Artifact | SHA256 |
|---|---|
| review/evidence/implementation-source-current.json — all16 rule test files | `fbaf28c90d9b646a43d41a641b5e5e1b5d497f8d8dda4faad7eac4418e533607` |
| processor/rule/rule_integration_test.go — final vet correction | `af3e0fb27a9f3324ef50d5a9300dbcd56313625b3236d58e4387b31ef0349e5b` |
| test/testinfra/cleanup_baseline.json | `1ffdf6ad20159fa8d25371c2b3820afba938323e66ecb488400c44002be34b82` |
| review/implementation-evidence.md | `7c99f33e53037e00988d4bcb23cd723399de4ad421a341467453824266145350` |
| review/final-handoff.md | `9373b1caf4f148c2adad3644f668c173fb92291add0470a0e52c87b70f31139c` |
| tasks.md at review | `28f5c05b54001a986d9f2360af3f71495a5ff393e0b4f1109fe491a1a9a1dc5d` |

Review-relative artifacts above are under `openspec/changes/rule-test-cleanup/`. All16 source hashes and the installed baseline hash independently still matched after the full gate. Git scope contains only the16 processor/rule test files, cleanup-baseline reconciliation, and this change's documentation/evidence. No guard, analyzer, production, schema, or exported API edit is present.

Accepted inventory checkpoint `047be916111816f353d2998a06b40e24bcecb0e6` and its ownership ledger remain the population authority. Accepted corrected design SHA256 `c2206a3471e0fc5c3f21f966422856b4ee1adc7fa9b02b0352f361ac9cb82e78` remains the target. No new inventory census was required for implementation review.

## Implementation and finding closure

All24 B roots and37 H helper call sites are reconciled to concrete owners. The37 physical helper sites comprise24 scheduler,6 cron Processor,5 run-scope harness, and2 revision harness calls; they overlap the B population and are not additional independent owners.

Private owners protect acquisitions before fallible setup, maintain provisional ownership through returning-helper setup, transfer immediately before return, and receive immediate lexical finalization at callers. Native Stop is synchronous, receives a fresh finite terminal context, records its attempt before entering native code, checks native/context errors, and precedes private Start cancellation. A failed explicit attempt cannot arm fallback retry. Restart/hardening phase fences, operation authority after first Stop, external rule_test boundaries, deliberate lifecycle probes, parent/subtest lifetimes and named goroutine joins remain intact. Subscriptions and substrate outlive component finalization; canonical NewTestClient keeps sole termination ownership.

The review identified and verified corrections to three concrete problem classes:

- External cases initially observed a wider cleanup authority than accepted Start and moved existing execution clocks before substrate/setup. Final source starts the original10/15-second phases after Initialize, installs a direct narrow finalizer before Start, and keeps an early setup fallback. Narrow finalization runs before its phase cancel; the attempted fence makes the early fallback skip.
- New proof tests initially had raw unbounded observation/join receives. Final proof source protects release before admission observation and uses finite body/failure-cleanup observations, preserving completion signals as the positive oracle.
- Full tagged vet rejected the captured/reassigned-cancel form of the first authority correction. The final direct-defer form removes it, preserves the ownership contract, passes the affected seven-case integration rerun, and passes broad tagged vet.

The earlier source/proof, wiring and manifest reports retain the evidence and attempted refutations. No source or packet correction remains open.

## Independent test-fidelity and evidence assessment

Native proofs reach the real Processor accepted-Start/command-lane/Stop seam and real CronScheduler admitted-action/Stop seam. They observe admission, fences, live authority, release, native return and owner completion rather than only fake Stop counters. An actual returning revision helper's post-Start/pre-transfer escape reaches concrete graph-ingest Stop. Executed external-package cases exercise the private external adapter.

The named-example PBT rationale is accepted for this finite ownership-order adaptation: setup exit, transfer/body exit, explicit phase fence/restart, failed Stop/fallback, and ended authority. The oracle is the existing test-cleanup-policy lexical ownership contract; no new production state machine, grammar or revision law is introduced. This is not a generated-property-execution claim.

The three targeted mutations are sufficient for the accepted adaptation risks: omit actual-helper provisional finalization, cancel Start before native Stop, and mark attempted only after success. Independent review verified intended assertion failures, passing baseline/restored selections, unchanged relevant assertion hashes, and exact restoration. Every mutant was reconstructed in memory from retained baseline bytes and the retained patch; all reconstructed mutant and restored source hashes matched. No invalid build, runner timeout, survivor, or skipped assertion was counted as detection.

Durable evidence was verified mechanically:

- Original implementation ZIP SHA256 `00114fa6a8e26e84996501df7ed4f3940406321ba775cd0410b6b5674b8ccb2e`:34/34 exact member hashes/sizes and original-byte equality.
- Correction ZIP SHA256 `a69d30ad10fdb84c49ed14b84e67780589c0fca48fc08ddbeb15af61e0aea404`:7/7 exact member hashes/sizes and original-byte equality, including the pre-vet backup.
- Refreshed B/H and correction tables:145 source pins resolve with expected helper/finalizer/cancel roles.
- Original selected canonical integration:34 top-level passes, zero skips,31.489seconds. Corrected external/helper selection:7 named passes, zero skips/fails,9.270seconds. Historical and current-source evidence are distinguished in the packet.

## Exact baseline and guard

Independent candidate review verified exactly24 deletions matching both B00–B23 and the actual guard's stale identities. All273 retained legacy entries and all90 existing resolutions preserve every field, fingerprint and order. Exactly four new source-reviewed non-lifecycle cancellation records are appended; final counts are273 legacy entries and94 resolutions. One successful independent gopls field-reference query per new field confirmed the sole assignment is context.WithCancel's private CancelFunc. The finite dependency lists cover types, constructors, methods and applicable build/import bindings. No lifecycle Stop exemption or blanket approval is introduced.

The exact approved candidate is installed, and the unchanged actual cleanup guard passes. Its standalone durable result records2360 sources,2357 typed sources,1361 sites and976 exclusions; the full gate reruns the guard on the final corrected source.

## Full local verification

`task check:push` completed with exit0 in858.046seconds. Its durable log is byte-identical to the original execution log:

- `review/evidence/check-push-final.txt`: SHA256 `004ad6ca25124004b936ff0d461451f9e38c25e10076e7843c7cd458f39b8f53`.
- `review/evidence/check-push-final-status.json`: SHA256 `049efb9c5b1d63395952c228fc68142c072b3a081c4f29fa02b2c4abfaf97fe7`.

The log records cleanup admission, default lint/vet/fmt/revive, fixed-port/NATS guards, build, integration and live_llm tagged vet, schema generation with clean drift check, contract tests, full default race tests, and canonical integration through the existing runner. Rule default race passed in6.360seconds; additive rule integration passed in45.131seconds. The canonical integration runner records completion. This verifies broad tagged vet after the correction, not merely the empty package-specific vet log. No hosted-CI result or actual E2E tier is claimed by these package runs; this test-only, nonbreaking change does not add a breaking-change E2E obligation.

## Task truth, native limits, and remaining steps

Tasks1–4,5.2,5.4 and5.5 accurately reflect completed branch-level work. Independent read-only GitHub queries confirmed PR#1429 carries the scoped implementation description, exact counts, evidence, implemented-by persona, pending archive/hosted state and #1421 hold; parent#1417 records branch273/94 separately from main297/90 and keeps the tracker open. After this verdict, the coordinator recorded the routine acceptance checkbox for task5.1; that records this review rather than new validation. Task5.3 archive/spec reconciliation is the only remaining unchecked duty; no task asserts future merge, hosted success, or issue closure. The handoff names required hosted checks without claiming they have passed.

Five seconds remains a supplied cooperative cleanup context, not a hard native interruption guarantee. Contextless watcher/cache operations and Processor's intentional post-deadline join may outlive that deadline when native work ignores cancellation. Scheduler expiry is explicitly not claimed as a completed component join. Proof failure timeouts report failed observations rather than successful teardown. Those native limits are disclosed and are outside this test-only repair's production scope.

No normative spec delta is needed because the current policy already states the adopted contract. Archive with validation enabled and --skip-specs as reviewed, then verify archive/spec reconciliation is the final content commit and retains these exact implementation/evidence identities. Any content correction afterward re-enters reconciliation.

APPROVE — implementation only. Final archive review remains required. #1421 merge hold remains in force for #1429 until a landed fix or explicit owner waiver on this PR; fresh green CI alone is insufficient.
