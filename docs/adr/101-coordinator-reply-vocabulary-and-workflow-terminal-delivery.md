# ADR-101: Coordinator reply vocabulary and workflow terminal delivery

## Status

**Proposed — unsigned architect draft (2026-08-26), pending owner ruling on #1094.** Records a cross-repo
contract; the mechanics live in `openspec/specs/agentic-terminal-events`, `agentic-loop`, and `agentic-tools`
via change `workflow-terminal-delivery`.

## Context

A product submits a root task on a channel; its coordinator hands off to a rule chain with
`decide(action="autoresearch")`; a later, rule-spawned coordinator answers with
`decide(action="respond_direct", reason=…)`. At beta.161 the framework delivers the handoff and drops the answer
(`processor/agentic-dispatch/terminal_settlement.go:192-196`), because delivery is keyed on "which loop owns a route",
and the `decide` tool is vocabulary-agnostic by design (ADR-026, `processor/agentic-tools/decide.go:162-181`) so
the framework cannot tell an answer from a handoff.

Two facts must be held somewhere: the route a workflow's answer belongs to, and which decision is an answer. The
first is already durable on the run root's loop record in `AGENT_LOOPS` together with the ancestry to reach it
(`agentic/state.go:56-59,81-84`). The second has no framework home; the only framework-owned decide action today is
the synthesized `needs_clarification` (`processor/agentic-loop/graph_writer.go:181`).

## Decision

1. **Two decide actions are a framework contract.** `respond_direct` and `ask_user` are reserved decide actions
   with user-facing semantics: `respond_direct` is delivered as a `result`, `ask_user` as a `prompt`, each carrying
   the decision `reason`. Every other decide action is a handoff and is never delivered to a user channel. The
   `decide` tool description stays vocabulary-agnostic; products enumerate actions in persona prose as before, and
   `restricted_decide_actions` may bar either reserved name (an autonomous mode bars `ask_user`).
2. **"Terminal" is a property of the decision, observed by the loop — never a rule-declared step.** agentic-loop
   carries the typed decision of a `decide` terminal on the completion event. Neither `publish_agent` nor any rule
   field, metadata key (`wakeup_mode`), or run-lifecycle transition selects the user-facing terminal.
3. **Origin correlation is observed, not carried.** Dispatch resolves a route-less user-facing decision's channel by
   walking the persisted loop ancestry (`ParentLoopID`, then `RunID`) in `AGENT_LOOPS` to the nearest routed
   ancestor. No new field rides `TaskMessage`, no run-entity predicate is added, and no second durable authority is
   created. The 24h `AGENT_LOOPS` key TTL is the documented horizon of this resolution.

## Consequences

- Products migrate their reply actions to the two reserved names if they differ (semteams already uses them). A
  routed front-door coordinator that ends in a non-reply decision stops receiving that decision as `result`.
- The typed decision is an additive `LoopCompletedEvent` field; `Result` is unchanged for `read_loop_result`.
- ADR-053 D3 stands: the run's lifecycle phase remains product-declared; this ADR does not infer run completion.
- Rejected alternatives: carrying the origin on the `AgentRun` entity (second home for a bucket fact; unsolvable
  root-handoff ordering), a rule-side terminal marker (author prediction), copying channel fields onto every spawn
  (leaks every internal phase).

## References

- #1094, #354, #1090; ADR-026, ADR-028, ADR-049, ADR-053.
- `docs/proposals/gh1094-workflow-terminal-delivery-design.md` — inventory and design handoff.
