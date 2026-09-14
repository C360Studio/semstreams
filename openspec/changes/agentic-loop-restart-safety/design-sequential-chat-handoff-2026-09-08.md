# Accepted design: prior conversation on each bounded chat turn

Status: independent DESIGN REVIEW PASS; owner accepted both optional PriorMessages and AutoContinue=false on
2026-09-08. Reviewed draft SHA-256 was
`b10470f4a67066f1dc27e3ff19688a3eea2b4352c83b5e1ed105c0d3789ec1cb`; this status update preserves that provenance.
The active proposal, canonical design, tasks, and capability deltas materialize this accepted target. The earlier
blanket continuation-removal proposal remains unapproved.

base: af829616305afa039dac0550efa78d07e856dd5f

Evidence: `inventory-sequential-chat-handoff-2026-09-08.md`, independent INVENTORY PASS at SHA-256
`4e4b178db78bc8e41f836e7eae21d3ba461a55a5de5e94edf23425f7047c8fb8`, 73/73 verified pins.

## Options and recommendation

| Option | Benefit | Cost |
|---|---|---|
| Explicit prior messages on existing inputs | The durable task contains its conversation input; replacement needs no earlier execution or retrieval service. | One optional field across three input DTOs; chat adapter supplies its displayed transcript; input grows with history. |
| Existing reply reference loads earlier exchange | Caller supplies a small reference. | Changes reply semantics and adds cross-execution retrieval. Retained requests mix conversation with budget/todo instructions; provider output can differ from the delivered answer; COMPLETE is one prompt/result, not an accumulated transcript. |
| Do nothing | No public change. | Completed-loop ReplyTo refuses and new submissions receive no history; the acceptance checkpoint stays unmet. |

Recommend explicit prior messages. No transcript store, conversation entity, supervisor, progress ledger, retrieval
service, or new payload category is needed. Do not use null-versus-empty presence as an execution-mode switch.

## Accepted public contract

Add to UserMessage, HTTPMessageRequest, and TaskMessage, using the existing ChatMessage type:

```go
PriorMessages []ChatMessage `json:"prior_messages,omitempty"`
```

Use `[]agentic.ChatMessage` outside package agentic. Omission, null, and an empty array all mean no supplied history.
Content/Prompt remains the new user turn. It is appended exactly once after the prior messages.

Change the existing AutoContinue default to false. Ordinary submissions without ReplyTo use the existing new-task
path and receive a fresh LoopID. Explicit ReplyTo and explicitly configured AutoContinue retain current attachment
behavior. After attachment admission succeeds, nonempty prior history is refused as conflicting intent before task
publication or live-loop mutation. Same-source redelivery recovers its retained task before consulting current
attachment state. No attachment feature is deleted.

The scoped default measurement is:

- `processor/agentic-dispatch/config.go:14` declares schema default true.
- `processor/agentic-dispatch/config.go:102` sets AutoContinue true in DefaultConfig.
- `processor/agentic-dispatch/component.go:1004` and `http.go:328` are task-selection readers.
- `processor/agentic-dispatch/component.go:861` and `http.go:236` also use this setting for implicit command targets.
- Under defaults, a command requiring a loop must name its loop_id. Correct the current suggestion to start a task
  first (`component.go:873`, `http.go:248`), since that no longer selects the command target. Explicit AutoContinue
  configuration retains the existing command fallback. Reconcile the generated schema and affected fixtures.

History contains ordered user-visible text:

- Role is user or assistant and Content is nonempty.
- Name, ReasoningContent, ToolCalls, ToolCallID, IsError, and ReasoningRecords are empty/zero.
- No system, developer, tool, or execution-internal messages.
- Preserve order and text; do not infer alternation, deduplicate, or rewrite.
- Nonempty history on a command is an input error, not an ignored field.

One private validator in agentic owns the subset contract and is consumed by TaskMessage.Validate. Generic
ChatMessage.Validate and direct AgentRequest clients retain their existing contracts. Dispatch produces observable
input errors through its existing synchronous/channel paths before task publication. A routable invalid submission
must reach that response path rather than being silently discarded by decoding.

## Responsibilities and durable done

The chat adapter supplies the transcript it actually displayed: user text and UserResponse.Content. It does not
reconstruct answers from raw AgentResponse or LoopCompletedEvent.Result; dispatch already projects Decision.Reason
for user-facing decisions. This is caller-supplied context, not verified execution provenance.

Dispatch forwards history into its existing durable TaskMessage. Retained-source comparison includes ordered roles
and content, treating nil and empty alike. Same-source identity with different history uses the existing conflict
disposition; neither transcript overwrites the other. No new task identity or recovery map is added.

