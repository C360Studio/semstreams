# Foundation B port-language approval

**Status:** implementation authorized; release not yet approved.

## Approved identities

- execution design: `docs/proposals/foundation-b-port-language-design.md`, 112 lines, 8,895 bytes, SHA-256
  `9ef118a5e2837cb0adfdcca3c9962fa4e23dd4dac99d1562de45225d4940c48d`;
- control record: `docs/proposals/foundation-b-port-language-control.md`, 142 lines, 9,795 bytes, SHA-256
  `f6c1d0c9d2ca1bca5661d424a96dcd9f285b02abbbbd6b1db9080679e1d3c39e`;
- immutable inventory identity: SHA-256
  `d957dfd00a2ca9bbf3ee3cf4aa2d0d9005008eb78198c7762403aa2c66ba9000`.

These hashes were computed from the actual files on 2026-08-07. They supersede any stale copied identity.

## Owner turns

1. On 2026-08-07, after presentation of the corrected Foundation B execution boundary, the owner answered `approve`.
   This accepted the strict canonical grammar and the corrected stop-risk rulings recorded in the exact design above.
2. On 2026-08-07, after the graph-gateway amendment was presented as an additional clean break, the owner explicitly
   approved removing graph-gateway input ports and replacing `queries` with the three mandatory outputs, acknowledging
   that existing downstream configurations will fail startup until migrated.

The second turn is explicit risk acceptance for the graph-gateway amendment; it is not inferred from the first turn.

## Accepted rulings

- Use the twelve canonical kinds, the single strict envelope, the unexported resolver, and immutable normalized facts.
- Add no graph-query exact-read declaration; declare only the five evidenced exact/list KV consumers.
- Make exact reads must-exist and non-provisioning, including lazy AGENT_LOOPS acquisition.
- Delete the dead `KVWrite` side lane during Foundation B.
- Keep only the two named raw-config owner families visible until Foundation C.
- Preserve the gated-DAG physical provisioning specialization at its documented narrow boundary.
- Make graph-ingest the strict canonical mutation provider.
- Make graph-gateway input-free in shared-mux composition and require exactly the three approved query-family outputs,
  with no alias, auto-fill, or compatibility shim.

## Release boundary

This approval authorized implementation checkpoints 1-4. It does not mark schema, race/integration, contract, E2E,
independent implementation review, post-B inventory, PR-ready, merge, or archive gates complete. Those remain the
checkpoint-5 tasks below and require actual evidence.
