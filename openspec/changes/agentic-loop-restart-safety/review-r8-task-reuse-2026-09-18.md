# R8 retained dispatch task reuse

base: 68c14c8eb25c512e988f740cbf7ea14b6815976f

## Authority and scope

Owner ruling: https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5726121727.
Accepted design: `design-r8-identity-absence-choice-2026-09-17.md`, SHA-256
`61710f30c79c3c037f7fe8d01d9578718f4759645e5208f5672d5af41aac5acc`.
That dated artifact remains unchanged. Its task-only commitment exception is promoted to the current dispatch delta,
design, proposal and migration notes. No expiry policy, new state, API, timer or recovery mechanism is implemented.

The developer owns only two dispatch production files and two existing test files, and has released write ownership.
Independent conformance and implementation review are pending at this evidence checkpoint.

## Ruling conformance

| Accepted obligation | Implementing boundary |
|---|---|
| Reuse validated retained task without another publication | `processor/agentic-dispatch/component.go:983`, `processor/agentic-dispatch/http.go:393` |
| Existing exact lookup validates commitment | Unchanged `processor/agentic-dispatch/task_recovery.go:76` |
| USER response still required before source settlement | `processor/agentic-dispatch/component.go:994`; existing error returns to the typed delivery owner |
| HTTP synchronous response and optional mirror unchanged | `processor/agentic-dispatch/http.go:409`, `:421` and `:423` |
| No expiry policy or evidence-loss fix inferred | No loop runtime change; the preserved loop regression hash remains `774ad585…` |

Production changes are only two `if !found` guards and explanatory comments: six net additional lines.
Absence, read/conflict classifications, metrics, logs, response behavior and downstream consumer position are unchanged.

## TDD and verification

All commands use:

```sh
env GOCACHE=/private/tmp/semstreams-r7-test-cache GOFLAGS=-mod=readonly GOPROXY=off GOSUMDB=off
```

The new four-case unit matrix first failed on redundant task publication in both USER and HTTP response paths.
The existing native replay fixture then failed its changed one-task assertion: expected 1, observed 2 after
replacement and duplicate-window expiry. An earlier test-hook signature compilation error was corrected before
these intended RED runs; it is not counted as behavioral evidence.

After the two guards, focused unit GREEN passed (six top-level cases, package 1.512s) and focused native GREEN
passed (package 4.070s). Additional HTTP read/conflict assertions use existing fixtures. Final commands:

```sh
go test -race -count=1 -json ./processor/agentic-dispatch
scripts/run-integration-tests.sh ./processor/agentic-dispatch \
  -run '^TestIntegration(UserMessageReplayAfterTaskCommitKeepsOneLogicalTask|UserMessageTaskMappingConflictQuarantines|DispatchTaskPublicationPreservesMintAndAttachment)$' -json
go vet -tags=integration ./processor/agentic-dispatch
go tool revive -config revive.toml -formatter friendly ./processor/agentic-dispatch/...
```

Final results on 2026-09-18:

1. Full dispatch race: 239 top-level and 258 nested PASS, 2.285s. One unchanged skip,
   `TestHandleActivityStream_NoClient`, requires integration infrastructure. No FAIL.
2. Canonical native selection: three top-level and 13 nested PASS, zero skips, 4.043s. Replay test 0.94s.
   Original task sequence and timestamp survive unchanged. Repeated USER handling publishes its required response
   before the fixture manually ACKs the native source. HTTP returns/mirrors its response without another task.
3. Tagged vet, pinned revive, formatting and diff check are clean. No fixture, container startup or sleep was added.
4. Root verified the four source hashes and final log hashes. Combined logs contain 513 test PASS records, the one
   disclosed skip and no failures. These are not full-repository or E2E results.
5. Root's initial document checks passed: 389/389 citations and strict OpenSpec 55/55. The final rerun after all
   developer edits also passes: `task spec:properties` 391/391; `openspec validate --all --strict` 55/55.

