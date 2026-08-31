## Loop tokens become full UUIDs (ADR-105, #1192) — enforce at the mint seams; no re-key

### What changes on the wire

- New loop IDs are canonical 36-byte UUIDs; the `loop_xxxxxxxx` (dispatch) and `rg_xxxxxxxx` (research) shapes
  are retired. Run entity IDs, `run_id`, `ResolveRun`, and the gh#256 echo contract are UNCHANGED.
- A submission whose `reply_to` or `loop_id` is not a canonical UUID is refused: synchronously at dispatch
  (error response naming the field), with a classified terminated delivery at the task-stream intake.
- Pre-v1 fresh storage (ADR-102 d7): no legacy tokens exist after redeploy; nothing resolves an old-shape ID.

### The obligations (per-sister; measured read-only 2026-08-31 — no production code changes required)

| Repo | Finding | Instruction |
|---|---|---|
| semteams | Zero shape reliance in production; stale shape comments (`ui/src/lib/stores/taskRefs.svelte.ts:3`, `ui/src/lib/services/messageLoggerApi.ts:60`) and persona placeholder examples (`configs/personas/fragments/researcher-research-synthesize/00-identity.md:40`) | Update comments/examples at leisure; refresh any UI e2e fixtures using `loop_`-shaped literals |
| semsource | Zero hits | None |
| semdev | Reads `loop_id` opaquely from tool calls (`internal/tools/*`) | None |

### Downstream action

Echo, never author: a loop token is a value the framework handed you. Continue passing `reply_to`/`loop_id`
verbatim; delete any test fixture that fabricates a non-UUID loop token and submits it — it will now be refused.
