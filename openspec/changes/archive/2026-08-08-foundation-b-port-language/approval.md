# Foundation B port-language approval

**Status:** implementation complete; archived after merged-tree verification.

## Approved identities

- execution design: `docs/proposals/foundation-b-port-language-design.md`, 112 lines, 8,895 bytes, SHA-256
  `9ef118a5e2837cb0adfdcca3c9962fa4e23dd4dac99d1562de45225d4940c48d`;
- control record: `docs/proposals/foundation-b-port-language-control.md`, 177 lines, 12,353 bytes, SHA-256
  `af63b6b85a8347b5fcd5badc684918f7b23fb8166c9f4e58c9a2b82e63969593`;
- immutable inventory identity: SHA-256
  `d957dfd00a2ca9bbf3ee3cf4aa2d0d9005008eb78198c7762403aa2c66ba9000`.
- accepted trajectory inventory: commit `8c6997a6`, 426 lines, 34,359 bytes, SHA-256
  `5a7dcf3591cc643ee93654515763ec69982f36c78c296cf02bb8234b3000dd2a`;
- accepted append-only trajectory contract: 514 lines, 30,140 bytes, SHA-256
  `4d32d7229e9c976a981d547765de94d57f23aca2a022d5d69b1345e88dcc0c93`.
- accepted request/reply response-bounds inventory: 344 lines, 22,788 bytes, SHA-256
  `26ea5b020e1f292ee646dfd45115bf753e0ac392493a6d672e5743c2336e182e`;
- accepted request/reply response-bounds design: 425 lines, 21,033 bytes, SHA-256
  `e71bd4f2e0e8ef24440c2632721bb939a2d24ad9344e6c95aea50887d93c1015`.

These hashes were computed from the actual files on 2026-08-07. They supersede any stale copied identity.
The immutable response-bounds design retains the pre-approval status prose it had when presented; owner turn 4 below
and this approval record supersede that historical label without changing the accepted artifact's bytes.

## Owner turns

1. On 2026-08-07, after presentation of the corrected Foundation B execution boundary, the owner answered `approve`.
   This accepted the strict canonical grammar and the corrected stop-risk rulings recorded in the exact design above.
2. On 2026-08-07, after the graph-gateway amendment was presented as an additional clean break, the owner explicitly
   approved removing graph-gateway input ports and replacing `queries` with the three mandatory outputs, acknowledging
   that existing downstream configurations will fail startup until migrated.
3. On 2026-08-07, the owner accepted the append-only trajectory audit contract at `139b8b1c`. The contract binds
   full-fidelity evidence through a registered Store, best-effort immutable KV observations, loud non-blocking audit
   degradation, the provider-first startup phase, the canonical query route, and GraphQL-only public reads.
4. On 2026-08-07, after independent review returned `DESIGN REVIEW PASS`, the owner answered `approved` for the
   response-bounds design identified above. The owner separately confirmed that SemSource compatibility need not be
   preserved when removing the ObjectStore request/reply API; downstream projects will migrate at the release break.

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
- Keep query answers on Core NATS request/reply. Attempt the real success response first; only an observed
  `nats.ErrMaxPayload` may be translated to the canonical `invalid/response_too_large` refusal.
- Expose the connected server's current maximum payload through a narrow `natsclient.Client.MaxPayload` observation
  for exact page fitting and diagnostics, never as adopter configuration or preflight correctness authority.
- Make graph prefix results explicitly paged end-to-end, including GraphQL `EntityPage{entities,next_cursor}`; remove
  the list-only GraphQL shape and the static 800 KiB prediction without an alias.
- Make trajectory reads strict, cursor-paged, and metadata/reference-only. They never hydrate evidence bodies; full
  evidence remains retrievable by an authorized reader through the registered Store named by the reference.
- Delete the ObjectStore request/reply API and dormant `graph/llm.NATSContentFetcher` cleanly. An ObjectStore `api`
  input or any `nats-request` input fails component construction; no inert port, deprecated code, or shim remains.
- Supersede generic bulk-response streaming with operation-owned continuation. No response stream, overflow bucket,
  generic continuation envelope, or public evidence-body endpoint is added by Foundation B.

## Release boundary

This approval authorized implementation checkpoints 1-4, the accepted trajectory cutover, and the response-bounds
clean break. The completed-tree static, race, integration, generated-artifact, contract, OpenSpec, and breaking E2E
results are recorded in `docs/proposals/foundation-b-release-evidence.md`. Independent implementation review and the
post-merge inventory completed through the #911 merged baseline before archive.

The prior agentic E2E startup failure on the redundant `trajectories` override is superseded by the completed-tree
agentic and aggregate E2E passes. The clean cutover adds the canonical required port, removes the seven redundant
complete-replacement overrides, and binds the durable append-only fact contract.

Hierarchy placement and the research create-before-append/hierarchy consequences remain deferred inputs to the
post-Foundation graph index program. The existing research-graph E2E is a Foundation B cutover gate only.