The native fixture exercises production business handlers and real retained publications, with manual source ACK.
It does not newly prove the installed callback's settlement wiring or all R8 retention/absence obligations.

## Evidence identity

Durable evidence and before/final source snapshots:
`/Users/coby/Code/c360/semstreams-wt/gh1146-rescue-checkpoint.y3bWGc/r8-task-reuse.FG0fcv/`.

| Artifact | SHA-256 |
|---|---|
| `component.go` | `5e25553cc79102047f4684fde8ef35f585ee07fc1e98bf433a62e2dd47d5cd8c` |
| `http.go` | `b6bd7333c06afe7e7798c3d5d0df266898a7b17bface46ee3f57f2fb7d451373` |
| `task_recovery_test.go` | `985dcc5e9f5e895e79f5f8ee6f74f5db6a1a6949d11f8fc10919b095847d6888` |
| `restart_identity_integration_test.go` | `752a241adec89b48addf505a5ddf00b65f48a8ebd13d7c8ee7fc1199ba08a0f9` |
| `candidate.diff` | `a98535b9d88ba5bb5b713bb42a828dbf69536518d09138eaf455db825e362e00` |
| `r8-task-reuse-unit-full.jsonl` | `f8c1516749c9e45ad53dab16620509dfb4103106ff4889d8b1122d9d4da2849f` |
| `r8-task-reuse-native-final.jsonl` | `0b1151c01fb03268ddcd8194f6dda4a844f45c80813b837318554aa1a1c1835c` |
| `r8-task-reuse-unit-red.jsonl` | `176b691315c1033884060e6620495f80fb58dbfada9e57d094384ebded3751a4` |
| `r8-task-reuse-native-red.jsonl` | `ffa87d86df5fa235f9d9f571fa307d8e38ce8880751a1cdefaa1bda0d1351105` |

No owned test process, container or integration lock remains per the developer handoff.
HEAD/upstream remain `68c14c8e`; local changes are uncommitted. No commit, push, restack, archive, merge or closure.
Frozen #1156, held #1311/#1312, the preserved loop evidence-loss RED and final combined gates remain open.

## Separate expiry investigation

The complete inventory-only supplement is `inventory-r8-origin-age-2026-09-18.md`, SHA-256
`bb968d5526815929612331c2a668320d290cc09151b091b8cfc3af249dd517d5`; all 43 pins mechanically verify.
Independent inventory review is pending. The root materialized the architect's complete handoff, changing only
prose bullets to numbered items and typographic apostrophes for the verifier; the facts/limits are unchanged.
Existing metadata preserves age across identical bytes, but current source/task construction and continuation
semantics do not supply an accepted age-based absence classifier. No threshold or policy is selected by this fact.

## Independent verdicts

The reviewer returned three separate verdicts on 2026-09-18:

1. CONFORMANCE PASS: promoted task-only exception matches accepted `61710f30…`; USER response PubAck and HTTP
   mirror/refusal semantics remain distinct. No expiry policy was promoted.
2. IMPLEMENTATION APPROVE, no findings, for candidate `a98535b9…` and all four source hashes above. Independent
   verification confirms intended REDs, final unit/native counts, preserved sequence/time, response behavior and
   manual-ACK limitation. No runtime tests were rerun by the reviewer; the exact recorded evidence was verified.
3. INVENTORY PASS for origin-age supplement `bb968d55…`, all 43 pins. Independent metadata/constructor, timeout
   callers, continuation, HTTP identity and byte-preserving publisher checks support its stated limits. Unlike
   the architect's earlier cache-limited attempt, reviewer `gopls` WithTime/SetTimeout references succeeded.

The earlier pending-review statements identify the evidence snapshot, not the current verdict. This closes only
the bounded task-reuse implementation/review and supplemental inventory review. It does not complete R8, fix the
preserved loop RED, or approve an expiry policy. Investigation may now frame bounded options against this inventory;
any new policy still requires independent pre-owner review and owner acceptance.

