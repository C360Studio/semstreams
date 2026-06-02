# Mapping SemStreams Patterns to Frontier-Harness Patterns

If you've worked with Claude Code, Cursor, Codex, Devin, or any other
frontier-LLM harness, you've seen patterns like skills, subagents,
tools, hooks, MCP servers, workflows, and plans. SemStreams ships
analogs to most of them — but the analogs have a different center of
gravity. This document maps the surface vocabulary so you can ground
new semstreams concepts in patterns you already know, then explains
the load-bearing distinction that makes the semstreams version of
each primitive different from the harness version.

The goal is dual-purpose: external consumers can navigate the
framework faster, and internal design conversations get a sharper
"what's the frontier-harness analog, and what does our shape change
about it?" lens for evaluating new proposals.

## The load-bearing distinction

Every frontier harness assumes a **conversation-centric** execution
model: one model, one context window, one user, ephemeral state.
Skills load instructions into the conversation. Subagents open a
fresh context window and return to the parent. Workflows are
multi-step recipes the model orchestrates from inside one
conversation. Memory persists; the live state of an in-flight flow
does not. When the conversation ends, the orchestration is gone.

SemStreams assumes a **graph-centric** execution model: durable
state, multi-tenant, operator-observable, restart-safe. Many
concurrent agent loops write to a shared knowledge graph. Lifecycle
Participants survive process restarts. Operators query in-flight
flows the same way they query any other graph entity. The framework
is a service — it expects N concurrent flows whose state outlives
any one conversation.

That distinction propagates through every primitive. The mapping
table below is useful for orientation, but every row carries the
same shift: "the conversation-local version of this is what you
might know; semstreams' version is the durable graph-native version
of the same idea."

## The mapping

| Frontier-harness pattern | SemStreams equivalent | What changes |
|---|---|---|
| **Skill / slash command** | NL classifier → rule → component dispatch (canonical: ADR-045 `research_graph`) | Skills inject prose into the model's context for the model to interpret; components run as code with bounded LLM calls at named judgment points |
| **Subagent** | `agentic-loop` with role + tool allowlist | Subagents own one conversation context; loops are durable Lifecycle Participants with restart recovery, operator-introspectable trajectory, concurrent execution |
| **Tool** | Tool executor registered in `agentic-tools` | Same shape conceptually; registry is config-driven, governance-rule-gated, with per-loop / per-role admission |
| **Hook** | Rule with action triggered on entity-state change or stream event | Hooks are imperative + conversation-local; rules are declarative + survive restart + observable in trajectory + composable across loops |
| **MCP server** | NATS-Direct gateway / GraphQL gateway / MCP gateway | MCP servers are per-conversation external; gateways are persistent first-class framework primitives sharing the same graph |
| **Workflow** | Rule chain + Lifecycle Participant (ADR-049) | Workflows are model-driven from inside one conversation; chains are operator-authored, durable per-instance, with audit history at no extra cost |
| **Plan / Plan mode** | Rule chain authoring + the `Plan` design primitive | Plans are model-generated and conversation-scoped; rule chains are operator-authored, version-controlled, composable across loops |
| **Memory** | Triples on entities + AGENT_LOOPS KV + ObjectStore content refs | Memory is conversation-spanning but model-managed; triples are operator-managed, queryable, structurally-typed via vocabulary |
| **Context management / compaction** | Trajectory + agentic-loop's compaction | Built-in to the loop with per-loop budget; not a harness-level concern operators have to wire |
| **Sub-skill / nested skill** | Component composed from BoundedDispatcher + sub-component fan-out (ADR-048) | Nested skills are model-driven; composition is structural |

## Worked example 1: `research_graph` as the skill analog

A Claude Code skill named `/research-graph` would load instructions
that say "to research a topic, decompose it into sub-queries, search
each, fuse results, synthesize an answer." The model reads those
instructions and drives execution — picking tools, managing
iteration, deciding when to stop.

The semstreams equivalent (shipped in ADR-045) is a tool
`research_graph(topic, hints?)` registered in
`processor/agentic-tools/executors/research_graph.go`. The parent
agent calls it like any other tool. Behind the tool, six processor
components execute a rule chain:

```
processor/research-graph-classify/    NL classifier wrap
processor/research-graph-route/       LLM judgment: which strategy?
processor/research-graph-execute/     Code: parallel multi-tier fan-out
processor/research-graph-assess/      LLM judgment: refine or finalize?
processor/research-graph-synthesize/  LLM judgment: compose answer
processor/research-graph-llmwrap/     Shared LLM-call infrastructure
```

The parent agent never sees inside. It calls `research_graph(topic)`,
its current iteration terminates, and the result arrives on a
later iteration via the standard continuation pattern. The
orchestration runs as a rule chain operating on triples; LLMs are
invoked at three named judgment points (`route_search`,
`assess_sufficiency`, `synthesize_answer`) with bounded structured
input/output schemas — not as free-form agents driving the loop.

**Why this matters**: ADR-045 explicitly addresses the
"reasoning crimp" failure (§Context "The reasoning crimp"):
frontier agents do not reliably drive graph-shape reasoning loops
even with good instructions and constrained tool allowlists. The
LLM lacks trained ergonomics for multi-hop graph traversal +
score normalization + fusion. The skill pattern — load
instructions, let the model drive — would hit this exact failure
mode. Semstreams moves orchestration out of the conversation
entirely and lets the LLM do what it's trained for: examine
concrete results, decide next move.

Surface-level: "skill with more structure" tracks. Architecturally:
the orchestration locus moved out of the LLM's loop, and that's
the load-bearing change.

## Worked example 2: `agentic-loop` as the subagent analog

A Claude Code subagent spawns a fresh conversation context, runs to
completion with its own tool allowlist and persona, then returns a
result to the parent. State is conversation-local; when the subagent
ends, its context disappears.

The semstreams `processor/agentic-loop/` runs the same shape
durably:

- Loop state lives in the `AGENT_LOOPS` KV bucket (the loop entity
  is graph-visible).
- Trajectory is observable in real time via operator gateways.
- Tool calls flow through governance rules (ADR-039) before
  execution.
- The loop is a Lifecycle Participant (ADR-049) — restart recovery,
  audit history, operator-writable controls (pause / resume /
  cancel).
- Multiple loops run concurrently in the same service; each has its
  own entity-ID; cross-loop reasoning is graph-native.

**Architectural shift**: subagents are designed for "I need a
separate context for a sub-task within one user's conversation."
Agentic loops are designed for "I need to run a structured agent
flow as part of a multi-tenant service." Same surface shape
(parent → child → return), different durability + observability +
concurrency guarantees.

## Worked example 3: Lifecycle Participant as the workflow analog

Frontier harnesses have grown explicit workflow primitives — named
multi-step flows that the model orchestrates from inside one
conversation, with named steps, tool calls, and decision points.
The user-facing affordance: make multi-step flows easier to author
and inspect within a conversation.

The semstreams equivalent is **rule chain + Lifecycle Participant**
(ADR-049). A workflow becomes:

- A `lifecycle.Workflow` declaration with phases, transitions, and
  operator-writable predicates.
- Per-instance Participant entities in `ENTITY_STATES` (the
  bucket-ownership rubric — workflow state is graph-visible).
- Rules that fire on phase transitions or entity-state changes,
  dispatching to typed components.
- Audit history at no extra cost via KV revision replay.
- Operator gateway endpoints (`GET /workflows?name=...`) that work
  the same across every workflow type.

**The architectural shift**: a Claude workflow is what a rule chain
looks like when you stay inside one conversation and let the model
drive step transitions. Semstreams pulls that out of the conversation
— steps become rules with declarative conditions, transitions become
triple writes the rule engine observes, the model is invoked at
bounded judgment points, and the whole flow has a named instance in
the lifecycle layer that operators can see, restart, audit, run in
parallel.

External positioning: *"Claude workflows are what semstreams' rule
chains look like when you only need one conversation's worth of
durability. Rule chains are what workflows look like when you need
a multi-tenant durable service."* That's a defensible posture — not
dismissive — because both primitives are solving real problems on
the same surface, just at different scales.

## Worked example 4: Gateways as the MCP-server analog

