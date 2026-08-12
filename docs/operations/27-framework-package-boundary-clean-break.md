# Framework Package Boundary Clean Break

**Historical cutover evidence — not an active release procedure.**

This document records the 2026 cutover plan. Stable release adoption starts on newly provisioned NATS storage; do not
execute this body as a release gate. Its no-shim conclusions remain evidence. Typed graph-poison recovery is governed
by [operations 17](17-predicate-cutover-clean-wipe.md) and [operations 33](33-graph-poison-response-runbook.md).

## Audience and timing

This notice is for SemConnect, SemDev, SemTeams, and maintainers of binaries that compose SemStreams. The boundary
change is intentionally breaking before v1. SemStreams will not provide deprecated import paths, compatibility
registries, dual registrations, or copied beta-state readers.

Downstream updates may land after the SemStreams cleanup. They are pre-v1 release gates: each owner must consume the
notice, update its composition, and run its affected validation before the v1 release.

## SemConnect owner inventory

SemConnect becomes the source owner for the complete OGC Connected Systems bundle. The following SemStreams import
paths are removed:

- `github.com/c360studio/semstreams/message/oms`
- `github.com/c360studio/semstreams/parser/sensorml`
- `github.com/c360studio/semstreams/pkg/swecommon`
- `github.com/c360studio/semstreams/vocabulary/csapi`
- `github.com/c360studio/semstreams/vocabulary/oms`
- `github.com/c360studio/semstreams/vocabulary/sosa`
- `github.com/c360studio/semstreams/vocabulary/swe`

SemConnect must own the equivalent packages, tests, canonical fixtures, vocabulary registration, and payload
registration. OMS payloads will no longer arrive through SemStreams' ambient builtin registration; SemConnect must
explicitly register payload type `ogc.oms.v3` in each binary that decodes it.

No SemStreams component schema or flow configuration is transferred for this bundle. SemConnect must update every
source import, generated artifact, fixture, and conformance path that names the removed packages. If the adopted
package shape or entity-ID contract changes persisted beta records, wipe the affected NATS state, restart, and reseed
from updated SemConnect sources. Do not preserve old SemStreams import aliases.

## SemDev owner inventory

SemDev becomes the source owner for GitHub webhook ingestion and forge tools. The following SemStreams surfaces are
removed:

- package `github.com/c360studio/semstreams/input/github-webhook`
- `processor/agentic-tools/executors/github_client.go`
- `processor/agentic-tools/executors/github_read.go`
- `processor/agentic-tools/executors/github_write.go`
- `processor/agentic-tools/executors/register_github.go`
- component name `github_webhook`
- payloads `github.issue_event.v1`, `github.pr_event.v1`, `github.review_event.v1`, and
  `github.comment_event.v1`
- executor registry entries `github_read` and `github_write`
- advertised tools `github_get_issue`, `github_list_issues`, `github_search_issues`, `github_get_pr`,
  `github_get_file`, `github_create_branch`, `github_commit_file`, `github_create_pr`, `github_add_comment`, and
  `github_add_label`
- `configs/github-pr-workflow.json`
- `configs/rules/github-pr-workflow/`
- `schemas/github_webhook.v1.json`

SemDev must move the webhook types and GitHub executors behind its existing `internal/boot` composition seam, update
its parity and schema-conformance tests, and own the workflow/rule policy. It must stop relying on
`componentregistry.Register`, `payloadbuiltins.Register`, or `executors.RegisterBuiltins` to add GitHub behavior
transitively.

After the move, regenerate SemDev's schemas and OpenAPI artifacts and run both production/E2E binary-parity tests plus
the GitHub intake and tool conformance suites. Wipe/reseed only beta state containing the moved payload envelopes or
workflow records if the owner-side copy changes their shape.

## SemTeams owner inventory

SemTeams becomes the source owner for OASF projection and AGNTCY directory registration. The following SemStreams
surfaces are removed from framework ownership:

- package `github.com/c360studio/semstreams/processor/oasf-generator`
- package `github.com/c360studio/semstreams/output/directory-bridge`
- package `github.com/c360studio/semstreams/vocabulary/oasf`
- component names `oasf-generator` and `directory-bridge`
- `schemas/oasf-generator.v1.json`
- `schemas/directory-bridge.v1.json`
- the `oasf-generator` and `directory-bridge` entries in `configs/agentic.json`

SemTeams must register and maintain these packages in its product composition, generate their schemas from the owning
source, and own cleanup and storage bounds for every OASF or directory record they derive. Its
`configs/flow-bootstrap.json`, `configs/e2e-flow-bootstrap.json`, OpenAPI, generated UI types, and component-catalog
tests must no longer obtain these contracts transitively from SemStreams.

