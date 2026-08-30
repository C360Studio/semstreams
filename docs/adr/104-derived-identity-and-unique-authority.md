# ADR-104: Derived Identities Digest Their Full Origin; the Platform Authority Is Unique by Default

## Status

**Proposed (2026-08-30)** — pending independent design review and owner acceptance on #1168. Amends ADR-102
(decision 4's collision clause and decision 5's consequence "the run derives from the loop's instance") and
ADR-053 (D1 `RunID()` from the entity key; D8's derivation of `RunEntityID`) by reference. Supersedes nothing in
full. Mechanics live in `entity-id-contract`, `graph-ingest`, and `component-runtime-config`.

## Context

The run entity copied the firing loop's instance under the local authority, so two peers' loops sharing an instance
token collapsed to one local run; #1148 made that a loud refusal, not a distinct identity. The derivation was spelled
at seven framework sites and six semteams sites, and the framework told adopters to recompute it. The authority pair
`platform.org`/`platform.id` was validated for shape only, so a cloned template minted a colliding authority that
ADR-102 d7 forbids ever rewriting. ADR-102 d4's import-collision rejection was accepted and knowingly absent because
no fact recorded which lane admitted a mirror. Inventory: `docs/proposals/gh1168-federation-identity-inventory.md`.
Owner ruling 2026-08-30: greenfield — no deprecation, no alias, no parallel path.

## Decision

1. **A framework identity minted from another entity digests that entity's full canonical ID.** The instance is
   the lowercase 64-hex SHA-256 of a length-framed sequence (versioned digest domain, then the origin ID), composed
   through the `pkg/types` identity-family table — the one home. The agent-run family (`chain.agent.execution`,
   digest domain `semstreams.agent.run.v1`) is its first member; rule triggers keep their existing framed digest
   through the same helper. A derived identity is **carried, never recomputed**: the run entity ID rides the task,
   the loop record, the loop events, tool metadata, and `agent.run.entity-id`. `RunID` keeps naming the root loop's
   bare identifier and its `AGENT_LOOPS` record; nothing derives the run from it.
2. **`platform.id` is unique by default.** On a deployment's first boot the framework mints a six-hex-byte entropy
   suffix from `crypto/rand`, records it once (atomic Create) in the shared configuration bucket, and adopts it on
   every later boot and in every co-process on that NATS server. `platform.unique: true` is the operator's statement
   that the identifier is globally unique; nothing else disables the suffix. The pair has exactly one source — the
   configuration document; the `STREAMKIT_` environment surface is removed.
3. **An import is admitted under exactly one lane.** A mirror's birth carries `entity.import.lane` = the arrival
   port's name, framework-owned and immutable; a foreign ID re-arriving on a different import lane is rejected
   `import_collision`. The key is the framework-observed port declaration, not the producer-claimed `source`.
4. **Zero-caller identity surfaces are removed, not deprecated:** `message.FederationMeta` and `WithFederation`
   (never serialized, never governed), `EntityID.DeploymentPrefix()`, `agentic.{Try,}ChainExecutionEntityID`,
   `agentrun.ResolveRun` and its readers, `AgentRun.RunID()`, `config.MinimalConfig`. `graph.NewAlertEvent` is not
   removed: ADR-076 decided the family and a filed defect owns its missing producer/consumer path.

## Consequences

- BREAKING, in the beta.163 wave: every run entity ID changes shape; `agentrun.Mint` and `NewMilestoneSubscriber`
  change arity; every deployment's `platform.id` gains a suffix on its next fresh boot unless it declares `unique`;
  the authority-pair budget is 168 bytes. Fresh storage, no migration (ADR-102 d7).
- Sisters read `RunEntityID`/`agent.run.entity-id` and delete their re-derivations; their e2e fixtures observe the
  effective pair instead of predicting it from a config file.
- `agent.run.origin-entity-id` gains its first readers: it is the run→root-loop pointer.
- Renaming an import port after admission rejects that lane's re-arrivals until fresh state.

## Alternatives rejected

- Making `RunID` the digest (breaks the loop-plane `AGENT_LOOPS/<RunID>` contract).
- An exported re-derivation builder for sisters (keeps thirteen homes of a prediction).
- Suffix only when `platform.id` is absent, or refusing entropy-less values (the footgun is a present, copied id;
  entropy is undecidable by grammar).
- A KV sidecar for the admission fact (no bucket ground; the CAS closure already holds the resident state).
- Keying O-4 on envelope `source` or `Triple.Source` (producer-claimed, unauthenticated).

## Cross-repo contract

A sister conforms when: it reads the run entity from the wire or the graph and composes none itself; its
composition root passes `deps.Platform` unchanged; its configs either accept the minted suffix or declare
`platform.unique`; its e2e fixtures read the effective pair from `semstreams_config/platform_identity`.
