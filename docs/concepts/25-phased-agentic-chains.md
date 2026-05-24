# Phased Agentic Chains

The framework pattern for composing N agent phases connected by rules
firing on decision triples. Each phase is a role with a persona + tool
palette + action_allowlist + iteration cap. Within a phase, the LLM
composes work through tool selection (implicit router). Between
phases, rules dispatch on decision triples stamped by the previous
phase (explicit router).

semstreams ships the substrate. Apps compose phases into specific
chains. The first reference instance is ADR-045's R0-R6 graph-research
chain (internal graph state); the second is the semteams gatherer
chain (external composition via `bash` + sandbox container).

If you're building a multi-step agentic workflow on semstreams, **read
this first**. Most of the primitives you need already ship; the gaps
that remain are scoped and tracked.

> **Note 2026-05-24**: The workflow-primitives design exercise
> [resolved as outcome C+](../proposals/workflow-primitives-decision.md):
> `BoundedDispatcher` ships as substrate, `.triples` completes the
> rule-engine multi-valued primitive set, and first-class workflow
> primitives are explicitly out of scope at this time. The
> phased-agentic-chain pattern remains the canonical shape for
> multi-step agentic workflows on semstreams.

## Why this pattern exists

Sub-frontier models collapse on multi-turn structured tool calling.
The Berkeley Function Calling Leaderboard documents the failure
shape: single-turn accuracy 77.5%, multi-turn 14.8–68%. Asking one
agent to juggle a five-step workflow in a single context window
fights the empirical floor of the models that most operators can
afford to run.

The framework's response is to encode multi-step orchestration as
**deterministic rule transitions between LLM-judgment phases**
rather than as a single agent juggling all state in its context
window. Each phase is focused — small tool palette, narrow persona,
constrained terminal vocabulary, bounded iteration. Between phases,
the rule engine handles transitions deterministically. The LLM
doesn't have to maintain state across phase boundaries; the graph
carries it.

This is the application of the [Two Layers](14-orchestration-layers.md)
discipline (rules orchestrate, components execute) to specifically
agentic workflows.

## Composition

```text
Phase     = (role, persona, tool_palette, action_allowlist, max_iter)
Transition = Rule(match: prev_role + decision_triple) → publish_agent(next_phase)
Chain     = ordered sequence of phases joined by transitions
```

A chain's vocabulary lives in its rule pack. Categories don't leak to
the coordinator above (the coordinator knows only category names like
`research` / `respond_direct`); phases don't leak to the components
within (a component knows only its inputs and outputs). This layered
encapsulation is what lets a chain evolve its internal phase graph
without touching upstream or downstream callers.

## Two-layer dispatch

Every phased agentic chain has two routing layers:

| Layer | Form | Where it lives | Who routes |
|---|---|---|---|
| **Phase transitions** | Explicit, structural | Rules pattern-matching on `(prev_role, decision_triple)` | Rule engine |
| **Within-phase tool use** | Implicit, model-judgment | LLM picks tools from the phase's allowlist, guided by persona prose | The agent in the phase |

The two compose: the LLM dispatches tool calls within the phase until
it emits `decide(action="...")`, which stamps a triple the rule engine
picks up to fire the next transition. Phase transitions are
deterministic and auditable; tool composition is flexible and
model-driven. This is the same shape as Anthropic skills or any
multi-step agentic framework, but with the orchestration layer made
first-class via rules instead of hidden inside a model-driven runtime.

## Graph triples as routing wire

Decision triples on the loop entity are the dispatcher's wire format.
There is no message-passing layer, no event bus. `decide(action="X")`
inside a phase stamps `coordinator.decision.next_action="X"` on the
loop entity; a transition rule matches the triple and fires the next
`publish_agent`. The mechanics are the same as any other rule firing
on KV state — the [KV Twofer](02-kv-twofer.md) carries it.

This makes the entire chain:

- **Observable**: every transition is a graph state change
- **Replayable**: trajectory + rule firings can be reconstructed
- **Audit-friendly**: who decided what, when, with what reason
- **Tool-callable**: other agents (or operators) can read chain state via the same graph queries everything else uses

## What semstreams ships (primitive inventory)

For *any* phased agentic chain to be implementable without reinventing
framework primitives:

