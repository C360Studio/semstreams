# Design: workflow-terminal-delivery

## Accepted evidence and owner decisions

- Inventory checkpoint: `INVENTORY PASS WITH DIVERGENCES` (independent blind review of revision 1, PR #1098 at
  `01b0f37f`); every divergence corrected in revision 2 (design §I.6). Body SHA-256 of the accepted revision of
  `docs/proposals/gh1094-workflow-terminal-delivery-design.md` to be recorded here by the caller.
- Owner acceptance of the design: PENDING. The nine owner items in the design §II.9 are recorded below as they
  are ruled; until ruled, the recommended default is what the tasks implement.

| Owner item | Recommended default | Ruling |
|---|---|---|
| 1. Home of the reply vocabulary | framework-reserved `respond_direct`, `ask_user` (ADR-101 draft) | UNRULED |
| 2. Synthesized decisions (never a tool result; `graph_writer.go:182`) — keep `Decision` nil, so a text-only routed coordinator still delivers raw `result`? | yes, keep | UNRULED |
| 3. Routed handoff decision | publish nothing (`handoff_settled`) | UNRULED |
| 4. Any in-run `ask_user` is user-facing; cancelled lane unchanged (no route/Decision on `LoopCancelledEvent`) | yes → `prompt` to origin; cancel unchanged | UNRULED |
| 5. Internal-phase failure | silent (unchanged) | UNRULED |
| 6. Fold #1090 reason rendering | yes, `Content = Decision.Reason` for reply decisions | UNRULED |
| 7. Normalisation of reserved names | exact match | UNRULED |
| 8. `origin_unresolvable` disposition | settle route-less, ACK | UNRULED |
| 9. Milestone | owner's | UNRULED |
| 10. Declared `agent_loops` read port in this change | yes (R8) | UNRULED |
| 11. Ancestry walker plane: AGENT_LOOPS walk vs `agentrun.ResolveRun` graph walk + one root read | AGENT_LOOPS walk | UNRULED |

## D1 — Typed decision is observed by the loop

The loop sets `LoopCompletedEvent.Decision` from the `decide` tool result's metadata when
`GetToolName(toolResult.CallID) == agentic.DecideToolName`. This is NEW plumbing: `handleCompleteResponse`
receives only `toolResult.Content` today (`processor/agentic-loop/handlers.go:2166`), and the only present knowledge
of a decide terminal is the post-hoc trajectory scan (`:1919-1927`); the tool result must be threaded in. Synthesized
decisions are graph triples written after completion (`graph_writer.go:182`) and never populate `Decision`.
Dispatch never infers a decision from `Result` JSON shape.

## D2 — One classifier, one tool-name home

`agentic.IsUserFacingDecideAction` is the only interpreter of the reply vocabulary; `agentic.DecideToolName`
replaces the three `"decide"` literals (`decide.go:71`, `handlers.go:1921,1935`).

## D3 — Selection by decision, not by route ownership

See spec delta `agentic-terminal-events` "Workflow terminal selection SHALL follow the typed decision".

## D4 — Origin by ancestry in AGENT_LOOPS

See spec delta "Origin route resolution SHALL observe persisted ancestry". The walk reuses `loadPersistedLoop`
and its existing transient/permanent classification; the only new dispositions are `handoff_settled` and
`origin_unresolvable`. Walk end at a record with neither link and no route (route-less root, or severed by a
non-loop-entity trigger) is `route_less_settled` — there was no origin; `origin_unresolvable` is reserved for a walk
that cannot complete (absent key — expired or never persisted —, cycle, hop bound) — the origin could not be observed.

## D6 — Bucket name observed from a declared port

Dispatch declares `{Name: "agent_loops", Config: KVReadPort{Bucket: "AGENT_LOOPS"}}` as agentic-tools does
(`processor/agentic-tools/config.go:134`) and resolves the bucket from the port in `loadPersistedLoop` and the
`/activity` reader; the constant at `http_activity.go:20` is removed.

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
