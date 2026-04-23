# Coordinator Pattern — Multi-Role in One Process

**When you want:** One chat UI, multiple specialist agents (ops, research, whatever), all running in a single semstreams binary — with the human always talking to a single "coordinator" agent that decides which specialist to invoke.

**What this guide covers:** how the coordinator pattern works on current semstreams, which files in the repo to crib from, the tradeoffs versus other multi-agent shapes, and the pitfalls to watch for.

## The Shape

```text
  ┌──────┐    POST /message       ┌──────────────────┐
  │ User ├──────────────────────▶ │ agentic-dispatch │
  └──────┘                        │ default_role:    │
                                  │   coordinator    │
                                  └────────┬─────────┘
                                           │ publishes
                                           │ agent.task.{loopID}
                                           │ (role=coordinator)
                                           ▼
                                  ┌──────────────────┐
                                  │  agentic-loop    │◀──┐
                                  │  (role-agnostic) │   │ coordinator
                                  │  persona loaded  │   │ next iteration
                                  │  from task.Role  │   │ reads specialist
                                  └────────┬─────────┘   │ output via
                                           │             │ read_loop_result
                                           │ coordinator LLM emits
                                           │ structured tool call
                                           │ e.g. decide(delegate_ops)
                                           ▼
                                  ┌──────────────────┐
                                  │ processor/rule   │
                                  │ matches trigger  │
                                  │ fires            │
                                  │ publish_agent    │
                                  │ role: ops        │
                                  └────────┬─────────┘
                                           │ publishes new task
                                           │ agent.task.{childID}
                                           │ (role=ops)
                                           ▼
                                  ┌──────────────────┐
                                  │  agentic-loop    │
                                  │  (same instance) │
                                  │  ops persona     │
                                  │  loaded          │
                                  └────────┬─────────┘
                                           │ on complete
                                           │ → triple emitted
                                           │ → coordinator reads it ──┘
                                           ▼
                                         user reply
```

**The key move:** only one `agentic-dispatch` component is needed — its `default_role` pins user messages to `coordinator`. Specialist roles are never the entry point for humans; they're invoked by the coordinator through rules.

## Why One `agentic-loop` Serves All Roles

`agentic-loop` is role-agnostic at construction. It reads `task.Role` per iteration and loads the matching persona on the fly:

- **Role is task-time, not construction-time.** `processor/agentic-loop/handlers.go:498` — `CreateLoop(task.TaskID, task.Role, task.Model, ...)`. The role on the task drives loop creation, not a config field on the component.
- **Persona resolves dynamically.** `processor/agentic-loop/prompt/registry.go:64-100` — `GetForContext(ctx)` filters fragments by `ctx.Role`. A fragment whitelisted with `Roles: ["ops"]` only assembles when the iteration's role is `ops`. Fragments without a role filter are shared across roles.
- **Tools scope per-task.** Dispatch's `DefaultTools` scopes the user-initiated coordinator task. `publish_agent.tools` in the rule action (`processor/rule/actions.go:556-576`) scopes specialist tasks independently.

One agentic-loop instance therefore handles coordinator, ops, research, or any other role you publish a task for. No multi-loop wiring needed.

## Wiring the Pattern

### 1. Dispatch pinned to coordinator

```json
{
  "name": "agentic-dispatch",
  "config": {
    "default_role": "coordinator",
    "stream_name": "USER",
    "permissions": { ... }
  }
}
```

Model after `configs/flows/deep-research.json` for the surrounding structure. One dispatch is sufficient — it stamps every user message with `role: coordinator`.

### 2. Coordinator persona

Register a persona fragment with `Roles: ["coordinator"]` that teaches the LLM when to delegate. The persona file goes in `configs/personas/fragments/` and is loaded at boot by `cmd/semstreams/main.go:141` via `persona.LoadFromDirectory`. Keep the prompt brief — "you receive user requests; decide whether to delegate ops-type work or research-type work; use the `decide` tool with a structured action; use `read_loop_result` to read specialist output before replying."

### 3. Delegation rules

For each delegation target, a rule listens for the coordinator's structured output and fires `publish_agent`. Template available in `configs/rules/deep-research/07-spawn-coordinator.json` — invert it so the coordinator is the SOURCE, not the target:

```json
{
  "name": "coordinator-delegate-ops",
  "trigger": { "event": "triple.added", "predicate": "delegate_ops" },
  "action": {
    "type": "publish_agent",
    "role": "ops",
    "tools": ["read_loop_result", "query_entity", "emit_diagnosis"],
    "prompt_from": "subject"
  }
}
```

