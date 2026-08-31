## Framed-digest run identity (ADR-105, #1192) — run entities are re-keyed; run IDs are carried, never derived

### What changes on the wire

- Run entity IDs become `org.platform.chain.agent.execution.<64-hex digest of the origin's full canonical ID>`.
  The instance is no longer the dispatch-root loop UUID.
- `run_entity_id` is now carried on `TaskMessage`, `LoopEntity`, `UserMessage`, the four loop events, and tool
  metadata `agent.run_entity_id`; `agent.run.origin-entity-id` on the run entity is the run→loop pointer.
- A submission carrying `run_id` without `run_entity_id` is rejected naming the field (gh#256: `run_entity_id` is
  the echo token for resuming a paused run).
- Deleted Go surface: `agentic.ChainExecutionEntityID`, `agentic.TryChainExecutionEntityID`,
  `agentrun.ResolveRun` (+ `LoopTripleReader`), `AgentRun.RunID()`; `agentrun.Mint` and the milestone-subscriber
  constructors change arity.
- The authority-pair budget tightens: 168 bytes effective, 161 declared (the agent-run family now binds); this supersedes the 170/163 figures in the ADR-104 section of this document — the sweep reconciles them.
- Pre-v1 one-time break (ADR-102 d7): fresh NATS storage; no alias, no dual format, no legacy reader.

**Doing nothing is loud everywhere the compiler or dispatch can reach — and SILENT in two places.** A rule pack
composing a run subject from `$entity.*` fragments resolves to a non-existent entity (no error is possible: the
subject is well-formed); a UI slicing the instance off a run entity ID gets a 64-hex digest where it expected a
loop UUID. Those two classes are the first two rows of the obligations table below — that silence is why this section exists.

### The obligations (per-sister; SHAs pinned at landing — measured read-only 2026-08-31)

| Carrier class | Sites (at the bounded pass) | Broken how | Instruction |
|---|---|---|---|
| Composed `add_triple` subject | semteams `configs/rules/agent-run/01b-handoff-marker-redispatch.json:34` | **silently, unfixably** — no substitution token yields the digest | rewrite; NOTE: `01b` is the workaround for the anchor-append bug and has NO substitute until semstreams #1193 lands |
| TS slicing the bare id | semteams `ui/src/lib/stores/runStatus.svelte.ts:52,171`; `ui/src/lib/utils/runHealth.ts:136,152-155`; the `ui/e2e/agentic/*.spec.ts` fixtures | **silently** — the slice returns a 64-hex digest, not a loop id | read `run_entity_id` from the wire; resolve the origin loop via `agent.run.origin-entity-id` |
| Go composing the prefix/id as a string | `semdev/internal/intake/admission/resolver.go:116,352,357` | reorder already owed; instance semantics change | read the carried value; a prefix LIST over `{org}.{platform}.chain.agent.execution` keeps working once the segment order is corrected |
| Rule-pack `entity_pattern` wildcards | semteams `configs/rules/agent-run/{02,03,04,04b,09,11,12,13}*.json` | reorder only — `*` instance is digest-safe | slice-A reorder only; no digest change |
| Vendored contracts | `semstreams-ui` `contracts/semstreams` (+ generated `api.generated.ts`); semteams `specs/openapi.v3.yaml` / `ui/src/lib/types/api.generated.ts` | schema drift (`run_entity_id` fields regenerate) | re-sync the vendored contract and regenerate |

### Downstream action

Read, never derive: delete every composition of `chain.agent.execution` identities; carry `run_entity_id`; echo it
on resume. semspec reads and never derives (`processor/execution-bridge/*`, `tools/emitchange/*`) — compatible,
re-verify after re-sync rather than assuming.
