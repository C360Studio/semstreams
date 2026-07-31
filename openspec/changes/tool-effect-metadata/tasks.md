# Tasks — tool-effect-metadata (gh#749)

**Task-line discipline:** amend a task line when the work HAPPENS, not only when it
succeeds. A gate that ran and found nothing is recorded as run; a gate that was
skipped is recorded as skipped, with why. A line predicting a gap costs as much as
a line predicting success.

**Residency:** every task here is completable from this repo. Sister adoption is
not a task — it lives in the adopter note and gh#753.

## 1. Canonical enum and field (agentic/tools.go)

- [x] 1.1 Add `type ToolEffect string` with `ToolEffectUnknown = "unknown"`, `ToolEffectReadOnly = "read_only"`, `ToolEffectMutating = "mutating"`, `ToolEffectExternal = "external_effect"`. Doc the worst-effect semantics, `unknown` as no-claim (treat at least as restrictive as `external_effect`), and the open-for-extension clause (no exhaustive switch without a default-to-unknown arm).
- [x] 1.2 Doc comment on `ToolEffectReadOnly` cross-references `agentic.FilesystemPolicyReadOnly` (`agentic/exec_policy.go:90`) and states the scope difference explicitly: a tool may be `external_effect` while executing under filesystem policy `read_only` — one classifies world-effect, the other worktree write-scope.
- [x] 1.3 Add `Effect ToolEffect \`json:"effect,omitempty"\`` to `ToolDefinition`.
- [x] 1.4 Add `func (e ToolEffect) Known() bool` — reports whether the value names a declared member. Empty is NOT known (it is undeclared, which registration treats as legal and resolution treats as `unknown`); document that asymmetry at the call sites, not by overloading `Known`.
- [x] 1.5 Add `func (e ToolEffect) Canonical() ToolEffect` — empty or unrecognized → `ToolEffectUnknown`; a declared member returns itself.
- [x] 1.6 Extend `ToolDefinition.Validate()` with the same non-empty-must-be-known check so the method stays truthful. Do NOT wire `Validate()` into `RegisterExecutor` — it also requires non-empty `Parameters`, which registration does not today; that widening is deferred (task 7.2).

## 2. Registration refusal and aggregation normalization (processor/agentic-tools/executor.go)

- [x] 2.1 In `RegisterExecutor`'s existing validate-then-commit FIRST pass, alongside the empty-`Name` check: reject a definition whose `Effect` is non-empty and not `Known()`. Error names the tool and the offending value. No entries committed on failure (the two-pass roll-back shape already guarantees this — assert it in the test, do not assume).
- [x] 2.2 In `ExecutorRegistry.ListTools()`, set `def.Effect = def.Effect.Canonical()` on the returned copy before appending. Must not mutate the producing executor's own definition — the loop already copies by value; add a comment pinning why that matters.
- [x] 2.3 Confirm by reading the code that `ListTools()` re-invokes `executor.ListTools()` live on every call and that `RegisterTool` never inspects a `ToolDefinition` — these are the two reasons 2.2 is load-bearing rather than belt-and-suspenders. Cite both in the doc comment.

## 3. Discovery projection (processor/agentic-tools/{external,component}.go)

- [x] 3.1 Add `Effect string \`json:"effect"\`` to `agentictools.ToolDefinition` — NO `omitempty`, so `"unknown"` is served explicitly.
- [x] 3.2 Populate it in both projection branches of `Component.ListTools()` (global and local) via `Canonical()`. Both branches, not just one — they are separate literal constructions.
- [x] 3.3 Structural decision test (`processor/agentic-tools/external_test.go`): reflect over `agentic.ToolDefinition`'s field names; assert each appears in exactly one of two explicit lists — projected {Name, Description, Effect} or deliberately-dropped {Parameters, Strict, Paginated}. Fail with a message telling the author to make the projection decision explicitly. Record in the test's doc comment WHY each dropped field is dropped (Parameters: payload weight, no discovery consumer; Strict: provider-wire concern; Paginated: loop concern).
- [x] 3.4 Mutation-check 3.3: add a scratch field to `agentic.ToolDefinition`, confirm the test FAILS, remove it. Read the output — a build failure is not a pass. **→ RAN: probe field added, guard failed with the intended assertion message (not a build error); field removed, green restored.**

## 4. Classify every in-repo producer

- [x] 4.1 Enumerate every `ListTools() []agentic.ToolDefinition` implementation under `processor/agentic-tools/` and `processor/agentic-tools/executors/` (scrape it; do not hand-maintain the list) and record the count in the PR. **→ 22 executors, 44 tool definitions; enforced by an AST scrape (`effect_classification_test.go`) rather than a hand-maintained list.**
- [x] 4.2 Classify each tool by worst-effect. Expected shape: `query_*`/`search_graph`/`summarize_graph`/`read_loop_result`/`component_catalog`/`flow_monitor`/`get_*`/`list_*`/`web_search` → `read_only`; rule and flow CRUD (`create_*`/`update_*`/`delete_*`/`deploy_*`/`start_*`/`stop_*`/`instantiate_*`), `scratchpad`, `write_todos`, `decide`, `emit_lesson`, `emit_diagnosis`, persona CRUD → `mutating`; `bash`, `http_request` → `external_effect`. Verify each against what the executor actually does — the table is the claim, the code is the evidence.
- [ ] 4.3 Include the full classification table in the PR body for reviewer scrutiny. Disputes resolve by the worst-effect rule.
- [x] 4.4 `RecordingExecutor.ListTools()` delegates to the wrapped executor — confirm it passes `Effect` through unchanged (it returns the wrapped slice directly; assert in a test rather than by inspection alone, since a wrapping executor that reconstructed definitions would silently drop the field).

