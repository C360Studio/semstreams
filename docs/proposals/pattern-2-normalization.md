# Plan: Pattern B normalization

Implementation plan for [ADR-029](../adr/029-instance-type-patterns.md) —
specifically the Pattern B normalization across rules, flows, personas, and
flow-templates.

## Scope

Four instance types get Pattern B shape (KV-backed CRUD Manager + Executor +
global tool registration). Two exist in partial form today; two are new.

| Instance type | Current state | Target state |
|---|---|---|
| Rules | `rule.ConfigManager` exists; `RuleExecutor` type exists; not registered as tools | Registered globally; tools available to any flow via `publish_agent.tools` scoping |
| Flows | `flowstore.Store` with direct CRUD; no Manager wrapper; no Executor | `flow.Manager` wrapping Store; `FlowExecutor`; registered |
| Personas | Nothing | `persona.Manager` + KV bucket + `PersonaExecutor` + registration |
| Flow-templates | Nothing | `flowtemplate.Manager` + KV bucket + `FlowTemplateExecutor` + registration |

## Shape every step must hit

Each step follows the same skeleton so Pattern B reads identically across
types:

```
// manager layer (pure Go, no agent concepts)
type {Type}Manager struct { ... }
func New{Type}Manager(bucket) *{Type}Manager
func (m *{Type}Manager) Save(ctx, id, item) error
func (m *{Type}Manager) Get(ctx, id) (*T, error)
func (m *{Type}Manager) Delete(ctx, id) error
func (m *{Type}Manager) List(ctx) (map[string]T, error)

// tool wrapper (agent-facing, in executors/)
type {Type}Executor struct { manager {Type}Manager }
// Implements agentic-tools.ToolExecutor with create_/update_/delete_/list_/get_ tools

// registration (in executors/)
// register_{type}s.go — called from executors.RegisterAll
```

Kebab-case tool names: `create_rule`, `list_flows`, `get_persona`, etc.

## Step 1 — Rules (register existing primitives)

**Scope:** Wire `RuleExecutor` into `executors.RegisterAll`. No new Manager
needed — `rule.ConfigManager` already satisfies the `RuleManager` interface.

**Files:**
- New `processor/agentic-tools/executors/register_rules.go` — takes a
  `RuleManager` parameter, calls `registerGlobal("create_rule"/"update_rule"/
  "delete_rule"/"list_rules"/"get_rule", ruleExecutor)`.
- Modified `processor/agentic-tools/executors/register.go` `RegisterAll`
  signature — accepts a `RuleManager` argument (or a more general registry
  struct if we want to future-proof — see "Dependency injection shape" below).
- Modified `cmd/semstreams/main.go` and `cmd/e2e-semstreams/main.go` — call
  sequence change. Move `executors.RegisterAll` to *after*
  `configureAndCreateServices` so the rule processor has created its
  `ConfigManager` by then. Expose `rule.Processor.ConfigManager()` so main
  can pull the manager out.

**Construction-order gotcha:** Previously flagged. Today `executors.RegisterAll`
runs at line 114 of main, before services start. Moving it post-start is
safe because no agent loop dispatches until the first user message or rule
fire. The window between component start and tool registration is tens of
milliseconds in practice. A stricter alternative is exposing rule processor's
`ConfigManager` early and wiring the executor pre-start — deferred to avoid
refactoring the rule processor startup path for a small gain.

**Tests:** Unit tests for the `RuleExecutor` already exist (moved back to
executors/rules_test.go in commit 38e321b). Add a small integration test
exercising `RegisterAll` with a live KV-backed `ConfigManager`.

**Verification:**
- `go test -race ./processor/agentic-tools/executors/ ./processor/rule/`
- `task e2e:core` — no regression
- `task e2e:deep-research` — no regression (it doesn't use rule CRUD but the
  registration path runs)
- Agent integration smoke: spawn an agent with `tools: ["list_rules"]` and
  confirm it can enumerate loaded rules.

## Step 2 — Flows

**Scope:** Introduce a `flow.Manager` wrapping the existing `flowstore.Store`,
and wire a `FlowExecutor` that exposes flow CRUD as tools. Store stays; the
Manager is a thin type-aware wrapper so the shape matches rules.

**Files:**
- New `flowstore/manager.go` — `Manager` type wrapping `*Store` with the
  pattern-B method set. Keeps `Store` as the KV-level API; `Manager` adds a
  typed surface.
- New `processor/agentic-tools/executors/flows.go` — `FlowExecutor` struct,
  `ListTools() []ToolDefinition` with `create_flow`/`update_flow`/
  `delete_flow`/`list_flows`/`get_flow`, `Execute` dispatcher.
- New `processor/agentic-tools/executors/register_flows.go` — takes a
  `FlowManager`, calls `registerGlobal`.
- Modified `RegisterAll` — accepts `FlowManager` too.
- Modified main.go — construct `FlowManager` from the existing flow store
  (the store is created in main for config-driven flow loading).

