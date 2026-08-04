# GraphRAG Pattern

GraphRAG (Graph Retrieval-Augmented Generation) uses community structure to provide organized context for LLM-powered answers.

## What is RAG?

Retrieval-Augmented Generation combines search with language models:

1. **Retrieval**: Find relevant information from your data
2. **Augmentation**: Inject that information into an LLM prompt
3. **Generation**: LLM produces an answer grounded in your data

Traditional RAG retrieves documents. GraphRAG retrieves from a knowledge graph organized into communities.

## Why Graph Structure Matters

**Traditional RAG problem:** You have 10,000 entities. A query matches 500. Which ones should the LLM see? Token limits force you to truncate, potentially losing critical context.

**GraphRAG solution:** Entities are pre-organized into communities with summaries. Instead of dumping raw matches, you provide:

- Community summaries (high-level context)
- Representative entities (key examples)
- Relationship structure (how things connect)

The LLM gets organized context, not a raw document dump.

## How SemStreams Implements GraphRAG

### Community-Based Search Flow

```text
User Question (natural language)
      │
      ▼
┌─────────────────┐
│ Query Classifier │◄── extracts temporal/spatial/intent
│                  │    from NL query
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│  Find relevant   │
│   communities    │◄── keyword/semantic matching
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│  Extract context │
│  - Summaries     │
│  - Key entities  │
│  - Relationships │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│  LLM synthesizes │
│     answer       │
└─────────────────┘
```

There is no single three-tier classifier path. The HTTP graph facade uses keyword
classification followed by an optional embedding-example match. The graph-query
component uses keyword classification followed by an optional LLM classifier; it
does not configure an embedding classifier. An unmatched enabled chain returns
the default Tier 0 result with no extracted filters or intent.

### Local Search

Starts from a known entity and explores its community. You provide an entity ID, and the search returns that entity's community membership, nearby entities within a hop radius, and their relationships.

**Use when:** You have a starting point and want related context.

**Example:** "What's happening around sensor-042?" → Returns sensor-042's community, nearby entities, and relationships.

### Global Search

Searches across all communities by topic. You provide a query string, and the search finds communities with matching keywords or semantic similarity, returning their summaries and key entities.

**Use when:** You have a question but no specific starting entity.

**Example:** "What battery issues exist?" → Finds communities related to batteries, returns summaries and key entities.

## GraphRAG vs PathRAG

| Aspect | GraphRAG | PathRAG |
|--------|----------|---------|
| Starting point | Query text | Known entity ID |
| Search type | Semantic/keyword | Structural traversal |
| What it finds | Topic-related communities | Connected entities |
| Best for | Discovery, Q&A | Impact analysis, dependencies |
| Requires | Communities (Tier 1+) | Indexes only (Tier 0) |

**Decision guide:**

```text
Do you have a specific entity to start from?
  │
  ├─ Yes: Do you want semantically related or structurally connected?
  │         │
  │         ├─ Structurally connected → PathRAG
  │         └─ Semantically related → Local GraphRAG
  │
  └─ No: You have a question/topic → Global GraphRAG
```

## Community Summaries

Communities carry pre-computed summaries for efficient GraphRAG. Each summary includes:

| Field | Description |
|-------|-------------|
| `id` | Unique community identifier |
| `level` | Hierarchical level (0 = finest, higher = coarser) |
| `members` | List of entity IDs belonging to this community |
| `keywords` | TF-IDF extracted terms representing the community |
| `representative_entities` | PageRank-identified hub entities |
| `text` | Human-readable narrative (Tier 2 only) |
| `status` | Summary type: "statistical" or "llm_enhanced" |

### Summary Tiers

| Tier | Summary Type | Source |
|------|--------------|--------|
| Tier 1 | Statistical | TF-IDF keywords, PageRank representatives |
| Tier 2 | LLM-enhanced | Statistical + LLM-generated narrative |

Statistical summaries are fast and deterministic. LLM summaries add natural language descriptions but require an LLM service.

## Configuration

### Enable GraphRAG

GraphRAG requires clustering to be enabled. The current clustering owner
recomputes on its timer; `batch_size` does not trigger change-threshold
recomputation. See [Community Detection](07-community-detection.md).

### Search Parameters

| Parameter | Default | Description |
|-----------|---------|-------------|
| `max_communities` | 5 | Maximum communities to return per query |
| `summarize_threshold` | 50 | Auto-summarize above this count; negative disables |
| `include_summaries` | true | Include community summary records |
| `include_relationships` | false | Include relationships between returned entities |
| `include_sources` | false | Include source-attribution records |

