# ADR-029: Instance-Type Patterns

## Status

**Accepted — Patterns A & B shipped for the instance types this ADR
covers.** Pattern A normalization landed in v1.0.0-beta.16
(2026-04-27): tools registry retired its package-level singleton and
joined `component.Registry` and the service registry as
constructor-injected boot-registries. Pattern B is shipped for rules,
flows, personas, and flow-templates (each shipped its own KV-backed
CRUD `Manager` per ADR-029 step 4). Pattern C (component lifecycle +
factory) was already in place. The generic `Manager[T]` interface
captured in "Future direction" remains deferred — the four concrete
managers serve us better today. The payload registry remains a v1
follow-up (still half-Pattern-A, half-singleton); see
`payloadregistry/global.go` for the residual singleton.

## Context

Semstreams has accumulated several "kinds of thing you can register and look
up at runtime": components, services, tools, rules, flows, and (coming) personas
and flow-templates. A audit done during the ADR-026 M2 planning work found
these handled inconsistently:

| Instance type | Current handling |
|---|---|
| Rules | `rule.ConfigManager` wrapping NATS KV, full Save/Get/Delete/List |
| Flows | Direct `flowstore.Store` against NATS KV (no manager wrapper) |
| Tools | `ExecutorRegistry` global singleton, init-time registration, no persistence |
| Components | `ComponentManager` + `component.Registry`, config-driven |
| Services | `service.Manager` + `service.Registry`, config-driven |
| Personas | Not started |
| Flow-templates | Not started |

Two kinds of inconsistency:

1. **Rules vs flows.** Both are KV-backed with roughly the same CRUD needs, but
   rules have a Manager wrapper and flows are called through the raw store.
   When the coordinator agent (ADR-026) wants tool-level CRUD over both, the
   shape of the executor layer diverges unnecessarily.

2. **Manager passed vs registry passed.** `ComponentManager` is passed through
   `component.Dependencies` as `ComponentRegistry` (the registry, not the
   manager). `service.Manager` is passed as `ServiceManager` (the manager
   itself). Two different idioms for similar concepts.

Adding personas and flow-templates without a framing commits us to whichever
ad-hoc pattern the author reaches for. That's how we got here.

The question this ADR answers: **when we add a new kind of thing, which
pattern applies?**

## Decision

Commit to three patterns, explicitly named. Every new instance type picks one
and stays inside it. We do not normalise to a single pattern — the three have
different responsibilities and collapsing them would force awkward fits.

### Pattern A: Boot-registry

**When:** The thing is known at binary startup, registered once, and looked up
by name at runtime. No persistence. No per-instance lifecycle beyond the
registration itself.

**Shape:**
- Global singleton or package-level registry.
- `Register(name, value)` from init() or a boot-time wire function.
- `Get(name)` / `List()` from callers.

**Who fits:** Tools (`ExecutorRegistry`), payload factories (`PayloadRegistry`),
command registry.

**Why not upgrade to Pattern B:** No caller writes tools at runtime. Persisting
them would be weight without value. Products that want to scope which tools an
agent sees do so via `publish_agent.tools` and `default_tools` on task messages,
not by mutating the registry.

### Pattern B: KV-backed CRUD Manager

**When:** Instances are persisted in a NATS KV bucket, mutated at runtime by
agents or operators, and discovered by listing the bucket. The coordinator
(ADR-026) composes flows by calling these; the ops agent (ADR-027) queries
them; human operators edit them.

**Shape:**
- A `{Type}Manager` type that wraps a KV bucket.
- Methods: `Save(ctx, id, item)`, `Get(ctx, id) (*item, error)`,
  `Delete(ctx, id)`, `List(ctx) map[string]item`.
- One `{Type}Executor` per manager, implementing the ToolExecutor interface and
  exposing `create_{type}`, `update_{type}`, `delete_{type}`, `list_{type}s`,
  `get_{type}` tools.
- Registration via a `processor/agentic-tools/executors/register_{type}s.go`
  file following the same shape as `register_read_loop_result.go`.
- Managers are constructed in main.go after streams/buckets are up and passed
  through `component.Dependencies` for any component that needs direct
  (non-tool) access.

**Who fits:** Rules (today, via `rule.ConfigManager`), flows (today via
`flowstore.Store` — needs a Manager wrapper), personas (new), flow-templates
(new).

**Why not fold into Pattern C:** Factory-based lifecycle doesn't apply — these
instances aren't *started*, they're read and mutated as data. A component
reads a rule to decide what to do; no one starts the rule as a process.

### Pattern C: Lifecycle + Factory Registry

**When:** Instances have a lifecycle (Start/Stop), are created from config at
boot or runtime, and are looked up as *running handles* by other parts of the
system.

**Shape:**
- A `{Type}Registry` holding factory functions keyed by name.
- A `{Type}Manager` that uses the registry to construct and own the lifecycle
  of running instances.
- Manager exposes `Initialize / Start / Stop / Get / List` over the running set.
- Passed through dependencies as a Manager handle (so dependents can call
  `GetService("name")`) rather than as a Registry.

**Who fits:** Components, services.

**Why not Pattern B:** Running instances aren't stored as inert data in KV;
they hold resources, connections, goroutines. CRUD over a factory registry
makes sense for *config* of what should run, but the runtime handle layer is
fundamentally different.

### Cross-pattern guidance

