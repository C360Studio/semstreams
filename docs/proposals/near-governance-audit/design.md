# NEAR audit planning

Status: draft for independent pre-owner review; #1367 / PR #1368. Execution and owner adoption are pending.

## Problem

The owner wants to determine whether SemStreams makes Necessity, Evidence, Authority, and Recovery understandable
and executable for developers and human operators. The existing edge-triage golden and its run issue #1315 are
adjacent authority; this claim does not establish a second audit or a new v1 release gate.

## Boundary

This work prepares a documentation-only audit plan for review. It does not run the audit, change runtime behavior,
change sister repositories, or authorize implementation of findings. #1315 remains the run obligation.

## Evidence and review checkpoint

The complete companion [inventory](inventory.md) contains both mandatory inventories and the search record.
Independent INVENTORY PASS covered base `c3c65889e7525009ce50aa91e8804eba6c7daf08`, artifact SHA-256
`28149c1c49357843296703b7fce16982ce92cb36bab99721a7d99c3efb2ed712`.
The verifier counted 46 valid pins; the reviewed artifact's statement of 49 was a clerical error.
This design does not supersede the measured inventory. The review record preserves that checkpoint and corrections.

The premises are bounded and measured:

| Premise | Inventory evidence |
|---|---|
| A worked-example instrument already exists. | Golden lines 5, 58, 89, 110, 114. |
| It requires human approval but lacks a separate operator protocol. | Complete golden; roles 24–28, R5 68–70. |
| Discovery is separate from remediation authority. | Golden 117; release-candidate-proof spec 13–14. |
| #1159 cannot satisfy the literal prerequisite. | Closed without merge; linked supersession in inventory. |
| Current sequencing is #1362 → #1301 → tag. | Owner-transcribed issue1362 comment5797928199. |
| A run must use a published tag. | Golden 18; release-candidate-proof separates proof from publication. |
| Release admission is already governed. | Protocol 12; ADR-106 RC-4/RC-6 and post-v1 authorization policy. |

These are premises for an audit procedure, not findings that runtime requirements pass or fail.

## Options and recommendation

| Option | Benefit | Cost or limitation |
|---|---|---|
| Keep the current golden unchanged. | No additional exercise or protocol. | Leaves human comprehension unmeasured and the stale prerequisite unresolved. |
| Extend the existing golden. | Reuses its application, evidence, scorer, and run issue. | Adds a bounded human exercise and a documented information-boundary revision. |
| Start a separate NEAR program. | Independent scheduling and broader scenarios. | Duplicates setup and records; expands scope before observing one application. |

Recommend the bounded extension. The exact procedure belongs in the golden, not in a second operational guide.
NEAR supplies four observation questions. It creates no framework runtime, configuration surface, or score.

## Proposed artifact change

The complete proposed instrument is the [golden draft](golden-draft.md). The approved golden remains unchanged
while this proposal awaits owner adoption; an adopted procedure would live in that existing instrument.

Reconcile the obsolete prerequisite with the recorded restart/composition/tag sequence. Retain R1–R10, their
statuses, the seven baseline measures, and the builder's §9 brief. Add one separately versioned NEAR supplement
and a human who neither built the application nor read its audit evidence. Use the same application commit.

Protocol P2 explicitly excludes the new planning/review corpus and supplemental scoring instructions from the
builder's reading. This closes a knowledge leak introduced by the audit artifacts themselves. Record the protocol
version and any exposure. R1–R10 behavior stays version 1; cross-protocol comparisons are qualified, never silently
presented as equivalent. No build work moves outside the original clock to improve baseline measurements.

After baseline artifacts are sealed, the scorer prepares reference facts and expected decisions from the example's
declared policy and observable state. The operator receives the task, policy, permitted actions, and public entry
points, not the answer sheet. Approval, rejection, insufficient evidence, and replacement are separate cases.
Unknown is a valid answer where the record does not establish the outcome; missing observation is never success.
The record keeps human understanding separate from whether the runtime performed the expected transition.

The proposed supplement adds at most two hours: 30 minutes preparation, 60 minutes operator, 30 minutes scoring.
It permits no repair or second application build. The existing builder ceiling stays eight hours. Resource and
monetary caps are execution inputs, recorded before starting; absent resources produce NOT RUN, not new scope.

## Timing and disposition

Prepare and review this plan alongside current code work. After the prerequisites land, #1315 selects a published
eligible tag and records its provenance. The run evaluates that tag; it cannot block publication of the same tag.
Subsequent milestone/RC runs follow the existing cadence. No audit is launched by accepting this document.

Run evidence contains observations and limits. Subsequent owner disposition names an existing-contract defect,
a documentation/usability improvement, product responsibility, or an accepted/deferred limitation. Filing and
milestone placement follow the protocol. Findings do not automatically become blockers or implementation work.
Any approved behavioral change later gets its own bounded OpenSpec delta. ADR-106 remains unchanged.

## Proposed v1 objective, not an adopted release gate

A developer can build the documented NEAR example through supported interfaces, and a human can govern its
consequential actions and handle its demonstrated failure cases through documented surfaces, with explicit limits.

This is a candidate agentic objective for owner consideration after the baseline. It does not require closing every
NEAR finding, prove every consumer ready, or move post-v1 authorization policy into v1.

## Validation and owner decisions

Review verifies unchanged R1–R10 and baseline definitions, explicit P2 comparability limits, bounded N1 execution,
no leaked answer material, and separation of observation, owner disposition, and release authority. Link/pin and
Markdown checks apply; no new runtime tests or property harness are justified by this documentation-only change.
The communication, orchestration, payload, query, and durable-state decision skills are not triggered: this is an
audit procedure using existing surfaces, with no runtime contract or primitive being designed.

Owner decisions still required: adopt P2/N1 and its proposed time bounds, and later decide whether the measured
example warrants the candidate v1 objective. Execution still requires a selected tag, participants, and resource
caps on #1315. Independent design review and owner adoption must precede treating the supplement as operative.
