# ADR-025: Semteams Consolidation

## Status

**Accepted — Phase 1 shipped; Phase 2 REVERTED (2026-06-12).** Phase 1
(framework primitives: approval filter, tool governance hooks) landed in
the alpha series and stands. **Phase 2 (the personalization layer) was
reverted** (`refactor/remove-operating-model-personalization`, −6,929 LOC):
the operating-model package, the `/onboard` command + interview, the
`layer_normalization` capability, and profile-context injection were
premature *product* logic — they crossed the very engine/product boundary
this ADR draws below ("semstreams is the engine; semteams is the product").
They write `user.teams.*` entities over a `teams.operating_model.*` flow,
were wired in no framework config, and are returned to semteams. The
framework keeps only the generic primitives it always owned (dispatch core,
the `intent_classifier` capability, approval/governance filters, built-in
executors). semteams may redesign and re-consume the framework substrates
(graph-ingest mutation lanes, ADR-054 indexing profile, payload registry,
rule engine) if it wants the feature back. semdragons doesn't import
semteams, so this ADR has no semdragons-facing surface.

## Context

Semteams started as a fork of semstreams, then became a reference design importing
semstreams as a dependency (`github.com/c360studio/semstreams v1.0.0-beta.6`). It
provides six `teams-*` processor components that parallel semstreams' six `agentic-*`
processors.

The duplication problem is substantial: roughly 85% of semteams' processor code
(14,000–16,000 LOC out of ~26,000) is literally duplicated from semstreams with only
package name changes. The repos drift apart on every bug fix. semstreams has
length-truncation detection (beta.2) and capability-based timeouts (ADR-024) that
semteams lacks; semteams has features — an intent classifier, approval filter, and
built-in executors — that have not been upstreamed.

Only ~4,000 LOC is genuinely novel across two categories:

**Framework primitives** — prompt assembler (304 LOC), tool categories (99 LOC),
`ToolCallFilter` + `ApprovalFilter` (86 LOC), tool call governance filter (168 LOC),
and built-in executors: bash, http\_request, web\_search/Brave, rules\_query (940 LOC).

**Personalization layer** — operating model five-layer interview (1,054 LOC), onboarding
interview (1,100 LOC across dispatch and memory), intent classifier (200 LOC), and
profile context injection (514 LOC).

An additional ~2,500 LOC — DID identity, boid handler, GitHub executors, graph\_query —
has already been upstreamed to semstreams.

The subject namespace difference (`teams.*` vs `agent.*`) is a deployment configuration
choice, not an architectural fork. semstreams' port-resolved subjects (beta.8) already
support arbitrary namespace configuration; no code change is required for operators who
want `teams.*` subjects.

Two planned capabilities — a coordinator agent that composes flows at runtime (ADR-026)
and an ops agent that tunes agent behavior (ADR-027) — both require a single component
registry and unified observability surface. The current split blocks both.

## Decision

Merge semteams' novel features into semstreams as config-gated additions. Semteams
becomes a product deployment with no custom Go processors.

### Scope boundary: UI stays in semteams

The semteams Svelte UI — flow builder, persona editor, rule management, ops dashboard —
is out of scope for this consolidation and remains in the semteams repository. The UI is
the product experience layer: it calls semstreams APIs (`flowstore`, `FlowEngine`,
component registry, graph queries) but does not duplicate engine logic. This is the
correct architectural boundary. semstreams is the engine (Go components, graph, NATS,
agentic processors). semteams is the product (UI, flow configs, rule configs, persona
configs, documentation). The coordinator agent (ADR-026) and the UI are peers — both
call the same `flowstore` and `FlowEngine` APIs to manage flows, one via tool executors,
the other via HTTP endpoints.

### Phase 1: Framework primitives

The prompt assembler moves to `agentic-loop/prompt/`. Tool categories move to
`agentic-tools/categories.go`. The `ToolCallFilter` interface and `ApprovalFilter`
implementation move to `agentic/filter.go` and `agentic-tools/approval_filter.go`. The
tool call governance filter moves to `agentic-governance/tool_filter.go`. Built-in
executors (bash, http\_request, web\_search, rules\_query) land in
`agentic-tools/executors/`.

Three config additions gate the new behavior: `approval_required` (list of tool names)
on agentic-tools, `enable_tool_governance` on agentic-governance, and `enable_categories`
on agentic-tools. All default to disabled.

