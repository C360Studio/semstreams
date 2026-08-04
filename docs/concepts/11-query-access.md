# Query Access

Why structured query access matters for knowledge graphs and when to use each pattern.

## The Access Problem

Knowledge graphs differ from traditional databases:

```text
Traditional Database:
┌─────────────────────────────────────────────────────────────┐
│                      Single Query Path                       │
│                                                              │
│   Application ────► SQL Engine ────► Tables ────► Response   │
│                                                              │
└─────────────────────────────────────────────────────────────┘
                          │
                    One interface
                    One protocol
```

Knowledge graphs serve diverse clients with different needs:

```text
SemStreams:
┌─────────────────────────────────────────────────────────────┐
│                    Multiple Access Paths                     │
│                                                              │
│   Web Apps ─────────┐                                        │
│                     │                                        │
│   AI Agents ────────┼──────► Graph ────► Knowledge           │
│                     │                                        │
│   Internal Services ┘                                        │
│                                                              │
└─────────────────────────────────────────────────────────────┘
                          │
                    Different needs:
                    - Latency
                    - Schema control
                    - Auditability
```

No general graph front door currently serves every caller. The remote endpoint is
a hand-written GraphQL-shaped facade, not a schema executor. Embedded access is
admitted only through operation-specific typed adapters; `graph/query.Client`
mixes direct KV and RPC and is provisional. MCP graph access is unavailable.

## Implemented and Future Access Patterns

### Remote HTTP: GraphQL-Shaped Facade

The current gateway accepts a bounded set of GraphQL-shaped HTTP operations. It
uses hand-written routing, argument parsing, and introspection, and forwards full
NATS JSON results without applying a selection set. Do not promise general
GraphQL validation or field projection.

```text
┌─────────────┐        ┌─────────────┐        ┌─────────────┐
│   Client    │───────►│ HTTP facade │───────►│ Internal op │
│             │        │             │        │             │
│ Documented  │        │ Hand-written│        │ Full JSON   │
│ root op     │        │ routing     │        │ response    │
└─────────────┘        └─────────────┘        └─────────────┘
```

**Key characteristics:**

- Only admitted root operations are supported
- Advertised introspection and handlers require drift checks
- Single HTTP endpoint for all operations
- Natural language classification can extract supported search intents

**Natural language support:** The gateway first applies keyword patterns for
temporal, spatial, similarity, path, aggregation, and ranking hints. If no keyword
matches and the optional embedding classifier is configured, it tries an
embedding example match. The gateway does not construct an LLM classifier.
An unmatched query returns the default Tier 0 classification with no extracted
filters or intent; it is not silently interpreted by another model.

**Best for:** External applications, web frontends, interactive exploration, natural language queries.

### MCP: No Implemented Graph Contract

SemStreams does not currently implement an MCP graph endpoint and graph tool set.
Do not route an AI agent to MCP, claim that MCP wraps GraphQL, or promise GraphRAG
or PathRAG through MCP. A future MCP capability requires its own implemented and
specified endpoint, bounded tools, audit behavior, and availability contract.

### Operation-Specific Typed Adapters

An embedded service may use a small typed adapter for a named operation when that
adapter is implemented and admitted. For example, `fusionnats` is admitted only
for its fusion contract. There is no general subject-hiding embedded graph client.

```text
┌─────────────┐                              ┌─────────────┐
│   Service   │─────────────────────────────►│  Component  │
│             │                              │             │
│  Request/   │      No gateway overhead     │  Owns data  │
│  Reply      │                              │             │
└─────────────┘                              └─────────────┘
      │                                            │
   Lowest                                     Direct
   Latency                                    Access
```

**When gateway overhead matters:**

- High-frequency internal queries
- Latency-sensitive operations
- Component-to-component communication
- Operations not exposed through GraphQL

**Best for:** The exact embedded consumer named by an admitted adapter.

## Choosing an Access Pattern

Use two steps. First choose the front door that fits the caller. Then inspect the
operation contract to learn whether authority or a named materialized view answers
and what evidence accompanies that answer.

```text
Step 1: front door
  External application  -> admitted remote HTTP operation
  Embedded service      -> admitted operation-specific typed adapter
  AI agent              -> no canonical MCP graph surface yet
  Projection owner      -> its declared storage seam
  Operator              -> explicit diagnostic access

Step 2: answer source
  Authority-backed       -> today: value only; GS-01 target: value + revision
  Named materialized view -> today: per-operation; GS-03+ target: owner status
```

### Decision Matrix

| Factor | HTTP graph facade | MCP | Typed adapter |
|--------|---------|-----|-------------|
| **Latency** | Higher (HTTP) | Not defined | Lowest (direct) |
| **Schema control** | Hand-written | Not implemented | Named operation |
| **Auditability** | Metrics/logs; no audit contract | Not implemented | Logs; no audit contract |
| **External access** | Yes | No graph surface | No (internal) |
| **Discovery** | Advertised surface may drift | No graph tools | Adapter contract |

### Common Patterns

**Web application backend:**
- Admitted remote operations for user-facing queries
- Admitted operation-specific adapters for internal queries

**AI-powered system:**
- Use admitted remote operations where they fit the application.
- Do not assume a canonical agent graph surface until MCP tools exist.

**Microservice mesh:**
- Operation-specific typed adapters between services
- Admitted remote operations for external API

## Trade-offs

### Schema Enforcement vs Flexibility

The current facade does not provide general schema-executor guarantees. An
admitted typed adapter enforces its named operation contract. MCP supplies no
graph schema today.

### Latency vs Features

```text
Typed adapter     -> named embedded operation only
HTTP facade       -> hand-written GraphQL-shaped operation
MCP               -> not implemented for SemStreams graph reads
```

### Discovery Mechanisms

Each pattern has different discovery:

| Pattern | Discovery Method | Granularity |
|---------|-----------------|-------------|
| HTTP graph facade | Advertised introspection | May drift from handlers |
| MCP | Not available for graph reads | None implemented |
| Typed adapter | Named operation contract | Per operation |

Treat advertised introspection as discovery evidence, not proof of parser or
handler parity. A named typed adapter documents only its own operation.

## Consistency Considerations

Front door and answer source are independent. An adopter does not select a bucket
to get stronger truth. The current entity operation returns the raw authority
value without a revision. The GS sequence makes the source that answers an
operation determine its consistency contract:

- GS-01 makes an authority read return current `ENTITY_STATES` plus its
  per-entity revision.
- GS-03 through GS-10 make each materialized-view read follow its owner's
  declared coverage, health, staleness, cycle, fallback, and reset behavior.
- GS-12 makes the required GraphQL gateway conformant and documents which
  source answers each field or result.

The GS target requires an unavailable optional tier to return its declared
lower-tier behavior or an explicit capability-unavailable result; it must not
become an empty authoritative answer. Current behavior remains provisional and
operation-specific until the applicable GS slice proves conformance. See the ADR-090
[program](../proposals/graph-state-read-write-program.md) for the canonical order.

## Related

**Concepts**
- [Knowledge Graphs](04-knowledge-graphs.md) - The data model being queried
- [GraphRAG Pattern](09-graphrag-pattern.md) - Community-based search operations
- [PathRAG Pattern](10-pathrag-pattern.md) - Graph traversal operations
- [Event-Driven Basics](01-event-driven-basics.md) - NATS fundamentals
