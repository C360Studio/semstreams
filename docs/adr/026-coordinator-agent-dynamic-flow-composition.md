# ADR-026: Coordinator Agent — Judgment, Orchestration, and Dynamic Flow Composition

## Status

Proposed — **refreshed 2026-04-18** with ADR-028 framing. The original "dynamic flow composition" framing stands for the tool set, but the coordinator's primary purpose is broader: it is the **judgment role** in the three-layer orchestration architecture (ADR-028). Dynamic flow composition is one of its capabilities, not its definition.

## Role within the three-layer orchestration architecture

Semstreams commits to rule skeleton + coordinator agent + ops agent (ADR-028). The coordinator is **Layer 3 — Judgment**. Its purpose:

- **Invoked by rules**, not continuously running. Rules fire at decision points where metadata isn't enough to choose the next action (e.g. "did the researcher produce fan-out-worthy subtopics, or is it done?"). Those rules spawn the coordinator as a normal agent loop with the coordinator role persona.
- **Reads agent output on demand** via `read_loop_result(loop_id)`. The upstream agent's prose is never injected into the coordinator's prompt — the coordinator fetches only what fits its context.
- **Returns a structured terminal decision** via a `decide()` tool whose schema enumerates the valid next actions (e.g. `fan_out`, `synthesize`, `retry`, `done`). Rules downstream match on `coordinator.next_action` triples and route accordingly.
- **Contains schema discipline to one role.** Researcher, coder, synthesizer, reviewer agents produce free text. Only the coordinator needs structured output — which means ADR-028's per-tool retry policy gets invoked at the coordinator's `decide` tool, not at every role's submit boundary.
- **Can manipulate flows and rules at runtime** via the six tool executors defined below. That's how the coordinator adapts the pipeline when its judgment reveals a gap.

The six dynamic-flow-composition tools originally proposed by this ADR remain correct and necessary — they're the coordinator's mechanism for shaping flows when static configuration doesn't cover the case. But the coordinator is not primarily a flow composer; it's the judgment layer that *happens to be able to* compose flows when it needs to.

## Why one judgment role, not per-agent schema

The operational learning that prompted the ADR-028 refresh: requiring every agent (researcher, coder, etc.) to submit via a schema-enforced terminal tool breaks on small models. Schema adherence fails; retries eat iteration budget; flows stall. Semspec lived this.

Concentrating the schema-adherence burden on the coordinator means:

- Researcher/coder/synthesizer/reviewer agents can run on small models, produce prose, call `read_loop_result` on each other's outputs, and not need structured returns.
- The coordinator is naturally the role you'd run on a stronger model (its decisions matter most), which makes structured output tractable.
- Per-tool retry policy (`agentic-tools.tool_retries`) gets declared on the coordinator's `decide` tool specifically. Retries + exponential backoff + validation-error kind gives the small-model-shaped failure mode a place to land.

## Context

SemStreams agents today operate within statically configured flows. When a coordinator
agent encounters a gap — a task requiring a pipeline that doesn't exist — it cannot create
one. The only recourse is human intervention: write JSON flow configs, define rule
definitions, restart components.

The superpowers proposal (`docs/proposals/agentic-superpowers.md`) mapped this gap and
identified that most of the required infrastructure already exists:

- `flowstore/` persists flow definitions in NATS KV (`semstreams_flows`) with optimistic
  concurrency.
- `FlowEngine.Deploy()` translates `Flow` entities into `ComponentConfig` records and
  writes them to the config KV bucket.
- `ComponentManager` with `WatchConfig: true` watches the config KV bucket and
  instantiates components at runtime.
- `schemas/*.v1.json` provides JSON schemas for all 32 registered component types.
- Rule processor's `ApplyConfigUpdate()` hot-reloads rule definitions without restart.
- `processor/rule/kv_config_integration.go` contains a `saveViaConfigManager()` stub
  that returns `ErrInvalidConfig` instead of writing — the method signature and
  surrounding plumbing exist; only the write is missing.
- The flow builder UI already performs AI-assisted flow generation via
  `POST /api/ai/generate-flow`.

