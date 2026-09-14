# Tasks: agentic-loop restart-safe settlement

## Finish line and status

Deliver #1146's supported agentic restart contract: no successful ACK for unfinished required work, recovery from
existing durable evidence, and working sequential chat. Keep the 15-subscription claim and the refusal, identity,
retention, lifecycle, and approval conditions. The owner-approved 2026-09-12 reduction withdraws research rendering,
prefix consolidation, and unknown-noncanonical-key authority poison; it does not waive canonical-current validation,
registered stream decoding, or durability before ACK.

R1's reduced dispatcher checkpoint passed complete local verification on 2026-09-12: `task check:push` (including
the full unit and Docker-backed integration race suites) and mock-provider `task e2e:agentic` (16 verification
stages, including approval after OS-process restart, cancellation, durable tool replay and process replacement).
Independent review approved the current checkpoint source, including the underlying R1 changes and exact reusable
graphview/shared-validator reviews. This is a verified draft checkpoint, not whole-#1146 completion or merge approval.

On resume, the worktree exactly matched the saved [pause checkpoint](history/rescue-pause-2026-09-12.md).
Corrections included a valid registered unsupported-payload fixture, existing-owner cancel-before-join,
owner-versus-client cancellation in activity SSE, the positive native AutoContinue caught-up barrier, and exact
audit/retired-consumer census metadata. The shutdown regression was observed failing before the correction;
the existing explicit-terminal-SSE test was retained unchanged. No new runtime owner or recovery mechanism was added.

The old 94-task checklist and execution journal are frozen in
[the historical checkpoint](history/task-checkpoint-2026-09-11.md). The mapping below accounts for every old ID.
The reconciliation itself completed no previously open task. R1 is now verified as narrowed below; its research
detour remains withdrawn by the owner, not implemented.
Group counts are not a percentage estimate of work remaining.
Accepted behavior lives in the eight capability deltas and design; historical task text is not a competing target.

Every new behavior test cites `// spec: <capability> / <Requirement heading>` from an active delta. Use RED → minimum
implementation → GREEN for a missing behavior. Reuse existing tests/evidence where applicable; distinguish unit,
seeded-checkpoint, component-replacement, and OS-process-replacement proof.

Remaining execution is limited to the already-listed evidence and minimal fixes for demonstrated failures
(owner reaffirmation, 2026-09-12). Reuse completed proofs; do not grow the checklist for hypothetical failures.
Any wider API, ownership, coordination, or storage change stops for the owner with the exact failing boundary,
why the existing primitives are insufficient, and the smallest proposed scope change. R3's separate retirement
ruling is recorded below; it authorizes no replacement mechanism.

## Completed checkpoints retained

These preserve the previously checked items at their recorded baselines, not whole-PR approval or a new test run.

- [x] P1 Accepted inventory/design checkpoints and owner rulings recorded (old 0.1–0.12).
- [x] P2 Frozen-parent setup and settlement/heartbeat owner foundation proved (old 1.1–1.7, including 1.2a).
- [x] P3 Stable task/request/execution identity, provider reuse-or-reinvoke, restricted exact reads, and producer-minted
  LoopID implemented and reviewed (old 2.1–2.4, 2.6, 3.1–3.3, 3A.1–3A.3).
- [x] P4 Registered prior-message handoff and sequential-chat replacement/provider-reuse checkpoints proved
  (old C.1, C.1a, C.2, C.3). C.4's remaining settlement proof is not included.
- [x] P5 Approval same-CallID isolation and required execution echo/admission/refusal checkpoint proved
  (old 6.4, 6.5a). The wider approval matrix remains open.
- [x] P6 Nested #1251 dead pause/signal surface removal integrated (old 7.1). Full paused-vocabulary retirement remains.
- [x] P7 Owner-selected observed DiscardNew/affected-closure direction retained (old 9.1), not admission completion.

## Closeout checklist

- The owner accepted the exact minimum operational loop-state contract on 2026-09-13 in comment `5652414046`.
  After active spec materialization, implementation resumes with the bounded first R6 slice: local API/vocabulary,
  mandatory caller/fixture adaptations and local coherence checks. Preserve published R1, existing WIP and its
  known regressions. The first local slice has now passed independent implementation review; the aligned R2 terminal
  correction follows. V4 remains historical and is not applicable verbatim. Acceptance completes no R2–R15 checkbox
  and does not revoke R3 or absorb wider #1244.

