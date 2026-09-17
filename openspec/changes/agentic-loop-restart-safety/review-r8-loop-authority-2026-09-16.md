# R8 loop-authority review and bounded judgment

## Inventory review

Independent INVENTORY PASS on 2026-09-16 for the bounded AGENT_LOOPS startup inventory at original SHA-256
`f3248bd511b7510d770f057ec60284e20c9a8c0d5765613331a8748d2179a5f4`.
No missing production acquisition owner or implemented admission gate was found.

Two record corrections were required: nine configuration files, not ten, and the truncated http_activity.go
checksum. The reviewer also identified two additional Bucket-only fixtures in the already inventoried helper
family, `processor/agentic-loop/loop_integration_test.go:61` and `:715`; both reach real startup.
Root corrected these records, and the reviewer approved the exact final materialization at
`4cf85988cdaa429000628afa907b6076fa1f3f54f8de79a7e1b5a4acc1358d32`. All 118 pins verify.

Approval-margin arithmetic and raw-config/port reconciliation remain unproven choices, not implementation
permission. Inventory review does not complete whole R8, waive tests or authorize another storage mechanism.

## Bounded approval-margin judgment

Evidence: `design-r8-loop-authority-lowering-2026-09-16.md`, SHA-256
`e4c9d3a907a6dbc5f80a8d7b56d00e2b02287bf35a1549bf3918107535a76a9c`, and its exact accepted inventory.
The judge answered only whether to retain a fixed approval reserve or amend admission to `0 < timeout < observedTTL`.
This is advisory input, not an owner ruling, design approval or implementation permission.

Recommendation: retain the reserve obligation, but ask the owner to define its intended nominal grace before
selecting a number. Confidence is medium. Evidence that the existing path can durably settle after loop authority
expires would change the recommendation.

The reserve establishes only `TTL - timeout >= M`: minimum nominal headroom for timeout processing, not a
guarantee of completion before expiry. The opened sweeper path publishes rejection work before a native consumer
applies it; publication can fail and retry (`processor/agentic-loop/approval_sweeper.go:115`). Structural checks
confirmed that call and the method recording RequestedAt/Timeout (`agentic/state.go:174`).

The current `continuation_unavailable` contract covers absent matching-gate evidence while reconstruction uses
current LoopEntity. Missing loop authority has a separate absence/unresolved/poison disposition. It is not proven
to produce the same durable failure after AGENT_LOOPS expiry (active loop delta, line 534). This distinction means
the stream-evidence absence contract cannot justify removing all authority headroom.

Strongest case for removing the reserve: strict-before-expiry is simple, keeps the accepted 12-hour default and
does not dress an unsupported constant as a safety guarantee. No finite reserve covers an unbounded outage.
It loses the judge's comparison because it permits arbitrarily small headroom, allowing expiry before even an
ordinary sweep and application delay. Both options use framework-owned rules and observed TTL, not a public knob.

Unproven: no measurement or contract bounds sweep delay, queued application, retries, restart duration or clock
discrepancy. Neither five seconds, twelve hours nor any other numerical margin is established by the evidence.
The record supports the reserve's purpose, not its value. No tests or runtime verification were performed.

The question prepared for the owner is what minimum nominal grace should remain between an approval deadline and
loop-state expiry, using private M and `0 < timeout <= observedTTL - M`; alternatively, whether to amend the
requirement and accept expiry before settlement during ordinary processing delay. No choice is inferred here.

The bucket-identity options were outside this judge question. They remain in the architect's bounded handoff.
No code, schema, configuration, bucket policy or research behavior changed during either read-only review.

## Independent pre-owner design review

DESIGN REVIEW PASS for the exact docket `e4c9d3a9…` and advisory record `d859f4c8…`, as an owner-decision
docket only. Neither option selection nor implementation is approved by this review.

N2 is sound when retirement is limited to the loop-side `LoopsBucket` / `loops_bucket` surface. The existing
port declaration becomes its sole identity, and the existing research agreement check observes that declaration
without changing research runtime semantics. Errors must name the retired key and canonical replacement, or the
conflicting component and bucket identities. Migration removes every loop-side occurrence of the retired key,
including default-valued occurrences, and preserves custom selection through the existing `loops` port.
Tools and stage configuration remain unchanged.

M1 is a legitimate owner choice about desired minimum nominal grace, not a request to predict framework
completion latency. Neither a numerical margin nor a settlement guarantee is established. M2 is a genuine
contract amendment accepting arbitrarily small headroom, not conformance-only lowering.