- **No forced uniformity of method names.** Patterns A and B both have
  `List`, but A's `List` returns `[]string` of names while B's returns a
  typed map of persisted objects. Naming should stay natural to each pattern.
- **`component.Dependencies` conveys the handle callers need.**
  Pattern A: not needed (global lookup). Pattern B: Manager. Pattern C: Manager.
- **Managers are always passed, not registries.** The current inconsistency
  (`ComponentRegistry` passed instead of `ComponentManager`) predates this
  ADR and can be corrected in a separate cleanup — out of scope here.

### Go interface consideration

Pattern B's CRUD shape is uniform enough to capture as a parameterised Go
interface:

```go
type Manager[T any] interface {
    Save(ctx context.Context, id string, item T) error
    Get(ctx context.Context, id string) (*T, error)
    Delete(ctx context.Context, id string) error
    List(ctx context.Context) (map[string]T, error)
}
```

We *do not* commit to this interface in this ADR. Go generics would let us
write it, but nothing in the near-term plan requires polymorphism over
Pattern B managers. The concrete `RuleManager`, `FlowManager`, `PersonaManager`,
`FlowTemplateManager` types each work independently. A shared interface is a
future cleanup if polymorphic passing ever comes up; introducing it now is
premature abstraction.

## Consequences

### Positive

- New instance types have a clear decision tree: is this registered at boot
  (A), persisted in KV with CRUD (B), or lifecycle-managed (C)? Three clicks
  to an answer.
- Tool executors for Pattern B types end up mechanical — `RuleExecutor`
  becomes the template for `FlowExecutor`, `PersonaExecutor`,
  `FlowTemplateExecutor`. Code looks the same, reduces cognitive load.
- The coordinator agent (ADR-026) and ops agent (ADR-027), both product-layer
  concerns, consume Pattern B managers through their tool executors uniformly.
  Products don't need to special-case rules vs flows.
- Reduces the risk of semteams/semspec early adopters building bespoke
  CRUD layers because the framework primitive wasn't obvious.

### Negative

- We commit to having three patterns, not one. The simplest mental model
  ("everything is a Manager+Registry") is rejected. That's slightly more to
  remember, justified by the different responsibilities.
- Flow-store and persona-store need new Manager wrappers even though the
  underlying KV access works today. Small refactor cost.

### Neutral

- Tool registration stays in Pattern A. Products that want to restrict which
  tools an agent sees do so at the dispatch/rule layer via `default_tools` and
  `publish_agent.tools` — already the shipped product-layer escape hatch from
  ADR-028.
- The shared Go interface is deferred. If three or four Pattern B managers
  end up existing in parallel and a caller wants to iterate over them
  polymorphically, we revisit.

## Alternatives considered

### "One Manager pattern for everything"

Force tools and components into a Pattern B shape. Rejected: tools don't need
persistence or per-tool lifecycle, components don't fit a data-mutation shape.
Both would require awkward adapter types to pretend they fit. The user's own
instinct during the 2026-04-18 audit session ("is it even the right pattern
for rules and flows and perhaps tools?") caught this.

### "No pattern — each type does what works"

The status quo. Rejected: that's how we ended up with flows using the Store
directly while rules use ConfigManager, and with ComponentManager passed as a
Registry while service.Manager is passed as the Manager. The first person to
add personas will make a fresh ad-hoc choice and future us will audit that
inconsistency too.

### "Write the generic Manager[T] interface now"

Tempting and small, but nothing in the near-term plan needs polymorphic
dispatch over Pattern B managers. Concrete types serve us better today;
interface surfaces when a second concrete caller wants to iterate over
them.

## Implementation sequencing

Captured in a separate plan doc — see
`docs/plans/pattern-2-normalization.md`. Summary:

1. **Rules** — already in Pattern B via `rule.ConfigManager`. Register
   `RuleExecutor` globally (one `register_rules.go` file) so rule CRUD tools
   reach the global tool registry.
2. **Flows** — wrap `flowstore.Store` in a `flow.Manager`. Add `FlowExecutor`
   + `register_flows.go`. Upgrades flows into the pattern.
3. **Personas** — new type. `persona.Manager` over a PERSONAS KV bucket,
   `PersonaExecutor`, `register_personas.go`. Required by BMAD/soul.md-style
   early adopters.
4. **Flow-templates** — new type. `flowtemplate.Manager` over a
   FLOW_TEMPLATES KV bucket, `FlowTemplateExecutor`, `register_flow_templates.go`.
   Required by the coordinator agent for flow composition (ADR-026 M2).

Each step independently reviewable. No fan-out before rules ships. Product
scoping via `publish_agent.tools` applies as always — framework ships the
primitives, products decide which agent gets which CRUD tool.

## Related decisions

- [ADR-025](025-semteams-consolidation.md) — upstreamed `RuleExecutor` from
  semteams; this ADR finally wires it.
- [ADR-026](026-coordinator-agent-dynamic-flow-composition.md) — M2 executors
  (`create_rule`, `manage_flow`, etc.) are Pattern B tool wrappers — this
  ADR is their architectural home.
- [ADR-027](027-ops-agent-meta-harness.md) — ops agent reads Pattern B
  managers for telemetry analysis.
- [ADR-028](028-orchestration-architecture.md) — framework/product split;
  Pattern B is framework, the coordinator and ops agents that use Pattern B
  tools are product.
