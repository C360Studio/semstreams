# End-to-End Testing

E2E tests validate SemStreams functionality in realistic deployment scenarios using Docker containers and real services.

## Philosophy

E2E tests follow the **Observer Pattern**: they run against real services in Docker containers, not mocks. Tests observe system behavior from the outside, just like production monitoring would.

### Key Principles

1. **Real Services**: Tests use actual NATS, graph processors, and embedding services
2. **Container Isolation**: Each test suite runs in isolated Docker Compose environments
3. **Observable Validation**: Tests query endpoints and KV buckets to verify behavior
4. **Graceful Degradation**: Tests validate fallback behavior when services are unavailable

## Quick Reference

```bash
# The four tiers this page details; `task --list` shows every tier task
task e2e:core        # Platform boots, data flows (~10s)
task e2e:structural  # Rules + PathRAG (~30s)
task e2e:statistical # BM25 + community detection (~60s)
task e2e:semantic    # Neural embeddings + LLM (~90s)

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
- Output components (file, HTTP POST, WebSocket)
- Data transformation validation

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
- All structural tier coverage
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
- All statistical tier coverage
- Neural embeddings (via SemEmbed)
- LLM summary quality
- Semantic search relevance

## Assertion Strategy

| Tier | What We Assert | What We DON'T Assert |
|------|----------------|---------------------|
| **Core** | Health endpoints, data flows | - |
| **Structural** | Entities in KV, predicates indexed, anomaly flags in index, PathRAG edges | LLM response quality |
| **Statistical** | Above + BM25 embeddings, communities detected | LLM summaries |
| **Semantic** | Above + LLM summary quality, semantic search relevance | - |

**Key insight**: Anomaly worker can run at structural tier with LLM, but we only assert on *index state* (flag exists), not LLM reasoning. LLM output assertions wait until semantic tier.

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

Full CI validation (no ML dependencies):

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
cd cmd/e2e && ./e2e --scenario tiered --variant core

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

All compose files are in `docker/compose/`. A tier boots the binary whose composition it proves: a tier proving the
production composition boots `cmd/semstreams`, and a tier that needs non-production registrations — examples,
fixtures, the mission workflow, a control responder — boots `cmd/e2e-semstreams`. An E2E-only hook that must run
*inside* the production composition lands in the binary its tier boots, behind that tier's build tag and, where it
must stay inert in the tier's other stages, an environment variable only that tier sets.

**The source of truth is the tier table in `openspec/specs/payload-registry/spec.md`** — until this change archives,
the table lives in `openspec/changes/agentrun-fanout-settlement/specs/payload-registry/spec.md`, and
`test/contract/e2e_tier_binary_contract_test.go` reads the live spec when it carries the table, otherwise exactly one
in-flight delta. That table
additionally carries each tier's gate, its E2E-only hooks, and the synthetic types it stamps, and the test re-reads it
against these compose files and `docker/Dockerfile` on every run. The list below is the navigation copy — when the two
disagree, the spec is right.

Twelve compose services, thirteen `e2e:<tier>` tasks. The units differ on purpose: `core` runs in two phases
against two services (rows 1 and 2), and rows 2 and 4 each serve two tasks off one service.

| Tier (`task e2e:<tier>`) | Compose file : service | Dockerfile target | Binary |
|---|---|---|---|
| `core` phase 1 | `e2e.yml` : `semstreams` | `production` | `cmd/semstreams` |
| `core` phase 2, `lessons` | `e2e.yml` : `semstreams-fixtures` (profile `fixtures`) | `e2e` | `cmd/e2e-semstreams` |
| `structural` | `tiered.yml` : `semstreams-structural` (profile `structural`) | `e2e` | `cmd/e2e-semstreams` |
| `statistical`, `throughput` | `tiered.yml` : `semstreams` (profile `statistical`) | `e2e` | `cmd/e2e-semstreams` |
| `semantic` (`:8b`, `:frontier` overlays) | `tiered.yml` : `semstreams-ml` (profile `semantic`) | `e2e` | `cmd/e2e-semstreams` |
| `lifecycle` | `lifecycle.yml` : `semstreams` | `e2e` | `cmd/e2e-semstreams` |
| `ops` | `ops.yml` : `semstreams` | `e2e` | `cmd/e2e-semstreams` |
| `research-graph` | `research-graph.yml` : `semstreams` | `e2e` | `cmd/e2e-semstreams` |
| `crud-tools` | `crud-tools.yml` : `semstreams` | `production` | `cmd/semstreams` |
| `deep-research` | `deep-research.yml` : `semstreams` | `production` | `cmd/semstreams` |
| `agentic` | `agentic.yml` : `semstreams` | `e2e-process-barrier` | `cmd/semstreams` (tagged) |
| `slow-consumer` | `e2e-slow-consumer.yml` : `semstreams` | `e2e-slow-consumer` | `cmd/semstreams` (tagged) |

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

### PR Checks

```yaml
steps:
  - task e2e:core
  - task e2e:structural
```

### Main Branch

```yaml
steps:
  - task e2e:core
  - task e2e:structural
  - task e2e:statistical
```

### Release

```yaml
steps:
  - task e2e:semantic
```

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
semantic or agentic tier on a `!` PR; the per-PR gate is gh#1117, the nightly run gh#769.

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
