# NEAR governance audit inventory

<!-- markdownlint-disable MD013 MD033 MD038 -->
<!-- Verbatim pin grammar and search commands require these narrow formatting exceptions. -->

base: c3c65889e7525009ce50aa91e8804eba6c7daf08

Status: inventory only; independent inventory review pending.

Materialization note (2026-09-24): independent inventory review returned PASS for the original artifact,
SHA-256 28149c1c49357843296703b7fce16982ce92cb36bab99721a7d99c3efb2ed712.
The prose pin count was then corrected from 49 to 46 to match the verifier. All pins remain unchanged.
Only this provenance note and the narrow Markdown lint directives accompany that count-only correction.

Repository: SemStreams. Claim: #1367 / PR #1368. Measured on 2026-09-24 in
`semstreams-wt/codex/gh1367-near-audit-plan`; the worktree was clean when inspected. Claim HEAD contains the
problem-only planning document above the supplied `c4a79fd5` runtime baseline.

This artifact inventories the documentation and decision surfaces for a bounded audit of Necessity, Evidence,
Authority, and Recovery. It does not assess runtime conformance, conduct the golden, select a tag, propose a new
capability, or admit findings to v1. It contains no target state or implementation tasks.

## Problem and scope

The owner wants to establish whether a developer can assemble the documented worked example and a human can
understand and exercise its controls. The immediate question is how that investigation fits the existing golden,
run obligation, release decisions, and product boundary.

The measured surface is the existing golden's instructions, roles, requirements, measurements, records, and
prerequisites; the release and documentation rules constraining an audit plan; and the live issues named by those
documents. Runtime implementations and sister repositories were not inventoried in this pass.

- `docs/proposals/near-governance-audit/design.md:7` — `The owner wants to determine whether SemStreams makes Necessity, Evidence, Authority, and Recovery understandable`
- `docs/proposals/near-governance-audit/design.md:8` — `and executable for developers and human operators. The existing edge-triage golden and its run issue #1315 are`
- `docs/proposals/near-governance-audit/design.md:13` — `This work prepares a documentation-only audit plan for review. It does not run the audit, change runtime behavior,`
- `openspec/project.md:14` — `SemStreams is a **framework, not a product**. It owns primitives and contracts;`
- `openspec/project.md:33` — `not trace exhaust**. SemStreams runs the loops, so it owns the audit primitives;`
- `openspec/project.md:65` — `- **SemTeams / SemSpec** — multi-agent team coordination and spec-driven`

The continuation of the product-boundary text assigns agent personas, review surfaces, and product workflows to
SemTeams / SemSpec. The purpose text assigns audit primitives to SemStreams while assigning their consumption
to the product above. A neutral audit application and product-level human interaction therefore have distinct
responsibility boundaries already.

## 1. The claimed gap

The briefing's possible gap is human understanding and execution of governance, not absence of a worked-example
instrument. The repository already contains that instrument.

- `docs/contributing/08-golden-edge-agent.md:3` — `Version 1 — written 2026-09-14 at owner direction. Tracked by #1306. Status: READY, not yet run.`
- `docs/contributing/08-golden-edge-agent.md:5` — `This is a measurement instrument, not a product. It is rerun by a fresh agent at every milestone tag and release`
- `docs/contributing/08-golden-edge-agent.md:68` — `- **R5 Human in the loop.** The effectful tool does not execute until a human approves. The pending approval is`
- `docs/contributing/08-golden-edge-agent.md:71` — `- **R6 Result and evidence.** After completion, a non-Go client (curl) can fetch: the loop's terminal state, the`
- `docs/contributing/08-golden-edge-agent.md:74` — `- **R7 Uplink loss.** With the loop mid-flight, make the model endpoint unreachable. Record what the loop does,`
- `docs/contributing/08-golden-edge-agent.md:77` — `- **R8 Air gap.** With no search API key and no egress, ask the agent to look something up. Record whether the`

R5 continues by requiring externally observable pending approval with enough context to decide, successful
approval, and rejection without execution. R6 names tool call, tool return, and approval-decision retrieval, plus
request and service counts. R7 requires observed loss-and-restoration behavior, explicitly without requiring
store-and-forward. R8 treats fabricated results as NOT MET.

