# Approval record — post-GS-01 graph read and derived-state foundation

- **Owner decision:** approved in the Codex owner task on 2026-08-06.
- **Approved target:** the complete 17-ruling target in
  `docs/proposals/post-gs01-graph-read-derived-foundation-design.md`.
- **Approved design SHA-256:** `533b2010a4da24f55915fbebf046530dcb7ae7c8cdaa134eb6853a5ce36385fb`.
- **Approved artifact size:** 1,996 lines, 132,759 bytes.
- **Baseline:** `d1570ef81b23096021af0d7bf3321b4c08c7e54b` (merged PR #898).
- **Accepted embedded inventory SHA-256:**
  `869be8fdfaef9c141dd7697071da0ff9fb5ffa1c4e3fbb5863837b25fb3be4ba`.
- **Independent design review:** `DESIGN REVIEW PASS` on the exact approved artifact; see
  `post-gs01-graph-read-derived-foundation-design-review.md`.
- **Review-record SHA-256 at approval:**
  `ef3fb8832b84e60ead0b8009177ce7032f7b57b3ae14bb1eecc8d138e175bda2`.

## Binding owner emphasis

The implementation may break SemStreams and all downstream projects while migrating to the approved target. It must
not add compatibility shims, deprecated aliases, dual readers, dual writers, fallback paths, or temporary legacy APIs.
Removed behavior is deleted; legitimate downstream capabilities migrate to the approved surface.

Many handoffs and context compactions are expected. Every roadmap slice, implementation handoff, and review must carry
the following identity packet verbatim or link to this approval record:

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

## Change control

The approved design remains byte-for-byte frozen. A roadmap may order and group its work but may not change target
semantics implicitly. Any material target change requires:

1. an explicit amendment against the frozen design;
2. a new exact artifact identity;
3. independent SemStreams design review; and
4. a new owner ruling.

Implementation may begin only from a reviewed dependency-ordered roadmap derived from this exact target. Issue order,
historical GS sequencing, agent convenience, and downstream breakage do not amend the roadmap or target.