## Expiry-first investigation result

The architect's complete direction draft is `design-r8-expiry-direction-2026-09-18.md`, SHA-256
`d13943cd9130b542f5549ef7a974a28af886c3611a064e8019a2c319cf7d9541`.
Independent PRE-OWNER DESIGN REVIEW returns PASS for this bounded direction, not implementation.
The reviewer attempted to refute the fresh-continuation/near-expiry authority witness: existing admission observes
existence/ownership/terminal facts, but neither remaining retention nor TimeoutAt, and does not refresh KV. A young
task can therefore arrive after older required authority expires. Current metadata alone cannot classify all three
absence cases. No new age gate is promoted or implemented.

The new owner question is https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5726318989.
It distinguishes preservation of minimal recovery facts through existing owners from an explicit end-of-recovery
refusal/fresh-submission contract. The retained-fact recommendation is a design direction with unmeasured cost,
not permission to retain unrelated AGENT history, remove TTL, create another authority or promise infinite recovery.
`status:needs-decision` marks this unresolved product choice. The previously approved task-reuse fix remains complete
locally; its review and test evidence do not depend on the next policy choice. No background agent/test work remains.

## Retained-facts design direction accepted

The owner subsequently answered “approved” to the bounded retained-facts design recommendation.
Ruling: https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5726829372.
This clears only that direction choice and authorizes inventory/design through existing owners, with explicit
storage/capacity/cleanup costs. It does not authorize a TTL change, new field/store, broad AGENT retention, new
recovery machinery or implementation. The architect resumes read-only inventory; no developer or test job runs.
The prior implementation snapshot and preserved RED are unchanged. Main has since advanced only through PBT
guidance PR #1321 to `7b3d0876`; its exact-SHA CI and container build pass. The claim remains at `68c14c8e`,
without rebase or parent advance.

## Retained-facts inventory checkpoint

The complete supplemental inventory is `inventory-r8-retained-facts-2026-09-18.md`, initially SHA-256
`0a5263afe3eaa96397046f44e2eb5e33438c7767d295191b70cd43750c071115` (54/54 pins).
Independent inventory review requested one correction: the actual generic `storage.Store`, live registry,
ObjectStore implementation and trajectory-evidence borrower were omitted by the initial narrow Store search.
The architect inspected that existing surface and supplied the bounded correction. Root materialized it without
adding a recovery owner or release policy. Corrected SHA-256:
`53fe8a90022dac6dd6a036c32a648109d2c073758c2cd2e85fed5bb4fd9dee6b`; all 71 pins verify.
The content-store owner exists; a source-settlement-aware release rule for the measured recovery facts has not
been established. Research's shared COMPLETE representation remains the existing #1288 constraint, not repair scope.
Independent corrected inventory review returns INVENTORY PASS for exact `53fe8a90…`, with 71/71 pins and no
remaining bounded findings. The design phase may proceed; no runtime implementation or runtime test is added here.
PR #1159's stop point now reflects owner direction `5726829372`; branch/upstream divergence remains zero.

## Retained-facts product-choice review

The initial complete draft `design-r8-retained-facts-2026-09-18.md`, SHA-256
`2a4bb985e5b383d73bdea9ac4841196c320d470508f0c7c38eb387c7e9393d25`, received one HIGH framing finding.
It called full-content custody the smallest coherent mechanism, bundling identity safety with successful
reconstruction after content expiry. The inventory establishes different obligations and does not prove that minimum.
The architect supplied bounded replacements; root also changed “smallest initial representation” to “one candidate
representation.” No implementation design was selected or promoted.

Corrected SHA-256 `82e6e3ceb358ac9c40b1a9efe72de252fa2164621373af417a0feb07c56aecca` receives DESIGN REVIEW PASS
**only as a pre-owner product-choice docket**. Full-content custody is an unselected higher-guarantee option.
Authority lifetime, research co-tenancy, partial-birth discrimination and task-scoped refusal remain unresolved.
The reviewer does not claim that refusal alone fixes the three absence cases or is necessarily cheaper overall.

