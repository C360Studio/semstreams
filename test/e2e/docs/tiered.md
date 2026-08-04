# tiered Scenario

## Purpose

The unified e2e test scenario covering all three tiers: Structural, Statistical, and Semantic. This single scenario replaces the previous separate scenarios with a variant-based approach.

## Progressive Enhancement Model

**SemStreams is about progressive enhancement.** Each tier builds on the previous, adding capabilities while inheriting all lower-tier features:

| Tier | Variant | Includes | Adds | Dependencies |
|------|---------|----------|------|--------------|
| **0** | `structural` | Foundation | Entity storage, graph indexes, rules, PathRAG | NATS |
| **1** | `statistical` | Tier 0 + | BM25 embeddings, LPA communities, semantic search | NATS |
| **2** | `semantic` | Tier 1 + | Neural embeddings, LLM summaries, GraphRAG | NATS + SemEmbed + SemInstruct |

### Tier 0: Structural (Foundation)
- Entity storage and retrieval
- Graph relationship indexes (incoming, outgoing, predicate, alias, spatial, temporal)
- Rules engine with state transitions (OnEnter/OnExit)
- **PathRAG graph traversal** (pure graph, no ML)

### Tier 1: Statistical (Tier 0 + Search)
- All Tier 0 capabilities
- BM25 lexical embeddings
- LPA community detection
- Semantic search with scoring
- Embedding fallback handling

### Tier 2: Semantic (Tier 1 + ML)
- All Tier 1 capabilities
- Neural embeddings (SemEmbed)
- LLM-enhanced community summaries
- **GraphRAG local/global queries**

## Invocation

```bash
# Structural tier (rules-only, no embeddings)
task e2e:structural
./e2e --scenario tiered --variant structural

# Statistical tier (BM25, no external ML)
task e2e:statistical  
./e2e --scenario tiered --variant statistical

# Semantic tier (Neural embeddings + LLM)
task e2e:semantic
./e2e --scenario tiered --variant semantic
```

## Variant Details

### Structural Variant (`--variant structural`)

**Purpose**: Validate rules-only processing with ZERO ML inference.

**Key Assertions**:
- `ExpectedEmbeddings = 0` - No embeddings generated
- `ExpectedClusters = 0` - No clustering occurred
- OnEnter/OnExit rule transitions fire correctly
- Dynamic graph modifications (add_triple/remove_triple)

**Stages** (structural-only):
1. `validate-zero-embeddings` - Asserts embedding count = 0
2. `validate-zero-clusters` - Asserts clustering runs = 0
3. `validate-rule-transitions` - Validates OnEnter/OnExit state changes

**Use Case**: CI fast-path, deterministic validation, rules engine testing.

### Statistical Variant (`--variant statistical`)

**Purpose**: Validate BM25 lexical embeddings and community detection without external ML services.

**Key Assertions**:
- BM25 embeddings generated
- Communities detected via LPA
- All 7 indexes populated
- Search returns results

**Stages**: Full 16-stage validation minus LLM-specific checks.

**Use Case**: CI comprehensive, local development, no GPU required.

### Semantic Variant (`--variant semantic`)

**Purpose**: Full ML stack validation with neural embeddings and LLM summaries.

**Key Assertions**:
- Neural embeddings via SemEmbed
- LLM-enhanced community summaries
- Semantic search quality
- All 7 indexes populated

**Stages**: All 16 stages including LLM comparison.

**Use Case**: Release validation, full ML pipeline testing.

## Stage Matrix

Stages are organized by tier following the progressive enhancement model:

| Stage | Tier | Structural | Statistical | Semantic |
|-------|------|------------|-------------|----------|
| **Common Setup** | | | | |
| verify-components | all | ✓ | ✓ | ✓ |
| send-mixed-data | all | ✓ | ✓ | ✓ |
| validate-processing | all | ✓ | ✓ | ✓ |
| verify-entity-count | all | ✓ | ✓ | ✓ |
| verify-entity-retrieval | all | ✓ | ✓ | ✓ |
| validate-entity-structure | all | ✓ | ✓ | ✓ |
| verify-index-population | all | ✓ | ✓ | ✓ |
| **Tier 0: Structural** | | | | |
| test-pathrag | 0 | ✓ | ✓ | ✓ |
| test-pathrag-boundary | 0 | ✓ | ✓ | ✓ |
| **Tier 0 Only: Zero-ML Validation** | | | | |
| validate-zero-embeddings | 0 | ✓ | - | - |
| validate-zero-clusters | 0 | ✓ | - | - |
| validate-rule-transitions | 0 | ✓ | - | - |
| **Tier 1+: Statistical** | | | | |
| wait-for-embeddings | 1+ | - | ✓ | ✓ |
| verify-search-quality | 1+ | - | ✓ | ✓ |
| test-http-gateway | 1+ | - | ✓ | ✓ |
| test-embedding-fallback | 1+ | - | ✓ | ✓ |
| validate-community-structure | 1+ | - | ✓ | ✓ |
| validate-retired-structural-bucket-absent | 1 | - | ✓ | - |
| **Tier 2: Semantic** | | | | |
| test-graphrag-local | 2 | - | - | ✓ |
| test-graphrag-global | 2 | - | - | ✓ |
| **Common Validation** | | | | |
| validate-rules | all | ✓ | ✓ | ✓ |
| validate-metrics | all | ✓ | ✓ | ✓ |
| verify-outputs | all | ✓ | ✓ | ✓ |

