# Approval decisions name the gate the human reviewed

base: 615997c658ef4c5c4ce2e8a44709913c5b6d70fe

Status: proposed for independent design review, then owner decision. No implementation or task 6.6 revocation.

Accepted inventory: `inventory-approval-applied-boundary-2026-09-10.md`, SHA-256
`6640c375572e2171790d7910de7663cf5928ea2b8aab99dd5c3d68ce50f197cb`, independent `INVENTORY PASS`, 159/159 verified
pins. Preserve that artifact unchanged as the inventory annex to this review bundle.

## Evidence and decision

Two bounded failures are distinct:

- Real replacement after approval application redelivers the identical source. The handler retries before examining
  retained execution evidence (`approval_response_handler.go:193–206`). Native log SHA-256:
  `f96e5f8a475ef7c1a48ac21030f12af8eb38312b5593644296ef271268ce056e`.
- A seeded current checkpoint B reuses A’s LoopID and provider CallID with different RequestID, ExecutionID and
  arguments. Delivering A’s serialized approval builds B’s call with A’s approver and clears B’s pending state
  (`approval_recovery_test.go:250–309`). This measures the production callback; it proves neither real execution nor
  a native two-gate history. Independent review approves this seeded callback fidelity. Log SHA-256:
  `c345f4afa7393044bb3b55f985c35325fe5d63eda3dddd4ccc556ff4d0dd5e89`; test-file SHA-256:
  `b2522d6a4d8ff4e805ef6d265a20f0fa25510dc19fbf61b72e754215e89d66b2`.

The first failure does not prove missing storage. The second exposes information the current input does not carry:
which execution the human reviewed. Pending state already has that identity; ApprovalResponse does not
(`agentic/state.go:146–156`, `agentic/approval.go:104–120`).

## Options

| Option | Benefit | Cost or limit |
|---|---|---|
| Leave the current behavior | No interface or semantic change | Preserves both measured failures; cannot support the intended approval safety claim. |
| Echo existing ExecutionID; retain strict historical applied-decision proof | Prevents selecting B with A’s identity; preserves the present meaning of successful settlement | Must still prove the particular decision’s durable application after pending clears. Retained ToolResult/history identifies execution but does not generally identify the winning decision, approver and modified arguments. No general proof is established. |
| Echo existing ExecutionID; define decisions for a noncurrent gate as inapplicable | Prevents wrong-gate application and lets legitimate late duplicates settle without reconstructing a historical winner | Explicitly changes the current strict applied-proof contract. Settlement no longer means that this decision won. |
| Use the approved Store fallback | Preserves its promised original request/response and applied-decision fingerprint | Storage alone does not tell an old input which gate it names. The public identity problem remains; post-clear fingerprint retention also needs its own proof. Task 6.6 still governs selection. |

Recommend the third option for this bounded seam. It expresses a conditional human decision against a framework-owned
execution identity. It does not establish a historical receipt service.

## Proposed bounded contract

Use the existing `ExecutionID` as the opaque gate identity. Loop already derives it from RequestID, provider CallID
and ordinal (`execution_identity.go:15–39`) and retains it through approval redispatch
(`approval_response_handler.go:118–128`). No new identifier, generator or RequestID derivation belongs in the UI.

Expose that value on `ApprovalPendingEvent` and the existing pending-approval HTTP projection. Require `execution_id`
on `ApprovalResponse` and the HTTP `ApprovalRequest`. The client echoes the value attached to the prompt the human
reviewed. Dispatch must compare that echo with current authority before publishing, and loop must compare it again
when applying. Dispatch must not silently substitute the current gate identity for an omitted or outdated echo:
that would reproduce the same bug at the HTTP boundary. The timeout publisher echoes the identity of its expired
pending snapshot.

At the native input, after payload validation and an exact current LoopEntity read:

| Observation | Outcome |
|---|---|
| Current pending ExecutionID matches | Validate existing pending/request/response correlation and apply the decision through the existing branch. Required publication precedes durable pending clear and source ACK. |
| Valid, coherent current loop has a different pending ExecutionID, or no pending gate | Record the inapplicable decision through the existing refusal/skip diagnostics, then ACK; produce no business publication or authority mutation. |
| Matching ExecutionID has conflicting CallID or inconsistent retained correlation | Quarantine; leave pending state intact. |
| Loop authority is missing, unreadable or malformed | Preserve the existing unresolved/absence/poison contract; do not infer inapplicability from failed observation. |

“Inapplicable” means only: **this decision cannot act on the gate currently exposed by this loop**. It does not claim
that the supplied identity existed historically, that this decision was applied, that its approver won, or that its
requested effect completed. It introduces no receipt record or public status vocabulary.

Inapplicable settlement is observable through the existing refusal/skip diagnostic path. Its diagnostic identifies
the loop and submitted execution and describes the decision as inapplicable, never as applied or successful.
“Publish nothing, mutate nothing” means no business consequence and no durable authority change; it does not
suppress diagnostics. This adds no receipt, public status, or fabricated applied-decision provenance.