**Tests:**
- Unit tests for `flow.Manager` (CRUD paths, error cases).
- Unit tests for `FlowExecutor` (mirror `rules_test.go`).
- Integration: round-trip a flow definition through the KV bucket via the tool.

## Step 3 — Personas

**Scope:** New instance type. Persona = a role fragment (system prompt chunk
tagged with one or more roles). Today lives in `processor/agentic-loop/prompt/
assembler.go` as a `DefaultFragments` slice. The persona registry is a
KV-persisted surface so products (semteams, BMAD-style flows) can register
custom personas at runtime.

**Files:**
- New top-level package `persona/` — houses `Fragment` type (move the type
  definition from `agentic-loop/prompt/registry.go`), `Manager`, CRUD, KV
  constants (bucket name `PERSONAS`).
- New `processor/agentic-tools/executors/personas.go` — `PersonaExecutor`
  with `create_persona`/`update_persona`/`delete_persona`/`list_personas`/
  `get_persona`.
- New `processor/agentic-tools/executors/register_personas.go`.
- Modified `processor/agentic-loop/prompt/assembler.go` — when a
  `PersonaManager` is injected via `AssemblyContext`, the assembler consults
  it first (KV-backed personas override `DefaultFragments`). Fall-through to
  defaults when the manager is nil or empty.
- Modified `cmd/*/main.go` — construct `PersonaManager`, pass through deps.

**Open question:** Do `DefaultFragments` stay in code as seed data, or get
upserted into the KV bucket at first boot? Start with "stay in code,
KV-backed personas override" — no silent rewrites of code-defined fragments.

**Tests:** Unit + integration, mirroring rules/flows. Plus an assembler test
that verifies a KV-registered fragment takes precedence over a default one
with the same role.

## Step 4 — Flow-templates

**Scope:** New instance type. A flow-template is a parameterisable flow
definition the coordinator agent can instantiate at runtime (ADR-026 M2). For
Phase 1 of this plan, the template is just a flow-config JSON with named
substitution points; real templating engines can come later.

**Files:**
- New `flowtemplate/` package — `Template` type, `Manager` over KV bucket
  `FLOW_TEMPLATES`, Instantiate method.
- New `processor/agentic-tools/executors/flow_templates.go` —
  `FlowTemplateExecutor` with `create_flow_template`/`update_flow_template`/
  `delete_flow_template`/`list_flow_templates`/`get_flow_template`.
  Instantiate exposed as a separate tool, `instantiate_flow_template`.
- New `processor/agentic-tools/executors/register_flow_templates.go`.
- Modified `RegisterAll` and main.go.

**Cross-cuts with ADR-026:** The coordinator agent's `manage_flow` tool
(milestone 2) uses both `flow.Manager` (step 2) and `flowtemplate.Manager`
(this step). Completing step 4 hands ADR-026 M2 its core primitives.

## Dependency injection shape

A small decision: does `executors.RegisterAll` take four separate manager
arguments as it grows?

```go
executors.RegisterAll(ctx, natsClient, platform, logger,
    ruleManager, flowManager, personaManager, flowTemplateManager)
```

That grows unwieldy. A per-step struct would be cleaner:

```go
type ToolDependencies struct {
    NATSClient       *natsclient.Client
    Platform         component.PlatformMeta
    Logger           *slog.Logger
    RuleManager      rule.RuleManager
    FlowManager      *flow.Manager
    PersonaManager   *persona.Manager
    FlowTemplates    *flowtemplate.Manager
}

executors.RegisterAll(ctx, deps)
```

Matches the user's established preference (see feedback_go_signatures memory:
"4+ args → request struct"). Adopt at step 2 when the signature first grows.

## Staging

Four commits, one per step. Each independently reviewable and ships a usable
increment:

1. `feat(executors): register RuleExecutor globally` — rules pattern complete
2. `feat(flowstore): flow.Manager + FlowExecutor` — flows in Pattern B
3. `feat(persona): persona.Manager + PersonaExecutor` — new type
4. `feat(flowtemplate): flowtemplate.Manager + FlowTemplateExecutor` — new
   type; completes Pattern B coverage

After step 4, ADR-026 M2 coordinator tools (`create_rule`, `manage_flow`,
`list_flow_templates`, `list_personas`) become thin wrappers over these
Pattern B primitives, and moving to ADR-026 M2 is a matter of publishing the
tools rather than building new ones.

## Verification per step

Each step:
- Unit tests for Manager (CRUD happy + error paths).
- Unit tests for Executor (one per tool).
- Integration test: round-trip a persisted item through the tool.
- `task lint` clean.
- `task e2e:core` + `task e2e:deep-research` + `task e2e:agentic` regression
  check.

## Out of scope

- **Go generic `Manager[T]` interface** — deferred per ADR-029 until we have a
  caller that needs polymorphism.
- **Fixing `ComponentRegistry`-passed-as-registry-vs-Manager inconsistency**
  — Pattern C cleanup, separate effort.
- **Cross-flow optimization by the ops agent** — ADR-027 Phase 3+, future.
- **Template engine semantics for flow-templates** — start with string
  substitution, upgrade when a real use case demands.
