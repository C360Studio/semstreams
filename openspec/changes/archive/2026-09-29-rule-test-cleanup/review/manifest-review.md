# Cleanup baseline candidate review — #1428 / #1429

Mode: implementation review, exact baseline reconciliation and manual-classification slice. Read-only; no tests, Docker, repository edits, baseline installation or commits.

## Reviewed identity

Uninstalled candidate: `/private/tmp/gh1428-baseline-final-candidate.json`, SHA256 `1ffdf6ad20159fa8d25371c2b3820afba938323e66ecb488400c44002be34b82`.

Source: HEAD `0d888375e332ac915ba55af50668cd6c0cad699b` plus the final 16-file source manifest, SHA256 `53be1e8c8ef8a31ebd1d7f14ba127f33dab1375a99e0234c81885a904c1a0ed0`. All 16 current source hashes still match. The existing baseline file is byte-identical to frozen inventory source `caa98f5acae60efbc669ad1e1795ab6e903abd42`.

Actual guard report: `/private/tmp/gh1428-census.json`, SHA256 `d03d78b38f2f6e5ee0a56c72d7398c47a33d7fff218aacf0129455876703f2e8`. Its parsed contents independently equal the JSON payload in `/private/tmp/gh1428-census.log`, SHA256 `427def87e2d081992dc2ef7bc2e59061e47a337c7b9197fa34041e6d7e066665`.

## Reconciliation

Independent exact-value and ordering comparisons establish:

- 297 original legacy entries become 273. There are exactly 24 deletions and no added or modified legacy entries.
- The removed identities equal the actual guard's complete 24 stale-approval diagnostics and exactly the accepted B00–B23 function population. All removed paths are in processor/rule. The previously reviewed migration source covers these entries.
- All 273 retained entries preserve every field and their order.
- All 90 existing manual resolutions preserve every field, dependency fingerprint, and their order.
- Exactly four manual resolutions are appended, yielding 94 total. Candidate rows exactly match `/private/tmp/gh1428-new-resolution-candidates.json`; identities, unresolved questions and site fingerprints exactly match the four actual unresolved callback sites. Version and other top-level metadata are unchanged.

This is selective reconciliation, not broad baseline regeneration. No new unbounded cleanup approval, deliberate-lifecycle exemption, path-wide approval or guard change is introduced.

## Independent classification review

Each row classifies the exact deferred private field invocation as `non-lifecycle-stop`. All four fields have type `context.CancelFunc`; each constructor leaves that field nil, and its only assignment is the cancel returned directly by `context.WithCancel` in that owner's startContext. The deferred invocation does not dispatch a component lifecycle method. The actual concrete Stop is separate, synchronous, and supplied a fresh bounded terminal context; that Stop is not exempted by these rows.

| Exact callback site | Sole assignment | Explicit dependencies | Decision |
|---|---|---|---|
| test_graph_ingest_owner_integration_test.go:38, graphIngestTestOwner.stop | same file:27–28 | owner type, constructor, startContext, stop, finish, provisionalFinish, transfer | approve non-lifecycle-stop |
| test_owner_external_integration_test.go:37, rule_test.processorTestOwner.stop | same file:26–27 | owner type, constructor, startContext, stop, finish | approve non-lifecycle-stop |
| test_owner_support_test.go:36, rule.processorTestOwner.stop | same file:25–26 | owner type, constructor, startContext, stop, finish, provisionalFinish, transfer | approve non-lifecycle-stop |
| test_owner_support_test.go:92, cronSchedulerTestOwner.stop | same file:81–82 | owner type, constructor, startContext, stop, finish | approve non-lifecycle-stop |

Attempted refutation used one independent gopls references query per new private field, with the integration build selection. All four succeeded. Graph-ingest, external Processor, and scheduler fields have only assignment/nil-check/deferred-invocation references. Internal Processor additionally has the existing proof's nil-check read at test_processor_owner_proof_test.go:100; it does not assign or escape the field. No competing assignment or callback target was found. The external package remains distinct from the identically named internal owner.

Queries (GOCACHE/GOPLSCACHE under /private/tmp; GOFLAGS=-tags=integration):

- `gopls references processor/rule/test_graph_ingest_owner_integration_test.go:17:2` → lines28,37,38.
- `gopls references processor/rule/test_owner_external_integration_test.go:17:2` → lines27,36,37.
- `gopls references processor/rule/test_owner_support_test.go:15:2` → lines26,35,36 plus proof100.
- `gopls references processor/rule/test_owner_support_test.go:72:2` → lines82,91,92.

The finite explicit dependency lists exactly match retained canonical dependency metadata. Inspection of cleanupSourceDependencyFingerprint confirms receiver-qualified method selection and inclusion of relevant build constraints and import bindings. Therefore integration-tag and context-import changes participate in freshness checks. Context.WithCancel is the Go standard-library cancellation primitive; these classifications do not depend on a third-party module's lifecycle implementation. The retained scratch harness calls that canonical fingerprint routine and contains no baseline-install or guard-bypass operation. Canonical fingerprint acceptance will be checked by the normal installed-candidate guard; it was not rerun by this reviewer.

Dependency metadata SHA256: `f5b0b57ce3af5e39c996070b350de9e11d37b8a458766a94361b15621b24e5ed`.
Retained generator text SHA256: `fdff68857e9a198b63f6c66dfe79624767ff3517bb21b845355b9d39d61343b0`.
New rows SHA256: `9ca79d6b38653a8b183d90018bc8eb7c84643aaf8f4000a6da94ccb5ef7fce96`.

## Verdict and limits

MANIFEST APPROVE for the exact candidate SHA256 above, including reviewer attribution on its four new exact non-lifecycle cancellation records. The accepted corrected design explicitly permits these actual-guard-required classifications while preserving the existing 90 records; final measured totals are 273 legacy entries and 94 resolutions.

No open finding in this candidate slice. Installation followed by the ordinary full guard and required verification gates remains the coordinator's next step. This verdict is not a passing-guard claim, full implementation/merge approval, or removal of the #1421 merge hold. Final durable implementation evidence and branch-checkable task truth remain to be reconciled.