The current instrument consequently overlaps Authority, Evidence, and Recovery substantially. These observations
are requirements of an instrument, not proof that the framework meets them.

The complete golden defines builder, scorer, and owner roles. It calls for a human approval in R5 but supplies no
separate operator brief or separate comprehension-scoring procedure in that document. This is a bounded finding
about the inspected instrument, not an absence claim about every product or test.

The bounded literal search for `NEAR`, `Necessity`, `necessity`, `operator walkthrough`, and `human walkthrough`
found the new problem-only claim but no other named NEAR or walkthrough match in the searched current
documentation/specification paths. The exact search is recorded below. The initial broad case-insensitive `near`
search produced many unrelated uses and was not used to establish absence.

## 2. Current spellings and owners of the measured facts

The golden already owns the application behavior, experimental roles, friction vocabulary, results vocabulary,
and record format. #1315 owns execution of its first run.

- `docs/contributing/08-golden-edge-agent.md:24` — `- **Builder** — a fresh Opus developer session that has NOT read the v1 identity review, the issue tracker, or any`
- `docs/contributing/08-golden-edge-agent.md:26` — `- **Scorer** — a separate reviewer session that verifies the friction log against the builder's transcript and`
- `docs/contributing/08-golden-edge-agent.md:28` — `- **Owner** reads the run record. Nothing here is a ruling.`
- `docs/contributing/08-golden-edge-agent.md:50` — `Out-of-tree Go module (`edgetriage/`), depending on `github.com/c360studio/semstreams` at the pinned tag. Neutral`
- `docs/contributing/08-golden-edge-agent.md:54` — `which was used. R7 requires a real endpoint that can be made unreachable.`
- `docs/contributing/08-golden-edge-agent.md:58` — `Each is scored MET / MET WITH FRICTION / NOT MET / NOT ATTEMPTED with one sentence of evidence.`
- `docs/contributing/08-golden-edge-agent.md:64` — `- **R3 Rule.** When a station's temperature exceeds a threshold, a rule fires once per crossing (not once per`
- `docs/contributing/08-golden-edge-agent.md:89` — `| # | Requirement | Needed to know | Where the docs said (or "nowhere") | Where it was actually found | Class | Minutes lost |`
- `docs/contributing/08-golden-edge-agent.md:96` — `The scorer verifies every row against the transcript and rejects rows it cannot verify.`
- `docs/contributing/08-golden-edge-agent.md:110` — `Counts are compared run over run at their tags. No score is computed; the table is the result.`
- `docs/contributing/08-golden-edge-agent.md:114` — `Filed by the scorer as `docs/contributing/golden-edge-agent-runs/<tag>.md`: tag, date, hardware,`
- `docs/contributing/08-golden-edge-agent.md:117` — `to do about the log is design work for the architect, filed as issues with milestones.`

The whole requirements section was read. R1 is composition; R2 is ingestion and HTTP retrieval; R3 is a
deterministic threshold crossing that starts a loop; R4 supplies an effectful application tool and framework
read-only graph tools; R5–R8 are described above; R9 is runtime self-description; R10 is optional clock skew.
R3 provides a deterministic/model boundary, but the requirement does not ask the builder to justify the model's
necessity or define how its judgment will be checked.

The seven existing measures are violations, silent failures, time to first completed loop, time to first approved
effectful call, composition burden, evidence retrieval cost, and optional idle footprint. The result is a table,
not an aggregate governance score.

The record includes endpoint choice, R1–R10 results, measurements, verified friction, application-source commit,
and comparison with the prior run. It expressly excludes recommendations. This already separates observation
from decisions about remediation.

`git ls-files 'docs/contributing/golden-edge-agent-runs/*'` returned zero paths at this baseline. This establishes
only that the inspected tracked tree contains no records at the specified location. It does not establish that no
untracked, external, or unfiled execution exists.

## 3. Adjacent repository authority

These pins constrain timing, interpretation, and where a resulting plan belongs.