What is missing is the tool executor bridge: agents have no tools to invoke this
infrastructure. Six executor implementations and two small wiring tasks close the gap.

This ADR depends on ADR-025 (semteams consolidation), which provides the full component
catalog, built-in executors, and the `ToolCallFilter` + `ApprovalFilter` mechanism
referenced in the safety model below.

## Decision

The coordinator is an `agentic-loop` instance configured with the coordinator role persona, its terminal `decide` tool, the six flow-composition tools, and a system prompt that describes the component catalog and the available decision actions. Its stock flow config lives at `configs/flows/coordinator.json`.

Seven tool executors in total: one terminal `decide` tool that is the coordinator's return-statement, and six flow/rule composition tools that are its reach.

### Terminal decision tool

**`decide`** is the coordinator's structured return value. Its schema enumerates a small, flow-specific set of next actions the coordinator can choose from — typically four to six options. For the deep-research flow's research coordinator:

```json
{
  "action": "fan_out" | "synthesize" | "retry" | "done",
  "reason": "short natural-language justification",
  "subtopics": ["..."],          // required when action=fan_out
  "retry_hint": "..."             // optional when action=retry
}
```

On successful decide, the tool emits a triple on the coordinator's loop entity:
`coordinator.next_action = <action>`. The rule engine matches downstream rules on that triple and routes mechanically. The reason + action-specific fields land in the loop's result text so they're inspectable via `read_loop_result` without any tool parsing.

Because `decide` is where schema discipline is concentrated, it's also the first real consumer of the opt-in `tool_retries` policy (ADR-028 Layer 1). Stock coordinator configs declare:

```json
"tool_retries": {
  "decide": {
    "max_attempts": 3,
    "backoff_initial_ms": 100,
    "retry_on_kinds": ["validation", "invalid_args", "timeout"]
  }
}
```

Per-flow coordinators can tighten or relax the retry envelope in their config.

### Flow-composition tool executors

The original six executors remain the coordinator's toolkit for shaping pipelines when static configuration doesn't cover the case. They're still required; the framing around them just shifts from "coordinator's purpose" to "coordinator's reach."

### Tool executors

**`create_rule`** accepts rule JSON, validates it against the schema in
`config_validation.go`, and writes the validated definition to the `semstreams_config` KV
bucket via the completed `saveViaConfigManager()` path. The config KV write triggers the
ConfigManager change notification, which calls `ApplyConfigUpdate()` on the rule processor.
The tool returns the validation result on failure or the created rule ID on success.

**`manage_flow`** provides CRUD operations for flows via `flowstore.Store`. Supported
operations: create, update, validate, deploy, start, stop. On deploy, the tool calls
`FlowEngine.Deploy()`, which translates the flow entity into `ComponentConfig` records.
The intent is that the coordinator's reasoning produces valid JSON flow definitions,
validated against component schemas before deployment.

**`list_components`** returns the registered component factory catalog with the JSON
schema for each type from `schemas/*.v1.json`. These are the building blocks the
coordinator uses to compose flows. Each entry includes the component type, a short
description, port definitions, and the config schema. The coordinator does not need to
guess what fields a component accepts; the schema is authoritative.

**`list_personas`** returns predefined agent role configurations stored in an
`AGENT_PERSONAS` KV bucket or config directory. Each persona defines system prompt
fragments (for the prompt assembler from ADR-025), tool allowlists, model preferences,
temperature, and maximum iterations. Stock personas shipped with the coordinator config:
`researcher`, `developer`, `reviewer`, `coordinator`, `ops`, `synthesizer`.

**`list_flow_templates`** returns skeleton flow configs for common patterns. Stock
templates are JSON files in `configs/templates/`:

| Template | Pattern |
|----------|---------|
| `deep_research` | Dispatch → N parallel researcher loops → synthesizer |
| `code_review` | Developer loop → reviewer loop |
| `incident_response` | Triage → investigation → remediation → approval gate |
| `conversational_assistant` | Intent routing → specialist agents → memory |

