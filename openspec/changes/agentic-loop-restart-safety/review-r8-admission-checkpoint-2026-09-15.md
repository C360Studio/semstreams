# R8 admission evidence and provider-ruling reconciliation

## Scope and verdicts

Pickup verified draft PR #1159 at `c347eff487f50b93bc338d764f43ef5b5ea5e133`, with no upstream divergence.
Frozen #1156 and held, main-based, design-only #1312 were not changed.
This slice changes documentation only; all eight live-matching source/test hashes still match their reviewed manifest.

The architect's deliberately incomplete evidence checkpoint is
`inventory-r8-admission-checkpoint-2026-09-15.md`, SHA-256
`4f0be2e266bf440a83ebab225a79de1e7d21f8897757f870d0e571208b6c4973`.
Root and independent reviewer verified 25/25 exact pins before the correction below.
The reviewer confirmed that it is an honest incomplete checkpoint, not full R8 INVENTORY PASS.
No runtime, horizon formula, safety margin, new deadline or new admission mechanism is approved by that review.

The shipped AGENT declaration uses DiscardOld; this is configuration evidence, not a live-server observation.
Unlimited terminal-delivery attempts defeat an attempt-count formula, but do not prove retention-scoped safety
impossible. Source/output stream relationships, evidence-age ordering and the required local recovery interval remain
unproven. Those gaps must not be replaced by guessed arithmetic or a universal deadline.

## Existing owner ruling restored

[Owner comment 5550778818](https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5550778818)
explicitly removes provider replay-horizon arithmetic and dependency on AGENT replay admission. The exact live JSON
was independently reviewed at SHA-256 `f1b15627c89e37ae8887972863bfea81f22be54711d8e2265649231f4bb2c70c`.
Its URL is the durable authority; `/private/tmp/semstreams-1146-provider-owner-ruling-5550778818.json` is only a locator.

Root reconciled the current design owner list and first-party publisher wording, plus the loop delta owner list,
concurrent-start scenario and mixed-stream scenario. The provider exception includes its response publisher;
retained-response reuse, typed absence permitting reinvocation and PubAck-before-source-ACK remain unchanged.
Other admission-dependent owners, rule-publisher obligations and lifecycle stop-admission clauses remain unchanged.
The inventory's former model-inclusion pin records the pre-correction defect; historical artifacts are not rewritten.

Independent reviewer returned APPROVE for the exact documentation correction:

| File | SHA-256 |
| --- | --- |
| `design.md` | `670853cf777d84bd43b0a6e44d03b9813cf36c83c61117f6349a1b3658ff1b56` |
| `specs/agentic-loop/spec.md` | `34d51e33dcbe9d51383f662bfc15531dd15d43f10c43ff65a95988372872b2a6` |

This restores an existing owner ruling. It is not a new policy decision or R8 implementation/design approval.

## Checks and remaining boundary

`openspec validate agentic-loop-restart-safety --strict` passes after reconciliation.
`git diff --check` passes. The reviewed live-match eight-file source manifest verifies 8/8 unchanged.
No runtime tests were rerun; previous green tests retain their previous source/evidence identity.
No commit, push, rebase, archive, merge or issue closure occurred. R7 and R8 remain unchecked.

The next evidence question is limited to the source/proposal/verdict dependency closure needed by R7 retained reuse.
It must establish actual stream resolution and evidence ordering before any executable admission requirement is
selected. Source-error propagation remains coupled to durable authority; #1311 settlement remains separate and held.

## Bounded dependency-chain follow-on

The architect subsequently answered that one source-resolution question in six read-only tool calls, without writes
or tests. This is factual follow-on evidence, not an additional inventory/design approval.

The durable source that can re-enter Propose is the loop's AgentResponse delivery, not the rule's proposed-call
delivery. The path reaches `handlers.go:1407`; proposal publication uses the canonical subject at
`governance_dispatcher.go:673`. Rule separately substitutes the verdict subject at `processor/rule/actions.go:2106`
and publishes through the existing JetStream publisher at `processor/rule/publisher.go:48`.

