# LLM Enhancement

SemStreams integrates with Large Language Models to generate natural language summaries for communities. This is a Tier 2 feature that enhances the statistical summaries with richer, context-aware descriptions.

## Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                        LLM Enhancement Pipeline                     │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  Community Detection                                                │
│         │                                                           │
│         ▼                                                           │
│  Statistical Summarizer (instant)                                   │
│         │                                                           │
│         ▼                                                           │
│  Save to COMMUNITY_INDEX (status: "statistical")                    │
│         │                                                           │
│         ▼                                                           │
│  EnhancementWorker (KV watcher)                                     │
│         │                                                           │
│         ├── See status="statistical"                                │
│         ├── Fetch community + entity data                           │
│         ├── Render prompt template                                  │
│         ├── Call LLM API                                            │
│         └── Update status to "llm-enhanced" or "llm-failed"         │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

## Enhancement Worker

The EnhancementWorker runs as a background goroutine pool that watches for communities needing enhancement:

```go
type EnhancementWorker struct {
    storage      CommunityStorage
    llmClient    LLMClient
    workers      int
    paused       atomic.Bool
}
```

### Pause/Resume

During community detection, the worker is paused to prevent race conditions:

```go
// In processor.go DetectCommunities():
if p.enhancementWorker != nil {
    p.enhancementWorker.Pause()   // Stop processing
    defer p.enhancementWorker.Resume()  // Resume after detection
}
```

This ensures:
1. No concurrent writes to COMMUNITY_INDEX during detection
2. In-flight LLM work completes before pause
3. Worker resumes automatically after detection

### Worker Pool

Multiple concurrent workers process communities:

```json
{
  "enhancement": {
    "workers": 3
  }
}
```

More workers = faster enhancement, but higher LLM API load.

## Prompt Templates

SemStreams includes three prompt templates:

### CommunityPrompt

Used for community summarization:

```go
CommunityPrompt = PromptTemplate{
    System: `You are an analyst summarizing communities of related entities.

Entity IDs follow a 6-part federated notation:
  {org}.{platform}.{system}.{domain}.{type}.{instance}

Parts:
- org: Organization identifier (multi-tenancy)
- platform: Platform/product within organization
- domain: Business domain (e.g., environmental, content, logistics)
- system: System or subsystem (e.g., sensor, document, device)
- type: Entity type within system (e.g., temperature, manual, humidity)
- instance: Unique instance identifier

Generate concise summaries (1-2 sentences) that leverage this structure.
For environmental domains: emphasize monitoring scope and measurements.
For content domains: emphasize topics, themes, and knowledge areas.
For mixed domains: describe relationships between different entity types.`,

    UserFormat: `Summarize this community of {{.EntityCount}} entities:
...`,
}
```

### SearchPrompt

Used for GraphRAG search answer generation:

```go
SearchPrompt = PromptTemplate{
    System: `You are a helpful assistant that answers questions based on entity graph context.
Use the provided community summaries and entity information to answer the user's question.
Be concise and factual. If the information is insufficient, say so.`,

    UserFormat: `Question: {{.Query}}

Relevant communities:
{{range .Communities}}- {{.Summary}} ({{.EntityCount}} entities, keywords: {{.Keywords}})
{{end}}
...`,
}
```

### EntityPrompt

Used for single entity descriptions:

```go
EntityPrompt = PromptTemplate{
    System: `You are a helpful assistant that describes entities in a knowledge graph.
Generate clear, informative descriptions based on the entity's properties and relationships.`,

    UserFormat: `Describe this entity:
ID: {{.ID}}
...`,
}
```

## 6-Part Entity ID Awareness

Prompts understand the federated entity ID notation:

```
{org}.{platform}.{system}.{domain}.{type}.{instance}
```

| Part | Index | Example | Purpose |
|------|-------|---------|---------|
| org | 0 | acme | Organization (multi-tenancy) |
| platform | 1 | logistics | Platform/product |
| domain | 2 | environmental | Business domain |
| system | 3 | sensor | System/subsystem |
| type | 4 | temperature | Entity type |
| instance | 5 | sensor-042 | Unique instance |

The LLM is taught this structure, enabling domain-aware summarization:
- **Environmental domains**: Emphasize monitoring scope and measurements
- **Content domains**: Focus on topics, themes, and knowledge areas
- **Mixed domains**: Describe relationships between different entity types

## Prompt Data Structures

### CommunitySummaryData

```go
type CommunitySummaryData struct {
    EntityCount    int           // Total entities in community
    Domains        []DomainGroup // Grouped by domain from entity ID part[2]
    DominantDomain string        // Most common domain or "mixed"
    OrgPlatform    string        // Common org.platform if uniform
    Keywords       string        // Comma-separated key terms
    SampleEntities []EntityParts // Parsed entity samples
}

