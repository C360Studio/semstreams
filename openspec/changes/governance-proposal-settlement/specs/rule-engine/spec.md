## ADDED Requirements

### Requirement: Governance proposal work has a dedicated admitted input

The Rule processor SHALL admit governance proposal work through a JetStream input declaring exactly
`agent.toolcall.proposed.>`. Stream and consumer settings SHALL come from canonical port facts.

A processor admitting this lane SHALL NOT also admit ordinary core-NATS or JetStream message inputs.
KV/entity inputs MAY coexist. Rule IDs, metadata, conditions, and nonexistent Definition subscription fields
SHALL NOT determine input ownership.

The processor SHALL inspect the actual delivered subject before ordinary message dispatch. A governance
proposal arriving through another input SHALL be refused before rule effects and SHALL NOT fall through to
ordinary successful ACK handling. Other messages SHALL retain their existing path and matching behavior.

Proposal definitions SHALL use the built-in expression implementation directly. Arbitrary registered factory
validation, construction, or evaluation SHALL NOT execute for proposal work.

#### Scenario: Dedicated proposals coexist with entity evaluation

- **GIVEN** a processor with the dedicated proposal input and KV/entity inputs
- **WHEN** proposal and entity work arrive
- **THEN** proposals use the admitted proposal lane
- **AND** entity rules retain their existing evaluation path without proposal retry semantics

#### Scenario: An unsupported physical input cannot bypass admission

- **WHEN** an ordinary message input is configured alongside the dedicated proposal input
- **THEN** admission fails before rule effects
- **WHEN** a proposal instead arrives through another input
- **THEN** it is refused before rule effects and does not receive ordinary successful ACK handling

#### Scenario: A registered factory cannot replace proposal execution

- **GIVEN** a registry-selected factory would execute custom behavior
- **WHEN** proposal definitions are admitted and evaluated
- **THEN** that factory is not invoked
- **AND** unsupported proposal definition types are refused

### Requirement: Governance proposal definitions admit only publication-only composition

The proposal lane SHALL admit any number of built-in message-expression definitions with identity/descriptive
fields, enabled state, conditions, logic, descriptive metadata, and ordered `on_enter` actions.

Conditions, guards, and evaluated references SHALL depend only on the immutable `$message.*` projection and
literal values. Entity, related, state, schedule, caller, graph, and lifecycle dependencies SHALL NOT be admitted.

The lane SHALL NOT admit `on_exit`, `while_true`, `on_recovery`, recovery opt-in, related patterns, cron
schedule/actions, positive rule iteration limits, or action firing caps. Cooldown SHALL be absent or zero.
`fire_every_n_events` SHALL be absent, zero, or one.

Admitted actions SHALL be limited to existing `publish`, `approve`, and `deny` behavior, retaining their
existing Subject, Properties, Reason, When, and descriptive action-ID authoring as applicable.
Mutation, dispatch, iteration, and other action types SHALL be refused before effects.

Required publication targets SHALL resolve through the shared declared-output/PubAck owner.
Core-NATS fallback SHALL NOT satisfy required proposal publication. No second output classifier, serializer,
replacement routing contract, new action language, or adopter retry-safety flag SHALL be introduced.

The documented publish-rejection-then-deny composition and integrated correlation/wire contract SHALL remain
supported. This requirement SHALL NOT extend automatic retry to projection mutations or unknown tool effects.

#### Scenario: Documented publication composition remains admitted

- **GIVEN** multiple built-in message rules containing publish, approve, deny, and message-only guards
- **WHEN** the complete proposal portion is admitted
- **THEN** rule multiplicity and existing subject/property authoring remain supported
- **AND** publish-rejection-then-deny is not replaced by a new action language

#### Scenario: Unsupported or malformed composition is refused before effects

- **WHEN** a proposal candidate contains unsupported actions, stateful controls, unknown fields,
  malformed action declarations, or non-message dependencies
- **THEN** admission rejects it before proposal effects
- **AND** malformed actions are not converted into absent action lists and then accepted

### Requirement: Governance proposals evaluate per delivery without match-cycle state

Each delivery attempt SHALL decode through the existing registered boundary, obtain the immutable
rule-readable proposal projection, and take one snapshot of the admitted proposal definitions under the
existing processor lock.

