# Design draft: publication-only governance proposal settlement

Status: owner accepted; implementation and restacking await the published #1159 prerequisites described below.

## Evidence and scope

Incorporate `openspec/changes/governance-proposal-settlement/inventory.md` **verbatim by reference**:

- Baseline: `4039530ff6213a25b946332c876b6d1e58c86a04`.
- SHA-256: `b915b346fe72a30a2b229ff233d03001397ab990ffbac812779300923d8899cb`.
- Independent verdict: INVENTORY PASS.
- The referenced earlier replay inventories retain their recorded identities and baseline limitations.

Measured premises:

- Physical ports select messages; `Definition` has no subscription field.
- Message rules currently use persisted match transitions, which can suppress a failed action on redelivery.
- Runtime rule replacement is sequential; desired-rule persistence does not establish successful activation.
- Registered factories may implement arbitrary behavior.
- Existing approve publication uses a registered envelope; generic publish and deny have different responsibilities.
- The shared settlement types, correlation changes, and R8 publisher correction remain stacked dependencies.

This change owns only immutable governance proposal work entering the existing Rule processor. It does not repair
general rule atomicity, implement ADR-094 activation tracking, or absorb #935.

## Options

1. **Defer the correction.** Keep the proposal-input prerequisite and affected #1146 guarantee open.
   This avoids implementation now but retains the demonstrated publication-loss window.
2. **Preserve bounded publication-only composition in the existing Rule processor.**
   Admit built-in message expressions whose proposal actions are publish, approve, and deny;
   execute their existing ordered action lists per delivery, without match-cycle persistence.
   Publication failures propagate to settlement. Previously completed publications may repeat.
3. **Introduce a single-policy, first-verdict language.**
   This simplifies policy arbitration but changes rule multiplicity, approve behavior, routing authoring,
   and the documented publish-plus-deny pattern. Those changes are not necessary for durable publication.
4. **Make arbitrary rule actions replayable.**
   This reaches mutations, partial effects, and persisted execution accounting: broader #935 work.

Recommend option 2. Its additional cost over option 3 is retaining the existing multi-rule/action iteration and
propagating publication failures. It does not require a ledger, rollback, policy arbiter, or general retry framework.
Its limitation is explicit: publication settlement does not prove that an author's policies select one consistent
verdict. Existing loop-side verdict interpretation remains authoritative.

## Proposed admission contract

The existing physical input declaration identifies the lane:

- Admit a JetStream input declaring exactly `agent.toolcall.proposed.>`.
- Its stream and consumer settings still come from canonical port facts.
- A processor admitting this lane cannot also admit ordinary core-NATS or JetStream message inputs. Existing
  KV/entity inputs may coexist.
- Do not infer this lane from rule IDs, metadata, conditions, or a nonexistent subscription field.
- Inspect the actual delivered subject before ordinary message dispatch. A proposal arriving through another
  input is refused before rule effects; it cannot fall into ordinary log-and-ACK handling.
- Other messages retain their existing path and matching behavior.

Admit any number of built-in message-expression definitions on the proposal lane. Entity-scoped definitions
remain on their existing evaluation path. Their coexistence does not make them eligible for proposal execution
or automatic retry.

The proposal policy uses the built-in expression implementation directly, not a registry-selected factory. Calling
an arbitrary factory's `Validate` does not establish that its `Create` or evaluation is effect-free.

Admitted definition fields:

- Identity/descriptive fields, `enabled`, `conditions`, `logic`, descriptive `metadata`, and ordered `on_enter`.
- Conditions and action guards may inspect the immutable `$message.*` projection and literal values.
- No entity/related/state/schedule/caller references or graph/lifecycle lookups.
- No `on_exit`, `while_true`, `on_recovery`, recovery opt-in, related patterns, cron schedule/actions, or positive
  rule iteration limit.
- Cooldown must be absent or zero. `fire_every_n_events` may only be absent, zero, or one.
- Action firing caps are not admitted: they would falsely imply persisted execution accounting on this lane.

Each proposal definition may use its existing ordered OnEnter list containing only:

- publish, with its existing Subject and Properties;
- approve, with its existing Subject and Reason;
- deny, with its existing Reason and local short-circuit consequence.

