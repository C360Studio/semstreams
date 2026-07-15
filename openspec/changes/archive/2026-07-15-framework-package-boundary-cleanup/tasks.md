## 1. Architecture and governance

- [x] 1.1 Validate the live package/consumer ledger against SemConnect, SemDev, SemTeams, SemSource, and SemSpec.
- [x] 1.2 Record the framework admission rule and retained graph-research exception in current project guidance.
- [x] 1.3 Add ADR-075 to supersede the affected ADR-025/042/044/050 ownership and registration decisions, add only
  supersession pointers to those historical ADRs, and reaffirm ADR-045's graph-ergonomics decision.
- [x] 1.4 Publish the clean-break package, config, schema, and product-owner notice.

## 2. Composition-root tests

- [x] 2.1 Add failing tests that the core component registry excludes GitHub, OASF, directory, A2A, and SLIM.
- [x] 2.2 Add failing tests that core payload registration excludes OMS, GitHub, and research payloads while the
  selected graph-research composition registers its research payloads.
- [x] 2.3 Add failing tests that core tool registration excludes GitHub tools while direct graph tools remain.
- [x] 2.4 Add failing tests that the production binary excludes example processors and payloads.
- [x] 2.5 Add failing tests for atomic graph-research composition: absent means unadvertised; partial means boot
  failure; complete means the tool and result path are available.
- [x] 2.6 Add a `go list -deps` contract test proving a core-only composition excludes graph-research, product,
  optional-adapter, example, and tooling packages.

## 3. Explicit framework composition

- [x] 3.1 Make the core component registry an isolated import root and create a separate graph-research composition
  import root for the five research components.
- [x] 3.2 Make core payload registration an isolated import root and register research payloads only from the separate
  graph-research composition root.
- [x] 3.3 Make core tool registration an isolated import root; move the research executor/registration behind the
  graph-research composition root and remove ambient GitHub registration.
- [x] 3.4 Update `cmd/semstreams`, `cmd/e2e-semstreams`, example binaries, and OpenAPI/schema generation to select their
  intended composition explicitly.
- [x] 3.5 Implement fail-closed graph-research configuration validation and actionable errors.
- [x] 3.6 Exclude OpenTelemetry from core registration and require explicit optional-adapter selection.

## 4. Remove false and dead capabilities

- [x] 4.1 Delete the current A2A facade, config references, tests, and generated schema.
- [x] 4.2 Delete the SLIM facade, config references, tests, and generated schema.
- [x] 4.3 Remove the AGNTCY provider stub and AGNTCY-specific identity coupling from core durable agent state.
- [x] 4.4 Delete the placeholder parser package and stale StreamKit documentation.
- [x] 4.5 Make OpenTelemetry reject unsupported protocol configuration and prove successful counters require a real
  exporter.
- [x] 4.6 Delete unused `federation`, `subjects`, and `input/cli` packages and remove references that falsely present
  them as active framework APIs.

## 5. Product and example boundaries

- [x] 5.1 Remove OMS, GitHub, OASF, directory, and product vocabulary packages from framework-default composition.
- [x] 5.2 Remove IoT and document example registration from `cmd/semstreams`; retain it in E2E/example binaries.
- [x] 5.3 Remove product adapter schemas from the framework OpenAPI/catalog output.
- [x] 5.4 Produce SemConnect, SemDev, and SemTeams handoff inventories with exact removed imports/config names.
- [x] 5.5 Remove the product-owned framework package copies without shims after recording exact break notices; do not
  block the local change on downstream repository timing.

## 6. Verification

- [x] 6.1 Run focused registry, payload, tool, configuration, and OpenAPI/schema tests.
- [x] 6.2 Run `task lint` and `go test -race ./...`.
- [x] 6.3 Run `task schema:generate` and prove `schemas/` and `specs/` are clean and reduced as specified.
- [x] 6.4 Run `go test ./test/contract/...` and the integration suite with race detection.
- [x] 6.5 Run `task e2e:core`, `task e2e:agentic`, and `task e2e:research-graph`.
- [x] 6.6 Record SemConnect, SemDev, and SemTeams clean-break validation as a pre-v1 release gate, not a local merge
  gate.

## 7. Closeout

- [x] 7.1 Update the entity-ID, graph-index, and bounded-storage changes to use the corrected framework source/store
  inventory.
- [x] 7.2 Seed the `framework-composition` current-truth specification and archive this change.
- [x] 7.3 Re-read live OpenSpec status and record any remaining downstream blockers without retaining compatibility
  code in SemStreams.
