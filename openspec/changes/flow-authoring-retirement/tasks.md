# Tasks — flow-authoring-retirement

**Amend a task line when the work HAPPENS, not only when it succeeds.** A gate that ran and was never recorded is
indistinguishable from one that was skipped. A `[~]` is a recorded decision and MUST also be noted in the spec delta.
No task here asserts a post-merge fact; the merge gate owns CI.

Word discipline: `scripts/openspec-queue.sh` reads the words hold / blocked / blocking / halt / red / failed / failing
in any OPEN task line as a live caveat. Everywhere else say "MUST fail", "does not compile", "abort", "barrier".

These tasks were carried over from `composition-validation-substrate` (PR #1101, #1092), where they were annotated
`(#1093)` and never performed: that PR shipped the substrate and this one performs the retirement it makes possible.
Premises were measured at `5cc0c7fb` and are re-measured at the claim head; the substrate PR moved lines in
`service/component_manager.go`, `service/component_manager_http.go`, `component/flowgraph/flowgraph.go`, and
`processor/agentic-tools/executors/`, so re-measure those before citing them.

## 1. Claim

- [x] 1.1 Claim #1093: dedicated worktree on an agent-prefixed branch off `origin/main` AFTER PR #1101 merges (this
      change's deletions rest on the substrate that PR lands, and its 2.5 engine-parity test is deleted here); draft PR
      with `Closes #1093` in the body; this change directory's claim tick is its first commit.
- [x] 1.2 Re-measure the premises: the removal list in the proposal against the claim head, and
      `grep -rn "flowstore\|flowtemplate\|flowengine\|flow-builder\|flowbuilder" --include='*.go' --include='*.json'
      --include='*.yml' --include='*.md' .` (main tree, `docs/adr` and `openspec/changes/archive` excluded) — record the
      starting count here so 3.2's closing count is a measurement and not an assertion.

      Claim head `78fe095c`. Worktree `../semstreams-wt/claude/gh1093-flow-authoring-retirement`, branch
      `claude/gh1093-flow-authoring-retirement`, PR #1116 (draft, `Closes #1093`).
      Command (run from the worktree root):
      `grep -rn "flowstore\|flowtemplate\|flowengine\|flow-builder\|flowbuilder" --include='*.go' --include='*.json'
      --include='*.yml' --include='*.md' .`
      Starting counts — raw **752**; with the task's two exclusions (`docs/adr/`, `openspec/changes/archive/`) **504**;
      live tree only (additionally excluding `docs/proposals/` and this change's own directory) **361**.
      Premise note for 3.2: the task's "→ 0" cannot hold for the two-exclusion filter, because `docs/proposals/*`
      (retired inventories/designs, 116 hits at claim) and this change's own artifacts are history that records the
      removed surface by name. 3.2 records the live-tree count and enumerates the residue by class instead of
      asserting a zero the filter cannot reach.

## 2. Baseline capture — write the removal guards first

- [x] 2.1 Removal guards (carried from `composition-validation-substrate` 2.10): `service/register_test.go`
      `TestServiceRegistryHasNoFlowBuilder`; `processor/agentic-tools/executors/register_test.go`
      `TestToolRegistryHasNoFlowTools` (asserts each of the eleven names is absent after `RegisterBuiltins` with every
      dependency non-nil); `test/contract/openapi_no_flow_routes_test.go` `TestOpenAPIHasNoFlowRoutes`;
      `service/stream_override_expiry_test.go` `TestStreamOverrideExpiryReporterRegistersWithoutFlowService`.
- [x] 2.2 Baseline capture: run each §2 file with `-run` and record its verbatim first failure line or compile error
      here, before any deletion, so each guard is proven load-bearing at the baseline. Commands:
      `go test -race ./service/ -run 'TestServiceRegistryHasNoFlowBuilder|TestStreamOverrideExpiryReporterRegistersWithoutFlowService' -v`;
      `go test -race ./processor/agentic-tools/executors/ -run TestToolRegistryHasNoFlowTools -v`;
      `go test ./test/contract/ -run TestOpenAPIHasNoFlowRoutes -v`.
      Commit the tests before any deletion.

      **Baseline capture at `e4acbdd4` (pre-deletion), verbatim first failure lines.**

      `go test ./service/ -run 'TestServiceRegistryHasNoFlowBuilder' -v`
      ```
      --- FAIL: TestServiceRegistryHasNoFlowBuilder (0.00s)
          register_test.go:37: service registry still offers "flow-builder"; ADR-100 D5 removes the flow-builder service without an alias
          register_test.go:43: GetAllOpenAPISpecs still carries the "flow-service" declaration; its init() registration must go with the service
          register_test.go:51: service "flow-service" still declares retired route "/flows"
      ```
      (seven route rows in all, one per retired path.)

      `go test ./service/ -run 'TestStreamOverrideExpiryReporterRegistersWithoutFlowService' -v`
      ```
      --- FAIL: TestStreamOverrideExpiryReporterRegistersWithoutFlowService (0.02s)
          Error: Should be true
          Messages: metric semstreams_streams_migration_override_expiredmap[owner:team-legacy stream:LAPSED] must be published
      ```

      `go test ./processor/agentic-tools/executors/ -run TestToolRegistryHasNoFlowTools -v`
      ```
      --- FAIL: TestToolRegistryHasNoFlowTools (0.00s)
          register_test.go:556: BuiltinGroupKeys still carries "flows"; the group it skipped no longer exists
          register_test.go:556: BuiltinGroupKeys still carries "flow_templates"; the group it skipped no longer exists
      ```
      Honest note on this one: at the baseline `ToolDependencies.FlowManager` is left nil, so `registerFlows`
      SKIPS and the eleven-name assertion passes vacuously; only the `BuiltinGroupKeys` assertion is load-bearing
      at the baseline. Setting the two managers non-nil here would mean naming `flowstore`/`flowtemplate` types that
      §3 deletes, i.e. a test that has to be EDITED to go green. The eleven-name assertion is proven load-bearing
      instead by the forced omission in 4.3, at the final head.

      `go test ./test/contract/ -run TestOpenAPIHasNoFlowRoutes -v`
      ```
      --- FAIL: TestOpenAPIHasNoFlowRoutes (0.00s)
          openapi_no_flow_routes_test.go:46: specs/openapi.v3.yaml still publishes "/flows"; ADR-100 D5 removes it without an alias
      ```
      (seven path rows and eight schema rows in all.)

## 3. GREEN — rehome, then delete

- [x] 3.1 **Rehome.** `service/stream_override_expiry.go` (constructor + `RegisterMetrics`) onto ComponentManager or
      the metrics service — decide and record which here — so the override-expiry metric survives. Its only host today
      is `service/flow_service.go:560-585`.

      DECIDED: **ComponentManager**, not the metrics service. It is the one service the framework refuses to compose
      without (`service/service_manager.go:385-387` `mandatoryServices`; `:167` returns `MandatoryServiceDisabledError`
      for a configuration that disables it), so a deployment that declares a `stream_migration_overrides` bridge cannot
      lose the report by not enabling a service. `metrics` is optional — `configs/graph-backend.json`,
      `configs/flows/ops-agent.json` and others compose without it — and hosting a report-only signal on an optional
      service is the phantom-signal class this reporter exists to avoid.
      Implementation: `service/component_manager.go` — field `overrideExpiry`; constructed in `NewComponentManager`
      from `currentConfig.Get` (LIVE, not the boot snapshot: an operator may renew a bridge without restarting);
      registered against `deps.MetricsRegistry` at composition and evaluated once so every declared bridge has a series
      from boot; the loop runs in `supervise` under a `sync.WaitGroup` joined before `supervisorDone` closes, so
      `Stop` joins it through the existing `waitSupervisor`.
      NOTE (inherited defect, fixed here rather than rehomed): the metric never actually reached `/metrics` before.
      `FlowService.RegisterMetrics` implemented the `Service` interface method, and **nothing in the framework calls
      `Service.RegisterMetrics`** (`grep -rn '\.RegisterMetrics(' --include='*.go' .` → test files only; the same
      finding is written up at `service/storage_observability.go:244-250`). Registering at composition against
      `deps.MetricsRegistry` is what makes the rehomed metric real; a faithful port of the old wiring would have moved
      a phantom. `ComponentManager` deliberately does NOT override `RegisterMetrics`: a method with no caller is the
      thing being removed.
- [x] 3.2 **Removal.** Delete: `flowstore/`, `flowtemplate/`, `engine/` (and the substrate PR's
      `composition/engine_parity_integration_test.go`, whose oracle is the engine), `service/flow_service.go`,
      `service/flow_runtime_*.go` and their tests, the four executor files and their tests, `service/register.go:15`,
      `configs/protocol-flow.json:39-42`, `cmd/semstreams/main.go:24-25,245,247,707-760`,
      `cmd/e2e-semstreams/main.go:27-28,185,187,418-460`, `test/e2e/client/observability.go:80-114`,
      `ToolDependencies.FlowManager`/`FlowTemplateManager` and the two gates
      (`register.go:51,53,114,116,201,203`), `docs/concepts/12-flow-architecture.md`,
      `docs/operations/migration-boot-only-flow-activation.md`. Re-run 1.2's grep → 0; paste the command and count here.

      DONE. Closing measurement, same command as 1.2, run from the worktree root:
      `grep -rn "flowstore\|flowtemplate\|flowengine\|flow-builder\|flowbuilder" --include='*.go' --include='*.json'
      --include='*.yml' --include='*.md' .`
      raw **752 → 439**; with the task's two exclusions **504 → 191**; live tree **361 → 43**.
      The "→ 0" in this task line is not reachable and 1.2 records why: history keeps the names. The 43 live hits are
      enumerated, every one deliberate — `docs/operations/migration-beta162-to-beta163.md` 22 (the new ADR-100 D5
      section, which exists to name what left), `openspec/specs/flow-authoring/spec.md` 8 (the capability spec, which
      leaves with `openspec archive`), the four removal guards 11 (`test/contract/openapi_no_flow_routes_test.go` 6,
      `service/register_test.go` 3, `service/stream_override_expiry_test.go` 2 — a guard must name what it forbids),
      `openspec/specs/composition-validation/spec.md` 1 (the `[~]` this change's MODIFIED requirement replaces at
      archive), and `test/e2e/client/websocket.go:150` 1 — see 3.3's FILED item.
      Also removed beyond the list above, all measured zero-caller and all residue of this same surface:
      `cmd/openapi-generator/openapi_generator_test.go:598-599` (expected `flow-service` in the registry),
      `processor/agentic-tools/categories.go` (`create_flow` category row), `configs/flows/ops-agent{,-test}.json`
      (four removed tool names in `allowed_tools`, replaced with `validate_composition` + `composition_graph`;
      the Phase-2 note's tool list), `configs/protocol-flow.json` log-forwarder `exclude_sources:
      ["flow-service.websocket"]`, `service/README.md` "Saved Flow Authoring Endpoints",
      `docs/basics/06-configuration.md` "UI Mode"/"Static Config → Flow Bridge" (replaced with the projection),
      `docs/concepts/18-rule-driven-artifacts.md`, `docs/operations/adopter-tool-effect-metadata.md`,
      `docs/operations/26-nats-kv-key-migration-ledger.md` (three ledger rows for packages that no longer exist),
      `persona/manager.go` × 3 and `doc.go` × 2 and `graph/kvcatalog.go` × 1 comment references,
      `test/e2e/scenarios/crud-tools/scenario.go` doc comment.
- [x] 3.3 **Re-judge the retained duplicate build.** `ComponentManager.GetFlowGraph` / `buildFlowGraph` /
      `flowgraph.BuildFromRegistry` and `GET <components>/paths` rebuild a graph from the admitted registry instead of
      serving the retained `composition.Result.Graph`. PR #1101 removed the `/gaps` judgment and recorded this build as
      a deliberate not-done in the `composition-validation` delta, scoped to this change. Decide: serve
      `Result.Graph`-derived reachability from `/paths`, or record why the rebuild stays. Whichever it is reaches the
      spec delta, not only this line.
      Also in scope here (PR #1101 review round 3, NIT-3): `AnalyzeConnectivity` still computes `ValidationStatus`
      through its own `hasCriticalIssues` walk (`component/flowgraph/flowgraph.go:822` sets `"healthy"`, `:858-876`
      flips it to `"warnings"`), and that walk carries the same latent defect the `/gaps` handler had — it treats every
      required stream `no_publishers` port as critical with no `External` check. It has NO production reader: the only
      readers are `component/flowgraph/flowgraph_test.go:258,312`, and `doc.go:35,55,141` teaches consuming it.
      `composition.Analyze` derives its own status and never reads the field. Delete the field and its computation with
      the doc paragraphs that teach it, or record why a status nothing reads stays.

      DECIDED, three parts, all removals; each reaches the `composition-validation` delta as a MODIFIED requirement
      (the `[~]` in `openspec/specs/composition-validation/spec.md:280-295` is replaced, not restated).

      1. **`GET <components>/paths` serves `composition.Result.Graph`.** Not retired — ADR-100 D1 blesses a read-only
         projection of the admitted composition, `/paths` derives no severity, and retiring it is a break ADR-100 D5
         does not enumerate and the delta carries no absence guard for. But the duplicate BUILD goes:
         `ComponentManager.GetFlowGraph`, `buildFlowGraph`, `invalidateFlowGraph`, the `flowGraphCache` type and the
         `graphCache` field are removed, and `GetFlowPaths` now walks `composition.Result.Graph` — the same result
         `<components>/flowgraph` and `<components>/validate` serve. `findInputComponents`/`isInputComponent` collapse
         into `isInputNode(composition.Node)`, which reads the projected `Type` and port `Pattern` instead of
         re-reading `componentConfigs`; `depthFirstTraversal`/`dfsVisit` collapse into `reachableFrom`. One
         caller-visible change, recorded in the migration doc: an uninitialized composition now answers 503, matching
         the sibling projections, where the rebuild answered 500.
      2. **`flowgraph.BuildFromRegistry` is removed.** With `engine/` and `buildFlowGraph` gone it had zero production
         callers (`grep -rn BuildFromRegistry --include='*.go' .` → one doc-comment mention in a test helper).
         `BuildFromDeclarations` is the one construction seam and `composition.Analyze` its production caller.
      3. **`FlowAnalysisResult.ValidationStatus` and `hasCriticalIssues` are removed**, with the three `doc.go`
         paragraphs that taught consuming them (`:35` diagram row, `:54-55` example, `:141` prose). No production
         reader existed; the walk carried the retired `/gaps` defect (every required stream `no_publishers` port
         critical, no `External` check). The two test assertions on it are replaced with assertions on the FACTS the
         analysis reports (`DisconnectedNodes`, `OrphanedPorts`), which is what it now returns.

      Also removed under this heading — measured zero-caller residue of the `/gaps` judgment PR #1101 retired, sitting
      beside the canonical library as a second exact-subject-match connection interpreter:
      `ComponentManager.analyzeFlowConnections` (no caller at all), `extractComponentPortInfo`/`extractPortDetail`
      (callers: `analyzeFlowConnections` + two tests), and the exported types `ComponentPortInfo`,
      `ComponentPortDetail`, `FlowConnection`, `ComponentPortReference`, `FlowGap` (no reference outside the file).
      Their two tests are not deleted but MOVED to the surviving home: the port-kind assertion now reads
      `composition.Result.Graph`'s `PortView.Kind`, and the unadmitted-instance rejection becomes "the projection
      carries only admitted instances".

      FILED, not fixed (unused path, and it belongs to ADR-096's retirement rather than ADR-100 D5):
      `test/e2e/client/websocket.go:138-152` `buildWSURL` still targets `/flowbuilder/status/stream`, a route ADR-096
      retired. Its only callers are `WebSocketClient.Health` and `WatchStatusStream`, and neither has a caller —
      `cmd/e2e/main.go:365-371,602` constructs the client and hands it to the core-dataflow scenario, which dials
      `gorilla/websocket` directly (`test/e2e/scenarios/core_dataflow.go:149`) and never touches it. Roughly 250 lines
      of dead e2e harness; deleting it is its own slice.
- [x] 3.4 Write `docs/operations/migration-composition-validation-adr100.md`: removed routes, tools, packages, buckets;
      per-repo instructions for semstreams-ui and semteams from inventory §9; what the projection and the verbs give
      back. The `/gaps` removal already has its section in `docs/operations/migration-beta162-to-beta163.md`; link it
      rather than restating it.

      DEVIATION (location only; content as specified). The section was written as a new `##` section INSIDE
      `docs/operations/migration-beta162-to-beta163.md` — "## Flow-authoring retirement (ADR-100 D5) — the
      saved-diagram surface is removed" — rather than as a separate
      `docs/operations/migration-composition-validation-adr100.md`. Reason: that document's own header states the
      convention "One `##` section per landing; later wave items … append their own sections below", and #1101 already
      placed the ADR-100 `/gaps` section there. A second file would split one release's ADR-100 story across two homes,
      which is the thing the "one home" rule refuses, and the section can reference the `/gaps` section directly
      instead of linking across files. Content is as the task specifies: removed routes (a table of ten, each with its
      replacement and the downstream action), tools, packages/symbols, buckets, the `/paths` and metric-host changes,
      and per-repo instructions for semstreams-ui and semteams measured at their pinned SHAs. Reversible with one
      `git mv` + a link if the owner prefers the separate file.
- [x] 3.5 Commit GREEN with a BREAKING footer before §4.
      `e097c8d9` `refactor(flow)!: retire the flow-authoring surface (ADR-100 D5)` — 76 files, +571/−11336.

## 4. Forced omissions — each guard must be load-bearing

Each: apply the omission, run the named command, record the verbatim failure, restore with `cp` from a copy taken
before the omission, and record `shasum -a 256` equality of the restored file. Commit before mutating.

- [x] 4.1 Delete the rehomed reporter registration → `TestStreamOverrideExpiryReporterRegistersWithoutFlowService`
      MUST fail.
- [x] 4.2 Re-add `"flow-builder"` to `service/register.go` → `TestServiceRegistryHasNoFlowBuilder` MUST fail.
- [x] 4.3 Re-add one flow tool registration → `TestToolRegistryHasNoFlowTools` MUST fail.
- [x] 4.4 Re-add one `/flowbuilder` OpenAPI row → `TestOpenAPIHasNoFlowRoutes` MUST fail.

      All four run against the GREEN head `e097c8d9`, committed before mutating. `[applied]` printed between each
      mutation and its test run; each file restored by `cp` from a copy taken before the omission and the restoration
      proved by `shasum -a 256` equality (4.3 by a clean `git status` — the omission was in a tracked file).

      **4.1** — deleted the `cm.overrideExpiry.register(deps.MetricsRegistry)` call in `NewComponentManager`.
      `go test -count=1 ./service/ -run 'TestStreamOverrideExpiryReporterRegistersWithoutFlowService'`
      ```
      --- FAIL: TestStreamOverrideExpiryReporterRegistersWithoutFlowService (0.02s)
          Messages: metric semstreams_streams_migration_override_expiredmap[owner:team-legacy stream:LAPSED] must be published
      ```
      restored `service/component_manager.go`
      `3fa4196141e036e9431176284aa567e6137e06b25849163c4ac592bcb4ec6b76` before == after.

      **4.2** — re-added `"flow-builder": NewMetrics` to `RegisterAll` (the constructor is gone; the NAME is what the
      guard forbids). `go test -count=1 ./service/ -run 'TestServiceRegistryHasNoFlowBuilder'`
      ```
      --- FAIL: TestServiceRegistryHasNoFlowBuilder (0.00s)
          register_test.go:37: service registry still offers "flow-builder"; ADR-100 D5 removes the flow-builder service without an alias
      ```
      restored `service/register.go` `48b619f167a2a3bf02e10aa42c11bf2c2ad7acd690374311af297aee4e199d34` before == after.

      **4.3 — the omission FOUND A HOLE IN THE GUARD, and the guard was fixed.** Re-added one retired registration in
      `registerComponentCatalog`: `tools.RegisterTool("create_flow", …)`.
      `go test -count=1 ./processor/agentic-tools/executors/ -run TestToolRegistryHasNoFlowTools` → **ok**, i.e. the
      guard stayed GREEN with `create_flow` registered. Cause: `ExecutorRegistry.ListTools` walks unique EXECUTORS and
      dedups by definition name (`processor/agentic-tools/executor.go:158`), so a name registered as a dispatch KEY
      whose executor advertises something else never appears there — while `Execute` keys straight off the
      registration map (`:211`), so the tool is callable. The guard read the advertised set only. Fixed by asking both
      questions: `containsName(reg.ListTools(), name)` AND `reg.GetTool(name) != nil`. Re-run with the omission still
      applied:
      ```
      --- FAIL: TestToolRegistryHasNoFlowTools (0.00s)
          register_test.go:530: tool registry still dispatches "create_flow"; ADR-100 D5 removes it without an alias
      ```
      Omission then removed; `git status` clean for
      `processor/agentic-tools/executors/register_component_catalog.go`; guard green.

      **4.4** — re-added a `"/flows"` GET row to the ComponentManager OpenAPI declaration
      (`service/component_manager_http.go`) and ran `task schema:generate`, so the mutation travels the real
      declaration → generator → document path rather than being hand-edited into the artifact.
      `go test -count=1 ./test/contract/ -run TestOpenAPIHasNoFlowRoutes`
      ```
      --- FAIL: TestOpenAPIHasNoFlowRoutes (0.00s)
          openapi_no_flow_routes_test.go:46: specs/openapi.v3.yaml still publishes "/flows"; ADR-100 D5 removes it without an alias
      ```
      restored `service/component_manager_http.go`
      `cfae8df40db6db2be3129850de845368f11a6170c58a5d09b95a00ba0fbcc353` and `specs/openapi.v3.yaml`
      `2711f3b5b66b91eec5a248e532cddfbc10f6dd5d398415a5be9c0e2be4825e11`, both before == after.

## 5. Schema regeneration

- [x] 5.1 `task schema:generate`; commit the removed `/flows*` rows and `Flow*` schemas. Second `task schema:generate`
      → `git diff --exit-code schemas/ specs/openapi.v3.yaml` clean. `go test ./test/contract/...`.
      `schemas/workflow-definition.v1.json` is stale (no factory, `cmd/openapi-generator/main.go:94`) and
      `test/contract` keeps it in `nonComponentSchemas`; decide its fate here and record it.

      DONE. `task schema:generate` → `specs/openapi.v3.yaml` −721 lines (seven `/flows*` path items / ten operations,
      the `Flow` / `FlowCreateRequest` / `FlowUpdateRequest` / `FlowListResponse` /
      `RuntimeHealthResponse` / `RuntimeMetricsResponse` / `RuntimeMessagesResponse` /
      `publishComponentConfigsResponse` schemas, and the `Flows` + `Flow observations` tags); `schemas/*.json`
      unchanged (no component factory was touched). Generator now reports "Found 5 service OpenAPI specs" (was 6).
      Second `task schema:generate` → `git diff --stat schemas/ specs/` identical to the first (−721 only) →
      NO DRIFT. `go test ./test/contract/` → `ok github.com/c360studio/semstreams/test/contract 3.135s`.
      Remaining `Flow` occurrences in the document are the retained `FlowGraph` **tag** on
      `/flowgraph`, `/paths`, `/validate` (4 lines) — the projection surface ADR-100 keeps.

      `schemas/workflow-definition.v1.json` — DECIDED: **it stays**, and the `nonComponentSchemas` exception stays with
      it. It is stale in this tree (no factory generates it), but it is not stale downstream: `semstreams-ui` vendors a
      copy at `contracts/semstreams/schemas/workflow-definition.v1.json` and `semteams` carries both
      `schemas/workflow-definition.v1.json` and a `test/contract/schema_contract_test.go` that names it. Deleting it is
      therefore a downstream contract break that ADR-100 does not authorize and this change's deltas carry no guard
      for; #1092 reached the same conclusion (`openspec/changes/archive/2026-08-27-composition-validation-substrate/
      tasks.md:414-415`). FILED for a separate owner ruling.

## 6. Standard gates — record each command and its result

- [x] 6.1 `task lint`.
      `task lint` → `go vet ./...`, `go fmt ./...`, `revive` and the two guards all clean; last line
      `ok github.com/c360studio/semstreams/test/natsclient 0.555s`. Zero findings.
- [x] 6.2 `go test -race -count=1 ./...`.
      First run on the GREEN head found ONE failure, fixed here: `test/testinfra` `TestInfrastructurePolicyGuard`
      ```
      policy_guard_test.go:68: stale policy baseline entries (2); remove resolved debt so the ratchet cannot hide a regression:
        integration-time-sleep|service/flow_runtime_messages_integration_test.go|TestRuntimeMessagesIntegration|time.Sleep(100 * time.Millisecond)|1
        integration-time-sleep|service/flow_runtime_messages_integration_test.go|TestRuntimeMessagesIntegration|time.Sleep(100 * time.Millisecond)|2
      ```
      The two entries named a file this change deletes, and the ratchet correctly refuses a baseline that carries debt
      for code that no longer exists. Removed from `test/testinfra/policy_baseline.json`.
      Re-run: `go test -race -count=1 ./...` → `grep -c '^FAIL'` = **0**, `exit=0`.
- [x] 6.3 `go test -race -count=1 -tags=integration -p 2 ./...`.
      → `grep -c '^FAIL'` = **0**, `exit=0` (ended 2026-08-27T14:01:34Z). Run alone on the host — the other agent's
      suite was confirmed finished first (`pgrep -fl 'go test'` empty).
- [x] 6.4 `task build` and the CI cross-compile line; `go vet -tags=integration ./...`.
      `task build` → `Built bin/semstreams`.
      `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/semstreams` → OK, and the same for
      `./cmd/e2e-semstreams` → OK (`.github/workflows/ci.yml:138` is the line mirrored).
      `go vet -tags=integration ./...` → clean, no output.
- [x] 6.5 `go test ./test/contract/...`.
      → `ok github.com/c360studio/semstreams/test/contract 2.670s`. `task schema:check-changes` → clean
      (`git diff --exit-code schemas/ specs/openapi.v3.yaml` passes).
- [ ] 6.6 **BREAKING e2e gate** (ADR-100 Consequences; the repository rule is that a BREAKING commit has its covering
      tier green BEFORE it lands): `task e2e:core`, `task e2e:crud-tools` (the tool registry boots without the flow
      gates), `task e2e:agentic` (the largest shipped composition through the boot check). Paste each tier summary here.
- [x] 6.7 Downstream measurement (read-only): `cd ~/Code/c360/semteams && go vet ./cmd/semteams/` against a `replace`
      to this branch in a scratch module (never edit semteams; snapshot its porcelain and use
      `GOFLAGS=-mod=readonly` or a scratch copy so its `go.mod` is not rewritten); record the compile errors as the
      migration document's semteams section. semstreams-ui: record the 15 call sites from inventory §9 in the migration
      document; the owner runs its suite.

      DONE, and written into `docs/operations/migration-beta162-to-beta163.md` § "Flow-authoring retirement (ADR-100
      D5)" → "Downstream action".
      semteams: probed in a scratch `rsync` copy (`.git`, `node_modules`, `.claude/worktrees` excluded) with a
      `replace` appended to the COPY's `go.mod`; the real checkout was never touched — HEAD `8a70b7e76e2598…` and
      `git status --porcelain` were snapshotted before and re-read after and are identical. `go vet ./cmd/semteams/`
      → three errors, all import-resolution:
      ```
      cmd/semteams/main.go:24:2: module github.com/c360studio/semstreams ... does not contain package .../engine
      cmd/semteams/main.go:25:2: ... does not contain package .../flowstore
      cmd/semteams/main.go:26:2: ... does not contain package .../flowtemplate
      ```
      (semteams already fails to compile against `main` for non-flow reasons — inventory §9; this adds three imports
      to a migration it already owed.) The symbols behind them are enumerated in the migration section.
      semstreams-ui at `39f5f04`: **17** hand-written `src/` files / **19** call sites, plus **16** `e2e/` files,
      enumerated by grep in the migration section, including `e2e/helpers/backend-helpers.ts` whose
      `reapOrphanedTestFlows` runs from `global-setup.ts` on every run.
## 7. Review and archive (inside the landing PR)

- [ ] 7.1 `semstreams-reviewer` on the GREEN + §4 + §5 head: verdict, every finding and its disposition (FIXED /
      FILED #n / ruling) recorded here. Findings on unused paths are FILED, not fixed.
- [ ] 7.2 Owner-run cross-agent round where the owner asks for it: verdict and dispositions recorded here; each fix
      re-enters 7.1.
- [ ] 7.3 `conformance.md`: replace every `__` placeholder with the measured `file:line` at the head that carries the
      last `.go` or delta change. Maintained as part of every commit that moves a line, not at the end.
- [ ] 7.4 Reconcile: every REMOVED requirement in `specs/flow-authoring/spec.md` and
      `specs/component-runtime-config/spec.md` names tests that no longer exist; every scenario in
      `specs/composition-validation/spec.md` names a test that exists and is green in 6.2/6.3/6.5; table recorded here.
      Any `[~]` in this file is ALSO written into the delta before archiving.
- [ ] 7.5 `openspec archive flow-authoring-retirement` with the spec sync as the final content commit — the
      `flow-authoring` capability directory leaves `openspec/specs/` with it; the narrow reviewer check of the
      archive/spec sync follows as a PR comment; then undraft. The PR body is a published layer: re-read it at undraft
      and correct any claim the branch no longer supports.

## 8. Not in scope (recorded so the archiver does not infer completion)

- A next-boot component-configuration write verb (design §7 item 1).
- `POST <components>/validate` with a draft body.
- semstreams-ui and semteams migrations (owners' work; instructions in the migration document).
- #1008, #1060, #1087 (their surfaces are removed; ruled out with ADR-100).