- `docs/contributing/08-golden-edge-agent.md:14` — `1. PR #1159 (agentic-loop restart, stacked on #1156) has squashed to main.`
- `docs/contributing/08-golden-edge-agent.md:18` — `3. A tag exists at or after both. **Every run pins to a tag, never HEAD.** Record the tag in the run record.`
- `docs/contributing/08-golden-edge-agent.md:133` — `Not a model-quality benchmark. Not a test of any sister. Not a product. Does not land in-tree until it is built on`
- `docs/contributing/08-golden-edge-agent.md:141` — `The spec is versioned in its header. A change to a requirement's behavior invalidates comparison with earlier runs`
- `docs/contributing/08-golden-edge-agent.md:142` — `and says so in the next run record. Adding a measurement does not.`
- `docs/contributing/06-openspec-change-discipline.md:9` — `- Use an OpenSpec change when behavior or an adopter-visible contract will change.`
- `docs/contributing/06-openspec-change-discipline.md:10` — `- Use a GitHub issue for sequencing, investigation, proof-only work, release`
- `.agents/protocol.md:12` — `| What gates the next tag | GitHub milestone named for the intended version | Membership is the gate: in or out; an unruled item is out. `horizon:pre-v1` means before v1.0.0, not before the next tag. |`
- `.agents/protocol.md:59` — `- **Tag:** milestone at 100% → candidate selection per `openspec/specs/release-candidate-proof/spec.md`. The`
- `docs/adr/106-beta-rc-v1-exit-criteria-and-the-two-tier-surface-freeze.md:62` — `lands in Tier 1, and getting its shape wrong after 1.0 is an incompatible change across every gateway. Per-surface`
- `docs/adr/106-beta-rc-v1-exit-criteria-and-the-two-tier-surface-freeze.md:108` — `| RC-4 | **Zero incompatible Tier 1 changes for 30 consecutive days**, with at least one active sister tracking a tag inside the window | `task api:compat` (#1246) |`
- `docs/adr/106-beta-rc-v1-exit-criteria-and-the-two-tier-surface-freeze.md:110` — `| RC-6 | No exported surface exists at RC without a walked path — a spec scenario citing it and an assertion exercising it on a booted binary — with an explicit **recorded exemption list** | mechanism is separate work |`
- `openspec/specs/release-candidate-proof/spec.md:13` — `Every release-truth finding outside approved runtime scope SHALL be recorded as an accepted limitation, a separately`
- `openspec/specs/release-candidate-proof/spec.md:14` — `approved blocker, or a deferred named program. Recording a finding SHALL NOT imply conformance or implementation`

The full ADR was read. Its authorization-policy continuation explicitly places per-surface authorization policy
post-v1 while placing the shared principal seam pre-freeze. A broad interpretation of NEAR Authority as requiring
all authorization policy before v1 would collide with that existing boundary.

RC-4 and RC-6 are existing release conditions. The golden is not sufficient by itself to establish either
repository-wide condition: it measures one application and does not replace compatibility comparison,
spec-to-assertion evidence, exemptions, or the release process.

The full release-candidate-proof specification was read. Candidate proof precedes publication and binds an exact
candidate. The golden pins to a published tag. Those are distinct evidence phases; the golden's published-tag
boundary cannot silently become a prerequisite for publishing that same tag.

`git ls-files 'openspec/changes/*' ':!:openspec/changes/archive/*'` returned no paths at this baseline. This is
the state of this inspected tree, not a claim that there are no in-flight OpenSpec changes in other claimed
worktrees. The active artifact supplied by this claim is the problem-only document read above.

## Adjacent claims

Live GitHub evidence was retrieved on 2026-09-24. Issue descriptions and historical checkpoints are not treated as
proof that their runtime requirements are implemented.

