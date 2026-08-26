# Change: Deliver the user-facing terminal of rule-spawned workflows

## Why

`agentic-dispatch` delivers a loop's result only when that loop owns a channel route
(`processor/agentic-dispatch/terminal_settlement.go:192-196`). A `publish_agent`-spawned loop owns no route
(`processor/rule/actions.go:1504-1511`), so a workflow's answer — the final wake-up coordinator's
`decide(action="respond_direct")` — settles `route_less_settled`, while the root coordinator's handoff
`decide(action="autoresearch")` is delivered as `result` because the root owns the HTTP route. SemTeams hit this in
its beta.161 adoption after removing its flat `user.response.>` writers (#1094).

The accepted design will be recorded in `docs/proposals/gh1094-workflow-terminal-delivery-design.md`
(body SHA-256 recorded in `design.md` once the owner accepts).

## What changes

- agentic-loop carries a typed `Decision{Action, Reason}` on `LoopCompletedEvent` when, and only when, the loop's
  terminal `StopLoop` tool was `decide` (observed through the tracked tool name, `handlers.go:2241`). `Result` is
  unchanged.
- `respond_direct` and `ask_user` become framework-reserved decide actions with user-facing semantics, classified
  by one function in `agentic`; the `"decide"` tool-name literal gets one home (`agentic.DecideToolName`).
- Dispatch selects the user-facing terminal by the typed decision: a reply decision publishes (`respond_direct` →
  `result`, `ask_user` → `prompt`, content = reason); a handoff decision publishes nothing (`handoff_settled`); a
  terminal without a decision keeps today's behaviour.
- Dispatch resolves a route-less reply decision's origin from persisted `AGENT_LOOPS` records: typed-first through
  the terminal's `RunID` (the run root), then by walking `ParentLoopID` for unthreaded chains, bounded at 32 hops; a
  missing parent lookup falls back to `RunID` before anything settles (mirroring `agentrun.ResolveRun`).
- The loop identifies the `decide` terminal through its existing tool-name fallback chain (tracked name, then
  `ToolResult.Name`), and `LoopCompletedEvent.Validate` rejects a present `Decision` with an empty `Action` or
  `Reason`, so a malformed decision is Termed by the fail-closed normalizer rather than classified as a handoff.
- Response identity, PubAck-before-ACK, `MaxDeliver=0`, and the bounded at-least-once declaration are unchanged.
- A walk that ends at a record with neither link and no route (a route-less bus-submitted root, or a hop severed by
  a non-loop-entity trigger) settles `route_less_settled`; `origin_unresolvable` is recorded only after the parent
  chain AND every encountered run anchor are exhausted (absent key), or on a cycle or the hop bound, and its log
  reason names what was exhausted.
- Dispatch declares an `agent_loops` KV read port (mirroring agentic-tools) and resolves the bucket from it in the
  settlement and `/activity` readers, replacing the hardcoded constant.
- `publish_agent` is unchanged; a guard test pins that spawned tasks carry no channel fields.

## Bounded guarantee

Origin resolution reads `AGENT_LOOPS`, whose keys expire 24h after their last write
(`processor/agentic-loop/component.go:761-766`) and whose writes are best-effort (`:1985-1987`). A workflow whose
routed ancestor record is not observable settles `origin_unresolvable`; no delivery is guaranteed past that horizon,
matching the existing AGENT 24h source posture. A workflow whose root never had a route, or whose ancestry was
severed by a non-loop-entity trigger, settles `route_less_settled` — the two are indistinguishable from the bucket.
Deduplication remains bounded by the USER `duplicates` window as clamped to the USER MaxAge
(`config/stream_drift.go:276-283`); this change claims at most one response identity per terminal, not exactly-once
delivery.

## Impact

- Modified capabilities: `agentic-terminal-events`, `agentic-loop`, `agentic-tools`.
- Runtime surfaces: `agentic` (types, one classifier), `processor/agentic-loop` (completion event; new plumbing
  from the terminal tool result into completion),
  `processor/agentic-tools` (constant home), `internal/agentterminal` (projection), `processor/agentic-dispatch`
  (settlement, declared `agent_loops` read port), `component` (`PortFacts.KVReadBucket`, added at implementation
  time — see below).
- **Two implementation-time corrections to this Impact list, both measured, neither ruled at design time:**
  1. `schemas/agentic-loop.v1.json` and `agentic-dispatch.v1.json` are NOT regenerated: `task schema:generate`
     produces no diff for either surface. The generated component schemas carry the config shape and the generic
     port-kind `oneOf`, never payload fields or declared port instances (`grep -n LoopCompletedEvent
     specs/openapi.v3.yaml` → 0; `grep -c "agent.complete" schemas/agentic-dispatch.v1.json` → 0 at the baseline).
     The wire contract is pinned by a production-decoder round-trip test instead (tasks 2.5, 3.5, gate 6.4).
  2. Two surfaces the design never named had to be touched for R8's new port token: `component/port_facts.go`
     gains `PortFacts.KVReadBucket` (new exported framework surface — the port grammar forbids any consumer
     outside the canonical projection owners from interpreting a port config), and
     `internal/portgrammarcontrol/target_test.go` gains the named census amendment plus its exactness test. Both
     were found by CI, not by the design (task 3.7).
- Consumers: SemTeams (autoresearch and research chains; #266, #267 downstream); every product using
  `decide` + `agentic-dispatch`.
- Behavioural change to name in the release note: a routed loop whose terminal is a non-reply decide action no
  longer receives that decision as `result`.
- Required evidence: focused `-race` tests, one real-NATS restart/redelivery proof, `task e2e:agentic`.

## Non-goals

- No new payload type, subject, stream, bucket, graph predicate, outbox, or adopter knob.
- No change to `publish_agent`, `run_scope`, or the AgentRun participant; ADR-053 D3 terminal authority stands.
- No exactly-once or post-eviction delivery guarantee.
- No progress/status responses for internal phase completions or failures (owner items #3/#5 in the design).
- No rendering change for terminals without a typed decision (#1090's non-decision shapes stay with #1090).
- No SAP-normalised comparison of the reserved names (owner item #7).