- [x] R1 Finish the minimal dispatcher by withdrawing the unpublished research rendering/prefix detour
  (old 6.7–6.13, narrowed by owner on 2026-09-12). Retain tracker/created/pending-consumer retirement, exact
  validated KV authority, immutable DTO/approval identity, ordinary completion activity, one view, canonical-current
  poison/healing, deletion/purge/expiry, lifecycle join, exact LookupLoopOwner, and existing routing/AutoContinue
  tuple/birth-gap obligations. Keep routeless terminal settlement, retention-bounded user routing and metric removal.
  Preserve the empty context_request_id exception and PendingApprovalInfo execution_id addition; no unrelated
  mapping change is implied. Core dependency closure and
  TestIntegrationApprovalRequiredResultRetriesMatchingPendingPrompt passed, retaining strict native drain checks;
  the full push gate, agentic E2E and current-checkpoint source review passed on 2026-09-12.
  Research rendering is withdrawn, not completed; no new payload behavior or shared record package is authorized.

- [x] R2: complete approval replacement/replay evidence and its remaining corrections (old 6.1, 6.1a, 6.2, 6.3, 6.5,
  6.5b–6.5d). Retain approve/modify/reject/timeout and original-deadline controls; exact execution/argument/trace
  correlation; matching-prompt PubAck; effect-free observable inapplicability; positive phase supersession before
  mutation; same-gate contention; and missing/unreadable/malformed evidence classifications.
  The 2026-09-12 bounded review reuses existing native approve/modify/reject/timeout replacement and original
  deadlines, matching-prompt replay, closed-gate/later-history replay, same-CallID isolation, observable
  inapplicability, and the explicit approval-after-OS-restart E2E. Preserve historical diagnostic-omission controls.
  Ordinal-2/repeated-CallID/equal-text coverage remains writer-based unit evidence, not native proof; the contract
  does not require every validation row to become a native test.
  `TestNewApprovalGateCannotOverwriteCancellationAfterObservation` now passes with unchanged production code:
  a real CancelLoop transition and persisted cancellation beat stale restoration through the existing revision
  check, without reopening the gate. This is seeded owner-ordering evidence, not native cancel settlement.
  Both native retained-absence regressions now pass: matching request/response absence produces durable
  `continuation_unavailable`, with matching COMPLETE_ and terminal publication before ACK and no repeated effects.
  The introduced visible-request-conflict precedence defect has a RED/GREEN control and shared-check correction;
  re-review remains open for the broader correction. `TestColdApprovalGatedResultConflictPrecedesMissingRequest`
  now has RED/GREEN evidence and independent approval for its isolated validation correction (`ad135fa160`): the
  unchanged stored-result checks run before request lookup, so a present conflict quarantines even with a missing
  request. The request-conflict counterpart, 24 refusal controls, and valid cold recovery also passed with `-race`
  (1.544s; retained log `gated-result-conflict-and-controls-green.log`, SHA-256 `65b627c1`). No terminal behavior or
  validation policy changed in this isolated fix. Broader `go test -race ./processor/agentic-loop -run 'Approval'
  -count=1` also passed (1.714s; `approval-unit-broader-after-validation.log`, SHA-256 `23346cb4`).
  **Terminal-write correction brought forward by owner approval:**
  `TestIntegrationMissingApprovalEvidenceCannotOverwriteCancellation`
  originally reproduced a terminal-path regression. After a cancellation commits revision 2 and releases process state,
  missing-request recovery reinstalls its older snapshot, publishes failure, overwrites current authority at
  revision 3, and returns an ACK decision. This is seeded-authority callback ordering with native retention and
  publication, not a native source-ACK proof. That RED preceded the reviewed correction and current GREEN recorded below.
  On 2026-09-12 the owner
  [approved bringing the existing terminal-write work forward][terminal-write-sequencing],
  limited to this demonstrated failure. Inventory and review the smallest correction in the existing owner before
  changing it; the sequencing
  approval does not select a new design. No new coordination mechanism, store, runtime, or broader terminal refactor
  is authorized. The terminal-write inventory passed independent review at hash `6ab3ea4c`.
  `decision-terminal-outcome-2026-09-12.md` records the advisory existing-result reuse alternative and its limits.
  Independent review passed hash `e1259b208c` for owner contract choice. The owner then
  [approved the saved-outcome reuse direction][terminal-outcome-ruling]: existing ordinary COMPLETE_ selects one
  terminal outcome, competing attempts validate/reuse it, and cancellation follows the same ordering before ACK.
  The bounded implementation handoff passed independent review and the owner accepted its remaining field gate
  on 2026-09-13. The applied implementation and bounded proof are recorded below; Create alone does not close R2.
  The separate validation correction is reviewed and green, not completion of the remaining R2 evidence.
  `terminal-selection-replay-2026-09-12.md` records the reviewed handoff at hash `b411304309`. The owner
  [approved the optional `LoopCompletedEvent.SyntheticDecideRequired` field][terminal-field-ruling]: it saves the
  already-computed required action that compacted history cannot always reconstruct. Eligibility policy, registered
  payload type, research, and storage placement remain unchanged. Implement that handoff with focused RED/GREEN
  evidence and independent source review; no new bucket, lock, coordinator, or additional outward surface is approved.
  On 2026-09-13 the optional field and builder stamp passed registered-codec and existing synthesis cases with
  `-race` (1.548s; `safe-checkpoint-flag-green.log`, SHA-256 `c867b3e7`). Competing saved-success/failure/cancellation
  tests first reproduced the overwrite. An edit safety check held the shared helper for exact-code review;
  incomplete caller routing was removed, restoring `component.go` byte-for-byte to `389ec626`.
  Only the approved field/builder and regression tests remain from that terminal implementation attempt.
  V4 remains a historical review-only candidate. Its post-R6 alignment handoff identifies the corrections required
  before implementation; see `terminal-selection-state-alignment-2026-09-13.md`. That historical checkpoint claimed
  no terminal GREEN or R2 completion; the later applied correction and proof are recorded below.
  The next helper fragment was rejected by the edit safety check before application. Independent exact-code review
  returned CHANGES REQUESTED because caller/revision/decision routes and other required owner changes are absent.
  See `review-terminal-fragment-2026-09-13.md`. That refusal preserved the approved first-R6 checkpoint; only bounded
  RED tests were added then. The owner subsequently [approved completing the correction as one reviewable
  patch, with independent review before application][terminal-complete-patch-ruling]. Preparation is authorized;
  application remains gated on independent exact-code approval of the complete helper/caller/test slice. The rejected
  fragment is not approved, and no indirect application is authorized. No new policy, mechanism or scope is added.
  Initial complete-code review of `c172d899cd2b9c0d` found a pre-birth admission leak. Corrected patch
  `d514d48e94b1e56f` passed independent application review and is applied locally: all 17 source files matched its
  candidate hashes. Required caller/revision routes, pre-birth Create, writer removal, protected-current checks and
  selected-result controls are present. Focused selection/pre-birth race tests and five native restart/approval
  controls pass. The first four-package race run passed agentic, dispatch and research but failed loop; it exposed
  a dropped cancellation decision and fixtures that reused already-terminal authority. The independently approved
  two-file authority-fixture repair is applied and its focused race run passes. The private cancellation-routing
  follow-up (`55e06d2c81dfc863`) passed independent application review, preserves retryable local-install collision
  handling, and is applied with all five candidate hashes matching. Full race runs now pass without exclusions for
  agentic, loop, dispatch and research. Six native follow-up controls pass on the corrected tree (37.356s);
  independent post-runtime review approves the tested core slice with no remaining HIGH/BLOCKING source finding.
  The owner [authorized the counter-plus-log diagnostic][terminal-cancel-diagnostic-ruling] for effect-free
  cancellation of already-terminal authority. The three-file diagnostic passed exact review, behavioral RED/GREEN
  and full four-package race tests; post-runtime review closes its MEDIUM finding. Eleven existing native approval
  replacement/replay cases pass on the corrected tree (197.971s). The existing mock agentic E2E passes with 16
  verification stages in 2m32.4378s, including actual approval after OS-process restart. Independent review returns
  R2 CLOSEOUT PASS and approves completing only this group; the evidence does not supply the separate R3 ruling.
  The later R3 owner choice is recorded below. Exact reviews, commands, hashes and backup locations are recorded in
  `review-terminal-complete-2026-09-13.md`; R6 and the remaining closeout groups are not completed by this R2 pass.
  This is matching ApprovalResponse recovery, not completion of R4/R5's other evidence lanes or R6 cancellation
  settlement.

