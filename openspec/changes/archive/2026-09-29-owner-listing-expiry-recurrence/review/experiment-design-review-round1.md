# Experiment design review: round 1

Reviewer: independent SemStreams reviewer.

**DESIGN CHANGES REQUESTED** for design SHA
`d01e6f15ef25e9a380f279cc1ae03e2d919cebab3a80478fcf5bf7d5c9da8146`.

## HIGH — design.md:18 — Measure before adding the transport-fault matrix

Mechanism: the recommendation adds a relay, forwarding owners, gate recovery and mutation proofs before measuring
construction, collection and Stop through the existing diagnostic. The decisive case can legitimately end
`GATE_NOT_REACHED` (line 94); successful injection establishes the source-known reachable synchronous deletion
delay while remaining unable to attribute CI's failure (line 114). The design does not identify what additional
decision warrants that machinery.

Smallest correction: make the first authorized experiment measurement-only: one transparent control and one retained
collection-expiry schedule, with phase timings, independent native Stop result and bounded SDK request attribution.
Defer relay construction and its two transport-fault cases.

Required decision gate: record what each result permits next. Measured slow Stop supports examining that delegation;
slow construction supports examining construction; prompt Stop with asynchronous deletion rejects that deletion as
the observed synchronous delay; two prompt runs leave recurrence causation unresolved. None authorizes a production
repair automatically. Require a separate, specific question and review before adding transport injection.

Verification/refutation: considered the matrix's benefit of separating reachable branches. Those branches are already
recorded in the accepted inventory, and the retained experiment already reached collection expiry with prompt return
and `nats: invalid subscription`. Phase/ownership measurements address missing evidence with less machinery.

## MEDIUM — design.md:143–146 — Reserve child startup and reporting time

Mechanism: fourteen seconds of work plus eight seconds of cleanup consumes the entire 22 seconds measured from parent
process start. Child initialization precedes its local work budget; reporting follows cleanup. The parent can kill a
child still honoring its allowances.

Smallest correction: specify deadline origins and reserve explicit startup/reporting slack, or shorten child allowances
within the parent envelope. Recalculate admission accordingly. Reusing the retained diagnostic's containment envelope
is preferable for the reduced two-case experiment.

Verification/refutation: the following three-second Wait reserve bounds parent joining; it does not grant the child
additional time before Kill. The retained diagnostic budgets 18 + 8 seconds against a 27-second kill trigger.

## Preserved constraints and verdict

Context fidelity, four deletion-owner distinctions, nonpartial-result oracle, trace limitations and separation of native
completion from process containment are sound constraints and must remain in the smaller experiment.

A phased measurement-only extension is sufficient for the next investigation step, not historical attribution or
closing #1421. No new inventory, production API or runtime change is needed for that revision.