The owner must select identity policy and the margin purpose/value/boundary before a complete implementation
handoff. No tests, runtime changes or new inventory were performed for this review.

## Concrete product-limit recommendation for owner selection

The architect subsequently recommended presenting a maximum approval wait of 12 hours for this release, also
the already approved default, while retaining loop state for 24 hours. This is a new product-limit proposal,
not a margin inferred from current defaults or measured completion latency. It leaves 12 hours of nominal
grace and adds no public knob or machinery. The existing recovery/refusal contracts remain unchanged.

The strongest downside is that a longer finite setting, such as 18 hours, would fail startup. That excludes
longer delayed-review workflows; their use by adopters has not been established. If such windows are required,
the owner should decline the cap. Any accepted migration must name this limitation and must not silently
shorten configured waits.

Both bucket retirement and this concrete lifetime choice were requested from the owner and recorded in issue
comment `5695760323`. They remain pending; earlier R7 test-cost approval does not select either R8 option.

## Owner acceptance and exact lowering conformance

The owner answered "agree - both" to the two choices above, recorded in
[comment 5696610710](https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5696610710).
This supersedes the preceding historical pending status: retire only loop-side `LoopsBucket` / `loops_bucket`,
and use a maximum/default approval wait of 12h with observed loop-state TTL24h. Nominal grace is not a recovery
or settlement guarantee. The removed-key error and longer-wait refusal remain explicit, without aliases or clamping.

Independent CONFORMANCE PASS, no findings, covers the complete eight-file tracked lowering plus the new task
handoff at base `c364b87c7034426c5f677a26479b52f43789ed20`. The reviewed pre-implementation bytes are:

| File | SHA-256 |
|---|---|
| proposal.md | 40f9030cdc9a9a542569b1abe4399bfc92e9fb6e49b0c2871da5e37e1068f79e |
| design.md | c481ad223285dc1ed82cb10a01fa7c96bba032c1824b5307876cbb9dd6b6d2db |
| tasks.md | 7055bce19df7258e073d41db1470a5b7123111613de36db564628a20fc4ebc9d |
| specs/agentic-loop/spec.md | be578a937a89aa5de9ca3c92f7856dd4056a63bce3f85cd97018f16329f064a9 |
| docs/concepts/17-approval-flow.md | 1956b8fa093911088eeb0c9096401a031bb1332a89f1cc3bddaf8d9898367081 |
| docs/operations/migration-beta162-to-beta163.md | c5aa891f4d06f976fa69221f42d8de8201bcb201453ad34740b4983ab0ca61ec |
| processor/agentic-loop/README.md | 8da0045835738c273d1856e1e72c2dc56bb24c702acd4ab5b23267b7fe19ad6a |
| processor/agentic-loop/doc.go | f00c5028d84c408d7aef8f068d1407e0e6f9ea7173f98c953dcbd0409275d2ae |
| task-r8-loop-authority-2026-09-16.md | 0e61fe13fb0afe3b31ac274e9585c774204c7fe5e9b8df47f0a4ef24ebc5d937 |

Bare filenames and `specs/` paths above are relative to this change directory; other paths are repository-relative.
The subsequent task-status reconciliation records this result without changing the reviewed normative target.

The reviewer verified the owner ruling, unchanged accepted inventory/docket, source fingerprints and all nine
contract/documentation homes. The four prior acquisition scenarios remain verbatim. Typed get/create/race handling,
observed policy, admission-before-handle/dependents, Start context/rollback, nonblocking trajectory policy and
retained deadlines are preserved. The custom-port migration example uses canonical named-port replacement.

Root verification: `openspec validate agentic-loop-restart-safety --strict`, `task spec:properties` (377/377
citations resolve) and `git diff --check` pass. No runtime tests or implementation were included in this verdict.
Root then released the bounded unit-first TDD handoff. This is not implementation approval or a publication waiver.
Any added native baseline cost needs its own measured approval; the R7 exception does not carry forward.
R7/R8, combined proof, E2E, #1311/#1312, frozen-parent, archive and landing holds remain open.

## Candidate 01 implementation and focused evidence

