## ADDED Requirements

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

### Requirement: A task carries its prior conversational input

TaskMessage SHALL accept optional `PriorMessages []ChatMessage`, serialized as `prior_messages,omitempty`. Missing,
null, and empty SHALL mean no supplied history. One private validator used by TaskMessage.Validate SHALL require each
entry to have user or assistant role and nonempty Content, with Name, ReasoningContent, ToolCalls, ToolCallID, IsError,
and ReasoningRecords empty or zero. It SHALL NOT impose alternation, rewrite text, or change generic ChatMessage or
direct AgentRequest validation. Prior user text SHALL NOT become system instructions.

Initial request assembly SHALL contain fresh execution instructions, existing embedded context when present, ordered
prior messages, and the current prompt exactly once. ContextManager SHALL receive the prior history once before the
prompt so subsequent tool iterations retain it. Cold reconstruction without a retained request SHALL rebuild from
the durable task including history; matching retained-request restoration SHALL NOT reseed it. A new history-bearing
task SHALL NOT attach to an existing execution owned by a different TaskID.

Committed task history SHALL suffice for that execution's conversational input without depending on earlier execution
retention or a new transcript store. Existing context compaction and provider limits remain separate; this input
boundary SHALL NOT silently trim the supplied history or import prior execution budget/todo instructions.

#### Scenario: Follow-up after replacement

- **GIVEN** a completed turn whose user and delivered assistant text accompany a later independent task
- **WHEN** started dispatch, loop, and model components are stopped, joined, and replaced with NATS retained
- **THEN** the later provider request carries that prior exchange and the new prompt in order under a different LoopID
- **AND** each supplied message occurs once with fresh iteration instructions
- **AND** a deterministic provider can answer using a fact present only in the prior exchange

#### Scenario: Only displayed text is admissible history

- **WHEN** a task supplies system, developer, or tool roles, empty Content, or nonzero name/tool/reasoning fields
- **THEN** task validation refuses it before execution work
- **AND** ordered nonempty user/assistant text is preserved without alternation or deduplication rules

#### Scenario: History survives within-turn tool work

- **GIVEN** an independent task supplies prior user/assistant text
- **WHEN** its first model response requests a tool and the result produces another model iteration
- **THEN** that iteration retains prior history and the current prompt once, together with the new tool exchange
- **AND** its budget instructions are generated for the current execution

#### Scenario: Cold reconstruction and retained-request restoration agree

- **GIVEN** a durable history-bearing task is redelivered after component replacement
- **WHEN** its initial retained request is absent
- **THEN** request reconstruction includes the supplied history once
- **WHEN** the matching retained request exists
- **THEN** restoration reuses it without seeding the history a second time

#### Scenario: History cannot replace a different task's active execution

- **GIVEN** a loop already belongs to a different TaskID
- **WHEN** a new history-bearing task targets that LoopID
- **THEN** it is refused before replacing its context or publishing execution work

### Requirement: LoopEntity has one operational state contract

`LoopState` SHALL remain an exported string type with exactly these admitted values:

| Symbol | Wire value | Meaning |
|---|---|---|
| LoopStateRunning | running | Nonterminal work not waiting for human approval, including model/tool work and waiting for results. |
| LoopStateAwaitingApproval | awaiting_approval | A current tool call is gated on a human decision. |
| LoopStateComplete | complete | Successful terminal loop outcome. |
| LoopStateFailed | failed | Failed terminal loop outcome. |
| LoopStateCancelled | cancelled | Cancelled terminal loop outcome. |

`LoopStateExploring`, `LoopStatePlanning`, `LoopStateArchitecting`, `LoopStateExecuting`,
`LoopStateReviewing`, `LoopStatePaused` and `LoopEntity.StateBeforeApproval` SHALL be removed.
Retired wire values SHALL NOT be translated, aliased or reserved.

Running SHALL NOT imply an executing goroutine, outstanding model request or settled delivery.
Existing request, execution, gate and consumer evidence SHALL retain ownership of those distinctions.
A loop's state SHALL NOT determine its enclosing AgentRun's phase.

Agentic SHALL privately reuse `lifecycle.Transitions` for one `loopTransitions` declaration:

| Source | Permitted differing-state targets |
|---|---|
| running | awaiting_approval, complete, failed, cancelled |
| awaiting_approval | running, failed, cancelled |
| complete | none |
| failed | none |
| cancelled | none |

Membership and edge checks SHALL use this declaration. Its structure SHALL be checked with the existing
validator. `LoopState.IsTerminal` SHALL return false for unknown values and otherwise use the table.
The table SHALL NOT be exported or configurable. No Lifecycle Manager, second validator primitive,
state-machine runtime or graph-backed second loop authority SHALL be introduced.

Local state-field coherence SHALL mean:

| State | Required local relationship |
|---|---|
| running | PendingApproval is nil. |
| awaiting_approval | PendingApproval is nonnil with nonempty CallID and ToolName. |
| complete, failed, cancelled | PendingApproval is nil. |

`LoopEntity.Validate` SHALL retain its existing nonempty ID and positive MaxIterations checks and enforce
state membership and local state-field coherence. It SHALL NOT require RequestID, ExecutionID, CallOrdinal,
retained stream evidence, terminal Outcome or CompletedAt, or introduce new timeout/timestamp rules.

The existing public method signatures SHALL remain unchanged. Their common state precondition SHALL be
known state and local state-field coherence, without newly imposing whole-record ID/budget prerequisites.

`TransitionTo` SHALL reject unknown source or target and contradictory source state fields before handling
same-state requests. A coherent same-state request SHALL return nil without mutation. Direct
running-to-awaiting_approval SHALL be refused without mutation because the state-only signature cannot
construct its gate; callers SHALL use the existing `BeginAwaitingApproval` method. Allowed transitions
from awaiting_approval to running, failed or cancelled SHALL clear PendingApproval and change State
together. Allowed running-to-terminal transitions SHALL change State. All other differing-state requests
SHALL be refused according to the table.

`TransitionTo` SHALL NOT change or infer Outcome, Result, Error, CompletedAt, cancellation metadata or
PendingToolResults. Local terminal state SHALL NOT be treated as committed terminal authority.

`BeginAwaitingApproval` SHALL accept only coherent running state and retain its existing CallID/tool-name
argument checks. It SHALL construct the pending value using its existing arguments and existing time/default
behavior, and install that value with awaiting_approval together. Given an otherwise valid receiver, its result
SHALL pass `Validate` immediately without caller identity stamping. A second Begin call while awaiting approval
SHALL be refused, even when CallID is unchanged.

`ResolveApproval` SHALL require locally coherent awaiting_approval state and clear PendingApproval while
returning to running. It SHALL require no framework correlation stamping or retained-message lookup.
Every failed public mutation SHALL leave the entire receiver unchanged.

Local validity SHALL NOT replace the existing lane owner's complete gate identity and retained-evidence
validation before effects, new durable gate commitment or settlement. Production correlation SHALL continue
to come from the actual request/result, not values predicted by public state-API callers.

Process installation, UpdateLoop and restoration SHALL validate local entity coherence and protect the current
process entry under existing synchronization. Stale restoration SHALL NOT overwrite a newer terminal or
incompatible gate. Startup approval-deadline hydration SHALL retain its existing record/deadline checks and
SHALL NOT acquire a new retained-stream-evidence prerequisite.

Creation SHALL establish a validated running record using creation semantics; an existing record SHALL be
read and checked rather than overwritten as another birth. Form checks SHALL precede collision checks.
The task owner SHALL establish required durable birth authority before entering a later failure-settlement
path that depends on its existence. Unreadable authority SHALL NOT be treated as absence.

