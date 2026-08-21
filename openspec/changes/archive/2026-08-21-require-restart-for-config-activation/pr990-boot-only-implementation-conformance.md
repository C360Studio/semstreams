# PR #990 boot-only implementation conformance

## Identity

- Historical PR head: `8f19ef3678a549913385b090e4de1766a7a43a27`
- Landed reconstruction: `8117858367e1cc9d1dc434d211989e7a2ed1e552` through PR #997
- Reconciliation baseline: `306d573c29d69ce4c052f3489d1f96970fb69ad4`
- Binding disposition SHA-256:
  `40b2534b604a14f64aacbb8f4db86bdbc38129f3f114e0ac40118c9f7259fc41`
- Passed inventory SHA-256:
  `5256057932030c7e854a3889ae2756fbec577870ee5e5c9c7c0e8ab86874541d`
- Status: ARCHIVED
- Credit: boot composition and Flow authoring only

Historical PR #990 remains evidence only. This ledger maps the separately landed PR #997 behavior to current main and
records the remaining archive proof.

## Current implementation evidence

| Claim | Current-main evidence | Status |
|---|---|---|
| One construction-time component configuration read | `service/component_manager.go:114-152` | LANDED |
| Fixed enabled boot set and Registry seal | `service/component_manager.go:252-327` | LANDED |
| Runtime handle and construction config remain ComponentManager-owned | `service/component_manager.go:930-963` | LANDED |
| Post-construction writes leave runtime identity and membership unchanged | `service/component_manager_boot_only_integration_test.go:20-123` | LANDED |
| Registry retains declarations without runtime handles | `component/registry.go:100-162` | LANDED |
| Registry rejects post-seal admission | `component/registry.go:233-260,330-360` | LANDED |
| Registry returns defensive declaration snapshots | `component/registry.go:683-745` | LANDED |
| Flow contains authoring and audit data without lifecycle state | `flowstore/flow.go:11-30` | LANDED |
| Engine validates and compiles without lifecycle authority | `engine/engine.go:18-25,49-112` | LANDED |
| Flow routes are CRUD, validation, publication, and name-keyed observations only | `service/flow_service.go:161-208` | LANDED |
| Publication is sorted, sequential, upsert-only, and progress-reporting | `service/flow_service.go:308-411` | LANDED |
| Publication behavior has focused source proof | `service/flow_publish_test.go:46-194` | LANDED |
| Retired lifecycle routes are absent | `service/flow_surface_test.go:12-41` | LANDED |
| Flow agent tools are CRUD-only | `processor/agentic-tools/executors/flows.go:12-127` | LANDED |
| Foreign shared-bucket identity fails Config Manager Start | `config/manager.go:172-223` | LANDED |
| Component writes are durable next-boot state | `config/manager.go:617-701` | LANDED |

## Retained capability deltas

Archive retains and promotes exactly:

- `component-discovery`
- `component-runtime-config`
- `flow-authoring`
- `framework-composition`
- `service-composition`

No Rule, readiness, lifecycle, shutdown, recovery, release, or tag-readiness requirement is promoted.

## Archive evidence

| Gate | Evidence | Status |
|---|---|---|
| Focused race proof | `go test -race ./component ./config ./engine ./flowstore ./service ./internal/maxdelivery` and `task test:integration` | PASS |
| Process-boundary activation | `go test -race -tags=integration ./service -run '^TestIntegration_ConfigBootActivationRequiresProcessRestart$' -count=1` | PASS |
| Core/CRUD E2E | `task e2e:core` (3/3) and `task e2e:crud-tools`; post-merge evidence | PASS |
| Repository gates | `task lint`; `go test -race ./...`; `go test ./test/contract/...`; `task schema:generate` with no schema/OpenAPI drift; `task openspec:validate` (51/51) | PASS |
| Independent review | SemStreams reviewer APPROVE after successful isolated replay of exactly five capabilities | PASS |

## Historical E2E timing

PR #997 was merged as a breaking change. This repository contains no durable artifact proving that relevant E2E ran
before that merge. This ledger does not and cannot claim that historical timing retroactively. Any E2E recorded during
this reconciliation is post-merge evidence for archive and tag confidence only.

## Completion rule

All proof and review gates are satisfied. Archive promotes only the five retained current-truth capabilities.