[terminal-write-sequencing]: https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5646790026
[terminal-outcome-ruling]: https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5647247843
[terminal-field-ruling]: https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5651819675
[terminal-complete-patch-ruling]: https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5653214732
[terminal-cancel-diagnostic-ruling]: https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5654482198
[approval-store-retirement-ruling]: https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5654729986

- [x] R3 Resolve the existing approval mechanism ruling from R2 evidence (old 6.6). After R2 CLOSEOUT PASS, the
  owner replied **"approved retire"** and [retired only the additional approval-continuation Store plan]
  [approval-store-retirement-ruling]. Active proposal, design, capability delta and approval concept now remove
  ApprovalContinuationV1, extra Store configuration, digest, associated cleanup and deliberate AGENT-eviction
  survival obligations. Existing KV/exact-message recovery and durable `continuation_unavailable` remain, including
  confirmed absence before the approval deadline. Default approval timeout, KV-retention validation,
  DiscardNew/admission, unrelated rulings and dated provenance are preserved. No third mechanism is introduced;
  the framework Store and other payload-storage uses are not retired. This closes only R3, not a runtime or PR gate.

- [x] R4 Close loop task/response and remaining no-premature-ACK proof (old C.4, 4.1–4.3). Account for birth/lineage,
  initial request and created publication, cold reconstruction, duplicate/conflict/absence, every required durable
  write/output failure, and terminal-marker ordering. Bare terminal state alone proves no particular ToolResult.
  Keep declared transition/refusal exits for #1244, no process-absence success, and no speculative terminal cache.
  Independent **R4 CLOSEOUT PASS** on 2026-09-14 covers this bounded obligation map; see
  `review-r4-settlement-2026-09-14.md`. A native RED exposed request-first publication allowing response completion
  before the missing created event. The correction only swaps the two existing publications: created PubAck before
  initial request. No new state, owner or recovery helper. Two test helpers select requests by subject; four new
  native rows prove both publication failures and warm/cold recovery. Full loop race passes (3.212s), all seven
  selected loop native tests pass without skips (66.718s), and unchanged provider full-race/four native proofs pass.
  This closes only R4; R5/R6, final combined validation and whole-PR review remain separate.