The existing built-in condition and action owners SHALL evaluate that snapshot without shared match/cooldown
state, the stateful evaluator, or persisted match/action counters. Proposal work SHALL NOT read or write
StateTracker bookkeeping to select or suppress actions.

For each matching rule, `on_enter` actions SHALL execute in declared order subject to existing When guards.
Approve SHALL remain permissive. Deny SHALL stop remaining actions of its own rule, not other matching rules.
No new priority, precedence, first-verdict, or deny-overrides contract between rules SHALL be introduced.

A required publication failure SHALL propagate immediately to delivery settlement instead of being logged
and treated as successful policy execution.

#### Scenario: Action order and local denial survive the new lane

- **GIVEN** multiple matching proposal rules
- **WHEN** one rule publishes a rejection and then denies
- **THEN** its remaining actions do not execute
- **AND** other matching rules are not suppressed by a new global denial policy
- **AND** approve does not acquire new short-circuit semantics

#### Scenario: Existing match bookkeeping cannot suppress redelivery

- **GIVEN** a proposal is redelivered after a required publication failure
- **WHEN** its current admitted rules match
- **THEN** their selected actions are evaluated again without consulting StateTracker
- **AND** proposal processing does not alter persisted match state or action counters

### Requirement: Governance proposal settlement follows publication consequences

The proposal lane SHALL use the existing semantic settlement and exact-consumer lifecycle owners.

| Outcome | Required consequence |
| --- | --- |
| Successful evaluation owing no required publication | Complete; no approval implied |
| Successful evaluation with all required publications acknowledged by JetStream | Complete |
| Transient required-publication failure, including uncertain acknowledgement | Retry; repeated publication is permitted |
| Cancellation before successful completion | Retry under the existing lifecycle/settlement contract |
| Malformed input or deterministic correlation/condition data failure | Terminate with the existing classified diagnostic |
| Unsupported policy at boot or reload | Refuse activation before effects; not source-delivery retry |
| Unexpected internal/control failure | Follow the existing settlement/lifecycle contract; no fallback ACK |

Complete SHALL require completed policy execution and PubAck for every required publication it owes.
Audit or optional-notification success SHALL NOT substitute for required PubAck.
A settlement-method failure SHALL NOT trigger a second, different terminal method.

Previously successful publications MAY repeat after retry or restart. No outbox, ledger, supervisor, new
retry runtime, durable execution accounting, or automatic retry of external mutations SHALL be introduced.

#### Scenario: A later failure cannot acknowledge earlier partial publication as completion

- **GIVEN** an earlier required publication receives PubAck
- **WHEN** a later required publication fails transiently
- **THEN** the proposal is retried rather than completed
- **AND** redelivery may repeat the earlier publication

#### Scenario: Source ACK follows the final required PubAck

- **GIVEN** a real JetStream proposal delivery with selected required publications
- **WHEN** the final required PubAck has not been received
- **THEN** the proposal is not successfully acknowledged
- **WHEN** policy execution succeeds and every required publication has PubAck
- **THEN** the proposal may Complete

#### Scenario: Poison and settlement-control failure are not successful completion

- **WHEN** proposal input or correlation/condition data is deterministically invalid
- **THEN** it receives the classified Terminate consequence rather than Complete or automatic Retry
- **WHEN** a settlement method fails
- **THEN** no second, different terminal method is attempted
- **AND** the existing consumer lifecycle contract governs the failure

### Requirement: Completed proposal evaluation does not imply approval

No matching rule, disabled or absent policy, no selected action, or successful policy execution owing no
verdict publication MAY Complete without a verdict. Bare deny SHALL retain its structural/audit meaning
and SHALL NOT be silently converted into routing publication.

If no usable terminal verdict is produced, enforce mode SHALL retain the loop's existing timeout/refusal
behavior. Audit mode SHALL remain nongating. Completion or evaluation counters SHALL NOT imply that a
verdict was published.

Verdict audit SHALL remain best-effort. Optional notifications SHALL retain their existing absence,
validation, and failure-observability contracts. Their failures SHALL NOT retroactively change a verdict
whose required publication succeeded.

#### Scenario: Publication-free completion grants no approval