Decision request: https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5727173168.
Root recommends safety facts plus explicit refusal when required reconstruction content is definitively unavailable,
not indefinite successful reconstruction by default. This is a recommendation, not an owner ruling.
`status:needs-decision` now marks that product choice; accepted investigation direction `5726829372` remains valid.
The PR stop point and R8 task truth agree. Queue remains 13/22; no runtime task is completed by this investigation.

Fresh document checks pass: 391/391 property citations, strict OpenSpec 55/55, diff check. No runtime/configuration
edits or runtime tests occurred. Root rechecked the exact four task-reuse source hashes and the preserved loop RED
hash above; all are unchanged. The claim stays at `68c14c8e`, upstream divergence 0/0. No developer, reviewer,
architect or test job remains running; no commit, push, restack, archive, merge or closure occurred.

## Owner accepts unavailable-history refusal

The owner answered “yes - approved” to the explicit-refusal recommendation. Ruling:
https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5727252562.
This chooses safety facts plus visible refusal when required reconstruction history is definitively unavailable,
not successful reconstruction after content expiry. Uncertain reads remain distinct from absence; valid birth,
partial birth, ordinary continuation, terminal suppression and settlement obligations remain binding.
The earlier product-choice docket at `82e6e3ce…` remains unchanged provenance, not an implementation plan.

Fresh pickup confirms claim HEAD/upstream `68c14c8e`, divergence 0/0, frozen parent `417beae5`, and unchanged main
`7b3d0876` with successful exact-SHA CI/container build. The claim's two hosted E2E Ladder checks remain successful
historical checks, not full CI or proof of local WIP. Retained-facts inventory hash `53fe8a90…` and all 71 pins verify.
Additional unrelated worktrees exist for #1323 guidance and #1317 E2E diagnostics; neither is touched.
The architect resumes read-only mechanical design using accepted evidence; no developer or runtime test job runs.
Clear only the resolved product-choice label. Existing blocked/stack/final-proof holds and the failing regression stay.

## Refusal-carrier inventory and lifetime challenge

The architect's complete refusal-carrier supplement is `inventory-r8-refusal-carriers-2026-09-18.md`, SHA-256
`b51881ee82610df1fd6c3f81a4a1757ae2c0f1b665a7bc8d76d0e9c9b298bf04`.
Independent review returns INVENTORY PASS, with 20/20 exact pins and no remaining bounded finding.
Quarantine stops the delivery owner; it is not a response to the caller. UserResponse requires routing, the loop
does not currently expose that output port, and activity SSE observes KV records rather than UserResponse.
The inventory leaves unrouted-task and SSE refusal delivery unresolved without claiming that a new payload is needed.

A bounded judge pass challenges whether non-expiring per-task/per-loop facts are necessary. Its recommendation:
the evidence establishes insufficient current age/absence handling, not the necessity of permanent bookkeeping.
Finite retention plus enforced publication ordering and reliable birth/attachment classification is viable but unproven.
In particular, the earlier equal-retention witness relied on republishing a present retained task. The accepted
task-reuse guards remove that step from both USER and HTTP. That historical witness must not be presented as a
current counterexample; removing it does not prove every finite-retention configuration safe.

The strongest case for enduring facts assumes that the same identity must remain admissible indefinitely after all
evidence disappears: forgotten execution and first birth would then be indistinguishable. The current loop delta
explicitly excludes an indefinite deduplication guarantee for arbitrary caller resubmission after expiry. The
remaining proof concerns supported broker redelivery, actual first-party republication, young attachments to older
authority, valid partial birth, and task-scoped refusal/settlement. Existing metadata and mutable StartedAt do not
provide an accepted classifier by themselves.

