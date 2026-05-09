# Agent-private observable state — operations guide

This guide is the operator-facing companion to
[ADR-036](../adr/036-agent-private-observable-state.md). It covers
when to enable `write_todos` for a role, how to phrase the persona
fragment that teaches the model when to use it, and what to look for
in the graph when diagnosing whether a deployment uses the primitive
well.

## Quick read

The framework registers `write_todos` for every role by default. The
tool writes structured working memory onto the calling loop's entity
that survives context compaction. Personas decide when to reach for
it; the discipline is descriptive (in the persona prompt), not
prescriptive (no rule, no validator gates content).

## When the tool earns its keep

ADR-036 Appendix A documents the threshold in detail. Short version:

| Signal | Use `write_todos`? |
|---|---|
| 2+ steps the model tracks across iterations | Yes |
| Iteration cap is generous (>5) and work spans iterations | Yes |
| Cross-loop coordination — parent waits on spawned children | Yes |
| Single-step lookup or one-shot synthesis | No |
| Coordinator that finishes in 1–2 iterations (chat front door) | No |
| Tiny-model deployments with already-busy tool surface | Persona-level opt-out |

Three runtime properties of semstreams shift the threshold downward
relative to Claude Code's `TodoWrite`:

- Compaction is more aggressive on small-model deployments.
- Multi-iteration loops with hard caps are the norm — `max_iterations
  = 30` on the dev-via-spec builder is typical, not exceptional.
- Cross-loop handoffs via `publish_agent` are async; the parent
  terminates and gets re-invoked when children complete, so todos
  that track in-flight delegations stay coherent across that gap.

## Persona fragment templates

Persona authors copy and adapt one of these paragraphs into the
role's persona file. They are descriptive — they tell the model what
kind of work benefits, not what format the todos must take. ADR-036
§Decision Rule 2 is the canonical statement: prescriptive format
demands ("you must produce ≥3 todos with this exact shape")
recreate a known LLM-on-LLM Goodhart failure mode where the
producer optimises for ceremony rather than substance, so the
templates below ARE descriptive ("if your work has X, do Y") rather
than prescriptive.

### Coordinator-shape (short loop, may delegate)

```markdown
## Tracking in-flight delegations

If you spawn parallel specialists you'll wait on, record each
delegation as a todo item with the spawned `loop_id` so the next
iteration of yourself (after specialists complete) knows what was
in flight. For pure classify-and-delegate flows that finish in one
or two iterations, you do not need todos — write them only when
your work spans iterations.
```

### Planner / multi-step builder

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

### Researcher / reviewer (read-heavy, may chase leads)

```markdown
## Tracking lines of inquiry

If your work fans out across multiple sources, citations, or
hypotheses you intend to circle back on, list them as todos. This
keeps the trail visible if your loop iterates further than one
turn and survives any compaction the loop hits. For a single
look-up or a one-shot synthesis, skip the tool.
```

## How the prompt assembler shows todos to the model

Every iteration after the first call to `write_todos`, the agent's
system message prefix carries a compact block right after the
iteration-budget warning:

```text
[Iteration Budget] Iteration 4 of 30 (13% used).

[Working list — your private working memory; you maintain this via write_todos]
[x] Survey existing rules
[~] Draft new rule
[ ] Wire e2e test
```

Status markers:

- `[x]` — completed
- `[~]` — in_progress
- `[ ]` — pending
- `[?]` — unrecognised (defensive — Stage 3 validates to the canonical
  enum, so `[?]` only appears if a future tool/persona writes outside
  the enum)

## What rules and ops can match on (and what they can't)

`agent.todo.id`, `agent.todo.status`, `agent.todo.position`,
`agent.todo.updated_at` are rule-matchable. Useful predicates:

- `agent.todo.status = "in_progress"` — find loops with active work.
- `agent.todo.updated_at < now-30m AND agent.todo.status =
  "in_progress"` — wedge detector.
- Count of `agent.todo.status = "completed"` — completion ratio.

`agent.todo.content` is **rule-opaque**. The vocabulary registry
flags it `RuleOpaque: true`; the rule-validator rejects any rule
whose `condition.field` names this predicate. This is the structural
mechanism that keeps content out of the rule-engine's branching
surface — see ADR-036 §Decision Rule 1 for why.

If you find yourself wanting a rule that reads todo content, you
need a coordinator agent in that path instead. Rules don't make
quality judgments over unstructured text (`CLAUDE.md` lines
161-170).

## Per-deployment opt-out

For small-model deployments where every tool slot competes for the
model's attention, persona authors can opt the role out of
`write_todos`:

```json
{
  "role": "researcher",
  "default_tools": ["graph_query", "web_search", "submit_work"]
}
```

`write_todos` is registered globally but only surfaces to a role's
calls when included in the role's tool allowlist (or omitted from
the persona's exclusion list, depending on your deployment's tool
discovery shape). The framework default is "available unless
configured out."

This is a deployment-tuning decision, not a framework-level safety
boundary — opacity is per-loop, not per-role. See the ADR-036
discussion of why role-gating buys no Goodhart safety.

## Observability

| Signal | Where | What it tells you |
|---|---|---|
| `agent.todo.*` triples on `agent.execution.*` entities | graph | Current and historical working lists |
| `[Working list...]` system message in trajectory | loop trajectory | What the model saw on a given iteration |
| Iteration count vs `agent.todo.status` distribution | graph queries | Are roles using the primitive? |
| `agent.todo.updated_at` lag | graph queries | Are agents updating status promptly, or batching at the end? |

The ops agent (ADR-027 Phase 1) is the natural consumer of these
signals. After ~2 weeks of deployment, file an ops-agent diagnosis
asking the four questions in ADR-036 Appendix A: per-role todo-call
frequency, correlation with loop outcome, compaction-survival
behaviour, and status-marker discipline. Use what surfaces to
revise the threshold and templates.

## Failure modes

- **Tool call fails mid-write** (e.g. graph-ingest unreachable
  between the 5 RemoveByPredicate calls and the batch add): the loop
  entity is in a half-cleared state. Next call to `write_todos`
  with the same args is idempotent — empty predicates are no-op
  removes, then the batch add commits. Convergence in two calls.
- **Read fails on iteration build**: the model just doesn't see the
  working-list block this iteration. Next iteration retries. The
  per-iteration read budget caps at 2s — the LLM call's latency is
  unaffected.
- **Model emits invalid status enum**: rejected pre-CAS as
  `ToolErrorInvalidArgs` with the canonical enum names in the error
  message. The model can self-correct without retrying through the
  loop's outer retry policy.

## Related ADRs

- [ADR-036](../adr/036-agent-private-observable-state.md) — the
  principle and the discipline rules.
- [ADR-027](../adr/027-ops-agent-meta-harness.md) — the read-side
  consumer of these signals.
- [ADR-028](../adr/028-orchestration-architecture.md) — the
  rules-don't-carry-content firewall this primitive extends.
- [ADR-035](../adr/035-strict-tool-calling.md) — `write_todos`
  ships with `Strict: true` so providers that honour
  `function.strict` constrain status to the enum.