MCP servers expose tools to a single conversation. The conversation
talks to the server over a transport; tools are invoked per-message;
server-side state is whatever the server chooses to keep. When the
conversation ends, the per-conversation tool-call history is gone
(though the server's persistent state survives).

Semstreams ships **gateways** as the persistent equivalent
(`graph/query/` family + GraphQL + MCP gateways at
`gateway/graph-gateway/`):

- Multiple consumers (agent loops, sister-repo services, external
  HTTP clients) share the same gateway and the same underlying
  graph.
- Tool invocations are observable in trajectory + gateway metrics.
- Governance rules can rate-limit, deny, or transform calls.
- Gateways themselves are first-class framework primitives — they
  are components, configured per-deployment, monitored as
  infrastructure.

**Shift**: MCP servers are external surfaces designed for
per-conversation tool exposure. Semstreams gateways are internal
infrastructure designed for multi-consumer durable queries. If
you're building a "tool" that one conversation needs once, MCP is
the right shape. If you're building a query path that many
concurrent consumers + a graph need to share, a gateway is.

## When to reach for which

The decision criterion is not "frontier-harness pattern vs
semstreams pattern" — it's "**where is my flow's center of
gravity?**"

| If your flow needs… | Reach for the harness pattern | Reach for the semstreams pattern |
|---|---|---|
| ...one user, one conversation, one outcome | ✅ | |
| ...transient state that's fine to lose on restart | ✅ | |
| ...the model to drive multi-step reasoning interactively | ✅ | |
| ...one author, one runtime, one execution | ✅ | |
| ...durability across process restarts | | ✅ |
| ...multi-tenancy (N concurrent flows of the same shape) | | ✅ |
| ...operator visibility on in-flight state | | ✅ |
| ...graph-native state shared across flows | | ✅ |
| ...governance / admission / audit on every action | | ✅ |
| ...flows that produce evidence consumed by later flows | | ✅ |

The interesting decisions live in the long tail — flows that COULD
fit inside one conversation but benefit from durability if you make
the lift. The bias should be conservative: don't carry framework
durability for flows that don't need it; do reach for it when you
can name the specific property (restart-safety, concurrency,
operator audit, cross-flow sharing) you're paying for.

## Anti-patterns from past lessons

When the wrong pattern is reached for, the failure mode is
consistent: a conversation-centric pattern gets stretched into a
graph-centric problem space, app code grows its own state plumbing
to compensate, and the resulting code becomes a migration blocker
because it predates the substrate that should have been used.

Three crystallized lessons worth naming explicitly:

**The semspec workflow trap** (referenced in ADR-045 §Context "The
semspec trap"). semspec was an early adopter, predating the mature
rule engine. To compensate for missing primitives, it built 7,264
LOC of `workflow/reactive/` — its own plan + execution state
machines, importing the now-retired `processor/reactive/` engine
plus app-side state plumbing. That code is now a migration blocker
for Phase 5 of the reactive-workflow retirement. The durable
lesson: *gaps in the framework surface upstream as engine work;
they never get worked around as app-side state plumbing*. If
research_graph had been written as a Claude skill with the LLM
driving — and then later needed durability — the team would have
hit the same trap.

**The role-compression reversal** (semteams ADR-040 → ADR-041,
captured in `feedback_frontier_floor_changes_role_split_calculus`).
semteams split researcher + curator into separate chain roles on
small-model cognitive-load grounds (ADR-040), then re-collapsed
them on frontier-floor grounds (ADR-041). The lesson generalizes
beyond role compression: orchestration choices that look right at
the small-model frontier may look wrong six months later, and
vice-versa. Anchoring orchestration in the framework substrate
(rules + components) instead of in conversation-shaped patterns
(skills + persona prose) makes those reversals cheaper because the
substrate is the same; only the prompts move.

**Graph-not-for-agent-reasoning**
(`feedback_graph_not_for_agent_reasoning`, 3 instrumented
incidents). Frontier agents (Gemini 3.x Pro, Sonnet 4.6) do not
reliably navigate the SKG even with `search_graph` / `query_entity`
/ `summarize_graph` in the allowlist and the persona prompt
explicitly encouraging graph-first lookups. The fix that worked
was injection-side (lineage triples into the prompt payload), not
query-side. This is exactly the failure mode the skill analog of
research_graph would hit: load instructions, give the model graph
tools, hope it uses them. Empirically: it doesn't. The rule-chain
shape moves the orchestration where the LLM doesn't have to drive
it.

**Reactive patches vs engine completion**
(`feedback_reactive_patches_vs_engine_completion`). When the same
shape recurs across 2+ tags as small ad-hoc additions, stop and
reframe as deliberate completion of the primitive set. Carrying
the wrong substrate forward per-tag is more expensive than
absorbing one architectural-clarity tag. Applies to the
"every-flow-becomes-its-own-bespoke-skill" failure mode at scale:
N skills with N-1 different orchestration shapes is the pattern
this discipline catches.

## Positioning posture for external comms

The mapping enables a defensible, non-dismissive framing:

> "Frontier harnesses (Claude Code, Cursor, etc.) are conversation-
> centric: one model, one context, ephemeral state. They're
> excellent for what they're designed for — interactive
> development, single-user assistance, prototype agent flows.
>
> SemStreams is graph-centric: durable, multi-tenant, operator-
> observable. It's designed for what frontier harnesses aren't —
> services running N concurrent agent flows whose state outlives
> any one conversation, sharing a knowledge graph, governed by
> declarative rules, surviving restarts.
>
> The primitives map cleanly: skills ↔ tools dispatching to rule
> chains; subagents ↔ agentic loops; workflows ↔ rule chains +
> Lifecycle Participants; MCP servers ↔ gateways. The shape of
> each is similar; the durability + observability + concurrency
> guarantees differ."

This positions semstreams as the right answer when those
durability + observability + concurrency properties are
load-bearing — not as a replacement for harness patterns when
they're not.

## When the mapping helps you internally

Three patterns worth applying in design conversations:

1. **Reaching for an analog to ground a new design**. "What's the
   frontier-harness version of this? OK, now what does our
   durability requirement change about that shape?" Usually
   surfaces the load-bearing constraint quickly.

2. **Stress-testing a proposed pattern**. "If we did this as a
   Claude skill — the model drives, state is conversation-local —
   what would fail?" If the answer is "nothing important," maybe
   the proposal doesn't need framework substrate at all. If the
   answer is "concurrent flows would collide / restart loses
   state / operators can't see in-flight progress," you're paying
   for the right thing.

3. **Recognizing semspec-trap-shaped proposals**. If a new component
   is starting to grow its own state machine + KV bucket + manual
   restart logic, ask: "is this a substrate gap or an app-shaped
   problem?" If substrate, file it as engine work per the
   discipline memory; don't paper over with app-side plumbing.

## Cross-references

**ADRs implementing the substrate mapped above**:

- [ADR-045](../adr/045-graph-search-rule-chain.md) — `research_graph` as the skill analog (canonical worked example).
- [ADR-049](../adr/049-lifecycle-prime-schema-over-entity-states.md) — Lifecycle Participant as the workflow analog.
- [ADR-048](../adr/048-bounded-dispatcher-and-triples-substrate.md) — BoundedDispatcher (the sub-skill / nested-component analog).
- [ADR-039](../adr/039-tool-call-governance-rule-driven.md) — Tool governance (the admission layer on the tool analog).

**Companion concept docs**:

- [Orchestration Layers](14-orchestration-layers.md) — the rules-vs-components discipline that shapes every workflow/chain.
- [Phased Agentic Chains](25-phased-agentic-chains.md) — the canonical multi-step pattern, applicable when frontier harnesses would reach for "workflow" or "multi-step skill."
- [Payload Registry](15-payload-registry.md) — polymorphic dispatch (what tools / hooks / skills look like when typed).
- [Query Access](11-query-access.md) — GraphQL / MCP / NATS-Direct gateway choice (the MCP-server analog).

**Discipline memories** that crystallize past lessons referenced here:

- `feedback_graph_not_for_agent_reasoning` — why the skill analog of `research_graph` would fail.
- `feedback_frontier_floor_changes_role_split_calculus` — why orchestration choices anchored in conversation patterns are fragile.
- `feedback_reactive_patches_vs_engine_completion` — when to reach for the substrate completion instead of per-pattern additions.
- `feedback_bucket_ownership_rubric` — when workflow state belongs in ENTITY_STATES vs a private bucket.

If you're building a new primitive on semstreams, the workflow is:
identify the frontier-harness analog (this doc), apply the
orchestration layering discipline (concept 14), pick the right
storage primitive (concept 02 KV-twofer, concept 03 streams-vs-KV),
and validate the shape against the bucket-ownership rubric
(`feedback_bucket_ownership_rubric`). The mapping is the orientation
step; the discipline memories are the design gates.