Templates lower the bar for the coordinator LLM: customizing a skeleton toward a specific
goal is a simpler reasoning task than composing from primitives.

**`monitor_flow`** reads flow runtime state from `flowstore`, loop state from the
`AGENT_LOOPS` KV bucket, and completion events. It returns an aggregated status: which
loops are running, which have completed, their outcomes, and token usage. This gives the
coordinator the feedback signal it needs to evaluate whether a spawned flow achieved its
goal and to decide the next action.

### Composition model: flows, not composite components

SemStreams implements composition at the flow orchestration level, not the component
level. There is no runtime "composite component" type — components are atomic processors
that transform data, and composition is the job of flows.

A `Flow` entity (`flowstore/flow.go`) contains `FlowNode` instances (component configs
with canvas positions) and `FlowConnection` edges (port-to-port wiring). `FlowEngine`
(`engine/engine.go`) translates a Flow into `ComponentConfig` records and writes them to
the `semstreams_config` KV bucket. `ComponentManager` watches that bucket and
instantiates components reactively. Deployment is atomic: all components in a flow deploy
together.

Connection discovery is emergent rather than explicit. Components declare input and output
ports with NATS subject patterns. `FlowGraph` (`component/flowgraph/flowgraph.go`)
validates connections by matching publisher subjects against subscriber patterns using
bidirectional NATS wildcard matching. Components do not reference each other by name —
they connect through subject overlap.

This architecture means the coordinator's `manage_flow` tool operates at the correct
abstraction level. The coordinator composes flows (groups of connected components), not
composite components. A "deep research flow" is a Flow entity containing dispatch, loop,
model, tools, and memory nodes with port connections between them — not a single compound
component that hides its internals.

### Infrastructure prerequisites

Two wiring tasks must be completed before `create_rule` has a working backend:

1. Implement the `saveViaConfigManager()` body in
   `processor/rule/kv_config_integration.go` — write the validated rule JSON to the
   `semstreams_config` KV bucket. The method signature, validation call, and surrounding
   structure already exist.
2. In rule processor `Start()`, subscribe to config updates from `ConfigManager` so that
   KV writes trigger `ApplyConfigUpdate()`. The method already works; it is simply not
   connected to the KV watch path.

A third infrastructure gap from the superpowers proposal — a cron-ticker input component
for time-triggered flows — is noted here for completeness but is not required for the
coordinator's core capability.

### Safety model

Agent-generated flow and rule changes pass through four layers before activation:

1. **Schema validation** — rule JSON is validated against `config_validation.go`; flow
   JSON is validated against the component schemas from `list_components`. Malformed input
   is rejected before any side effect.
2. **Governance review** — agent-generated content passes through the `agentic-governance`
   filter chain (PII detection, prompt injection detection, content moderation). This layer
   already runs on every agent message; agent-generated rules and flows are treated the
   same way.
3. **Sandbox evaluation** — proposed rules are dry-run against a set of test entities to
   verify logic before activation. This is new but small: a stateless evaluator that calls
   the existing rule expression engine against synthetic input without writing any state.
4. **Human approval** — high-risk operations (tool removal, model switches, rules granting
   unrestricted execution) require human approval via the `ToolCallFilter` +
   `ApprovalFilter` mechanism from ADR-025. Low-risk operations (monitoring rules, threshold
   adjustments) auto-approve and activate immediately.

The `approval_required` list on the `agentic-tools` config determines which tool
invocations require a human gate. The coordinator's stock config sets conservative
defaults; operators can widen or narrow them.

## Consequences

### Positive

- Agents can create new capabilities at runtime: low-risk operations without human
  intervention, high-risk operations with a lightweight approval gate.
- When an agent encounters a gap, it can write a new rule or compose a new flow rather
  than halting and waiting for human intervention.
- Flow configs are the coordinator's artifacts — inspectable, versioned in `flowstore`,
  and replayable.
- No new component types are required. The six executors register in `agentic-tools` using
  the existing `RegisterTool()` / `ToolExecutor` interface.
- Stock templates lower the reasoning burden on the coordinator LLM and reduce the token
  cost of flow composition.

