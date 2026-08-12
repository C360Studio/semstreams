# Adopter note — canonical tool effect metadata

**Audience:** semdev and the second gh#749 consumer, and any repo building tool
discovery, a tool picker, or a default approval policy over SemStreams tools.

**Effect-metadata status:** additive. The effect field and enum remain compatible.

**Discovery-address status (2026-08-11):** breaking. The logical port remains
`tool.list`, but its kind is now `nats-request` and its default request subject is
`discovery.tool.list`. Clients using the former default address and deployments
with an explicit kind `nats` override must migrate. See
[Move the tool-discovery default](migration-tool-discovery-default.md).

This note states **the rules**, not the diff. Mechanics live in
`openspec/specs/agentic-tools/`; ADR-089 records the decision and the rejected
alternatives.

## Rule 1 — the value is a worst-effect claim, not a list of behaviors

```go
type ToolEffect string

const (
    ToolEffectUnknown  ToolEffect = "unknown"
    ToolEffectReadOnly ToolEffect = "read_only"
    ToolEffectMutating ToolEffect = "mutating"
    ToolEffectExternal ToolEffect = "external_effect"
)
```

A tool declares **how bad it can get**, not everything it does. `external_effect`
dominates `mutating`, so a tool that POSTs to a third party is `external_effect`
alone — do not expect to see both, and do not model this as a set. A tool whose
severity depends on its arguments declares the worst case it admits.

`read_only` means it changes no state anywhere, inside or outside the deployment.
An outbound **read** is `read_only`: what a query discloses is a governance
question, not an effect classification.

## Rule 2 — absent means unknown, and unknown is not "probably fine"

Read the field through `Canonical()`, never by comparing the raw value:

```go
switch def.Effect.Canonical() {
case agentic.ToolEffectReadOnly:  // ...
case agentic.ToolEffectMutating:  // ...
case agentic.ToolEffectExternal:  // ...
default:                          // unknown — treat as at least as restrictive
                                  // as external_effect
}
```

`unknown` is **no claim**, not a middle rung. Empty and unrecognized values both
resolve to it. If your policy maps effect onto an approval default, `unknown`
belongs on the restrictive side — the whole point of this contract is that
missing metadata must never imply read-only.

## Rule 3 — never switch exhaustively without a default

The enum is **open for extension**. A future member must be able to land
additively, without a coordinated release across repos. A switch with no default
arm is the thing that would make the next addition breaking, so the default arm
resolving to `unknown` is load-bearing, not defensive style.

## Rule 4 — over discovery, the value is already resolved and always present

The response served by logical port `tool.list` includes one field. With the
default configuration, request it from `discovery.tool.list`:

```json
{"tools": [
  {"name": "query_entity", "description": "…", "provider": "internal",
   "available": true, "effect": "read_only"},
  {"name": "some_app_tool", "description": "…", "provider": "internal",
   "available": true, "effect": "unknown"}
]}
```

`effect` is **never omitted** — an unclassified tool serves the literal
`"unknown"`. You do not need to distinguish absent from unknown, because the
framework boundary already did. On the canonical Go struct
(`agentic.ToolDefinition`) the field *is* `omitempty`, so an undeclared tool
carries no key and an unrecognized value survives decode as-received — which is
why Rule 2 says to read it through `Canonical()` there.

The framework subscribes only to the subject resolved from the `tool.list`
`nats-request` port. It does not also answer the former default subject, add an
alias, or repair a legacy kind `nats` declaration. A custom subject remains valid
when the override retains kind `nats-request`.

## Rule 5 — adopting this changes no enforcement, in either direction

Effect metadata is **descriptive**. SemStreams' authoritative controls are
unchanged and remain name-based: the configured `approval_required` set, the
configured `allowed_tools` set, and the per-loop advertised-tool admission check
(gh#551).

Concretely, and asserted by test:

- A tool declaring `read_only` that is named in `approval_required` **still
  gates**. Declaring an effect does not buy a tool out of a gate an operator
  configured deliberately.
- A tool declaring `external_effect` that is **not** named **is not gated**.
  Declaring an effect does not add a control the operator did not configure.

If you build an approval default on top of this, it is yours, and it composes
with — never replaces — the framework gate.

## Rule 6 — if you later gate on effect, read it from the registry

When a policy layer does consume effect, take the value from the definition the
**registry serves at dispatch**, not from a copy carried in `TaskMessage.Tools`
or on a `ToolCall`. Task-carried and discovery-carried copies are display and
discovery grade; treating a task-supplied value as authoritative would let a
crafted task downgrade a tool's declared effect and weaken the control it feeds.

SemStreams will follow the same rule when it builds its own effect-derived
approval defaults (deferred, filed separately).

## What SemStreams classified

Every tool in the in-repo packages that register into the shared executor
registry is classified, enforced by a source-level check that validates the
value (not just the field's presence) so a new tool in those packages cannot
ship unclassified or misspelled. Broad strokes:

| Effect | Tools |
|---|---|
| `read_only` | `query_entity`, `query_entities`, `query_relationships`, `query_neighbors`, `query_by_type`, `read_loop_result`, `component_catalog`, `flow_monitor`, `instantiate_flow_template` (renders, persists nothing), and the `list_*` / `get_*` half of the rule, flow, persona, and flow-template tools |
| `mutating` | the `create_*` / `update_*` / `delete_*` half of those same CRUD tools, `deploy_flow`, `start_flow`, `stop_flow`, `undeploy_flow`, `scratchpad`, `write_todos`, `decide`, `emit_lesson`, `emit_diagnosis`, `research_graph`, and `web_search` **when backed by a real provider** — it writes observation triples to the graph; the no-provider stub is `read_only` |
| `external_effect` | `bash`, `http_request` |

Note `web_search`: two implementations share one tool name and legitimately
carry different effects. Read the effect off the catalog your deployment serves
rather than assuming a name maps to a fixed classification.

**Your own executors are not covered by that check** — it scans only the in-repo
packages that register into the shared registry. The fail-safe covers yours
instead: an undeclared effect resolves to `unknown`, never to `read_only`.
Classify them yourself if you want them to read as anything better than
unclassified.

## Two rules for classifying your own tools

- **A metered external read is `read_only`.** Quota consumption is a cost, not an
  effect on the world. `external_effect` "spend" means an irrevocable commercial
  action the tool initiates — an order, a transfer, a booking.
- **Mediation does not launder effect, but one hop is not external.** A tool that
  writes config is `mutating` even if the thing it configures later reaches
  outward; that outbound action is the deployed component's effect. Classify what
  the tool itself does.
