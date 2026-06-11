# SemStreams Roadmap

## Alpha Blockers

Items requiring completion before alpha release.

### Search Query Classification
**Status:** Implemented (All Three Tiers Active)

Hybrid NL intent extraction with progressive fallback:
- Tier 0: Keyword heuristics — **active** (temporal, spatial, similarity, path, zone, aggregation, ranking intents)
- Tier 1/2: Embedding similarity to domain examples — **active** (wired via `enable_embedding_classifier` +
  `domain_examples_path` config fields)
- Tier 3: LLM classification for complex queries — **active** (via `LLMClientAdapter`, integration tested with
  Ollama, defaults to qwen3:1.7b, configurable via OLLAMA_TEST_MODEL)

Current state: `ClassifierChain` runs all three tiers in sequence. T0 `KeywordClassifier` covers 10+ regex patterns
including aggregation (`how many`, `count`, `average`, `sum`, `total`, `min`, `max`) and ranking (`top N`, `bottom N`,
`most`, `least`). T1/T2 `EmbeddingClassifier` is instantiated at startup when `enable_embedding_classifier` is true and
loads domain JSON from `domain_examples_path`. T3 `LLMClassifier` uses `LLMClientAdapter` to bridge
`graph/llm.Client` → `query.LLMClient`, handles reasoning model quirks (qwen3 `<think>` tags, markdown fences). New
`SearchOptions` fields: `AggregationType`, `AggregationField`, `RankingIntent`, `Strategy: "aggregation"`.

**Remaining roadmap items:**
- Expose `UpgradeVectors()` path for hot-swapping BM25 → neural vectors at runtime
- Add classifier observability metrics (tier hit rate, confidence distribution, fallback frequency)
- Expose classification to MCP handler (currently only GraphQL `globalSearch` and `semantic`)
- Align `graph-query` to use `ClassifierChain` instead of bare `KeywordClassifier`

### Anomaly Approval Workflow
**Status:** Implemented

ReviewWorker and HTTP handlers wired into runtime:
- ReviewWorker started in graph-clustering when enabled
- `/inference/*` HTTP handlers registered in graph-gateway
- Suggestion generation added to core anomaly detectors
- `TargetEntity` support for approving anomalies with empty targets

Current state: Fully operational. Human-only mode works without LLM.

### Mutation E2E Testing
**Status:** Partial Coverage

Add explicit mutation tests beyond rule-driven coverage:
- Direct API tests for AddTriple/RemoveTriple
- Relationship mutation tests (Create/Delete)
- Index consistency verification after mutations

Current state: Mutations only tested indirectly via rules engine.

### Transitivity Detector Wiring
**Status:** Implemented

Transitivity gap detector wired into anomaly detection pipeline:
- `kvRelationshipQuerier` implementation preserves predicate information
- Transitivity detector registered with anomaly orchestrator
- Detection of missing edges in transitive predicate chains enabled

Current state: Fully operational. Detects transitivity gaps for configured predicates.

### Query Pattern Enhancements
**Status:** PathRAG Implemented, Gateway Exposure Needed

**PathRAG — processor complete:**
- Direction control (incoming, outgoing, both) — **implemented and tested**
- Predicate filtering — **implemented and tested**
- Per-request timeout — **implemented and tested**
- MaxPaths bound — **implemented and tested**
- All features accessible via direct NATS `graph.query.pathSearch` subject

**PathRAG — gateway gap:**
- GraphQL schema only exposes `startEntity`, `maxDepth`, `maxNodes`
- `direction`, `predicates`, `timeout`, `maxPaths` not in GraphQL schema or `transformPathSearchVars()`
- `IncludeSiblings` field declared but not wired in BFS logic

**GraphRAG — not yet implemented:**
- Relationships in response — show connections between returned entities
- Source attribution — link answers to specific entities/communities for explainability
- Response control parameters (include_summaries, include_relationships)

