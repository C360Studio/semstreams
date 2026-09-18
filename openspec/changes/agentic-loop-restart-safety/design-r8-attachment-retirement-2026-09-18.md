# R8 accepted live-attachment retirement — mechanical lowering

base: 68c14c8eb25c512e988f740cbf7ea14b6815976f

Status: architect handoff for independent conformance review; not a completed runtime task.

Authority: owner comment `5728438234`. Corrected inventory
`e027be801c417f08f12143dc5527d2fda30ae035fa04fbac5dd066c03564de74` passed independently, 72/72 pins.
The complete evidence appendix remains unchanged in
[the retirement inventory](inventory-r8-attachment-retirement-2026-09-18.md).
Alternatives and product costs remain in reviewed docket `ff890002…`; no new product decision is proposed.

## 1. Implementation boundary

Remove:

- `Config.AutoContinue`, its default/schema/debug output, nine shipped declarations, four inferred-target branches,
  and now-unused `activeLoop`.
- Exported `UserMessage.ReplyTo` and `HTTPMessageRequest.ReplyTo`, the corresponding rule-readable field, dispatch
  attachment admission, and the attachment argument/comparison in task preparation/recovery.
- `loopOpContinue`; `attachContinuation`; attachment-only HandleTask branches; ErrLoopBusy/ErrLoopTerminal and their
  attachment-specific handling where no surviving caller remains.

Preserve:

- Explicit command arguments and the custom CommandHandler/CommandExecutor signatures. This branch has no separate
  built-in `agent_request` command surface to remove.
- Cancel/status/read/approval targets and permissions.
- RunID, InReplyTo, ParentLoopID and their validation.
- PriorMessages, same-task recovery, retained-task reuse, partial-birth handling, terminal suppression, and
  within-execution model/tool/approval continuation.

Every genuinely new TaskMessage producer execution mints a fresh LoopID. Retries of the same already-marshaled task
retain TaskID, LoopID and bytes. No discriminator, alias, payload version, store, lifetime calculation or
expired-identity detector is introduced.

### Retired JSON handling

Reject presence, not the value, of `auto_continue` and submission `reply_to`: false, null, empty string and other
values are all refusals. Match the case-folded key spellings accepted by the existing JSON decoder; escaped JSON
keys are decoded before comparison. Missing keys remain accepted.

- **Configuration:** use the existing component-local retired-key pattern before ordinary decoding/allocation.
  No blanket `DisallowUnknownFields` change.
- **HTTP:** reject `reply_to` at `/message` body decoding, before task lookup/minting, command execution or
  publication, using the existing synchronous JSON error response.
- **USER:** remove the exported field but retain only a private, nonserialized presence bit during UserMessage
  decoding. Do not retain or interpret its value. Existing `Validate` rejects that bit. Reset it on every decode.
  Validate the decoded UserMessage in `handleUserMessage` **before** choosing command versus task handling.
- A routable invalid USER receives the existing registered `ResponseTypeError` naming the retired field and
  directing the caller to a new turn with `prior_messages`. Required negative publication failure returns Retry;
  PubAck precedes Terminate. Malformed/unregistered/unroutable input retains its existing decoder/refusal behavior;
  do not invent a response route.
- The private bit is rejection metadata, not compatibility acceptance, a public getter or durable state. Do not
  change the shared envelope decoder or payload registry.

### Known task conflicts

Keep CreateLoopWithID's form-first, no-overwrite behavior. HandleTask must not interpret an existing LoopID as
permission to rebind TaskID.

At durable intake, a known different-task/loop correlation conflict uses the existing fatal-correlation result:
Quarantine, with existing owner-health/error observability. Align the warm collision with the retained-authority
conflict; do not synthesize LoopFailed, replay the former task's result as the new task's result, modify COMPLETE,
or overwrite authority.

This is rejection of unsupported correlation, not a caller-facing task-completion event. Same-task/same-loop
recovery and terminal suppression retain their current paths. If all relevant identity evidence has expired,
runtime detection of arbitrary old-token reuse is not promised.

