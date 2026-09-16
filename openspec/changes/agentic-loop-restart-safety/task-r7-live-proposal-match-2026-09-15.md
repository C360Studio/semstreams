# R7 live proposal-match implementation handoff

## Authority and scope

Use the accepted inventory unchanged:
`inventory-r7-retained-verdict-refresh-2026-09-15.md`,
SHA-256 `8b73908fd59f17708f9c5602fd9c4be0f4bb532f4c30bab792deade735c09a1c`,
183/183 pins, independent INVENTORY PASS.

This implements the existing requirement that conflicting proposal/verdict correlation quarantines.
It does not complete retained-verdict recovery, R7, R8 or #1311.
No specification change, new exported API, storage, context retention or coordination owner is required.

Alternatives considered: no change leaves self-consistent but wrong-proposal verdicts admissible;
combining retained reads now also requires the still-open read wiring/admission/error-propagation work.
The bounded live correction has independent value and does not require that larger combination.

## Exact private responsibility

Production changes belong in `processor/agentic-loop/governance_dispatcher.go`.

1. Extract the existing full ProposedToolCallPayload construction into one private preparation function.
   Preserve its fields, argument coercion, lifted command/URL and existing fingerprint algorithm.
   ParentLoopID remains the value supplied by the current MessageHandler owner; do not invent ancestry.
   Preparation produces the complete fingerprinted value before a live waiter can receive a verdict.

2. Keep `publishProposed` as the audit caller's existing private entry point.
   Share the actual prepared-payload publication operation with enforce Propose.
   Enforce publishes the SAME prepared value that established its expected correlation, without
   rebuilding or recomputing a second proposal. The registered envelope and subject remain unchanged.

3. Change the existing waiters map value to a private waiter record containing:
   - the prepared ProposedToolCallPayload value;
   - the existing buffered verdictArrival channel.
   This extends the existing process-local waiter; it is not another map, authority or lifecycle.
   Keep the current mutex, registration-before-publication ordering and release ownership.
   Do not retain the caller's ToolCall slice or recompute expected fields from a later mutable call.

4. Add one private proposal-comparison function beside the existing normalizer.
   The normalizer continues to own shape, required fields, duplicate-field conflicts and diagnostics.
   Comparison owns only agreement between an already-normalized verdict and the prepared proposal:

   | Verdict field | Expected value |
   | --- | --- |
   | LoopID | proposal.LoopID |
   | RequestID | proposal.RequestID |
   | ExecutionID | proposal.ExecutionID |
   | ProposalFingerprint | proposal.ProposalFingerprint |
   | CallID, when nonempty | proposal.CallID |

   Empty CallID remains allowed. Reason and RuleID are not proposal identity.
   Parent, tool, arguments and lifted fields participate through the existing full fingerprint;
   add no verdict fields or alternate fingerprint interpretation.

5. enforceDispatcher.HandleVerdict keeps its approved signature and order:
   normalize → locate ExecutionID waiter → compare → enqueue.
   A mismatch returns Quarantine with an error naming the conflicting field, before channel mutation.
   Missing waiter remains Retry; full waiter remains Quarantine; matching enqueue remains ACK.
   Do not infer missing verdict identity from the expected proposal or actual subject.

The exact unexported function/record names are implementation mechanics, not another approval gate.
There is one preparation home and one proposal-match home. Neither is a public normalize-first API.

## Preserved boundaries

- GovernanceDispatcher.Propose and HandleVerdict(VerdictPayload) remain unchanged publicly.
- No new Component dependency, constructor argument, setter, option or configuration field.
- Component registry decoding, actual-subject checking and SettleDelivery remain unchanged.
- Disabled remains pass-through; audit remains non-gating/best-effort and does not acquire live waiters.
- Preserve optional CallID, diagnostic precedence, Properties immutability and direct/wire parity.
- Preserve existing normal completion/release and supported nil-publisher behavior.
- Preparation/publication failures retain the existing per-call failure handling in this slice;
  do not quietly change them into a new source-settlement policy.
