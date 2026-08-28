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

External entities use the same six-part canonical ID as internal ones — the lexical contract
(`openspec/specs/entity-id-contract/spec.md`: exactly six positions, ASCII alphabet, 256 bytes) is enforced at
every graph boundary today:

```text
org.platform.system.domain.type.instance
```

The positions have meanings (ADR-102, #1095). `org.platform` is the **minting deployment authority** — the
composition root's `platform.org` / `platform.id` of the deployment that produced the entity. `system` is the
**source** that produced it: a repository, feed, world, board, API, or framework component. `domain.type` is a
delegated taxonomy. `instance` is the leaf.

```text
acme.dep1.myapp.git.repo.myapp        ← minted by deployment acme.dep1 from the source myapp (a repository)
acme.dep1.gcs.robotics.drone.001      ← minted by acme.dep1 from the source gcs
acme.dep2.myapp.git.repo.myapp        ← the same repository as held by the peer deployment acme.dep2
```

`domain` is a **shared vocabulary, not an exclusive claim.** Two products may both delegate `web`, and the
framework permits it (owner ruling 2026-08-28): they are told apart by `system`, so
`acme.dep1.semsource.web.page.001` and `acme.dep1.semdragon.web.doc.001` are distinct entities, and ADR-099 level 0
is source x taxonomy, so they land in distinct communities. That is what gives the cross-source pattern
`org.platform.*.web.*.*` its meaning — "everything in this taxonomy, whoever produced it". A `system` prefix
(`org.platform.<system>`) narrows the same question back to one source. Nothing in the framework reports an overlap:
two products meaning different things by one token is a vocabulary problem — someone picked the wrong token — and
detecting it at composition time would be the wrong layer.

The **product** that produced an entity (semsource, semmem, …) is provenance — `Triple.Source` and the envelope
`source` — and is never a position of the ID. Isolation between sources of one deployment comes from `system`;
isolation between deployments comes from `org.platform`, and nothing coordinates that pair automatically: two
deployments that choose the same `platform.org` / `platform.id` mint colliding identities, and the ID format does
not isolate them by itself.

What the boundary enforces today is the lexical contract only. Enforcement of the authority pair — a subject whose
`org.platform` is not the deployment's own is rejected unless it arrives on an input port declared as an import
lane, a subject claiming the local pair on an import lane is rejected, and an import is a read-only mirror no local
lane mutates — is the second half of #1095 (`openspec/changes/entity-id-segment-semantics/`, graph-ingest delta).
Until it lands, an entity claiming a foreign or colliding authority is accepted as local truth.

## Relationships Are Triples

External sources encode relationships as triples, not as a separate edge structure. A "calls"
relationship between two functions is a triple like any other fact:

```text
Subject: acme.dep1.myapp.ast.function.main
Predicate: source.code.calls
Object: acme.dep1.myapp.ast.function.helper
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
          "config": {"kind":"jetstream","subjects":["entity.federated"]}
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
          "config": {"kind":"jetstream","subjects":["entity.>"]}
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
          "config": {"kind":"jetstream","subjects":["entity.federated.alpha"]}
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
          "config": {"kind":"jetstream","subjects":["entity.federated.beta"]}
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

1. **Entity ID** — canonical six-part identifier in the order above, at most 256 bytes, carrying the
   producing deployment's own `org.platform`
2. **Triples** — `[]message.Triple` with canonical subjects and explicit `@id` relationship objects
3. **A registered payload type** — the payload's `message.Type` must be registered in the **receiving** binary's
   payload registry through a `RegisterPayloads(reg *payloadregistry.Registry) error` function called from its
   composition root (see [Payload Registry](15-payload-registry.md)). SemStreams has no `federation` package and
   no `EventPayload` type of its own: the `EventPayload` in the diagram above is semsource's
   `graph/event_payload.go` (`semsource.entity.v1`), the shipped example of a federated Graphable.

The receiving SemStreams instance does not need to know the source's internal schema or data
model. If it has an ID and triples, it's a graph entity.