## 2. Exact proposal/design replacements

### `proposal.md` — replace “Approved product direction” body

> Sequential chat uses independent executions. The adapter supplies its displayed user/assistant transcript through
> optional `PriorMessages` on UserMessage, HTTPMessageRequest and TaskMessage; each new turn receives a fresh
> TaskID/LoopID and execution budget. The committed task carries this input through restart recovery.
>
> Owner comment `5728438234` supersedes the earlier retention of live attachment and the AutoContinue-default-only
> decision. Live attachment and inferred command targets are retired. Explicit cancel/status/read/approval targets,
> run/reply/parent lineage, same-task redelivery, and model/tool/approval continuation within one execution remain
> supported.
>
> Settlement-first recovery and retained-result reuse remain unchanged. This retirement introduces no conversation
> store, new payload, identifier scheme, compatibility acceptance or indefinite duplicate-detection guarantee.
> Remaining R8 missing-request and supported-retention proofs stay open.

In **What Changes**, replace the chat bullet with:

> Independent chat turns carry ordered, text-only displayed history. Missing, null and empty history are equivalent.
> Commands reject nonempty history. `auto_continue` and submission `reply_to` are removed and explicitly refused when
> present in JSON. Commands requiring a target use an explicit loop ID. The adapter owns conversation recall and
> supplies the displayed transcript.

Replace the task-identity bullet's continuation sentence with:

> Every new task producer execution mints one fresh v4 LoopID locally before validation and marshal. Retry of the
> same already-marshaled task and downstream redelivery preserve its TaskID, LoopID and bytes. Existing identity
> never authorizes different-task rebinding.

Delete the AutoContinue-convenience/birth-gap bullet. Remove AutoContinue from **current** projection/adopter claims
elsewhere; retain historical descriptions only when clearly labelled superseded.

### `design.md` — replace attachment paragraph in “Independent chat turns”

> Every newly submitted turn starts a fresh execution, even while another execution is active. Live attachment and
> inferred command targeting are retired under owner comment `5728438234`. Dispatch removes AutoContinue and
> submission ReplyTo while explicitly rejecting their retired JSON keys. Commands use explicit target arguments;
> cancel, status, reads and approval keep their existing admission.
>
> Same-source redelivery resolves and validates its committed task without reminting or republishing that retained
> task. Ordered role/content comparison remains, with nil and empty history equivalent; conflicts use existing
> correlation quarantine. No TaskMessage may rebind another task's execution.

Replace **AutoContinue** subsection with **Retired live attachment** containing the “Retired JSON handling” and
“Known task conflicts” text above.

Mechanical reconciliation elsewhere:

- Projection users become `/activity`, `/loops`, `/debug/state`; retain custom ownership lookup.
- Replace AutoContinue adopter row with:
  `Former attachment caller | Remove auto_continue and submission reply_to; send each new turn with displayed prior_messages | Known retired keys refuse; omission starts a new execution | Component schema, HTTP schema, typed USER refusal and migration guide | No loop targeting or retention calculation for chat`
- Remove AutoContinue-only invariants, birth-gap acceptance promises and verification entries.
- Replace current “continuation producer echoes admitted existing token” claims with the task-identity paragraph above.
- Do **not** mechanically replace “continuation” in approval, model/tool or retained-request sections.
- Add: `This slice does not close R8's progressed-authority/missing-request RED, source-to-task retention proof,
  or in-flight source/evidence proof.`

## 3. Active dispatch delta

Replace the AutoContinue/attachment paragraphs in **Prior messages accompany an independent chat turn** with:

> Every new submission SHALL start an independent execution with a freshly minted LoopID. No submission SHALL attach
> to or rebind an existing execution. Commands requiring a loop SHALL use an explicit target argument; their existing
> control/read admission and custom-command signatures remain unchanged.
>
> Config.AutoContinue, UserMessage.ReplyTo and HTTPMessageRequest.ReplyTo SHALL be absent. Supplying `auto_continue`
> in component JSON or `reply_to` in submission JSON SHALL be explicitly refused, including null or empty values;
> omission SHALL remain valid. The check SHALL recognize the case-folded spellings previously consumed by
> encoding/json without introducing general strict-JSON policy or accepting a compatibility value.
>
> A routable rejected USER input SHALL publish the existing typed negative response and receive PubAck before
> Terminate. Publication failure SHALL Retry. HTTP SHALL return its existing synchronous error response. Refusal
> SHALL occur before command effects, task lookup/minting/publication or loop mutation.
>
> Same-source redelivery SHALL validate and reuse its retained task. Source comparison SHALL preserve ordered history
> and all surviving correlation fields; it SHALL NOT reinterpret a replay as a new conversational turn.

Replace attachment/default scenarios with:

```markdown
#### Scenario: A new turn never targets an active execution

- **GIVEN** another execution exists for the user/channel route
- **WHEN** an otherwise valid new submission arrives
- **THEN** its task has a fresh LoopID and contains only the supplied displayed history
- **AND** no existing execution is rebound or mutated

#### Scenario: Retired targeting is refused explicitly

- **WHEN** component JSON contains auto_continue, or submission JSON contains reply_to, with any value
- **THEN** the applicable boundary refuses and names the retired key
- **AND** no command, task publication, new identity or loop mutation occurs
- **AND** a routable durable USER receives its negative response before termination
- **AND** failed negative publication retries without acknowledging that source

#### Scenario: Explicit controls remain available

- **WHEN** a caller supplies an explicit cancel/status/read/approval target
- **THEN** its existing token, authority and permission checks remain in force
- **AND** no target is inferred from another active loop
```

Remove AutoContinue-only projection scenarios; remove its mentions from surviving projection/corruption/activity
scenarios without changing those scenarios' other obligations.

In **Loop existence and ownership are merged facts**, replace the continuation-after-replacement scenario with:

```markdown
#### Scenario: An explicit control reads authority after replacement

- **GIVEN** dispatch was replaced and an exact durable loop record remains
- **WHEN** a caller explicitly requests status, cancellation or approval for that loop
- **THEN** dispatch uses that durable authority and the operation's existing admission rules
- **AND** it neither creates nor rebinds an execution
```

Carry complete **MODIFIED** copies of affected base requirements into the active delta before archive: remove only
the `continue` operation and attachment scenarios from **One gate admits every request that names an existing loop**
and **The ownership model binds the user lane, and approval is deliberately not owner-scoped**. Preserve
cancel-own/cancel-any, approval permission without ownership, read behavior and the non-authorization warning
verbatim. Do not modify base specs directly during implementation.

## 4. Active loop delta

Add under **REMOVED Requirements**:

```markdown
### Requirement: Creating a loop that already exists is refused; a continuation attaches to it

**Reason:** Owner comment 5728438234 retires live attachment.
**Migration:** Submit a new task with a fresh LoopID and optional displayed PriorMessages.
Create refusal, same-task recovery and terminal suppression are preserved by the replacement requirement.
```

Add under **ADDED Requirements**:

