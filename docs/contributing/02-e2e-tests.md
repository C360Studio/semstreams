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
# 5 E2E tasks - one per tier
task e2e:core        # Platform boots, data flows (~10s)
task e2e:structural  # Rules + PathRAG (~30s)
task e2e:statistical # BM25 + community detection (~60s)
task e2e:semantic    # Neural embeddings + LLM (~90s)

# Cleanup
task e2e:clean
```

## Test Tiers

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

## Docker Compose Files

All compose files are in `docker/compose/`:

| File | Purpose | Profiles |
|------|---------|----------|
| `e2e.yml` | Core E2E tests | - |
| `structural.yml` | Structural tier | - |
| `tiered.yml` | Statistical + Semantic | `statistical`, `semantic` |
| `federation.yml` | Edge-to-cloud federation | - |

## Directory Structure

```
test/e2e/
├── client/
│   ├── observability.go    # HTTP client for component API
│   ├── nats.go             # NATS KV validation
│   └── metrics.go          # Prometheus metrics client
├── config/
│   └── constants.go        # Test configuration
└── scenarios/
    ├── core_health.go
    ├── core_dataflow.go
    ├── semantic_basic.go
    ├── semantic_indexes.go
    ├── tiered.go           # Statistical + Semantic tiers
    └── tiered_structural.go  # Structural tier validation

cmd/e2e/
└── main.go                 # Test runner CLI

taskfiles/e2e/
├── common.yml              # Shared tasks (clean, check-ports)
├── core.yml                # Core protocol tests
├── structural.yml          # Structural tier
├── statistical.yml         # Statistical tier
├── semantic.yml            # Semantic tier
└── federation.yml          # Federation tests
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
