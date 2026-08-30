# Change: Make entity identity collision-free across deployment authorities

**Status: design phase.** This proposal is the claim commit for #1168. The surface inventory and the adopter seam
inventory are pending independent review; the target state below is the owner's ruled *shape*, not a design. Three
owner decisions are open on the issue and nothing here implements past them.

## Why

Two measured cases on `main` at `300e57fe` (issue #1168):

- **Case A — a framework builder derives identity from a fragment of a foreign origin.**
  `agentic/agentrun.Mint` builds `TryChainExecutionEntityID(org, platform, rootLoopID)` — local authority, foreign
  token. Two imported loops from distinct foreign authorities sharing an instance token collapse to one local
  `org.platform.chain.agent.execution.<instance>`. PR #1148 made `Mint` fail closed on an origin mismatch (the safety
  half); the identity is still not collision-free. `graph/events.go` (`alertInstance`) and
  `processor/rule/graph_event_identity.go` already derive from the **full** origin via a length-framed digest —
  `Mint` is the outlier.
- **Case B — the authority pair is uncoordinated.** `config` checks `platform.org`/`platform.id` for shape only. The
  realistic failure is a cloned template: N deployments provisioned from one config share `platform.id` and mint
  colliding identities, and ADR-102 d7 says identity is never rewritten — so this is pre-v1-or-never.
- **O-4 — the import-collision rejection accepted in ADR-102 d4 is knowingly absent.** `openspec/specs/graph-ingest/spec.md`
  carries a change-scoped DEFERRED paragraph pointing at this issue; the fact the rejection must compare (who first
  admitted an ID) is never retained beside the entity.

## What Changes — the owner-ruled shape (2026-08-29)

- Case A: every framework family minted *from* another entity derives its instance via a length-framed digest over
  that entity's **full canonical ID**, consolidating onto the existing `alertInstance` primitive. Consequence to rule:
  `RunID` stops meaning "the dispatch-root loop UUID"; `agent.run.origin-entity-id` becomes its read path.
- Case B: `platform.id` gets a framework-minted **entropy suffix by default**, persisted at first boot
  (`com-acme.dep-7f3a9c.…`). A pure-readable override is the operator knowingly owning uniqueness. Legal under
  today's grammar; arity stays six. Rejected with reasons on the issue: a seventh segment, UUID at position 6, a
  discriminator inside the instance token, an origin digest in `system`.
- O-4: implement the import-collision rejection; **delete the DEFERRED paragraph** from the graph-ingest spec and
  state the rejection in its place.
- Correct `docs/proposals/gh1095-entity-id-segment-semantics-design.md:268` ("a foreign `org.platform` never
  collides with local"): true only given distinct pairs, silent about identities derived from an import.

## Open owner decisions — recorded on #1168, unruled

1. Does `RunID` stop meaning "the dispatch-root loop UUID"? Case A requires yes, with `agent.run.origin-entity-id`
   (written by `Mint`, read by nobody today) becoming its read path.
2. Does config load **refuse** a `platform.id` carrying no entropy, or only **default** one when the field is absent?
   The first protects a hand-written clone; the second only a fresh boot.
3. O-4's comparison key: the arrival **port name** (framework-observed) or the envelope `source` string
   (producer-chosen, unauthenticated)?

## Seam-adjacent issues this design must absorb or explicitly exclude

#1154 (five agentic entity types carry org/platform on the wire and recompute `EntityID()` from mutable envelope
fields), #1172 (hierarchy skips a foreign-authority entity unobservably), #1174 (`Mint`'s origin-mismatch error
embeds a peer's entity ID), #1171 (rename `entity_domain_authority.go`). Unmilestoned by rule; each gets a
disposition in the design, not silence.

## Non-goals

- Any rewrite, alias, or migration of stored identity (ADR-102 d7).
- Ownership, claims, registries, or any cross-deployment coordination (ADR-091 posture; a collision-free name is the
  anti-ownership move).
- Editing sister repositories (communicate only; impacts go in `docs/operations/migration-*.md`).

## Capabilities touched (to be confirmed by the inventory)

`entity-id-contract`, `graph-ingest`, `agentic-runs` (or wherever `Mint` is specified), `configuration`.
