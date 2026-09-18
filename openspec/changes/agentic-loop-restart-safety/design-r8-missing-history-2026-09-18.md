# R8 missing-history lowering: prerequisite, not implementation approval

Base: `68c14c8eb25c512e988f740cbf7ea14b6815976f`, plus the recorded dirty checkpoint.
Architect handoff, 2026-09-18. Accepted inventory:
`inventory-r8-missing-history-2026-09-18.md`, SHA-256
`fe162dcab86781c4b52b3534c65d5a44925bd0c382fac7a87f96b471c3ecfa74`, independent INVENTORY PASS, 51/51 pins.

## Finding

Current task recovery reads no exhaustive fact distinguishing valid partial birth from lost request history.
The existing terminal owner can publish the accepted refusal, but eligibility cannot be inferred from request
absence, iterations, timestamps, or missing process memory. No runtime change is approved by this document.

The architect identified this pair under currently admitted port overrides: original task retained for 24h,
request stream for 1m, loop authority for 24h. This is a witness against current admission, not a claim that the
intended R8 contract must admit that retention relationship:

| Valid partial birth | Lost required history |
| --- | --- |
| T creates running authority E; initial request publication fails. | T creates E and publishes the request; task ACK is lost. |
| Repeated attempts may persist identical E before unsuccessful publication. | First response dispatches an ordinary tool; stop before its result. |
| Replace the component with no retained request. | Request expires; replace with original T and E retained. |

Both can expose the same T, running E, zero iterations, no accumulated results or approval gate, and absent request.
Ordinary pending-tool tracking is process-local. See `handlers.go:1240`, `:1423`, `:1630`, `:2488`, and
`component.go:1607`, `:1784`. KV revision does not name request publication; retries can write identical authority.

## Already-approved prerequisite and stop condition

R8's source/evidence admission remains unimplemented. Completing it is already authorized, but a sufficient
invariant is not yet proved. It must address supported source redelivery and first-party forwarding/republication,
not merely compare two MaxAge values. Governance still owns ordinary at-least-once forwarding
(`processor/agentic-governance/component.go:318`, `:444`). Dispatch retained-task reuse removes its earlier witness.

The next bounded question is whether actual supported wiring and observed policy can exclude this ambiguity,
including in-flight source delivery. Do not implement a classifier or assume admission resolves it before that proof.
If policy observation is insufficient, stop with the concrete boundary. Adding a durable distinction requires
owner approval and write-ordering analysis; a post-publication flag alone leaves a crash window. Refusing every
absent request instead changes the accepted valid-partial-birth behavior and also requires an owner ruling.

## Existing-owner consequence once eligibility is established

1. Preserve invalid-input refusal, task/role/model conflict precedence, matching terminal suppression,
   transient-read Retry, and validated retained-request reuse.
2. Read the exact authority revision when preparing the missing-history failure.
3. Use `BuildFailureMessages` with the existing `LoopFailedEvent` reason `continuation_unavailable`.
4. Use `persistTerminalOutcome` for selected COMPLETE reuse, publication, conditional final marker and settlement.
5. Do not send a newly prepared failure through the early terminal ACK at `component.go:1315`; that branch denotes
   already-committed terminal authority.
6. Leave approval eligibility unchanged. Its StartedAt/retention test is not a task-birth discriminator.
7. Preserve registered terminal/error routing. Add no carrier, public field, bucket, timer or conversation store.

These constraints conform to owner rulings `5727252562` (unavailable-history refusal and valid partial birth),
`5728438234` (retired live attachment), and `5712921768` (observed retention, no consumer-timer horizon).
They do not fill the unresolved condition before step 2.

## Proposed conformance wording and minimum proof

The prior-message delta currently promises reconstruction on unqualified request absence. Its requirement and
scenario need to distinguish valid partial birth. Proposed replacement, pending conformance review:

> Cold initial reconstruction SHALL use the durable task, including supplied history, only where the supported
> source/evidence contract establishes valid initial or partial birth. Exact request absence alone SHALL NOT
> authorize restarting prior execution. A matching retained request SHALL be restored without reseeding history.
> Definitively unavailable required reconstruction history SHALL produce the accepted visible refusal;
> unresolved observation SHALL retain Retry.

Reuse retained-request, correlation, terminal-suppression, PriorMessages and native cold-publication-failure controls.
The missing proofs are the sufficient eligibility invariant; production-owner refusal without initial-request
publication; and existing terminal settlement semantics applied to that case, including competing authority,
transient observation and final-marker Retry. The seeded `Iterations=2` RED is not classifier or retention proof.

This adds no orchestration layer. R8 stays open; the handoff is prerequisite analysis, not READY TO IMPLEMENT.
