# Change: Settle publication-only governance proposal inputs

## Why

A governance verdict publication failure can currently be logged while the rule consumer acknowledges its proposal.
Changing ACK to NAK alone is insufficient: persisted message-match state can prevent the failed OnEnter action from
being selected on redelivery.

This is [#1311](https://github.com/C360Studio/semstreams/issues/1311), a prerequisite of #1146's intended end-to-end
governance guarantee, outside its frozen fifteen subscriptions. It does not require pulling forward #935's general
partial-action/atomicity work.

## What Changes

This is a design-phase claim. The owner approved separately scoping publication-only governance proposal work:

- Evaluate immutable proposals per delivery using existing condition and action owners, without depending on
  persisted match-cycle selection to retry a required publication.
- Require verdict PubAck before proposal-source ACK; repeated publication remains permitted.
- Declare and visibly enforce the admitted composition before effects. Do not add an adopter retry-safety knob.
- Preserve ordinary message rules, KV/entity evaluation, projection no-replay contracts, best-effort verdict audit
  and optional rule notifications. Enforce mode remains gating; audit mode remains deliberately non-gating.

The admission mechanism, hot-reload behavior, precise implementation boundary and settlement dispositions still
require a bounded reviewed design and owner acceptance. This proposal does not approve their implementation.

No supervisor, retry runtime, bucket, outbox, ledger or generic rule-action retry is in scope.

## Accepted Scope and Evidence

The [owner direction and placement](https://github.com/C360Studio/semstreams/issues/1311#issuecomment-5666981667)
select separate scoping and claiming, not an implementation-ready contract.

Accepted scope artifact in the local #1146 checkpoint:
`agentic-loop-restart-safety/design-r7-rule-input-scope-2026-09-14.md`,
SHA-256 `b72ce0bbbc6a76b7cea0455901690713a86b9724ba0bab676b19d6fea11db306`.

Independently passed source inventories, preserved unchanged in that checkpoint:

- `inventory-r7-governance-evidence-2026-09-14.md`:
  `96d9c252c96d938a19629cf7c59da95da67815f6e4b9f5a2d612d2bffe65904a`
- `inventory-r7-rule-replay-2026-09-14.md`:
  `54e20f241c932b8e751ff9f193ba6dca1ff2e615312f2292e4cdada7e57939af`

Those inventories describe c347eff4 and its recorded working snapshot. They are local provenance, not a claim that
every pin or integration dependency already exists on main. The measured defect and source locations are also
recorded directly in #1311; this claim's baseline-specific inventory must stand on its own before design acceptance.

## Baseline and Integration Holds

The claim starts from origin/main at `7698a59fa32c251924636acbaae8d89cad23b161`.

The rule input/evaluation/state/publisher surfaces relevant to the measured replay defect are unchanged from the
inventoried source. Proposal correlation and settlement APIs are not:

- #1156 owns the unpublished semantic delivery types.
- #1159 additionally owns the no-heartbeat SettleDelivery entry point and execution-ID/fingerprint propagation.
- #1146 R8 owns the related publisher/PubAck correction.

The design must establish an explicit integration and landing sequence before implementation. Starting this claim
from main does not establish independent landability. A reviewed stacked integration may compose these claims;
do not duplicate settlement machinery, alter the frozen parent, or move another claim's code without an explicit
ownership decision.

## Scope Boundaries

This claim does not absorb #935, #1043 or #1045, change general rule atomicity, retry projection mutations, or close
#1146 R7. Full R7 acceptance still needs the combined source-to-verdict proof and its other recorded prerequisites.

Any breaking composition restriction requires migration notes and relevant agentic E2E evidence before landing.

## Current Stop Point

Open the draft claim, then finish the bounded admission/reload and integration design. No runtime implementation
or capability-delta promotion is authorized before independent design review and owner acceptance.
