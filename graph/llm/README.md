# LLM Package

OpenAI-compatible LLM client and prompt templates for graph processing.

## Purpose

This package provides LLM integration for:
- **Community Summarization** - Generate natural language descriptions of entity communities
- **GraphRAG Search** - Answer questions using community context
- **Entity Description** - Generate descriptions for individual entities

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        LLM Package                          │
├─────────────────────────────────────────────────────────────┤
│  Prompt Templates (package variables)                       │
│  ┌─────────────────┐ ┌──────────────┐ ┌──────────────────┐ │
│  │ CommunityPrompt │ │ SearchPrompt │ │   EntityPrompt   │ │
│  └─────────────────┘ └──────────────┘ └──────────────────┘ │
├─────────────────────────────────────────────────────────────┤
│  OpenAI Client                                              │
│  - Chat completions                                         │
│  - Model listing                                            │
│  - Health checks                                            │
├─────────────────────────────────────────────────────────────┤
│  External Services (OpenAI-compatible)                      │
│  ┌─────────────┐ ┌────────────┐ ┌───────────┐              │
│  │ semshimmy   │ │ seminstruct│ │  OpenAI   │              │
│  │(inference)  │ │  (proxy)   │ │  (cloud)  │              │
│  └─────────────┘ └────────────┘ └───────────┘              │
└─────────────────────────────────────────────────────────────┘
```

## 6-Part Entity ID Awareness

Prompts understand the federated entity ID notation:

```
{org}.{platform}.{system}.{domain}.{type}.{instance}
```

| Part | Index | Example |
|------|-------|---------|
| org | 0 | acme |
| platform | 1 | logistics |
| domain | 2 | environmental |
| system | 3 | sensor |
| type | 4 | temperature |
| instance | 5 | sensor-042 |

The LLM is taught this structure in the system prompt, enabling intelligent summarization based on domain context. For example, environmental domains emphasize monitoring scope and measurements, while content domains focus on topics and knowledge areas.

## Usage

### Direct Prompt Usage

```go
// Render community summary prompt
data := llm.CommunitySummaryData{
    EntityCount:    10,
    DominantDomain: "environmental",
    Domains: []llm.DomainGroup{
        {Domain: "environmental", Count: 7, SystemTypes: []llm.SystemType{
            {Name: "sensor.temperature", Count: 4},
            {Name: "sensor.humidity", Count: 3},
        }},
    },
    Keywords: "temperature, humidity, monitoring",
}

rendered, err := llm.CommunityPrompt.Render(data)
// rendered.System = "You are an analyst..."
// rendered.User = "Summarize this community..."
```

### OpenAI Client

```go
// Create client
cfg := llm.OpenAIConfig{
    BaseURL: "http://seminstruct:8083/v1",
    Model:   "default",
}
client, err := llm.NewOpenAIClient(cfg)

// Chat completion
resp, err := client.ChatCompletion(ctx, llm.ChatRequest{
    SystemPrompt: rendered.System,
    UserPrompt:   rendered.User,
    MaxTokens:    150,
})

fmt.Println(resp.Content) // "This community monitors..."
```

### Override Prompts via File

```go
// Load custom prompts from JSON
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
    "system": "...",
    "user_format": "..."
  }
}
```

## Prompt Templates

### CommunityPrompt

Used by `clustering.LLMSummarizer` to generate community descriptions.

**Data Structure:** `CommunitySummaryData`

| Field | Type | Description |
|-------|------|-------------|
| `EntityCount` | int | Total entities in community |
| `Domains` | []DomainGroup | Grouped by domain from entity ID part[2] |
| `DominantDomain` | string | Most common domain or "mixed" |
| `OrgPlatform` | string | Common org.platform if uniform |
| `Keywords` | string | Comma-separated key terms |
| `SampleEntities` | []EntityParts | Parsed entity samples with 6-part breakdown |

### SearchPrompt

Used by `querymanager.GlobalSearch` to generate GraphRAG answers.

**Data Structure:** `SearchAnswerData`

| Field | Type | Description |
|-------|------|-------------|
| `Query` | string | User's question |
| `Communities` | []CommunitySummaryInfo | Relevant community summaries |
| `Entities` | []EntitySample | Top matching entities |

### EntityPrompt

Used for single entity descriptions.

**Data Structure:** `EntityDescriptionData`

| Field | Type | Description |
|-------|------|-------------|
| `ID` | string | Entity identifier |
| `Type` | string | Entity type |
| `Properties` | []PropertyInfo | Property predicates and values |
| `Relationships` | []RelationshipInfo | Outgoing relationships |

## Supported Backends

Any OpenAI-compatible API:

| Backend | Description | Use Case |
|---------|-------------|----------|
| **semshimmy + seminstruct** | Local inference stack | Development, edge deployment |
| **OpenAI** | Cloud API | Production with API key |
| **Ollama** | Local models | Development, privacy |
| **vLLM** | High-performance serving | Production self-hosted |

## Configuration

JSON configuration:
```json
{
  "llm": {
    "provider": "openai",
    "base_url": "http://seminstruct:8083/v1",
    "model": "default"
  }
}
```

Docker services (Tier 2):
```yaml
services:
  semshimmy:
    image: ghcr.io/c360studio/semshimmy:latest
  seminstruct:
    image: ghcr.io/c360studio/seminstruct:latest
    environment:
      - SEMINSTRUCT_SHIMMY_URL=http://semshimmy:8080
```

## Package Files

| File | Description |
|------|-------------|
| `client.go` | LLM client interface |
| `openai_client.go` | OpenAI SDK implementation |
| `config.go` | Configuration types |
| `prompts.go` | Prompt templates and data types |
| `doc.go` | Package documentation |

## Package Location

`processor/graph/llm/` - Part of the graph processing package family:

- `processor/graph/clustering/` - Uses LLM for community summaries
- `processor/graph/querymanager/` - Uses LLM for GraphRAG answers
- `processor/graph/embedding/` - Vector embeddings (separate from LLM)

## Related Documentation

- [Tiered Inference Architecture](../../../docs/e2e/tiers.md) - Tier 0/1/2 capabilities
- [Clustering Package](../clustering/README.md) - Community detection and summarization
- [QueryManager](../querymanager/README.md) - GraphRAG search operations
