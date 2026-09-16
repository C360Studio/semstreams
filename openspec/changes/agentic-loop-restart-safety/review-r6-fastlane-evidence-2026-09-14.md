# R6 fast-lane and operational-state evidence

Status: **R6 CLOSEOUT PASS** — independent implementation, evidence and task-truth review complete. Not whole-PR approval.

Baseline: `c347eff487f50b93bc338d764f43ef5b5ea5e133`, draft PR #1159, on frozen parent
`417beae5552f8f15ad3540edd7d8504c87174c13`. The R6 delta is local and uncommitted.

## Scope and authority

The owner answered **“Retry transient persistence failures”** to the specific post-terminal-PubAck final-KV-write
question, recorded in [comment 5661115059](https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5661115059).
The runtime change is one return expression in `component.go:1891`: final Update failure uses the existing
`loopSettlementDecision(err)`. Cancellation, terminal approval and confirmed-absence handling preserve that decision;
ordinary `persistHandlerResult` callers already classified the returned error. No new classifier, state, bucket,
payload, public API, retry configuration or production hook was added.

Earlier validation, selection, synthetic-effect, context and terminal-publication exits are unchanged. Saved
completion alone does not authorize retry of unresolved effects. Active design/spec prose no longer makes that
blanket claim. Existing comments/logs now describe real missing/full-waiter and lane-specific release behavior.

## Bounded obligation map

| R6 obligation | Evidence and boundary |
|---|---|
| Operational five-state contract and local installation/cancellation validity | Reuse `review-loop-state-implementation-2026-09-13.md`; state implementation is unchanged in this slice. |
| Current authority, selected completion, observed revisions and approval exceptions | Reuse the R2 closeout in `review-terminal-complete-2026-09-13.md`, including matching/noncurrent gates, absence, conflict and replacement. This slice changes only the final-write disposition. |
| Four physical fast callbacks, malformed inputs and panic ownership | Existing production callback/refusal tests retained; the focused control command below includes the approval-panic owner case. |
| Approved/rejected verdict ready, missing and full waiter | Six new rows use registered envelopes, actual setup callbacks and the real enforce dispatcher; verify exact execution routing, ACK/NAK/unsettled result, retained queued verdict and exact owner drain. |
| Unsupported registered signal | Seventh row proves Terminate without KV lookup/mutation, process mutation or consumer drain. |
| Cancellation final-write failure and replacement | Native UserSignal source proves Retry without quarantine, unchanged nonterminal KV, selected COMPLETE and terminal publication, release, then same stream/durable/source redelivery and final-marker-before-ACK. |
| Missing-waiter verdict replacement | Native rejected-verdict source is NAKed; a fresh component with the existing waiter receives the same source and routes the decision before ACK. Both physical ports are covered by the six callback rows. |
| One release point and nonblocking audit/graph | Reuse existing release-map/idempotence/reader-order/durable-result/sweeper proofs and R2 graph/audit evidence; no second release owner or graph authority was introduced. |

These are native durable-consumer/component-replacement tests, not new OS-process-restart or E2E claims.
Optional verdict reason decoding and retained-governance proposal/fingerprint proof remain R7.

## Review and verification

Independent reviewer returned INVENTORY PASS for the 253-pin inventory and its mechanical refresh, DESIGN REVIEW
PASS for the exact final-KV spec correction, and APPROVE for the seven callback rows. The initial native/runtime
review found no remaining findings. The final named replacement-subtest refactor and refreshed native evidence
also passed independent review. The reviewed inventory's pre-task-tick SHA-256 is
`75582571c5d93516f568d3b09c9c6fc4f70c6f5be3acf02c0f1cab11de337725`; final materialization refreshes only
the R6 checkbox and the shifted R7 task pin, with searches recorded in that inventory.

Evidence originated in `/private/tmp/gh1146-r6-callbacks.IFg6DO/` and is copied to `test-evidence/` under
`/Users/coby/Code/c360/semstreams-wt/gh1146-rescue-checkpoint.y3bWGc/r6-fastlanes.bl2d61/`.
The same checkpoint's `decision-evidence/` preserves exact owner comments and the reviewed spec patch.
Its nine-file `r6-source.tgz` snapshot contains the five source/test files plus active design/spec/tasks/inventory,
SHA `752bce49964c380e2569ba9e234aebc3d00b53de54e8c83aa52fbba5c66ac1c9`.
The review record itself is copied separately, avoiding a self-referential archive hash.