- #1367 — [NEAR audit planning](https://github.com/C360Studio/semstreams/issues/1367), OPEN. Defines documentation-only planning, reuse of #1315, an operator exercise, preservation/versioning of comparability, and no automatic runtime or release-blocker admission. Its placement is the existing `v1.0.0-rc.1` audit milestone.
- #1315 — [Golden run 1](https://github.com/C360Studio/semstreams/issues/1315), OPEN. Still names #1159 as an unchecked prerequisite, marks adopter-path documentation repairs complete through #1308 / #1309, and requires a later tag. Filing the run record is its completion obligation; subsequent design work is separate.
- PR #1159 — [Closed without merge](https://github.com/C360Studio/semstreams/pull/1159#issuecomment-5733410892), CLOSED with `mergedAt: null`. The 2026-09-18 supersession comment says #1146 became an epic with separate landing children. The literal golden prerequisite cannot now be fulfilled by merging this PR.
- #1146 — [Restart-safety parent](https://github.com/C360Studio/semstreams/issues/1146), OPEN. Its acceptance includes approval recovery using existing loop authority and exact retained request/response evidence; confirmed missing evidence produces `continuation_unavailable`. It is not a completed restart guarantee in this inventory.
- #1362 — [Restart-safety L4b](https://github.com/C360Studio/semstreams/issues/1362), OPEN. Owns the approval cold branch, verdict handling after waiter loss, terminal ownership, route-ambiguity metering, and approval-after-restart proof. Its body explicitly says no startup hydration of approval deadlines and distinguishes answering after replacement from proving deadline firing.
- #1362 — [Owner-recorded sequencing, 2026-09-23](https://github.com/C360Studio/semstreams/issues/1362#issuecomment-5797928199): `#1362 → #1301 → tag`. This is more current sequencing evidence than the golden's #1159 prerequisite. It is recorded as an owner-ruling transcription.
- #1301 — [Composition ownership](https://github.com/C360Studio/semstreams/issues/1301), OPEN. The issue proposes addressing handwritten boot/registration composition and distinguishes existing component start barriers. The golden already associates in-tree promotion of its application with this work. No implementation or completion claim was verified.
- PR #1309 — [Adopter-path documentation repair](https://github.com/C360Studio/semstreams/pull/1309), MERGED on 2026-09-14, merge commit `7698a59fa32c251924636acbaae8d89cad23b161`. This confirms the merge fact reported by #1315; it does not certify all current documentation.

The concrete overlap is one golden, one first-run tracker, and a planning claim concerning that same instrument.
The concrete conflict is the obsolete #1159 prerequisite in both the golden and #1315 versus the supersession and
current sequencing records. No replacement wording is selected in this inventory.

## 4. Consumer at birth

There is no proposed exported symbol, port, subject, bucket, configuration field, durable primitive, or
runtime-coordination primitive in the inspected claim. Category 4 therefore concerns the documentation consumers
already present: an outside builder, an independent scorer, the human responding to the example's approval
request, and the owner interpreting the resulting evidence.

The named present consumers are not hypothetical observability consumers: the golden explicitly defines the
builder/scorer/owner and requires a human approval decision. The proposed planning issue also explicitly names
developers and human operators. Their ability to consume the current instrument is inventoried below.

No same-class collision table is triggered: this bounded investigation proposes no durable, communication, or
runtime-coordination primitive. The overlapping documentation and issue owners have instead been enumerated in
categories 2 and 3.

## 5. Problem shape

The shape is an evidence-producing exercise followed by a separate disposition decision: execute a bounded
adopter path, preserve observations and provenance, then decide what the findings require.

Its closest existing instance is the golden itself, including separate builder/scorer roles, verification against
a transcript, explicit evidence categories, no aggregate score, and a run record without recommendations.

An independent documentation investigation exhibits the same separation of measurement, evidence limits, and
remediation scope:

- `docs/proposals/docs-audit-2026-09/README.md:12` — `Issue [#1302] owns this audit; remediation is separately scoped. This PR changes audit Markdown only.`

The README was read through its findings and confidence statement. It distinguishes source contradictions from
executed failures, says tutorials were not executed, and identifies supporting inventory and search records.
It is an existing example of investigation without implying repair authority.

No new reusable primitive is being established, so the pattern-establishment adoption sweep is not triggered.
Whether a later design extends the existing instrument or adopts another shape is deliberately undecided here.

## Adopter seam inventory

This inventory addresses the people using the audit instructions. It does not infer actual runtime failure modes
from documentary gaps.

### Builder

