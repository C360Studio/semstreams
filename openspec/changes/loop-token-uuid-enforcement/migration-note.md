## Loop tokens become full UUIDs (ADR-105, #1192) — enforce at the mint seams; no re-key

### What changes on the wire

- New loop IDs are canonical 36-byte UUIDs; the `loop_xxxxxxxx` (dispatch) and `rg_xxxxxxxx` (research) shapes
  are retired. Run entity IDs, `run_id`, `ResolveRun`, and the gh#256 echo contract are UNCHANGED.
- A submission whose `reply_to`, `loop_id`, `parent_loop_id`, `in_reply_to`, or `run_id` is not a canonical
  UUID is refused: a typed error response naming the field at dispatch (synchronous on HTTP; via the response
  subject on the channel path), a classified terminated delivery at the task-stream intake. Four seams enforce
  it — `TaskMessage.Validate`, dispatch submission, `LoopManager.CreateLoopWithID`, `agentrun.Mint`. Other
  carriers (`UserSignal`, `ApprovalResponse`, uncensused control requests) still accept a non-canonical token:
  **#1228**.

> **A loop token is NOT an authorization token.** Enforcement is canonical FORM, not provenance — a
> client-authored fresh UUID is accepted. And provenance is the wrong axis: a second party echoing another user's
> token *verbatim* honors "echo, never author" and still takes over the loop's tracker entry, completion routing,
> and in-flight context (#1227). Multi-user is a supported pre-v1 configuration, so **multi-tenant deployments
> MUST NOT rely on loop tokens for isolation until #1227 lands.**
- Deleted Go surface: `graphresearch.WithResearchGraphIDGenerator` (zero production consumers measured; the one sister hit
  is a comment).
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

Upgrade peers before importing from them: a peer deployment that has not adopted ADR-105 still mints
`loop_xxxxxxxx`-shaped tokens, and a loop imported from it is refused by `task.Validate()`
(`processor/rule/actions.go:1885`), so `publish_agent` publishes nothing for that loop — loudly (`Failed to
execute action` at ERROR plus `actionFailuresTotal{action_type="publish_agent"}`), never silently.