- **WHEN** evaluation selects no required publication or executes only bare deny
- **THEN** the proposal may Complete without a routing verdict
- **AND** enforce mode does not interpret that completion as approval
- **AND** audit mode remains nongating

#### Scenario: Observability does not replace the required verdict

- **GIVEN** audit or optional notification succeeds while required verdict publication fails
- **THEN** proposal settlement follows the required publication failure
- **GIVEN** required verdict publication succeeds while best-effort audit fails
- **THEN** the audit failure remains observable without changing the verdict

### Requirement: Proposal reload preserves the prior proposal portion until installation

Boot SHALL validate the complete proposal portion before rule construction or subscription installation.

Reload SHALL decode and validate the complete desired proposal portion without lossy action conversion.
Malformed actions, unknown fields, unsupported composition, or a failed required KV read SHALL reject the
candidate rather than produce a shorter successful proposal snapshot.

All built-in proposal replacements SHALL be prepared before active proposal entries change.
Unrelated entity updates SHALL then use their existing owner and failure behavior.
Only after that work succeeds SHALL the prepared proposal portion be installed together under the existing lock.

If unrelated entity construction or application fails, the complete prior proposal portion SHALL remain active.
Unrelated entity entries already changed by the existing sequential path MAY remain changed; no rollback of
those entries is promised.

The deferred commit SHALL cover every rule ID belonging to either the old or candidate proposal portion.
An ID changing between proposal and entity evaluation SHALL NOT overwrite or remove an old proposal entry
before that commit. The commit SHALL update the corresponding existing rules, definitions, configurations,
and counter-map entries without exposing a partially installed proposal portion.

A failed reconcile SHALL report failure while retaining the prior proposal policy. A saved desired definition
SHALL NOT be reported as confirmed active. No second active registry, activation bucket, receipt system, or
general atomic-reload mechanism SHALL be introduced.

#### Scenario: Failed preparation retains the complete prior proposal policy

- **WHEN** candidate decoding, a required KV read, validation, or proposal preparation fails
- **THEN** the candidate is rejected
- **AND** the complete prior proposal portion remains active

#### Scenario: An unrelated entity failure cannot partially install proposal changes

- **GIVEN** a replacement contains proposal changes and unrelated entity changes
- **WHEN** entity construction or application fails
- **THEN** all prior proposal entries remain active
- **AND** this holds for IDs moving between proposal and entity evaluation
- **AND** unrelated entity entries already changed are not claimed to have been rolled back

### Requirement: Proposal retry uses the policy active on the next attempt

Each delivery attempt SHALL use the proposal-policy snapshot active when that attempt takes its snapshot.
An in-flight attempt SHALL finish against that snapshot; later redelivery MAY observe a newer admitted policy
and publish a different verdict. Earlier successful publications SHALL NOT be withdrawn.

This SHALL remain at-least-once processing, not policy-version pinning or exactly-once decision execution.
Existing loop correlation, retained-verdict behavior, and verdict interpretation SHALL remain authoritative.

#### Scenario: Replacement between attempts does not freeze the earlier policy

- **GIVEN** an attempt used policy A and requires redelivery
- **WHEN** policy B becomes active before the next attempt takes its snapshot
- **THEN** the next attempt evaluates B
- **AND** an already-running attempt retains its own snapshot
- **AND** earlier publications are neither withdrawn nor claimed to be exactly once

## MODIFIED Requirements

### Requirement: Recovery-only rules are evaluated

On the ordinary message and entity evaluation paths, a rule whose only actions are declared in
`on_recovery` MUST be admitted to the stateful evaluator, MUST persist match state during live operation,
and MUST fire its recovery actions on the bootstrap path after restart. Empty enter/exit/while action lists
MUST NOT cause spurious firings or exclude the rule from evaluation. The dedicated governance proposal
lane has its separately declared admission contract and does not enter this stateful path.

#### Scenario: fail-closed recovery park fires on restart

- **GIVEN** a rule with conditions matching an in-flight work entity and actions only in on_recovery
- **WHEN** the entity matches during live operation and the processor restarts
- **THEN** the recovery actions fire exactly once for that entity on bootstrap

#### Scenario: never-matched entities do not recover

- **GIVEN** the same rule and an entity that never matched before the restart
- **WHEN** the processor restarts
- **THEN** no recovery action fires for that entity