Shipped `configs/agentic.json` co-locates these families in AGENT, but runs governance in audit mode. Resolved input
ports can differ (`processor/agentic-loop/component.go:235` and `:962`); output stream-name declarations alone do not
reroute publication, which follows the actual server subject capture. Proposal publication currently hardcodes its
canonical subject rather than resolving the declared proposed output. Thus shipped co-location is not a general
retention invariant for every admitted composition or evidence of enforce-mode recovery.

The exact reader at `processor/agentic-loop/settlement_recovery.go:56` currently has only request/response operations.
Its returned value at `:63` contains subject/data, not publication time or sequence. Retained-verdict addressing and
readback are not implemented. Initial causal order is response, proposal, verdict; the allowed response republication
contract prevents treating that order alone as proof that every verdict is newer than every source copy.

The remaining question is the actual retained-evidence invariant for this dependency closure. Neither a finite
attempt-count formula nor a new timestamp/deadline API has been justified by these facts.

## Bounded judge recommendation — historical pending-decision checkpoint

The judge considered whether a finite verdict-retention horizon is necessary solely to authorize repeatable
governance evaluation after a successful exact lookup returns typed absence. Its recommendation is no: absence need
not prove that no earlier decision existed when a new evaluation is permitted. This is a recommendation, not a ruling.

