# ADR-028: Agentic Orchestration Architecture — Rule Skeleton + Coordinator + Ops

## Status

Proposed. Layer 1 (data-flow substrate + retry plumbing) is built. Layers 3
and 4 (coordinator agent, ops agent) are named here and deferred to refreshed
ADR-026 and ADR-027 respectively. This ADR supersedes the implicit assumption
in earlier rule-engine work that pure-rule orchestration is sufficient for
real-world agentic flows; it does not yet supersede ADR-026 or ADR-027, but
flags both as needing a refresh with the clarifications below.

## Context

The rule engine in `processor/rule/` fires actions on metadata triples. It's
good at mechanical routing: "when agent X completes with role=researcher,
spawn a synthesizer." It is not good at — and should not try to be — reading
unstructured agent output and deciding whether the work was good enough,
what to do next, or whether the flow should change shape on the fly.

Three recent pain points crystallised the architectural question:

1. **Small-model schema adherence.** Semspec's production experience is that
   requiring agents to submit structured JSON via `submit_work` breaks on
   small models. Retries help but don't eliminate the pain; every role is a
   schema-adherence failure point.

2. **Content-carrying in rules.** A brief experiment adding an
   `agent.loop.result` triple — so rules could substitute an agent's output
   into a downstream prompt — silently truncated any result over 16KB and
   violated the codebase's metadata-only-triples convention
   (`agentic/trajectory_entity.go:30-32`). For dev flows producing code
   artifacts, truncation would have corrupted downstream inputs without
   notice.

3. **Stochastic output + small context windows.** An agent's output is
   probabilistic. Deterministic rules cannot route on it reliably unless the
   output is structured — and structured output is exactly what small models
   can't deliver. Injecting full content into downstream prompts explodes
   context windows on those same small models.

Comparable frameworks: OpenClaw (fastest GitHub stars in agentic coordination
as of this writing) is a local-first gateway with config-driven routing and
per-agent workspaces — no supervisor agent, no ops agent. Its sister project
Clawe adds Kanban task coordination with cron heartbeats and a watcher
service but still no judgment agent. LangGraph's supervisor pattern and
AutoGen's GroupChatManager are the closest analogues to the architecture
below. What's described here — rule skeleton + coordinator + ops — is the
architectural moat for semstreams/semteams, not table stakes.

## Decision

Commit to a three-layer agentic orchestration architecture:

### Layer 1 — Data-flow substrate (this commit)

- Rules carry **references** (IDs, paths, storage refs), never content.
- Bulky content lives in durable stores: `COMPLETE_{loopID}` in AGENT_LOOPS
  KV, the `agent.complete.*` JetStream stream, ObjectStore for very large
  payloads via the existing `ContentStorable` pattern.
- Agents retrieve on demand via tools: `read_loop_result(loop_id, max_bytes,
  offset)` shipped with this ADR, artifact tools planned for dev flows.
- Small-context-window friendly: the agent chooses what to load.

### Layer 2 — Rule skeleton (already built)

- Rules fire on metadata triples: `agent.loop.role`, `agent.loop.outcome`,
  `research.has_evidence`, etc.
- Rules do mechanical routing: spawn next agent, write KV, emit triples,
  enforce cooldowns and retry budgets at the flow level.
- Rules do **not** parse agent output, make quality judgments, or branch
  on the semantic content of a result.
- The rule engine reliability fixes that landed in this working period
  (per-rule feedback, bootstrap recovery, `SourceRevision` dedup, TTL
  sweep, spawned-task triples) are the foundation for this layer.

### Layer 3 — Coordinator agent (deferred; refreshes ADR-026)

- A dedicated agent role invoked by rules at judgment points.
- Reads upstream agent output via `read_loop_result` (Layer 1), reasons
  over it, and emits a **structured terminal decision** via a small
  schema-backed tool.
- Contains the schema-discipline problem to **one role** — the coordinator
  — instead of requiring every agent to hit a schema. The coordinator is
  naturally the role you'd run on a stronger model, which makes structured
  output tractable.
- Can manipulate rules and flows at runtime via tool executors (flow
  compose/update), making the coordinator the primary control surface for
  dynamic behaviour — not rules.
- ADR-026 needs a refresh to frame this precisely: the coordinator is not
  merely a "dynamic flow composer"; it is the judgment layer of the
  orchestration architecture and the home of schema adherence.