AGENT_LOOPS SHALL remain current loop authority. Discarding a failed speculative candidate and reconstructing
from freshly observed authority SHALL NOT be treated as an ordinary reverse edge. A changed durable revision
SHALL NOT be overwritten by a stale candidate.

The existing selected COMPLETE payload, required effects, PubAck, revision-conditioned final marker and
source-specific settlement obligations SHALL remain unchanged. Final outcome/timestamp agreement SHALL be
checked by the settlement owner, not inferred from a local state transition. PendingToolResults SHALL NOT be
cleared merely because state becomes terminal. Existing truncated-outcome versus failed-event behavior SHALL
remain unchanged.

This pre-v1 contract SHALL target freshly provisioned storage. It SHALL NOT introduce an assumed legacy-record
translation, rewrite, drain or disposal procedure. An actually discovered retained deployment requiring
migration or recovery SHALL require its own evidence-based owner-reviewed plan.

#### Scenario: State membership and terminality use the declared vocabulary

- **WHEN** every admitted, retired and unknown state is checked
- **THEN** only the five declared values pass state membership
- **AND** only complete, failed and cancelled are terminal
- **AND** unknown terminality remains false

#### Scenario: Direct transitions enforce edges and coherent no-ops

- **WHEN** each source/target pair is submitted directly to TransitionTo
- **THEN** it follows the declared direct-method behavior
- **AND** coherent same-state requests are unchanged no-ops
- **AND** contradictory same-state records are refused
- **AND** every refusal leaves the entire receiver unchanged

#### Scenario: Public approval methods require no hidden identity stamping

- **GIVEN** an otherwise valid running LoopEntity
- **WHEN** BeginAwaitingApproval is called with valid existing arguments and then Validate is called
- **THEN** validation succeeds without RequestID, ExecutionID or CallOrdinal stamping
- **AND** ResolveApproval returns it to a locally valid running state without retained-message lookup

#### Scenario: A state-only call cannot construct an approval gate

- **GIVEN** a coherent running LoopEntity
- **WHEN** TransitionTo requests awaiting_approval
- **THEN** it refuses without mutation and directs the caller to BeginAwaitingApproval
- **AND** a second Begin call on an awaiting loop also refuses without mutation

#### Scenario: Leaving approval clears only the local gate

- **GIVEN** a locally coherent awaiting_approval LoopEntity
- **WHEN** TransitionTo requests running, failed or cancelled
- **THEN** State changes and PendingApproval becomes nil together
- **AND** outcome, timestamps, cancellation metadata and PendingToolResults remain unchanged
- **AND** a request for complete is refused unchanged

#### Scenario: Local validity does not satisfy delivery correlation

- **GIVEN** a locally valid approval record with missing or conflicting lane-required correlation
- **WHEN** a delivery owner considers effects or settlement
- **THEN** its existing full correlation and evidence checks retain their classified refusal
- **AND** local Validate success does not authorize publication or ACK

#### Scenario: Startup installation does not require retained stream evidence

- **GIVEN** current approval records satisfying local validity and existing startup record/deadline checks
- **WHEN** the approval deadline owner is replaced
- **THEN** installation requires no new retained request or response lookup
- **AND** later delivery handling still performs its required lane-specific evidence checks

#### Scenario: Restoration cannot regress current authority

- **GIVEN** a stale nonterminal snapshot and newer terminal authority or an incompatible current gate
- **WHEN** restoration or replacement is attempted
- **THEN** stale state does not overwrite the newer state
- **AND** a changed durable revision is reread and reclassified rather than overwritten

#### Scenario: Fresh storage and replacement share the contract

- **GIVEN** freshly provisioned storage
- **WHEN** the component starts, creates records under this contract and is subsequently replaced
- **THEN** cold start and replacement use the same admitted state and local-validity rules
- **AND** no legacy-record conversion is introduced

### Requirement: All six loop input classes settle after owner-specific durable done

Agentic-loop SHALL classify task, response, tool-result, cancel-signal, approval-response, and governance-verdict
deliveries through their existing binding owners. Task, response, and tool-result SHALL use the permanent typed
heartbeat owner. Cancel signal, approval response, approved verdict, and rejected verdict SHALL retain native
settlement only in their four private binding owners and SHALL expose no native message or work-owning no-heartbeat
adapter.

Each non-heartbeat physical subscription SHALL invoke its typed business handler using the callback installed by its
production setup branch. All delivery-derived work SHALL join before the private callback passes its decision and
cause to `natsclient.SettleDelivery`. JetStream consumer configuration owns AckWait and redelivery; agentic-loop SHALL
NOT derive a universal work deadline from AckWait. An operation MAY use an ordinary business timeout. A physical
subscription SHALL move to the existing heartbeat owner only after measured legitimate work can exceed its
configured acknowledgement interval. Cancellation-ignoring or non-returning work SHALL fail lifecycle review.

Decode, correlation, KV, Store, transition, and required publication failures SHALL NOT become successful callback
completion. ACK means the lane-specific durable transition or defined refusal and every required PubAck completed;
Retry means the lane's declared correlation and durable evidence make re-execution safe; Terminate means permanently
invalid with no useful retry; Quarantine means collision, impossible correlation, panic, or invariant failure
prevents a safe choice.

Cancel SHALL remain the entire durable UserSignal vocabulary. ApprovalResponse SHALL remain a separate input.
`LoopStatePaused` and wire value `paused` SHALL be removed from the valid state vocabulary, exported transition
acceptance, schema, examples, and documentation. Persisted `state:"paused"` SHALL be refused as invalid. No
compatibility shim, alias, reserved enum, migration, checkpoint, supervisor, or workflow state machine SHALL be
added. `ResponseAction.Signal` and `ClassifiedIntent.SignalType` SHALL remain outside ownership of durable
`agent.signal.*` settlement.

A cancel that validates already-terminal current authority SHALL acknowledge an effect-free inapplicable delivery,
not claim a newly completed cancellation. It SHALL log the signal, loop, terminal state and why no cancellation is
needed, and increment one private unlabeled counter in the existing loop metrics when metrics are enabled. The count
is per observed delivery, not per unique signal. This branch SHALL NOT mutate durable authority or selected completion,
repair missing COMPLETE evidence, or publish a business outcome. Failed authority observation SHALL NOT emit this
diagnostic or establish inapplicability. Existing transient-state cleanup and source settlement remain unchanged.

The first fatal result from any loop delivery owner SHALL synchronously latch into the component's existing health
surface before owner-stop observation drains the exact handle. Owner loss SHALL take status precedence over
trajectory-audit degradation without reclassifying either condition: health SHALL report `Healthy=false`, status
`delivery ownership lost`, the exact cause in `LastError`, and exactly one increment of the existing error count.
Later fatal results SHALL neither overwrite nor recount the first cause. This adds no metric family, public state,
durable state, or communication path.

#### Scenario: Paused state is refused everywhere

- **GIVEN** an exported transition input, decoded persisted loop, or schema-validated document carries `paused`
- **WHEN** state validation runs
- **THEN** it is refused as an invalid state
- **AND** no shim, alias, reserved enum, or migration converts it to another state

#### Scenario: Operational quiesce is lifecycle shutdown

- **GIVEN** an operator needs the component to quiesce
- **WHEN** Stop is invoked
- **THEN** admission stops, exact handles drain, owned work cooperatively cancels, and all work joins
- **AND** no arbitrary execution pause or resume API is used