```markdown
### Requirement: A task owns one execution without rebinding

Every new TaskMessage producer execution SHALL mint a fresh canonical v4 LoopID before validation and marshal.
Retries of the same already-marshaled publication and downstream redelivery SHALL retain its TaskID, LoopID and bytes.
A fresh execution of an upstream producer remains outside that publication-retry identity claim.

Loop intake SHALL NOT treat existence as permission to attach a different task. CreateLoopWithID SHALL retain
form-first validation and refuse an existing token before overwriting loop, context or pending-tool state.
A known different-task correlation SHALL use the existing fatal correlation refusal at durable intake,
without rebinding, new model work, replacement authority or a terminal result attributed to the wrong task.

Same-task/same-loop recovery, pending-output retry and matching terminal suppression SHALL remain supported.
Model, tool and approval continuation within that execution SHALL remain unchanged.
This requirement SHALL NOT promise detection of arbitrary reused identities after all relevant evidence expires.

#### Scenario: A different task cannot steal an existing execution

- **GIVEN** task A owns a known loop, whether running, awaiting approval or terminal
- **WHEN** task B supplies that LoopID
- **THEN** intake returns the existing correlation-conflict disposition
- **AND** A's state, context, pending work and selected terminal outcome remain unchanged
- **AND** no task-B request or fabricated task-B terminal outcome is published

#### Scenario: Redelivery remains the same work

- **GIVEN** the same TaskID and LoopID are redelivered
- **WHEN** current or retained evidence establishes their existing execution
- **THEN** existing deduplication, recovery or terminal-suppression behavior applies
- **AND** no different task is attached
```

In **Loop task, request, and tool work use only required correlation**, replace its opening producer paragraph with
the first paragraph above. Preserve all publication and terminal-ordering requirements.

Remove “admissible continuation boundaries” from Running's phase description, without removing model/tool work.
Replace “Direct create/attach refusal” with “Direct create refusal and different-task correlation refusal.”
Approval continuation requirements remain unchanged.

## 5. Active entity-ID delta

Replace the opening task-production sentences with:

> For a newly produced TaskMessage, its producer is the framework birth seam and MUST mint a fresh canonical v4
> LoopID before validation, envelope marshal and publication. Retry of the same already-marshaled task and
> downstream redelivery reuse its identity and bytes. No different task may attach to an existing execution.

Replace the ReplyTo example in the form-versus-provenance paragraph with:

> A supplied canonical UUID can pass the form predicate regardless of who authored it. Passing that predicate does
> not establish birth provenance, existence, ownership or authorization. Surviving explicit control/read operations
> perform their existing authority and permission checks; the form predicate is not an expired-identity detector.

Replace the dispatch validation bullet with:

> Dispatch MUST refuse a non-canonical `run_id` or `in_reply_to` before task minting or publication through its existing
> typed response route. Retired submission `reply_to` MUST be refused by the dispatch retirement requirement, not
> resolved or validated as an attachment target. User control, approval and HTTP path-token validation remain unchanged.

Replace the ReplyTo-form scenarios with a cross-reference to **Retired targeting is refused explicitly**. Recast the
canonical-form example around a surviving explicit control/read target: canonical form can pass while exact
authority lookup returns not found. Remove ReplyTo-absence premises from new-task and run/reply-lineage scenarios.
Preserve all other token-field, signal, approval and provenance obligations.

## 6. Migration replacements

Replace section 3's live-continuation promise with:

> ### 3. Live attachment is retired
>
> Remove `auto_continue` from dispatch configuration, including declarations set to false. Remove submission
> `reply_to` and Go references to Config.AutoContinue, UserMessage.ReplyTo and HTTPMessageRequest.ReplyTo. Supplying
> either retired JSON key is refused rather than silently changing its meaning.
>
> Send each conversational turn as new work with `prior_messages` containing the user text and assistant responses
> actually displayed. Each turn gets a fresh execution and budget. Do not reuse the prior LoopID to continue chatting.
>
> Explicit cancellation, status/read and approval targets remain supported. `run_id`, `in_reply_to` and
> `parent_loop_id` retain their separate lineage meanings. Internal model/tool rounds, approval waits and same-task
> redelivery are not retired.
>
> Direct TaskMessage producers mint a fresh LoopID per new task and preserve it when retrying the same serialized
> publication. Known identity conflicts refuse without altering the existing task. Detection of arbitrary identity
> reuse after all evidence expires is not guaranteed.

Rename **Independent chat turns and the AutoContinue default** to **Independent chat turns**; replace its opening
paragraph with the new-turn contract above. Keep the displayed-history example. Replace “command or admitted
attachment” with “command”; remove the opt-in attachment promise and “under defaults” qualifiers.