### Layer 4 — Ops agent (deferred; refreshes ADR-027)

- Watches completed loops, trajectories, and rule-fire telemetry for
  failure patterns: recurring tool errors, iteration-budget overruns,
  rules that consistently fire-then-reverse, coordinator decisions that
  correlate with downstream failures.
- Proposes prompt, rule, schema, and retry-policy refinements via the
  same runtime tooling the coordinator uses.
- Closes the improvement loop: rules are plumbing, the coordinator is
  judgment, ops is learning.
- ADR-027 needs a refresh to frame this as a telemetry-consumer + flow
  editor, not a separate orchestration primitive.

## Implications for rule authors

- **Rules carry references, not content.** The prompts in your rule
  actions may substitute `$entity.id` and metadata triples but should
  never substitute free-text content predicates. If you find yourself
  wanting to do that, either (a) move the decision to a coordinator
  that reads the content via `read_loop_result` or (b) fetch it via a
  tool in the downstream agent's prompt.
- **Rules do not parse agent output.** If a rule condition needs to
  branch on the content of a result ("did the researcher find
  subtopics?"), that's a judgment call. The rule should trigger a
  coordinator; the coordinator's terminal tool result emits a triple
  (e.g., `coordinator.next_action = fan_out`) that a subsequent rule
  can match on deterministically.
- **Tool retries live in config.** For tools where transient failures
  (timeout, external 5xx) are worth auto-retrying at the framework
  layer, declare a `tool_retries` entry in the agentic-tools component
  config. Validation-shaped errors (invalid_args, not_found) still
  route back through the agent's iteration loop — those need LLM
  feedback, not blind retry.

## What's built in this layer 1 commit

- `read_loop_result` tool — `processor/agentic-tools/loop_result.go`.
  Fetches a completed loop's full Result from AGENT_LOOPS KV with paging.
- Opt-in retry policy — `processor/agentic-tools/config.go` (`ToolRetries`
  + `RetryPolicy`), applied in `component.go` `executeWithTimeout`.
  Metrics: `retries_total`, `retries_exhausted_total`.
- Deep-research rule updates — rules 01/02/04/05 carry references, not
  content; rule 03 (subtopic fan-out) disabled because it requires a
  judgment call that belongs to the coordinator.
- This ADR.
- `CLAUDE.md` subsection pointing at this ADR.

## What's not built here

- **Coordinator agent** (Layer 3). Follow-up plan, refreshes ADR-026.
- **Ops agent** (Layer 4). Follow-up plan, refreshes ADR-027.
- **Artifact store** for dev flows — named workspace + `write_artifact`
  / `read_artifact` / `list_artifacts` tools. Follow-up plan. Layer 1's
  read_loop_result handles single-result retrieval; artifact store
  generalises it to multi-file dev outputs.
- **Rule 03 re-enable** — depends on coordinator emitting a
  `coordinator.next_action = fan_out` triple for rule 03 to match on.

## Consequences

**Positive:**

- Deep-research e2e becomes viable with references-only rules plus
  `read_loop_result`. The chain works on small models because content
  is pulled on demand, not pushed.
- Schema-discipline retries are available at the tool boundary without
  every tool reinventing the pattern. Coordinator's future decide()
  tool will be the first real consumer.
- Architectural clarity: rules don't pretend to reason, coordinator
  doesn't pretend to be infrastructure, ops doesn't pretend to
  orchestrate.

**Negative:**

- Flows depending on rule-branching over content must wait for the
  coordinator to land. Rule 03 is the first visible example.
- Coordinator and ops are meaningful engineering commitments. This ADR
  signs up for their follow-ups.
- A rule author who wants "simple" without introducing a coordinator
  will find some flows genuinely aren't expressible without one.
  That's a feature, not a bug — it prevents us from silently accreting
  fragile text-marker rules that "work" until the model drifts.

## Related decisions

- ADR-025 — semteams consolidation, which upstreamed the personalization
  layer this architecture sits on top of.
- ADR-026 — coordinator agent (proposed, pending refresh with this
  framing).
- ADR-027 — ops agent (proposed, pending refresh with this framing).
- `processor/rule/` reliability work (this session) — prerequisite for
  Layer 2 being something we can rely on.
