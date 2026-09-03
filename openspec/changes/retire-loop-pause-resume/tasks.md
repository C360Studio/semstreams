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
- [x] 2.5 Record the ruling-to-file conformance table in `proposal.md`, covering all nine Tier 1 removals and
  the semsage obligation. The owner's follow-up ruling explicitly authorizes `LoopEntity.StateBeforePause`
  removal while retaining `LoopStatePaused`
