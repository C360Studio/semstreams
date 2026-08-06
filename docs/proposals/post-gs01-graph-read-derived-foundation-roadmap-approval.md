# Approval of post-GS-01 graph read and derived-state foundation roadmap

## Decision

Approval status: **approved**.

The owner approved the reviewed roadmap on 2026-08-06. R0 may land the durable program records, and R1 may begin only
after R0 is merged. This approval orders implementation; it does not amend the approved design.

## Approved authority

- approved design SHA-256:
  `533b2010a4da24f55915fbebf046530dcb7ae7c8cdaa134eb6853a5ce36385fb`
- design approval SHA-256:
  `d9be98e27fd333b242ad892f796445dd36759abc11eee5f411c17de7d580d8f8`
- roadmap lines/bytes: 576 / 22,884
- roadmap SHA-256:
  `0f16d7de739ea70c09312a897089ca01b79c28c9e43fbf0b78bf596bdc1504a2`
- roadmap review SHA-256:
  `f1b7eb7d5bf64d372e5c81d35a7534102df1b5789be7f33eb8b58258330c8f14`
- reviewed pre-acceptance baton lines/bytes: 118 / 5,196
- reviewed pre-acceptance baton SHA-256:
  `a8089d2529e56f5553f7077cf4fe3c34ba5ef3422d1223da34db5ef48795ca23`
- baseline: `d1570ef81b23096021af0d7bf3321b4c08c7e54b`

The baton is expected to change after this decision because it records current program status. The design and roadmap
identified above remain frozen.

## Owner constraints

1. Preserve SemStreams as an offline-first, edge-capable, tiered semantic graph framework.
2. Prefer pragmatic, easy-to-use, and easy-to-comprehend foundations over abstraction growth.
3. Make clean breaks when required. Do not add compatibility shims, deprecated paths, aliases, fallbacks, or dual
   readers/writers.
4. Keep downstream repositories as holdout validation. They prove feature parity but do not veto foundation repair or
   require current API shape.
5. Carry the identity packet and exact approved authority across every context or agent handoff.
6. Stop and return for owner disposition if implementation evidence contradicts the approved target; do not patch
   around the contradiction.

## Execution authorization

- R0: authorized to publish and merge the nine durable records consisting of the eight reviewed R0 records plus this
  owner-approval record.
- R1: authorized to begin the bounded acquisition/lifecycle/retry inventory after R0 merges.
- R2-R9: not pre-authorized by this record. Each slice must satisfy its roadmap entry, review gate, and current baton
  before integration.
- issue queue: remains evidence for the program, not an alternate implementation schedule.
- runtime changes: prohibited in R0.

## Meaning of approval

Approval is for the exact roadmap identified above. Later prose, issue comments, agent summaries, or implementation
convenience may not silently weaken its delete proofs, atomic cutovers, no-compatibility rule, or SemStreams identity.