- [x] R5 Close tool-result and completed-effect recovery proof (old 5.1–5.3). Preserve globally correlated execution
  identity, ordered partial batches, once-only iteration charging, prior exchange order, result persistence before
  downstream output, repeated publication until PubAck, and exact completed-outcome reuse without executor calls.
  Account for missing/conflicting evidence, post-effect ambiguity, executor panic, observed output bounds, and all
  replacement boundaries. Update the four named current tool requirements, including existing outcome telemetry,
  without losing unaffected scenarios; preserve #759's immutable-poison Terminate and post-execution Create
  ambiguity Quarantine policies, and the existing optional ToolResult.Name contract;
  add no claimed/in-progress ledger. Prior ordinary/direct-terminal proofs do not complete this whole group.
  Independent **R5 CLOSEOUT PASS / APPROVE**, 2026-09-14, covers the full bounded obligation map in
  `review-r5-tool-recovery-2026-09-14.md`. Existing completed-outcome proof remains valid; the one runtime correction
  supplies an omitted optional result name from its exact dispatched call before persistence and applied proof.
  Present conflicts and saved-result validation stay strict; no new state, payload field or tool-owner change.
  All eight regression rows and full loop/tools race pass; native compact replacement, terminal replay and actual
  compact producer/registered decoding pass without skips. Active specs now preserve the accepted #759 dispositions
  and complete existing telemetry requirement. This closes only R5, not R6 or the final combined/whole-PR gates.