#### Scenario: Future suspension requires a new contract

- **WHEN** suspend-at-next-durable-boundary behavior is proposed
- **THEN** it requires a new evidence-backed capability contract and owner ruling
- **AND** this capability supplies no reserved state or compatibility API for it

#### Scenario: Required output publication fails

- **WHEN** a handler computes a transition
- **AND** a required publication does not receive PubAck
- **THEN** the source is not positively acknowledged
- **AND** its disposition preserves safe redelivery or quarantines an unsafe invariant

#### Scenario: Duplicate is proven applied

- **WHEN** a redelivered input's exact identity is present in a committed later request or terminal outcome
- **THEN** agentic-loop acknowledges the duplicate without repeating non-idempotent work

#### Scenario: Missing process correlation is not proof of staleness

- **WHEN** a delivery arrives after replacement and its process map entry is absent
- **THEN** agentic-loop performs exact durable read-through
- **AND** does not log-and-drop or ACK solely because memory is empty

#### Scenario: Required correlation conflicts

- **WHEN** one stable task, request, or tool-execution identity is observed with conflicting required correlation
- **THEN** agentic-loop quarantines and stops the exact owner
- **AND** does not apply either mapping by preference

#### Scenario: Unknown control signal is permanent

- **WHEN** a registered UserSignal carries any value other than cancel
- **THEN** validation or handling terminates it as permanently invalid
- **AND** no warning-only return becomes ACK

#### Scenario: Cancel completes durably

- **WHEN** an admitted cancel signal is handled
- **THEN** current cancellation state and `COMPLETE_<loopID>` commit
- **AND** the terminal event receives PubAck before source ACK

#### Scenario: Approval handler panics

- **WHEN** approval work panics
- **THEN** handler recovery returns a non-nil fatal-classified error
- **AND** the production delivery callback returns Quarantine without persistence or settlement
- **AND** the exact owner stops and drains
- **AND** the panic is never rewritten to nil

#### Scenario: Loop delivery metadata is unavailable

- **WHEN** a loop settlement adapter cannot observe native delivery metadata
- **THEN** it invokes no loop work and makes no heartbeat or settlement call
- **AND** quarantines with `delivery_metadata_unavailable`
- **AND** drains the exact consume handle
- **AND** loop health becomes negative with the exact cause and one error-count increment

#### Scenario: Verdict arrives without a waiter

- **WHEN** an exact verdict arrives after replacement with no waiter
- **THEN** while retained, its validated identity remains recoverable for response replay
- **AND** missing or full process channel is not completed log-and-drop

#### Scenario: Loop business work reaches its own deadline

- **WHEN** a delivery-owned loop operation reaches a timeout required by that operation
- **THEN** its context is cancelled
- **AND** all operation work joins before the callback settles or returns

### Requirement: Loop recovery is lane-specific and read-through

Agentic-loop SHALL load the exact `LoopEntity` identified by an incoming delivery and reconstruct only the material
required for that delivery. Ordinary recovery SHALL NOT enumerate or replay the full AGENT stream.

#### Scenario: Model response arrives after replacement

- **WHEN** AgentResponse carries a structured RequestID
- **THEN** agentic-loop resolves LoopID and loads current loop state
- **AND** reconstructs request context from committed AgentRequest material
- **AND** validates the response against the active or already-applied turn

#### Scenario: Tool result arrives after replacement

- **WHEN** ToolResult carries RequestID and execution identity
- **THEN** agentic-loop reads the originating AgentResponse
- **AND** reconstructs the ordered batch from response and accumulated durable results
- **AND** publishes the next required output at least once and waits for PubAck before source ACK

### Requirement: Tool execution has stable framework correlation

The framework SHALL preserve provider ToolCall ID for conversation semantics and stamp a distinct execution identity
derived from RequestID, provider CallID, and positive call ordinal. Tool, approval, governance, and completed-outcome
correlation SHALL use the framework identity.

`ToolResult.Name` SHALL remain optional. The matched originating ToolCall SHALL supply the conversation tool name;
an omitted result Name SHALL NOT be treated as conflicting required correlation during live handling, recovery,
or applied-proof checking. A supplied nonempty Name SHALL agree with the matched call. RequestID, execution identity,
provider CallID, ordinal, and the existing result-content proof requirements SHALL remain unchanged.

#### Scenario: Provider repeats a CallID in another request

- **WHEN** two provider responses use the same CallID under different RequestIDs
- **THEN** their execution identities differ and their completed outcomes cannot collide

#### Scenario: A correlated tool result omits its optional name

- **GIVEN** an ordinary, compact, panic, or policy-rejection result with matching required execution correlation
- **WHEN** its optional Name is omitted during live handling or after replacement
- **THEN** the originating call supplies the conversation tool name
- **AND** omission alone does not cause a correlation refusal
- **AND** a supplied nonempty conflicting Name retains the existing correlation refusal

### Requirement: Loop task, request, and tool work use only required correlation

Every TaskMessage producer SHALL supply a nonempty canonical LoopID before validation, envelope marshal, and
publication. Each execution producing new loop work SHALL mint one fresh random version 4 UUID locally.
Retry of the same already-marshaled publication and downstream redelivery of its retained AGENT bytes SHALL reuse
TaskID, LoopID and bytes. No different task may attach to an existing execution. A fresh execution of an upstream producer is a
separate production attempt outside this identity-reuse claim. Agentic-loop SHALL validate TaskID-to-LoopID mapping,
SHALL reject conflict, and SHALL NOT mint, derive, scan for, or separately persist a replacement for absent identity.
Provider work SHALL carry a stable RequestID. Tool work SHALL carry the framework execution identity derived from
RequestID, provider CallID, and positive call ordinal.

Created, request, approval, continuation, and terminal publications are ordinary durable at-least-once outputs.
Their source ACK SHALL wait for required PubAck. `Nats-Msg-Id` MAY provide bounded duplicate suppression but SHALL NOT
be treated as permanent identity or proof of publication. Exact retained reads SHALL exist only at named boundaries
where they prevent repeating non-repeatable work or prove a lane-specific durable transition already applied.

For ordinary agentic-loop success, failure and cancellation, `COMPLETE_<LoopID>` SHALL select one terminal
outcome using Create. If the record already exists, the owner SHALL read, validate and reuse its ordinary
terminal payload rather than replace it. Only the selected outcome SHALL drive required terminal effects and
publication. Cancellation SHALL follow the same rule and SHALL NOT replace a saved outcome. An existing
malformed or identity-conflicting record SHALL retain classified refusal; uncertain storage outcomes SHALL Retry.

`LoopCompletedEvent.SyntheticDecideRequired` SHALL record whether the existing completion builder computed a
required synthetic graph-decision action. This field SHALL NOT change the eligibility predicate or populate
the user-facing Decision field. When true, initial execution and replay SHALL complete the existing synthetic
action using the selected completion's LoopID and Result before terminal publication. Missing or false SHALL
request no such action. Replay SHALL NOT infer the obligation from volatile trajectory, Decision absence,
or historical-record heuristics.

For every terminal `LoopEntity` transition, the bare `AGENT_LOOPS/<LoopID>` terminal write SHALL be the final
lane-applied marker after all settlement-required terminal effects for that lane, including the selected
`COMPLETE_` record, settlement-required synthetic effects, and terminal-event PubAck where applicable.
Best-effort trajectory audit and the existing atomic completion/failure graph batch, including
evidence-integrity condition evidence, SHALL remain nonblocking and are not marker prerequisites.