### LLM Integration (Tier 2)

For LLM-enhanced summaries and natural language answers, configure an LLM provider (HTTP endpoint or local model). See [LLM Enhancement](../advanced/02-llm-enhancement.md) for configuration details.

## API and Response

SemStreams has no implemented MCP graph contract. The current HTTP facade and
internal NATS subjects are provisional read fronts; confirm an operation is
explicitly admitted for the caller rather than inferring protocol parity. When an
admitted operation returns a GraphRAG result, it uses two query patterns:

**Natural Language Q&A** — Submit a question in plain English. The system finds relevant communities, assembles context, and returns an LLM-generated answer with source attribution.

**Community Search** — Search for communities by topic or keywords. Returns matching community summaries and their key entities without LLM synthesis.

### Response Fields

| Field | Description |
|-------|-------------|
| `answer` | LLM-generated response (Q&A mode only) |
| `answer_model` | Model identifier when answer synthesis uses one |
| `entities` | Returned full entity records |
| `entity_ids`, `entity_digests` | Bounded summarized entity forms |
| `summarized`, `community_summaries` | Summary mode and matched summaries |
| `relationships`, `sources` | Optional requested relationship and attribution data |
| `count`, `duration_ms` | Result count and processing duration |
| `degraded`, `degraded_reason` | Answer-synthesis fallback evidence |
| `entities_truncated` | Candidate corpus was capped and the result is not exhaustive |

## How Context Flows to the LLM

When you ask "What's wrong with the warehouse sensors?":

```text
┌──────────────────────────────────────────────────────────────┐
│  1. Community Matching                                       │
│     Query: "warehouse sensors" → matches keywords            │
│                                                              │
│  2. Context Assembly                                         │
│     ┌─────────────────────────────────────────────────────┐  │
│     │ Community: warehouse-sensors-zone-a                 │  │
│     │ Summary: "Environmental monitoring cluster..."      │  │
│     │                                                     │  │
│     │ Key Entities:                                       │  │
│     │   sensor-003 (45.2°C - anomalous)                   │  │
│     │   sensor-001 (23.1°C - normal)                      │  │
│     │                                                     │  │
│     │ Relationships:                                      │  │
│     │   sensor-003 → alerts → alert-high-temp-001         │  │
│     │   sensor-003 → located_in → zone-A                  │  │
│     └─────────────────────────────────────────────────────┘  │
│                                                              │
│  3. LLM Prompt = Context + Question                          │
│                                                              │
│  4. Answer: "Sensor-003 in zone A is reporting high          │
│     temperature (45.2°C), exceeding the 35°C threshold.      │
│     An alert has been triggered."                            │
└──────────────────────────────────────────────────────────────┘
```

The LLM receives organized context—summaries, key entities, relationships—rather than raw data, enabling grounded answers.

## When to Use GraphRAG

**Strong fit:**
- Natural language questions about your domain
- Discovery ("what exists related to X?")
- Summarization ("give me an overview of Y")
- Cross-entity analysis ("how do these things relate?")

**Weak fit:**
- Exact entity lookups (use direct query)
- Structural traversal (use PathRAG)
- Real-time alerting (use rules engine)

## Common Issues

### "Answers seem generic, not grounded in my data"

1. Check community summaries exist (`status: "statistical"` or `"llm_enhanced"`)
2. Verify ContentStorable payloads have text content for keyword extraction
3. Ensure clustering is running (`detection_interval` not too long)

### "Wrong communities are being selected"

1. Check keyword extraction—are TF-IDF keywords representative?
2. For Tier 2, verify embeddings are generating (semantic matching)
3. Tune `max_communities` if too few/many results

### "LLM answers take too long"

1. Community summaries are cached—first query is slowest
2. Use local LLM (Ollama) for lower latency
3. Pre-warm communities by running detection before queries

## Related

**Concepts**
- [Real-Time Inference](00-real-time-inference.md) - How GraphRAG fits in the hybrid streaming model
- [Community Detection](07-community-detection.md) - How communities form via LPA
- [PathRAG Pattern](10-pathrag-pattern.md) - Structural traversal alternative for impact analysis
- [Embeddings](05-embeddings.md) - Semantic matching that enables community search
- [Query Access](11-query-access.md) - HTTP facade, typed adapters, and MCP status

**Configuration**
- [Clustering Configuration](../advanced/01-clustering.md) - Community detection settings
- [LLM Enhancement](../advanced/02-llm-enhancement.md) - LLM provider configuration