- [ ] R6 Finish cancel/approval/verdict fast lanes and the accepted operational state contract (old 7.2–7.8).
  First implement `agentic-loop / LoopEntity has one operational state contract`: five-state vocabulary,
  private lifecycle.Transitions reuse, unchanged unknown IsTerminal behavior, local Validate/Begin/Resolve/direct
  TransitionTo behavior, StateBeforeApproval retirement and mandatory caller/fixture/documentation adaptations.
  Include cancellation's atomic local gate cleanup and local validation at process installation/replacement.
  Do not add stream-evidence prerequisites to startup hydration.
  Subsequent R2/R6 work proves stale-current/restoration protection together with observed revisions, selected
  COMPLETE reuse and cancel's required durable state/terminal PubAck. Keep unknown-signal refusal; missing/full
  waiter, conflict, panic, replacement and pre-marker proofs; one release point; lane-specific applied proof;
  approval exceptions; and nonblocking audit/graph evidence. ResponseAction/intent hints are not additional
  durable signal verbs. The earlier shared-validator correction is not completion of this group.
  The first local slice passed independent implementation review on 2026-09-13 with no remaining findings:
  `review-loop-state-implementation-2026-09-13.md` records behavioral RED/GREEN, the 39-file delta/manifest and
  the one test-assertion review correction. Agentic, dispatch and graphresearch race suites pass; the loop suite
  retained its three pre-existing terminal-selection failures. Later terminal/native evidence is recorded under R2;
  it does not claim whole-R6 completion or a new E2E result.

- [ ] R7 Close governance settlement/correlation proof (old 8.1–8.3). Cover allowed/denied/filter-error/panic/budget,
  missing/full waiters, exact retained verdicts, and replacement before/after proposal/verdict/tool publication.
  All three validation handlers propagate classified outcomes and required PubAcks. Ordinary output remains
  at-least-once with no general committed-output lookup. Match RequestID, ExecutionID and proposal fingerprint.
  If retained evidence is insufficient, report the concrete
  failpoint before proposing additional state. Policy content and framework-wide wire enforcement remain separate.

- [ ] R8 Close replay admission, first-party producer and loop-authority proof (old 9.2–9.10). Preserve local observed
  requirements, zero non-agentic lookup, zero dependent allocation on refusal, DiscardNew backpressure and queued
  source safety. Verify all six classifier/configuration surfaces and four static producers, declaration-only and
  dynamic-subject refusals, canonical wildcard coverage, registered non-Graphable TaskMessage, malformed/unregistered
  envelope refusal, and PubAck.
  Loop authority uses typed not-found/create-exists handling and observes History 10, TTL 24h, nonbinding MaxBytes
  and approval lifetime before consumers/sweeper; no reconciliation of retained drift or additional public gate.

- [ ] R9 Close cross-lane publication and source-retention evidence (old 2.5, 9.11). Every named ordinary publication
  is at-least-once and source ACK follows required PubAck; uncertain PubAck may republish. Nats-Msg-Id is only
  bounded duplicate suppression. Measure USER and TOOL retention for physical rows 1, 11, and 17; AGENT admission
  cannot stand in for those observations. Report insufficiency rather than claiming the complete tranche.

- [ ] R10 Close lifecycle/cancellation proof (old 10.1–10.3). Cover all five affected component owners and trajectory
  batching; use the exact operation context, stop admission/drain exact owners/cancel/join, and prove no work,
  mutation, publication or settlement survives Stop. Production structs retain no context. A dependency ignoring
  cancellation is a lifecycle defect, not justification for masking machinery.

- [ ] R11 BLOCKED: verify the final combined source after R1–R10 (old 11.1–11.2). Run the complete #1146-owned
  real-NATS replacement matrix and eight non-heartbeat callback proofs, race/integration, lint, tagged vet, build,
  schema/contracts, and agentic E2E including approval after process restart and the admitted first-party path.
  Existing focused greens and older whole-suite greens do not satisfy this candidate gate. No known unfixed
  required-job flake is waived by a later green run. R1's current full-suite/E2E checkpoint is recorded above;
  the final candidate after remaining R2–R10 work still requires this gate.

- [ ] R12 Reconcile user documentation, migrations, schema/examples, and PR claims (old 11.3–11.4). Explain the
  settlement/message-pump pattern and correct concepts 03/17/27, linking concept 33. Cover provider duplicate risk,
  retained-result reuse, approved continuation/storage outcome, admission/backpressure, authority/boot order,
  heartbeat/tool/producer identity changes, all removed surfaces and direct downstream migration instructions.
  Preserve #1251 provenance, implemented-by: Sol, Closes #1146, Refs #759/#1155/#1249, and no Closes #1155.
  Do not mutate sister repositories or claim #1159's staged merge is the main landing.