Before the final marker succeeds, an attempt beginning from nonterminal durable authority SHALL leave that
authority nonterminal. A failed pre-marker attempt SHALL discard speculative process-local terminal state,
retain the selected completion, and preserve the lane's stage-specific settlement disposition. After required
terminal effects and terminal-event PubAck, a transient final-marker persistence failure SHALL return Retry.
This failure alone SHALL NOT quarantine or stop the delivery owner. Redelivery SHALL reread current authority
and, when the marker remains uncommitted, reuse the selected completion with the existing lane-specific evidence.
Fatal or invalid errors SHALL retain their existing classified refusal. Earlier unknown-effect or publication
failures SHALL retain their existing lane-specific dispositions; saved completion alone SHALL NOT authorize
automatic retry of an unresolved effect. Required effects MAY repeat compatibly with the selected outcome.
A changed authority revision SHALL NOT be overwritten with stale speculative state.

Already-cancelled durable authority SHALL NOT be regressed because its COMPLETE_ record is absent.
Existing malformed-state and identity-conflict refusals SHALL remain unchanged. The final terminal marker
proves application only where the lane's required correlation identifies the delivered source; neither bare
terminality nor selected-record existence is generic tool-execution or source-applied proof.

#### Scenario: task identity is fixed before durable publication

- **GIVEN** first-party dispatch or rule production of a new TaskMessage
- **WHEN** the producer validates and marshals the registered envelope
- **THEN** LoopID is already a random version 4 UUID in canonical form
- **AND** retry of that same already-marshaled publication and downstream redelivery of its retained AGENT bytes reuse
  that TaskMessage identity
- **AND** a fresh execution of the upstream producer is outside this identity-reuse claim
- **AND** agentic-loop does not mint, derive, scan for, or separately persist a replacement identity

#### Scenario: task mapping is stable across process replacement

- **GIVEN** the exact registered bytes of a rule-produced TaskMessage have been retained
- **WHEN** agentic-loop is replaced and those bytes redeliver after any earlier loop birth evidence committed
- **THEN** the task still names exactly the producer-supplied LoopID
- **AND** recovery validates the same TaskID-to-LoopID mapping and cannot birth a second loop identity

#### Scenario: Task mapping is stable across redelivery

- **WHEN** a task redelivers after its LoopEntity or initial request committed
- **THEN** agentic-loop validates the same TaskID-to-LoopID mapping
- **AND** any required ordinary publication may repeat and receives PubAck before source ACK

#### Scenario: Request or execution correlation conflicts

- **WHEN** one RequestID or framework execution identity names conflicting required correlation
- **THEN** agentic-loop quarantines the source delivery
- **AND** does not advance the loop or choose either mapping

#### Scenario: Ordinary required publication repeats

- **WHEN** PubAck uncertainty causes a created, request, approval, continuation, or terminal publication to repeat
- **THEN** the duplicate is an admitted at-least-once outcome
- **AND** consumers use the lane's required correlation and durable transition rules

#### Scenario: Terminal settlement fails before its applied marker

- **WHEN** a settlement-required `COMPLETE_`, synthetic effect, or terminal publication fails before the final bare
  `LoopEntity` Put
- **THEN** the bare durable record remains nonterminal and speculative process-local state is discarded
- **AND** redelivery reconstructs from exact retained evidence and ordinary terminal effects may repeat

#### Scenario: Terminal publication precedes the applied marker

- **WHEN** terminal publication receives PubAck but the final bare `LoopEntity` Put has not committed
- **THEN** the lane is not settled and source ACK is withheld
- **AND** a replacement may repeat the ordinary terminal publication

#### Scenario: Transient final-marker persistence failure retries

- **GIVEN** the selected terminal outcome is saved and required terminal effects and publication have completed
- **WHEN** the final conditional LoopEntity write fails transiently after terminal-event PubAck
- **THEN** the owner returns Retry without source ACK and discards speculative process state
- **AND** that persistence failure alone does not quarantine or stop the consumer
- **AND** redelivery rereads current authority and reuses the saved outcome when the final marker remains uncommitted
- **AND** earlier fatal, unknown-effect and unknown-publication exits retain their existing lane-specific dispositions

#### Scenario: Exact model response is proven applied

- **WHEN** the current retained `AgentRequest` matches the delivered `AgentResponse` RequestID and the final terminal
  `LoopEntity` marker exists
- **THEN** agentic-loop may acknowledge that model-response duplicate as already applied

#### Scenario: Terminal loop does not prove a tool result applied

- **WHEN** a cold `ToolResult` is durably correlated but only a bare terminal `LoopEntity` is present
- **THEN** agentic-loop retries until execution-specific applied proof exists
- **AND** task 4 neither acknowledges the tool result nor reconstructs its ordered batch

### Requirement: Approval continuation after replacement is exact and evidence-bounded

When an approval-required `ToolResult` first establishes its gate, agentic-loop SHALL persist both awaiting-approval
state and the result in current `LoopEntity` before source ACK. That durable pending write SHALL precede publication of
`ApprovalPendingEvent`; a failed write SHALL prevent the prompt publication.
Replay after phase advancement SHALL follow `Approval-required tool statuses settle by observed execution phase`.

`ApprovalPendingEvent` SHALL expose the pending framework ExecutionID as `execution_id`. `ApprovalResponse` SHALL
require that opaque identity in addition to its existing fields. The response SHALL echo the identity of the gate
reviewed, not an identity computed by the caller or substituted from a later gate. A timeout response SHALL echo
the ExecutionID of its expired pending snapshot. Missing response identity SHALL terminate as invalid input.

After payload validation, the owner SHALL exact-read and validate current `LoopEntity` before deciding
applicability. A matching pending ExecutionID SHALL use the existing pending/request/response validation and
approve, modify, reject, or timeout branch. A valid, coherent current loop with another pending ExecutionID or
no pending gate SHALL make the decision inapplicable.

An inapplicable decision SHALL produce a structured refusal/skip log and increment a narrow private loop metric,
then positively settle without business publication or durable authority mutation. The log SHALL identify LoopID
and submitted ExecutionID and describe inapplicability, never application or success. Metric labels SHALL remain
bounded and SHALL NOT contain those identities. This diagnostic extension SHALL introduce no receipt, public
status, public helper, configuration surface, or applied-decision graph/audit event.

Inapplicable settlement SHALL mean only that the submitted decision cannot act on the gate currently exposed by
the loop. It SHALL NOT claim that the submitted identity existed, the decision applied, its approver won, or its
requested effect completed. It SHALL NOT overwrite decision provenance. A missing, unreadable, or malformed
authority SHALL retain its existing unresolved/absence/poison disposition; failed observation or missing process
memory SHALL NOT establish inapplicability.

There SHALL be one logical approval gate per execution. Retry SHALL reconstruct the same gate, and replay SHALL
NOT reopen it after closure. A new human review after closure SHALL require a new execution. Existing same-gate
single-resolution and provenance obligations SHALL remain unchanged.

Matching-gate reconstruction SHALL use current `LoopEntity`, latest exact `agent.request.<LoopID>`, and exact
`agent.response.<RequestID>`. It SHALL perform no stream scan and no `ToolResult` lookup by provider CallID.

Provider CallID SHALL be interpreted only within the current RequestID. An older response carrying the same CallID
SHALL not participate.