The current A2A and SLIM facades are deleted, not transferred. SemTeams must remove:

- packages `github.com/c360studio/semstreams/input/a2a` and
  `github.com/c360studio/semstreams/input/slim`
- component names `a2a-adapter` and `slim-bridge`
- `schemas/a2a-adapter.v1.json` and `schemas/slim-bridge.v1.json`
- matching references in `configs/flow-bootstrap.json`, `configs/e2e-flow-bootstrap.json`, OpenAPI, and generated UI
  types

Do not recreate these facades by copying their implementations. A future A2A or SLIM adapter requires a conformant
transport, authentication, cancellation, status, and lifecycle contract with an explicit owner.

The AGNTCY provider stub in `agentic/identity/agntcy_provider.go` and its AGNTCY-specific durable loop coupling are
removed rather than handed off. If SemTeams needs AGNTCY identity, it must implement and validate the real product-side
provider. No caller should treat the old stub as a migration source.

SemStreams also removes the `a2a-adapter` entry from `configs/agentic.json`; no SLIM entry was active there.

## SemSource consumer inventory

The live SemSource Go source has no remaining import of the deleted `semstreams/federation` package. Its contributor
guidance and completed migration note still claim that `federation.Entity`, `federation.Event`, `federation.Edge`,
`federation.Provenance`, and `federation.Store` come directly from SemStreams. SemSource must correct those stale
documents before v1 so a new maintainer does not reintroduce the removed dependency. This is a documentation release
gate, not a SemStreams compatibility requirement.

## SemSpec consumer inventory

SemSpec has no direct Go import of a removed package, but its checked-in generated UI type catalog still advertises
`a2a-adapter.v1`, `directory-bridge.v1`, `github_webhook.v1`, `oasf-generator.v1`, and `slim-bridge.v1`. SemSpec must
regenerate its SemStreams-derived TypeScript types from the reduced framework OpenAPI document and prove its UI and
catalog tests no longer present those components. Product-owned schemas must come from their owning product API, not
remain in SemSpec through an old SemStreams snapshot.

## Live downstream evidence

The pre-v1 handoff ledger was checked against the current sibling repositories when this break was prepared:

- SemConnect directly imports the removed SensorML, SWE Common, OMS, SOSA, and CS API packages throughout its
  Connected Systems gateway. Those are compile-time migration points, not merely generated artifacts.
- SemDev directly imports `input/github-webhook` in its intake and parity tests and still describes the framework
  GitHub executors in its boot seam and walking-skeleton change. Its existing product-owned
  `github_list_comments` tool is unaffected.
- SemTeams still names OASF generator, directory bridge, and A2A in both bootstrap configs and retains generated
  A2A, directory, GitHub webhook, OASF, and SLIM schemas and UI types. It must delete the false A2A/SLIM surfaces and
  re-home only the OASF/directory behavior it actually owns.
- SemSource has only the stale federation documentation described above; no live Go import remains.
- SemSpec has the stale generated component catalog described above; no live Go import remains.

These findings are the initial downstream blocker snapshot. Owners may wipe and reseed beta state after changing
their source composition. SemStreams must not add aliases, copied packages, or ambient registrations to make an
unchecked downstream tree compile.

## Framework-wide removals and composition changes

The following unused or misleading packages are deleted with no replacement:

- `federation`
- `subjects`
- `input/cli`
- `processor/parser`

The production `cmd/semstreams` binary stops registering `examples/processors/iot_sensor` and
`examples/processors/document`. Those examples remain available to E2E and example composition roots.

OpenTelemetry remains in SemStreams but is no longer part of core registration. A binary that needs component
`otel-exporter` and schema `schemas/otel-exporter.v1.json` must select the optional adapter explicitly. Unsupported
export protocols fail startup rather than reporting successful no-op export.

Graph research remains framework-owned. Binaries select it through its dedicated composition root, which adds the
research payloads, five `research-graph-*` components, R0-R6 rules, `research_graph`, ObjectStore evidence, and result
retrieval as one capability. A product must not copy this chain merely because it is no longer linked into core-only
binaries.

## Validation checklist

Each affected product owner must record:

1. zero imports of removed SemStreams packages;
2. an explicit component, payload, and tool composition inventory;
3. regenerated schemas, OpenAPI, and generated client types with no stale facades;
4. green unit, integration, contract, and affected product E2E suites; and
5. any required beta NATS wipe/reseed evidence.

SemStreams records those product results as pre-v1 release evidence. Missing downstream validation does not justify a
compatibility shim in the framework.
