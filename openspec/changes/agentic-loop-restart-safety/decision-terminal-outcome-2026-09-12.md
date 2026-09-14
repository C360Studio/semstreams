# R2 terminal outcome: bounded owner decision

Status: advisory; no implementation authorized.

Inventory: `inventory-terminal-write-boundary-2026-09-12.md`
SHA-256: `6ab3ea4c698f041b9e35d5882e5e2c5e34a99c23b720d2ec48724e34a51137f7`
Base: `5e0e2259aa7392f7f3255d7f01533869862d8174`

## Identity question resolved

Legitimate continuation does NOT reopen a terminal ordinary LoopID.

- `openspec/specs/agentic-loop/spec.md:698–699` requires refusing terminal continuation and forbids
  minting a replacement under that token.
- `processor/agentic-loop/state.go:292–295` implements that terminal refusal.
- `processor/agentic-dispatch/loop_admission.go:265–268` refuses terminal continuation.
- `processor/agentic-dispatch/http_activity.go:319` excludes terminal records from auto-continuation.
- Cold task recovery preserves terminal state (`settlement_recovery.go:279–280`); its caller ACKs that
  already-terminal delivery without reopening it (`component.go:1278–1279`).
- The active loop delta at :208–209 requires a new random UUID for new execution and permits echoing only
  an admitted existing LoopID for continuation.

Therefore legitimate sequential chat is not evidence that terminal COMPLETE_ must be replaceable under the
same LoopID. This is an admitted identity-lifetime conclusion, not proof covering expiry or arbitrary token reuse.

## Research question resolved within the measured path

The research writer normally targets a distinct child research loop, not its ordinary parent's completion key.

- `frameworkcapabilities/graphresearch/executor.go:231` mints a new UUID.
- At :248 that UUID becomes the taskless research LoopEntity; :250 stores the calling ordinary LoopID
  as ParentLoopID. At :291 the research trigger uses the new child ID.
- Synthesis obtains its pipeline LoopID from its subject (`research-graph-synthesize/component.go:390`)
  and passes that ID to PutLoopCompletion (:512).
- `research-graph-synthesize/adapters.go:170` writes COMPLETE_ plus that child ID.

The shared key grammar is not a separate namespace or blanket collision guarantee. Nevertheless, the measured
ordinary-parent/research-child flow is not two writers competing for the parent's completion key. The held
research lifecycle/readback defect remains #1288; no research migration is established as an R2 prerequisite.

## Existing COMPLETE_ role

Classification remains (b): an existing record whose contract could be strengthened.

Success, failure and cancellation currently use unconditional Put (`component.go:2180`, :2206, :2230).
Readers consume its content/outcome, but it is not implemented as first-writer-wins outcome selection.
The skill's “write-once” description (`entity-or-bucket/SKILL.md:58–59`) conflicts with those writers.

Making Create/Get/reuse select one immutable terminal outcome would strengthen its authority contract.
It would not make record existence proof of publication, final-marker completion, or source settlement.

## Options and recommendation

1. Hold the new absence-terminal branch. Lowest change risk; R2 remains blocked.
2. Fresh revision recheck plus final CAS. Small change, but only partial: conflicting COMPLETE_/publication
   can precede the failed final CAS. Do not present this as the complete correction.
3. Strengthen the existing ordinary COMPLETE_ selection/reuse contract. Preferred direction for owner review:
   terminal identity is final, the result already exists, and no new store or coordinator is needed.
   All ordinary success/failure/cancellation writers must participate; changing only the absence branch fails.
4. Explicit bounded local exclusion. An alternative ownership change, not uniquely necessary and not selected.

Recommendation: authorize option 3's semantic direction before considering additional synchronization.
This is ready for an owner contract choice, not an implementation-ready patch or a claim that Create alone fixes R2.

Minimum affected ordinary owners are persistHandlerResult, publishFailureEvents and handleCancelSignal,
through the existing three completion persistence helpers. Cancellation's current bare-state-before-COMPLETE_
ordering must participate. Required effects still precede the final bare-state marker and source ACK.
All effects must use the selected compatible outcome; losing attempts cannot publish their competing candidate.

## Limits that survive the choice

The current cancellation RED seeds CancelLoop + bare persistence + release but omits cancellation COMPLETE_.
An absent completion key therefore cannot alone authorize a new failure; that fixture must remain protected.
Native cancellation settlement, interrupted publication/replay, and compatible selected-result reuse are not yet
proved. Precise replay behavior for all required terminal effects remains an implementation-design obligation.

Preserve registered payload validation and coherent current-state refusal; do not turn malformed cancelled
authority with pending approval into successful inapplicable ACK. R3 and its Store ruling remain separate.
Ordinary duplicates remain allowed; contradictory outcomes do not become “at least once.”
No public API, fields, bucket, supervisor or state-machine runtime is proposed.

Stop for independent review and owner choice.