A transient or unresolved required read SHALL Retry. Confirmed retained absence of required matching-gate evidence
SHALL durably fail
`continuation_unavailable`. Malformed or identity-conflicting evidence SHALL Quarantine. Durable applied-state proof
SHALL permit settlement; otherwise the required continuation publication MAY repeat and SHALL receive PubAck before
source ACK. A matching ExecutionID with conflicting CallID or other required correlation SHALL Quarantine without
clearing pending state. Different pending ExecutionID SHALL instead follow the inapplicable rule above.

Applicable approve/modify SHALL preserve the actual decision's approver and chosen arguments in the dispatched
ToolCall. Reject/timeout SHALL preserve existing rejection provenance. `PendingApproval` in durable current
authority SHALL clear only after required PubAck or durable applied-state proof; an inapplicable delivery SHALL
clear nothing. Local candidate mutation follows `LoopEntity has one operational state contract` and SHALL NOT
be mistaken for durable gate closure.

Approval continuation SHALL use the existing loop KV and exact retained request/response evidence above, without
an additional continuation Store, configuration, digest, or associated cleanup. Owner comment `5654729986` retires
that plan after the R2 replacement proof. Successful continuation after required message eviction is not promised,
including eviction before the approval deadline; confirmed absence SHALL follow `continuation_unavailable` above.
The existing default approval timeout, KV-retention validation and DiscardNew/admission policies remain unchanged.

The ApprovalResponse amendment SHALL NOT change ordinary final-tool-result or model-response applied-proof
requirements. The separately approved approval-required ToolResult requirement below
admits its pre-mutation exact authority read even on warm routes and its narrow phase-supersession outcome.
The retirement supersedes only the additional approval-Store obligation accepted through comment `5463183450`,
not its unrelated policies or later amendments. A pass under inapplicable-decision semantics SHALL NOT be described
as proof of the superseded historical applied-decision claim.

#### Scenario: Same CallID exists under two requests

- **GIVEN** an old and current `AgentResponse` share CallID but have different RequestIDs and arguments
- **WHEN** continuation reconstructs
- **THEN** only the response named by the current `AgentRequest` participates
- **AND** the older response cannot change or satisfy the result

#### Scenario: Exact approval continuation matches

- **GIVEN** the response ExecutionID matches the current pending gate
- **WHEN** current state and retained request/response evidence validate and agree
- **THEN** approve or modify publishes tool work at least once or proves its durable transition already applied
- **AND** reject or timeout publishes a rejection transition at least once or proves it already applied
- **AND** `PendingApproval` in durable current authority clears only after required PubAck or durable applied-state proof
- **AND** applicable approve/modify retains the actual approver and chosen arguments
- **AND** reject/timeout retains existing rejection provenance

#### Scenario: An old decision arrives during another execution with the same CallID

- **GIVEN** an old decision names execution A
- **AND** coherent exact current state exposes execution B with the same LoopID and provider CallID
- **WHEN** the old decision arrives
- **THEN** the owner logs and counts the decision as inapplicable and positively settles it
- **AND** it publishes no business output and leaves durable authority and B's pending state unchanged
- **AND** it neither applies A's approver or arguments to B nor fabricates applied-decision provenance

#### Scenario: A decision is redelivered after its gate closes

- **GIVEN** a valid decision and exact coherent current loop authority with no pending gate
- **WHEN** the decision is redelivered after replacement
- **THEN** the owner logs and counts inapplicability and positively settles the source
- **AND** business publications and durable authority mutations are zero
- **AND** it claims neither historical application nor a winning approver and emits no applied-decision audit event

#### Scenario: A direct approval omits its gate identity

- **WHEN** an ApprovalResponse omits ExecutionID
- **THEN** the owner terminates the invalid input
- **AND** it neither substitutes current identity nor publishes or changes pending authority

#### Scenario: Timeout preserves the expired gate identity

- **GIVEN** the timeout owner snapshots pending execution A
- **WHEN** it publishes the timeout decision
- **THEN** the decision carries A's ExecutionID
- **AND** if current authority has moved to B before delivery, the decision settles as inapplicable without changing B

#### Scenario: Replay cannot reopen a closed execution

- **GIVEN** an execution's logical approval gate has closed
- **WHEN** its request, approval-required result, or decision is retried or redelivered
- **THEN** the closed execution does not acquire a new approval gate
- **AND** a new human review requires a new execution
- **AND** replay before closure reconstructs the same gate identity

#### Scenario: Competing decisions target the same open gate

- **GIVEN** conflicting decisions carry the same current pending ExecutionID
- **WHEN** their handling overlaps
- **THEN** the existing single-resolution and provenance obligations remain satisfied
- **AND** ExecutionID equality is not treated as proof of cross-owner exclusion

#### Scenario: Required retained evidence is confirmed absent

- **GIVEN** the decision names the current pending execution
- **WHEN** observed retention says required reconstruction evidence should remain but its exact subject is absent
- **THEN** the loop durably fails with `continuation_unavailable`

#### Scenario: Approval evidence conflicts

- **GIVEN** the response ExecutionID matches the pending gate
- **WHEN** CallID or another required identity, name, argument, or durable applied-state fact conflicts
- **THEN** the delivery quarantines
- **AND** no pending state clears

### Requirement: Approval-required tool statuses settle by observed execution phase

For a validated `approval_required` ToolResult, the existing loop delivery owner SHALL exact-read and validate
current LoopEntity with its revision before accumulator, pending-tool, trajectory, restoration, or gate mutation.
This SHALL apply to warm and cold delivery. Evidence SHALL use only current LoopEntity and the already-admitted
exact originating-response and latest-request reads; no scan, CallID-indexed lookup, or new authority is admitted.

The owner SHALL positively settle the status as superseded only when validated evidence for the exact execution
proves progression beyond its approval gate phase. Positive evidence SHALL be a coherent closed-gate checkpoint
retaining the exact gated result, a correlated post-gate result retained in current LoopEntity, or a correlated
later-request history entry. History proof SHALL validate the originating stamped assistant batch and the
execution's ordinal-selected tool message, not provider CallID or a different RequestID alone.

The closed-checkpoint predicate SHALL rely on the invariant that unseen gated siblings are not accumulated under
another open gate. Map membership, absent pending state, process absence, bare terminal state, or arbitrary unequal
content SHALL NOT independently prove supersession. A matching open gate SHALL NOT be classified as superseded.
Contradictory required correlation SHALL retain existing conflict refusal; unresolved evidence SHALL Retry.

Superseded settlement SHALL log the execution, increment a private unlabeled loop counter, and ACK with no business
publication, durable authority mutation, accumulator replacement, or fabricated applied-decision provenance.
Execution identifiers SHALL appear only in the log, not metric labels. It SHALL claim only phase supersession, not
the historical winning decision or completion of every later effect. Ordinary final-result content proofs and
model-response proofs SHALL remain unchanged.

A matching pending gate SHALL reconstruct any required prompt from its persisted snapshot and receive PubAck
before source ACK. It SHALL NOT reset gate identity, RequestedAt, or timeout or rewrite pending state merely to
replay that prompt. A new eligible gate SHALL retain pending-before-prompt durability. An unseen different
approval-required sibling SHALL Retry or return the existing classified refusal before insertion or other mutation.

Any new gate-state write SHALL be conditional on the observed KV revision and precede its required publication.
A lost revision SHALL Retry without publishing the speculative gate or using unconditional Put. A stale
observation or process restoration SHALL NOT regress committed closure. Existing owner synchronization SHALL be
used; any necessary local critical section SHALL be explicit, bounded, and tested. No new coordination registry,
runtime, durable state, store, or receipt is authorized.

#### Scenario: Warm replay follows a committed gate close

