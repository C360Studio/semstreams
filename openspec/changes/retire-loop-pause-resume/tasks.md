# Tasks

## 1. Correct the drifted clause

- [x] 1.1 Restate the `One control-signal payload travels the loop signal subject` requirement so the pause/resume
  clause describes the verbs as removed rather than as unread, keeping the heading and all four scenarios verbatim
- [x] 1.2 Verify this delta strands no `// spec:` citation: the restated requirement heading is byte-identical,
  so the six citations pointing at it still resolve. NOTE: `task spec:properties` exits 1 overall on three
  failures that are **pre-existing on `main`** and in a different capability (`agentic-loop`) — see PR #1257,
  which fixes them. Nothing here introduced or masks them.
- [x] 1.3 Verify the delta validates — `openspec validate retire-loop-pause-resume --strict`
- [x] 1.4 Bind the new normative clause to a scenario and named tests, matching the four scenarios beside it
