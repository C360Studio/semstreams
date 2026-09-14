# Terminal selection and required-action replay

base: 5e0e2259aa7392f7f3255d7f01533869862d8174

Runtime checkpoint: `settlement_recovery.go` SHA-256
`ad135fa1606f103ed36b07927fcd5b53aa2686f8e5b1393ec7611a58404d414d`;
`approval_response_handler.go` SHA-256 `7850ba07b8cbe3ece0c70301c4d5b7627f234f7b59a05d3e82f4f29333c5020e`.

Status: bounded implementation handoff for review. Owner approval of the one added field remains pending.
The selected-outcome direction is approved; synthesis policy is unchanged.

Evidence: `inventory-terminal-write-boundary-2026-09-12.md`,
SHA-256 `6ab3ea4c698f041b9e35d5882e5e2c5e34a99c23b720d2ec48724e34a51137f7`;
approved direction `decision-terminal-outcome-2026-09-12.md`, checkpoint `e1259b208c`.

## One explicit outward addition

Add to the existing registered `agentic.LoopCompletedEvent`:

```go
SyntheticDecideRequired bool `json:"synthetic_decide_required,omitempty"`
```

Meaning: completing publication of this selected success requires the existing synthetic graph-decision action.
It records the already-computed obligation, not historical decide activity or a new synthesis policy.

In `handleCompleteResponse`, keep the current predicate unchanged. Before marshaling the completion, set the
field from its computed SyntheticDecide obligation. Preserve Decision == nil for synthesized decisions.
The saved Result supplies the action's Reason; no second reason, record model, payload type or Metadata flag.

The existing codec and registration remain; concrete payload validation and category/outcome validation remain.
Adopters need do nothing: absent/false does not request this framework-specific action. No inference shim attempts
to reconstruct an omitted historical obligation. Apply the existing greenfield/fresh-state adoption policy.
This exported field is the additional owner approval gate; no other public surface is proposed.

## Existing owners and implementation order

`agentic/events.go`: the field above.
`processor/agentic-loop/handlers.go`: copy the existing computed obligation onto the completion before marshal.
`component.go`: shared ordinary terminal selection/effects/finalization, including cancellation.
`settlement_recovery.go` / `approval_response_handler.go`: retain validation and correct source dispositions.
No research writer, approval Store, supervisor, bucket, lock or runtime is added.

1. Validate the delivered input and exact current authority using existing validators and correlation checks.
   Recheck authority before the new absence branch hydrates or authors failure.
   Already-cancelled authority with absent COMPLETE_ remains cancelled; absence never authorizes failure.
   Preserve existing refusal for malformed current state, including pending approval outside its allowed state.

2. At the existing COMPLETE_ persistence boundary, select using Create.
   On already-exists, Get and validate the existing ordinary terminal payload and its loop/task identity.
   Never replace it with the candidate. Read/write uncertainty retries; poison or identity conflict refuses.
   Preserve the existing ordinary stored payload representation and registered publication envelope.
   Use existing event types, not a second saved-record struct or a generic serializer framework.

3. The selected payload—not candidate PublishedMessages, candidate timestamps or speculative process state—
   supplies the terminal publication and outcome-dependent graph effects.
   Existing success/failure graph stamps retain their nonblocking classification.
   For selected success with SyntheticDecideRequired=true, execute the existing SyntheticDecideRequest using
   selected LoopID and selected Result. Its existing bounded graph write must succeed before terminal publication.
   False means no synthetic action. Replaying the original model/tool call is unnecessary for this action.
   Do not re-run hasDecideToolCall, infer eligibility from Decision, or consult audit-history completeness.

4. Publish the selected terminal through its existing resolved subject and registered event envelope.
   Success/cancellation retain agent.complete; failure retains agent.failed.
   Repeated compatible publication is allowed. Never publish the losing candidate's contradictory outcome.

5. After required effects and PubAck, commit compatible bare terminal LoopEntity state as the final marker.
   Preserve the lane's required input-correlation material; do not use a losing candidate's speculative snapshot
   as proof that its input applied. A changed authority revision cannot be overwritten unconditionally.
   A delivery lacking the required correlated material retries; the selected record is not generic applied proof.
   No additional provider/tool call is introduced to execute the saved synthetic action.

6. Release speculative per-loop process state on failed pre-marker attempts and after completed finalization.
   Source ACK waits for the lane's required effects/final marker and existing correlation proof.
   An existing selected result alone does not authorize ACK.

All ordinary terminal paths participate: persistHandlerResult; publishFailureEvents/handleLoopFailure;
and handleCancelSignal. Cancellation selects before its required effects and final bare-state commit,
rather than persisting cancelled state and only later writing COMPLETE_.
The three existing completion persistence helpers cannot retain an unconditional replacement bypass.

## Exact replacement for the active loop delta's terminal-marker paragraph

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
retain the selected completion, and Retry using it together with the lane's existing exact retained evidence.
Required effects MAY repeat compatibly with the selected outcome. A changed authority revision SHALL NOT be
overwritten with stale speculative state.

Already-cancelled durable authority SHALL NOT be regressed because its COMPLETE_ record is absent.
Existing malformed-state and identity-conflict refusals SHALL remain unchanged. The final terminal marker
proves application only where the lane's required correlation identifies the delivered source; neither bare
terminality nor selected-record existence is generic tool-execution or source-applied proof.

## Small TDD proof list

- Existing cancellation-overwrite regression remains protected, including its absent COMPLETE_ fixture.
- Competing ordinary candidates select one saved payload; neither output nor final state uses the loser.
- Replacement after selection reuses saved content and required-action flag without provider/tool execution.
- Selected synthetic action failure withholds terminal publication, final marker and ACK; retry completes it.
- False/default field performs no synthetic action; existing builder eligibility tests retain their outcomes.
- Poison, identity conflict, Create uncertainty, publication failure and final-marker conflict preserve disposition.
- Native cancellation settlement covers the reordered COMPLETE_/publication/final-marker boundary.
- Registered completion roundtrip preserves the optional field; core dependency closure stays green.

R3 and its Store ruling remain held. Research and synthesis-policy changes remain out of scope.
The 15-subscription duties, failed pre-marker cleanup, and #1156/#1249 atomic landing obligations survive.