- The measured publication/cancellation-to-rejection error-propagation gaps remain open R7/R8 work.
  Preserving them here is not a claim that they satisfy the final restart-safety contract.
- No timer, goroutine, root context, stored context, durable record or duplicate-owner protocol is added.

## Clause-derived TDD

Use the current governance spec:
“Governance publications are durably at-least-once” lines81–83;
“Governance verdict correlation survives process replacement” lines58–73;
“Governance verdicts use one registered wire and typed handoff” lines125–139 and149–152.

Observe these RED assertions before implementation:

1. Through real enforce Propose, capture its published registered proposal. Send an otherwise valid
   verdict with the SAME ExecutionID but independently wrong LoopID, RequestID or fingerprint.
   Each returns Quarantine and cannot complete the call or change any waiter.
   A subsequently matching direct verdict demonstrates that refusal did not consume the buffered slot.

2. Nonempty conflicting CallID quarantines; omitted CallID succeeds. Correct CallID succeeds.
   Diagnostic changes alone do not affect matching and preserve the existing reason/rule behavior.

3. Matching approved and rejected verdicts reach only their originating calls.
   Include two calls whose provider IDs repeat but whose request/ordinal execution identities differ.
   A verdict for one execution cannot approve another.

4. Capture a nontrivial proposal containing parent, arguments, command/URL and correlation.
   Assert the existing published field/fingerprint contract survives extraction, and use the captured
   proposal as the verdict producer's evidence—not the new private builder as the test oracle.
   Cover an early verdict delivered by the publisher callback before Propose begins waiting.

5. Exercise matching and mismatching verdicts through the existing Component registered-wire handler
   as well as direct HandleVerdict. Keep malformed/missing-input and subject-conflict controls.
   A correct self-consistent envelope with the wrong originating fingerprint is the decisive new RED.

6. Extend the existing Go-native fuzz invariant/seed coverage rather than adding a framework:
   arbitrary correlation strings/Properties and equivalent registered JSON must preserve
   direct/wire outcomes; a refused or proposal-conflicting verdict cannot mutate a waiter.
   Valid seeds include both decisions, nested correlation, omitted CallID and diagnostic disagreement.
   Expected outcomes come from the cited clauses and fixed expected proposal, not the matcher.

Use channels/publisher observations for synchronization; do not add sleeps.
Touched old fixtures that currently use the literal “fingerprint” with real Propose must echo the
captured proposal instead. Do not weaken matching to retain those fixtures.

## Callers, native proof and gates

Production callers remain MessageHandler.handleToolCallResponse and Component's verdict callback;
neither needs a new public call shape.

Adapt private waiter fixtures in governance_dispatcher_test.go, execution_identity_test.go,
delivery_owner_test.go, verdict_wire_test.go, verdict_wire_integration_test.go and
fastlane_replacement_integration_test.go as compiler/reference evidence requires.
Keep test-only expected proposals explicit; retain no permissive “unknown expected identity” mode.

Add or extend one native installed-callback proof using a real Propose-created waiter:
publish the captured proposal's matching registered verdict and observe delivery before source ACK.
A separate same-subject/wrong-fingerprint case must show no ACK/Term/NAK, no waiter mutation,
and the existing exact-owner quarantine behavior. Isolate it from the matching control because
quarantine stops the consumer owner. This is LIVE matching proof, not retained recovery.

Required handoff gates: intended REDs; affected package race tests; existing wire/fuzz seeds;
bounded native fuzz; focused native callback test under the shared integration runner;
lint/vet and independent implementation review. Root owns scheduling and durable evidence.

Remaining R7/R8: retained lookup before proposal republication, observed admission/absence semantics,
replacement-before/after-publication proof and source error propagation. Existing human-approval
restart E2E and audit-governance metrics remain insufficient for enforce retained recovery.
#1311/#1312 source-settlement work stays separate and held.

The orchestration-check confirms the existing component owns these execution mechanics.
No new owner-policy choice was identified. Stop only for the requested conformance review, then TDD.
