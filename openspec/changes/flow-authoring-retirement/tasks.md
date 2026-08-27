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
      `claude/gh1093-flow-authoring-retirement`, PR #____ (draft, `Closes #1093`).
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

- [ ] 2.1 Removal guards (carried from `composition-validation-substrate` 2.10): `service/register_test.go`
      `TestServiceRegistryHasNoFlowBuilder`; `processor/agentic-tools/executors/register_test.go`
      `TestToolRegistryHasNoFlowTools` (asserts each of the eleven names is absent after `RegisterBuiltins` with every
      dependency non-nil); `test/contract/openapi_no_flow_routes_test.go` `TestOpenAPIHasNoFlowRoutes`;
      `service/stream_override_expiry_test.go` `TestStreamOverrideExpiryReporterRegistersWithoutFlowService`.
- [ ] 2.2 Baseline capture: run each §2 file with `-run` and record its verbatim first failure line or compile error
      here, before any deletion, so each guard is proven load-bearing at the baseline. Commands:
      `go test -race ./service/ -run 'TestServiceRegistryHasNoFlowBuilder|TestStreamOverrideExpiryReporterRegistersWithoutFlowService' -v`;
      `go test -race ./processor/agentic-tools/executors/ -run TestToolRegistryHasNoFlowTools -v`;
      `go test ./test/contract/ -run TestOpenAPIHasNoFlowRoutes -v`.
      Commit the tests before any deletion.

## 3. GREEN — rehome, then delete

- [ ] 3.1 **Rehome.** `service/stream_override_expiry.go` (constructor + `RegisterMetrics`) onto ComponentManager or
      the metrics service — decide and record which here — so the override-expiry metric survives. Its only host today
      is `service/flow_service.go:560-585`.
- [ ] 3.2 **Removal.** Delete: `flowstore/`, `flowtemplate/`, `engine/` (and the substrate PR's
      `composition/engine_parity_integration_test.go`, whose oracle is the engine), `service/flow_service.go`,
      `service/flow_runtime_*.go` and their tests, the four executor files and their tests, `service/register.go:15`,
      `configs/protocol-flow.json:39-42`, `cmd/semstreams/main.go:24-25,245,247,707-760`,
      `cmd/e2e-semstreams/main.go:27-28,185,187,418-460`, `test/e2e/client/observability.go:80-114`,
      `ToolDependencies.FlowManager`/`FlowTemplateManager` and the two gates
      (`register.go:51,53,114,116,201,203`), `docs/concepts/12-flow-architecture.md`,
      `docs/operations/migration-boot-only-flow-activation.md`. Re-run 1.2's grep → 0; paste the command and count here.
- [ ] 3.3 **Re-judge the retained duplicate build.** `ComponentManager.GetFlowGraph` / `buildFlowGraph` /
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
- [ ] 3.4 Write `docs/operations/migration-composition-validation-adr100.md`: removed routes, tools, packages, buckets;
      per-repo instructions for semstreams-ui and semteams from inventory §9; what the projection and the verbs give
      back. The `/gaps` removal already has its section in `docs/operations/migration-beta162-to-beta163.md`; link it
      rather than restating it.
- [ ] 3.5 Commit GREEN with a BREAKING footer before §4.

## 4. Forced omissions — each guard must be load-bearing

Each: apply the omission, run the named command, record the verbatim failure, restore with `cp` from a copy taken
before the omission, and record `shasum -a 256` equality of the restored file. Commit before mutating.

- [ ] 4.1 Delete the rehomed reporter registration → `TestStreamOverrideExpiryReporterRegistersWithoutFlowService`
      MUST fail.
- [ ] 4.2 Re-add `"flow-builder"` to `service/register.go` → `TestServiceRegistryHasNoFlowBuilder` MUST fail.
- [ ] 4.3 Re-add one flow tool registration → `TestToolRegistryHasNoFlowTools` MUST fail.
- [ ] 4.4 Re-add one `/flowbuilder` OpenAPI row → `TestOpenAPIHasNoFlowRoutes` MUST fail.

## 5. Schema regeneration

- [ ] 5.1 `task schema:generate`; commit the removed `/flows*` rows and `Flow*` schemas. Second `task schema:generate`
      → `git diff --exit-code schemas/ specs/openapi.v3.yaml` clean. `go test ./test/contract/...`.
      `schemas/workflow-definition.v1.json` is stale (no factory, `cmd/openapi-generator/main.go:94`) and
      `test/contract` keeps it in `nonComponentSchemas`; decide its fate here and record it.

## 6. Standard gates — record each command and its result

- [ ] 6.1 `task lint`.
- [ ] 6.2 `go test -race -count=1 ./...`.
- [ ] 6.3 `go test -race -count=1 -tags=integration -p 2 ./...`.
- [ ] 6.4 `task build` and the CI cross-compile line; `go vet -tags=integration ./...`.
- [ ] 6.5 `go test ./test/contract/...`.
- [ ] 6.6 **BREAKING e2e gate** (ADR-100 Consequences; the repository rule is that a BREAKING commit has its covering
      tier green BEFORE it lands): `task e2e:core`, `task e2e:crud-tools` (the tool registry boots without the flow
      gates), `task e2e:agentic` (the largest shipped composition through the boot check). Paste each tier summary here.
- [ ] 6.7 Downstream measurement (read-only): `cd ~/Code/c360/semteams && go vet ./cmd/semteams/` against a `replace`
      to this branch in a scratch module (never edit semteams; snapshot its porcelain and use
      `GOFLAGS=-mod=readonly` or a scratch copy so its `go.mod` is not rewritten); record the compile errors as the
      migration document's semteams section. semstreams-ui: record the 15 call sites from inventory §9 in the migration
      document; the owner runs its suite.

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