- `docs/contributing/08-golden-edge-agent.md:34` — `- `README.md`, everything under `docs/` except `docs/contributing/golden-edge-agent-runs/`, and Go doc comments`
- `docs/contributing/08-golden-edge-agent.md:37` — `- The framework's public HTTP and CLI surfaces at runtime (`semstreams catalog`, `validate`, OpenAPI, `/loops`).`
- `docs/contributing/08-golden-edge-agent.md:41` — `- `cmd/`, `examples/`, `configs/`, `test/`, `scripts/`, `taskfiles/`, `openspec/`, `.agents/`, `.claude/` except via a`
- `docs/contributing/08-golden-edge-agent.md:43` — `- Any sister repository. Any prior run record. GitHub issues, pull requests, and discussions.`
- `docs/contributing/08-golden-edge-agent.md:45` — `A violation is logged with the reason it was necessary and the run continues. **The violation count is the primary`
- `docs/contributing/08-golden-edge-agent.md:128` — `> framework or its docs; record and route around. Stop when R1–R9 are each MET, MET WITH FRICTION, or NOT MET with`
- `docs/contributing/08-golden-edge-agent.md:129` — `> evidence, or after eight hours, whichever first. Report the log, the app source, and the R-table.`

| Question | Observed obligation or gap |
|---|---|
| What must this person know? | Which tag to use; which reading paths are permitted; how a documentation link changes permission; how to log violations; R1–R10 behavior; allowed model substitutions and R7's real-endpoint requirement; evidence/friction formats; prohibition on fixing framework/docs; and the eight-hour stopping rule. |
| What happens if they do nothing? | The document does not itself prevent reading prohibited material or repairing the framework. Its declared response to a necessary violation is to log it and continue. Missing logs or contaminated prior knowledge undermine the experiment; this pass did not inspect any automation enforcing the blindfold. |
| Where do they find out? | Documentation and the supplied §9 brief. Violations are checked afterward against the transcript by the scorer, not exposed as compile or boot failures. |
| What should they have to know? | For their role as an outside adopter, the application behavior and public entry points. The experimental duties are additional obligations created by measurement. The gap is that substantial protocol knowledge accompanies the product-building task, while framework knowledge acquired during that task is itself what the instrument measures. |
| Prediction or observation? | The builder must record elapsed time and where knowledge was obtained while working. They can observe these directly. Selecting whether an unpublished or moving dependency is eligible is a different, release-state judgment; the current brief takes a supplied `<TAG>`. |

More than two protocol facts are required. That is a concrete carrying-cost finding about the instrument, not a
claim that the runtime is unusable or a recommendation to remove experimental controls.

### Scorer

| Question | Observed obligation or gap |
|---|---|
| What must this person know? | The builder's allowed information boundary; all requirement meanings; friction classes; evidence-verification rule; distinction between model substitutions and R7; measurement definitions; pinned tag and application-source commit; previous-run comparability; and the separation between observations and recommendations. |
| What happens if they do nothing? | An unverified row must be rejected under the existing instruction. Failure to verify or preserve provenance can yield an unsupported result; no executed scorer workflow was inspected. |
| Where do they find out? | The golden and builder transcript. Requirements, measurement definitions, and versioning are documentary checks. The scorer may read everything, unlike the builder. |
| What should they have to know? | The experiment's criteria and evidence necessary to judge them. They should not have to infer whether a changed requirement still permits comparison; the current document already makes behavior-change invalidation explicit. The remaining burden is interpreting and applying that distinction consistently. |
| Prediction or observation? | The scorer observes transcript and result evidence. Inferring successful behavior from absent errors or treating a requirement as MET without evidence would exceed the declared procedure. |

The scoring surface is deliberately evidence-based, but its correctness depends on a person applying several
documented distinctions. There is no measured scorer run in the tracked records examined here.

### Human operator

