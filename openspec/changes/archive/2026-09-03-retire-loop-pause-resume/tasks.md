# Tasks

## 1. Correct the drifted clause

- [x] 1.1 Restate the `One control-signal payload travels the loop signal subject` requirement so the pause/resume
  clause describes the verbs as removed rather than as unread, keeping the heading and all four scenarios verbatim
- [x] 1.2 Verify this delta strands no `// spec:` citation: the restated requirement heading is byte-identical,
  so the six citations pointing at it still resolve. NOTE: `task spec:properties` exits non-zero on this branch —
  **3 of 49 citations unresolved**, all in a different capability (`agentic-loop`) and none introduced here.
  PR #1257 fixed exactly those three, and it is on `main` at `5b7c3db3` — but **this branch is nested under
  PR #1159 by owner ruling (2026-09-02)**, and that stack's foundation is `461b6902`, one commit behind
  `5b7c3db3`. So the three cannot go green here: they resolve when the #759 → #1146 → #1239 stack reaches `main`.
  The three are named and unchanged by this branch — `processor/agentic-loop/create_vs_exists_fence_test.go:411`
  and `:492` cite *Creating a loop that already exists is refused; a continuation attaches to it*;
  `terminal_release_test.go:437` cites *Per-loop in-process state is released at terminal, through the one release
  point*.
  Re-run `task spec:properties` once the stack lands and expect zero unresolved. This is a base-selection
  consequence, not an unexplained exception.
- [x] 1.3 Verify the delta validates — `openspec validate retire-loop-pause-resume --strict`
- [x] 1.4 Bind the new normative clause to a scenario and named tests, matching the four scenarios beside it

## 2. Retire the surviving advertisements (Codex round 2)

- [x] 2.1 `docs/concepts/13-agentic-systems.md` — the Signal Types diagram still listed `approve`, `reject` and
  `retry` promising `complete`/`failed`/`exploring`. Reduced to `cancel`, with approval redirected to
  `ApprovalResponse` on `agent.approval_response.*` (ADR-039)
- [x] 2.2 `processor/agentic-loop/README.md:35` — the feature line still read "Cancel and approval signals";
  pause/resume had been dropped from it and "approval" left standing. Now names `cancel` as the whole vocabulary
- [x] 2.3 `processor/agentic-dispatch/intent_classifier.go:24` — the `IntentSignal` godoc still read
  "(approve, reject, etc.)". Found by an independent per-verb sweep, not named in the review
- [x] 2.4 `processor/agentic-dispatch/intent_classifier_test.go:52` — the `extractJSON` fixture carried
  `signal_type: approve`; the payload is arbitrary to that test, so it now spells `cancel`
- [x] 2.5 Record the ruling-to-file conformance table in `proposal.md`, covering all ten Tier 1 removals and
  the semsage obligation. R3 authorized `LoopEntity.StateBeforePause` removal while retaining `LoopStatePaused`;
  R4 (2026-09-03) superseded that retention and the table now carries `LoopStatePaused` as the tenth removal
- [x] 2.6 Remove `LoopStatePaused` under R4: delete the constant and its `isValidLoopState` entry, validate
  `LoopEntity.TransitionTo`'s argument against the vocabulary, refuse an invalid persisted state in
  `processor/agentic-dispatch`'s loop reader under its existing permanent classification, and strip the state
  from the three documentation tables (`agentic/README.md`, `docs/concepts/13-agentic-systems.md`,
  `processor/agentic-loop/README.md`), the ASCII state diagram at `docs/concepts/13-agentic-systems.md:102`, the
  godoc list, the OpenAPI query description and the migration note.
  Proved by `TestPausedIsRefusedAtTheExportedTransition`, `TestUndefinedStatesAreRefusedAtTheExportedTransition`,
  `TestPausedIsRefusedEvenWhenTheEntityAlreadyHoldsIt`, `TestPersistedPausedRecordFailsValidation`,
  `TestTransitionLoopRefusesPaused` and `TestIntegrationPersistedInvalidStateIsPermanent`

## 3. Review round 2 (2026-09-19, PR #1339)

- [x] 3.1 The reader validates the WHOLE persisted entity, not only its state. The round-1 narrowing to a
  state-only check named `research-graph-route`/`-execute` as writers of the records this reader loads; verified
  and false. Every AGENT_LOOPS writer but `persistLoopState` (`processor/agentic-loop/component.go:2032`) uses a
  PREFIXED key — `COMPLETE_<id>` (`component.go:1951,1978,2003`), `research.request.received.<id>`
  (`frameworkcapabilities/graphresearch/register_tool.go:101`), `classify./route./execute.<id>`
  (`research-graph-route/adapters.go:81-84`, `research-graph-execute/adapters.go:366-368`) — while the reader
  Gets the bare loop id and rejects an id mismatch. `NewLoopEntity` floors `max_iterations` at 20
  (`agentic/state.go:263-267`), so no production record can fail validation on that field; the records the wider
  check refused were this reader's own test fixtures, now repaired to a full loop shape as #1329 repaired its own
- [x] 3.2 `agentic.LoopState.IsValid` deleted with the narrowing that motivated it. Zero consumers remained, and
  a phantom export on a Tier 1 package owes an ADR-106 RC-6 walked path for surface nobody calls
- [x] 3.3 The archived `agentic-dispatch` delta gained a `## MODIFIED Requirements` block for *Loop existence and
  ownership are merged facts, never process memory alone*. The applied spec had gained that requirement's new
  normative paragraph with no delta behind it — current truth the record could not reconstruct. All five existing
  scenarios are restated verbatim and a sixth names `TestIntegrationPersistedInvalidStateIsPermanent`. Dated
  amendment record in `proposal.md`
- [x] 3.4 `agentic/user_types.go:53-58,352` no longer advertises "a paused run" or "the RunID it held from the
  pause state". ADR-053 lists pause/resume under Deferred (`docs/adr/053-agent-run-substrate.md:280`); the
  mechanism is a run that ended awaiting a reply, and only the vocabulary was the deleted one
- [x] 3.5 The semteams migration row is re-measured case-insensitively for `paus`, not by quoted literal: the
  literal sweep missed `AgentLoopCard.svelte:73-74`'s `.state-badge.paused` CSS rule and
  `TaskDetailPanel.svelte:268`'s branch, and named `agentChatBridge` — which has zero hits outside its test
- [x] 3.6 `TestIntegrationInvalidPersistedRecordIsToleratedOnlyBecauseTheTrackerAnswers` pins the combination
  nothing covered: a tracker hit plus a permanently defective durable record. The request is admitted from the
  tracker, the refused record contributes no facts, and the tolerated WARN carries the permanent cause
- [ ] 3.7 **Deleted by #1329, not here.** `processor/agentic-dispatch/loop_tracker.go:572-579`'s `isTerminalState`
  duplicates `agentic.LoopState.IsTerminal` (`agentic/state.go:42-44`) as raw string literals, read by
  `trackerLoopFacts` and `persistedLoopFacts` (`loop_admission.go:385`). #1329 deletes the tracker outright, so
  migrating it here would be work thrown away; recorded rather than edited
- [x] 3.8 The OpenAPI loop-state filter enumeration completed: it listed six of the ten values a caller can pass,
  omitting `exploring`, `planning`, `architecting` and `reviewing`. Pre-existing, fixed on the line this change
  already rewrote (`processor/agentic-dispatch/http.go:1039` → `specs/openapi.v3.yaml:339`)
