# Post-GS-01 foundation baton — R0 roadmap

## Identity

- approval: `post-gs01-graph-read-derived-foundation-design-approval.md`
- approved design SHA-256: `533b2010a4da24f55915fbebf046530dcb7ae7c8cdaa134eb6853a5ce36385fb`
- design review SHA-256: `ef3fb8832b84e60ead0b8009177ce7032f7b57b3ae14bb1eecc8d138e175bda2`
- approval SHA-256: `d9be98e27fd333b242ad892f796445dd36759abc11eee5f411c17de7d580d8f8`
- roadmap SHA-256: `0f16d7de739ea70c09312a897089ca01b79c28c9e43fbf0b78bf596bdc1504a2`
- roadmap review SHA-256: `f1b7eb7d5bf64d372e5c81d35a7534102df1b5789be7f33eb8b58258330c8f14`
- roadmap owner acceptance: `post-gs01-graph-read-derived-foundation-roadmap-approval.md`
- roadmap approval SHA-256: `071404e2cf7bf1883db757a0fd567c8aae5382f1f3f464d8f9a9469c41c3a478`
- baseline: `d1570ef81b23096021af0d7bf3321b4c08c7e54b`
- slice: R0, pre-implementation roadmap
- rulings implemented: none; R0 records order and handoff control only

## SemStreams identity packet

1. SemStreams is an offline-first, edge-capable, tiered semantic graph framework.
2. Pragmatic, easy-to-use, and easy-to-comprehend design outranks speculative guarantees and abstraction purity.
3. `ENTITY_STATES` is current authority with graph-ingest as its sole physical writer.
4. Mutations use NATS request/reply, CAS, typed outcomes, and no mutation stream.
5. Eventual consistency is accepted; honest stale/unknown/ambiguous state is preferable to fabricated certainty.
6. External graph reads use conformant gqlgen GraphQL; internal components use narrow typed operations and ports.
7. There is no semantic ownership, CQRS/event-store framework, recovery product, auto-stub policy, general embedded
   graph client, or NATS CLI dependency.
8. Derived owners share behavioral obligations only; no universal derived-view runtime exists without the approved
   three-owner/reduced-code proof.
9. Downstream repositories are holdouts, not design constraints. Clean breaks are allowed and feature parity—not API
   shape—is the migration test.
10. No shim, deprecated bridge, compatibility alias, or dual path may survive a slice.

## Current truth

- merged prerequisite: PR #898 at baseline `d1570ef8`
- approved target: frozen and independently reviewed
- implemented in this program: no runtime slice
- not implemented: R1–R9
- current wire/storage format: merged post-GS-01 baseline
- current test state: design/inventory focused evidence only; R0 changes no runtime
- current handoff: roadmap accepted; R0 publication and merge are authorized

## Surface inventory

| Surface | Present behavior | Target disposition |
|---|---|---|
| Accepted inventory | Local reviewed evidence | Land unchanged in R0 |
| Inventory review | Local `INVENTORY PASS` record | Land unchanged in R0 |
| Approved design | Frozen 17-ruling target | Never edited implicitly |
| Design review | Local exact `DESIGN REVIEW PASS` record | Land unchanged in R0 |
| Owner approval | Local content-addressed decision | Land unchanged in R0 |
| Roadmap | Durable dependency order | Accepted unchanged by owner |
| Roadmap review | Independent order/coverage review | Land final pass record in R0 |
| Roadmap approval | Owner acceptance of exact reviewed roadmap | Land in R0 |
| Baton | This R0 record | Replaced in-place with current slice truth after every merge |

## Adopter seam

| Surface | Must know | Do-nothing behavior | Discovery | Should know |
|---|---|---|---|---|
| Program migration | Holdouts pause until release candidate | Slices may break integrations | Approval and roadmap | Feature parity only |

## Atomic contract

- additions: accepted inventory, inventory review, frozen design, design review, design approval, roadmap, roadmap
  review, roadmap approval, and baton
- replacements: none
- deletions: none
- prohibited: runtime work, target amendment, issue-driven scheduling, compatibility planning

## Delete proof

- not applicable to R0
- every runtime slice must list exact retired identifiers and allowed historical occurrences

## Verification

| Check | Result | Evidence |
|---|---|---|
| Approved design hash | verified | `533b2010...` |
| Embedded inventory hash | verified | `869be8fd...` |
| Roadmap review | passed | `ROADMAP REVIEW PASS` |
| R0 publish-set review | passed | `R0 FINAL REVIEW PASS` |
| Roadmap owner acceptance | recorded | exact roadmap SHA-256 in approval record |
| Markdown whitespace | passed | `git diff --check` |
| Mutable R0 records | passed | repository Markdownlint configuration |

## Complexity ledger

- authored production lines added/removed: 0/0
- generated lines excluded: 0
- front doors before/after: unchanged
- buckets/streams/services before/after: unchanged
- adopter-visible runtime concepts before/after: unchanged

## Risks and rollback

- risk: roadmap ordering could accidentally amend target semantics
- control: independent roadmap review against exact approved design
- rollback: delete/revert documentation-only R0 before implementation
- mixed-version deployment: not applicable

## Blockers and falsifications

- target contradiction: none known
- missing legitimate capability: none known
- proof failure: none known
- owner amendment required: no

## Shared-file ownership

- R0 owns the eight reviewed post-GS-01 authority/evidence/program records plus the roadmap approval and lands all
  nine together
- runtime files remain unreserved until R1 is accepted

## Next slice

- candidate: R1 catalog acquisition, lifecycle poison localization, and rule retry truth
- prerequisite: merge this R0 record set containing the owner-accepted, independently reviewed roadmap
- first action: SemStreams architect file:line inventory against current `main`
- inherited delete list: lifecycle full-graph guard/latch and generic reader acquisitions
- evidence needed: R1 proof enumerated in roadmap

## Gates

- architect: roadmap draft delivered
- developer: not applicable
- reviewer: `ROADMAP REVIEW PASS`; `R0 FINAL REVIEW PASS`
- technical writer: `TECHNICAL WRITER PASS`
