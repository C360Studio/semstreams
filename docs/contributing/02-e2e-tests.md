# End-to-End Testing

E2E tests validate SemStreams functionality in realistic deployment scenarios using Docker containers and real services.

## Philosophy

E2E tests observe the assembled application through its real HTTP, NATS, graph and file boundaries.
Some dependencies, including the agentic model, use scripted fixtures. Each run must identify those fixtures and
the behavior they exercise; passing a scripted model flow does not establish live model quality.

### Key Principles

1. **Real Services**: Tests use actual NATS, graph processors, and embedding services
2. **Container Isolation**: Each test suite runs in isolated Docker Compose environments
3. **Observable Validation**: Tests query endpoints and KV buckets to verify behavior
4. **Explicit scope**: Required checks fail when observation is unavailable; declared diagnostics retain their outcome.
5. **Independent expectations**: Compare consumer-visible results with identities and values retained from inputs.

## Quick Reference

```bash
# Individual tiers; `task --list` shows every tier task
task e2e:core        # Platform boots, data flows (~10s)
task e2e:structural  # Rules + PathRAG (~30s)
task e2e:statistical # BM25 + community detection (~60s)
task e2e:semantic    # Neural embeddings + LLM (~90s)
task e2e:agentic     # Scripted model, tools, streaming and recovery
task e2e:core-inference-agentic # Core + structural + statistical + semantic + agentic

# Cleanup
task e2e:clean
```

## Test Tiers

