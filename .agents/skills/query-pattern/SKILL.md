---
name: query-pattern
description: >-
  Choose an admitted remote operation or operation-specific typed adapter.
  General embedded and MCP graph front doors are not implemented contracts.
argument-hint: [access scenario or caller description]
---

# Query Access Pattern Selection

## What is the access scenario?

$ARGUMENTS

## Implemented and Future Access Patterns

The required gateway is provisional until GS-12 makes it conformant GraphQL.
There is no canonical general embedded graph front door.
A controlled remote caller may use an enumerated admitted facade operation. If no
named typed adapter exists for an embedded operation, that operation is not
admitted; raw KV or subjects are not a fallback.

| Pattern | Best For | Key Property |
|---------|----------|-------------|
| **HTTP graph facade** | Remote apps, admitted operations only | Hand-written GraphQL-shaped routing |
| **Typed adapter** | Embedded service, for its named operation | Admitted operation-specific contract |
| **MCP** | Future AI-agent access | No implemented SemStreams graph contract yet |

## Two-Step Decision

```
Step 1: Which front door fits the caller?

  External app / web frontend  --> Admitted HTTP operation
  AI agent / LLM               --> No canonical graph MCP surface yet
  Internal service             --> Admitted operation-specific typed adapter
  Projection owner / operator  --> Declared bucket seam / diagnostics

Step 2: What source does that operation declare?

  Authority-backed             --> Today: value only; GS-01 target: value + revision
  Named materialized view      --> Today: per-operation; GS-03+ target: owner status
```

## Decision Matrix

| Factor | HTTP graph facade | MCP | Typed adapter |
|--------|---------|-----|-------------|
| Latency | Higher (HTTP) | Not defined | Lowest (direct) |
| Schema control | Hand-written facade | Not implemented | Named operation contract |
| Auditability | Metrics/logs; no audit contract | Not implemented | Logs; no audit contract |
| External access | Yes | No graph surface | No (internal only) |
| Discovery | Advertised operations may drift | No graph tool list | Named adapter contract |

## Common Combinations

| System Type | Recommended Pattern |
|------------|---------------------|
| Web app backend | Admitted remote operations only |
| AI-powered system | Admitted HTTP operations; add no MCP assumption |
| Microservice mesh | Operation-specific typed adapters when admitted |
| Full platform | Combine only admitted operation contracts |

## Key Points

- Front door and answer source are separate decisions. Exact-entity authority
  uses whichever admitted front door implements it.
- Protocol does not determine consistency. Today the entity operation returns a
  raw value without revision, and view behavior is operation-specific. GS-01
  targets authority value plus revision; GS-03 through GS-10 make owner status
  normative one owner at a time.
- The current remote surface is GraphQL-shaped, but is not a schema executor or
  selection-set projector.
- `graph/query.Client` mixes direct KV and RPC and is not an adopter default.
- An embedded operation is admitted only through its specific typed adapter.
- Raw KV and subjects are not application fallbacks.
- MCP graph reads are unavailable until an endpoint and graph tools are
  implemented and specified.
- Direct bucket access is for declared owners and operator diagnostics.

## GraphRAG vs PathRAG

Choose query strategy only after confirming the operation exists on the selected
access surface:

| Pattern | Use When | Returns |
|---------|----------|---------|
| **GraphRAG** | Discovery, Q&A, "what do we know about X?" | Community-scoped results with summaries |
| **PathRAG** | Impact analysis, dependencies, "what's affected by X?" | Bounded traversal from known entity |

Read `docs/concepts/11-query-access.md` for full documentation.
Read `docs/concepts/09-graphrag-pattern.md` for GraphRAG details.
Read `docs/concepts/10-pathrag-pattern.md` for PathRAG details.