type DomainGroup struct {
    Domain      string       // Domain name
    Count       int          // Entity count
    SystemTypes []SystemType // Systems within domain
}

type SystemType struct {
    Name  string // System.type combination
    Count int    // Entity count
}
```

### EntityParts

```go
type EntityParts struct {
    Full     string // Complete entity ID
    Org      string // Organization
    Platform string // Platform
    Domain   string // Business domain
    System   string // System/subsystem
    Type     string // Entity type
    Instance string // Unique instance
}
```

## Custom Prompts

Override prompts via JSON file:

```go
err := llm.LoadPromptsFromFile("custom-prompts.json")
```

File format:

```json
{
  "community_summary": {
    "system": "Custom system prompt...",
    "user_format": "Custom template with {{.EntityCount}}..."
  },
  "search_answer": {
    "system": "Custom search system prompt...",
    "user_format": "Question: {{.Query}}..."
  },
  "entity_description": {
    "system": "Custom entity prompt...",
    "user_format": "Describe {{.ID}}..."
  }
}
```

## LLM Client

The OpenAI-compatible client works with any compliant API:

```go
cfg := llm.OpenAIConfig{
    BaseURL:        "http://seminstruct:8083/v1",
    Model:          "default",
    TimeoutSeconds: 60,
    MaxRetries:     3,
}
client, err := llm.NewOpenAIClient(cfg)

// Chat completion
resp, err := client.ChatCompletion(ctx, llm.ChatRequest{
    SystemPrompt: rendered.System,
    UserPrompt:   rendered.User,
    MaxTokens:    150,
})

fmt.Println(resp.Content)
```

## Supported Backends

Any OpenAI-compatible API:

| Backend | Description | Use Case |
|---------|-------------|----------|
| **semshimmy + seminstruct** | Local inference stack | Development, edge deployment |
| **OpenAI** | Cloud API | Production with API key |
| **Ollama** | Local models | Development, privacy |
| **vLLM** | High-performance serving | Production self-hosted |
| **Azure OpenAI** | Enterprise cloud | Azure deployments |

### semshimmy + seminstruct Stack

The SemStreams local inference stack:

```yaml
services:
  semshimmy:
    image: ghcr.io/c360studio/semshimmy:latest
    # Local model inference engine

  seminstruct:
    image: ghcr.io/c360studio/seminstruct:latest
    environment:
      - SEMINSTRUCT_SHIMMY_URL=http://semshimmy:8080
    # OpenAI-compatible proxy
```

- **semshimmy**: Runs local models (Mistral, Llama, etc.)
- **seminstruct**: Provides OpenAI-compatible API interface

## Configuration

```json
{
  "enhancement": {
    "enabled": true,
    "workers": 3,
    "domain": "default",
    "summarizer_endpoint": "http://seminstruct:8083",
    "llm": {
      "provider": "openai",
      "base_url": "http://shimmy:8080/v1",
      "model": "mistral-7b-instruct",
      "timeout_seconds": 60,
      "max_retries": 3
    }
  }
}
```

| Option | Default | Description |
|--------|---------|-------------|
| `enabled` | false | Enable LLM enhancement |
| `workers` | 3 | Concurrent enhancement goroutines |
| `domain` | default | Prompt domain hint |
| `summarizer_endpoint` | - | seminstruct service URL |
| `llm.provider` | none | Backend: `openai`, `none` |
| `llm.base_url` | - | LLM service URL |
| `llm.model` | - | Model identifier |
| `llm.timeout_seconds` | 60 | Per-request timeout |
| `llm.max_retries` | 3 | Retry count |

## Summary Status Flow

```
┌──────────────┐     ┌───────────────┐     ┌─────────────────┐
│ statistical  │ ──▶ │ llm-enhanced  │  or │   llm-failed    │
└──────────────┘     └───────────────┘     └─────────────────┘
      │                                            │
      │                                            │
      └──── (LLM unavailable at startup) ─────────┘
                        │
                        ▼
              ┌─────────────────────┐
              │ statistical-fallback │
              └─────────────────────┘