### Negative

- Agent-generated flows could be resource-wasteful — spawning too many loops, selecting
  expensive models. Governance rate limiting and `LoopCostUSD` predicates in the rule
  engine mitigate this, but do not eliminate it.
- The four-layer safety model adds complexity to the tool execution path. Each layer is
  individually simple and independently testable, but operators must understand the full
  chain to reason about activation latency for high-risk operations.
- `saveViaConfigManager()` completion and the ConfigManager wiring are blocking
  prerequisites. Until both are done, `create_rule` has no backend and cannot be shipped.

### Neutral

- The `manage_flow` tool wraps the same API the flow builder UI uses (`flowstore` +
  `FlowEngine.Deploy()`). There is no new persistence path or deployment mechanism — the
  coordinator uses the same infrastructure a human operator uses through the UI.
- Flow templates are JSON files in `configs/templates/`. Adding a new template requires no
  compilation and no code review beyond the JSON itself.
- The coordinator role persona is one entry in the `AGENT_PERSONAS` KV bucket. It is
  configuration, not a privileged system component. Operators can modify or replace it
  without touching code.

## Alternatives Considered

### A. Dedicated coordinator component (`processor/coordinator/`)

A new processor managing flow lifecycle internally, with its own state machine for
tracking spawned flows and aggregating outcomes. Rejected: this violates the orchestration
boundary principle established in `docs/concepts/14-orchestration-layers.md` — components
execute work, they do not orchestrate it. The coordinator's reasoning IS the LLM's
output. A dedicated component would duplicate the loop's state machine and create a second
place where "what the coordinator decided" is recorded, diverging from the trajectory
graph.

### B. Rule-only self-programming (no flow creation)

Agents can create and modify rules but cannot compose new flows. Rejected: rules
coordinate existing components but cannot add new pipeline stages. A coordinator that can
adjust conditions and routing but not topology is limited to optimization of the current
pipeline — it cannot respond to a genuinely missing capability. The proposal's
`deep_research` and `incident_response` patterns both require composing pipeline stages
that may not exist in the currently deployed flow.

### C. MCP-based flow management as the primary path

Expose flow CRUD via MCP tools rather than built-in executors, so the coordinator reaches
the same `flowstore` API through an MCP server rather than a direct function call.
Rejected as the primary path: adds a network hop and an external process dependency for
an operation that is in-process by design. MCP exposure remains appropriate as a secondary
access pattern for external tooling (e.g., the flow builder UI's existing
`POST /api/ai/generate-flow` endpoint), but the coordinator should not depend on an
external network call to manage its own infrastructure.

## Related decisions

- [ADR-028](028-orchestration-architecture.md) — names the coordinator as Layer 3 of the three-layer architecture and explains why schema discipline is contained here.
- [ADR-027](027-ops-agent-meta-harness.md) — the ops agent is Layer 4, consumes coordinator trajectories to propose improvements, and reuses the same runtime composition tools defined here.
- [ADR-025](025-semteams-consolidation.md) — provides the framework primitives (prompt assembler, approval filter, governance filter) the coordinator depends on.
- [ADR-024](024-layered-llm-timeouts.md) — the timeout model the coordinator's `decide` tool inherits.

## Implementation sequencing

Before a first coordinator ships, in order:

1. `decide` terminal tool — simplest, most important. Defined per flow, emits the `coordinator.next_action` triple. No flow-composition capability yet; just judgment.
2. `read_loop_result` is already built (ADR-028 Layer 1).
3. Opt-in retry policy is already built (ADR-028 Layer 1) — the coordinator's stock config opts in for `decide`.
4. Stock research coordinator persona + `configs/flows/coordinator-research.json`.
5. Re-enable rule 03 (subtopic fan-out) in the deep-research flow, this time matching on `coordinator.next_action = fan_out`.
6. End-to-end coordinator exercise on the deep-research flow.
7. Six flow-composition executors — after the judgment-only coordinator is proved out.

The two infrastructure prerequisites (`saveViaConfigManager()` completion and ConfigManager wiring) gate step 7, not steps 1–6.