| Question | Observed obligation or gap |
|---|---|
| What must this person know? | R5 presumes enough context to decide an effectful `set_station_mode` call and an available approval/rejection action. R6 identifies result/evidence material available after completion. The golden does not separately enumerate the operator's briefing, permitted prior knowledge, accountable role, or comprehension criteria. |
| What happens if they do nothing? | R5 specifies approval and rejection outcomes but does not define a no-answer observation. R10 optionally observes approval under clock skew. The current issue #1362 separately records that process replacement does not re-arm approval deadlines; that is a prerequisite/contract fact, not a golden execution result. |
| Where do they find out? | R5 expects pending approval to be observable over HTTP or KV with enough context to decide. This inspection establishes the required surface, not what a real operator actually sees or understands. |
| What should they have to know? | The decision within their responsibility and the evidence supporting it. The measurable gap in the instrument is that “enough context to decide” is required without a separate operator knowledge boundary or correctness criterion. |
| Prediction or observation? | The current requirement asks for an observable pending approval and observable results. It does not establish whether an operator can distinguish a proposed action, a reported result, and an independently confirmed outcome. That distinction remains unmeasured by this inventory. |

### Owner interpreting the record

The golden explicitly reserves rulings to the owner and excludes recommendations from the run record. The
protocol separately assigns tag admission to milestone membership, while the release specification distinguishes
accepted limitations, separately approved blockers, and deferred work.

The owner must therefore distinguish observation from admission and the agentic example's scope from
repository-wide release conditions. Calling a failed observation a v1 blocker is not a mechanical consequence of
the instrument.

## Measurements and limits

The full architect contract, project context, golden, ADR-106, OpenSpec change discipline, shared protocol,
release-candidate-proof specification, and this claim's problem-only document were read. Relevant current issue
bodies and targeted supersession/owner-ruling comments were retrieved.

There are 46 repository pins in this artifact. External issue evidence is separately linked and dated. The parent
session must materialize this exact artifact, record its content hash, and run the existing inventory verifier
before independent inventory review.

The first broad search and the first large PR-history responses exceeded useful output bounds. Their unrelated or
truncated material is not used to establish absence or runtime truth. Targeted queries subsequently retrieved the
specific supersession, issue bodies, and sequencing evidence used here.

No framework execution, model call, runtime test, benchmark, operator session, release selection, sister-repository
inspection, or documentation modification was performed. No gap here establishes a needed runtime change.

## Open evidence questions

1. The golden and #1315 still contain the obsolete PR prerequisite. The replacement must account for the
   supersession and the current owner-recorded sequence; no reconciliation is selected here.
2. The tracked run-record path is empty at this baseline. Any external or unfiled run would need its own provenance
   before being treated as a baseline.
3. R5's “enough context to decide” is not accompanied by a separate operator protocol. The current instrument
   therefore does not itself establish a human-comprehension result.
4. The selected tag, available model endpoint, hardware, operator identity, and actual budget are execution facts
   not yet established by this planning inventory.
5. Any claim that all NEAR findings must close before v1 would need to reconcile with existing scope and release
   decisions, including the post-v1 authorization-policy boundary.
6. The framework mechanisms' actual behavior at the eventual tag remains unmeasured here. Current code work,
   accepted limitations, and documentation eligibility must not be inferred from historic PR checkpoints.

## Searches

All commands below were read-only and ran from the claimed worktree. Whole-document reads required by the role
contract were used for governing authority; searches located the decisive pins. No Go structural query was
needed because this inventory introduces or investigates no Go declaration, implementer, or caller.

