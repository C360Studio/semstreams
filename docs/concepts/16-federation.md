# Federation

How external graph sources (like semsource) send entities into a SemStreams knowledge graph.

## The Core Idea

Federation is not a special system. External services produce entities with triples — the same
`Graphable` interface every internal processor uses. Federation is just graph-ingest receiving
entities from an external source instead of a local one.

```text
┌──────────────┐     WebSocket      ┌──────────────┐     entity.>      ┌──────────────┐
│  semsource   │ ──────────────────► │  WebSocket   │ ────────────────► │ graph-ingest │
│  (external)  │   EventPayload     │    Input     │   BaseMessage     │              │
└──────────────┘                     └──────────────┘                    └──────┬───────┘
                                                                               │
                                                                        ENTITY_STATES KV
```

The `EventPayload` carries an `Entity` with an ID and triples. It implements `Graphable`, so
graph-ingest processes it exactly like any other entity source. No special merge processor, no
separate federation pipeline.

## Entity ID Namespacing

External entities use the same 6-part ID format as internal ones:

```text
org.platform.domain.system.type.instance
```

The `domain.system` segments naturally prevent ID collisions between sources:

```text
acme.ops.semsource.git.repo.myapp       ← from semsource
acme.ops.semstreams.gcs.drone.001       ← local entity
acme.ops.semsource.ast.function.main    ← from semsource
```

No special namespace filtering is needed. The ID format handles isolation by design.

## Relationships Are Triples

External sources encode relationships as triples, not as a separate edge structure. A "calls"
relationship between two functions is a triple like any other fact:

```text
Subject: acme.ops.semsource.ast.function.main
Predicate: source.code.calls
Object: acme.ops.semsource.ast.function.helper
Datatype: @id
```

The `@id` datatype is what makes the object an entity reference. An untyped string is a scalar value even when it
looks like an entity ID. Every federated root ID, triple subject, and `@id` object must satisfy the exact six-part,
256-byte canonical entity contract before it can enter authoritative graph state.

This means federated entities land in the graph and participate in queries, community detection,
and inference exactly like locally-produced entities.

## Ingestion Patterns

There are two ways to wire external sources into a flow, depending on operational needs.

### Pattern A: Single Endpoint, Multiple Sources

One WebSocket input accepts connections from all external sources. All entities publish to the
same subject. Source identity is carried by the entity IDs, not the transport layer.

```json
{
  "federation-input": {
    "type": "input/websocket",
    "config": {
      "mode": "server",
      "server": { "http_port": 8081, "max_connections": 100 },
      "ports": {
        "outputs": [{
          "name": "federated_entities",
          "type": "jetstream",
          "subject": "entity.federated"
        }]
      }
    }
  },
  "graph-ingest": {
    "type": "processor/graph-ingest",
    "config": {
      "ports": {
        "inputs": [{
          "name": "entity_stream",
          "type": "jetstream",
          "subject": "entity.>"
        }]
      }
    }
  }
}
```

**When to use:** Most deployments. Simpler to operate, fewer moving parts. Works well when all
sources are trusted equally and don't need independent backpressure or access control.

### Pattern B: Dedicated Endpoint Per Source

Each external source gets its own WebSocket input on a separate port and subject. Graph-ingest's
`entity.>` wildcard subscription picks up all of them.

```json
{
  "semsource-alpha": {
    "type": "input/websocket",
    "config": {
      "mode": "server",
      "server": { "http_port": 8081 },
      "auth": { "type": "bearer", "bearer_token_env": "SEMSOURCE_ALPHA_TOKEN" },
      "ports": {
        "outputs": [{
          "name": "alpha_entities",
          "type": "jetstream",
          "subject": "entity.federated.alpha"
        }]
      }
    }
  },
  "semsource-beta": {
    "type": "input/websocket",
    "config": {
      "mode": "server",
      "server": { "http_port": 8082 },
      "auth": { "type": "bearer", "bearer_token_env": "SEMSOURCE_BETA_TOKEN" },
      "ports": {
        "outputs": [{
          "name": "beta_entities",
          "type": "jetstream",
          "subject": "entity.federated.beta"
        }]
      }
    }
  }
}
```

**When to use:** When you need per-source authentication, independent rate limiting or
backpressure, separate monitoring per source, or the ability to disconnect one source without
affecting others.

### Choosing Between Patterns

| Concern | Pattern A (shared) | Pattern B (dedicated) |
|---------|-------------------|----------------------|
| Operational simplicity | One port, one config | Port per source |
| Per-source auth | No | Yes |
| Independent backpressure | No | Yes |
| Per-source metrics | By entity ID only | By subject and port |
| Source isolation | Shared connection pool | Full isolation |
| Scaling | Vertical (max_connections) | Horizontal (add inputs) |

Both patterns require zero code changes — they are purely flow configuration decisions.

## What External Sources Must Provide

An external source sends `EventPayload` messages over WebSocket. Each payload must contain:

1. **Entity ID** — canonical six-part identifier, at most 256 bytes
2. **Triples** — `[]message.Triple` with canonical subjects and explicit `@id` relationship objects
3. **Valid schema** — the payload must be registered via `federation.RegisterPayload(domain)`

The receiving SemStreams instance does not need to know the source's internal schema or data
model. If it has an ID and triples, it's a graph entity.