The judge verified supplied hashes/HEAD and opened cited ranges; no tests or live broker measurements ran.
Its two gopls reference checks failed on restricted cache loading, so it claims no reference-completeness proof.
This is a recommendation, not an owner ruling or a finite-retention implementation proof. No permanent bucket,
age threshold, publication discriminator, payload, TTL change or recovery service is selected. The architect resumes
one bounded mechanical pass against the accepted inventories and this corrected premise; no developer runs.

## Finite task-intent proposal review

The complete bounded architect handoff is `design-r8-finite-task-intent-2026-09-18.md`, initially SHA-256
`50f057a70f8b2063acfa38a017b5fb9089001910167ae4e6e0f511e24e727625`.
Independent review returns DESIGN REVIEW PASS **as a bounded contract proposal only**, not implementation.
Both dispatch paths populate LoopID for new and attached work; PriorMessages, InReplyTo and ancestry have other
meanings. The reviewer confirms two production constructor owners. One proposed required TaskMessage field,
`loop_mode: new|attach`, carries the distinction without introducing another payload type or storage owner.

The reviewer flags one MEDIUM wording finding: “the single missing semantic fact” overstates completeness.
Root replaces it with “one missing producer-known distinction”; the proposal itself still lists the other proofs.
The strongest implementation blocker is a task whose attachment authority disappears after publication: its
caller-visible refusal and settlement are not yet specified for routeless tasks or activity-SSE callers. Approving
the field alone would strand this branch. The architect is checking only whether the existing terminal record/event
can truthfully express that refusal, preserving ownership/correlation; it is not authorized to invent another carrier.

Finite retention still owes in-flight-after-source-expiry and supported-republication proof. No wire field,
runtime change, new store, TTL change or permanent record is implementation-approved. R8 remains open.

## Live-attachment product choice

The bounded carrier follow-up establishes that an existing LoopFailed outcome cannot truthfully refuse every new
attachment. A selected COMPLETE_L for task A can outlive bare loop authority; attachment task B cannot take over
that selected outcome or claim A's result. The architect frames retirement of live attachment as an alternative to
designing a distinct task-admission result contract. This is not already authorized by unavailable-history refusal.

The expanded docket at `26bd7e54…` received two product-framing findings: HIGH for promising visible refusal to every
unchanged raw-task producer, and MEDIUM for omitting loss of the live execution's non-displayed context/budget.
Root corrects both. Current SHA-256:
`ff890002b7566620e43fcc2688ba42451227c347f22bff17a2dea328427679c1`.
Independent final review returns DESIGN REVIEW PASS **only as a product-choice docket**, both findings resolved.

Owner question: https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5727538325.
Keep live attachment and design its distinct refusal contract, or retire attachment while preserving conversational
turns as new executions with supplied user/assistant history. Retirement loses reuse of the old live execution's
tool/system/working context and budget. Identifiable USER/HTTP entry points can refuse; raw-wire migration/detection
is unresolved, not a promised zero-field solution. Remaining source-lifetime/republication proof stays open either way.
Root recommends retirement only if injecting input into an already-running execution is not a product requirement.

`status:needs-decision` now names this separate product choice; prior acceptance `5727252562` remains binding.
No target-state delta, wire field, feature retirement, storage mechanism or implementation is approved here.
No agent or runtime test job remains running. Current document checks pass: 391/391 property citations, strict
OpenSpec 55/55 and diff check. Refusal-carrier inventory pins are 20/20. Claim remains `68c14c8e`, divergence 0/0;
the recorded runtime source and RED hashes are unchanged. No commit, push, restack, merge, archive or closure.

## Owner accepts live-attachment retirement

The owner answers “agree as recommended - continue.” Ruling:
https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5728438234.
This selects retirement of live attachment from reviewed docket `ff890002…`, while preserving ordinary chat as
new executions with supplied displayed history, cancel/approval controls, within-turn iterations and restart/retry.
The lost capability is live execution-context/budget reuse. The source-history refusal ruling remains binding.

