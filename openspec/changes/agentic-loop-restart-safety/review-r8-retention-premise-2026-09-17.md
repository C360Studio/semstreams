# R8 retention-premise review

Baseline: `68c14c8eb25c512e988f740cbf7ea14b6815976f` in the dedicated #1159 worktree.
This is evidence review, not an owner ruling or completion of R8.

## Inventory checkpoint

Artifact: `inventory-r8-retention-premise-2026-09-17.md`.
SHA-256: `fb937d0ef9c3c53e3a85deed5ee3d426f7cff3b786cc930767aa56d1881b2a40`.

The root materialized the complete architect handoff, changing only prose bullets to numbered items and the
search-section heading to the verifier's recognized spelling. The architect confirmed complete handoff fidelity.
The first verifier invocation rejected the original prose/search bullet formatting, not the source pins.
After formatting correction, root and independent reviewer each verified 58/58 pins with no drift or parse failures.

Independent reviewer verdict: **INVENTORY REVIEW PASS**.

The reviewer independently traced the named absence paths before comparing the artifact. No material omission
blocks this bounded premise review. The matrix preserves dispatch remint risk, loop task reconstruction,
terminal ancestry's degraded settlement versus own-record refusal, the distinct governance/provider exceptions,
and catalog-owned no-lifecycle completed-tool authority.

Consumer attempt timing does not establish elapsed retention age. Safe refusal is not a promise of eventual
automatic recovery. Neither observation authorizes removing an accepted retention safeguard.

## Scope and evidence limits

No runtime code, configuration, tests or active specification changed in this investigation. The previously reviewed
nil-publisher implementation remains unchanged and uncommitted. No runtime tests or live NATS observations ran.
No commit, push, restack, merge or issue closure occurred. The frozen parent and #1311/#1312 sequence remain unchanged.

Current design still requires the local horizon/safety-margin calculation. Any amendment needs a reviewed design and
the owner's explicit acceptance. R8, R9's separate source-retention proof and final combined verification remain open.

## Pre-owner design checkpoint

Complete draft: `design-r8-retention-decision-2026-09-17.md`.
SHA-256: `52742eff4e91b06e9ddb8e80f1ab025bbcfa235cfb670a4d1efeb1f8a44b67a0`.
The unchanged full inventory above accompanies the draft as its binding evidence appendix.

Independent reviewer verdict: **PRE-OWNER DESIGN REVIEW PASS**. Both artifacts were read completely.
The amendment removes only the unproven timer-derived calculation. It preserves observed retention/admission,
publication and named absence requirements. Dispatch mapping and loop-task authority remain unfinished proof
obligations, including first-party republication and independent port overrides. The resubmission discussion grants
no new identifier-reuse semantics. No blocking findings were reported.

The separate bounded judge recommends the same amendment with high confidence. An enforced total replay-age bound,
including outage and republication, would change its conclusion about whether a finite horizon can be derived.
The strongest counterargument is that removing a retention floor may allow identity evidence to expire before replay.
That defeats wholesale removal of retention protection, not the conclusion that the current timer formula lacks its
required premise. DiscardNew alone does not establish identity preservation.

Judge evidence limits: no runtime or live NATS measurements; source republication bounds and complete source/evidence
retention relationships remain unproven. Both judge `gopls references` attempts failed during workspace loading because
cache access was denied. Reference completeness for that pass is UNVERIFIED; its cited function bodies and callers
were read directly. The separate inventory reviewer independently enumerated the surface and returned PASS.

No reviewer or judge recommendation is an owner ruling. Active proposal/design/spec requirements remain unchanged.

## Owner acceptance and contract promotion

The preceding sections record the pre-owner checkpoint. The owner subsequently answered “approve” to removing the
calculation from the spec, then testing the two paths and fixing only demonstrated failures. This is recorded at
https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5712921768.

The root materialized the architect's narrow promotion into proposal, design, loop delta, tasks and migration notes.
It removes only timer-derived recovery-horizon/safety-margin computation. Existing observed retention/admission,
settlement, KV and named absence protections remain. The two identity proofs stay open; document promotion completes
neither. Dated inventories, decision draft and their hashes remain unchanged historical evidence.

The exact proof-only execution slice is `task-r8-identity-proof-2026-09-17.md`. It distinguishes observed production
retention/republication from fabricated absence and excludes operator deletion. Production code remains unchanged.

