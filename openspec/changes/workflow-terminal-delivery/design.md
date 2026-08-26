# Design: workflow-terminal-delivery

## Accepted evidence and owner decisions

- Inventory checkpoint: `INVENTORY PASS` — PENDING. Body SHA-256 of
  `docs/proposals/gh1094-workflow-terminal-delivery-design.md` to be recorded here by the caller.
- Owner acceptance of the design: PENDING. The nine owner items in the design §II.9 are recorded below as they
  are ruled; until ruled, the recommended default is what the tasks implement.

| Owner item | Recommended default | Ruling |
|---|---|---|
| 1. Home of the reply vocabulary | framework-reserved `respond_direct`, `ask_user` (ADR-101 draft) | UNRULED |
| 2. `needs_clarification` user-facing? | no | UNRULED |
| 3. Routed handoff decision | publish nothing (`handoff_settled`) | UNRULED |
| 4. Any in-run `ask_user` is user-facing | yes → `prompt` to origin | UNRULED |
| 5. Internal-phase failure | silent (unchanged) | UNRULED |
| 6. Fold #1090 reason rendering | yes, `Content = Decision.Reason` for reply decisions | UNRULED |
| 7. Normalisation of reserved names | exact match | UNRULED |
| 8. `origin_unresolvable` disposition | settle route-less, ACK | UNRULED |
| 9. Milestone | owner's | UNRULED |

## D1 — Typed decision is observed by the loop

The loop sets `LoopCompletedEvent.Decision` from the `decide` tool result's metadata when
`GetToolName(toolResult.CallID) == agentic.DecideToolName` (`processor/agentic-loop/handlers.go:2164,2241`).
Dispatch never infers a decision from `Result` JSON shape.

## D2 — One classifier, one tool-name home

`agentic.IsUserFacingDecideAction` is the only interpreter of the reply vocabulary; `agentic.DecideToolName`
replaces the three `"decide"` literals (`decide.go:71`, `handlers.go:1921,1935`).

## D3 — Selection by decision, not by route ownership

See spec delta `agentic-terminal-events` "Workflow terminal selection SHALL follow the typed decision".

## D4 — Origin by ancestry in AGENT_LOOPS

See spec delta "Origin route resolution SHALL observe persisted ancestry". The walk reuses `loadPersistedLoop`
and its existing transient/permanent classification; the only new dispositions are `handoff_settled` and
`origin_unresolvable`.

## D5 — Identity and retention unchanged

`terminal-user-response:<source id>`; PubAck before ACK; `MaxDeliver=0`; at-least-once within USER duplicate
window; AGENT 24h; plus the AGENT_LOOPS 24h origin horizon.

## Adopter seam

A product that submits over HTTP and chains with `publish_agent` must know: the two reserved reply action names.
If it does nothing and already uses them (SemTeams), delivery is correct with no configuration. If it uses other
names for its answers, its answers settle `handoff_settled` — observable as a metric reason and a Warn naming the
loop id and action. The rule author never marks a terminal.

## Boundaries

In: agentic types, loop completion event, decide constant home, terminal normalizer projection, dispatch
settlement, schema regeneration, docs named in tasks. Out: `publish_agent`, AgentRun, lifecycle, progress
signals, #1090's non-decision rendering, e2e chain scenario (filed as a coverage gap).
