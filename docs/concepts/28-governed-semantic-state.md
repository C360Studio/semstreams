# Governed Semantic State

SemStreams stores domain facts in `ENTITY_STATES` so they can be queried, watched, indexed, and replayed. For simple
fact ingestion, a `Graphable` payload can emit triples and the graph can merge them.

That is not enough once the same entity is written by more than one component.

## Identity, Facts, and Governance Are Different Layers

Graph governance is a contract *about* writes — **not a new metadata model and not a second graph**. It is a
governed projection over the canonical graph identity you already have. Three (really four) layers answer
distinct questions and do not compete:

| Layer | Answers | Mechanism |
|---|---|---|
| **Entity ID** | "What entity is this?" | the stable 6-part address `org.platform.domain.system.type.instance` |
| **rdf/type facts** | "What does it classify as; how does it export/read?" | RDF / SOSA / SensorML triples |
| **MessageType + projection contract** | "Which producer is writing, with what write semantics?" | the payload type + its declared graph projection |
| **Ownership** | "Who may write which predicate groups and foreign edges, and how?" | runtime arbitration over predicate-group claims |

IDs and RDF facts remain the **portable semantic shape** — what external tools and other systems read.
Governance adds the **operational contract** for safely *producing and updating* those facts. A producer still
writes `sensorml.process.type`, `uid`, `label`, `isHostedBy`, …; the new bit is that the write is stamped with a
producer MessageType and an ownership claim that says "this producer owns these predicates; this foreign edge is
expected; route it to its subject." That is governance *around* the graph, not a competing canon.

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

## Runtime Config vs Contract Migration

SemStreams has runtime-configurable surfaces: rule thresholds, routes, model selection, prompts, flow shape, and
component behavior can be changed while the system is running when the component supports it.

Ownership contracts are different. They declare data-plane authority over entity patterns, predicate groups, write
modes, producer identities, and foreign-edge behavior. Changing that authority is closer to a schema migration or
online DDL than ordinary hot config.

Runtime config may change behavior *inside* an already declared ownership contract. For example, a rule pack can
hot-reload the threshold that decides when it emits `alert.active` if that predicate is already part of its projection
contract. A new owned predicate, write mode, entity pattern, producer identity, or foreign-edge mode is a contract
migration.

The current model treats static projection contracts as deployment-time declarations with runtime liveness. Owners
renew presence through `OWNER_PRESENCE`; they do not renegotiate predicate-claim shape during normal hot reload.

Future online contract migration would need an explicit protocol:

1. Register the proposed contract and check for overlapping claims.
2. Quiesce or fence old writes that would conflict with the new authority.
3. Backfill, reconcile, or validate existing graph state as needed.
4. Flip producers to the new contract.
5. Observe the new owner and retire the old contract when safe.

The practical rule is:

```text
Hot reload changes behavior. Contract migration changes authority.
```

## What Governance Does Not Do

Keep these edges sharp:

- **It is not semantic dedupe.** Ownership arbitrates *writes* over `(entity-ID pattern, predicate)` cells; it does
  not reconcile two IDs that denote the same real-world object. If one component mints a deterministic child ID and a
  later client posts that same object as a standalone entity under a different ID/UID, the graph holds two entities —
  governance will not merge them. Dedupe is a separate concern.
- **Aliases are read compatibility, not equal write contracts.** Ownership and indexes are exact-predicate driven. A
  read path may understand both a canonical predicate and an alias (e.g. `rdf.type` ↔ `sensorml.process.type`), but
  writers must emit the canonical framework constant — an alias is not an interchangeable write/ownership key.
- **It is not authorization (yet).** On the current release this is provenance + write-semantics discipline with
  **observe-only** enforcement: overlaps and unclaimed foreign edges are metered and logged, not rejected. Hard
  rejection and owner-token write leases are later increments; cryptographic authentication of authorship is reserved
  (ADR-057).
- **Ownership arbitration is not a domain fact — but provenance can be.** Two halves:
  - *Do not* encode ownership arbitration as domain triples. Ownership claims live in the `OWNER_CLAIMS` substrate and
    arbitrate who may write predicate groups; they are never facts on the entity.
  - *Do* allow explicit provenance triples when they describe entity **materialization, source, audit, or lineage**.
    For example, `core.identity.stub_owner` records which producer caused a referential-integrity stub to be
    materialized — that is provenance-on-entity, **not** an ownership claim and **not** an enforcement rule.

  This matters for readers: graph consumers (e.g. CS API clients) will see provenance-like facts in the graph and must
  not mistake them for authorization or write ownership.

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
