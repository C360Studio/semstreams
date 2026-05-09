# ADR-036: Agent-Private Observable State

## Status

**Proposed — 2026-05-08.** Generalises a primitive that has been
implicit in [ADR-027](027-ops-agent-meta-harness.md) (ops agent reads
loop telemetry) and [ADR-028](028-orchestration-architecture.md)
(rules carry references, not content) but has never been named or
disciplined. First concrete instance is a TodoWrite-shaped scratchpad
available to any agent role; the principle scales to any future
per-loop working memory.

## Context

Long-horizon agent flows in semstreams (`processor/agentic-loop`,
the dev-via-spec chains in semteams, semspec's plan→requirements→
scenarios→review pipeline) reconstruct their plan from prose every
turn. The trajectory is the only durable record of "where am I in
this task," and trajectory gets compacted (beta.21 length-truncation
handler, alpha.90 age-based GC retirement). After compaction, the
agent's plan exists only in compressed paraphrase. Coordinators
re-derive next-action from this each iteration; planner agents
re-emit step lists every turn; reviewers re-parse plans from
markdown.

Three forces meet here:

1. **Compaction erases plans.** Anything held only in chat history
   is at risk. Structured state held outside the chat history
   survives, and can be re-injected at prompt assembly time
   (`processor/agentic-loop/prompt/assembler.go`).

2. **Rules can match structured facts but not prose.** CLAUDE.md
   commits hard to "rules can't make quality judgments over
   unstructured text — that's coordinator work." A predicate like
   `agent.todo.status = pending` is exactly the structural surface
   the rule engine *can* branch on; a predicate like
   "todo content mentions X" is exactly what it cannot.

3. **LLM-on-LLM checklist contracts Goodhart.** semteams's
   `feedback_format_compliance_goodhart.md` documented the failure
   mode: when one LLM grades another against a format checklist,
   the producer optimises conformance away from substance. The
   chain converged on ceremony. This is a real risk for any
   structured agent state primitive that becomes a contract between
   roles.

What's missing is a principle that says *what kind of structured
agent state is safe* — the absence has meant projects either avoid
the primitive entirely (today) or risk recreating the semteams
checklist failure mode if they introduce one.

## Decision

Commit to the pattern of **agent-private observable state**:
structured working memory held on an entity owned by the agent that
produces it, with asymmetric access discipline.

### Access asymmetry

