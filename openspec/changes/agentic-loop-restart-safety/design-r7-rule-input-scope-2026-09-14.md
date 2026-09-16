# R7 external rule-input boundary: owner decision draft

Status: **scope-level recommendation for independent review and owner decision**. Not an implementation-ready target.

## Evidence and authority

The following independently passed inventories are incorporated unchanged:

- `openspec/changes/agentic-loop-restart-safety/inventory-r7-governance-evidence-2026-09-14.md`
  SHA `96d9c252c96d938a19629cf7c59da95da67815f6e4b9f5a2d612d2bffe65904a`
- `openspec/changes/agentic-loop-restart-safety/inventory-r7-rule-replay-2026-09-14.md`
  SHA `54e20f241c932b8e751ff9f193ba6dca1ff2e615312f2292e4cdada7e57939af`

Baseline: `c347eff487f50b93bc338d764f43ef5b5ea5e133`, with reviewed R6/R7 working changes preserved.

Owner ruling `5538906152` permits at-least-once publication. It does **not** authorize replaying arbitrary rule effects
or expanding #1146's frozen subscription scope.

## Answer: must we wait for #935?

**Not necessarily.** Publication-only governance work can have a narrower correction than #935's general
partial-action/atomicity problem.

But changing the rule callback from ACK to NAK is insufficient:

- The same source delivery retains its message ID and MatchState key.
- A failed OnEnter publication currently still persists `IsMatching=true`.
- Redelivery therefore selects WhileTrue, not the failed OnEnter action.
- Action caps increment before execution; arbitrary replay can also conflict with mutation contracts.

Evidence: message_handler.go:142,442; stateful_evaluator.go:200,234,249,328,348,423;
rule-projection-mutations/spec.md:84–89.

#935 is broader and presently unestimated. This evidence does not justify calling it a quick dependency.

## Options

| Option | Consequence and cost |
|---|---|
| **Do not change the rule input; narrow the guarantee** | No additional runtime work. Enforce mode remains fail-closed when a verdict is missing; audit mode deliberately does not wait for a verdict. Automatic proposal-to-verdict recovery cannot be promised. Requires an explicit owner-approved scope/spec reduction—not marking R7 complete against its current promise. |
| **Extend existing persisted message-match evaluation** | Propagate failures and prevent failed required actions from becoming committed match completion. Retains MatchState, transition selection and caps. Ordered partial effects still require a bounded replay contract; generic retry would collide with #935 and projection's no-replay requirement. |
| **Separately correct publication-only proposal work** | Treat an immutable proposal as work evaluated on each delivery, using existing condition/action owners rather than persisted match-cycle selection. Require durable verdict publication before source ACK. This avoids solving general action atomicity, but introduces a real, restricted rule-input contract that needs its own claim, review and acceptance. |

## Recommendation

**Do not pause for all of #935 or absorb it into #1159.** If the full governance restart guarantee is required,
separately claim the third option as a prerequisite.

Its proposed boundary is:

- Governance proposal inputs only; ordinary message rules and KV/entity rules retain their existing behavior.
- Reuse current condition evaluation and action execution. No supervisor, retry runtime, bucket, outbox or ledger.
- Admit the existing publication/approve/deny policy shapes; required publication failure stops that attempt before
  acknowledging its source.
- Redelivery may repeat already-published verdicts. It does not depend on OnEnter becoming selected again through
  persisted MatchState.
- Unsupported mixed mutation or match-cycle-dependent compositions must be refused visibly **before effects**,
  not silently retried or treated as safe.
- Best-effort verdict audit and optional `rule_events` do not become required routing consequences.

That restriction is **new behavior**, potentially breaking for existing compositions, and not something R7 already
authorized. The separate design must explicitly declare and enforce the admitted composition restriction before
effects, without an adopter "retry-safe" knob. Rule authors should declare policy; the framework should validate
the observed composition. The admission mechanism and hot-reload behavior remain unproven.

The inventory establishes why this boundary is plausible. It does **not** establish every admission/hot-reload
implementation detail. Those require the separately owned, bounded implementation design before code begins.

## Guarantee and scope reconciliation

The active governance delta's requirement—

> Every validated task, request, response, proposal, and verdict publication … receive PubAck before source ACK.

—is an **end-to-end acceptance condition spanning more than #1146's frozen fifteen subscriptions**.

For the recommended option:

- Retain that intended guarantee.
- Explicitly identify rule proposal-input settlement as a separately owned prerequisite.
- Keep R7 unchecked until its source-to-verdict proof and the existing R8 publisher/PubAck correction pass.
- Do not count an audit record or a successful publisher-only test as that proof.

For the no-change option, the owner must instead approve this reduced guarantee:

> This change establishes settlement for its enumerated consumers, not automatic recovery of rule-produced verdicts.
> In enforce mode, a missing verdict fails closed; audit mode deliberately remains non-gating.
> Proposal-to-verdict recovery after a rule-input failure remains unsupported.

Mode evidence: `processor/agentic-loop/governance_dispatcher.go:301` and `:314` take the non-gating audit path;
`:449` and `:478` handle enforce-mode timeout. The shipped `configs/agentic.json:319` selects audit mode.

## Required proof for the separate correction

Bounded acceptance must cover:

1. Required verdict publication fails, then the **same source delivery** retries and publishes before ACK.
2. Replacement before and after verdict PubAck, allowing duplicate publication.
3. Documented publish/deny ordering and fallback approval never acknowledge an incomplete required consequence.
4. Unsupported mutation/match-cycle compositions are refused before effects.
5. KV/entity evaluation, projection no-replay behavior and optional audit/notification contracts remain unchanged.

The orchestration-check skill reinforces this boundary: existing rules own policy selection and existing components
own execution; no additional progress owner is needed.

**Owner decision requested:** pursue this separately claimed prerequisite, or accept the explicitly narrower
guarantee. Neither decision requires solving all of #935 now.
