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

      **CORRECTED CENSUS (review HIGH-2).** The census above used the task's case-SENSITIVE five-token pattern, which
      cannot see `FlowService`, `FlowManager`, `flow_template`, the bucket names, or the tool names, and it missed six
      survivors: `service/doc.go:29-33` (FlowService listed as a Core Service Type) and `:283-285` (an adopter example
      teaching `manager.RegisterConstructor("flow-service", …)` → `service.NewFlowService(d, flowEngine, flowStore)`,
      a signature that never existed on this head), `doc.go:116` (`engine: Component orchestration and lifecycle`),
      `message/README.md:670` (dangling link to `../engine/`), `component/flowgraph/doc.go:39-44` ("service.
      ComponentManager owns that construction" — now `composition.Analyze` through `BuildFromDeclarations`), and
      `processor/agentic-tools/executors/personas.go:14` (`FlowManager` in a comment). All six swept.
      Re-run with the wide pattern:
      `grep -rniE 'flowstore|flowtemplate|flowengine|flow-builder|flowbuilder|FlowManager|FlowService|flow_template|semstreams_flows|FLOW_TEMPLATES|create_flow|update_flow|delete_flow|list_flows|get_flow|instantiate_flow|manage_flow' --include='*.go' --include='*.json' --include='*.yml' --include='*.yaml' --include='*.md' .`
      raw **650**; excluding `docs/adr/` + `openspec/changes/archive/` **336**; live tree **89**. Every live hit is in
      one of five permitted classes, none of them production code:

      | Count | Where | Class |
      |---|---|---|
      | 30 | `docs/operations/migration-beta162-to-beta163.md` | the migration section, which exists to name what left |
      | 25 | the four removal guards + `service/stream_override_expiry_test.go` | a guard must name what it forbids |
      | 14 | `openspec/specs/flow-authoring/spec.md` | the retired capability spec; leaves with `openspec archive` |
      | 16 | `testutil/flow.go` (12) + `testutil/doc.go` (4) | pre-existing dead `FlowBuilder`, zero callers — FILED by the coordinator, not this change |
      | 2 | `docs/operations/migration-beta34-to-beta35.md` | a historical migration doc, history like `docs/adr/` |
      | 1 | `test/e2e/client/websocket.go:150` | ADR-096 residue, FILED (3.3) |
      | 1 | `openspec/specs/composition-validation/spec.md` | the `[~]` this change's MODIFIED requirement replaces at archive |

      Production `.go` outside `testutil/` and the guards: **zero**
      (`grep -rniE '<wide pattern>' --include='*.go' . | grep -v _test.go` returns only the two FILED files).
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

      Behavioural guard for part 1, added after the first self-review found the rewrite untested:
      `TestFlowPathsTraverseTheRetainedGraph` (`service/component_manager_paths_test.go`) builds a four-node
      composition through the production Registry — a `type: "input"` timer-driven poller, a network-listening
      processor, a middle processor and an output sink — and asserts BOTH origins are found and each reaches the whole
      downstream chain in depth-first order. Every other test on this surface only checks that the handler returns 200,
      and an empty `paths` map is a well-formed 200. Two forced omissions, each restored by `cp` with
      `shasum -a 256` `3fa4196141e036e9431176284aa567e6137e06b25849163c4ac592bcb4ec6b76` before == after:
      removing `isInputNode`'s declared-type branch → `"map[listener:[listener middle sink]]" should have 2 item(s),
      but has 1`; dropping the edge-derivation loop in `GetFlowPaths` → `Not equal` on the reachable chain.

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

      `schemas/workflow-definition.v1.json` — **DELETED.** This reverses the DECIDED-stays I recorded first, on the
      owner's ruling on #1122 (2026-08-27): the file belongs to the flow-authoring retirement and goes with it. My
      earlier reasoning weighed the two vendored downstream copies as a break ADR-100 did not authorize; the owner
      ruled the opposite way and the ruling governs.

      Premise correction: the ruling located the exemption at `cmd/openapi-generator/main.go:94`. Measured, there is
      no `nonComponentSchemas` there — `:102` held only a stale comment ("Workflow definition schema generation
      removed"), which is deleted with the artifact it described. The exemption lives in
      `test/contract/schema_contract_test.go:17-21` (declaration) with skip sites at `:64`, `:113`, `:237` and
      `test/contract/schema_export_test.go:30`. All removed; the map is gone entirely rather than left empty, so no
      file in `schemas/` is exempt from any guard. `schema_export_test.go`'s `name` local existed only to feed the
      skip and went with it.

      Evidence the removal makes the guards REAL rather than cosmetic — restored the deleted artifact
      (`git show origin/main:schemas/workflow-definition.v1.json`, sha256
      `7938cc0671725d535a1c64e2f61963d2b0e991d91c394eedaa69bd5f49b4c61c`) and re-ran `go test ./test/contract/...`:
      ```
      --- FAIL: TestCommittedSchemasMatchCode
      --- FAIL: TestCommittedSchemasValidStructure
      --- FAIL: TestNoOrphanedSchemaFiles
          schema_contract_test.go:221: Orphaned schema file: workflow-definition.v1.json (no corresponding component registered)
      --- FAIL: TestSchemaExportCarriesDefaultPorts
      ```
      Four guards that had been skipping it now catch it. Artifact deleted again; `go test ./test/contract/...` →
      `ok github.com/c360studio/semstreams/test/contract 2.645s`.

      `task schema:generate` run twice after the deletion: the generator does NOT re-create the file (confirming "no
      factory"), and `git status --porcelain schemas/ specs/` shows only the deletion both times → no drift.
      `task schema:check-changes` clean. `schemas/` is now **33 files for 33 registered factories** — the count the
      ADR-100 inventory said it should have been (`docs/proposals/gh1089-flow-boundary-inventory.md:358`).
      Downstream obligations (semstreams-ui `39f5f04`, semteams `8a70b7e7` — file AND its contract-test exemption at
      `test/contract/schema_contract_test.go:14-18,57,106,188`) are recorded in
      `docs/operations/migration-beta162-to-beta163.md` § "`schemas/workflow-definition.v1.json` is deleted".

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

      A LATER run on this host went red once on a test this change does not touch —
      `service` `TestStartHealthListener_BindsHealthAndHealthz`:
      `bind health listener: listen tcp :55079: bind: address already in use`. Mechanism, read rather than guessed:
      `freePort` (`service/service_manager_health_listener_test.go:277-286`) binds `:0`, reads the number and CLOSES
      the listener, then the test binds that number again — a probe whose answer goes stale the instant anything else
      binds. Ten call sites across three files share it. Not caused here (`git log origin/main..HEAD` touches none of
      those files) but plausibly AGGRAVATED here: the two new guard tests each started an embedded NATS server on a
      random ephemeral port, which HOLDS it. Response: the two now share ONE server for the whole file
      (`sharedOverrideExpiryNATS`), halving the ports this change adds to the package. The helper's own race is FILED,
      not fixed — a ten-call-site test-helper change does not belong in a retirement PR.
      Final run after that change: `go test -race -count=1 ./...` → `grep -c '^FAIL'` = **0**, `exit=0`;
      the named test passed 3/3 in isolation before and after.
      Re-run again after the #1122 ruling (deleting `schemas/workflow-definition.v1.json` and the
      `nonComponentSchemas` exemption): `go test -race -count=1 ./...` → `grep -c '^FAIL'` = **0**, `exit=0`.
- [x] 6.3 `go test -race -count=1 -tags=integration -p 2 ./...`.
      → `grep -c '^FAIL'` = **0**, `exit=0` (ended 2026-08-27T14:01:34Z). Re-run after the §7.1 self-review additions
      → `grep -c '^FAIL'` = **0**, `exit=0`. Run alone on the host both times — `pgrep -f 'go test'` was checked empty
      before each.
- [x] 6.4 `task build` and the CI cross-compile line; `go vet -tags=integration ./...`.
      `task build` → `Built bin/semstreams`.
      `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/semstreams` → OK, and the same for
      `./cmd/e2e-semstreams` → OK (`.github/workflows/ci.yml:138` is the line mirrored).
      `go vet -tags=integration ./...` → clean, no output.
- [x] 6.5 `go test ./test/contract/...`.
      → `ok github.com/c360studio/semstreams/test/contract 2.670s`. `task schema:check-changes` → clean
      (`git diff --exit-code schemas/ specs/openapi.v3.yaml` passes).
      Re-run after the #1122 ruling, with every schema now covered by every guard and no exemption map:
      → `ok github.com/c360studio/semstreams/test/contract 2.645s`; `task schema:check-changes` clean.
- [x] 6.6 **BREAKING e2e gate** (ADR-100 Consequences; the repository rule is that a BREAKING commit has its covering
      tier green BEFORE it lands): `task e2e:core`, `task e2e:crud-tools` (the tool registry boots without the flow
      gates), `task e2e:agentic` (the largest shipped composition through the boot check). Paste each tier summary here.

      All three run on this branch's head `2c5d4add`, one at a time, with the host coordinated
      (`task e2e:check-ports` clean, no foreign `semstreams|e2e` containers, `/private/tmp/claude-501/e2e-lock-gh1093`
      held for the sequence and removed after teardown). Every tier tore its compose stack down; `docker ps` after the
      sequence shows only the unrelated `semdev-nats`.

      **`task e2e:core` → exit=0.**
      ```
      [OK] All services are healthy
      [OK] Readiness and heartbeat report 12/12 healthy components
      Scenario PASSED name=core-health
      Scenario PASSED name=core-dataflow          (36.3s; 10 messages sent, 10 file lines, ws output route 101)
      Scenario PASSED name=core-graph-roundtrip
      Test suite complete passed=3 failed=0 total=3
      [OK] SIGTERM exited 0, released listeners, completed shutdown, and left NATS healthy
      [OK] Early SIGTERM canceled blocked NATS boot, exited 1, and fenced service startup
      ```
      This tier boots `configs/protocol-flow.json`, the one shipped config that named the `flow-builder` service; it
      boots with that block removed and the component count is unchanged (the removal was a service, not a component).

      **`task e2e:crud-tools` → exit=0.** The tool registry boots with the two flow gates gone:
      ```
      Scenario completed successfully duration=981.4ms
        verify-registered-tools_duration_ms:38  verify-tool-effect-catalog_duration_ms:2
        tool_executions:4  rule_size_bytes:342  hotreload_pickup_latency_ms:329
        fire_every_n_triggered_delta:9  fire_every_n_gate_passes_delta:3  fire_every_n_not_triggered_delta:0
      ```

      **`task e2e:agentic` → exit=0.** The largest shipped composition through the boot check:
      ```
      [OK] Services are healthy (NATS + mock-llm + semstreams)
      Scenario completed successfully duration=45.26s
        graph_loop_triples:10  graph_model_triples:6  trajectory_facts:10
        governance_verdicts_total:1  governance_verdicts_approved_audit:1
        durable_tool_replay_executor_invocations:1  stream_chunks_total:5  tool_executions:1
      ```

      **`task e2e:ops` → exit=0** (added on review MED-3: this tier boots `configs/flows/ops-agent-test.json`, whose
      `allowed_tools` this change rewrote; the reviewer traced it as expected-green, which is an inference until
      measured):
      ```
      [OK] Services are healthy (NATS + mock-llm + semstreams)
      Scenario completed successfully duration=622.0ms  assertions_run=9
        verify-registered-tools_duration_ms:36  verify-diagnoses-via-http_duration_ms:19
        verify-lesson-proposed_duration_ms:14   promote-lesson_duration_ms:17
        wait-for-loop-completion_duration_ms:259
      ```
      Same host protocol: foreign suite waited out, ports checked, `e2e-lock-gh1093` held and removed, teardown
      confirmed clean.

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

- [x] 7.1 `semstreams-reviewer` on the GREEN + §4 + §5 head: verdict, every finding and its disposition (FIXED /
      FILED #n / ruling) recorded here. Findings on unused paths are FILED, not fixed.

      **Fable review at `32a12d12`: APPROVE WITH CHANGES — 0 BLOCKING, 2 HIGH, 6 MEDIUM, 6 NIT.** Every finding is
      dispositioned below; all applied in one round.

      | # | Finding | Disposition |
      |---|---|---|
      | HIGH-1 | `supervise` joins the loop via `loops.Wait()`, but the three FlowService guards for that property were deleted with no replacement; the reviewer dropped `loops.Wait()` and `-race ./service/` stayed green | **FIXED.** Three guards added in `service/stream_override_expiry_test.go`: `TestSuperviseHoldsDoneUntilTheOverrideExpiryLoopReturns` (zero-timeout select on the very channel `Stop` joins), `TestComponentManagerStopWaitsForTheOverrideExpiryLoop` (whole Start→Stop path), `TestComponentManagerFailedStartDoesNotLaunchOverrideExpiryLoop`. The lever is the reporter's OWN `configOf` — `run`'s first act is an immediate `evaluate`, so a config source the test holds open holds the loop open inside production code, with no test seam on the manager. Mutation transcript below |
      | HIGH-2 | case-sensitive census missed six survivors outside the permitted classes | **FIXED**, all six swept (`service/doc.go` ×2, `doc.go`, `message/README.md`, `component/flowgraph/doc.go`, `executors/personas.go`); census re-run with the wide case-insensitive pattern and recorded in 3.2. Note `service/doc.go:283-285` taught `service.NewFlowService(d, flowEngine, flowStore)` — a signature that never existed on this head, so the example was wrong before it was stale; replaced with the real `Registry.Register` shape |
      | MED-1 | `component_manager.go:79-80` comment ("Nil when the boot configuration declares no override source") contradicts `:239-242`, which always assigns | **FIXED** by correcting the comment. The nil-check at `:537` is NOT dead and stays: 30 tests build a `&ComponentManager{}` struct literal, which has no reporter. Both the field comment and the guard now say so |
      | MED-2 | bare `context.WithoutCancel(ctx)` in a `t.Cleanup` | **FIXED** — bounded with `context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)`, matching the two other detached contexts in the file |
      | MED-3 | `task e2e:ops` unmeasured though it boots the rewritten `ops-agent-test.json` | **FIXED** — run, green, row recorded in 6.6 (`assertions_run=9`) |
      | MED-4 | `TestFlowPathsTraverseTheRetainedGraph` left the `PatternHTTPClient` origin arm uncovered | **FIXED** — added a third origin node with an `HTTPClientPort` and re-stated the doc comment as three rules. Mutation-verified: deleting the arm now fails with `should have 3 item(s), but has 2` |
      | MED-5 | migration doc understates the metric's reach | **FIXED** — added "Expect output you have never seen before": the WARN previously fired only where `flow-builder` was enabled and the gauge reached `/metrics` in no process at all, so an already-lapsed bridge will start reporting on the first beta.163 boot |
      | MED-6 | `docs/adr/096:63` links the deleted migration guide | **FIXED** — one status line beneath the link pointing at ADR-100 and `migration-beta162-to-beta163.md`; no other edit to the ADR |
      | NIT | `executors/register.go:50-52` Pattern-B step comments now skip 2 and 4 | **FIXED** — a note that the numbers are historical and why |
      | NIT | `composition/findings.go:66` cites a deleted path | **FIXED** — marked as provenance at the SHA where it existed |
      | NIT | migration doc `:156-157` needs a forward pointer to the `/paths` 500→503 change | **FIXED** |
      | NIT | semstreams-ui count should separate the classes | **FIXED**, and MEASURED rather than taken: 17 hand-written `src/` files, 16 `e2e/` files naming `flowbuilder`, and **4** (not 10) further `e2e/` files driving the `/flows` UI routes without naming the proxy path — `flow-crud.spec.ts`, `flow-management.spec.ts`, `navigation.spec.ts`, `pages/FlowListPage.ts` — for 20 e2e files total. The four are named in the doc |
      | — | migration table lacked rows for `FlowServiceConfig`, `OverallHealth`, `ComponentHealth`, `ComponentMetric`, `RuntimeMessage`, `FlowExecutor`, `FlowTemplateExecutor` | **FIXED** — six rows added, each "none — served the removed routes", with the `file:line` each had on `origin/main` |
      | — | reviewer saw the held #1122 edits land mid-review as an unexplained writer | Explained: they were prepared under coordinator instruction and held unpushed pending the owner's ruling on #1122. Now folded into this round; task 5.1 amended from DECIDED-stays to the ruling |

      **FILED, not fixed** (unused paths, pre-existing, filed by the coordinator): `testutil/flow.go:185-255` dead
      `FlowBuilder` (zero callers outside `testutil/`, confirmed by grep); `metric/registry.go:243-247`
      `RegisterGaugeVec` discards the existing collector on a second registration.

      **HIGH-1 mutation transcript.** `service/component_manager.go` sha256
      `c3e19ff557ed1895e0e55a9af78e2e97f1d560fe574e7e67a813a63af3031ce3` before; `cp` backup taken; `loops.Wait()`
      replaced with a comment; `[applied]` printed; then
      `go test -race -count=1 ./service/ -run 'TestSuperviseHoldsDone|TestComponentManagerStopWaitsForTheOverrideExpiryLoop'`:
      ```
      --- FAIL: TestSuperviseHoldsDoneUntilTheOverrideExpiryLoopReturns (0.05s)
          stream_override_expiry_test.go:348: supervise released done while the override-expiry loop was still running: Stop would return with a live goroutine behind it
      --- FAIL: TestComponentManagerStopWaitsForTheOverrideExpiryLoop (0.04s)
          stream_override_expiry_test.go:393: Stop returned (<nil>) while the override-expiry loop was still inside evaluate
      ```
      Restored from the copy; sha256 `c3e19ff557ed1895e0e55a9af78e2e97f1d560fe574e7e67a813a63af3031ce3` — before ==
      after; suite green.

      **MED-4 mutation transcript.** Same file, same sha256 before and after; `case component.PatternNetwork,
      component.PatternHTTPClient:` narrowed to `case component.PatternNetwork:`;
      `go test -count=1 ./service/ -run TestFlowPathsTraverseTheRetainedGraph`:
      ```
      --- FAIL: TestFlowPathsTraverseTheRetainedGraph (0.00s)
          component_manager_paths_test.go:121: "map[listener:[listener middle sink] poller:[poller middle sink]]" should have 3 item(s), but has 2
      ```
      FILED #n / ruling) recorded here. Findings on unused paths are FILED, not fixed.
- [ ] 7.2 Owner-run cross-agent round where the owner asks for it: verdict and dispositions recorded here; each fix
      re-enters 7.1.
- [x] 7.3 `conformance.md`: replace every `__` placeholder with the measured `file:line` at the head that carries the
      last `.go` or delta change. Maintained as part of every commit that moves a line, not at the end.

      DONE at `2c5d4add` + this commit; every `__` in `conformance.md` replaced with a measured `file:line`, each
      re-read after the last code change on the branch. The CARRIED row now records the 3.3 resolution rather than
      pointing at an open question.
- [x] 7.4 Reconcile: every REMOVED requirement in `specs/flow-authoring/spec.md` and
      `specs/component-runtime-config/spec.md` names tests that no longer exist; every scenario in
      `specs/composition-validation/spec.md` names a test that exists and is green in 6.2/6.3/6.5; table recorded here.
      Any `[~]` in this file is ALSO written into the delta before archiving.

      **Table A — every test named by a REMOVED requirement is gone.** Command:
      `grep -oE "Test[A-Za-z0-9_]+" <the two REMOVED deltas> | sort -u` then `grep -rn "func <name>(" --include='*_test.go' .`
      All **17** return 0 occurrences: `TestManagerUpdatePreservesStoredCreatedAt`,
      `TestManagerUpdateIgnoresForgedCreatedAt`, `TestManagerUpdateSuccessMutatesInputAfterCommit`,
      `TestManagerDiagramCRUDAndVersioning`, `TestManagerUpdateFailedWriteDoesNotMutateInput`,
      `TestManagerUpdateTwoManagersExactlyOneWins`,
      `TestFlowCRUDDoesNotPublishAndExplicitPublicationRetriesThroughConfigManager`,
      `TestFlowUpdateRequestSchemaOmitsServerAuditFields`, `TestFlowOpenAPIPreservesFlowCRUDWireSchema`,
      `TestManagerListEmptyBucketReturnsNonNilEmpty`, `TestManagerListSkipsOnlyVanishedKey`,
      `TestManagerListPreservesPerKeyTransientFailure`, `TestManagerListPreservesCorruptRecordFailure`,
      `TestManagerListRejectsCancellationDuringEnumeration`, `TestHandleListFlowsEmptyResponseIsNonNullArray`,
      `TestEnsureDefaultFlowEmptyListUsesTypedOutcome`, `TestFlowExecutorListFlowsRealManagerEmpty`.

      **Table B — every test named by the `composition-validation` delta exists and is green in 6.2/6.5.**

      | Test | Location | Gate that ran it |
      |---|---|---|
      | `TestComponentGapsOperationIsAbsent` | `service/component_manager_gaps_removed_test.go:45` | 6.2 |
      | `TestExternalInputIsNeverACriticalOrphanOnAnyComponentOperation` | `service/component_manager_gaps_removed_test.go:68` | 6.2 |
      | `TestServiceRegistryHasNoFlowBuilder` | `service/register_test.go:29` | 6.2 |
      | `TestOpenAPIHasNoFlowRoutes` | `test/contract/openapi_no_flow_routes_test.go:41` | 6.5 |
      | `TestToolRegistryHasNoFlowTools` | `processor/agentic-tools/executors/register_test.go:501` | 6.2 |
      | `TestStreamOverrideExpiryReporterRegistersWithoutFlowService` | `service/stream_override_expiry_test.go:181` | 6.2 |
      | `TestComponentManagerFlowReportingUsesRetainedPortsAfterComponentMutation` | `service/component_manager_port_facts_test.go:107` | 6.2 |
      | `TestComponentManagerProjectionCarriesOnlyAdmittedInstances` | `service/component_manager_port_facts_test.go:90` | 6.2 |

      No `[~]` remains in this file; 3.3's decisions are SHALL clauses in the delta, not deferrals.
- [ ] 7.5 `openspec archive flow-authoring-retirement` with the spec sync as the final content commit — the
      `flow-authoring` capability directory leaves `openspec/specs/` with it; the narrow reviewer check of the
      archive/spec sync follows as a PR comment; then undraft. The PR body is a published layer: re-read it at undraft
      and correct any claim the branch no longer supports.

## 8. Not in scope (recorded so the archiver does not infer completion)

- A next-boot component-configuration write verb (design §7 item 1).
- `POST <components>/validate` with a draft body.
- semstreams-ui and semteams migrations (owners' work; instructions in the migration document).
- #1008, #1060, #1087 (their surfaces are removed; ruled out with ADR-100).
