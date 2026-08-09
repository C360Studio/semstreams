# Design — Post-Foundation-B declaration generation

**Status:** OWNER-ACCEPTED / DESIGN PASS — implementation active. Slices A and B are complete and independently
approved. Slice C implementation is complete through C.8 and awaits C.9 independent review; Slices D and E remain
unchecked.
**Repository baseline:** `ee3b43ce67f3ee6b39547317529da7ce1a783233`.

The accepted inventory and owner-accepted design below are incorporated by reference as the sole authority for this
change. Their hash-pinned source artifacts and independent review companions are not reproduced, modified, summarized,
or reinterpreted here.

## Part I — Accepted inventory (incorporated by reference)

- **Authority:** `docs/proposals/post-foundation-b-remap-inventory.md:1-595`.
- **Identity:** 595 lines, 36,227 bytes, SHA-256
  `58e44190937c247a30ae5ce55621da27cddd113da6da858d64a2e9bc51bdd7fb`.
- **Independent review:** `docs/proposals/post-foundation-b-remap-inventory-review.md:1-16`; verdict `INVENTORY PASS`.

## Part II — Owner-accepted design (incorporated by reference)

- **Authority:** `docs/proposals/post-foundation-b-declaration-generation-design.md:1-1035`.
- **Identity:** 1,035 lines, 73,672 bytes, SHA-256
  `be8e4c2c6fbcfbcd966038448011cf98112e62e52147b088a0a794808ec9b814`.
- **Owner acceptance and independent review:**
  `docs/proposals/post-foundation-b-declaration-generation-design-review.md:1-7,167-171`; verdict `DESIGN PASS`.

The reviewed identity above remains the historical design baseline. On 2026-08-09 the owner approved a bounded Slice C
census amendment: retire exactly four configurations whose enabled factories have no production registration, add no
alias/synthetic factory/substitute, repair three production-constructibility prerequisites at their owners, and freeze
the mechanically recomputed 21-config census. Independent review of this amendment remains task C.9.

## Part III — Ordered implementation slices

The exact implementation order, current task truth, and independent review checkpoints live in [tasks.md](./tasks.md).
Slices A and B are complete and independently approved. Slice C is implemented through C.8 and awaits C.9; Slices D
and E remain unchecked. The bounded owner-approved Slice C census correction is recorded above and in the accepted
design artifact.

Decision-skill outcomes already bound by the accepted design:

- `kv-or-stream`: declaration observation is current, local, fan-out capable, cheap, and idempotent, but needs no
  cross-process durability; use one bounded coalescing in-process observer, not KV or JetStream.
- `orchestration-check`: the pre-start service seal is process lifecycle composition, not a rule, workflow, lifecycle
  entity, scheduler, or orchestration-state machine.

## Part IV — Owner-ruling conformance

The OpenSpec change translates the original twelve owner-accepted rulings plus the bounded 2026-08-09 Slice C census
amendment. The evidence sets below bind each ruling to proposal, delta-spec, and implementation-task text. There are no
deviations from either authority.

| Ruling | Accepted disposition | Evidence set | Deviation |
|---:|---|:---:|:---:|
| 1 | Registry-owned generation snapshot replaces the stale Foundation C shape | R1 | None |
| 2 | A declaration is immutable within its generation | R2 | None |
| 3 | Registry solely owns declaration-derived resource admission | R3 | None |
| 4 | Default-only JetStream output coverage is exactly 61/61/0 | R4 | None |
| 5 | Logger expansion preserves the amended 21-config census and three accepted overlaps | R5 | None |
| 6 | Identity-free component admission is removed without a shim | R6 | None |
| 7 | Provisioning intent and admitted runtime declaration remain distinct facts | R7 | None |
| 8 | Registry snapshot observation remains an internal framework API | R8 | None |
| 9 | Observation is process-local and coalescing, without durable replay | R9 | None |
| 10 | Classification is shared while each owner retains its policy response | R10 | None |
| 11 | Services become sealed, restart-only process composition | R11 | None |
| 12 | Registry snapshots remain group-neutral admission shape | R12 | None |

### Exact evidence ledger

- **R1:** proposal `proposal.md:17-19`; spec `specs/component-discovery/spec.md:3-13`; tasks `tasks.md:33-38`.
- **R2:** proposal `proposal.md:24-25`; spec `specs/component-runtime-config/spec.md:29-36`; tasks
  `tasks.md:43-47`.
- **R3:** proposal `proposal.md:20-21`; spec `specs/component-discovery/spec.md:36-42`; tasks `tasks.md:37-38`.
- **R4:** proposal `proposal.md:51-53`; spec `specs/stream-provisioning/spec.md:31-38`; tasks
  `tasks.md:77-79`.
- **R5:** proposal `proposal.md:30-37`; spec `specs/message-logger/spec.md:56-77`; tasks `tasks.md:99-127`.
- **R6:** proposal `proposal.md:22-23`; spec `specs/component-discovery/spec.md:90-96`; tasks `tasks.md:39`.
- **R7:** proposal `proposal.md:51-53`, `proposal.md:69-70`; spec `specs/stream-provisioning/spec.md:3-9`; tasks
  `tasks.md:74-76`.
- **R8:** proposal `proposal.md:26-29`, `proposal.md:87`; spec `specs/component-discovery/spec.md:51-62`,
  `specs/component-discovery/spec.md:84-88`; tasks `tasks.md:40-42`.
- **R9:** proposal `proposal.md:26-29`, `proposal.md:87`; spec `specs/component-discovery/spec.md:51-62`,
  `specs/component-discovery/spec.md:84-88`; tasks `tasks.md:40-42`, `tasks.md:80-82`.
- **R10:** proposal `proposal.md:51-53`, `proposal.md:69-70`; spec `specs/stream-provisioning/spec.md:8-9`,
  `specs/stream-provisioning/spec.md:24-29`; tasks `tasks.md:74-76`.
- **R11:** proposal `proposal.md:35-50`; spec `specs/service-composition/spec.md:8-221`; tasks `tasks.md:10-29`.
- **R12:** proposal `proposal.md:66-67`, `proposal.md:89`; spec `specs/component-discovery/spec.md:104-110`; tasks
  `tasks.md:48-49`, `tasks.md:80-84`.