## Existing-control diagnostic

Root command on the unchanged runtime source, 2026-09-17:

```sh
env GOCACHE=/private/tmp/semstreams-r7-test-cache GOFLAGS=-mod=readonly GOPROXY=off GOSUMDB=off \
  go test -race ./processor/agentic-dispatch ./processor/agentic-loop \
  -run '^(TestUnreadableRetainedTaskEvidenceDoesNotMintOrRefuse|TestInvalidUserMessageIdentityIsRejectedBeforeTaskIdentity|TestColdTaskRedeliveryReusesRetainedRequest|TestColdTaskRedeliveryConflictingMappingQuarantines|TestColdTaskRedeliveryWithoutRequestRebuildsFromTaskAndPreservesLoop)$' \
  -count=1 -v
```

Exit 0: five top-level tests PASS, no FAIL/SKIP. Dispatch 1.490s; loop 1.805s. This refreshes existing controls only:
unreadable identity evidence retries, invalid source identity refuses, retained loop request is reused, conflicting
mapping quarantines, and partial-birth request absence reconstructs while preserving the loop.
No live retention expiry, new regression, completed-work suppression, whole-package or final R8 proof is claimed.

## Promotion review and unchanged native control

Independent CONFORMANCE REVIEW PASS covers the five-file promotion at hashes `1c90349f…` (proposal),
`2d5e98b1…` (design), `4078db86…` (loop delta), `8b55d1cc…` (tasks), and `a4ea14cd…` (migration).
One proof-handoff clarification required explicit approval before extra native-container starts or baseline runtime.
That exact guard is applied; correction-only PASS covers proof handoff SHA-256
`2a07f42380eca39b8ab5abf269f4d26d784eac44e43348f72160a469dab2ad78`.
`git diff --check`, 387/387 tracked property citations and strict OpenSpec validation (55/55) pass after promotion.

The existing real-NATS dispatch control was refreshed without source or test changes:

```sh
env GOCACHE=/private/tmp/semstreams-r7-test-cache GOFLAGS=-mod=readonly GOPROXY=off GOSUMDB=off \
  scripts/run-integration-tests.sh \
  -run '^TestIntegrationUserMessageReplayAfterTaskCommitKeepsOneLogicalTask$' -v ./processor/agentic-dispatch
```

Exit 0: one test PASS at 0.90s; package 4.883s; no FAIL/SKIP. Original source sequence 1 redelivered twice after
connection replacement and beyond the destination duplicate window; both task publications retained the same
TaskID and LoopID. The existing fixture uses DiscardOld and retains all evidence. This is neither DiscardNew startup
admission nor evidence-expiry proof, and it does not complete either new identity obligation.

The run acquired/released the canonical host lock, used the cached pinned NATS image, and started the existing
package TestMain NATS container plus the test's NATS fixture and Ryuk. Both NATS containers explicitly terminated;
post-run Docker listing was empty and the lock absent. No new fixture, startup count or test runtime was added.
Temporary full output: `/private/tmp/semstreams-r8-retention.QcKh7k/existing-dispatch-native.log`.
Durable identical copy: `r8-retention.ihsH4A/existing-dispatch-native.log` beneath the existing sibling
`gh1146-rescue-checkpoint.y3bWGc` evidence root; SHA-256
`eea02cf28e7808ff74a2989facff93e97b79da203d10a8f6cbece7d802b08fbe`.

## Loop identity diagnostic: seeded absence

The developer added two unit tests to the existing `settlement_recovery_test.go`, with no production changes.
The exact test-file SHA-256 is `774ad585d2b173351a33ca820cb346fac0f86abaecd81b36d4171f82532b80d0`.

```sh
env GOCACHE=/private/tmp/semstreams-r7-test-cache GOFLAGS=-mod=readonly GOPROXY=off GOSUMDB=off \
  go test -race -count=1 -json ./processor/agentic-loop \
  -run '^TestColdTaskRedelivery(WithoutRequestRebuildsFromTaskAndPreservesLoop|WithTerminalAuthorityCreatesNoRequest|WithProgressAndMissingRequestRefusesInitialRebuild)$'
```