Retain existing When guards and descriptive action IDs. Refuse mutation, dispatch, iteration, or other action
types before effects. Match-cycle lists, persisted firing limits, recovery controls, and nonzero cooldown remain
outside this lane. No retry-safety flag is introduced.

Required publication targets must resolve through the integrated R8 declared-output/PubAck owner.
A core-NATS fallback cannot satisfy a required proposal publication. Do not add another output classifier here.

Preserve the documented publish-rejection-then-deny composition and existing correlation templates.
Main's current generic-publish implementation is not evidence that this public composition must be retired.
Its integration must use #1159's accepted correlation/wire contract and R8's publication consequence.
This change neither invents a second serializer nor claims those parent prerequisites are already implemented.

## Per-delivery behavior and ordering

1. Decode through the existing registered boundary and obtain the immutable proposal projection.
2. Take one snapshot of the currently admitted proposal definitions under the existing processor lock.
3. Evaluate each built-in expression against this delivery without shared match/cooldown state or StateTracker.
4. For each matching rule, walk OnEnter in declared order and apply its existing When guards.
5. Invoke the existing admitted action owner. Propagate publication failure immediately to settlement.
6. Complete only after every required publication actually attempted by successful policy execution has PubAck.

Preserve existing ordering boundaries: actions are ordered within one rule; no new priority or precedence between
rules is introduced. Approve remains permissive. Deny stops the remaining actions of its own rule, not all other
matching rules. This is not a new deny-overrides or first-verdict policy engine.

A retry reevaluates the snapshot active on the next attempt and may repeat earlier successful publications.
Different matching rules may publish different verdicts. This correction does not introduce arbitration or
change the loop's accepted interpretation/correlation contract.

No matching rule, no selected action, or policy execution that owes no verdict publication may Complete without
a verdict. That means completed evaluation, never approval. Bare deny retains its existing structural/audit meaning;
it is not silently converted into a routing publication.

If required publications succeed but none provides a usable terminal verdict, the loop's existing enforce-mode
timeout/refusal remains the observable result. Audit mode remains nongating. Counters must not imply a verdict
was published merely because evaluation completed.

Keep the existing settlement table: transient required-publication failure retries; deterministic poison terminates;
successful or publication-free evaluation completes. No mutation or external-effect retry is admitted.

## Existing owners and narrow code changes

Reuse the existing built-in expression and ActionExecutor owners, including publish, approve, deny, and their
existing wire/correlation contracts. Change proposal-path error propagation and settlement, not the action language.

Do not add a new approve/reject publisher, derive replacement routing semantics, retire publish-plus-deny, or
change ordinary approve/deny behavior.

Audit remains best-effort. Optional notifications retain their existing contract. Neither audit nor an optional
notification substitutes for required publication PubAck. Proposal deliveries do not enter the stateful evaluator
or write persisted match/action counters.

## Settlement

| Outcome | Consequence |
| --- | --- |
| No required publication selected | Complete; no publication owed and no approval implied |
| All required publications receive PubAck | Complete; no usable verdict still means no approval |
| Transient required-publication failure, including uncertain acknowledgement | Retry; repeated publication is accepted |
| Cancellation before successful completion | Retry under the existing lifecycle/settlement contract |
| Malformed input or deterministic correlation/condition data failure | Terminate with the existing classified diagnostic |
| Unsupported policy at boot/reload | Refuse activation before effects; not a source-delivery retry |
| Unexpected internal/control failure | Follow the existing settlement and exact-consumer lifecycle contract; no fallback ACK |

A settlement-method failure must not trigger a second, different terminal method. Integration proof must cover the
existing owner's required response to that failure.

No automatic retry is added for external mutations or unknown tool effects: those actions cannot be admitted here.

## Boot and reload

Boot validates the complete proposal portion before rule construction or subscription installation.
Unsupported proposal declarations fail before effects.

Reload follows prepare-then-install using the existing maps and lock:

1. Decode and validate the complete desired proposal portion without lossy action conversion.
   Malformed actions, unknown fields, unsupported actions, or a failed required KV read reject the candidate.
2. Prepare all built-in proposal replacements before mutating the active proposal portion.
3. Apply unrelated entity changes through their existing owner and failure behavior.
4. Only after that work succeeds, install the prepared proposal portion together under the existing lock.

