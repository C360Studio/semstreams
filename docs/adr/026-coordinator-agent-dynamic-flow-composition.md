# ADR-026: Coordinator Agent — Dynamic Flow Composition

## Status

Proposed

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

Introduce six new tool executors that give agents the ability to create, modify, and
monitor flows and rules at runtime. The coordinator agent is not a new component type — it
is an `agentic-loop` instance configured with the coordinator role persona, these six
tools in its allowlist, and a system prompt that describes the component catalog. Its stock
flow config lives at `configs/flows/coordinator.json`.

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
