# Governed Semantic State

SemStreams stores domain facts in `ENTITY_STATES` so they can be queried, watched, indexed, and replayed. For simple
fact ingestion, a `Graphable` payload can emit triples and the graph can merge them.

That is not enough once the same entity is written by more than one component.

## The Problem

A predicate can mean different things operationally:

| Predicate shape | Write meaning |
|---|---|
| Current phase, status, location, configuration | One owner replaces the current value |
| Lifecycle transition state | One owner changes state under compare-and-swap |
| Observation, finding, backlink, evidence | Many writers append facts |
| Foreign edge onto another entity | A producer writes a relationship whose subject is not its primary entity |

If all of these arrive as undifferentiated triples, the graph cannot tell whether a write should replace, append, wait
for a target entity, or be rejected. The failure mode is silent corruption: a gateway update, rule action, lifecycle
transition, or agent tool can remove a fact that another component owns.

## The Contract

Governed semantic state adds an ownership contract above raw triples:

```text
payload type -> graph projection -> predicate-group ownership -> graph-ingest enforcement
```

- **Payload type** tells the system what kind of message or request is flowing.
- **Graph projection** declares the entity pattern and predicates the type can emit.
- **Predicate-group ownership** declares whether those predicates are replaced, CAS-transitioned, appended, or emitted
  as foreign edges.
- **graph-ingest enforcement** rejects overlapping owners and stale owner leases at the write boundary.

The payload registry and ownership registry are not competing systems. The payload registry is local type lookup for
`BaseMessage` decoding. The ownership registry is distributed arbitration for shared graph state. Projection contracts
are the bridge between them.

## When It Matters

Use governed semantic state when a domain has:

- more than one writer for the same entity family
- lifecycle or workflow state stored as graph facts
- PATCH/PUT style replacement of resource fields
- rules or agents that write triples back into the graph
- regulated or operator-facing audit requirements
- cross-entity edges produced during ingestion

Do not use it for high-volume opaque execution traces, raw telemetry streams, or one-shot requests. Those belong in
JetStream streams, ObjectStore, or component-specific buckets with graph references when needed.

## Why It Is Still Flow-Based

The flow does not change: components still emit messages, `Graphable` still describes entities, and graph processors
still react through KV watches. The added contract makes the write side explicit before data enters shared state.

The practical rule is:

```text
Facts can flow freely after their write semantics are declared.
```

That keeps the graph usable as shared state without requiring every consumer to build a private CQRS mirror.

## Related Documents

- [Graphable Interface](../basics/03-graphable-interface.md)
- [Payload Registry](15-payload-registry.md)
- [KV Twofer](02-kv-twofer.md)
- [Streams vs KV Watches](03-streams-vs-kv-watches.md)
- [Orchestration Layers](14-orchestration-layers.md)
- [ADR-056: Authoritative Semantic State](../adr/056-authoritative-semantic-state.md)