`publish_agent` stamps `task.Role = "ops"` and `task.Tools` on a new TaskMessage published to `agent.task.*`. The same agentic-loop consumes it, loads the ops persona, runs.

### 4. Return path (specialist → coordinator)

When a specialist loop completes, a companion rule watches `agent.complete.*`, emits a triple the coordinator can read on its next iteration, and that's the signal for the coordinator to continue. Read-back uses `read_loop_result(loop_id, max_bytes, offset)` — already in the tool registry.

## Running Two Dispatches (Escape Hatch)

You generally don't need this in the coordinator pattern — one dispatch is the point. But if a product genuinely wants two dispatches in one process (e.g., to split trusted admin traffic from public user traffic onto different subject namespaces), it now works via `consumer_name_suffix`:

```json
[
  { "name": "public-dispatch",
    "config": { "default_role": "coordinator", "consumer_name_suffix": "public" } },
  { "name": "admin-dispatch",
    "config": { "default_role": "admin-coordinator", "consumer_name_suffix": "admin" } }
]
```

`consumer_name_suffix` appends to every JetStream consumer name that dispatch creates, keeping the two instances from colliding. Combine with port-level subject overrides (e.g. the `user.message` port subject on one dispatch set to `user.message.admin.>`) if you also need subject isolation — otherwise both dispatches will receive every user message and each will stamp its own default role, which is usually not what you want.

## Compared to Other Shapes

**Single-role single-process.** `configs/flows/ops-agent.json` is the reference. One dispatch, one role, one purpose. Fine for focused deployments where humans chat directly with one specialist.

**Rule-driven fan-out without coordinator.** `configs/flows/deep-research.json` is the reference. A rule spawns a researcher loop; another rule spawns a coordinator on specialist completion. No human-facing coordinator; the pattern runs headless as a pipeline. Good for batch workflows, wrong for chat UIs.

**Coordinator pattern (this guide).** Human stays in conversation with one agent that orchestrates internally. The coordinator can reason across specialist outputs, ask follow-up questions, and present a unified response.

## Pitfalls

**Tool scoping precedence.** Coordinator tools are set by dispatch `DefaultTools`. Specialist tools are set by `publish_agent.tools` in the delegating rule. These are independent — a coordinator that has `read_loop_result` does not automatically grant `read_loop_result` to the ops specialist. List each role's tools explicitly in the rule that spawns it.

**Loop-ID propagation.** For the coordinator to read a specialist's output, the specialist's `loop_id` must reach the coordinator. `publish_agent` carries parent-loop metadata, and the completion-rule on the specialist side should emit a triple that includes both IDs (`coordinator_loop_id` as subject, `specialist_loop_id` as object). Without this link, the coordinator can't find what to read back.

**Response routing to the user channel.** Only the coordinator's loop has `ChannelType`/`ChannelID` in `LoopInfo`. Specialist loops don't — their completions flow back through the rule layer to the coordinator, not directly to the user. Don't try to have a specialist respond to the user channel; that breaks the pattern's contract.

**Two dispatches with overlapping subjects.** If you set `consumer_name_suffix` but both dispatches still filter on `user.message.>`, every user message lands in both. Each stamps its own default role. That spawns two tasks for every message, one per role, which is almost never what you want. If you truly need two dispatches, ALSO override the `user.message` port subject per-dispatch so each only sees its intended traffic.

## Observability

`LoopInfo` (the struct returned by `GET /loops`) now carries `role` on every entry. UIs can filter "show me research loops" versus "show me ops-analyst loops" without a graph lookup, and test harnesses can identify loop types by role via the natural endpoint. The field is omitted from JSON when unset, so older clients see no change.

## Reference Files

- `processor/agentic-loop/handlers.go:498` — role on task drives loop creation
- `processor/agentic-loop/prompt/registry.go:64-100` — per-role persona assembly
- `processor/rule/actions.go:556-576` — `publish_agent` stamps role + tools
- `processor/agentic-dispatch/config.go:13` — `default_role` required field
- `processor/agentic-dispatch/config.go:17` — `consumer_name_suffix` for multi-instance
- `configs/flows/deep-research.json` — structural template for the flow config
- `configs/rules/deep-research/07-spawn-coordinator.json` — template for `publish_agent` delegation
- `cmd/semstreams/main.go:141` — where persona fragments are loaded at boot

## When Not to Use This Pattern

- **Single-role deployments.** Direct `default_role: "ops"` is simpler and fits `configs/flows/ops-agent.json`.
- **No-chat batch pipelines.** Rule-driven fan-out without a coordinator is lighter and doesn't need persona resolution per iteration.
- **Multi-human deployments.** The coordinator pattern is single-conversation-per-user. Multi-tenant chat needs additional loop-tracking not covered here.