- [ ] R13 Obtain complete implementation and owner-requested cross-agent review; fix and re-review findings
  against the complete retained claim set (old 11.5–11.6). The reconciliation review is documentation-only.
- [ ] R14 After implementation/proof review, reconcile every modified requirement and unaffected scenario, then
  archive and synchronize all eight capability specs as the final content commit (old 11.7). Do not archive now.
- [ ] R15 Obtain the narrow final archive/spec-sync review and record the exact staged candidate and PR/base
  identity (old 11.8). Hosted checks, undraft and non-default merge follow that review under the shared protocol;
  they are not post-merge checkboxes. #1249 starts from the resulting reviewed checkpoint A.

## Obligation mapping and evidence

All old IDs appear in P1–P7 or R1–R15 above. Range endpoints are inclusive; suffix IDs are explicit. R1 is also
the home of the newly identified introduced dependency/projection corrections, not an extra research program.
Old IDs cited in dated artifacts resolve through this mapping; they do not revive superseded directions.

- Accepted rulings and baseline reviews: historical checkpoint section 0 and design's provenance references.
- Sequential chat/provider result reuse: historical checkpoint C.1–C.3, not the unfinished C.4 claim.
- Tool batches, terminal markers and rule-race integration: historical section 5; bounded source/proof approvals,
  not R4/R5 closure.
- Approval decisions/replay: historical section 6. Committed `160ad091` passed full local gates; the prior production
  E2E checkpoint is `5f70c69b`.
- R2 source/evidence locations at the R1 checkpoint:
  [bounded approval inventory](inventory-r2-approval-evidence-2026-09-12.md), baseline `5e0e2259`.
  This locates existing proofs; it is not a new test run or whole-R2 approval.
- R2 uncommitted correction checkpoint: the two retained-absence cases passed with native NATS and `-race`;
  the seeded new-gate/cancellation ordering case and existing 24 unresolved/poison controls also passed.
  Historical RED command, now superseded by the passing reviewed correction above: `go test -race -tags=integration
  ./processor/agentic-loop -run '^TestIntegrationMissingApprovalEvidenceCannotOverwriteCancellation$' -count=1 -v`.
  The separate same-input reentrant experiment was withdrawn because the delivery runner serializes those callbacks;
  it is not evidence and must not be restored as a required scenario. No full gates or push occurred for this slice.
- Unpublished dispatch source: `/private/tmp/gh1146-validator-resume.9SLJyk/final-source.sha256`, SHA-256
  `5d8e4d7ba397ef62fb7e0aa0ff843e29b376121cbe83af5e2dc20b71c619b0b0`; 61 source/generated entries, not a commit.
- Unpublished focused results: `final-handoff.md` and `results.json` beside that manifest. Mixed-view/native retention,
  matching-pending retry and focused race checks passed. Whole implementation review remains incomplete.
- Pre-rescue combined gate: `/private/tmp/gh1146-dispatch-final.Bdd1Kx/check-push.log`, SHA-256
  `4adab5744122ea365b87c53d32ec7d2aad01190c55b64eb570b04a43df32b8ac`: FAILED core→research closure. Later full
  unit/integration stages did not run; no combined-source E2E is claimed.
- Resumed combined gate: `/private/tmp/gh1146-resume.qbsIrH/check-push.log`, SHA-256
  `58fddbb65bc20f5204743344603f97673dea033247d4645cd855c2cb6af15728`: build/lint/tagged vet/schema/contracts passed;
  unit race failed `TestAuditRepositoryFullWithAbsoluteRootReportsRepositoryRelativeCandidates`,
  `TestFoundationBTargetCompleteness`, and `TestMessageLoggerShippedSubjectCensusArtifactIsCompleteAndExact`.
  Full integration did not run. This failed run is historical, not combined-candidate approval.