Fresh pickup verifies unchanged claim HEAD/upstream `68c14c8e` (0/0), frozen parent `417beae5`, unchanged main
`7b3d0876` with successful exact-SHA CI/container build, and preserved code/RED fingerprints. The unrelated #1317
worktree advanced to `4a03e2bb`; it is not touched. Existing published-head E2E checks remain historical evidence,
not proof of local WIP. Queue is 13/22 with R11 blocked. No prior writer or test job remains active on this claim.

Only the resolved product-choice label is removed. The architect resumes a bounded inventory supplement for
retired attachment entry points, config/schema/callers and raw-wire migration behavior; no storage re-census or
runtime implementation begins. Required mechanical design/review and existing stack/proof holds remain.

## Attachment-retirement inventory gate

The bounded inventory is `inventory-r8-attachment-retirement-2026-09-18.md`, SHA-256
`e027be801c417f08f12143dc5527d2fda30ae035fa04fbac5dd066c03564de74`.
Independent review initially found one missing adjacent contract: the active/base entity-ID clauses still promise
ReplyTo/AutoContinue. The architect supplied those exact ranges; root materialized the correction without changing
the target contract. Re-review returns INVENTORY PASS, independently verifying 72/72 pins with no bounded residual.

The migration hazard is explicit: current JSON decoders ignore unknown keys, so field deletion alone can silently
turn retired targeting into new work. The lowering must preserve separate cancellation/approval targets and lineage,
same-task recovery, and ordinary new-execution chat. Arbitrary expired raw identity reuse remains outside the claimed
guarantee; no permanent detector, new task-mode field, payload, store, TTL extension or compatibility path is selected.

The architect now drafts only the accepted retirement's exact spec/migration/mechanical changes. Independent design
conformance review still precedes runtime implementation. The missing-request RED and source-lifetime proof remain
open. Document check: 391/391 property citations resolve. No new runtime tests, commit, push or landing action.

## Retirement mechanical lowering

Architect handoff `design-r8-attachment-retirement-2026-09-18.md`, SHA-256
`08741a60000d0549a5a2259ee20e495143cf55d3e81e9db893531cdc267b6f04`, receives independent DESIGN CONFORMANCE PASS.
The reviewer identifies no materially new owner decision. USER decoding can retain a private nonserialized
retired-key presence bit for existing Validate/negative-response handling without changing the shared decoder;
PubAck precedes Terminate and failed publication retries. Warm different-task conflict uses the existing fatal
correlation Quarantine rather than fabricating terminal state. Chat, explicit controls, lineage and same-task
recovery remain supported. Adjacent gate/ungated and non-authorization clauses are reconciled with spec promotion.

Root materializes the accepted lowering in current proposal/design, dispatch/loop/entity-ID deltas and migration,
with separate unchecked implementation/proof tasks under R8. The reviewer will check this promotion against the
handoff. This is not runtime completion, full R8 acceptance or merge readiness. Existing missing-request RED and
finite supported-retention obligations remain open; no production file changes belong to this documentation step.

The promoted contract receives COMBINED PROMOTION CONFORMANCE PASS after two mechanical corrections: earlier
migration advice now retires every submission ReplyTo value, and four current design producer paragraphs no longer
promise attachment echoes. The historical sequential-chat map explicitly marks superseded rows. Reviewed current
design SHA-256 `a1edfe6c7adf292735a9170caf00587760adb0f4b8e8b9930655d97dea191490`; migration SHA-256
`528d1a6ff284a66915c306046b248d053aaafcf0c95df06ee8d9a5aae51d76b7`.
Strict OpenSpec passes 55/55 and diff check passes. Property citations are currently 389/391: two existing loop tests
still cite the removed attachment requirement and require behavioral adaptation with the runtime slice, not a
mechanical citation-only green. The first bounded developer now owns input/config retirement; root reconciles
the package/concept documentation. Raw-loop rebinding retirement follows separately. No runtime gate is complete.
