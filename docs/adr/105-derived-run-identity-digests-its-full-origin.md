# ADR-105: A Derived Run Identity Digests Its Full Origin and Is Carried, Never Recomputed

## Status

**Proposed (2026-08-31)** — pending independent design review and owner acceptance on #1192. Amends ADR-102
(decision 5's consequence "the run derives from the loop's instance") and ADR-053 (D1 `RunID()` from the entity
key; D8's derivation of `RunEntityID`) and ADR-104 (decision 3's 170-byte effective / 163-byte declared budget
figures — now 168/161 with the agent-run family binding) by reference; completes the half ADR-104's scope note routed to #1192.
Supersedes nothing in full. Mechanics live in `entity-id-contract` and `graph-ingest`.

## Context

The run entity copied the firing loop's instance token under the local authority, so two peers' loops sharing an
instance token collapsed to one local run; #1148 (slice B) made that a loud refusal, not a distinct identity. The
derivation was spelled at eight production sites in this repository and four carrier classes in sisters, and the
framework told adopters to recompute it. Owner ruling 2026-08-30 (#1168): greenfield — no deprecation, no alias, no
parallel path; the scope cut of the same day moved this decision to #1192.

## Decision

1. **A framework identity minted from another entity digests that entity's full canonical ID.** The instance is
   the lowercase hex SHA-256 of a length-framed sequence (versioned digest domain, then the origin ID), truncated
   to the family's declared instance length, composed through the `pkg/types` identity-family table — the one home.
   The agent-run family (`chain.agent.execution`, 64-byte instance, digest domain `semstreams.agent.run.v1`) is its
   first derived member; rule triggers keep their existing framed digest through the same helper, byte-unchanged.
2. **A derived identity is carried, never recomputed.** The run entity ID rides the task, the loop record, the loop
   events, tool metadata, and `agent.run.entity-id`. `RunID` keeps naming the root loop's bare identifier and its
   `AGENT_LOOPS` record; nothing derives the run entity from it. `agent.run.origin-entity-id` is the one run→loop
   pointer and gains its first readers.
3. **The re-derivation surfaces are removed, not deprecated:** `agentic.{Try,}ChainExecutionEntityID`,
   `agentrun.ResolveRun` and its readers, `AgentRun.RunID()`. A client resuming a paused run echoes
   `run_entity_id` (gh#256); dispatch rejects `RunID` without `RunEntityID`.

## Consequences

- BREAKING, in the beta.163 wave: every run entity ID changes shape; `agentrun.Mint` and the milestone-subscriber
  constructors change arity; the authority-pair budget tightens to 168 bytes effective / 161 declared. Fresh
  storage, no migration (ADR-102 d7).
- Sisters read `RunEntityID`/`agent.run.entity-id` and delete their re-derivations (migration note, beta.163).
- A rule pack that composes a run subject from `$entity.*` fragments resolves to nothing — semteams `01b` loses its
  literal-subject workaround for #1193 and has no substitute until #1193 lands.

## Alternatives rejected

- Making `RunID` the digest (breaks the loop-plane `AGENT_LOOPS/<RunID>` contract in `agentic-terminal-events`).
- An exported re-derivation builder for sisters (keeps every home of a prediction; the owner steer's rejected shape).
- Keeping the #1148 refusal (two legitimate foreign origins can never both have a run).

## Cross-repo contract

A sister conforms when it reads the run entity from the wire or the graph and composes none itself, resolves a
run's origin loop through `agent.run.origin-entity-id`, and echoes `run_entity_id` when resuming a paused run.
