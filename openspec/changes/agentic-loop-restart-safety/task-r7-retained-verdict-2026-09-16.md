# R7 retained-verdict recovery through the existing response owner

base: c347eff487f50b93bc338d764f43ef5b5ea5e133

## Authority and reused evidence

Owner rulings:

- https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5682070598
- https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5694233488

Reuse the independently accepted surface and adopter seam inventory:
`inventory-r7-retained-verdict-refresh-2026-09-15.md`,
SHA-256 `8b73908fd59f17708f9c5602fd9c4be0f4bb532f4c30bab792deade735c09a1c`.

The only measured production drift from that inventory is the independently approved live-match dispatcher:
`processor/agentic-loop/governance_dispatcher.go`,
SHA-256 `85e35f619380fc23cbbe2411b130d9f5ee2d90c5b2a57828fb9da3c7729cef24`.

Component, handlers, settlement recovery, execution identity, config and state match their inventory hashes.
Current owner-amended contracts supersede the historical finite-verdict-horizon prerequisite.
No new inventory, public interface, store, timer or recovery owner is introduced.

## File ownership and implementation

The developer owns the following bounded source/test changes. Root retains documentation and task-truth ownership.

### processor/agentic-loop/settlement_recovery.go

Extend private loopSettlementEvidenceReader with a named exact verdict operation, implemented by
natsLoopSettlementEvidenceReader through existing readExact.

Existing anchors:

- :28 private evidence interface
- :49 exact native read
- :57 only ErrMsgNotFound becomes found=false, nil
- :92 existing input-address ownership
- :335 response authority reconstruction
- :923 existing error-to-delivery classification

Resolve both canonical decision/execution subjects through their respective configured verdict input facts.
Use each input's own stream; do not assume co-location with the response or with the other verdict.
A missing reader, unresolved address, missing stream or failed read is not typed message absence.

Decode each found record through the existing registered verdict decoder and actual-subject validation.
Compare it with the expected proposal using prepareProposedToolCall and matchVerdictProposal.
Do not add raw-map fallback, another fingerprint implementation, timestamp comparison or stream scanning.

Results:

- One validated matching verdict and opposite typed absence: reuse that decision.
- Both exact reads return typed absence: permit the same correlated proposal under current policy.
- Either required read fails or remains unresolved: Retry without re-proposal.
- Required correlation conflicts: Quarantine.
- Both opposing verdicts validate and match: Quarantine without selecting either.
- Invalid carrier/input: preserve the existing verdict decoder's classified disposition.

Preserve classifications through existing errs wrappers and loopSettlementDecision.
All reads receive the exact operation context and complete synchronously. No retained context, detached
work, background dependency or unbounded waiting mechanism is added.

### processor/agentic-loop/component.go

The existing response delivery owner must supply the retained operation on every governed response path.
Its production call is :1554; authority reconstruction already precedes it at :1530.
The existing error path at :1573-1575 releases speculative process state and returns the classified error.

Thread a required private per-invocation governance operation into the existing MessageHandler implementation.
Do not install an optional reader field on an exported-constructed dispatcher, inspect VerdictPublisher for
additional capabilities, or require another public setter.

Use the existing decoder at :2570 and factor/reuse the actual-subject check at :2561 so live and retained
reads share its interpretation. Retained recovery must not call the live waiter handoff to simulate delivery.

Read the required evidence before invoking policy for absent calls. Merge reused decisions and newly evaluated
calls into the existing approved/rejected split in originating order.

The installed custom GovernanceDispatcher must remain behind this mandatory response-owner operation.
Replacing it through the existing setter must not bypass the retained read.

### processor/agentic-loop/handlers.go

Keep public signatures unchanged. Factor the existing HandleModelResponse implementation privately so:

- The Component supplies its mandatory retained-aware governance operation.
- Existing direct public MessageHandler calls retain their live business-helper behavior.
- The private operation reaches the current governance seam at :1407.
- No nil operation or missing reader silently falls back to re-proposal on the Component path.

The direct public constructors already lack KV/source-settlement ownership; they do not acquire a new
restart guarantee. Normal NewComponent users receive the behavior automatically, with no new configuration
or remembered setter. Disabled/audit behavior remains unchanged.

Preserve existing execution stamping at :1361, parent lookup at :1406, rejection-result construction,
metadata propagation, ordering and downstream tool-effect identity.

### processor/agentic-loop/governance_dispatcher.go

Reuse:

- :206 matchVerdictProposal
- :406 existing proposal-bearing waiter
- :447 Propose
- :612 prepareProposedToolCall
- :647 publishPreparedProposed

Keep live waiter registration, matching and release ownership unchanged.
Required enforce-mode preparation/publication failures must propagate through Propose's existing error return,
rather than becoming successful synthetic policy rejections. Preserve existing invalid-input classifications.
Return context cancellation as an error; retain ordinary configured verdict timeout as the existing
fail-closed business outcome. Audit/disabled behavior and public signatures remain unchanged.

Do not change missing-waiter Retry or full-waiter Quarantine. A successful retained read does not itself
authorize ACK of an orphan verdict delivery. Any later change to that settlement needs its own proof.

## TDD sequence

Every new behavior test cites the active governance requirement.

1. Exact reader matrix:
   both decision subjects; separate configured streams; one match plus opposite absence; both absent;
   failed/unresolved reads; registered decoder refusal; actual-subject conflict; proposal mismatch;
   both opposing matches producing Quarantine.

2. Actual Component response path:
   matching approval and rejection reuse without proposal publication; absent calls reach policy;
   mixed batches preserve ordering and execution identity; custom dispatcher cannot bypass reads;
   missing reader never becomes successful absence.

3. Coupled error propagation:
   required proposal publication failure and context cancellation reach the response owner;
   no synthetic rejection, required downstream publication or positive source settlement follows;
   speculative process state is released and redelivery can reuse retained authority.

4. Native replacement:
   retain real response/request/loop authority and verdict bytes, replace Component, and redeliver response.
   Prove reuse, absence/current-policy evaluation, source retention on failed required publication,
   and dual-match Quarantine stopping only the affected response consumer.
   Preserve exact operation-context cancellation and join proof.

5. Reuse existing live-match, decoder, optional-diagnostic, missing/full-waiter and tool-effect controls.
   Adapt private evidence test implementations mechanically; do not weaken their existing assertions.

The reader, mandatory wiring, propagation changes and their source-settlement tests form one implementation
slice. Source-error propagation must not land independently of the durable authority that makes retry safe.

## Remaining limits and holds

This slice does not complete R7 or R8. Observed DiscardNew, other local admission obligations, required PubAck,
and separate tool-effect protection remain required. No finite verdict-retention horizon is reintroduced.

#1311 retains proposal-source-to-verdict settlement ownership. A fake policy publisher or seeded retained
record cannot prove that separate boundary. Preserve frozen #1156, the #1312 stacking hold and combined
replacement/E2E/review gates.

This handoff requires independent conformance review before execution. Subsequent implementation still
requires its focused verification and independent implementation review.