Current state: PathRAG BFS engine is feature-complete with direction control, predicate filtering, timeout, and path limits. All features work via NATS but are not exposed through the GraphQL/MCP gateway. GraphRAG doesn't include relationships or source attribution.

### Rules Processor Completion
**Status:** Partial Implementation

Complete stubbed action implementations in rules processor:
- ActionTypePublish: Implemented for agentic workflows
- ActionTypePublishAgent: Implemented for spawning agent tasks
- ActionTypeUpdateTriple: Triple metadata updates (partial)
- Dynamic watch pattern reloading without restart

Current state: Stateful ECA rules work. Publish actions implemented for agentic system integration. Update triple actions partially implemented.

### Rule Engine + Lifecycle Harness
**Status:** Implemented (Reactive rules + `pkg/lifecycle`)

SemStreams no longer carries a separate reactive workflow engine. Current
workflow-shaped behavior is expressed as coordinated rule sets over
lifecycle-managed graph entities:
- KV watch, NATS subject, and cron triggers
- Unified typed condition evaluation
- Cooldown, debounce, dedup, and per-action iteration caps
- Fire-and-forget publish and publish-agent actions
- Lifecycle actions (`lifecycle_transition`, `lifecycle_complete`,
  `lifecycle_fail`) over `ENTITY_STATES`

Current state: The retired `processor/reactive/` path has been replaced by the
rule engine plus `pkg/lifecycle`. Durable phase/progress state that is part of a
named entity lives as graph triples in `ENTITY_STATES`; opaque execution
artifacts and high-volume traces stay in component-owned buckets or ObjectStore
refs. See `docs/concepts/14-orchestration-layers.md` and ADR-049.

### Agentic Components
**Status:** Implemented

LLM-powered autonomous task execution with six specialized components:
- agentic-loop: State machine, orchestration, trajectory capture
- agentic-model: OpenAI-compatible LLM endpoint caller
- agentic-tools: Tool dispatch with executor registry
- agentic-dispatch: User message routing, commands, permissions
- agentic-memory: Graph-backed persistent memory
- agentic-governance: PII filtering, rate limiting, content governance

Current state: Fully operational. Run `task e2e:agentic` for validation.

### UI Flow Builder
**Status:** WIP | **Repo:** semstreams-ui

Visual flow builder for designing, deploying, and managing flows through a drag-and-drop interface. Backend APIs (Flow CRUD, component lifecycle, live metrics) are implemented in semstreams. The frontend UI is under active development in the `semstreams-ui` repository.

Current state: Backend ready. UI planned for beta release.

---

## Future Enhancements

Items planned but not required for alpha.

### Content Processing

#### LLM-Generated Abstracts
**Priority:** Medium | **Complexity:** Medium

Auto-generate abstracts/summaries for content using LLM agents.

- **Use cases:** Documents without descriptions, long-form content needing summaries
- **Integration:** ContentStorable processing pipeline
- **Approach:** Send `RawContent()` fields to LLM, store generated abstract in content fields
- **Pattern:** KV-watching async worker
- **Tier requirement:** Semantic (LLM required)

#### Content Analysis Processor
**Priority:** Medium | **Complexity:** High

LLM-powered analysis of operational documents to suggest rules and
lifecycle-backed orchestration patterns:
- Watch for new documents by configurable type/category patterns
- Two-phase analysis: detect candidates, then extract full definitions
- Extract conditional logic as rule suggestions
- Extract multi-step procedures as coordinated rule/lifecycle suggestions
- User review/approval via HTTP API before deployment

- **Use cases:** Early adopters uploading SOPs before field deployment
- **Tier requirement:** Semantic (LLM required)
- **Pattern:** KV-watching async worker
- **Depends on:** Rule engine, lifecycle harness

Current state: Rule engine + lifecycle harness are implemented. Content
analysis implementation can proceed when prioritized.

---

### Community Detection