| Role | Access | Stake |
|---|---|---|
| Owning agent (writer of this loop's state) | read-write | uses state as personal working memory |
| Any reader of the graph (humans, ops agent, debug UI, downstream rules) | read-only | observes; does not interpret content |
| Authorised actuator agents (per ADR-026 deploy surface) | indirect — may write to *other* entities (configs, prompts, rules) on the basis of what was observed | learns from patterns across many loops, never edits a live loop's private state |

The writer is the **sole writer** and the **sole interpreter of
content**. Other parties may read structural facts (existence,
counts, status enums, timestamps, transitions). They may not
predicate on content, and they may not write back into the
owner's state mid-loop.

### Three discipline rules

These keep the asymmetry intact. Drop any one and Goodhart returns.

**Rule 1 — Rules predicate on structural facts only.**
The rule engine may match `agent.todo.status_count.pending = 0`,
`agent.todo.last_transition > 30s ago`, or
`agent.todo.completion_ratio >= 1.0`. It may not match
`agent.todo.content contains "X"` or otherwise branch on what the
agent wrote in free-form fields. The moment a rule reads content,
content becomes a contract and the agent will optimise to satisfy
the rule rather than reflect actual progress.

**Rule 2 — Persona prompts describe state descriptively, not
prescriptively.**
"Maintain a working list for yourself — what you plan to do, where
you are" is descriptive. "You must produce ≥3 todos with format
`### Goal / ### Steps / ### Checks`" is prescriptive and recreates
the semteams checklist Goodhart. The persona surfaces the tool;
the agent decides what to put in it.

**Rule 3 — Authorised feedback flows via ADR-026's deploy surface,
never via direct mutation of live private state.**
Ops agent (ADR-027) and any future authorised actuator agent may
read agent-private state across many completed loops, diagnose
patterns, and propose changes through ADR-026's runtime composition
tools — `create_rule`, `manage_flow`, persona edits, model
selection. Those changes are reviewed and approval-gated per
ADR-026's safety model and ADR-027's phased delivery. They flow
back through *configuration*, not by writing into the owner's
todos or scratchpad. Human review and authorised-agent action are
both downstream of the same diagnosis surface; the two consumers
share the gradient discipline.

This third rule preserves the asymmetry under a non-trivial
condition: the gradient from observation to behaviour change is
slow (config-time, not per-token sampling), visible (governance and
approval per ADR-026), and indirect (through configuration entities,
not through the live owner's state). That is what makes it
categorically different from a per-decision LLM-on-LLM checklist.

## First instance — `write_todos`

The principle's first concrete realisation is a TodoWrite-shaped
tool available to any agent role. The Goodhart discipline is
asymmetric per loop, not per role: every agent is the sole writer
of its own todos and the sole interpreter of its own content. No
cross-role contract forms regardless of which role calls the
tool, so role-gating buys no safety. Long-horizon work shows up
in many roles — coordinator decisions, planner step-emission,
developer test-fix cycles, researcher citation chasing,
reviewer pass-criteria walking — and all benefit equally from
externalised plan state surviving compaction.

### Tool definition

```go
// In processor/agentic-tools/executors/register_write_todos.go
const ToolNameWriteTodos = "write_todos"

// Argument shape:
//   {
//     "todos": [
//       {"id": "1", "content": "Survey existing rules", "status": "completed"},
//       {"id": "2", "content": "Draft new rule", "status": "in_progress"},
//       {"id": "3", "content": "Wire e2e test", "status": "pending"}
//     ]
//   }
```

The executor is a passthrough: it validates the argument shape,
writes one triple per todo onto the loop entity, and returns
`ToolResult.Content` with a compact summary. Following the decide
tool pattern (`processor/agentic-tools/decide.go`), validation
errors surface as `ToolErrorInvalidArgs` with the canonical schema
in the message so the LLM can self-correct.

`StopLoop=false` — unlike `decide`, this tool is meant to be called
many times in a single loop. It is working memory, not a terminal.

### Predicates

Add to `vocabulary/agentic/predicates.go` under a new `Todo`
constant block:

```go
const (
    TodoID         = "agent.todo.id"          // string
    TodoContent    = "agent.todo.content"     // string — opaque to rules
    TodoStatus     = "agent.todo.status"      // enum: pending|in_progress|completed
    TodoPosition   = "agent.todo.position"    // int — order within list
    TodoUpdatedAt  = "agent.todo.updated_at"  // timestamp
)
```

`TodoContent` is registered with metadata flagging it as
**rule-opaque**: rule validators reject any rule that predicates on
this field. (Implementation: extend `processor/rule/config_validation.go`
with a `RuleOpaquePredicates` denylist sourced from vocabulary
metadata. Same machinery serves any future content-bearing
predicate that follows this pattern.)

### Compaction survival

`processor/agentic-loop/prompt/assembler.go` reads the current todo
list from the loop entity at every prompt build and injects it
into the system message as a structured block. Trajectory may be
compacted; the todo state is reconstructed from triples each turn.
This is the load-bearing reason for the primitive — without it,
the value of the tool collapses to a pretty version of "write notes
in your reply."

### Goodhart cross-checks

Following `feedback_format_compliance_goodhart.md`'s lesson, add
one structural cross-check that catches the obvious failure mode:
a todo marked `completed` should plausibly correspond to evidence
in the loop's trajectory or graph. The implementation is a
periodic ops-agent diagnosis (Phase 1 territory), not a hard
runtime gate. Hard runtime gating would itself be a contract and
recreates the failure mode.

## Future candidates

The principle generalises beyond todos. Anything with the shape
"the agent wants to externalise working memory; observers want to
diagnose patterns; the agent should be the sole interpreter" is
a candidate:

| Future primitive | Use | Owner |
|---|---|---|
| `agent.hypothesis.*` | Researcher tracks hypotheses under test | Researcher |
| `agent.scratch.*` | Free-form notes across iterations | Any role |
| `agent.breadcrumb.*` | Exploration trail (visited states / tried approaches) | Coordinator, debugger |
| `agent.budget.*` | Self-imposed limits (iterations remaining, tokens spent) | Any role |

Each would follow the same pattern: a tool that writes triples to
the loop entity, content fields flagged rule-opaque, persona
surface descriptive not prescriptive, ops agent observes
post-hoc, authorised actuators feed back through configuration.

## Consequences

**Positive.**
- Long-horizon flows survive compaction without losing plan state.
- Rules gain a deterministic predicate surface for plan progress
  (status counts, transitions, durations) without violating the
  ADR-028 content/rule firewall.
- Ops agent (ADR-027 Phase 1) gains a richer observation substrate;
  Phase 2/3 authorised actuators get the same surface without
  needing privileged access to live loops.
- One discipline pattern serves all future agent-private state
  primitives — no per-feature reasoning about Goodhart risk.

**Negative.**
- Per-deployment tool-surface tuning. The framework default
  registers `write_todos` for all roles. Operators running
  small-model deployments where every tool slot competes for
  attention may still want to opt specific roles out via the
  persona's tool set — but this is a deployment-tuning decision,
  not a framework-level safety boundary. semteams smoke #7
  (decide-tool action allowlist drift, beta.40/41/44) is the
  reference case for "small models drown when the tool surface
  widens"; persona-level opt-out remains the right lever.
- Rule-validator complexity. Enforcing the rule-opaque predicate
  list at config-load is new machinery (a few dozen LOC, but it
  needs tests). Trade-off worth taking because the same machinery
  protects every future content-bearing predicate.
- Persona-prompt discipline is convention-enforced, not
  framework-enforced. A persona author can still write "you must
  produce ≥3 todos." Mitigation: review checklist for new
  personas, with the rule called out explicitly. (Memorialise in
  CLAUDE.md alongside the existing "rules carry references" rule.)

**Neutral.**
- `write_todos` does not interact with parent/child loop
  spawning. Each loop owns its own todo state; child loops do not
  inherit the parent's todos. Cross-loop plan handoff (parent
  decomposes; children solve items) routes through the existing
  rule-mediated `publish_agent` path with `RelatedLoops` lineage
  (beta.50 PR #33, beta.51 PR #36). The two primitives compose
  cleanly without coupling.

## Relationship to other ADRs

- **ADR-027** (ops agent): this ADR formalises the read-side
  contract Phase 1 ops already operates under, and disciplines
  the feedback flow Phase 2/3 will introduce.
- **ADR-028** (orchestration architecture): the rule-opaque
  predicate convention is the structural extension of ADR-028's
  "rules carry references not content" principle to agent-owned
  state.
- **ADR-026** (coordinator runtime composition): authorised
  feedback flows from ops observations land here, not as direct
  mutations of agent-private state. Same approval gates apply.
- **ADR-035 / ADR-034** (strict tool calling, response format):
  `write_todos` should ship with `Strict: true` on its
  `ToolDefinition`. The argument schema is small and structural —
  a perfect candidate for sampling-constrained tool args.

## Appendix A — Trigger pattern (when to call `write_todos`)

The framework provides the tool; personas teach the model when to
reach for it. This appendix sets a starting threshold and three
fragment templates. It is intentionally a starting point: the ops
agent (ADR-027 Phase 1) will diagnose whether deployed personas
under-use or over-use the primitive, and the threshold should evolve
with that empirical signal.

### Baseline — Claude Code's published heuristic

The reference pattern Claude Code surfaces to its agents (and that
its TodoWrite tool documentation describes):

- Use it when work has **3+ distinct steps** the model will need to
  track across multiple tool calls.
- Use it for **multi-file changes** where the model benefits from
  visible per-file progress.
- Use it when **dependencies between subtasks** matter — B requires
  A's output, and the model might forget partway through.
- **Do not** use it for single-step lookups, trivial fixes, one-shot
  Q&A, or tasks that complete in one tool call.
- **Mark items completed immediately** after the underlying work
  finishes — never batch status updates at the end. (This is what
  makes the list a faithful record rather than a post-hoc summary.)

This is the right baseline to copy because it's been pressure-tested
against millions of real agent-loop sessions, but three runtime
properties shift the threshold downward for semstreams.

### Three properties that shift the threshold

**1. Compaction is more aggressive than Claude Code's default.**
Small-model deployments (qwen3:0.6b classifier, 1.7b mid-tier in
the seminstruct topology beta.51 split) hit context pressure faster
than Claude Code's typical Sonnet/Opus configurations. The
agentic-loop's `processor/agentic-loop/prompt/assembler.go` rebuilds
the prompt every iteration; anything not held in durable state
outside the chat history is at risk on the next compaction. The
threshold for "worth tracking in a todo list" should be **2+ steps
that span a compaction-eligible iteration boundary**, not Claude
Code's 3+ rule.

**2. Multi-iteration loops are the norm, with hard iteration caps.**
The dev-via-spec builder runs `max_iterations = 30`
(`configs/personas/fragments/dev-via-spec-builder/10-bash-iteration-contract.md`,
calibrated 8 → 30 per smoke #6 evidence). Researcher and reviewer
loops also commonly exceed Claude Code's typical few-iteration
envelope. Any role with `max_iterations > ~5` is a candidate.

**3. Cross-loop handoff is observable and matters.**
Claude Code's Task tool returns synchronously; semstreams's
parent-child via `publish_agent` does not — the parent terminates
and gets re-invoked when the child completes. A coordinator's todos
that track "spawned researcher for X (loop_id=...); awaiting
reviewer for Y" stay coherent across that gap in a way Claude
Code's pattern doesn't need to.

### Empirical evidence — Exhibit A: dev-via-spec builder Step 2

The builder fragment's `10-bash-iteration-contract.md` already
encodes a planning step:

> **Step 2 — plan locally, then execute.** Inside the prose part
> of your message (not in bash), think about what files you need to
> produce and in what order. Do not turn this into a multi-paragraph
> design exercise — the dev-via-spec chain already designed. State
> your build plan in 3–6 bullets, then start writing.

That's exactly the shape `write_todos` formalises. Today the plan
lives only in chat-history prose and dies on compaction; with the
tool, the same 3–6 bullets become structured state on the loop
entity, observable to the architect on retry, queryable by ops
agent for "did builders that wrote ≥3 todos finish more reliably
than those who didn't?" diagnoses, and re-injected by the prompt
assembler every iteration.

This is the canonical case. Other personas with implicit
"state-your-plan-then-execute" guidance (architect's output
contract, reviewer's evaluation walk-through) are next-priority
adopters.

### Counter-case — Claude Code coordinator (chat front door)

The semteams coordinator's `00-identity.md`:

> Your loop is short. One-to-two iterations per user message in the
> common case: classify, delegate, done.

A two-iteration classify-and-delegate flow does not need
`write_todos`. A four-iteration follow-up flow tracking parallel
specialist invocations *does*. The trigger language must
distinguish between "my work is short" and "my work has spawned
work I'm waiting on" — the latter is where the coordinator's todo
list earns its keep, listing each in-flight delegation by `loop_id`.

### Three fragment templates

Persona authors copy and adapt these. They are descriptive
(per Rule 2 from the §Decision section), not prescriptive — they
tell the model what kind of work benefits, not what format the
todos must take.

**Template — coordinator-shape (short loop, may delegate).**

```markdown
## Tracking in-flight delegations

If you spawn parallel specialists you'll wait on, record each
delegation as a todo item with the spawned `loop_id` so the next
iteration of yourself (after specialists complete) knows what was
in flight. For pure classify-and-delegate flows that finish in one
or two iterations, you do not need todos — write them only when
your work spans iterations.
```

**Template — planner-shape / multi-step builder.**

```markdown
## Tracking your plan

When you take on work that has 2+ distinct steps you'll need to
track across iterations — especially if the work crosses a
compaction-eligible boundary or involves multiple files,
dependencies, or test-fix cycles — call `write_todos` near the
start of your loop with the plan as a list. Update each item's
status as you complete it (don't batch at the end). For
single-step work or one-shot tool calls, skip todos entirely.
```

**Template — researcher / reviewer (read-heavy, may chase leads).**

```markdown
## Tracking lines of inquiry

If your work fans out across multiple sources, citations, or
hypotheses you intend to circle back on, list them as todos. This
keeps the trail visible if your loop iterates further than one
turn and survives any compaction the loop hits. For a single
look-up or a one-shot synthesis, skip the tool.
```

### Tool-surface budget — the per-deployment lever

The builder persona's `00-identity.md` is explicit:

> small models degrade with tool sprawl, and bash is the most
> heavily-trained-on tool surface.

For roles like the dev-via-spec builder where the toolset is
deliberately kept at 4 tools, persona authors deploying small
models may opt out of `write_todos` and instead keep the
existing prose-plan guidance. This is a per-deployment tuning
decision (see §Consequences), not a framework default. The
framework registers the tool; the persona decides whether to
expose it.

### Iteration plan post-launch

Land `write_todos` with the three templates above as the canonical
starting point. After the tool has been deployed for ~2 weeks
across the existing journey fixtures (dev-via-spec, deep-research,
research-iterative, ops-agent-baseline), have the ops agent
diagnose:

- Per-role todo-call frequency (calls per loop, distribution).
- Correlation between todo presence and loop outcome (`agent.loop.outcome`).
- Compaction events on loops with vs. without active todos —
  did plan state survive?
- Status-marker discipline — are agents marking items completed
  in the same iteration the work happened, or batching at the end?

Revise the threshold and templates based on what surfaces. The
trigger pattern is empirical, not theoretical, and ADR-027's
infrastructure exists precisely to drive this kind of revision.
