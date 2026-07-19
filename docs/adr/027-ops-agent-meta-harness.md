# ADR-027: Ops Agent — Meta-Harness Pattern for Agent Tuning

## Status

**Accepted (Phase 1)** — refreshed 2026-04-18 with ADR-028 framing; Phase 1 shipped 2026-04-20. The Meta-Harness pattern and the three-phase delivery remain correct; this refresh clarifies that the ops agent is **Layer 4 of the three-layer orchestration architecture** and reuses the coordinator's runtime composition tools, not a parallel control path.

Phase 1 (read-only diagnosis) is complete: the ops agent observes completed loops and graph telemetry, emits structured findings as `ops.diagnosis.*` triples via the `emit_diagnosis` tool, and is e2e-verified via `task e2e:ops`. Phase 2 and Phase 3 remain proposed. The ops seam now carries a second emission tool beside `emit_diagnosis`: `emit_lesson` (ADR-080), which distills completed work into evidence-cited, lifecycle-gated `agent.lesson.*` records injected back into future loops.

## Role within the three-layer orchestration architecture

Semstreams commits to rule skeleton + coordinator agent + ops agent (ADR-028). The ops agent is **Layer 4 — Learning**. Its purpose:

- **Reads telemetry, not individual decisions.** Where the coordinator (Layer 3) makes per-invocation judgments, the ops agent looks at patterns across many completed loops and coordinator decisions. Its observation substrate is the graph — the same ~113 predicates Phase 0 laid down.
- **Proposes refinements to any harness axis.** System prompts, rules, flow topology, model selection, coordinator `decide` schemas, retry policies, tool allowlists. Anything that affects agent behaviour at scale is fair game.
- **Reuses the coordinator's runtime composition tools.** `create_rule`, `manage_flow`, `list_components`, `list_personas`, `list_flow_templates`, `monitor_flow` — all defined in ADR-026 — are the ops agent's deployment surface. Schema validation + governance + sandbox + approval gates (ADR-026 safety model) apply uniformly. There is no separate ops-only deployment path, and no privileged bypass.
- **Closes the improvement loop.** Coordinator makes decisions; ops agent observes whether those decisions correlate with good outcomes; ops agent proposes changes to how decisions are made (the coordinator's `decide` schema, its stock prompt, its retry policy). Rules are skeleton, coordinator is judgment, ops is learning.

## Why reuse coordinator tools

The operational learning from ADR-028: containing schema discipline to one role (the coordinator) works because the surface area is small. The same reasoning applies to deployment tooling — having one set of runtime composition tools, used by both coordinator and ops, means:

- Governance rules reviewed once apply to both.
- Approval gates configured once cover both.
- The audit trail (who changed what, when, why) lands on the same graph entities.
- Adding a new composition capability (e.g. "adjust a retry policy") ships to both in one change.

A parallel ops-only toolset would double the safety surface and invite drift between "what the coordinator can do at runtime" and "what the ops agent can do at runtime." The ADR-028 commitment to one runtime composition surface explicitly rejects that split.

## Context

SemStreams agents produce detailed execution telemetry — 113+ graph predicates across 11
categories covering loop outcomes, tool usage, token costs, error classifications, and
context pressure. This observability surface was built in beta.5 (Phase 0) with the
explicit intent of enabling an ops agent. The Phase 0a query-readiness tests (currently in
semteams, moving upstream via ADR-025) validate that the graph can answer nine operational
queries: success rate by role, tool usage frequency, token cost per step, iteration
distribution, model cost-effectiveness, step-to-loop linkage, tool failure rate, error
categories, and predicate discoverability.

What is missing is the agent that consumes this telemetry to diagnose performance issues
and tune the system.

Stanford's Meta-Harness framework (Lee et al., [arXiv:2603.28052](https://arxiv.org/abs/2603.28052),
March 2026) provides the theoretical foundation. The Meta-Harness insight: LLM performance
depends not just on model weights but on the **harness** — the code that determines what
information to store, retrieve, and present to the model. This encompasses prompts,
retrieval logic, context management, error handling, retry mechanisms, and state
management. Meta-Harness uses an agentic proposer to automatically discover and optimize
harnesses end-to-end through five steps:

1. Inspection — read full execution traces (the proposer reads a median of 82 files per
   iteration)
2. Analysis — counterfactual diagnosis across traces ("this failed because X, not Y")
3. Proposal — generate a modified harness based on causal analysis
4. Evaluation — run the modified harness on held-out tasks
5. Pareto tracking — maintain non-dominated solutions across accuracy/cost/latency
   tradeoffs