## 5. Acceptance tests (issue-named, mapped to seams)

- [x] 5.1 **Reject unknown enum values** → `RegisterExecutor` test driving the production registry: invalid effect fails registration; no tool names committed; empty effect registers fine.
- [x] 5.2 **JSON round-trip per value** → round-trip `TaskMessage` carrying `Tools[].Effect` through the PRODUCTION decoder (payload registry / `message.NewDecoder`), not an anonymous `json.Unmarshal` cast. Cover all four values plus absent → `""` → `Canonical()` = `unknown`.
- [x] 5.3 **Registration → discovery preservation** → executor → `RegisterExecutor` → `registry.ListTools()` → `Component.ListTools()` → `tool.list` response body; assert the value survives each hop for a classified tool.
- [x] 5.4 **Absent resolves to fail-safe** → both paths: an undeclared producer, and an unrecognized value arriving on a wire-decoded `TaskMessage`. Assert `unknown` and explicitly assert NOT `read_only`.
- [x] 5.5 **Enforcement not weakened (effect-blindness)** → a `read_only` tool named in `ApprovalRequired` still gates; an `external_effect` tool not named does not gate; `FilterToolCalls` and the advertised-tools admission path (`agentic.AdvertisedToolsFromMetadata` consumer) produce identical outcomes across all four values and undeclared. Both directions — proving the field changes nothing IS this increment's contract.

## 6. Decision record and adopter guidance

- [x] 6.1 One-page ADR at the next free number in `docs/adr/`: the four values, worst-effect semantics, fail-safe-unknown, descriptive-not-enforcement boundary, open-for-extension clause, and the does-not-cross-the-provider-wire non-goal. Decision only — mechanics live in the seeded spec.
- [x] 6.2 Adopter note for the two gh#749 consumers: the field, the resolved discovery value, the "never switch exhaustively" rule, and the explicit statement that adopting it changes no enforcement.

## 7. Deferrals — file BEFORE merge, by name

- [x] 7.1 File "agentic-tools: effect-derived default approval policy" — a config knob compiling down to the existing `ApprovalFilter` name set at boot, NOT a second runtime gate. Record the binding constraint: the authoritative value is the registry's definition read at the dispatch seam, never the copy in `TaskMessage.Tools` / `ToolCall`, else a crafted task downgrades a declared effect. Note the registration-order wrinkle (filter built before all executors register) as the reason it is deferred. **→ filed as gh#808 (2026-07-31), 7.2 folded into it.**
- [x] 7.2 Fold into 7.1 (or file separately): wiring `Validate()` into `RegisterExecutor`, which would broaden the boot gate to also require non-empty `Parameters` — a separate breaking-ish decision.
- [x] 7.3 Per-argument / dynamic effect classification is out of scope permanently unless a real consumer demands it; worst-effect semantics is the answer to argument-dependence. Record in the ADR's non-goals, not as an issue.

## 8. Gates and wrap-up

- [x] 8.1 `gofmt`, `task lint` (revive warnings = CI fail), `go vet ./...` plain AND `-tags=integration` AND `-tags=live_llm`, all under `-mod=readonly`. **→ RAN, all clean.**
- [ ] 8.2 BOTH suites CI runs: `go test -race ./...` AND `go test -race -tags=integration -p 2 -count=1 ./...`. Grep `^FAIL` — pipeline exit codes report the tail stage.
- [x] 8.3 `task schema:generate` + `git diff schemas/ specs/` clean (expected: no drift — `ToolDefinition` appears in no committed schema; verify rather than assume). **→ RAN: zero drift, prediction held.**
- [ ] 8.4 `go test ./test/contract/...`.
- [x] 8.5 **E2E coverage gate**: the catalog change is operator-visible. Extend the agentic tier with a `tool.list` assertion (a known tool's response carries an explicit non-empty `effect`) if the harness surfaces `tool.list` cheaply; if it does not, file the named coverage-gap issue BEFORE merge. No hand-wave. **→ STAGE SHIPPED, not deferred: `verify-tool-effect-catalog` in the crud-tools tier — the scenario already holds a NATS client, so a `tool.list` request/reply was cheap. Uses `RequestClassified` (gh#337 guard caught the raw-`Request` first draft).**
- [ ] 8.6 `task e2e:agentic` green before merge — it exercises the tools+loop path this touches. Take a `docker info` latency reading first; attribute a container failure to substrate before code. Not BREAKING (purely additive), so the breaking-change e2e hard rule is not triggered — this is the coverage-gate run.
- [ ] 8.7 `semstreams-reviewer` pass on the full diff.
- [ ] 8.8 Owner-run Codex round; fix findings; arm `--auto` only AFTER it closes (the ruleset enforces neither approvals nor stale-review dismissal).
- [ ] 8.9 Owner CONFIRM-CLOSE before closing gh#749.
- [ ] 8.10 Archive: seed `openspec/specs/agentic-tools/` with a WRITTEN Purpose (not the `TBD - created by archiving` stub) plus an explicit statement of what it does NOT cover.