Exit 1: three top-level cases, two PASS and one FAIL; package 0.720s; no SKIP or race report.
The unchanged partial-birth control passes. The new retained-terminal control generates no request, returns ACK
through the ordinary task handler, and installs no active loop. The new progressed-loop case fails as intended:
with retained running authority at `Iterations=2` and seeded typed request absence, recovery returns no error and
generates a registered AgentRequest containing the original task prompt and `Iteration 1 of 3`.

Independent evidence review verified the exact test, runtime and raw-log hashes without rerunning the tests.
It confirms generated output only: the unit fixture does not publish to NATS, and seeded absence does not prove
production expiry or replay reachability. The ordinary production caller passes the Created result to its existing
publication path, but this test does not execute that publication. No runtime refusal fix is approved by this result.
The failing test remains intact pending the bounded reachability check; no whole-package green is claimed.

Full handoff, before/after source, diff and raw JSON are preserved under `unit-identity-red/` beneath the same durable
`r8-retention.ihsH4A` evidence directory. Raw log SHA-256:
`5ca2c4dbfdc20f8434cfb55d0759848b17e27928f83b440ecbf19fdf37e5b6b6`.
Test-only diff SHA-256: `39ad665b4379db09ff162016b28a463bf99c484f4a36141f6b13fa35493030f7`.

## Dispatch identity diagnostic: actual expiry

A temporary variant of the existing native control used actual DiscardNew streams: USER MaxAge 1m, task-evidence
MaxAge 200ms, unchanged 600ms source AckWait and 100ms duplicate window. AutoContinue stayed false to exclude its
unrelated ready-view requirement. No extra fixture/startup, sleep, purge, deletion or production edit was introduced.
The original test was backed up, the variant run once, then the original restored and checksum-verified.

```sh
env GOCACHE=/private/tmp/semstreams-r7-test-cache GOFLAGS=-mod=readonly GOPROXY=off GOSUMDB=off \
  scripts/run-integration-tests.sh -v \
  -run '^TestIntegrationUserMessageReplayAfterTaskCommitKeepsOneLogicalTask$' ./processor/agentic-dispatch
```

Exit 1 at the intended original LoopID-equality assertion: one test FAIL, 0.88s; package 3.638s; no SKIP/race report.
Original USER sequence 1 was delivered twice after connection replacement. Task sequence 1 had actually expired:
exact lookup returned typed `jetstream.ErrMsgNotFound`, while the original source bytes remained readable.
The production dispatch handler returned ACK/nil after publishing task sequence 2 with the same TaskID and
SourceMessageID but a different LoopID. One replacement task remained retained, as expected after actual expiry.

This is a measured unsafe-retention configuration and a concrete obligation for unfinished admission. The fixture
does not run startup admission; it does not establish that a final admitted configuration violates its contract.
Do not infer a new runtime mechanism or change identifier semantics from this result. Loop-side TTL reachability
remains separate from the seeded unit failure above.

The canonical runner owned/released the host lock; both existing NATS fixtures terminated normally. Post-run
inspection found no containers, test processes or lock. Original dispatch source SHA-256 after restoration:
`57384dc33d207752c742ffc7b00529035ea760adf69a20c712cb0bc8bc51a924`.
The unit RED source remains unchanged. Full handoff, before/tested source, diff, log and restoration hash are in
`dispatch-expiry-red/` beneath the durable `r8-retention.ihsH4A` evidence directory.
Raw log SHA-256: `c7c6ef1d9df3813b44f4f7c643f74855bbdaddf7b9ed621a4723f89d8d004292`.
Diagnostic diff SHA-256: `28946f57893fd8f03a59194ff4b4a58e0bbbccaa2bc744091631fcffc58090fd`.
Independent native-evidence review returned PASS after verifying the artifact hashes, intended assertion and exact
source restoration. It confirms the unsafe-policy admission obligation, not a final admitted-policy defect or a
runtime-fix approval. No runtime correction, full push gate or R8 completion is claimed.

After adding the two unit tests, tracked property citations pass 389/389 and strict OpenSpec passes 55/55.
These documentation/citation checks do not supersede the intentionally failing runtime diagnostic.

## Loop reachability: source-order witness and stopping point

Complete architect supplement: `inventory-r8-loop-retention-reachability-2026-09-17.md`, SHA-256
`cd9c12ae52a35c8f7eb10f04892cba44b01ab1b9ba82ab96f98ecc9cdba89cec`.
The root materialized the complete handoff, changing only prose bullets to numbered entries for the verifier.
The initial invocation verified all 29 pins but rejected 12 prose bullets; after formatting, 29/29 pins pass with
no drift or parse failures. Independent bounded witness review also returned PASS and verified all 29 pins.

