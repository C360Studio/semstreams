# R8 retention decision draft

base: 68c14c8eb25c512e988f740cbf7ea14b6815976f

Status: architect recommendation after independent INVENTORY PASS; not owner approval.

## Accepted evidence

The unchanged, accompanying inventory is:

`openspec/changes/agentic-loop-restart-safety/inventory-r8-retention-premise-2026-09-17.md`

SHA256: `fb937d0ef9c3c53e3a85deed5ee3d426f7cff3b786cc930767aa56d1881b2a40`

Its complete text is the binding evidence appendix to this draft, not superseded by the summary below. Independent review passed all 58 pins with no missing evidence.

## Decision in plain language

Consumer timers tell us how an attempt runs. They do not tell us how long the process can be down, when someone will resubmit work, or whether an identity mapping still exists.

Therefore, a computed “recovery horizon plus safety margin” does not establish the general guarantee presently attributed to it. Removing that calculation does **not** make retention irrelevant: dispatch can mint another LoopID when its old task mapping is absent, and loop task handling can reconstruct work when authority is absent. Those two obligations must remain explicit.

## Options

| Option | Benefit | Cost and limitation |
|---|---|---|
| **A. Keep the computed horizon requirement.** | Leaves the accepted specification unchanged. | The inventory supplies no elapsed-recovery bound from which to derive it. A formula using AckWait, BackOff, MaxDeliver, or work timeout would add a number without proving the identity guarantee. Making it meaningful would require an additional supported-recovery contract; that expansion is outside this slice. |
| **B. Replace the calculation with boundary-specific, observed retention proof, extending existing checks. Recommended.** | Uses actual stream policies and actual dependencies; preserves useful protection without inventing a timer or adopter knob. | Policy inspection alone is insufficient for the two identity-sensitive boundaries. Their concrete ordering/retention proofs remain required before those portions of R8 close. This is a smaller honest contract, not immediate completion of R8. |
| **C. Make no change and hold the remaining R8 work.** | Avoids changing an approved requirement before the owner rules. Nil-publisher work can still finish independently. | Leaves the disputed calculation unresolved and adds no recovery evidence. |

## Recommended contract boundary

Replace only the requirement to compute a local elapsed recovery horizon and safety margin.

Preserve:

1. Inspection of the **actual** source/evidence streams resolved from production ports, rather than assumptions about `AGENT`, `USER`, or co-location.
2. Required DiscardNew and non-evicting policy protections, publication acknowledgment, registered-envelope validation, and classified refusal/retry.
3. Existing KV retention ownership, including non-expiring completed tool outcomes.
4. The distinction between confirmed absence and failed, malformed, conflicting, or uncertain reads.
5. The narrowly accepted model and governance absence permissions.
6. Existing approval absence settlement and its observed policy/age/revision checks.
7. The requirement to prove dispatch and loop-task identity safety; neither becomes an exemption.

Safe settlement means the durable input advances only when its required effects or defined negative outcome are durable. It does not promise successful continuation after every possible outage or evidence expiry.

For ordinary cold response/tool-result handling, refusing to advance without required evidence preserves safety but may leave work unable to recover. Documenting that limitation is different from pretending a timer guarantees recovery.

## Two remaining concrete proofs

| Boundary | Required proof before closing its R8 obligation |
|---|---|
| **Dispatch source → task mapping** | Show that a supported operational redelivery cannot encounter expired/evicted mapping evidence and mint a replacement LoopID. Exercise independently overridden source/evidence streams and the real publish-before-source-settlement path. Check observed retention and all supported source-republication paths; original source-before-task ordering alone is not enough. |
| **Loop task → current/terminal authority and request** | Show that a supported task replay cannot outlive the current/terminal authority required to distinguish completed work from new work, or reconstruct an earlier request after meaningful progress. Include republication by dispatch, not just the original task publication. Existing response/tool-result Retry behavior does not prove this task path. |

These are proof obligations, not authorization to add storage, a state machine, another deadline, or a generic recovery runtime. If either proof fails, report the exact production counterexample and seek the smallest correction at its existing owner.

## Operational redelivery versus later resubmission

A pending delivery of an already-stored source message is restart recovery. A caller publishing the same identifier again after its evidence has expired is a separate input event; it is not automatically covered by the pending message’s retention or retry policy.

Do not promise indefinite deduplication for such resubmission. Equally, do not silently treat every framework republication as an unrelated new task to make the proof pass. First-party republication belongs in the two proofs above.

Any proposed change to identifier-reuse semantics needs its own explicit owner decision. This draft grants none.

## Proposed owner ruling

> Replace R8’s timer-derived recovery-horizon/safety-margin requirement with boundary-specific proof against observed retention and supported replay behavior. Preserve the other safety and settlement requirements. Do not claim an elapsed recovery guarantee from consumer timers. Dispatch mapping and loop-task authority remain explicit, unfinished proof obligations; this change does not exempt them or authorize new recovery machinery.

The development skill kept this proposal within existing owners and production-path proof. No new communication, durable state, payload, public API, or orchestration primitive is proposed.

Stop for independent pre-owner review, then the owner’s ruling.