#### Content-Aware Keyword Extraction
**Priority:** Medium | **Complexity:** Medium

Enhance keyword extraction to use ContentStorable document content.

- **Current state:** Keywords from entity types and triple metadata only
- **Gap:** ContentStorable content (body, abstract, title) is used for embeddings but ignored for keywords
- **Proposed:** Hybrid extraction with weighted combination of metadata and content terms
- **Benefit:** Richer, more descriptive community keywords

---

### Embeddings & Retrieval

#### Multimodal Video Embeddings
**Priority:** Low | **Complexity:** High

Generate embeddings from video content for semantic search.

- **Approach options:**
  1. Embed thumbnail only (simple, fast)
  2. Extract keyframes, send to vision LLM for descriptions, embed descriptions
  3. Use video-specific embedding models (expensive, specialized)
- **Integration:** Extends `BinaryStorable` pipeline via `ContentRoleMedia`
- **Pattern:** KV-watching async worker
- **Tier requirement:** Semantic (vision LLM or multimodal model)

#### Image Embeddings
**Priority:** Medium | **Complexity:** Medium

Generate embeddings directly from images.

- **Approach options:**
  1. Vision LLM generates description, embed the text
  2. Direct image-to-vector using multimodal models (CLIP, etc.)
- **Integration:** Extends `BinaryStorable` pipeline via `ContentRoleMedia`
- **Pattern:** KV-watching async worker
- **Tier requirement:** Semantic (multimodal embedding provider)

---

### Query & Classification

#### PathRAG Gateway Exposure
**Priority:** High | **Complexity:** Low

Expose PathRAG features through GraphQL/MCP gateway:
- Add `direction`, `predicates`, `timeout`, `maxPaths` to GraphQL schema arguments
- Update `transformPathSearchVars()` to forward these fields
- Wire `IncludeSiblings` into BFS logic or remove the dead field

Current state: All features work via direct NATS. Gateway just needs schema + transform updates.

#### Classification Metadata in GraphQL Response
**Status:** Implemented

Classification metadata (tier, confidence, intent) returned in standard GraphQL `extensions` field for `globalSearch` and `semanticSearch` queries. Non-search queries omit extensions. Implemented via `writeGraphQLSuccessWithExtensions` delegation pattern.

#### GlobalSearch GraphQL Schema Enrichment
**Status:** Implemented

`SearchResult` split into `GlobalSearchResult` (entities, community_summaries, relationships, sources, count, duration_ms, answer, answer_model) and `LocalSearchResult` (entities, communityId, count, durationMs). Added `includeSummaries`, `includeRelationships`, `includeSources` boolean args to `globalSearch` query, mapped to backend `GlobalSearchRequest` fields.

#### Classifier Observability
**Priority:** Medium | **Complexity:** Low

Add Prometheus metrics for classification behavior:
- Counter per tier (T0/T1/T2/T3) hit rate
- Histogram for classification confidence
- Counter for fallback frequency (embedding miss → keyword → LLM)
- Counter for MCP vs GraphQL classification usage

---

### Graph Providers

#### Spatial/Temporal Graph Providers
**Priority:** Low | **Complexity:** Medium

Add `SpatialGraphProvider` and `TemporalGraphProvider` for clustering:
- Pattern proven by existing `SemanticGraphProvider`
- Indexes exist and are populated, just need provider implementations
- Would enable geo-proximity and time-correlated community detection

**Current state:** Spatial and temporal indexes are fully operational — bounding box and time-range queries work via GraphQL and NATS. This enhancement adds clustering integration only.

---

## Legend

| Priority | Description |
|----------|-------------|
| High | Customer-requested or blocking other work |
| Medium | Significant value, plan for next iteration |
| Low | Nice to have, opportunistic |

| Complexity | Description |
|------------|-------------|
| Low | < 1 day, isolated change |
| Medium | 1-3 days, touches multiple components |
| High | > 3 days, architectural impact |
