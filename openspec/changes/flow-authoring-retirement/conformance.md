# Conformance — flow-authoring-retirement

Per-decision map from ADR-100 decision D5 ("Retirement without aliases") and the ADR's superseded clauses to the code,
spec delta, and test that carry them. Every `__` is replaced with the measured `file:line` at the head that carries the
last change to any `.go` file or spec delta on the branch (tasks 7.3). `tasks.md` rows cite section numbers. Decisions
D1–D4 and primitives P1–P7 were carried by `composition-validation-substrate` (PR #1101, #1092); its conformance table
is the record for those and is not restated here.

| # | Decision / clause | Implementation | Spec delta | Test / evidence |
|---|---|---|---|---|
| D5.a | The framework registers no `flow-builder` service | `service/register.go:__` (row removed) | `specs/composition-validation/spec.md` "The framework owns no composition authoring store" | `TestServiceRegistryHasNoFlowBuilder` |
| D5.b | The framework registers none of the eleven flow / flow-template tools | `processor/agentic-tools/executors/__` (four files removed; `ToolDependencies.FlowManager`/`FlowTemplateManager` and their two gates removed) | same requirement | `TestToolRegistryHasNoFlowTools` |
| D5.c | The generated OpenAPI document serves no `/flowbuilder` or `/flows` path and carries no `Flow*` schema | `specs/openapi.v3.yaml:__`, `schemas/__` | same requirement | `TestOpenAPIHasNoFlowRoutes` |
| D5.d | The framework creates no `semstreams_flows` or `FLOW_TEMPLATES` bucket | `flowstore/` and `flowtemplate/` removed | same requirement | `tasks.md` 3.2 grep count |
| D5.e | The override-expiry metric survives the removal | `service/__:__` (rehomed reporter, `tasks.md` 3.1) | same requirement, second scenario | `TestStreamOverrideExpiryReporterRegistersWithoutFlowService` |
| D5.f | `flow-authoring` is retired as a capability | eleven requirements REMOVED | `specs/flow-authoring/spec.md` | the named tests are deleted with their packages (`tasks.md` 7.4 table) |
| D5.g | Flow publication is removed from `component-runtime-config` | `service/flow_service.go` removed | `specs/component-runtime-config/spec.md` (1 REMOVED) | `config.Manager.PutComponentToKV` has no route or tool in front of it (`tasks.md` 3.2 grep) |
| ADR-100 §Consequences | BREAKING e2e gate | `tasks.md` 6.6 | — | `task e2e:core`, `task e2e:crud-tools`, `task e2e:agentic` |
| CARRIED | The duplicate graph build retained by PR #1101 (`GetFlowGraph` / `BuildFromRegistry` / `GET <components>/paths`) is re-judged here | `tasks.md` 3.3 | `specs/composition-validation/spec.md` `[~]` note carried from PR #1101, resolved or restated here | — |