```

| Status | Meaning |
|--------|---------|
| `statistical` | Initial summary, queued for LLM |
| `llm-enhanced` | LLM narrative complete |
| `llm-failed` | LLM enhancement failed |
| `statistical-fallback` | LLM disabled, using statistical |

## Graceful Degradation

If LLM service is unavailable:

1. **Statistical summary always generated** - Immediate, no external dependency
2. **Enhancement marked as failed** - Status set to `llm-failed`
3. **System continues operating** - Graph queries work with statistical summaries
4. **Retries possible** - Re-save community with `statistical` status

### Retry Failed Enhancements

```bash
# Reset a failed community for retry
nats kv get COMMUNITY_INDEX "graph.community.0.comm-0-A1" | \
  jq '.summary_status = "statistical"' | \
  nats kv put COMMUNITY_INDEX "graph.community.0.comm-0-A1"
```

The worker will pick up the community for another enhancement attempt.

## Enhancement Window

Controls detection pause during LLM enhancement:

```json
{
  "schedule": {
    "enhancement_window": "120s",
    "enhancement_window_mode": "blocking"
  }
}
```

### Window Modes

| Mode | Behavior |
|------|----------|
| `blocking` | Hard pause until window expires or all complete |
| `soft` | Allow detection if changes exceed threshold |
| `none` | No pause, detection continues immediately |

### Trade-offs

| Mode | Freshness | Summary Quality | CPU Usage |
|------|-----------|-----------------|-----------|
| `blocking` | Lower | Higher | Bursty |
| `soft` | Medium | Medium | Moderate |
| `none` | Highest | Lower (may invalidate) | Steady |

## Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `graph_processor_enhancement_latency_seconds` | Histogram | LLM call latency |
| `graph_processor_enhancement_queue_depth` | Gauge | Pending enhancements |
| `graph_processor_enhancement_success_total` | Counter | Successful enhancements |
| `graph_processor_enhancement_failure_total` | Counter | Failed enhancements |

## Best Practices

### Model Selection

| Environment | Recommended | Notes |
|-------------|-------------|-------|
| Development | Ollama + Mistral 7B | Fast, local, free |
| Edge deployment | semshimmy + Mistral | Self-contained |
| Production | OpenAI GPT-4o-mini | Fast, cost-effective |
| High quality | OpenAI GPT-4 | Best summaries, slower |

### Timeout Tuning

```json
{
  "llm": {
    "timeout_seconds": 60
  }
}
```

- **Local models**: 60-120s (CPU can be slow)
- **Cloud APIs**: 30-60s (faster, but network latency)
- **GPU inference**: 10-30s (fast local)

### Worker Count

| LLM Speed | Recommended Workers |
|-----------|---------------------|
| Local CPU | 1-2 |
| Local GPU | 3-5 |
| Cloud API | 5-10 |

More workers help when LLM is fast. Too many workers with slow LLM just builds backlog.

### Error Handling

1. **Check metrics** for failure rates
2. **Monitor queue depth** for backlog
3. **Review logs** for specific errors
4. **Consider fallback** to statistical-only in high-failure environments

## Troubleshooting

### Enhancement Not Running

1. Check `enhancement.enabled: true`
2. Verify LLM endpoint is reachable
3. Check for communities with `statistical` status
4. Monitor `enhancement_queue_depth` metric

### Slow Enhancement

1. Check LLM latency metrics
2. Verify model is appropriate for hardware
3. Consider reducing `workers`
4. Use faster model/backend

### High Failure Rate

1. Check LLM service health
2. Increase `timeout_seconds`
3. Increase `max_retries`
4. Review error logs for patterns

## Related Documentation

- [Clustering](01-clustering.md) - Community detection
- [Performance](03-performance.md) - Optimization strategies
- [Index and Bucket Reference](05-index-reference.md) - Storage bucket details

### Background Concepts

For foundational understanding of the RAG patterns and algorithms:

- [GraphRAG Pattern](../concepts/09-graphrag-pattern.md) - Community-based RAG explained
- [Community Detection](../concepts/07-community-detection.md) - How communities form
- [Similarity Metrics](../concepts/06-similarity-metrics.md) - TF-IDF keyword extraction