The four sections below detail the four oldest tiers. [Every tier and the binary it boots](#every-tier-and-the-binary-it-boots)
lists all thirteen.

### Core (`task e2e:core`)

Platform boots, data flows. Validates basic health and dataflow.

| Duration | Purpose | Dependencies |
|----------|---------|--------------|
| ~10s | Component health + data pipeline | NATS only |

**Coverage**:
- UDP input component
- JSON processors (filter, map)
- Component health for configured outputs (file, HTTP POST, WebSocket); dataflow proof observes the file sink
- Run-correlated pass-through count, content and identity under `configs/protocol-flow.json`

### Structural (`task e2e:structural`)

Rules + PathRAG. Deterministic behavior, no embeddings or anomaly detection.

| Duration | Purpose | Dependencies |
|----------|---------|--------------|
| ~30s | Stateful rules + PathRAG | NATS only |

**Coverage**:
- Stateful rules with OnEnter/OnExit
- Dynamic graph manipulation (add_triple/remove_triple)
- Alert generation
- PathRAG on explicit edges
- Anomaly flags in index (assertion on index state, not LLM output)

### Statistical (`task e2e:statistical`)

BM25 + community detection. No external ML services required.

| Duration | Purpose | Dependencies |
|----------|---------|--------------|
| ~60s | BM25 embeddings + LPA communities | NATS only |

**Coverage**:
- Shared graph-path checks selected for the statistical variant; structural zero-ML checks remain separate
- BM25 embedding generation
- Community detection (Label Propagation)
- Keyword search
- TF-IDF summaries

### Semantic (`task e2e:semantic`)

Neural embeddings + LLM. Full ML stack validation.

| Duration | Purpose | Dependencies |
|----------|---------|--------------|
| ~90s | Neural embeddings + LLM summaries | NATS + SemEmbed + SemInstruct |

**Coverage**:
- Shared graph-path checks selected for the semantic variant; this does not substitute for every statistical check
- Neural embeddings (via SemEmbed)
- Controlled semantic query identity
- Summary and partition quality diagnostics; these are not model-quality acceptance thresholds

## Named Required Evidence

A complete report proves only its declared required set for that invocation. It does not claim that every capability
or every historical assertion in a tier has been audited. Existing fatal checks remain fatal when a scenario adopts
named evidence. Aggregate success, a nonzero assertion count and a callback returning nil are insufficient on their own.

The `e2e-evidence` capability contract governs declarations, observation identity, finalization and persistence.
The [testing policy](01-testing.md#testing-discipline) governs independent expectations and sensitivity evidence.

### Selection and Scope

| Selection | Named proof scope |
|---|---|
| CLI default / `--scenario all` | Core health and core dataflow only |
| `task e2e:core` | Core scenarios plus readiness, heartbeat, authority, shutdown, early cancellation, refusal and graph identity |
| Structural | Required components, graph identity, zero embedding execution and zero clustering runs |
| Statistical | Required components, graph identity and controlled search fixture identity |
| Semantic | Required components, graph identity, SemEmbed availability and controlled query identity |
| Agentic | Matching terminal task/loop, tool request/result and streaming chunks |
| Slow-consumer | Each existing attribution condition, retained as an individual required check |

`task e2e:core-inference-agentic` runs core, structural, statistical, semantic and agentic in sequence. `e2e:all`
is its alias and prints that scope. The composite excludes slow-consumer (which has its own CI job), lessons,
research-graph direct/execute, deep-research, CRUD-tools, ops, lifecycle, throughput, heavy/fallback semantic variants
and the live OpenAI adapter. An exclusion does not remove a separate CI or release obligation.

CLI `semantic` selects the semantic variant explicitly; `rules` selects structural. Both use the same resolver and
preserve caller flags as `tiered --variant ...`. Unknown selections and variants fail. Legacy scenarios outside the
adopted set retain execution behavior and are reported as **unattested**; their old counts cannot fill a required member.

### Author a Required Check

Declare stable check IDs and required/diagnostic classification before setup or observation. Keep the declaration in
one scenario-owned method used by both the runner and scenario. Pass run/member identity through explicit scenario
configuration. Repeated intentional rounds need distinct declared IDs; recording the same ID twice is an error.

At each observation site:

1. Retain the expected identity and value from the controlled input or governing contract.
2. Read the actual consumer-visible result and compare it with that expectation.
3. Record `passed`, `failed` or `skipped` for the declared ID, with the run/member identity and bounded evidence.
4. Preserve errors and partial observations for finalization, including observation transport failures.

Evidence contains expected/observed summaries, identities and artifact references, never secrets or full payloads.
A failed or skipped observation carries a reason. Shell assertions record their comparison at the assertion site;
a banner or successful shell block is not an observation. The report command only records and finalizes evidence;
Task owns command order, processes and cleanup.

The shared `Result` finalizer rejects empty required sets, undeclared or duplicate observations, mismatched identities,
and missing, skipped or failed required checks. Recording errors stay fatal even if a caller ignores an error return.
`AssertionsRun` counts evaluated required observations (passed plus failed); it excludes diagnostics, missing and
skipped checks. It is supplemental to the named set. Setup, execution, validation, cleanup and required-write errors
also prevent success. Domain metadata is projected after that final outcome.

Diagnostics are explicit observations with their real outcome. Agentic TTFT, B0 thematic quality and B2 partition
co-location remain diagnostics. Their failure neither fills a required obligation nor becomes a passed measurement.
Required streaming chunks remain distinct from diagnostic TTFT.

### Keep the Oracle Independent

Core's current filter criteria and mappings are empty. Retain each successfully sent run marker, sequence and value;
require at least `MinProcessed` distinct sent IDs in that run's output and validate every selected record against its
sent value. Duplicate lines cannot increase the distinct count. Retrieval failure cannot substitute component health.
This proves a minimum of correct pass-through output, without claiming complete UDP delivery, filtering, exactly-once
output or ordering.

For statistical/semantic search, derive expected entity IDs from a controlled submitted fixture and compare them with
the actual query response. Recomputing search statistics from the response does not establish the expected identity.
For agentic checks, bind terminal/tool evidence to the submitted task, loop and request. A process-wide counter alone
cannot establish attribution; a controlled baseline/delta needs isolated execution and matching request evidence.
Structural zero clustering means no clustering execution, not an empty store of community records.

Challenge both observation and propagation. Healthy controls need counterparts for absent output, foreign identity,
transport error and skipped observation. For acceptance logic, exercise omission, duplicate/conflicting observations,
empty membership, cleanup failure and write failure. Use the policy's bounded properties and compiled mutations to
show that the intended assertion detects the plausible fault. A shell stub proves wrapper exit handling; assembled
E2E runs supply application behavior evidence.

### Read and Retain a Run

The existing `TestRun` writer owns versioned run records. Each invocation has a unique run ID and file; initial and
final writes atomically replace that invocation's aggregate. Child members retain distinct identities. A failed run
keeps partial observations and the missing set. Typed tier exports are analysis projections, not acceptance authority.
Older reports remain readable as unattested.

Run records bind selection, required members, exclusions, executed test/child arguments, absolute paths, UTC times,
exit status and provenance. Task aggregates identify the resolved Task target; they explicitly mark original launcher
arguments unavailable when those were not observed. For example, a report naming `e2e:core` does not claim to remember
whether the caller added `--verbose` or `--parallel`. This declared launcher-history limit does not weaken required
checks or permit missing test/application provenance.
Provenance distinguishes source/dirty inputs, the runner executable, the actual application image/binary, and selected
Compose/config/fixture/settings inputs. Retained manifests identify constituent files and digests. Unavailable identity
has an explicit reason and prevents full proof; a guessed image tag or runner SHA cannot stand in for the application.

Reporting starts after the existing build and port-preflight prerequisites, including cleanup before probing ports.
A direct task that fails before initialization has outer status/logs only; no JSON is promised. An already initialized
composite records that child's failure and missing report. After initialization, controlled exits attempt final evidence
after cleanup, retaining both the original failure and any cleanup/write failure. Process or host death may leave an
incomplete envelope. Incomplete artifacts never satisfy a required suite.

CI adoption must retain available evidence even on failure and expose artifact absence or upload failure. The current
required-check change has not yet enabled those upload steps. Direct remote inspection may retain useful observations
while lacking application provenance; it remains incomplete proof.
Release candidate selection and tag authorization still belong to
[release-candidate-proof](../../openspec/specs/release-candidate-proof/spec.md).

A probe that hits its client deadline fails with the transport error verbatim; an empty result fails with the stage's own sentence; the two never share a message.

## Test Selection Guide

### During Development

Fast feedback for iterative changes:

```bash
task e2e:core  # ~10s, platform basics
```

### Pre-Commit

Validate core functionality:

```bash
task e2e:core
task e2e:structural
```

### Pre-Merge

Example local selection for changes spanning core and graph paths (no ML dependencies):

```bash
task e2e:core
task e2e:structural
task e2e:statistical
```

### Full Validation

Complete stack with ML services:

```bash
task e2e:semantic
```

## Running Tests

### Using Task Runner

```bash
# List available e2e tasks
task --list | grep e2e

# Run specific tier
task e2e:structural

# Run with cleanup first
task e2e:clean && task e2e:core
```

### Direct CLI

```bash
# Build first
task build:e2e

# List available scenarios
cd cmd/e2e && ./e2e --list

# Run specific scenario
cd cmd/e2e && ./e2e --scenario tiered --variant statistical

# With verbose output
cd cmd/e2e && ./e2e --scenario tiered --verbose
```

### Debug Mode

Leave containers running for inspection:

```bash
task e2e:core:debug
docker logs -f semstreams-e2e-app
```

## Every tier and the binary it boots

All compose files are in `docker/compose/`. Both framework binaries boot through one composition function,
`internal/boot`; they differ only by the options `cmd/e2e-semstreams` enables from `SEMSTREAMS_E2E_*` environment
variables (`internal/e2eboot`). A tier proving the shipped binary boots `cmd/semstreams` through the `production`
target, which can enable no option. Every E2E-only registration or hook — examples and fixtures, the mission workflow,
the lifecycle seed, a control responder, a probe, a barrier — is an option its tier's compose service enables on the
`e2e` target by setting exactly that option's variable to a nonempty value; with none set, the E2E binary's boot
options are the production options. No build tag gates an E2E-only hook, and `docker/Dockerfile` has exactly two
runnable targets.

**The source of truth is the tier table in `openspec/specs/payload-registry/spec.md`**, and
`test/contract/e2e_tier_binary_contract_test.go` re-reads it against these compose files and `docker/Dockerfile` on
every run. While an in-flight change carries a restated copy of the table in its delta, exactly one such delta governs
instead (it describes the artifacts that change is editing); two are refused as ambiguous. That table additionally
carries each tier's E2E-only hooks and the synthetic types it stamps. The list below is the navigation copy — when the
two disagree, the spec is right.

Twelve compose services, thirteen `e2e:<tier>` tasks. The units differ on purpose: `core` runs in two phases
against two services (rows 1 and 2), and rows 2 and 4 each serve two tasks off one service.

| Tier (`task e2e:<tier>`) | Compose file : service | Dockerfile target | Binary | Gate (`SEMSTREAMS_E2E_*`) |
|---|---|---|---|---|
| `core` phase 1 | `e2e.yml` : `semstreams` | `production` | `cmd/semstreams` | none |
| `core` phase 2, `lessons` | `e2e.yml` : `semstreams-fixtures` (profile `fixtures`) | `e2e` | `cmd/e2e-semstreams` | `EXAMPLES` |
| `structural` | `tiered.yml` : `semstreams-structural` (profile `structural`) | `e2e` | `cmd/e2e-semstreams` | `EXAMPLES` |
| `statistical`, `throughput` | `tiered.yml` : `semstreams` (profile `statistical`) | `e2e` | `cmd/e2e-semstreams` | `EXAMPLES` |
| `semantic` (`:8b`, `:frontier` overlays) | `tiered.yml` : `semstreams-ml` (profile `semantic`) | `e2e` | `cmd/e2e-semstreams` | `EXAMPLES` |
| `lifecycle` | `lifecycle.yml` : `semstreams` | `e2e` | `cmd/e2e-semstreams` | `MISSION`, `LIFECYCLE_SEED` |
| `ops` | `ops.yml` : `semstreams` | `e2e` | `cmd/e2e-semstreams` | `LESSON_CURATION` |
| `research-graph` | `research-graph.yml` : `semstreams` | `e2e` | `cmd/e2e-semstreams` | `EXAMPLES` |
| `crud-tools` | `crud-tools.yml` : `semstreams` | `production` | `cmd/semstreams` | none |
| `deep-research` | `deep-research.yml` : `semstreams` | `production` | `cmd/semstreams` | none |
| `agentic` | `agentic.yml` : `semstreams` | `e2e` | `cmd/e2e-semstreams` | `PROCESS_BARRIER`, `MILESTONE_PROBE` |
| `slow-consumer` | `e2e-slow-consumer.yml` : `semstreams` | `e2e` | `cmd/e2e-semstreams` | `SLOW_CONSUMER` |

`task e2e:openai-responses` is the fourteenth task and is not in this table: it is a live wire test against the paid
API with no container of its own. Each tier file above defines its own `nats:`; `services.yml` carries the shared
side services (semembed, seminstruct, step-ca, prometheus, grafana). `tiered.8b.yml` and `tiered.frontier.yml` are
model overlays for the semantic tier: they build no SemStreams image, and because compose merges `environment:`
across `-f` files, a variable set on their `semstreams-ml` block still reaches the running container — which is why
the contract test sweeps every compose file's raw text for `SEMSTREAMS_E2E_*`, not just the services that build.

## Directory Structure

```
test/e2e/
├── client/                 # Observability HTTP, NATS KV and Prometheus clients
├── config/                 # constants.go, per-tier validation thresholds
├── harness/                # E2E-only server-side hooks (see the tier table above)
│   ├── lessoncuration/     # ops: lesson-promotion control responder
│   ├── milestoneprobe/     # agentic: milestone settlement probe
│   └── processbarrier/     # agentic: process-replacement barrier
├── mock/                   # mock LLM image
└── scenarios/              # one package or file per scenario (core_health.go, ops/, agentic/, ...)

cmd/e2e/
└── main.go                 # Test runner CLI

taskfiles/e2e/
├── common.yml              # Shared tasks (clean, check-ports, reserve-ports)
├── openai-responses.yml    # The live paid-API test; no container, no table row
└── <tier>.yml              # One file per tier in the table above
```

## KV Validation

Tests validate actual data storage, not just component health.

### Validation Thresholds

```go
const (
    DefaultMinStorageRate   = 0.80  // 80% of sent entities must be stored
    DefaultValidationTimeout = 5 * time.Second
)
```

### Index Validation by Tier

| Tier | Indexes Validated |
|------|-------------------|
| Structural | ENTITY_STATES, PREDICATE_INDEX, SPATIAL_INDEX, TEMPORAL_INDEX, ALIAS_INDEX, INCOMING_INDEX, OUTGOING_INDEX |
| Statistical | All above + EMBEDDING_INDEX (BM25), COMMUNITY_INDEX |
| Semantic | All above + EMBEDDING_INDEX (neural), enhanced communities |

## Troubleshooting

### Port Conflicts

```bash
task e2e:check-ports
lsof -ti:8080 | xargs kill -9
task e2e:clean
```

If the preflight passed and the bind still failed, the port was probably taken by the kernel's ephemeral
allocator between the two — see [Troubleshooting → Port conflicts](../operations/02-troubleshooting.md) and
`task e2e:reserve-ports` (gh#1279).

### Services Not Healthy

```bash
docker logs semstreams-e2e-app
docker compose -f docker/compose/tiered.yml ps
```

### NATS Connection Failed

```bash
docker ps | grep nats
docker logs semstreams-tiered-nats
```

### Storage Rate Below Threshold

Check graph processor logs for errors. Increase timeout if processing is slow.

## CI Integration

The [E2E Ladder workflow](../../.github/workflows/e2e-ladder.yml) runs statistical and slow-consumer as separate jobs
on pull requests and explicit workflow dispatch. Both retain available run evidence on success and failure.
This describes current scheduling; the five-family local composite does not add CI jobs.

A statistical run does not establish core shutdown/cancellation proof, structural zero-ML absence, or agentic proof.
Select additional local tiers according to the changed path and existing release obligations. Functional E2E is not
scheduled nightly. Semantic CI is tracked in #1117; agentic/CRUD CI and proof retain #769/#1128 ownership.

Release selection, exact candidate identity and required evidence belong to
[release-candidate-proof](../../openspec/specs/release-candidate-proof/spec.md). One semantic run or a locally green
composite does not by itself authorize a release.

## Breaking Changes Require an E2E Tier Before Merge

Any commit or tag marked **BREAKING** in the changelog or commit message (a `!` after the type/scope) MUST have at
least one relevant E2E tier green BEFORE the breaking commit lands on main. Unit and integration tests do not
exercise the full ingest → entity → graph store → query path; registry singleton retirements and similar migrations
can leave a sister binary half-migrated and silently break every flow that uses it.

Concrete case (2026-05-07): beta.18 retired the payload-registry singleton. `cmd/e2e-semstreams/main.go` got the
migration; `cmd/semstreams/main.go` did not. Three months of beta releases shipped on top of a silently broken
Docker semantic stack because nobody ran `task e2e:semantic` on main between the migration and the forensic
discovery.

Before tagging anything labeled BREAKING:

```bash
task e2e:semantic            # Or whichever tier covers the touched path
# Confirm green. If no tier covers the path, that is a coverage gap: file it before tagging.
```

After landing a registry-retirement-style migration (singletons, `init()` shims, factory + payload split), grep for
every binary that imports the migrated package and verify each has the explicit registration call:

```bash
grep -rn "iotsensor\." cmd/   # Or whichever package was migrated
```

If only `cmd/e2e-semstreams` has it, the framework binary is half-migrated. Follow the
[payload registration checklist](../../.agents/skills/new-payload/SKILL.md). The per-PR ladder does not yet run the
semantic or agentic tier on a `!` PR. Semantic CI remains #1117; agentic/CRUD CI and proof remain #769/#1128.
Those open items do not waive the relevant local E2E requirement before landing.

## External Dependencies

### SemEmbed

Lightweight Rust embedding service using fastembed-rs.

| Property | Value |
|----------|-------|
| Port | 8081 |
| Model | BAAI/bge-small-en-v1.5 |
| API | OpenAI-compatible /v1/embeddings |
| Used by | semantic tier |

### SemInstruct

OpenAI-compatible LLM proxy for summarization.

| Property | Value |
|----------|-------|
| Port | 8083 |
| Backend | shimmy or OpenAI |
| Used by | semantic tier |

## Related Documentation

- [Testing Patterns](01-testing.md) - Unit and integration test patterns
