# Design: workflow-terminal-delivery

## Accepted evidence and owner decisions

- Inventory checkpoint: `INVENTORY PASS WITH DIVERGENCES` (independent blind review of revision 1, PR #1098 at
  `01b0f37f`); every divergence corrected in revision 2 (design §I.6). Body SHA-256 of the accepted revision of
  `docs/proposals/gh1094-workflow-terminal-delivery-design.md` to be recorded here by the caller.
- Owner ruling 2026-08-26 (owner-run Codex round on PR #1098, recorded on the issue): items 1–7 and 9–10 accepted
  as recommended; 8 accepted conditionally (C2); 11 — AGENT_LOOPS plane accepted, traversal corrected (C1, R4′);
  binding corrections C1–C4 folded in revision 3. Acceptance of the revision as a whole: PENDING.

| Owner item | Recommended default | Ruling |
|---|---|---|
| 1. Home of the reply vocabulary | framework-reserved `respond_direct`, `ask_user` (ADR-101 draft) | ACCEPTED |
| 2. Synthesized decisions (never a tool result; `graph_writer.go:182`) — keep `Decision` nil, so a text-only routed coordinator still delivers raw `result`? | yes, keep | ACCEPTED |
| 3. Routed handoff decision | publish nothing (`handoff_settled`) | ACCEPTED |
| 4. Any in-run `ask_user` is user-facing; cancelled lane unchanged (no route/Decision on `LoopCancelledEvent`) | yes → `prompt` to origin; cancel unchanged | ACCEPTED |
| 5. Internal-phase failure | silent (unchanged) | ACCEPTED |
| 6. Fold #1090 reason rendering | yes, `Content = Decision.Reason` for reply decisions | ACCEPTED |
| 7. Normalisation of reserved names | exact match | ACCEPTED |
| 8. `origin_unresolvable` disposition | settle route-less, ACK | ACCEPTED CONDITIONALLY — only after parent chain AND `RunID` are exhausted; distinct from `route_less_settled`; exhaustion order in requirement text and log reason (C2) |
| 9. Milestone | owner's | ACCEPTED |
| 10. Declared `agent_loops` read port in this change | yes (R8) | ACCEPTED |
| 11. Ancestry walker plane: AGENT_LOOPS walk vs `agentrun.ResolveRun` graph walk + one root read | AGENT_LOOPS walk | PLANE ACCEPTED; r2 traversal NOT — typed-first `RunID`, parent walk for unthreaded chains, `RunID` fallback at a missing parent (C1, R4′) |

## D1 — Typed decision is observed by the loop

The loop sets `LoopCompletedEvent.Decision` from the `decide` tool result's metadata when
`GetToolName(toolResult.CallID) == agentic.DecideToolName`. This is NEW plumbing: `handleCompleteResponse`
receives only `toolResult.Content` today (`processor/agentic-loop/handlers.go:2166`), and the only present knowledge
of a decide terminal is the post-hoc trajectory scan (`:1919-1927`); the tool result must be threaded in. The tool is
identified through the loop's existing name-fallback chain — `GetToolName(callID)`, then `toolResult.Name`
(`handlers.go:2241-2245`; synth path `:1370-1378`; agentic-tools stamps `Name` before publishing,
`agentic-tools/component.go:680`, `:710-711`) — so a restart or cache loss does not demote a decide terminal (C3). A
present `Decision` with empty `Action` or `Reason` fails `LoopCompletedEvent.Validate()` (`events.go:89-97`) and is
Termed by the normalizer (`agentterminal/terminal.go:114`); unknown non-empty actions remain valid handoffs (C4).
Synthesized decisions are graph triples written after completion (`graph_writer.go:182`) and never populate
`Decision`. Dispatch never infers a decision from `Result` JSON shape.

## D2 — One classifier, one tool-name home

`agentic.IsUserFacingDecideAction` is the only interpreter of the reply vocabulary; `agentic.DecideToolName`
replaces the three `"decide"` literals (`decide.go:71`, `handlers.go:1921,1935`).

## D3 — Selection by decision, not by route ownership

See spec delta `agentic-terminal-events` "Workflow terminal selection SHALL follow the typed decision".

## D4 — Origin by ancestry in AGENT_LOOPS (R4′, owner-corrected)

See spec delta "Origin route resolution SHALL observe persisted ancestry". Order, mirroring `agentrun.ResolveRun`
(`agentrun.go:284-296`): (1) typed-first — the terminal's `RunID` names the run root's record; routed → origin;
present but route-less → walk parents from the root; absent → walk parents from the terminal; (2) parent walk to
the nearest routed ancestor; at an ABSENT parent key the current record's not-yet-tried `RunID` is consulted before
anything settles; (3) 32 hops, visited set. `origin_unresolvable` is recorded only after the parent chain AND every
encountered run anchor are exhausted, or on a cycle/bound, with the Warn `origin_unresolvable: parent chain ended at
absent <loopID>; run anchor <RunID> absent | none`. A walk end (no `ParentLoopID`, no untried `RunID`, no route) is
`route_less_settled` — there was no origin. The walk reuses `loadPersistedLoop` and its transient/permanent
classification.

## D5 — Identity and retention unchanged

`terminal-user-response:<source id>`; PubAck before ACK; `MaxDeliver=0`; at-least-once within USER duplicate
window as clamped to the USER MaxAge (`config/stream_drift.go:276-283`); AGENT 24h; plus the AGENT_LOOPS 24h
and best-effort-persistence origin horizon.

## Adopter seam

A product that submits over HTTP and chains with `publish_agent` must know: the two reserved reply action names.
If it does nothing and already uses them (SemTeams), delivery is correct with no configuration. If it uses other
names for its answers, its answers settle `handoff_settled` — observable as a metric reason and a Warn naming the
loop id and action. The rule author never marks a terminal. An operator running a non-default loops bucket binds
the same name on dispatch's `agent_loops` port instead of discovering, after the fact, that a constant ignored it.

## Boundaries

In: agentic types, loop completion event, decide constant home, terminal normalizer projection, dispatch
settlement, schema regeneration, docs named in tasks. Out: `publish_agent`, AgentRun, lifecycle, progress
signals, #1090's non-decision rendering, e2e chain scenario (filed as a coverage gap).
