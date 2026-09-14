# Orchestration Layers

SemStreams composes multi-step applications from rules and components. Rules evaluate declared conditions and
trigger actions. Components execute work. The lifecycle harness gives named entities explicit phases and transitions.

Applications choose their domain vocabulary, sequencing, completion criteria, and human participation.
These choices can describe deterministic local processing, agentic work, or a combination of both.

## Why no separate workflow engine?

The earlier reactive workflow engine under `processor/reactive/` was retired. Its old tutorials describe historical
APIs. Current orchestration uses the component framework, rule engine and lifecycle primitives, keeping inputs,
outputs, dependencies and state ownership visible in the application composition.

The [framework/product boundary](../../openspec/project.md#product-boundary) separates these primitives from
the workflows and policies an application builds with them.

## The Two Layers

| Responsibility | Owner |
| --- | --- |
| Evaluate conditions and select actions | Rule engine |
| Track rule matches and action firing caps | Rule engine |
| Execute model calls, tools, queries and processing | Components |
| Define application phases and permitted transitions | Application lifecycle declarations |
| Validate lifecycle transitions and publish their state | Lifecycle harness through graph-ingest |

### Substrate primitives (used by both layers)

Use [lifecycle](../../openspec/specs/lifecycle/spec.md) when a named entity needs explicit phase, progress or
operator interaction. Lifecycle manages declared transitions; application components perform the work.
Use [BoundedDispatcher](../../pkg/dispatch/doc.go) for bounded parallel work inside a component.
Rule actions provide guards, firing caps and per-item dispatch through the
[current action types](../../processor/rule/actions.go).
These primitives support the two layers; they do not introduce a separate workflow runtime.

## Signal kinds the rule engine watches

Entity rules evaluate decoded facts from `ENTITY_STATES`. Message rules evaluate the fields a payload exposes
through `message.RuleReadable`; generic JSON payloads retain their data-map surface. Scheduled rules provide
time-based triggers.

The entity evaluator does not decode arbitrary component KV records. Setting `entity_watch_buckets` to
`AGENT_LOOPS`, or using a `COMPLETE_*` key pattern, does not create an operational-state rule adapter.
See the [rule-engine specification](../../openspec/specs/rule-engine/spec.md) and
[entity watch boundary](../../processor/rule/entity_pattern_contract.go) for admitted fields, buckets and patterns.

## Pattern Catalog

These shapes explain where decisions and work belong. They are conceptual patterns; use current action types and
an explicitly composed application when turning a shape into configuration.

### Pattern 1 — Single trigger

**Shape:** `condition becomes true → request work`

Use a rule to detect a declared condition and invoke a component through an action. Expose an entity rule's relevant
domain or lifecycle fact on the entity. The component owns execution and its resulting artifacts.
A private completion record is not automatically readable by an entity rule.

### Pattern 2 — Linear pipeline

**Shape:** `A → B → C`

Use coordinated rules to request each stage from the state or message that permits it. Model durable,
operator-visible progress on a named lifecycle entity. Keep the stage's result distinct from that progress:
phase and relationships can be graph facts; full reports and model output belong with their producer.
Pass references to those bodies so the next component can retrieve them.

For agent phases, the application chooses roles, model configuration, tools and decision vocabulary.
The [phased-chain guide](25-phased-agentic-chains.md) illustrates that division of responsibility.

### Pattern 3 — Conditional branch

**Shape:** `A → if condition then B, otherwise C`

An action's `when` field is a list of condition expressions; all must match for that action to execute.
The rule's trigger and each action's guard answer separate questions. Branch on an explicit fact or declared
message field. A validation outcome might select further processing or an application-defined review step;
the application decides whether a person participates.

Use the [action definition](../../processor/rule/actions.go) for condition fields. Free-form expression strings
from the retired tutorials are not the current shape.

### Pattern 4 — Bounded iteration

**Shape:** `review → revise → review`, with a declared stopping condition

Use rules for transitions and components for each unit of work. Keep named phase and progress on the lifecycle entity.
An action's `max_iterations` limits repeated firing for a rule/entity match cycle. `loop_max_iterations` limits
iterations inside the agent loop spawned by `publish_agent`; the two budgets are separate.

The action firing cap defaults to three when omitted; explicit zero means unlimited. A requested loop budget cannot
widen the component's configured ceiling. A firing cap is not an application completion policy: declare what
success, failure, cap exhaustion and further review mean in the application's state and rules.

### Pattern 5 — Async fan-out / fan-in

**Shape:** `A → work for several items → continuation when the application's condition holds`

The current `publish_agent` executor supports `for_each` over a resolved list, binding the named iteration variable
for each item dispatch. Other action types do not acquire iteration behavior merely by carrying that field.

Per-item dispatch and runtime concurrency are different properties. Published task count does not establish how
many loops or tool calls execute simultaneously. Use the selected components' execution contracts and validate
the deployed composition. For work inside one component, use a bounded dispatcher. For dependency-gated units,
consult the [gated-DAG dispatch contract](../../openspec/specs/gated-dag-dispatch/spec.md).

An empty list produces no dispatches. The current executor logs an unresolved or incorrectly shaped list and falls
back to a single dispatch without the iteration binding. Inspect that warning when the task count is unexpected.

Fan-in needs an application meaning: which identified results satisfy the continuation condition, and how are
failure, cancellation and duplicates handled? Neither `for_each` nor publication declares an all-results,
majority or first-success policy.

## State Storage Boundaries

| Fact or artifact | Home | Reader |
| --- | --- | --- |
| Domain facts and lifecycle phase | `ENTITY_STATES` | Graph queries and typed entity rules |
| Rule match state and firing counters | Rule engine's owned state | Rule engine |
| Component execution records and trajectories | Component-owned storage | Supported component readers |
| Bulky content | ObjectStore or producer's content store | Stored reference and supported reader |
| Published work and messages | Declared messaging ports | Corresponding consumers |

Only graph-ingest writes authoritative `ENTITY_STATES`. Components use graph ingestion or the admitted mutation
surface to publish changes. Lifecycle maintains state through that same authority. Retained transition history is
bounded; current graph state is not an unlimited audit log.

A run's outcome and relationships can be graph facts while its full output remains in component storage.
A reference connects the facts to that content without copying the body into a rule action.
Storage placement and rule readability are separate decisions. A private bucket needs a reason and an owner;
it does not become readable by entity rules through configuration.
See [Entity or Bucket](../../.agents/skills/entity-or-bucket/SKILL.md) for placement criteria.

## Human participation and tool effects

Applications decide where people participate: review, clarification, approval, intervention or observation.
SemStreams supplies controls and observable state that an application can compose into that experience.

Tool effects describe the worst effect a tool declares; they do not enable or remove an approval gate.
Execution controls include configured `approval_required` and `allowed_tools` name sets, plus per-loop
advertised-tool admission. A `read_only` tool can require approval; `external_effect` does not automatically
require it. Absent or unrecognized effect metadata means `unknown`.

See [tool effect metadata](../operations/adopter-tool-effect-metadata.md) and the
[tool contract](../../openspec/specs/agentic-tools/spec.md) before deriving application policy.

## Rules of Thumb

- Put trigger conditions in rules and execution mechanics in components.
- Model named, operator-visible phase through lifecycle declarations.
- Give each fact one owner; distinguish semantic facts from execution artifacts.
- Carry content references through coordination paths.
- Separate action firing caps, loop budgets and worker concurrency.
- Tie component behavior to its inputs and configuration, rather than an assumed caller.
- Record a framework gap when an application needs an unsupported reusable primitive.

## The semspec trap

Earlier applications accumulated parallel orchestration state around the retired workflow engine. That history
explains the emphasis on declared composition and explicit ownership. It does not establish the current migration
status of a sister repository. Start new applications with current contracts and identify missing primitives directly.

## Debugging Orchestration Issues

### Symptom: action fires multiple times unexpectedly

Inspect the entity or message triggering each evaluation, rule match transitions, and the action firing cap.
Distinguish repeated triggering from redelivery. For effectful tools, review the executor's idempotency contract.

### Symptom: chain stalls partway through

Identify the expected next rule and the actual state or message it evaluates. Check entity patterns, predicates,
action guards and component readiness. Confirm `publish_agent` has its required subject, role, model and prompt.
A private completion record alone does not establish that the next entity rule has readable input.

### Symptom: component behaves differently in chain vs. standalone

Compare its actual inputs, configuration and dependencies. Look for hidden assumptions about caller identity
or state supplied only by one composition.

### Symptom: looking for "the workflow ID"

Start with the application's named entity and lifecycle phase. Use loop, task and call identifiers for their
respective execution records. One identifier or storage record need not represent the whole application.

## Use Case Examples

A telemetry application can use deterministic components and rules without a model. An agentic review app can
add focused phases, explicit outcomes and a review gate. External connectivity depends on selected components,
tools and model providers.

Start with the checked [First Processor](../basics/05-first-processor.md) guide for component composition.
The [SemSource walkthrough](../basics/09-building-semsource.md) separates product semantics
from framework responsibilities.

## References

- [Rule engine contract](../../openspec/specs/rule-engine/spec.md)
- [Lifecycle contract](../../openspec/specs/lifecycle/spec.md)
- [Rule action definitions](../../processor/rule/actions.go)
- [Orchestration decision skill](../../.agents/skills/orchestration-check/SKILL.md)
- [KV Twofer](02-kv-twofer.md)
- [Streams vs KV Watches](03-streams-vs-kv-watches.md)
- [ADR-028: Orchestration Architecture](../adr/028-orchestration-architecture.md)