The 27-file implementation snapshot is local/uncommitted on base `c364b87c7034426c5f677a26479b52f43789ed20`.
Source copies, manifests, commands and logs are preserved beneath
`/Users/coby/Code/c360/semstreams-wt/gh1146-rescue-checkpoint.y3bWGc/r8-loop-authority.NQb8q7/`.
The manifest `candidate-01.sha256` hashes to
`d9b366cf229abd60781a9e8e4e5dc3f0803058cc61b65b0fea4347bbe3eed5de`;
the complete WIP-relative `candidate-01.patch` hashes to
`92d0c41c344f68d30b851420352a3094cb26d093bb453bc8199667219fcd9f3e`.
Root rechecked all 27 live hashes after the temporary agent-usage interruption; none had changed.

Independent implementation review completed with one CHANGES REQUESTED finding: the explicit-field map lookup is
case-sensitive, while the subsequent standard JSON struct decoder consumes case-folded names. Consequently,
folded `approval_timeout: null` defaults silently, and folded retired `loops_bucket` is ignored after field removal.
The correction belongs only in the existing precheck, with production-entry-point regression tests. This does not
authorize a general strict decoder, compatibility alias or new configuration mechanism. No other concrete findings
were raised. Candidate 01 is not implementation-approved.

Focused evidence on this snapshot:

- Final three-package unit race run: loop 4.178s, internal/loopbucket 2.129s, graphresearch 2.714s. The independent
  reviewer counted 611 top-level PASS records and no FAIL/SKIP records in `packages-race-final.log`, SHA-256
  `53b47a521a80847ac949a6debf4420d9d6367f65a8ec612d88ab5f41820cafc5`.
- Tagged static vet and canonical schema generation exited 0. Only `schemas/agentic-loop.v1.json` changed;
  schema generation reported its existing absent-metaschema validation skip. `git diff --check` passed.
- Canonical focused native runner exited 0 on 2026-09-16, 12:08:02–12:08:19 UTC. Package time was 12.412s;
  rounded wrapper wall time was 17s, including compilation/runner overhead. Five top-level tests passed, with
  no FAIL/SKIP records. `native-measured.log` SHA-256:
  `a2f488ae2341c98676b8c6d6e0c0c575347c947fbf5774f6002764e3aa2f9ad4`.
- New `TestIntegration_LoopAuthorityAdmission` passed in 0.45s, including its one new NATS start and cleanup.
  Its six leaves cover fresh default, existing custom, History/TTL/MaxBytes refusal and concurrent fresh startup.
  Four existing controls passed: approval-timeout wire 5.15s, immutable trajectory policy 0.05s, incompatible
  trajectory degradation 0.40s and trajectory capture 2.22s.
- The run started four NATS containers: one for the new fixture, the inherited TestMain container and two existing
  control containers. One Ryuk also started. All four NATS containers explicitly terminated; the developer verified
  empty Docker state and released host lock afterward. Wrapper 54967 and runner/lock owner 54978 completed.

The native command used `GOCACHE=/private/tmp/semstreams-r7-test-cache GOFLAGS=-mod=readonly GOPROXY=off GOSUMDB=off`
with `scripts/run-integration-tests.sh -v ./processor/agentic-loop` and this exact selector:

```text
^(TestIntegration_LoopAuthorityAdmission|TestTrajectoryFactBucketIsImmutableHistoryWithoutTTL|TestExistingIncompatibleTrajectoryBucketDisablesAuditAndDegradesHealth|TestIntegration_LoopTrajectoryCapture|TestIntegration_ApprovalTimeoutSweeper_PublishesWireResponse)$
```

Earlier failed RED/adaptation attempts remain preserved, including misleadingly named `config-start-green.log`
and `focused-green.log`; neither is passing evidence. The folded-name correction requires its own RED/GREEN and
review. These focused results do not satisfy fresh full pre-push, E2E or combined R11 gates.

## Narrow native baseline-cost request — approval pending