Results from the paper: +7.7 points over SOTA on text classification with 4x fewer context
tokens, +4.7 on IMO-level math problems across five models, 76.4% on TerminalBench-2
(ranking #1 among all Haiku 4.5 agents).

SemStreams can implement this pattern using graph-backed observation instead of filesystem
access. Where the Meta-Harness proposer reads files and greps logs, the ops agent would
query the knowledge graph for execution patterns using semantic triples. This should
enable relationship-aware analysis — e.g., correlating tool failures with specific models
and context pressure levels via graph traversal rather than text search. Whether this
advantage materializes in practice depends on the quality of the predicates and the LLM's
ability to reason over graph query results.

This ADR depends on ADR-025 (consolidated component registry and upstreamed Phase 0a tests)
and ADR-026 (coordinator tools for deploying modified configurations).

## Decision

Implement the ops agent as a standard `agentic-loop` instance with a specialized role,
tool set, and system prompt. The ops agent observes execution traces via graph queries,
performs counterfactual diagnosis, proposes harness modifications, and optionally deploys
them using ADR-026 tools. It is not a new component type — its stock flow config lives at
`configs/flows/ops-agent.json` with a stock persona at `configs/personas/ops.json`. It
uses the existing `agentic-loop`, `agentic-model`, and `agentic-tools` components without
modification.

### Tunable harness elements

The "harness" in SemStreams terms maps to seven axes the ops agent can observe and adjust:

| Axis | What is tuned | Observability signal |
|------|---------------|----------------------|
| System prompts | Fragment composition per role (ADR-025 assembler) | Outcome by fragment combination |
| Model selection | Endpoint per role or task type | `LoopCostUSD` / `LoopOutcome` by `LoopModelUsed` |
| Tool allowlists | Enable or disable tools per role | `StepToolStatus` by `StepToolName` by `LoopRole` |
| Context budget | Compaction threshold, headroom reserve, priority weights | `StepUtilization`, `StepTokensEvicted` |
| Rule parameters | Thresholds, timeouts, retry limits | Iteration distribution, escalation frequency |
| Flow topology | Pipeline stages, fan-out degree | Outcome analysis by flow pattern |
| Vocabulary | Triple predicates for graph query precision | Query hit rates |

### Delivery phases

**Phase 1 — read-only analysis.** The ops agent runs with `graph_query` and `rules_query`
tools. Its system prompt instructs periodic analysis of execution patterns. Findings are
written back to the graph as entities with predicates `ops.diagnosis.finding`,
`ops.diagnosis.recommendation`, `ops.diagnosis.confidence`, and `ops.diagnosis.evidence`.
Human operators review and apply changes manually. Phase 1 delivers immediate diagnostic
value before any automated changes are enabled.

> **Implementation note:** `rules_query` does not exist in the shipped implementation.
> The actual read tools for rule and configuration state are `list_rules` + `get_rule`,
> with equivalent `list_*` / `get_*` read tools for flows, personas, and flow-templates.

**Phase 2 — proposed changes.** The ops agent gains ADR-026 tools (`create_rule`,
`manage_flow`). It proposes harness modifications as draft flows and rules. High-risk
changes (model switches, tool removals, topology changes) route through the ADR-026
`ToolCallFilter` and `ApprovalFilter` for human sign-off. Low-risk changes (threshold
adjustments, prompt fragment reweighting) can auto-approve subject to governance review.

Phase 2 requires **no new code**. Operators enable it by adding the proposal tools
(`create_rule`, `update_rule`, etc.) to `configs/flows/ops-agent.json` `allowed_tools`
and mirroring the same list into `agentic-tools.config.approval_required`. The existing
`ApprovalFilter` mechanism handles block-until-approved transitions automatically.

**Phase 3 — automated tuning loop.** The ops agent runs continuously, observing execution
patterns after its own modifications take effect. It maintains a Pareto frontier of
configurations as graph entities. Each tested configuration is stored under the namespace
`{org}.{platform}.ops.config.{configID}` with predicates for `ops.config.accuracy`,
`ops.config.cost_per_task`, `ops.config.p95_latency`, `ops.config.active`, and
`ops.config.parent` (for lineage), plus the full harness parameter set. Pareto dominance
is computed via graph queries. The active configuration is selected based on the current
optimization objective, which operators adjust at runtime.

The intent for Phase 3 is that the ops agent performs counterfactual reasoning —
comparing configurations on multiple dimensions and selecting based on operator-defined
objectives. The evidence for each comparison is a set of retrievable `ops.config.*`
triples. The quality of this reasoning depends on the LLM's ability to perform causal
analysis over graph data, which Phase 1 and 2 will help validate before Phase 3 is
attempted.

### Safety constraints

All changes go through the ADR-026 safety model (schema validation → governance →
sandbox → approval → hot-reload). Additional constraints specific to the ops agent:

- **Budget ceiling**: configurable token budget per analysis cycle prevents runaway costs.
- **Rate limiting**: minimum interval between configuration changes (default 1 hour) prevents
  oscillation.
- **Rollback**: `ops.config.parent` lineage allows reversion to any prior Pareto-optimal
  configuration when a change degrades performance.
- **Blast radius**: Phase 3 initial scope is one flow at a time. Cross-flow optimization is
  Phase 3+.

## Consequences

### Positive

- Reduces manual tuning effort by surfacing evidence-based recommendations. In Phase 3,
  low-risk tuning (threshold adjustments, prompt reweighting) can be automated.
- Graph-backed observation should enable relationship-aware diagnosis over structured
  telemetry, though this advantage over filesystem-based approaches (Meta-Harness) needs
  validation in practice.
- Pareto frontier tracking makes cost/quality tradeoffs explicit and operator-controllable;
  the optimization objective is a runtime parameter.
- Configuration lineage provides auditability — every tuning decision is a graph entity
  with evidence triples.
- Phase 1 (read-only) delivers diagnostic value before any automated changes are enabled,
  de-risking the later phases.
- The ops agent uses the same infrastructure as every other agent — same governance,
  observability, and lifecycle management.

### Negative

- Automated tuning loops can oscillate — Configuration A outperforms B, ops agent switches
  to A, workload shifts, B is now better, switch back. Mitigated by the minimum change
  interval and Pareto tracking; oscillation between Pareto-optimal points is bounded, not
  infinite.
- Counterfactual reasoning quality depends on LLM capability. The ops agent requires an
  Opus-class model for reliable causal analysis. Using a weak model risks incorrect
  diagnoses that degrade rather than improve system performance.
- Graph query volume — continuous analysis generates sustained read load on the knowledge
  graph. Mitigated by configurable analysis intervals and query batching.

### Neutral

- The ops agent's diagnostic entities (`ops.diagnosis.*`, `ops.config.*`) follow the same
  triple pattern as every other entity in the graph. No schema extension is required beyond
  new predicate constant definitions.
- Stanford Meta-Harness reads a median of 82 files per proposer iteration. The ops agent
  issues graph queries instead. The operational pattern — inspect, diagnose, propose,
  evaluate — is the same; the observation mechanism differs.
- The Pareto frontier concept is objective-agnostic. Operators choose what to optimize
  (accuracy, cost, latency, or weighted combinations). Changing the objective is a
  configuration update, not a code change.

## Alternatives Considered

### A. DSPy-style prompt optimization

Use Stanford DSPy's MIPROv2 optimizer for automated prompt tuning. Rejected as the primary
approach: DSPy optimizes individual prompts in isolation. The ops agent must tune the full
harness across seven axes simultaneously. DSPy could serve as a component within the ops
agent's toolkit for the prompt-specific axis, but cannot replace holistic harness
optimization.

### B. Metrics-only dashboard (no agent)

Expose Prometheus metrics and Grafana dashboards for human operators. Rejected as the sole
approach: dashboards show symptoms; the ops agent diagnoses causes. Operators cannot
perform counterfactual reasoning across traces at scale. Prometheus metrics (already
emitted) complement the ops agent for real-time alerting but do not substitute for causal
analysis.

### C. Centralized tuning service

A dedicated service outside the agentic system reads telemetry and writes config changes.
Rejected: violates the "agents all the way down" principle. A centralized service would
require its own lifecycle management, monitoring, and deployment pipeline separate from the
agent infrastructure that already exists. The ops agent benefits from the same governance
and observability as every other agent precisely because it is an agent.

### D. A/B testing framework

Deploy two configurations simultaneously and route traffic between them for direct
comparison. Considered for Phase 3+ as a complement to Pareto tracking, not a replacement.
A/B testing requires traffic splitting, which adds flow topology complexity. Pareto tracking
with sequential evaluation is simpler and sufficient for the initial implementation.

## Related decisions

- [ADR-028](028-orchestration-architecture.md) — names the ops agent as Layer 4 of the three-layer architecture and explains why it reuses the coordinator's runtime composition tooling.
- [ADR-026](026-coordinator-agent-dynamic-flow-composition.md) — defines the runtime composition tools this ADR reuses (`create_rule`, `manage_flow`, etc.) and the safety model they route through.
- [ADR-025](025-semteams-consolidation.md) — upstreamed the Phase 0a query-readiness tests from semteams that prove the graph can answer the nine operational queries the ops agent depends on.
- [ADR-080](080-push-based-agent-memory-and-lesson-artifacts.md) — the ops seam's second terminal
  tool, `emit_lesson`, distills completed work into evidence-cited, lifecycle-gated lessons pushed
  back into future loops' briefs. See [Agent Memory](../concepts/32-agent-memory.md) for the full
  model.

## Implementation sequencing

Gated by ADR-026 step 7 (six flow-composition executors complete). Before then, ops Phase 1 (read-only) can ship with just `graph_query` and `rules_query` — those tools exist today.

1. Stock ops persona + `configs/flows/ops-agent.json` — Phase 1 read-only.
2. Add `ops.diagnosis.*` predicate constants to the agentic vocabulary so findings are queryable.
3. Ship ops Phase 1 — graph-backed analysis + manual human review of findings. Validates that the predicates Phase 0 laid down are sufficient for causal reasoning before wiring any automation.
4. ADR-026 coordinator + `decide` tool + flow-composition executors ship.
5. Ops Phase 2 — grant ops agent the flow-composition tools. High-risk changes gate on human approval; low-risk changes (threshold adjustments, prompt reweighting) auto-approve through the same filter chain coordinator uses.
6. `ops.config.*` predicate constants + Pareto entity schema.
7. Ops Phase 3 — continuous tuning loop, single flow at a time, minimum 1-hour interval between deployments. Cross-flow optimization deferred further.

Phase 1 is valuable on its own and does not require ADR-026 coordinator work to ship. Phases 2–3 build on coordinator infrastructure.
