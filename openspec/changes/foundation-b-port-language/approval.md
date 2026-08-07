# Foundation B port-language approval

**Status:** implementation authorized; release not yet approved.

## Approved identities

- execution design: `docs/proposals/foundation-b-port-language-design.md`, 112 lines, 8,895 bytes, SHA-256
  `9ef118a5e2837cb0adfdcca3c9962fa4e23dd4dac99d1562de45225d4940c48d`;
- control record: `docs/proposals/foundation-b-port-language-control.md`, 142 lines, 9,795 bytes, SHA-256
  `f6c1d0c9d2ca1bca5661d424a96dcd9f285b02abbbbd6b1db9080679e1d3c39e`;
- immutable inventory identity: SHA-256
  `d957dfd00a2ca9bbf3ee3cf4aa2d0d9005008eb78198c7762403aa2c66ba9000`.
- accepted trajectory inventory: commit `8c6997a6`, 426 lines, 34,359 bytes, SHA-256
  `5a7dcf3591cc643ee93654515763ec69982f36c78c296cf02bb8234b3000dd2a`;
- accepted append-only trajectory contract: commit `139b8b1c`, 499 lines, 28,672 bytes, SHA-256
  `53b169fbdf2cd25dfb9d3e4c87d1fb7135713ec5053d1ed1e6d93409b57b537e`.

These hashes were computed from the actual files on 2026-08-07. They supersede any stale copied identity.

## Owner turns

1. On 2026-08-07, after presentation of the corrected Foundation B execution boundary, the owner answered `approve`.
   This accepted the strict canonical grammar and the corrected stop-risk rulings recorded in the exact design above.
2. On 2026-08-07, after the graph-gateway amendment was presented as an additional clean break, the owner explicitly
   approved removing graph-gateway input ports and replacing `queries` with the three mandatory outputs, acknowledging
   that existing downstream configurations will fail startup until migrated.
3. On 2026-08-07, the owner accepted the append-only trajectory audit contract at `139b8b1c`. The contract binds
   full-fidelity evidence through a registered Store, best-effort immutable KV observations, loud non-blocking audit
   degradation, the provider-first startup phase, the canonical query route, and GraphQL-only public reads.

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
- Record one immutable `TrajectoryFactV1` per processing attempt in `AGENT_TRAJECTORIES`; retries within one invocation
  reuse identity, while redelivery appends another observed fact.
- Store full canonical evidence through the configured registered `storage.Store`, defaulting to logical instance
  `objectstore` backed by `AGENT_CONTENT` in all seven shipped agentic assemblies.
- Audit failure MUST degrade through existing logs, bounded metrics, and Health while agent work continues.
- Trajectory reads MUST report only `coverage: observed` and `observed_totals`; an ordinary `loop.terminal` fact is an
  observed outcome, never a seal or completeness proof.
- Start and register StoreProvider components before the parallel consumer barrier, and fail duplicate registration
  loudly without clobbering the incumbent.
- Keep graph-gateway's three outputs and route its existing `agentic.query.*` family to agentic-loop's declared exact
  `agentic.query.trajectory` input. GraphQL is public; typed NATS request/reply is internal.

## Release boundary

This approval authorized implementation checkpoints 1-4 and the accepted trajectory cutover. At `d630c8fd`, the
pre-trajectory implementation had green local lint/build/vet, race, integration, schema-cleanliness, contract, and
OpenSpec evidence. Those results are historical and MUST be rerun after the accepted trajectory contract is
implemented. No current release gate is checked by this OpenSpec slice. Breaking E2E, independent implementation
review, post-B inventory, PR-ready, merge, and archive remain open.

The prior agentic E2E startup failure on the redundant `trajectories` override now has an accepted disposition. The
clean cutover adds the canonical required port, removes the seven redundant complete-replacement overrides, and binds
the durable append-only fact contract. Runtime implementation and E2E proof remain pending.

Hierarchy placement and the research create-before-append/hierarchy consequences remain deferred inputs to the
post-Foundation graph index program. The existing research-graph E2E is a Foundation B cutover gate only.
