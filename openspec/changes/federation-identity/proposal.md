# Change: Make entity identity collision-free across deployment authorities

**Status: design under independent pre-owner review.** Inventory:
`docs/proposals/gh1168-federation-identity-inventory.md` (`5967394f`, sha256 `7ec8c088…`, INVENTORY PASS WITH
DIVERGENCES). Design: `docs/proposals/gh1168-federation-identity-design.md`. ADR draft: ADR-104. Owner decisions
O-1..O-8 are recorded on #1168 when ruled; nothing here is approved.

## Why

Three measured facts on `main@300e57fe` (inventory §1):

- **Case A.** The run entity's instance is the firing loop's instance copied verbatim (`agentrun.go:290`), so two
  foreign origins sharing an instance token derive one local run ID; #1148 made the second mint refuse loudly, and
  the identity is still not collision-free. The derivation `chain = f(org, platform, loopInstance)` is spelled at
  seven in-repo sites and six semteams sites, and `agentic/tools.go:396-399` tells adopters to recompute it.
- **Case B.** `platform.org`/`platform.id` are validated for shape and byte budget only; a cloned template mints a
  colliding authority, and ADR-102 d7 makes that pre-v1-or-never. A first-boot persist-and-compare mechanism already
  exists (`config.Manager.Start`, bucket `semstreams_config`); it persists a value the file holds and never mints one.
- **O-4.** ADR-102 d4's import-collision rejection is knowingly absent (`graph-ingest/spec.md:934-948`): the fact to
  compare is never retained; the port name is not carried to the gate; the envelope `source` is dropped at ingest.

## What Changes

- **BREAKING (Case A):** the `agent-run` framework family joins `pkg/types.FrameworkIdentityFamily` with a 64-hex
  instance = framed SHA-256 over the origin loop's full canonical ID; `agentrun.Mint(ctx, mgr, org, platform,
  originEntityID)` is the one minter; `RunEntityID` is carried on `TaskMessage`, `LoopEntity`, `UserMessage`, the
  four loop events and tool metadata, and read — never derived — by every consumer. Deleted: `agentic.
  {Try,}ChainExecutionEntityID`, `agentrun.ResolveRun`, `LoopTripleReader`, `NATSLoopTripleReader`,
  `AgentRun.RunID()`, the recompute instruction in `agentic/tools.go`. `NewMilestoneSubscriber(mgr, logger)`.
  The authority-pair budget becomes 168 bytes (the new family binds).
- **BREAKING (Case B):** `platform.id` gains a framework-minted entropy suffix (`-` + 6 hex) on first boot, persisted
  in `semstreams_config/platform_identity` and adopted by every later boot and co-process on that NATS server;
  `platform.unique: true` is the operator's opt-out. Deleted: the `STREAMKIT_*` environment override surface and
  `config.MinimalConfig`.
- **O-4:** an import-lane birth is stamped `entity.import.lane = <arrival port name>`; a foreign ID re-arriving on a
  different import lane is rejected `import_collision` (metered `authority_collision`). The DEFERRED paragraph in
  `graph-ingest/spec.md` is deleted and the rejection stated in its place.
- **Consolidation:** `processor/rule`'s duplicate frame writer consolidates onto `pkg/types`; the corpus audit gains
  `derived_family_composed`; `pkg/types/entity_domain_authority.go` → `entity_domain.go` (#1171).
- **Deleted zero-caller surfaces (owner greenfield ruling):** the `message.FederationMeta` family and its options and
  docs; `EntityID.DeploymentPrefix()`.
- **Filed, not deleted:** `graph.NewAlertEvent` — ADR-076's alert/trigger entities have no producer/consumer path to
  the graph (`graph.events.entity.*` has no consumer). A separate issue.
- Doc corrections: `docs/proposals/gh1095-entity-id-segment-semantics-design.md:275`, `docs/concepts/16-federation.md`.

## Non-goals

- Any rewrite, alias, or migration of stored identity (ADR-102 d7); fresh-state break only.
- Ownership, claims, registries, or cross-deployment coordination (ADR-091).
- Web-observation and lesson digest idioms (owner steer: left alone absent a measured collision).
- #1154's five-type `EntityID()` recompute (sequenced after), #1172 (hierarchy observability).
- Any new environment override of the platform pair (O-6: none).
- Editing sister repositories; impacts go in `docs/operations/migration-beta162-to-beta163.md`.

## Capabilities touched

`entity-id-contract` (MODIFIED ×2, ADDED ×2), `graph-ingest` (MODIFIED ×2), `component-runtime-config`
(MODIFIED ×1, ADDED ×1). The inventory found no `agentic-runs` or `configuration` capability; `Mint`'s contract
lives in `graph-ingest`, config load in `component-runtime-config` and `entity-id-contract`.