Linked effort: [#1146](https://github.com/C360Studio/semstreams/issues/1146), claimant Codex/Sol; repository owner Coby.
The requested exception covers only the new-test baseline-increase rule in `docs/contributing/01-testing.md` for
`processor/agentic-loop/loop_authority_integration_test.go / TestIntegration_LoopAuthorityAdmission`.

Exact cost: one additional NATS fixture start, measured at 0.45s for the complete six-case top-level test on the warm
host above. This is not a measurement of a whole-package wall-clock delta or a guarantee of future timing.
The pre-existing TestMain and other control containers are reported separately, not attributed to the new test.

The evidence requires actual server policy and the public Start path, including concurrent acquisition; manager
fakes cannot establish those observations. The pure error/classification matrix remains unit-level. The six cases
use one isolated top-level fixture, separately named buckets/consumers, joined component cleanup and the canonical
helper's bounded container cleanup. The concurrent case intentionally shares only the authority it is testing.

The exception becomes invalid if the six-case matrix, resource/isolation strategy or timing strategy changes;
any additional baseline cost requires new review. Remove it if this fixture is retired or its service-boundary proof
can be replaced without losing the tested production semantics. It grants no shared-TestMain exception, blanket
package waiver, top-level/package ceiling increase, arbitrary sleep, full-gate or E2E waiver.
Owner and independent reviewer approval are both still required. The earlier R7 cost ruling does not apply.

## Candidate 02 correction and bounded implementation approval

Candidate 02 changes only `component.go` and `loop_authority_test.go` from candidate 01. The existing precheck now
uses `strings.EqualFold` for the retired loop key and explicit approval-timeout value check, matching the field
spellings consumed by the standard decoder. No compatibility alias, fallback, decoder or other runtime mechanism
was added. The intended RED showed eight failed assertions across four leaves, covering both production entry
points; folded valid `5m` and non-string refusal controls remain. RED log `folded-fields-red.log` SHA-256:
`05ceb0cb149bfcbd13f555461f4e014db11f6fd08a2395e4fb5ed27641d82ddc`.

The exact 27-file `candidate-02.sha256` manifest hashes to
`0fa81213a73d461a7bfe4bab6f4d3e5cf0d1c12cf3e6156132a482accb63593f`;
`candidate-02.patch` hashes to `0254bd6d6ddf975483a7532cdb480866e7b43bd4a3562a72ef2a2107c71d959b`.
The independent reviewer, root and developer post-run checks found all 27 live hashes unchanged.

Fresh correction evidence uses the same environment and selectors recorded above:

- Three-package race: loop 4.679s, helper 1.358s, research 2.629s; 611 top-level PASS records, no FAIL/SKIP.
  `packages-race-candidate-02.log`: `d12283197649e0ccb987beca58fafb62b72def5e817fe35dcaf571572f92bdab`.
- Tagged vet, scoped pinned revive and diff check exited 0. Generated schema is unchanged from candidate 01.
- Native runner exited 0, 2026-09-16 14:41:36–14:41:53 UTC, package 12.437s, rounded wrapper wall 17s.
  New six-case test: 0.44s; unchanged controls: 5.15s / 0.07s / 0.41s / 2.21s. No FAIL/SKIP records.
  `native-candidate-02.log`: `39513cccd633db4be7d354306782da0a3546a598ed786d3fcaec7bb7a5627c91`.
  The same four-NATS/one-Ryuk accounting applies. All NATS containers explicitly terminated; Docker was empty
  at 14:42:28 UTC. Wrapper 57579 and runner/lock owner 57589 completed, with the host lock released.

Independent bounded IMPLEMENTATION APPROVE: the sole blocking finding is closed, with no remaining code findings.
The complete candidate-01 review plus exact candidate-02 delta/evidence review cover this approval; neither is a
whole-PR approval. The developer released source ownership after the final checks.

The reviewer separately APPROVED the exact one-test baseline-cost terms in the preceding section at record hash
`669649cb719d5d97615ca677ef5a55c4cc31f49aed25bfd7af3ad86061f74cae`. Owner approval was requested asynchronously
and remains pending. The 0.44s corrected measurement does not enlarge the unchanged six-case fixture or exception.

No fresh full pre-push gate, relevant E2E, combined proof, commit/push, archive, stack change or landing is claimed.
Whole R7/R8/R11/R13 and #1311/#1312 prerequisites remain open; #1156 remains frozen. The earlier required full-gate
pass belongs to published c364, not this local source. The source is ready for those next gates once authorized.

Root staged exactly these 27 source/config/test files and ten owned documentation/specification files so new tests
participate in tracked-corpus checks. On that staged candidate, `task spec:properties` passed 385/385 citations,
strict OpenSpec validation passed 55/55 items, the entity-ID audit passed 1,302 candidates, and
`task schema:check-changes` plus `git diff --cached --check` passed. These are cheap checkpoint checks, not a fresh
`task check:push` or combined-proof pass. No runtime file changed during this record reconciliation.

## Owner cost approval and fresh preflight — 2026-09-17

The owner answered "approved" to the exact one-test baseline-cost request above; the ruling is recorded in
[comment 5710924052](https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5710924052).
Together with the existing independent reviewer approval, this clears only that cost hold. All exception terms,
expiry/removal conditions and exclusions remain unchanged. The historical pending statements above describe the
pre-ruling checkpoints, not current approval status.

Pickup confirmed unchanged HEAD `c364b87c`, zero upstream divergence, the same dedicated claim/base and all 27
candidate-02 source hashes. Prior writers released ownership. No runtime edit or additional test case accompanies
this ruling. The fresh full preflight is the next gate; older full-suite results do not approve this source.
Whole R7/R8 and combined-proof/landing holds remain open. No parent advance, restack or closure is implied.

## Fresh full preflight failure — 2026-09-17

Independent bounded materialization review APPROVED staged tree
`90c885ddadbdde1212fa4b0ed4e7818bde9fe35e` with no findings. It covered the owner-ruling reconciliation,
not a running gate result. The 27 candidate-02 implementation hashes and previously accepted target documents
remained unchanged.

The exact staged candidate then ran `task check:push` using the same readonly/offline environment above,
from 07:56:17 to 07:59:30 UTC. It FAILED: task exit 201, canonical native runner/test exit 1.
Evidence is in `prepush-approved.vGCFkl/` beneath the existing R8 bundle. `check-push.log` SHA-256:
`2ed0eeff9a6d47e662856e18c0d862adf38275ae17d3e0ccc8b3bd0458d111fe`.

Build, lint, both tagged static vet passes, schema consistency, contracts and unit race passed. Native
`TestIntegrationApprovalAfterLoopAndDispatchReplacement` failed at the post-Start empty-map assertion in
`approval_replacement_integration_test.go:620`; loop package fail-fast ended at 2.644s. The native suite did
not complete. Startup had restored the pending loop, while the test still associated omitted timeout with
untimed approval and no deadline restoration. The accepted omission-to-12h change invalidates that premise;
the existing pre-Start empty-state and retained-KV assertions must remain protected in the bounded correction.
No production change or blind rerun is authorized by this diagnosis.

Required local CI-parity extras passed: module tidy diff, pinned revive version, fixed-port/inventory/API-guard
fixtures, strict OpenSpec 55/55, property citations 385/385, entity audit 1,302 candidates, OpenAPI generator
and Linux amd64 build. Generator proof includes two explicit existing missing-metaschema skips. The nonblocking
API compatibility report remains UNAVAILABLE: offline apidiff setup failed before any package comparison.
It is not a compatibility pass or a new waiver.

All source and module/schema hashes matched after the failed gate; the working tree matched its staged candidate.
The runner and known processes completed, Docker was empty and the host lock was released. Source remains
uncommitted; full preflight, relevant E2E, combined proof and publication/landing holds remain open.

## Existing restart-fixture adaptation

The bounded correction removes only the three obsolete post-Start empty-map assertion lines. The existing
unconditional pre-Start isolation assertion and every retained-KV/deadline/correlation/settlement/effect assertion
remain unchanged. Production files, test cases, fixtures and timing are unchanged. Independent source review
APPROVED this correction; focused-evidence reconciliation remains separate from the complete pre-push gate.

Evidence: `approval-start-adaptation.g0z3TF/` in the R8 bundle. Patch SHA-256
`06098f2c4661d14538ae87e0af62e26f91621ec4462fa397ad10ca4fa943b4ea`; the 28-file candidate manifest is
`3b380ba56e3338b8f492e20e1c858df6db531c1403713cf0f94c90d23fb73587`.
Existing unit controls passed (6 top-level/27 nested, no skips), with tagged vet, pinned revive and formatting clean.
The canonical native replacement selector passed all 12 top-level tests / 13 semantic cases with no FAIL/SKIP,
08:05:12–08:08:38 UTC, package 200.628s. `native.log` SHA-256:
`a351ba6cc359d64e1e1dbd0a9655e1ea8c70bade49e4be70ab6cb74cdc5b7fa3`.
All 14 existing NATS fixtures terminated; the developer verified empty Docker, absent lock and completed owners
at 08:08:59 UTC. This fixes the observed test-contract mismatch, not a production recovery defect.
The corrected full pre-push gate and relevant agentic E2E remain required; no whole R7/R8 completion is claimed.

Independent review subsequently verified the complete focused native log and all 28 candidate hashes and
APPROVED the correction's focused proof. The next complete gate used staged tree
`5a8ad91c7da1716533b3dcf9d01cdb4fe47e1e08`, 08:10:34–08:15:38 UTC. It FAILED (task 201/native 1)
at `TestStartWithoutUsableTrajectoryBucketMarksEveryLoop_Integration`: its hand-built component supplied no
captured `loops` output before directly invoking initialization. Admission correctly returned
`loops kv-write output is required`, before the intended trajectory-degradation assertions.
Evidence: `prepush-corrected.WIxXGJ/check-push.log`, SHA-256
`2eda2bb8bc05f994cf9fc36c5e535452175330e96326e6ee30722e127f26f984`.
The loop package ended at 211.491s; no full native-suite pass or E2E is claimed. Source/module/schema and
index-to-working-tree checks remained clean. Processes, Docker and the native lock were released.
A bounded initializer-fixture caller check is required before correcting that fixture and rerunning; preserve
the trajectory degradation assertions and the production refusal, with no runtime fallback or wider scope.

The initializer caller check found nine direct test calls: eight constructor-backed, and this one hand-built
fixture missing captured ports. The direct-Start fixture subset found no additional same-class omission.
Only the failing test now uses production `NewComponent` with the same NATS client, test registry and `acme.ops`
platform. Its shared helper, collector and all degradation/terminal assertions remain unchanged. No production
code, fixture count, case or timing changed. Independent source review APPROVED; native evidence review follows.
Patch SHA-256 `639d9a9f3265668cd66215f6f49607f358c50074f6833fce2152a01d44c8590a`;
29-file manifest `fbaeebe71665ab097b03ddacf263b393ce1457d7a5e339ffb25b8ff2f437e330`.
The three existing native tests passed at 0.30s / 0.05s / 0.37s (package 4.644s), canonical runner exit 0,
08:23:52–08:24:02 UTC. Evidence: `trajectory-fixture-adaptation.c1434O/native.log`, SHA-256
`ff8b528c41eb82f00d4996e18074c2b35d3e4fc4c9a91691ec970ac295d2c81a`.
Tagged vet, pinned revive, formatting and diff checks passed. The next full gate must cover both fixture corrections.

## Verified publication checkpoint — 2026-09-17

Independent review APPROVED the trajectory fixture correction and its exact focused native proof. Both fixture
adaptations are test-only, with no added case/container/timing cost; candidate-02 production code is unchanged.

The corrected full gate used HEAD `c364b87c` plus staged tree
`984d3009842ab60d59cbad7d83f53ea0662c2624`. Evidence is under `prepush-fixtures.LYE62h/` in the R8 bundle.
The exact commands used the readonly/offline environment recorded above:

```bash
task check:push
AGENTIC_LLM_URL=http://mock-llm:8080/v1 task e2e:agentic
```

- Full pre-push: exit 0, 08:26:13–08:38:48 UTC. `check-push.log` SHA-256:
  `252e7f6d8659bb499918980b563ea005d48dfb15ff7143ef1c553006d9a0fdfb`.
- Unit/native race: 155 passing packages each; unit 145 cached, native zero cached; 20 no-test-file packages each.
  Native loop 666.461s. Plain output does not establish individual assertion/skip counts.
- Mock-backed agentic E2E: exit 0, 08:39:28–08:42:35 UTC; scenario 2m32.474s and 16 verification stages,
  including approval after process restart. `e2e-agentic.log` SHA-256:
  `cd8bf9643fb1c57747067a5dbb0b9fc4e5ed6a7473be44b5df7212a2e1012e97`.
- Source stability: all 2,253 Go-file and 36 module/schema hashes matched after full pre-push and E2E.
  Working tree matched the staged source. Later root-owned evidence/task updates do not change runtime bytes.

Full-gate evidence has independent verification with no findings. Required CI-only checks recorded above passed;
the two existing metaschema skips and unavailable nonblocking API report remain explicit, not new waivers.
Spec validation passed 55/55 with 385/385 property citations after both fixture corrections were staged.
The native runner and E2E released their owned processes, containers, volume and lock; final inspection found
no Docker containers or volumes. No paid provider, timeout override, parent advance, restack or archive occurred.

E2E observation `semstreams_agentic_loop_active_loops -1` after process replacement repeats the already recorded
[#1242 observation](https://github.com/C360Studio/semstreams/issues/1242#issuecomment-5654575077).
This checkpoint does not validate or fix that gauge. No new issue or metrics work was added.

This is publication evidence for the bounded loop-authority slice, not whole R7/R8, #1311/#1312 readiness,
R11's eventual combined proof, whole-PR review or landing approval. Preserve the frozen #1156 parent and draft PR.