An applicable approve/modify preserves the actual decision’s approver and chosen arguments in the dispatched ToolCall;
reject/timeout preserves the existing rejection provenance. An inapplicable delivery must not overwrite provenance
or fabricate an applied-decision audit event. The retained source remains a record of a submitted decision, not proof
of successful application.

ExecutionID is sufficient for the demonstrated A/B distinction because the two requests produce distinct execution
identities. Its sufficiency requires a declared invariant: **one logical approval gate per execution**. Retry
reconstructs that gate; a new human review after closure belongs to a new execution. Existing redispatch deliberately
keeps ExecutionID. Reopening the same execution as a fresh gate would make the echo ambiguous and must be covered
by replay proof, not assumed safe.

This change does not resolve competing approve/modify/reject inputs while the same gate remains open. The existing
single-resolution and provenance obligations still apply; the current in-process mutex is not evidence of
cross-owner exclusion. If the bounded race proof exposes a further failure, return with that evidence before
introducing coordination or retained decision state.

## Required owner rulings and spec homes

1. Accept the breaking required ExecutionID echo on the two approval inputs and its exposure on existing pending
   outputs. The HTTP contract gains a gate precondition; a stale displayed prompt receives 409 without publication,
   and an omitted identity receives 400. Direct wire input without identity terminates as invalid.
2. Amend both approval contract homes to admit the inapplicable outcome:
   `Approval continuation after replacement is exact and evidence-bounded` at
   `specs/agentic-loop/spec.md:302–334`, and the approval-specific clauses and scenarios of
   `Per-loop in-process state is released at terminal, through the one release point` at
   `specs/agentic-loop/spec.md:500–505,525–547`. Distinguish a different or no-longer-current gate from conflicting
   evidence within the matching gate. A validated exact current LoopEntity may establish that the approval is
   inapplicable without proving which historical decision won. Process absence alone establishes nothing.
   Reconcile task 7.7 and the matching active design prose, including lane 8 at `design.md:442` and its approval
   branch descriptions at `design.md:541,547`, with this approval-only exception. Tool-result and model-response
   applied-proof, Retry, and Quarantine requirements remain unchanged.
3. Add the one-gate-per-execution invariant to that requirement and its repeated-call/replay scenarios. Preserve
   PubAck-before-clear, matching-gate validation, human provenance, and the existing exact read boundary.
4. Narrow the unchanged-`LoopInfo`-schema promises in `specs/agentic-dispatch/spec.md:147,172` and tasks 6.8/6.10
   solely to permit `execution_id` on the existing nested `PendingApprovalInfo`. Carry that observed identity
   through the existing pending projection and regenerate the corresponding JSON/OpenAPI schema. Preserve all
   unrelated DTO fields and projection contracts. This exception does not authorize tracker retirement or any
   unrelated projection expansion.

These are approval-specific deviations. They do not alter tool-result or model-response applied proofs, authorize
additional reads, or revoke the Store fallback. Any later task 6.6 decision must acknowledge the revised semantics;
a pass under this contract must not be described as proof of the superseded historical-decision claim.

## Adopter path, migration and acceptance

The SemTeams UI already submits decisions through `submitApproval`
(`semteams/ui/src/lib/services/agentApi.ts:383–417`). It would retain one opaque value with the displayed prompt and
echo it. It should never compute identity or fetch a replacement identity when the user clicks an old prompt.

The do-nothing path is a visible refusal: older clients omit `execution_id` and receive HTTP 400 or invalid wire
settlement. An outdated prompt receives HTTP 409. Missing identity never selects whichever gate happens to be
current.

Migration affects approval payloads, the HTTP DTO/schema, pending projections, timeout production, and fixtures.
Current `PendingApprovalInfo` lacks ExecutionID (`loop_tracker.go:64–70`); the KV-backed `loopFromEntity` currently
omits PendingApproval (`loop_wire.go:71–98`). The existing projection must carry the observed pending record so a
client after replacement can obtain the same prompt identity. This does not require taking on tracker retirement.

Document downstream changes in SemStreams’ migration guide. Downstream owners update their own repositories.
The existing SemTeams observer treats decision publication as run-resumption input; this draft adds no claim that
publication proves application and does not redesign that observer.

Acceptance covers the two RED cases, HTTP stale-prompt refusal, all four decision branches, absent identity,
same-gate correlation conflict, noncurrent-gate settlement, and replay without reopening a closed execution.
For inapplicable settlement, assert the existing refusal/skip diagnostic, zero business publications, unchanged
durable authority, and no applied-decision audit event. Preserve provenance assertions and the existing same-gate
contention obligation. JSON/OpenAPI verification permits only the declared nested approval identity addition;
all unrelated schema-preservation assertions remain. A relevant agentic E2E must pass before the breaking change
lands.

Normal multi-turn chat remains naturally supported: new model requests already create distinct tool execution
identities even when a provider reuses CallID. No conversation store, ledger, supervisor, or additional recovery
read is part of this proposal.
