# Post-R1c foundation remap: owner approval

**Status:** accepted by the owner on 2026-08-06.

## Accepted identities

- repository baseline: `c38e3e82d5a0b1deec598ad1bf8bb21a6bf0b3fa`
- inventory: `docs/proposals/post-r1c-foundation-remap-inventory.md`
  - 447 lines
  - 25,852 bytes
  - SHA-256 `d347b99935e9d9a8f3ddf1e97b6e3595d187e51087829ea96e06aa25321de953`
  - independent verdict: `INVENTORY PASS`
- roadmap: `docs/proposals/post-r1c-foundation-remap-roadmap.md`
  - 626 lines
  - 35,379 bytes
  - SHA-256 `9183f1e85e3249f362bb63b81ed5e31fdfd624be96fd4e6c26a7ef9bd99a4075`
  - independent verdict: `DESIGN REVIEW PASS`

The inventory and roadmap review records are part of the accepted evidence trail. This approval does not alter either
reviewed artifact.

## Accepted rulings

The owner accepts all seven rulings in roadmap section 14:

1. Use the three-slice foundation-first sequence and stop for a fresh remap after Foundation C.
2. Delete `COMPONENT_STATUS` and its reporter APIs/calls while preserving generic message-logger KV access,
   ComponentManager health, `GRAPH_STATUS`, and domain lifecycle.
3. Establish the typed canonical port grammar, concrete configs including `KVReadPort`, common declaration envelope,
   strict boot validation, unexported resolver, and clean deletion of the old builder and dead NATS config types.
4. Break `Discoverable` cleanly to `Ports() PortConfig`, use complete-replacement merge rules, delete
   `PortConfig.KVWrite`, and make the registry own one immutable snapshot per instance generation.
5. Delete message-logger raw-config prediction through the replaying registry snapshot observer while keeping auto mode
   limited to declared normalized NATS/JetStream subjects.
6. Preserve the existing shared `graph/readiness` acquisition and outcome classification; response policy remains
   owner-local and no readiness implementation is authorized by this roadmap.
7. Hold #810/#842, indexes, queries, GraphQL, other message-logger behavior issues, and downstream migration until the
   mandatory remap.

## Execution authority

Foundation A is the only immediately authorized implementation slice. Foundation B begins only after Foundation A is
merged and its assumptions are checked against the resulting tree. Foundation C has the same gate after Foundation B.

Each slice remains subject to its exact boundary, stop condition, tests, breaking-change E2E tier, independent
SemStreams review, and implementation report. This approval does not authorize work outside the roadmap exclusions.