- Subsequent combined gate: `/private/tmp/gh1146-resume.qbsIrH/check-push-corrected.log`, SHA-256
  `b04d39fc177f0269fa1f647f55bff7dbcba65f266911b7a239acb860eced3910`: failed the existing explicit-terminal-SSE
  Stop test. Corrected the owner/client cancellation distinction; the test remained unchanged.
- Verified R1 full gate: `/private/tmp/gh1146-resume.qbsIrH/check-push-final.log`, SHA-256
  `e531b842261be76880730d37f41f0d36d1a7dd0f2ad47483cf560332578fdc22`: `task check:push` passed, including
  full unit/integration race suites. Native dispatch passed in 85.910s; native loop passed in 383.253s.
- Verified R1 E2E: `/private/tmp/gh1146-resume.qbsIrH/e2e-agentic.log`, SHA-256
  `f032d230ab26096bab9d539c04b9d77f2b8ef5b4496b4121cd0e8cf82f7886b4`:
  `AGENTIC_LLM_URL=http://mock-llm:8080/v1 task e2e:agentic` passed in 2m32s, `assertions_run=16`.
  The tier explicitly ran `walk-approval-after-restart`; this is process-replacement evidence, not a seeded test.

Local temporary paths are locators, not guaranteed durable proof. Before completing a closeout gate, supply its
reproducible in-tree test/source or durable CI evidence at the reviewed candidate; do not promote a missing local
log or a historical source hash to current verification.

## Separate ownership and landing

- #1288 owns the inherited research completion-envelope/readback/current-state mismatch. It has no selected API or
  milestone. Its producer and registered payload remain unchanged. The new research activity-rendering requirement
  is withdrawn from #1146; R1 must remove its introduced dependency without weakening existing stream validation.
- #1158 owns broader registered application-subject enforcement; #1112 owns graph-ingest validation/identity panic;
  #609 owns the observed statistical readiness residual. They are not new unchecked tasks in this change, and
  applicable required-job flake rules still apply.
- #1140 owns governance policy content; #1145 owns framework-wide pattern work; #1244 owns the later declared
  transition design. None is silently implemented or closed by this reconciliation.
- #1249 retains transferred AgentRun H.1/H.2 from exact post-#1146 checkpoint A. #1155's combined proof stays open.
  PR #1159 remains based on frozen parent `417beae5552f8f15ad3540edd7d8504c87174c13` in PR #1156. Completing and
  merging #1159 into that branch is not landing on main; #1156 still requires #1249 and combined review/proof.

## Minimum loop-state contract: owner accepted, implementation incomplete

The 2026-09-13 inventory passed independent review at SHA-256
`6f1c33a257ee6c0e6174c2cdcb95c005b67b6d426dcc7c42270dfed7970924c6` (118/118 pins, eight source fingerprints).
The exact corrected design at SHA-256
`1cf370eba73c99f1ff5d38a702d813f77dc424b32513590f6283d02e5fb5d21a`
passed independent DESIGN REVIEW and was accepted by the owner in
[comment 5652414046](https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5652414046).
The dated inventory/design/review artifacts remain unchanged.

The active target is `agentic-loop / LoopEntity has one operational state contract`, summarized in
`design.md / Accepted operational loop-state contract`. Its first implementation slice belongs to R6.
R2/R4 retain the separate durable-authority, creation, terminal-effect and settlement proofs.

Reviewed first-slice RED/GREEN covers the complete table/direct-call matrix, contradictory same-state refusal, unchanged
receivers on failure, public Begin→Validate→Resolve→Validate without identity stamping, gate cleanup on allowed
exit, terminal-field preservation, local manager cancellation and installation validity, and mandatory
caller/fixture adaptation. See `review-loop-state-implementation-2026-09-13.md`. It does not claim complete
stale-revision protection or terminal settlement.

Published R1 and all unrelated WIP remain preserved. R2 has independent CLOSEOUT PASS; R3's owner retirement is
recorded above. R4 also has independent CLOSEOUT PASS; R5–R15 remain open.
V4 is unapplied verbatim; its bounded alignment assessment and next implementation sign-off are recorded in
`terminal-selection-state-alignment-2026-09-13.md`. The R3 decision, #1249, #1288 and wider
#1244 work remain separate. Freshly provisioned-storage cold-start/E2E and replacement proofs remain required;
no unmeasured retained-deployment migration is introduced.
