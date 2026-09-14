# Terminal selection: alignment with the accepted operational state contract

## Authority and checkpoint

This is the architect's bounded implementation handoff for existing R2/R6 obligations, not a new policy or spec.
The first-R6 implementation passed independent review; see `review-loop-state-implementation-2026-09-13.md`.
Active normative authority remains `agentic-loop / LoopEntity has one operational state contract` and the existing
terminal-selection and source-settlement requirements in this change.

The architect compared exact unapplied candidate V4 SHA-256
`13489f70f1bf74a8dd2b9fcf168eaa15828e14237db1bf5ae4b1e7e956ca1aad` with accepted design SHA-256
`1cf370eba73c99f1ff5d38a702d813f77dc424b32513590f6283d02e5fb5d21a` and the first-R6 source.
V4 is a useful implementation basis but cannot be applied verbatim. No new owner-policy decision was identified.
The assessment performed no edits or tests and does not supply runtime proof.

## Required corrections to the existing candidate

### Preserve local coherence instead of repairing contradictory records

In V4 section 2, remove `marker.StateBeforeApproval = ""` and unconditional `marker.PendingApproval = nil`.
After obtaining the prepared marker, use its existing Validate and classify refusal before selection or effects.
Validate the completed final candidate before effects as well. Do not move outcome, timestamp or retained-message
requirements into public LoopEntity.Validate. Preserve PendingToolResults.

First-R6 TransitionTo, ResolveApproval and CancelLoop clear a gate on legitimate exits. A terminal prepared record
that still has PendingApproval is contradictory; persistence must not silently repair it.

### Retain the truncated-outcome distinction

V4 section 2 must not overwrite the failed marker's Outcome unconditionally from the failed event. Existing
failLoop prepares OutcomeTruncated where appropriate while buildFailureEvent intentionally emits OutcomeFailed.
Retain the compatible prepared marker's OutcomeFailed or OutcomeTruncated and refuse an incompatible outcome.
Continue using the selected failure's error/timestamp and publishing its failed event. This preserves existing
behavior explicitly required by the accepted design; it is not a new outcome policy.

### Carry the revision that supports the prepared operation

V4 section 5 obtains the revision supporting the unchanged gate but discards it. The shared helper then reads a
fresh revision and checks only TaskID. That could use a newer incompatible nonterminal gate as the write basis
for an older prepared result.

Carry the authority revision supporting the validated operation through existing private calls to the final
Update. A fresh observation must not silently replace it: changed authority requires reread/reclassification
before selection or effects; a later change must fail the revision-conditioned final write. Apply the same rule
to cancellation and other terminal callers. Existing private arguments/returns suffice; do not add an exported
HandlerResult field, durable revision field, registry, lock or coordinator.

Preserve the accepted protected-current-entry checks at restoration/UpdateLoop. First-R6 candidate validation
alone does not supply those checks. This remains existing R2/R6 work.

Do not require a direct edge from durable state to final marker: a legitimate approval operation may perform
awaiting_approval → running → complete before its final marker. Its existing lane evidence establishes that work.

### Preserve real cancellation evidence

Correct V4's explanation after section 5: CancelLoop now clears PendingApproval immediately, so the cancellation
fixture commits a locally valid cancelled record. Preserve it because authority changed, not because it is malformed.

Retain TestIntegrationMissingApprovalEvidenceCannotOverwriteCancellation: unchanged cancelled bytes/revision,
no COMPLETE_, no failure publication and released process state. A local Validate assertion may strengthen the
fixture; do not weaken ordering or replace the case with malformed-record coverage.

### Apply onto the current source

Retain first-R6's manager transition, installation validation, five-state fixtures and the independently reviewed
approval-result validation ordering. Redundant pending-outside-awaiting checks may use the existing authoritative
reader's strengthened Validate; full request/execution correlation remains the delivery owner's responsibility.

## Unchanged obligations and TDD slice

Keep selected COMPLETE reuse and incompatible-candidate refusal. Selected timestamps and SyntheticDecideRequired
drive replay; equal terminal content alone does not prove a particular input applied. Keep required effects,
terminal PubAck, final CAS marker and source-specific settlement in that order. Preserve warm/cold DeliveryDecision
propagation: pre-effect storage uncertainty remains retryable; approval's unknown-effect cancellation joins and
quarantines without ACK, NAK or TERM.

Keep the pre-birth correction at the existing task owner using a validated running record and non-overwriting
Create. Do not weaken the terminal helper's current-authority requirement.

After first-R6 review, implement the corrected candidate and necessary private revision/caller adaptations.
Use the existing saved-outcome, synthetic-action, pre-birth, cancellation-overwrite and warm/cold regressions.
Add bounded behavioral assertions for contradictory prepared terminal refusal before effects, truncated marker
outcome with a failed event, and authority changing after gate validation but before terminal selection, alongside
the existing final-Update conflict proof. Exact code and runtime proof close this work, not the state table alone.

Architect sign-off: proceed within those accepted boundaries, then obtain independent implementation review.
Published R1, R3's hold, AgentRun/#1249 and research/#1288 remain unchanged. No broader task family is added.

## Subsequent execution checkpoint

The first application attempt was an incomplete helper fragment and was rejected before changing production.
Independent exact-code review did not approve its application; see `review-terminal-fragment-2026-09-13.md`.
This architectural alignment sign-off does not approve that fragment or waive its complete caller/revision/decision
proof. The owner subsequently approved completing the correction as one reviewable patch, with independent review
before application, in issue comment `5653214732`. Preparation is authorized; production edits remain gated on
independent exact-code approval of the complete slice. The reviewed first-R6 checkpoint and behavioral RED tests
are preserved. No new mechanism or task family is authorized.