- **GIVEN** exact current authority proves that the delivered approval-required execution passed its gate
- **WHEN** the original status redelivers with a warm execution route
- **THEN** the owner classifies it before handler mutation and ACKs it as superseded
- **AND** it logs the execution, increments the private counter, publishes no business output, and leaves authority
  and accumulated results unchanged
- **AND** it does not reopen the gate or fabricate applied-decision provenance

#### Scenario: A post-gate result supersedes the old gated status

- **GIVEN** current authority retains a correlated final or synthetic rejection result for the exact execution
- **OR** validated later-request history records that execution's post-gate progression
- **WHEN** its old approval-required status redelivers
- **THEN** the owner may ACK that gate-phase status as superseded without replacing the later result
- **AND** this does not permit two unequal ordinary final results to satisfy each other's applied proof

#### Scenario: Matching pending prompt publication remains required

- **GIVEN** current authority still exposes the matching gate and its retained gated result
- **WHEN** the approval-required status redelivers after pending persistence but uncertain prompt PubAck
- **THEN** the owner reconstructs the same pending prompt and awaits required PubAck before ACK
- **AND** the gate identity and original deadline remain unchanged
- **AND** a publication failure retries rather than being classified as superseded

#### Scenario: A different unseen gated sibling arrives

- **GIVEN** current authority exposes gate A and does not contain consumed-gate evidence for execution B
- **WHEN** an approval-required result for B arrives
- **THEN** the owner retries or returns its existing classified refusal before any insertion or other mutation
- **AND** closing A cannot turn that refused delivery into a fabricated consumed-gate record for B

#### Scenario: A genuinely new gate remains live

- **GIVEN** exact authority and existing correlation checks admit a first approval gate for the execution
- **WHEN** its approval-required result arrives without positive supersession evidence
- **THEN** the owner conditionally persists the new pending snapshot before publishing its prompt
- **AND** successful required PubAck permits normal source settlement

#### Scenario: The observed revision loses to gate closure

- **GIVEN** gate handling holds an older exact authority revision
- **WHEN** another owner commits closure before its conditional gate-state write
- **THEN** the stale write fails and the source retries without speculative gate publication
- **AND** no unconditional Put or process restoration regresses the committed closure

#### Scenario: Phase evidence is unavailable or conflicting

- **WHEN** phase observation is unresolved, including missing current authority or a transient evidence-read failure
- **THEN** the owner retries without business or authority mutation
- **AND** malformed or required-correlation-conflicting evidence retains existing poison/conflict refusal
- **AND** process state, terminal state, or arbitrary map membership never authorizes quiet ACK
- **AND** the existing reconstruction path still durably fails with `continuation_unavailable` when required
  retained evidence is confirmed absent; that confirmed-absence outcome is not converted to indefinite Retry

### Requirement: Loop-state authority has one port declaration

Agentic-loop SHALL select its loop-state bucket only from the normalized KV-write facts of its admitted output
named `loops`. The default SHALL remain AGENT_LOOPS. `DeclarePorts` and `NewComponent` SHALL share the existing
configuration derivation.

The loop-side exported `Config.LoopsBucket`, JSON setting `loops_bucket`, default and generated-schema entry
SHALL be removed. Any supplied top-level `loops_bucket` key SHALL fail configuration admission regardless of
value, including a value equal to the port bucket. The error SHALL name the retired key and canonical replacement.
No compatibility alias, ignored value or raw-name precedence SHALL remain.

Research common-bucket validation SHALL obtain agentic-loop's effective bucket through its existing `DeclarePorts`
and canonical port facts. It SHALL compare that bucket against tools and research stages using their unchanged
current selections. It SHALL NOT change those owners' provisioning or execution behavior.

#### Scenario: Default and custom port identities agree across entry points

- **WHEN** valid default configuration or a valid custom `loops` KV-write override is decoded
- **THEN** DeclarePorts, NewComponent and loop initialization select the same bucket
- **AND** no raw bucket field or consumer-local default selects another bucket

#### Scenario: Removed JSON key is never ignored

- **WHEN** configuration supplies `loops_bucket`, including null, empty, default-valued or matching-port values
- **THEN** DeclarePorts and NewComponent return a configuration error naming the key and canonical replacement
- **AND** no bucket or dependent work is acquired

#### Scenario: Research compares actual loop declaration

- **GIVEN** a selected research capability whose tools and stages name bucket A
- **WHEN** agentic-loop's effective loops port names bucket B
- **THEN** existing composition validation refuses when A and B differ and identifies the conflicting owners and values
- **AND** matching custom declarations pass without changing research runtime behavior

#### Scenario: Invalid loop port is not repaired

- **WHEN** effective `loops` configuration cannot resolve as a valid output KV-write bucket
- **THEN** configuration admission fails with component and port context
- **AND** no literal fallback, raw configuration value or partial declaration repairs it

### Requirement: Approval lifetime is bounded by loop-state authority

Agentic-loop approval timeout SHALL default to 12h when `approval_timeout` is omitted.
A supplied value SHALL be a JSON string parsing as a Go duration satisfying `0 < timeout <= 12h`.
Explicit empty, null, non-string, malformed, zero, negative and above-12h values SHALL fail configuration admission
before dependent loop work. Invalid values SHALL NOT be defaulted or clamped.

Startup SHALL additionally require the actual loop-state authority policy defined by
"Loop-state authority is acquired and observed before loop work."
Scalar timeout validation SHALL NOT substitute for observed KV policy, nor SHALL a shorter timeout admit a bucket
with a different TTL.

The 12h maximum and observed TTL24h provide nominal grace only. They SHALL NOT be represented as a guarantee of
successful continuation, timeout application or settlement before expiry. This configuration rule SHALL NOT reset,
shorten or otherwise rewrite an already retained pending approval deadline.

#### Scenario: Timeout is omitted

- **WHEN** configuration omits `approval_timeout`
- **THEN** its effective value is 12h
- **AND** startup still observes and admits the actual loop-state authority before dependent work

#### Scenario: Explicit valid duration reaches the inclusive limit

- **WHEN** a supplied duration is positive and no greater than 12h
- **THEN** scalar timeout validation accepts it, including exactly 12h
- **AND** the configured value is preserved without clamping

#### Scenario: Explicit invalid or excessive timeout is refused

- **WHEN** a supplied value is empty, null, non-string, malformed, zero, negative or greater than 12h
- **THEN** configuration admission fails with the field, offending value and allowed duration range
- **AND** no approval wait, bucket acquisition or dependent consumer starts

#### Scenario: Replacement preserves a retained deadline

- **GIVEN** a valid retained pending approval and different valid replacement configuration
- **WHEN** replacement restores its deadline
- **THEN** the retained RequestedAt and Timeout are unchanged
- **AND** this startup configuration rule does not select or apply an approval decision

### Requirement: Approval deadlines are reconstructed narrowly

When timeout is configured, agentic-loop SHALL reconstruct awaiting-approval deadlines from current AGENT_LOOPS facts
after replacement. This owns approval timers only and SHALL NOT become a generic supervisor.

#### Scenario: Timer owner is replaced

- **WHEN** replacement occurs while a persisted approval deadline remains active
- **THEN** agentic-loop reconstructs that deadline without enumerating unrelated work for replay

### Requirement: Delivery work joins before settlement

Every goroutine spawned by delivery work SHALL join before its callback returns. A deadline cancels the operation but
SHALL NOT authorize return while work remains live.

#### Scenario: Delivery work exceeds its budget

