# Graph Mutation Contracts

SemStreams has one physical graph authority: graph-ingest writes `ENTITY_STATES`. Components ask it to mutate graph
state through a typed NATS request/reply port. There is no framework concept of semantic predicate ownership.

## The four operations

The `semstreams.graph.mutation` v1 interface admits exactly four subjects:

| Operation | Meaning | Concurrency evidence |
|---|---|---|
| `graph.mutation.entity.create` | Birth one absent entity with its initial facts | Atomic KV create |
| `graph.mutation.entity.reconcile` | Replace the complete desired set for named predicates | Required KV revision |
| `graph.mutation.triple.append` | Add exact tuples to existing subjects, suppressing duplicates | Per-subject result |
| `graph.mutation.entity.delete` | Delete one entity if it is still at the observed revision | Required KV revision |

No legacy subject aliases or compatibility request shapes are served. Create is strict: an existing entity returns
`entity_already_exists`. Reconcile, append, and delete are must-exist operations. They never create a missing entity.

## Exact reads are CAS evidence

An authoritative exact read returns the validated entity and the nonzero KV revision from the same `ENTITY_STATES`
entry. The entity's logical `Version` field is not a KV revision. Reconcile and delete decisions must use the revision
returned by the exact read.

The GraphQL exact-entity operation exposes the same pair as `{entity, kvRevision}`. Embedded framework code uses the
narrow exact-reader adapter; application code does not read raw graph KV.

## Projection contracts are local schemas

`projection.Contract` describes the graph shape one component intends to emit. A contract contains:

- an entity pattern;
- optional create-time birth predicates;
- named `reconcile` groups for complete selected-predicate state; and
- named `append` groups for exact evidence tuples.

Contracts validate local intent. They do not reserve predicates, register owners, or prevent another component from
writing the same entity. Two overlapping contracts are valid. If writers race, atomic Create and CAS outcomes expose
the real conflict.

## Retry belongs to the component

The framework sends each mutation request once. A definite `revision_mismatch` proves that no write occurred, but a
retry is safe only when the owning component can reconstruct its intent from fresh authority. The rule engine cannot
replay an old `ExecutionContext`: its reconcile action makes one exact read and one mutation attempt, then returns a
classified mismatch visibly with no second read or mutation.

Lifecycle transitions have different semantics. `Transition` and `TransitionWith` own a bounded local loop that, on
each definite conflict attempt, re-reads authority and rebuilds phase and edge validation, the retained occurrence
chain, audit values, projection, optional mutator output, desired predicates, and expected revision. This is not a
shared retry policy or a claim about `UpdateFromOperator`.

`unavailable` (no responder) and `deadline` (context already done before send) are definite non-commits.
`commit_unknown` means a send was attempted but a timeout, disconnect, malformed reply, or semantically invalid
success reply left the result unproved. The client does not retry it automatically and does not infer authorship from
matching state found later. The component decides whether and how to resolve that ambiguity.

## Eventual references

A relationship to an absent entity is valid graph state. The source edge remains authoritative; dereference,
hydration, or traversal reports the missing object through its typed missing result. SemStreams does not create a
stub, pending-edge record, rollback, or repair workflow. If the object is created later, the next read resolves it
without rewriting the source.

This is an eventually consistent graph, not a financial transaction system. Invalid stored entity bytes are still
reported as per-entity poison, but one missing reference does not stop the graph or change `GRAPH_STATUS`.

## Operations and backups

Graph mutation has no checkpoint, restore, or recovery subsystem. Clustered NATS deployments use their normal
operational backup practices. Edge and offline deployments should maintain NATS data-store backups at useful
checkpoints. SemStreams does not attempt to coordinate or attest those backups.

See [Operating the Projection Mutation Client](../operations/34-projection-mutation-client.md) for Go examples.