| Primitive | Status | Location |
|---|---|---|
| Rule engine with predicate matching | ✅ shipped | `processor/rule/` |
| `publish_agent` rule action | ✅ shipped | `processor/rule/actions.go:118` |
| `action_allowlist` on rule action | ✅ shipped | `processor/rule/actions.go:118` |
| `MaxIterations` per-action | ✅ shipped | rule action level |
| agentic-loop with `decide()` tool | ✅ shipped | `processor/agentic-loop/`, `processor/agentic-tools/decide.go` |
| `action_allowlist` enforcement + SAP coercion | ✅ shipped | `processor/agentic-tools/decide.go:399` |
| SAP coercion metric (`action_allowlist_sap_coerced_total`) | ✅ shipped | drift telemetry |
| Persona system with cross-role namespacing | ✅ shipped | beta.78 file-loader fix |
| Per-role tool filtering | ✅ shipped | ADR-039 governance |
| Trajectory manager | ✅ shipped | KV-backed |
| Loop lineage (`loop_id`, parent chain) | ✅ shipped | message metadata |
| `read_loop_result` tool | ✅ shipped | inter-phase data passing |
| `BashExecutor` (local + sandbox) | ✅ shipped | `processor/agentic-tools/executors/bash.go` |
| Sandbox client | ✅ shipped | `processor/agentic-tools/sandbox/` |
| Chain-scoped worktree on `BashExecutor` | 🟡 gap | tracked, see issues |
| Always-warm sandbox documented as default | 🟡 gap | tracked, see issues |
| Preview-first output for large `bash` fetches | 🟡 gap | tracked, see issues |
| URL allowlist + egress rate ceilings | 🟡 gap | tracked, see issues |
| Per-chain URL response cache | 🟡 gap | tracked, see issues |
| Trajectory enrichment for URL fetches | 🟡 gap | tracked, see issues |

For graph-internal chains (the R0-R6 class), that's the whole list —
everything an app needs to build a phased agentic chain over internal
graph state already ships. The gaps are entirely on the
external-composition axis (the gatherer class).

## Substrate / capability / application split

semstreams serves multiple applications (semteams as a multi-agent
research application; semconnect as an OGC Connected Systems API
server; future apps as they appear). Not every app needs every part
of the framework. The split that lets one substrate serve N apps
cleanly:

| Layer | What lives here | Example |
|---|---|---|
| **Substrate** (semstreams core) | Primitives every consumer uses or might use. The inventory above. | Rule engine, agentic-loop, `BashExecutor` |
| **Capability** (semstreams package or sister) | Opt-in bundles for a class of apps. Reusable Go components + reference rule shapes. | The R0-R6 components (`nl_classify`, `route_search`, etc.) as reusable primitives any agentic-reasoning app can compose |
| **Application** (semteams, semconnect, etc.) | Specific phase configuration — rule packs, persona prose, action_allowlist values, tool palette choices, the chain's actual wiring | Semteams's research chain rule pack; semconnect's CS API endpoint configs |

**Substrate ships in semstreams core.** Capabilities ship as
semstreams packages or sister repos. Applications wire substrate +
selected capabilities into specific chains.

The boundary is not "config vs code." Even some config
(deployment recipes, reference compose files, example chain shapes)
lives in framework because it documents how to use framework
primitives. The boundary is **reusability across consumers**: if every
consumer might want it, substrate; if a class of consumers wants it,
capability; if one specific app wants it, application.

## Reference instance 1: R0-R6 graph-research chain (internal)

Internal-only chain — operates entirely on local graph state. See
[ADR-045](../adr/045-graph-search-subloop.md) for the full design and
`docs/operations/22-adr045-phase1-plan.md` for the implementation
plan.

Phase shape:

```text
classify → route → execute → assess → synthesize
```

- **`nl_classify`** — intent → routing-category triple
- **`route_search`** — picks search strategy → triple
- **`execute_subqueries`** — runs queries → result triples
- **`assess_sufficiency`** — done? → triple
- **`synthesize_answer`** — produces output → terminal triple

Each component is an LLM-judgment step writing a triple the next
transition rule matches on. The five components ARE substrate
primitives (reusable across any agentic-reasoning app); the rule pack
wiring them in this specific sequence is a reference instance, not
canonical config for every consumer. Apps with different intent
spaces or different sufficiency criteria compose the same components
into different chains.

## Reference instance 2: semteams gatherer chain (external)

External-composition chain — the gatherer phase reaches out to the
web via `web_search` (structured) and `bash` + `curl` (sandboxed).
See `project_semteams_gatherer_sandbox_case_study` (in agent memory)
for the production write-up.

Phase shape:

```text
coordinator → plan → gather → synthesize
```