- **WHEN** bounded work reaches its deadline
- **THEN** the owner cancels and joins before callback return

#### Scenario: Terminal approval rejection reaches a bounded graph write

- **WHEN** an approval rejection produces a terminal result and its bounded graph write reaches cancellation
- **THEN** graph-write work observes the delivery-derived context and joins before the callback returns
- **AND** the approval source is not settled while that work remains live

### Requirement: Loop shutdown closes every delivery owner

Agentic-loop shutdown SHALL stop admission, drain every task, response, tool-result, cancel, approval-response, and
verdict consume handle, await each handle's exact `Closed` signal, then cancel and join delivery workers, loop-state
observers, and the approval sweeper. Shutdown SHALL NOT return while any callback can settle, publish, or mutate loop
authority.

#### Scenario: Shutdown races all owner classes

- **WHEN** loop Stop begins while heartbeat and fast-owner callbacks and the approval sweeper are active
- **THEN** admission stops, every exact handle drains and closes, and every worker, observer, and sweeper joins
- **AND** Stop returns only after no later ACK, publication, or loop-state mutation is possible

### Requirement: Restart-safe replay observes and admits local stream bounds

Each recovery-dependent dispatch, governance, and loop owner SHALL invoke pure internal
`agentstreamadmission.ObserveAndValidate` after resolving its own PortFacts and before its own first dependent
allocation. Stream identity and requirement SHALL derive only from that component's resolved facts and local typed
source/evidence retention obligations and PubAck dependency. AckWait, BackOff, MaxDeliver and work timeout SHALL NOT
be used to infer an elapsed recovery horizon or safety margin. No owner SHALL read another config,
shared maxima, factory names, or raw JSON. Dispatch SHALL admit its AGENT outputs before USER intake. Non-agentic
components SHALL perform zero lookup.

The provider-invocation lane, including its response publisher, SHALL NOT depend on replay admission. Its retained
response reuse, permitted reinvocation on typed absence, and required PubAck remain governed by the agentic-model
settlement and publication requirements.

Admission SHALL require observed DiscardNew, age/eviction policy compatible with the named source/evidence
dependency, and no earlier message bound. Passing those policy checks alone SHALL NOT establish identity safety.
The dispatch source-to-task mapping and loop task-to-current/terminal-authority/request obligations SHALL be
proved against supported operational redelivery and first-party republication. Initial publication ordering alone
SHALL NOT stand in for that proof. This requirement introduces no elapsed recovery guarantee or indefinite
deduplication guarantee for arbitrary caller resubmission after evidence expiry.
Refusal SHALL be typed
`agent_stream_replay_inadmissible`, name observed/required values, leave only the affected closure not ready, and
allocate or positively settle nothing. It SHALL mutate no stream and persist no state. Approval lifetime is excluded
and belongs only to loop-state acquisition.

For R7 governance re-proposal only, a successful exact retained-verdict lookup returning typed absence SHALL
permit the same exactly correlated proposal to be evaluated under current policy without a finite
verdict-retention horizon prerequisite, as specified by the governance correlation requirement.
Startup admission, local Requirement and observed-retention checks SHALL NOT reintroduce that prerequisite.
Observed DiscardNew, required PubAck, #1311 source-to-verdict settlement, separate tool-effect protection,
other lanes' retention requirements and all other R8 obligations SHALL remain unchanged.
The provider exception remains separate. No new state, timer, timestamp API, recovery runtime, policy-version
pinning or guarantee after source loss is introduced.

#### Scenario: Capacity policy discards old evidence

- **WHEN** an affected resolved stream uses DiscardOld
- **THEN** that closure does not start and readiness names capacity-eviction risk

#### Scenario: Capacity is full under admitted policy

- **WHEN** DiscardNew refuses required publication
- **THEN** the producer returns Retry and retains its source without core-NATS fallback

#### Scenario: Concurrent components reject before allocation

- **WHEN** dispatch, governance, and loop start concurrently against inadmissible resolved streams
- **THEN** each affected closure remains not ready with zero dependent allocation or positive settlement
- **AND** queued USER remains unconsumed

#### Scenario: Non-agentic and mixed-stream components are isolated

- **WHEN** composition contains a non-agentic component and agentic stream overrides
- **THEN** the non-agentic component performs zero lookup and can start
- **AND** each admission-dependent agentic owner observes only its own resolved stream

### Requirement: Loop-state authority is acquired and observed before loop work

Agentic-loop SHALL use its admitted `loops` KV-write bucket and call internal `loopbucket.AcquireOwner`.
The helper SHALL get first, create only for typed `jetstream.ErrBucketNotFound`, propagate every other lookup
failure without creation, and perform exactly one get after typed `jetstream.ErrBucketExists`.
Creation SHALL declare History 10, TTL 24h and nonbinding MaxBytes.

After get, create or race-get, actual status/backing-stream observation SHALL establish History exactly 10,
TTL exactly 24h and MaxBytes `<=0`. Failed or incomplete observation SHALL refuse admission.
Drift SHALL be refused without update or reconciliation, with observed and required policy values in the error.
I/O failures SHALL preserve their cause.

Only after both observed policy and the effective approval-lifetime requirement pass SHALL the component publish
the handle, perform approval-deadline discovery, allocate dependent consumers/query subscriptions or start its sweeper.
The operation SHALL use the Start-derived context and existing failed-Start rollback.
Trajectory-audit degradation SHALL remain a separate nonblocking policy.

#### Scenario: Two owners race to create a fresh bucket

- **WHEN** two processes acquire the same absent bucket with matching declaration
- **THEN** one create wins and the other gets the existing bucket
- **AND** both observe matching actual policy before dependent work

#### Scenario: Retained or race-winning policy drift exists

- **WHEN** actual History, TTL, or MaxBytes differs
- **THEN** startup refuses without updating the bucket
- **AND** publishes no handle and allocates no dependent work

#### Scenario: Lookup fails for a reason other than absence

- **WHEN** initial lookup returns permission, timeout, transport, or another non-not-found error
- **THEN** acquisition returns it and calls CreateKeyValue zero times

#### Scenario: Concurrent create wins between lookup and create

- **WHEN** CreateKeyValue returns typed ErrBucketExists
- **THEN** acquisition performs exactly one KeyValue get and validates the winner

#### Scenario: Policy observation fails

- **WHEN** status or required backing-policy observation fails or supplies incomplete evidence
- **THEN** startup returns an error rather than treating missing values as matching policy
- **AND** no authority handle, deadline discovery or dependent work is published or started

#### Scenario: Admission refusal precedes dependent allocation

- **WHEN** loop authority or approval-lifetime admission fails
- **THEN** the component remains not ready and returns the failure through existing rollback
- **AND** deadline discovery, task/response/result/signal/approval/verdict consumers and query subscriptions have not started
- **AND** no approval sweeper is running

#### Scenario: Trajectory failure retains its separate policy

- **GIVEN** loop authority and approval lifetime are admitted
- **WHEN** trajectory audit storage is incompatible or unavailable
- **THEN** the existing observable audit degradation policy remains nonblocking
- **AND** it does not weaken loop-authority admission

### Requirement: Long-running loop heartbeat policy is valid before acquisition

Task, response, and tool-result consumers SHALL default to heartbeat 15s against BackOff `[30s,2m]`. They SHALL
validate the exact acquisition config before consumer allocation; heartbeat SHALL be no greater than half the
shortest positive BackOff. MaxDeliver SHALL be at least the number of BackOff entries, so the fixed two-entry BackOff
requires MaxDeliver at least 2. Omitted or zero MaxDeliver SHALL default to 2. An explicit value below 2 SHALL be
refused before consumer allocation; the owner SHALL NOT truncate BackOff or admit a single-delivery posture.