[Owner comment 5676602813](https://github.com/C360Studio/semstreams/issues/1311#issuecomment-5676602813) explicitly
accepts evaluation of proposal redelivery against the then-active policy. The judge verified the exact comment
artifact at SHA-256 `17d1630a33c822be39fff963787f8bb545a7064a4119f777a4453fb46f85a4a6` and the accepted #1312
replacement semantics. Its initial acceptance-UNVERIFIED caveat, based on the older rule-input draft, is withdrawn.
The #1312 status-updated design remains `1c2ffd60ee4d45a5a012db1dabe0c22fe4fccee065722bd49f23bad014cddc61`;
its review record ties that status-only update to the owner-accepted `d561f666...` design.

The distinction remains binding: #1311 permits changed-policy evaluation on proposal delivery, but explicitly leaves
loop retained recovery and the existing R8 prerequisite intact. It does not itself authorize the response handler
to bypass R8's calculated horizon when issuing a fresh proposal after typed absence.

The precise owner question is whether R7 may re-propose the same correlated proposal after typed retained-verdict
absence without proving a finite verdict-retention horizon. An expired rejection could then be followed by approval,
or vice versa, under current policy. Absence would never authorize execution by itself.
DiscardNew, exact correlation and conflict refusal, failed-read retry, required PubAck before source ACK, #1311
source settlement, and separate tool-effect protection remain required. No retention-window change for another lane,
new store, timer, timestamp API, or recovery runtime is requested. The provider exception remains separate.

The owner was asked through the current session. Until an explicit answer, current requirements remain unchanged;
there is no runtime implementation, new proof, or completion claim from this recommendation.

## Owner acceptance and narrow contract reconciliation

The owner answered **approved**, recorded verbatim with the precise question at
[comment 5682070598](https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5682070598).
This supersedes the pending-decision checkpoint above. The issue's `status:needs-decision` label was removed;
its existing blocked status and stacking/combined-proof holds remain.

The architect supplied the exact five-file transcription, materialized by root in proposal, design, tasks,
and the governance and loop spec deltas. Only the finite verdict-retention horizon prerequisite for R7 re-proposal
after successful exact typed absence is removed. Matching validated verdicts are reused. Failed or unresolved reads
Retry; required-correlation conflicts Quarantine. Current policy may return a different decision after an expired
verdict. Absence supplies neither approval nor proof that no prior decision existed.

DiscardNew, required PubAck before source ACK, #1311 source-to-verdict settlement, separate tool-effect protection,
other lanes' retention requirements and other R8 obligations are unchanged. No store, timer, timestamp API,
recovery runtime, policy-version pinning or guarantee after source loss is added. The provider exception remains
separate. Source-error propagation still cannot land separately from the durable authority that makes retry safe.
R7/R8 remain unchecked; accepting the policy completes no implementation or proof gate.

The five pre-amendment files are preserved in
`/Users/coby/Code/c360/semstreams-wt/gh1146-rescue-checkpoint.y3bWGc/r8-approved.mExyAz/pre-amendment.tar.gz`,
SHA-256 `7c3224c0973ba9ffea2e2a1bd28edac0441ae050bd247e7339d2454b1f9be52a`.
Historical inventories retain their original evidence identities; their removed horizon premise is now historical.

Strict OpenSpec validation and `git diff --check` pass after materialization. The existing live-match source manifest
still verifies 8/8 unchanged. No runtime tests were rerun.

Independent CONFORMANCE review returned APPROVE with no findings after reading the full owner ruling and every
pre-amendment-to-current diff from the archive above, then checking correction propagation. Exact reviewed files:

| File | SHA-256 |
| --- | --- |
| `proposal.md` | `cebc93589125de7a0ad4cc0ffa6c55b9bd26d87017f04e67bcb19ca09a72024a` |
| `design.md` | `2f7870748831bd78cfb6b45777d90caaf540126082aa955d2c87bd774329a21d` |
| `tasks.md` | `a0cd7b624d359cb046a44e9fbb0875c77a1da44d7fd495622ecfbb00ebf38196` |
| `specs/agentic-governance/spec.md` | `7b977f50551f5dd102604ad662c5dcfaeed30271ec71186134360999b3bd93a4` |
| `specs/agentic-loop/spec.md` | `d2524d8f047e9e4f11827101ea737f4372c0169e3b7cba3d483c4148c380c0b9` |

This is approval of the owner-ruling transcription, not full R8 readiness, a runtime/API design, implementation
or replacement proof. The historical push exception does not extend.

## Retained-verdict implementation boundary — 2026-09-16

Pickup confirmed the same `c347eff487f50b93bc338d764f43ef5b5ea5e133` HEAD/upstream, 0/0 divergence, frozen parent,
and exact five reviewed contract identities above. The eight-file live-match source manifest remains unchanged.
No new runtime evidence follows from that verification. The owner's bounded-intake ruling remains in force.

The architect reused the independently reviewed `inventory-r7-retained-verdict-refresh-2026-09-15.md` at
SHA-256 `8b73908fd59f17708f9c5602fd9c4be0f4bb532f4c30bab792deade735c09a1c` and performed seven focused
investigation calls after current-contract intake. All seven measured production files match that inventory's
recorded identity except `governance_dispatcher.go`, which matches the approved live-match `85e35f61…` snapshot.
Relevant updated declarations are `matchVerdictProposal` at line 206, `verdictWaiter` at 406, and
`prepareProposedToolCall` at 612. This is a bounded implementation draft, not full conformance approval.

### Architect's private-owner mapping

1. Extend private `loopSettlementEvidenceReader` at `settlement_recovery.go:28` with one named verdict-read
   operation, implemented through existing `readExact:49`. Preserve its distinction between `ErrMsgNotFound`
   and read failure.
2. Component resolves and reads both canonical `approved.<ExecutionID>` and `rejected.<ExecutionID>` subjects
   using their respective configured input-stream facts (`config.go:421/425`). Neither stream identity nor
   typed absence may be inferred from the other.
3. Reuse `decodeVerdictPayload` (`component.go:2570`), actual-subject checking (`:2561`), and
   `matchVerdictProposal` (`governance_dispatcher.go:206`). Reconstruct expected identity with the existing
   preparation function and preserved parent-loop value. Keep optional CallID and diagnostic precedence unchanged.
4. Insert this operation into the Component-to-MessageHandler invocation as a required private per-invocation
   dependency, before Propose. `HandleModelResponse` has 67 references and exactly one production caller at
   `component.go:1554`; a private implementation can serve it while preserving the public signature. Matching
   retained calls enter the existing approved/rejected split; only successfully absent calls reach public
   Propose. Preserve original batch order.
5. Keep `VerdictPublisher`, `GovernanceDispatcher`, public constructors and setters unchanged. Standalone
   constructors remain live business helpers without source/KV settlement ownership. Normal `NewComponent`
   supplies recovery automatically, including when a custom dispatcher is installed. No nil-reader fallback
   or new adopter setter is permitted. The discarded optional built-in-dispatcher-only reader would have let
   an existing custom-dispatcher installation bypass recovery; it is not the proposed path.
6. Couple source-error propagation to recovery: failed reads and required proposal publications return errors
   through `handlers.go:1407–1413` to `component.go:1573–1575`, classified by existing
   `loopSettlementDecision:923`. Context cancellation returns its cause, not a synthetic policy rejection.
   Preserve the configured governance timeout's existing fail-closed behavior and disabled/audit semantics.

### Exact remaining disposition question

Both retained decision subjects can contain valid opposing verdicts that individually match the same proposal.
The proposal has no decision. The current requirement's explicit Quarantine applies to identity/fingerprint
mismatch (`specs/agentic-governance/spec.md:78–81`), which both records can pass. `matchVerdictProposal` does not
compare a decision. Live full-waiter Quarantine at `governance_dispatcher.go:585` concerns channel capacity,
not selection between retained records. The architect found no existing two-record selection or refusal rule.

The owner has been asked whether recovery should Quarantine without selecting either when both retained decisions
match. That is a recommendation awaiting the owner, not a ruling. Deny-wins, latest-wins, first-read-wins, and
treating this automatically as required-correlation conflict would each add an interpretation. Runtime changes
remain paused at this specific question; the approved typed-absence amendment itself is not reopened.

The independent reviewer attempted to refute the gap in two focused calls and confirmed it. Normalization at
`governance_dispatcher.go:168–181` rejects conflicting representations within one payload, not disagreement
between two independently valid messages. No examined active clause supplies an election or dual-match refusal.
This is question validation, not design/conformance approval. The exact request is recorded at
https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5694094134;
`status:needs-decision` marks this new question only. No owner answer has been recorded.

### Proposed regression boundary

- One matching retained approval or rejection plus opposite typed absence: reuse without proposal publication.
- Two successful exact typed absences: the same correlated proposal, current-policy evaluation, required PubAck.
- Either unresolved read: Retry with no re-proposal or tool publication.
- Malformed wire, subject mismatch, or proposal mismatch: existing classified refusal.
- Required publication failure or cancellation: response settlement sees the error, not synthetic denial or early ACK.
- Component replacement, mixed retained/absent batch, custom dispatcher and independent verdict-stream overrides.
- The owner-selected dual-match disposition, once recorded and reconciled.

The developer independently prepared these cases from the governance correlation, publication and registered-wire
requirements plus `All six loop input classes settle after owner-specific durable done`. Six focused fixture reads
after intake identified the existing reuse points, without code edits or test runs:

| Test file under `processor/agentic-loop/` | Existing proof/fixture to reuse |
| --- | --- |
| `live_proposal_match_test.go:17–51` | `proposalTestPublisher`, production `publishedGovernanceProposal`, matching and registered-verdict builders |
| `verdict_wire_test.go:22` | `verdictWireComponent` uses the production constructor/registry; existing refusal and diagnostic-parity controls |
| `settlement_recovery_test.go:34,98,115,129` | `settlementBucket`, request/response-only `settlementEvidence`, `settlementEnvelope`, `retainedRequest` |
| `settlement_recovery_test.go:211` | Cold-response test enters `handleResponseMessage` for actual source-disposition assertions |
| `recovery_test.go:34` | Explicit governance context propagation and cancellation/join pattern |
| `verdict_wire_integration_test.go:28` | Real publication, installed callbacks, `beforeAck`, exact-owner drain and `newTerminalMarkerProcess` |
| `fastlane_replacement_integration_test.go:209` | Native redelivery across Stop/replacement; its manual waiter is not retained-verdict recovery proof |

Optional CallID remains accepted; Reason/RuleID preserve the reviewed diagnostic precedence. Replacement proof must
enter replacement response work without manually recreating its waiter. The fixtures are a prepared test plan,
not an implemented or independently reviewed retained-recovery slice.

R8 DiscardNew and other admission duties remain open. #1311 owns proposal-source-to-verdict settlement;
tool-effect protection remains separate. A retained-read test or mock policy publisher cannot complete those
guarantees. No source edit, runtime test, commit, push, parent edit or restack occurred in this follow-on.

### Operator note and preservation

The added `Governance replay after replacement` subsection in
`docs/operations/migration-beta162-to-beta163.md:1221` independently received CONFORMANCE APPROVE, no findings.
It states the accepted target and explicitly preserves incomplete implementation/proof status. All preexisting
content is unchanged from the backed-up file at SHA-256
`2315b6ca2df1004fdb71ef39c3603774b1d4292751ce496df1024931b3cb652a`.
The reviewed current migration file is `fd256b7500e8f55c210e5f5af76fd3375e5430fcb72e64551582f71d821c69d2`.
Strict OpenSpec validation, diff checks and the added subsection's line-length check pass; no runtime tests ran.

The existing loop tree, including untracked tests, is backed up at
`/Users/coby/Code/c360/semstreams-wt/gh1146-rescue-checkpoint.y3bWGc/r7-retained-20260916.uSBSQW/loop-before.tar.gz`,
SHA-256 `01f9d83dfc40079e18c0a754e5d1893587745aaa8cb5524bb8c345db716b353d`.
The same directory holds `migration-before.md`. Earlier checkpoints remain intact.

## Dual-match ruling and finalized task — 2026-09-16

The owner answered **approved** to the exact dual-match recommendation. The question and verbatim answer are
recorded in [comment 5694233488](https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5694233488).
That ruling supersedes the pending-disposition status above. Only `status:needs-decision` was cleared; the issue
remains open with its existing blocked status and parent/prerequisite/combined-proof holds.

Root materialized the architect's minimal proposal, design, governance-spec and task reconciliation, plus a
matching operator-note sentence. Two validated opposing retained decisions that match one proposal Quarantine
the response delivery without choosing either, positively settling the source, or publishing proposal/tool work.
No precedence, timestamp comparison or additional authority is introduced. Missing/full live waiter outcomes
are unchanged; this ruling does not authorize orphan-verdict ACK.

The complete finalized architect task is `task-r7-retained-verdict-2026-09-16.md`, SHA-256
`5359f69e4d69c7cc9e91d0329d98c8af1795b1f36e0bf842164ed86df5242b1e`, based at the unchanged `c347eff4` checkpoint.
It keeps mandatory recovery at the private Component-owned response path, including custom dispatchers, and
couples exact evidence reads to required source-error propagation. Public signatures remain unchanged.
Independent task/transcription review returned CONFORMANCE APPROVE with no findings or further design choice.
The reviewer verified the exact task, supplied document/archive/source hashes, one production response caller,
and the three existing evidence-reader implementations. No tests ran during that review. Implementation was
then authorized for the four named loop production files and necessary same-package tests; root retains docs.

Reviewed transcription identities before the task-status update:

| File | SHA-256 |
| --- | --- |
| `proposal.md` | `f2d841a10484dafd896649f0d41a92daefff61b0928f59e9c377fd0f29f6d614` |
| `design.md` | `a7f77c98012e49d0f452b6035a0e6e3275de8327b635381e74ce68d6aea0c205` |
| `tasks.md` | `23b5ed8b8b227672a96a59a858a0d6542e946a6daa4c4521b9ad241e15ae8fd5` |
| `specs/agentic-governance/spec.md` | `9b9706d5017bd1dd04a3015736def1f63d07a77bf84291bbb87d0e21a4a0246a` |
| `docs/operations/migration-beta162-to-beta163.md` | `f0a498ebd9020788ec4418002e8d6f9ae1880f817ec17b06dead080721b0092e` |

This is conformance approval only, not implementation, full R7/R8, or push/merge readiness.

The six pre-reconciliation files are preserved as `before-dual-ruling.tar.gz` in the directory above,
SHA-256 `2dd8ef286cd701545acd770ecf7ec622f488e19eb2e861c068fa4875a23b1fb2`.
Strict OpenSpec validation and `git diff --check` pass. This is contract reconciliation, not runtime-test evidence.