The gatherer phase has a mixed tool palette: a structured-tool
(`web_search`) and a sandbox escape (`bash`). The model picks per
step based on persona-prose decision criteria; the persona teaches
the routing heuristic ("when a snippet points at a URL but the
snippet itself isn't the evidence, use bash"). 13 bash + 11
web_search calls interleave in one production trajectory on
gemini-2.5-flash — the compositional pattern that snippet-only
tooling can't reach.

The chain pattern is identical to R0-R6. The phase shapes differ:
R0-R6's phases are single-tool LLM-judgment steps; the gatherer
phase is a mixed-tool composition step. Both fit the pattern.

## Discipline for designing phases

Two memorialized disciplines apply to every phase design:

- **[Tool signatures ask for intent, not structure](#)** (memory:
  `feedback_tool_signature_intent_not_structure`). Tools require only
  fields where model judgment is the value-add; backend derives IDs,
  envelopes, timestamps, defaults from intent. The model writes what
  it knows; the executor builds the wire format.
- **[Tool-selection persona prose needs decision criteria](#)**
  (memory: `feedback_persona_prose_needs_decision_criteria`). Every
  tool sentence in agent prose must carry a "when to use this"
  criterion; sentences without criteria get ignored or backfire on
  small models. Per-tool purpose framing, decision-axis framing,
  concrete examples of non-obvious idioms, anti-ceremony nudges,
  negative-shape definitions of punt actions.

These two disciplines together — backend handles structure, persona
teaches when — are what make phase design tractable for sub-frontier
models. Tool surface + decision criteria + action_allowlist +
iteration cap define a phase the model can execute reliably.

## When to use this pattern

Use a phased agentic chain when:

- The workflow has 3+ distinct steps each requiring LLM judgment
- The steps have a deterministic order (or branching that fits a small DAG)
- Each step has a focused output that the next step can pattern-match on
- Audit / replay / governance matter

Use a single-phase agent (just an agentic-loop with a rich tool
palette) when:

- The workflow is exploratory / open-ended
- Step order is genuinely model-discovered, not author-specified
- Composition is more important than sequencing

Use a rule-only chain (no agents at all, just components) when:

- No step requires LLM judgment
- Everything can be expressed as deterministic component invocations on graph state changes
- See [Orchestration Layers](14-orchestration-layers.md) §Pattern Catalog

## Anti-patterns

- **`shell_exec` or `run_tool` as a global registry-level escape
  hatch.** Loses the composition benefit (one model turn per command =
  the multi-turn collapse failure mode) AND bypasses per-phase
  governance. Sandbox tools live in specific phase palettes, not the
  global registry.
- **Magic-number thresholds in persona prose** ("after 5 web_searches
  use bash"). Models literally count and bail at the threshold even
  with thin evidence. Goodhart on iter count. The router rule lives
  in the *condition shape* ("when a snippet points at a URL but the
  snippet itself doesn't have the evidence"), not a counter.
- **Cross-phase summaries in role prose** ("and synthesize will reject
  if you don't gather X"). Lateral coupling between phases. The
  dispatcher rule is the right place for cross-phase contracts, not
  persona prose. See `feedback_persona_prose_needs_decision_criteria`.
- **Front-loading "how this loop ends" framing** as a structural
  prelude before the workflow. Empirically reduces work output —
  phases do less work and bail faster when given an end-state
  preamble. Counter-intuitive but reproducible.
- **App-side state machines around rule-engine gaps.** If the chain
  pattern needs a primitive the rule engine doesn't have, file it as
  engine work. Don't build app-side state plumbing — see [the semspec
  trap](14-orchestration-layers.md#the-semspec-trap).
- **Shipping a chain in semstreams that only one consumer uses.**
  Specific chain configurations are application concerns. Components
  generalize; chains don't. If exactly one app wants the chain, the
  chain belongs in that app; semstreams ships the components it
  composes.

## See also

- [ADR-028: Orchestration Architecture](../adr/028-orchestration-architecture.md) — the rule-skeleton + coordinator + ops architecture this pattern instantiates
- [ADR-039: Tool-Call Governance via Rules](../adr/039-rule-driven-tool-governance.md) — the per-role tool-allowlist primitive
- [ADR-041: Unified Evaluator + Role Compression](../adr/041-unified-evaluator.md) — the `when`-clause guard mechanism used in transition rules
- [ADR-045: Graph Search Subloop](../adr/045-graph-search-subloop.md) — the first reference instance (R0-R6)
- [Concepts: Orchestration Layers](14-orchestration-layers.md) — rules vs components vs workflow, the foundational discipline
- [Concepts: KV Twofer](02-kv-twofer.md) — the wire-format substrate that makes graph-triple routing work
- [Concepts: Tool-Result Hints and Pagination](24-tool-result-hints-and-pagination.md) — the structured-signaling pattern for tool results that compose with the persona-prose decision-criteria discipline