#### Scenario: Legacy loop default is refused before allocation

- **WHEN** setup observes heartbeat 60s and BackOff `[30s,2m]`
- **THEN** it returns a typed error naming the values and 15s ceiling
- **AND** allocates no consumer

#### Scenario: Single delivery is refused before allocation

- **WHEN** setup observes MaxDeliver 1 with BackOff `[30s,2m]`
- **THEN** it returns a typed policy error naming observed 1 and required minimum 2
- **AND** allocates no consumer

#### Scenario: Minimum valid delivery count reaches acquisition

- **WHEN** setup observes MaxDeliver 2, heartbeat 15s, and BackOff `[30s,2m]`
- **THEN** heartbeat and delivery-count validation pass
- **AND** setup may allocate the consumer with the unchanged two-entry BackOff

## REMOVED Requirements

### Requirement: Creating a loop that already exists is refused; a continuation attaches to it

**Reason:** Owner comment `5728438234` retires live attachment.
**Migration:** Submit a new task with a fresh LoopID and optional displayed PriorMessages.
Create refusal, same-task recovery and terminal suppression are preserved by the replacement requirement
"A task owns one execution without rebinding".

## MODIFIED Requirements

### Requirement: Per-loop in-process state is released at terminal, through the one release point

Every per-loop map the loop manager holds MUST be released when a loop reaches a terminal state. The release MUST
happen at the component's existing single terminal-release point after the loop's terminal observation, terminal
graph write, durable loop-state transition, and required terminal publication have completed. It MUST remain
idempotent and MUST release the loop entity, context manager, pending-tool set, queued tool calls, cached tool
definitions, tool choice, metadata, request timeout and response format, task prompt, truncation-retry counter,
trajectory step aggregate, and observed-audit-loss marker.

Release changes no durable authority. The exact `AGENT_LOOPS` record and operation-specific committed outputs remain
readable without process maps. Approval-timeout sweeping remains limited to nonterminal awaiting-approval records.
Direct create refusal and different-task correlation refusal remain owned by durable admission; process memory is defense in
depth only.

A late tool result or model response MUST NOT be positively settled merely because process state is absent or the
loop is terminal. The lane owner MUST read the exact durable loop state and use only its declared
lane-specific evidence to prove that input already applied. For an approval-required tool status only,
`Approval-required tool statuses settle by observed execution phase` also permits positive phase-supersession
proof; ordinary final-result and model-response proofs remain unchanged. Durable applied-state proof permits the owner's typed
already-applied terminal outcome. An unreadable authority or unresolved absence returns Retry. A malformed input,
required-correlation conflict, impossible transition, or contradictory durable state returns Quarantine. There is no
unconditional quiet settled-drop.

A late approval response MUST follow `Approval continuation after replacement is exact and evidence-bounded`.
Validated exact coherent current authority with a different pending ExecutionID or no pending gate permits the
observable, effect-free inapplicable ACK, without historical applied-decision proof. It MUST produce the
inapplicable structured log and private metric, no business publication or durable authority mutation, and no
fabricated applied-decision provenance. Process absence alone permits nothing. Missing or unreadable authority
retries; malformed authority follows existing poison handling. Invalid approval payloads terminate, while
matching-gate required-correlation conflicts quarantine. Tool/model applied-proof rules are unchanged.

#### Scenario: a completed loop's per-loop state is released

- **GIVEN** a loop that has run several iterations with a populated conversation, cached tool definitions, and a
  task prompt
- **WHEN** it reaches a terminal state and its terminal observation, graph write, durable state, and publication
  have returned
- **THEN** every per-loop entry the loop manager held for that token is gone
- **AND** the tests that verify this are `TestTerminalReleaseClearsEveryPerLoopMap` and
  `TestTerminalReleaseIsIdempotent`

#### Scenario: releasing does not run before the terminal readers have finished

- **GIVEN** a loop reaching a terminal state
- **WHEN** its terminal trajectory observation, terminal graph write, durable persistence, and terminal publication
  run
- **THEN** each observes the loop entity it needs, and release happens after all of them
- **AND** the test that verifies this is `TestTerminalReleaseHappensAfterTerminalReaders`

#### Scenario: a late approval response names no current gate

- **GIVEN** a settled loop whose process state has been released
- **AND** a valid approval response and exact coherent current authority with no pending gate
- **WHEN** the approval response is redelivered
- **THEN** the approval owner logs and counts inapplicability and positively settles the source
- **AND** it publishes no business output, changes no durable authority, and fabricates no applied-decision audit event
- **AND** it does not claim the decision historically applied or its approver won

#### Scenario: a late ordinary final tool or model response has durable applied proof

- **GIVEN** a settled loop whose process state has been released
- **AND** the lane's declared durable state proves the same ordinary final tool or model response was applied
- **WHEN** that response is redelivered
- **THEN** its owner returns the typed already-applied outcome and positively settles the source
- **AND** the tests that verify this are `TestLateToolResultRequiresAppliedProof` and
  `TestLateModelResponseRequiresAppliedProof`

#### Scenario: a late ordinary final tool or model response lacks applied proof

- **GIVEN** process state is absent and durable state is terminal
- **AND** the input is an ordinary final tool or model response
- **WHEN** exact required state or output evidence is absent or transiently unreadable
- **THEN** the owner returns Retry without positive settlement
- **AND** process absence or terminal state alone is not treated as proof

#### Scenario: a late approval-required status is superseded

- **GIVEN** validated execution-specific evidence proves advancement beyond the input's approval gate phase
- **WHEN** the old approval-required ToolResult redelivers after process state is released
- **THEN** the owner follows `Approval-required tool statuses settle by observed execution phase`
- **AND** it logs and ACKs supersession without business publication, authority mutation, or fabricated application proof
- **AND** bare terminal state or process absence alone remains insufficient

#### Scenario: a late approval cannot observe current authority

- **GIVEN** process state has been released
- **WHEN** the exact current loop authority is missing or transiently unreadable
- **THEN** the approval owner retries without positive settlement
- **AND** it does not infer a closed gate from failed observation

#### Scenario: a late tool or model response conflicts with required correlation

- **GIVEN** the expected lane correlation names conflicting durable state or an impossible transition
- **WHEN** a late tool or model response arrives
- **THEN** the owner quarantines it with the typed collision/refusal reason
- **AND** it does not overwrite or silently drop either value

#### Scenario: a late approval conflicts within the matching gate

- **GIVEN** exact current authority exposes the response's ExecutionID
- **WHEN** CallID or other required retained correlation conflicts
- **THEN** the approval owner quarantines the delivery
- **AND** it leaves pending authority intact rather than treating that conflict as inapplicability

#### Scenario: a settled loop's result is still readable from the durable record

- **GIVEN** a completed loop whose per-loop in-process state has been released
- **WHEN** another agent reads that loop's result through the loop-result tool
- **THEN** the full result is returned from the durable loop record
- **AND** the test that verifies this is `TestSettledLoopResultReadableAfterRelease`

#### Scenario: approval-timeout sweeping is unaffected

- **GIVEN** a loop awaiting approval past its timeout and a set of already-settled loops
- **WHEN** the approval sweeper snapshots expired approvals
- **THEN** the awaiting loop is still a candidate and the settled loops contribute nothing
- **AND** the test that verifies this is `TestApprovalSweepUnaffectedByTerminalRelease`