The short request-stream override demonstrates unfinished admission. Equal task/request MaxAge alone is insufficient:
a normal model/tool-call response can persist nonterminal KV after its request, then dispatch can republish the
original task later. Request expiry can therefore precede both current-authority and replayed-task expiry.
Cold task recovery reaches reconstruction before the ordinary response/tool business-timeout check.

The source-derived witness assumes the original USER delivery remains unacknowledged and retained until dispatch
republishes; the existing durable task consumer survives offline; republication is accepted beyond any duplicate
window; and pre-outage model handling is within its business timeout. These are possible production-order conditions,
not a native 24-hour reproduction, a successful-recovery guarantee or final admission-policy approval.

The reviewer attempted refutation through terminal suppression, iteration increments, original task-before-request
ordering and business timeout. None defeats that bounded nonterminal witness. `running/Iterations=0` is compatible
with either partial birth or first-round tool work already dispatched. Do not fix the unit RED by treating zero as a
publication-state marker.

This reaches the authorized proof-only stopping point. Next is a bounded correction design at the existing admission
and task-recovery owners, preserving the partial-birth and terminal controls and accounting for actual republication.
No new state, identifier semantics, timer or recovery machinery is approved by these diagnostics. The previously
reviewed missing-publisher slice is unchanged. The frozen parent and #1311/#1312 holds remain; no commit/push,
restack, full push gate, archive, merge, closure or completion of R8 is claimed.

## Identity-absence correction draft

The owner's subsequent “continue” resumes bounded design, not a new retention or identifier policy.
The architect's complete draft is `design-r8-identity-absence-choice-2026-09-17.md`; initial SHA-256
`8cebadd8e06f092fd323791da80a036d67feb833ba08497f63034a91d039258b`.
The two unchanged, previously reviewed inventories accompany it as binding evidence appendices.
The draft recommends skipping a repeated task publication when dispatch's exact retained task proves commitment,
while preserving the caller's remaining response work. It does not fix the preserved absence regression or close R8.

Initial independent pre-owner review required two narrow corrections before approval:

1. Explicitly amend the task-only settlement clause: exact retained commitment can satisfy the prior task
   publication despite a lost PubAck. The current clause still requires synchronous PubAck, so this exception cannot
   be described as already permitted. Ordinary required publications retain their PubAck requirement.
2. Distinguish callers. Durable USER handling retries a failed required response publication. HTTP preserves its
   synchronous response and existing optional stream-mirror behavior; this slice authorizes no HTTP error-policy fix.

The reviewer confirms that skipping the extra publication neither deletes the retained task nor advances its
destination consumer. It does not repair late-created DeliverNew consumers or exhausted delivery budgets.
Origin-age and retained-fact alternatives remain direction choices with real costs, not usable implementation
predicates. No source, active specification or prior test evidence changes in this design pass.

The corrected complete draft has SHA-256
`61710f30c79c3c037f7fe8d01d9578718f4759645e5208f5672d5af41aac5acc`.
Independent PRE-OWNER DESIGN REVIEW returns PASS at that exact hash: both narrow corrections are resolved.
The owner decision request is recorded at
https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5719772486.
`status:needs-decision` marks this pending choice, not an owner ruling. No implementation or active-spec promotion
is authorized by the review. The small task-reuse change and the separate expiry-first investigation direction
await owner approval; absence classification, the preserved RED regression and R8 completion remain open.

## Owner acceptance — 2026-09-18

The owner answered “approved” to the small task-reuse implementation and bounded expiry-first investigation.
The ruling is https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5726121727.
It accepts only the reviewed task-publication exception and USER/HTTP behavior at draft hash `61710f30…`.
The dated draft remains unchanged provenance. Active contract promotion and implementation follow separately.
Expiry duration, timestamp interpretation, clock policy, continuation rules, absence classification and retention
changes are not approved. Their bounded investigation must return for review and owner acceptance before promotion.
`status:needs-decision` is cleared only for this resolved request; `status:blocked` and existing stack/landing holds
remain. No runtime completion, fresh full-suite result, push or merge is claimed by this acceptance.