### Phase 2: Personalization layer — REVERTED (2026-06-12)

> **Reverted.** Everything in this section was removed from semstreams as
> premature product logic and returned to semteams (see Status). The
> `intent_classifier` capability is the one piece retained (as a generic,
> default-off dispatch capability). The rest below is kept only as the
> historical record of what was upstreamed and then reversed.

The operating model package moves to `agentic/operating-model/`, following the same
`Graphable`/`Triple` pattern as every other entity type — it is a domain concept, but
one that produces triples exactly like any other. The intent classifier moves to
`agentic-dispatch/intent_classifier.go`. The onboarding command moves to
`agentic-dispatch/onboarding_*.go`. Profile context handlers move to
`agentic-loop/profile_context_handler.go` and `agentic-memory/profile_context.go`.

Three config additions: `enable_intent_classification` and `enable_onboarding` on
agentic-dispatch, `enable_profile_context` on agentic-memory. All default to disabled.

### Phase 3: Migration

A stock flow config at `configs/personal-agent.json` demonstrates the consolidated
personal-agent use case. Semteams rule configs are migrated to reference semstreams
component types. All `processor/teams-*` directories are deleted from semteams.

### Phase 4: Validation

All semteams e2e tests pass against semstreams with the personal-agent config. All
existing semstreams unit, integration, and e2e tests continue to pass. Semteams' ops
agent Phase 0a query-readiness tests (from `graph_writer_ops_test.go`) move upstream,
ensuring observability coverage survives the consolidation.

## Consequences

### Positive

- Eliminates dual-maintenance of six parallel processor implementations. Every bug fix,
  performance improvement, and new feature lands once.
- Unblocks ADR-026 (coordinator agent) and ADR-027 (ops agent), both of which require a
  single component registry and a unified observability surface.
- semstreams ships with built-in executors and personalization features, removing the need
  for a separate project to demonstrate personal-agent use cases.
- Ops agent Phase 0a query-readiness tests move upstream, ensuring that observability
  coverage is verified as part of the semstreams CI pipeline rather than a separate repo.

### Negative

- Touching all six agentic processors in Phases 1–2 carries regression risk. Mitigated
  by the existing test pyramid: unit tests, integration tests (testcontainers), and tiered
  e2e tests.
- The operating model is a domain concept — how a user works — living in a graph engine
  framework. The counterargument is that it is just another entity type producing triples,
  following the same pattern as every other `Graphable`. The discomfort is real but the
  architectural objection is weak.
- Semteams feature work must freeze during Phases 1–3 to prevent further divergence. Any
  new capability developed during the consolidation window must land in semstreams directly.

### Neutral

- Subject namespace (`agent.*` vs `teams.*`) is already configurable via port definitions
  (beta.8). No code change is required for deployments that want `teams.*` subjects.
- Semteams as a repository remains substantial post-consolidation: the Svelte UI, flow
  configs, rule configs, persona configs, and documentation. What it loses is duplicated
  Go processor code. What it keeps is the product experience layer.

## Alternatives Considered

### A. Thin layer — semteams imports agentic-\* directly, adds only novel components

Would limit semteams to genuinely new processor types. Rejected: the novel features
(intent classification, profile context injection, approval filter) need to hook into the
internals of agentic-dispatch, agentic-loop, and agentic-tools at points that are not
currently exposed. Without hook points, semteams would fork those files again within one
or two feature cycles, recreating the divergence problem.

### B. Extension points — semstreams adds interfaces so semteams plugs in without forking

Would have semstreams define `MessageClassifier`, `ContextInjector`, `ToolCallFilter`,
and `ProfileProvider` interfaces, with semteams providing implementations. Rejected:
designing good interfaces takes 3–5 weeks and carries its own risk — wrong abstractions
are harder to reverse than merged code. The merge path is 3–4 weeks of moving tested code
that already works, with two CI pipelines confirming correctness at every step. Two
separate repos with two CI pipelines also remain, which is the operational problem the
consolidation is solving.

### C. Status quo — keep separate repositories

Rejected. 85% duplication with active drift, dual maintenance burden on every bug fix,
and a hard blocker on two planned architectural capabilities (ADR-026, ADR-027). The
evidence has been accumulating since beta.1; continuing to defer has a compounding cost.