Loop assembles fresh execution instructions, existing embedded context if any, prior messages, and the current prompt.
It seeds the same prior messages once into ContextManager recent history before the current prompt so tool iterations
retain them. Current budget/todo instructions are generated normally. Cold reconstruction without a retained request
uses the task's history; matching retained-request restoration reuses that request without reseeding.

| Situation | Required consequence |
|---|---|
| Valid new submission | Existing producer commits a history-bearing TaskMessage with a fresh LoopID. Required downstream PubAcks precede source ACK. |
| Dispatch replacement after task commit | Recover the committed LoopID and history, rather than selecting another current active execution. |
| Loop replacement before initial request commit | Rebuild from the durable task with history included once. |
| Matching provider output retained | Reuse it; no extra provider invocation. |
| Confirmed provider-output absence | Invoke again under the existing at-least-once contract. |
| Required durability failure | Do not ACK; use existing classified failure and speculative-state discard. |
| Invalid history or conflicting attachment | Observable input error; no new task or live-loop mutation. Required negative-response failure remains unsettled. |

Existing transport/provider limits apply. Do not silently trim history or add a caller-computed wrapping-size budget.
Observed failures propagate through the owner. Within-execution context compaction remains separate.

## Adopter effect

Under the accepted defaults, the adopter sends the new message and, for a follow-up, the prior user/assistant text.
They do not supply LoopID, RequestID, bucket, subject, or retention predictions for an independent turn. Omitted
history makes a context-free turn; it does not promise automatic conversation recall.

SemStreams supplies this input contract, not a hosted conversation store. The adapter owns retaining the displayed
transcript across its own restart. Once a TaskMessage commits, that execution's supplied history no longer depends on
the adapter or earlier execution retention. Sister owners implement their own adapter adoption; migration notes live
in SemStreams.

## Acceptance and implementation scope

Use real NATS and a deterministic fake provider through started dispatch, loop, and model components:

1. Submit turn one under defaults and observe its terminal UserResponse and settled execution.
2. Stop/join and replace the relevant components, retaining NATS and the test adapter's displayed exchange.
3. Submit a follow-up containing that exchange as prior messages. Assert a distinct LoopID, original order, prior
   messages once, current prompt once, and fresh execution instructions. The fake provider's answer depends on a fact
   present only in the earlier exchange.
4. Interrupt at the provider settlement boundary after a real response PubAck but before source ACK. Observe actual
   redelivery to a fresh component and unchanged invocation count. Retain separate absence and failed-output proofs.
5. Cover a delivered answer whose Decision.Reason differs from Result; subsequent history carries the delivered text.
6. Prove omitted/null/empty equivalence, forbidden-content refusal, retained-history conflict, and history preservation
   into subsequent within-turn iterations. Keep explicit attachment without history working.
7. Prove default submissions remain independent when another execution is active; explicit command loop targets and
   configured AutoContinue behavior remain as declared.

Touched implementation: the three existing DTOs and their validation/serialization, dispatch normalization and
retained-task comparison, initial loop message/context assembly, default/schema, focused tests, and migration notes.
No conversation-history retrieval, new bucket, state-machine runtime, mid-flight steering, or #1244 prerequisite.

## Capability delta handoff

For agentic-dispatch, add requirement **Prior messages accompany an independent chat turn**: ordinary submissions
under defaults create independent executions. PriorMessages omission/null/empty is equivalent. Valid nonempty history
is copied into the durable task; resolved attachment plus supplied history is refused before publication. Redelivery
recovers the committed task and compares ordered history as part of source correlation. Command targets under defaults
are explicit. Explicit ReplyTo/configured AutoContinue remain separate supported behavior.

For agentic-loop, add requirement **A task carries its prior conversational input**: prior messages are nonempty text
in user/assistant roles without tool/reasoning/execution fields. Initial assembly and ContextManager preserve the
ordered history and append the current prompt once. Fresh execution instructions are framework-owned; prior text
never becomes system instructions. Cold reconstruction includes history; retained-request restoration does not
duplicate it. A new history-bearing task cannot attach to an execution owned by a different TaskID.

Scenario **Follow-up after replacement**: given a completed turn whose user and delivered assistant text accompany a
later task, when components are replaced before that task executes, its initial provider request contains the prior
exchange and new prompt under a different LoopID with a fresh iteration budget.

## Review and owner boundary

Skills: kv-or-stream keeps the history-bearing task on the existing work stream; orchestration-check leaves execution
with existing component owners. No new storage owner is selected.

Independent design review covered the public field, validation/refusal reachability, AutoContinue default and command
consequence, assembly ordering, retained-source comparison, and the acceptance boundary. The owner then accepted both
choices. Implementation and its independent review remain required; design acceptance is not implementation evidence.
