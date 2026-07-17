## 1. Failing Tests First

- [x] 1.1 TaskMessage: table tests for max_iterations nil/1/valid/0/negative (Validate rejects < 1), JSON
      round-trip of the pointer field, and clamp-to-component-ceiling at loop creation
- [x] 1.2 publish_agent: loop_max_iterations literal + substituted-from-triple + non-integer substitution
      fails the action with a classified error and rejection metric/log (no silent skip)
- [x] 1.3 Exhaustion reason: both paths — tools-in-flight at cap AND model-response-at-cap — publish
      failure reason "max_iterations"; the sentinel maps via errors.Is, not string matching

## 2. Implementation

- [x] 2.1 Add `MaxIterations *int` to TaskMessage (omitempty) + Validate ≥ 1; plumb into CreateLoop/
      CreateLoopWithID with min(spawn, component) clamp
- [x] 2.2 Add `loop_max_iterations` to the publish_agent action config + substitution + validation; document
      the near-miss distinction from the firing-cap field in the action schema description
- [x] 2.3 Typed sentinel (e.g. ErrMaxIterationsReached) from the model-response guard; failure-handler maps
      to reason "max_iterations"; keep the tool-drain path's reason unchanged
- [x] 2.4 `task schema:generate` — commit regenerated schema; operator JSON round-trip test covers the new
      action field

## 3. Gates

- [x] 3.1 `task lint`, `go test -race ./...`, `go test ./test/contract/...`, schema drift clean
- [x] 3.2 `go test -race -tags=integration -p 2 ./processor/agentic-loop/... ./processor/rule/...`
- [ ] 3.3 `task e2e:agentic` green (loop budget exercised end-to-end) — deferred to supervisor per task brief
- [ ] 3.4 Changelog entry naming the guard-path reason change; close gh#528/gh#529 with fix references —
      deferred to supervisor per task brief