In dispatch-projection sections, remove AutoContinue from reader lists; retain `/activity`, `/loops`, `/debug/state`,
readiness and poison behavior. Replace its dedicated paragraph with a link to the retirement migration. Replace the
verification sentence with:

> Verify fresh-run chat across component replacement, explicit cancel/status/approval after replacement,
> retired-target refusal, unavailable-view responses and terminal routing. No beta-state preservation or
> compatibility layer is required.

Record the inventoried SemTeams/SemDev true settings, SemSpec false settings and mirrored client/schema regeneration
as downstream-owner migration work. Do not modify sister repositories.

## 7. Executable slices and verification

| Slice | RED/GREEN evidence | Forbidden effects / retained behavior |
|---|---|---|
| Retired inputs and config | Production constructor; real registered USER decoder → dispatch callback; HTTP handler. Missing key accepted; present null/empty/false/string and case variants refused. | No task lookup/mint/publication, command execution or loop mutation; USER negative PubAck then Terminate, failure Retry. |
| Remove rebind | Existing create-vs-exists and delivery-owner fixtures. Same task replay versus different task, all operational/terminal states; warm and retained authority. | No TaskID overwrite, request for rejected task, terminal appropriation or loss of prior context/pending work. |
| Preserve chat and controls | Reuse `TestIntegrationSequentialChatAfterComponentReplacement`; retain exact-history/fresh-budget/distinct-ID assertions and existing cancel/approval tests. | Adapt only obsolete field assertions; no additional integration container needed. |
| Breaking E2E | Extend existing agentic tier after its counter-sensitive stages: send two independent HTTP turns, observe returned terminal text, supply it as PriorMessages, verify distinct IDs and exact retained second AgentRequest; send retired targeting over HTTP and registered USER and verify refusals/no task. | Use existing deployed stack/mock provider. Explicitly count the new assertions; tier name alone is not evidence. Existing process-replacement stage and chat integration retain their distinct scopes. |

Prospective testing-policy decision:

- **Native fuzz:** applies to known-key parsing/decoding. Assert refusal for present retired keys and
  acceptance/preservation of otherwise valid omitted-key input; seed null/empty/case/escaped-key/duplicate-key
  boundaries. Observe production-boundary behavior, not the private bit.
- **History/PBT:** applies to ownership/replay. Existing deterministic warm/cold, same-task/different-task,
  pending/terminal cases cover this small finite distinction without adding a new Rapid state model. State explicitly
  that this does not prove expiry or concurrent histories.
- **Mutation required:** consequential identity and refusal invariants. Independently disable the retired-key check
  and enable different-task rebinding; fixed tests must detect the missing refusal or forbidden mutation. Record
  baseline, compiled mutant, intended assertion failure, byte restoration and restored pass. No new test dependency.
- Run integration through the existing host-locked runner. Run relevant breaking E2E before landing. Preserve
  unresolved results; no retrospective overhaul of reviewed task-reuse evidence.

## 8. Task truth

Add under R8, initially unchecked:

```markdown
- [ ] Lower and independently review accepted live-attachment retirement across dispatch, loop and entity-ID deltas.
- [ ] Remove public/config targeting and explicitly refuse retired JSON keys through existing response/settlement routes.
- [ ] Remove different-task rebinding while preserving same-task recovery, terminal suppression and explicit controls.
- [ ] Reconcile schemas, shipped configs, current docs and migration; record downstream-owner actions.
- [ ] Complete focused RED/GREEN, fuzz/property applicability and targeted mutation evidence.
- [ ] Reuse sequential-chat integration and extend/run the existing agentic E2E with actual retirement/chat assertions.
```

Keep R8 unchecked. The progressed-authority/missing-request RED and finite supported source/evidence-retention proofs
remain open. This handoff authorizes no implementation beyond the accepted retirement and its necessary conformance proof.