- `git status --short` — no changes when inspected.
- `git rev-parse HEAD` — `c3c65889e7525009ce50aa91e8804eba6c7daf08`.
- `cat .agents/contracts/semstreams-architect.md` — full canonical role contract.
- `cat openspec/project.md` — full purpose, boundary, and specification discipline.
- `git grep -n -i -E 'NEAR|necessity|golden.edge|blind.build|governance.audit' -- docs/contributing docs/proposals docs/adr openspec/changes openspec/specs .agents/protocol.md` — located golden and claim; case-insensitive `near` also produced many unrelated hits; not an absence proof.
- `cat docs/contributing/08-golden-edge-agent.md docs/adr/106-beta-rc-v1-exit-criteria-and-the-two-tier-surface-freeze.md docs/contributing/06-openspec-change-discipline.md .agents/protocol.md` — governing documentary authority; golden was reread separately after combined output truncation.
- `nl -ba docs/contributing/08-golden-edge-agent.md` — complete instrument with line numbers.
- `cat docs/proposals/near-governance-audit/design.md` — full current problem-only claim.
- `cat openspec/specs/release-candidate-proof/spec.md` — full current release-proof contract.
- `git ls-files 'docs/contributing/golden-edge-agent-runs/*' 'openspec/changes/*/proposal.md' 'release/*' 'scripts/*inventory*'` — file census; returned archived changes, release list, and inventory scripts, but no run-record path.
- `git grep -n -E '\bNEAR\b|Necessity|necessity|golden-edge-agent|built blind|blind build|operator walkthrough|human walkthrough' -- README.md docs/contributing docs/basics docs/operations docs/proposals/near-governance-audit openspec/project.md openspec/specs .agents/protocol.md` — golden references and new claim matched; no separate named operator/human walkthrough match in these paths.
- `git ls-files 'docs/contributing/golden-edge-agent-runs/*'` — zero tracked paths.
- `git grep -n -E 'docs.*audit|measurement instrument|friction|owner.load|No score' -- docs/contributing openspec/specs/owner-load-gate docs/proposals/docs-audit-2026-09` — existing golden measurements and documentation-audit records; no conclusion drawn about an owner-load capability from this targeted path.
- `git grep -n -E 'framework, not a product|audit primitives|SemTeams / SemSpec|Use an OpenSpec|Use a GitHub issue|milestone named|Tag:|30 consecutive|RC-6|What this ADR does not decide|Auth lands|Per-surface|Every release-truth|Candidate selection SHALL|Long-running paid|tag SHALL|publication' -- openspec/project.md docs/contributing/06-openspec-change-discipline.md .agents/protocol.md docs/adr/106-beta-rc-v1-exit-criteria-and-the-two-tier-surface-freeze.md openspec/specs/release-candidate-proof/spec.md` — release, product-boundary, and record-ownership pins.
- `git ls-files 'openspec/changes/*' ':!:openspec/changes/archive/*'` — zero tracked active-change paths at this baseline.
- `sed -n '1,90p' docs/proposals/docs-audit-2026-09/README.md` — bounded precedent for an evidence-only documentation investigation.
- `sed -n '1,90p' scripts/inventory-verify.sh` — checked pin grammar and distinction between pin verification and completeness.
- `gh issue view 1315 --repo C360Studio/semstreams --json number,title,state,body,comments,milestone,url,updatedAt` — full small run issue; no comments.
- `gh pr view 1159 --repo C360Studio/semstreams --json number,title,state,body,comments,mergedAt,url` — historical response exceeded output bound; not used as complete evidence.
- `gh issue view 1146 --repo C360Studio/semstreams --json number,title,state,body,comments,milestone,url,updatedAt` — historical response exceeded output bound; narrowed to body below.
- `gh issue view 1362 --repo C360Studio/semstreams --json number,title,state,body,comments,milestone,url,updatedAt` — live issue and comments, including sequencing ruling; body reread separately below.
- `gh pr view 1159 --repo C360Studio/semstreams --json number,state,mergedAt,url,comments --jq '{number,state,mergedAt,url,comments:[.comments[] | select(.body | test("supersed|closed|L1|layer";"i")) | {url,createdAt,body}]}'` — still included oversized historical checkpoints; narrowed further below.
- `gh issue view 1146 --repo C360Studio/semstreams --json number,title,state,body,url,updatedAt --jq '{number,title,state,body,url,updatedAt}'` — full current issue body and state.
- `gh issue view 1362 --repo C360Studio/semstreams --json number,title,state,body,url,updatedAt` — full current issue body and state.
- `gh pr view 1159 --repo C360Studio/semstreams --json number,state,mergedAt,url,comments --jq '{number,state,mergedAt,url,supersession:[.comments[] | select(.body | startswith("## Superseded — closed without merge")) | {url,createdAt,body}]}'` — exact supersession comment, CLOSED, no merge timestamp.
- `gh issue view 1301 --repo C360Studio/semstreams --json number,title,state,body,url,updatedAt` — full current composition issue body and state.
- `gh issue view 1367 --repo C360Studio/semstreams --json number,title,state,body,url` — full planning issue.
- `gh pr view 1309 --repo C360Studio/semstreams --json number,state,mergedAt,mergeCommit,url` — documentation prerequisite merge evidence.