## Configuration

```go
type TieredConfig struct {
    Variant              string        // "structural", "statistical", "semantic"
    MessageCount         int           // Default: 20
    MessageInterval      time.Duration // Default: 50ms
    ValidationTimeout    time.Duration // Default: 30s
    PollInterval         time.Duration // Default: 100ms
    MinProcessed         int           // Default: 10
    MinExpectedEntities  int           // Default: 50
    
    // Structural tier constraints
    ExpectedEmbeddings   int           // Default: 0 (structural)
    ExpectedClusters     int           // Default: 0 (structural)
    MinRulesEvaluated    int           // Default: 5
    
    // URLs
    NatsURL              string        // Default: nats://localhost:4222
    MetricsURL           string        // Default: http://localhost:9090
    GatewayURL           string        // Default: http://localhost:8080/api-gateway
    OutputDir            string        // Default: test/e2e/results
}
```

## Assertions by Variant

### Structural Assertions (ZERO Constraints)

| Assertion | Description | Failure Mode |
|-----------|-------------|--------------|
| Zero embeddings | `embedding_requests_total = 0` | Hard failure |
| Zero clusters | `clustering_runs_total = 0` | Hard failure |
| OnEnter fired | At least 2 state entries | Warning |
| OnExit fired | At least 1 state exit | Warning |
| Rules evaluated | At least 5 evaluations | Warning |

### Statistical/Semantic Assertions

| Assertion | Description | Failure Mode |
|-----------|-------------|--------------|
| Entity count ≥ 50 | Entities indexed | Configurable |
| Required indexes | 5 core indexes populated | Strong |
| Required metrics | Prometheus metrics present | Strong |
| HTTP Gateway | 200 OK response | Medium |
| Rule metrics | Evaluations occurred | Medium |

## Backwards Compatibility

Legacy variant names are supported:
- `--variant core` → maps to `statistical`
- `--variant ml` → maps to `semantic`

## Output Files

Results saved to `OutputDir`:
- `comparison-structural-{timestamp}.json`
- `comparison-statistical-{timestamp}.json`
- `comparison-semantic-{timestamp}.json`

## Docker Compose Profiles

| Variant | Compose Profile | Command |
|---------|-----------------|---------|
| structural | `structural.yml` | `docker compose -f docker/compose/structural.yml` |
| statistical | `tiered.yml --profile statistical` | `docker compose -f docker/compose/tiered.yml --profile statistical` |
| semantic | `tiered.yml --profile semantic` | `docker compose -f docker/compose/tiered.yml --profile semantic` |

## Example Output

### Structural Variant
```
[tiered] Variant: structural
[tiered] Stage 1/12: verify-components (45ms)
[tiered] Stage 2/12: send-mixed-data (1.1s)
[tiered] Stage 3/12: validate-processing (2.0s)
[tiered] Stage 4/12: validate-zero-embeddings (23ms)
[tiered] ZERO embeddings confirmed (0 requests)
[tiered] Stage 5/12: validate-zero-clusters (18ms)
[tiered] ZERO clusters confirmed (0 runs)
[tiered] Stage 6/12: validate-rule-transitions (156ms)
[tiered] OnEnter: 3, OnExit: 2
[tiered] SUCCESS (Duration: 8.2s)
```

### Statistical Variant
```
[tiered] Variant: statistical (BM25 embeddings)
[tiered] Stage 1/16: verify-components (52ms)
...
[tiered] Stage 8/16: test-semantic-search (3.1s)
[tiered] BM25 fallback active
[tiered] SUCCESS (Duration: 45.3s)
```

### Semantic Variant
```
[tiered] Variant: semantic (neural embeddings)
[tiered] Stage 1/16: verify-components (48ms)
...
[tiered] Stage 8/16: test-semantic-search (4.2s)
[tiered] SemEmbed active: BAAI/bge-small-en-v1.5
[tiered] Stage 11/16: compare-communities (1.8s)
[tiered] LLM summaries detected: 5 communities
[tiered] SUCCESS (Duration: 72.1s)
```

## Related Files

- `test/e2e/scenarios/tiered.go` - Scenario implementation
- `test/e2e/client/metrics.go` - Metrics client with wait functions
- `test/e2e/client/tracer.go` - FlowTracer for message validation
- `test/e2e/client/nats.go` - NATS validation client
- `docker/compose/tiered.yml` - Docker Compose configuration
- `docker/compose/structural.yml` - Structural tier Docker Compose