**If unrelated entity construction or application fails, the proposal portion remains the previous active portion.**
Unrelated entity entries already changed by the existing sequential path may remain changed. There is no rollback
claim for those entries.

The deferred proposal commit must include every rule ID belonging to either the old or candidate proposal portion.
An ID moving between proposal and entity evaluation must not overwrite or remove an old proposal entry early.
Prepare that replacement first and defer its map changes to the same final commit. This is local candidate data,
not a second active registry or durable generation record.

The final commit updates the corresponding entries in the existing rules, ruleDefinitions, ruleConfigs, and
counter maps under their existing lock. No proposal subset becomes visible before that commit.

This is a narrow proposal-activation guarantee. It does not make unrelated entity/cron construction pure,
make general replacement atomic, or implement ADR-094 activation tracking.

A failed reconcile reports failure while the old proposal policy remains usable. A saved desired definition is
not reported as confirmed active. No activation bucket, receipt system, supervisor, or retry ledger is added.

## Replacement and replay semantics

Each delivery attempt uses the policy active when that attempt takes its snapshot.

An in-flight attempt finishes against its snapshot. A subsequent redelivery may observe a newer policy and select
a different verdict. Earlier successfully published verdicts are not withdrawn.

This is ordinary at-least-once processing, not policy-version pinning or exactly-once decision execution. Existing
loop correlation and retained-verdict behavior remain their owners' responsibility.

If policy immutability across attempts is required, that is a separate owner decision—not a hidden ledger added here.

## Stacked integration and landing

The current #1312 branch remains main-based and design-only until the prerequisite checkpoint is published.

The owner-approved Git arrangement is:

1. Publish the reviewed #1159 prerequisite checkpoint, including the required settlement entry point, correlation
   contract, and R8 publisher correction. Keep the affected R7 guarantee unchecked.
2. Rebase #1312 onto that exact published #1159 checkpoint and target #1159's branch. Review #1312 as its bounded
   child diff.
3. Implement and prove the rule correction on this combined stack. Do not copy helpers, hoist APIs, or modify the
   frozen #1156 parent.
4. Merge the approved child into #1159's integration branch—not independently to main.
5. Land #1156 through its existing gates, then reconcile the combined #1159 branch with resulting main and rerun
   affected verification.
6. Complete #1159's remaining loop-side R7 evidence and final combined proof with the rule prerequisite present.
7. Only then may #1159 claim the end-to-end guarantee and land on main.

The final default-branch PR must explicitly carry the agreed issue-closing references for the integrated work.
A child merge into a non-default branch is not completion of #1311 on main.

## Acceptance and migration

Required proof includes:

- Boot and reload refusal before any proposal effect, including malformed/lossy action declarations.
- Rejected reload preserves the previous proposal policy.
- Custom factories are not invoked for proposal work.
- Multiple matching rules retain their action order and rule-local deny short-circuiting.
- A later publication failure retries the delivery and permits repeating earlier successful publications.
- Entity-rule failure preserves the complete prior proposal portion, including IDs changing evaluation lanes.
- Native source redelivery after verdict publication failure; source ACK observed only after required PubAck.
- Cold restart between publication and ACK, with accepted duplicate publication.
- Policy replacement between attempts, proving the documented newer-policy behavior.
- No-match/no-action completion never produces approval.
- Ordinary message/entity/projection controls and best-effort audit remain unchanged.
- Exact consumer shutdown and settlement-control failure behavior.
- Relevant agentic E2E on the combined stack before the breaking change lands.

Migration notes must explain the dedicated proposal input, admitted publication-only actions, removal of
match-cycle controls from this lane, and the integrated correlation/wire contract. Existing rule multiplicity,
subject/property authoring, and publish-rejection-then-deny composition remain supported.
Sister-repository changes remain their owners' work.

## Owner acceptance

The owner accepted the dedicated proposal-input contract and stacked integration arrangement in
https://github.com/C360Studio/semstreams/issues/1311#issuecomment-5676602813. No single-policy restriction,
first-verdict language, or retirement of publish-plus-deny is approved.

Declared costs are the isolated physical message input, restriction to immutable-message conditions and
publication-only actions, at-least-once publication, and current-policy evaluation on each attempt. This does not
promise policy arbitration, policy-version pinning, or atomic replacement of unrelated rule types.