| Run | Result | Artifact SHA-256 |
|---|---|---|
| Seven callback rows plus existing production/panic cases, race | PASS, 1.547s; coverage-only GREEN, no invented RED | `callback-race.log`: `7fb994c70b8394de55dced30a55d8db0b193e02bf91089e631d91542fd3c3ced` |
| Native cancellation against unchanged final Update | Intended RED: expected NAK 1, actual 0; exact signal owner lost on injected final Update failure, package 3.201s | `cancel-native-red-executed.log`: `3b77b05294f9b40efaee2cc4812abf1040ba3a9400027b4e4d393cc9d36e2a3f` |
| Both native replacement tests after one-line fix, before test-length cleanup | PASS, cancellation 30.41s and verdict 30.45s, package 62.912s; no skips | `fastlane-native-green.log`: `9294f292def3eeaa67728064f170d75f581ce6a0c2d636e8409340056540d5cc` |
| Existing focused controls, race | PASS, 1.632s; 27 matching top-level declarations, not a full-package claim | `runtime-controls-race.log`: `c67a332665a30db8c33bdf77c6624d2ecb17bbc6c495817f1c7d291219c33fa1` |
| Final named-subtest native rerun, race | PASS, both tests 30.43s, package 62.900s; no skips, normal container cleanup | `fastlane-native-final.log`: `61f019d4c9adbe6fb594c78bf422654643c36adb194705c40f35f1a7236e0075` |
| Whole-package pinned revive after formatting | PASS, exit 0 with no warnings | `package-revive-formatted.log`: empty success output, SHA `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |

Root independently captured the final package-lint command and exit 0 on frozen source in
`decision-evidence/package-revive-root.json`, SHA `39dff533ed44f2c729051162c8ae0572e9847ac98647860a3523b09b4fde2049`.
Root's final inexpensive checks passed: `git diff --check`, strict OpenSpec 55/55 and inventory verifier 253/253.
`task openspec:queue` reports 13/22 with R11's combined-source blocker unchanged. Final inventory SHA is
`5ef90052f0854c9910c2fe0ac9b6f085871d603bfe0b8f53b5626dd6cd98e973`.

The initial sandbox Docker refusal in `cancel-native-red.log` is not behavioral RED evidence. Scoped lint then
identified a 104-statement cancellation test, exceeding the repository's 80-statement limit. Its actual replacement
phase became a named subtest; no assertion or production behavior changed. Final native and package-context lint
passed. The earlier partial-file package-comment warnings are not asserted to be production defects.

Exact commands (run from the claim worktree):

```bash
GOCACHE=/private/tmp/semstreams-r6-gocache GOPROXY=off GOSUMDB=off go test -race ./processor/agentic-loop -run '^TestLoop(Production|ApprovalPanicProductionCallback)' -count=1 -timeout=60s
GOCACHE=/private/tmp/semstreams-r6-gocache GOPROXY=off GOSUMDB=off go test -race ./processor/agentic-loop -run '^Test(LoopProduction|LoopApprovalPanic|LoopCancellationUnknownPublication|CancellationPreEffect|TerminalSelection|SelectedSyntheticActionFailure|ApprovalTerminalSelectionUncertainty|ApprovalGateFailureUsesExistingDeliveryClassification|ApprovalGateInvalidArgumentsUsesExistingOwnerClassification|ApprovalGateDurableFailureCannotAck|ColdApprovalFinalStateFailureRetries|PersistHandlerResultReturnsPublicationFailure|TerminalLoopEntityIsFinalAppliedMarker|ResponseTerminalSignals|FailureTerminalSignals|TerminalToolSignals|TerminalRelease)' -count=1 -timeout=60s
GOCACHE=/private/tmp/semstreams-r6-gocache GOPROXY=off GOSUMDB=off scripts/run-integration-tests.sh ./processor/agentic-loop -run '^TestIntegration(CancelFinalMarkerRetryAfterComponentReplacement|VerdictMissingWaiterRetriesAfterComponentReplacement)$' -v
GOCACHE=/private/tmp/semstreams-r6-gocache GOPROXY=off GOSUMDB=off go tool revive -config revive.toml -formatter friendly ./processor/agentic-loop/...
```

The native runner preserves the shared host lock, integration tag, race detector, count 1, failfast and production
BackOff. The native RED used only the cancellation test in that command. All owned commands have exited; source
is frozen and no test/agent is continuing implementation in the background.

## Tested source identity

Final production and callback source is unchanged by the test-length cleanup:

```text
9ff88f288c6013deec20de0de58606b903b43be0edfd2c61e02d31b3d8fa5008  processor/agentic-loop/component.go
4bcc9d02b38c8d57cecff74959a1ccf064bcf1a70898df2b4b8b3c78ca99c863  processor/agentic-loop/governance_dispatcher.go
76c124c18e0787735469b0f9b97efe2cc71e1bcc4488f437da50eb8174b1ac80  processor/agentic-loop/trajectory_handler_wiring.go
a3b646ea2e1844038b120d594225e9281bc0b47b6c7dfa1f49b52e664bcab557  processor/agentic-loop/delivery_owner_test.go
68adfb63d9de2ba1855e7eb398db4a5820018b32eabacd9a7788448289c0000c  processor/agentic-loop/fastlane_replacement_integration_test.go
```

The native RED used test SHA `01bb635cd9fb10758d7406a5de3bf9371ff88dc228043692e6a77f2ec199d853`
and comments-only component SHA `85f7a0f95cccfba6c71714e930960ce1546065accba5d34725a5c5c58a1f45d3`.
The first native GREEN used that same test with final production above. The final native rerun used the named-subtest
hash in the manifest, not an inferred green from the earlier test. The exact five-file source patch against the
checkpoint is `full-source.patch`, SHA `b487a54f492cfed67863019d5d62a3b6f1a40a8c68b5138a4910e3584b7606bb`;
its checked manifest is `final-source.sha256`, SHA `9ab6cc9d20c733084169b89016e84aab599418b379dbac3b274e810d1086054d`.

## Remaining gates

R6 has independent CLOSEOUT PASS for source, bounded evidence and task truth. R7–R15 and the final combined
source, full push gate,
relevant E2E, complete PR review and archive/spec-sync gates remain open. The earlier full push gate failure in
unchanged natsclient Docker startup remains recorded in the R5 review; its draft-only exception authorized only
published `c347eff4`, not another push or merge. No main landing, issue closure or ownership transfer is claimed.
