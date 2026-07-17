# Entity-ID Contract Clean Cutover

This procedure is the SemStreams-local pre-v1 cutover for canonical entity IDs. The source audit used here is a
bounded fixture-hygiene lint over statically identifiable candidates; it is not implementation-surface coverage or
enforcement evidence. This is a clean break that does not preserve, rewrite, inspect, or roll back incompatible beta
graph state.

## Breaking Contract

A literal entity ID:

- has exactly six dot-separated positions: `org.platform.domain.system.type.instance`;
- is at most 256 serialized bytes, including separators;
- uses only ASCII segments matching `[A-Za-z0-9][A-Za-z0-9_-]*`; and
- is validated without trimming, lowercasing, escaping, hashing, Unicode normalization, or any other rewrite.

The related languages are deliberately distinct:

- a declaration pattern has exactly six positions, each either a complete literal segment or the complete token
  `*`;
- a query prefix has one through six literal positions; an API that supports match-all handles its empty sentinel
  before calling the prefix validator; and
- a triple with datatype `@id` contains a string that is itself a canonical literal entity ID.

## Source and Configuration Update

Before starting the breaking binary:

1. update every entity constructor, literal, relationship reference, pattern, prefix, schema example, test fixture,
   seed, and exact query to the canonical contract;
2. run the bounded local source gate:

```bash
task entity-id:audit
```

The command must be green. An intentional negative test requires one exact source classification; it is not
permission for a whole file, directory, or value family.

## Stop and Wipe Incompatible Local State

Stop every writer and SemStreams process that uses the target NATS account. If the deployment is the repository's
Docker e2e stack, remove its containers and volumes:

```bash
docker compose -f docker/compose/e2e.yml down -v --remove-orphans --timeout 15
docker compose -f docker/compose/tiered.yml --profile structural down -v --remove-orphans --timeout 15
docker compose -f docker/compose/tiered.yml --profile statistical down -v --remove-orphans --timeout 15
docker compose -f docker/compose/tiered.yml --profile semantic down -v --remove-orphans --timeout 15
docker compose -f docker/compose/agentic.yml down -v --remove-orphans --timeout 15
```

For a persistent local NATS account, select the intended NATS CLI context and derive the destructive bucket set
from the rendered deployment configuration and the framework-owned bucket constants:

```bash
nats context info
```

Then remove the authoritative graph state and every derived graph projection enabled by that deployment. The
default core set is:

```bash
nats kv rm ENTITY_STATES
nats kv rm ENTITY_SUFFIX_INDEX
nats kv rm GRAPH_INGEST_APPLIED_SEQ
nats kv rm OUTGOING_INDEX
nats kv rm INCOMING_INDEX
nats kv rm PREDICATE_INDEX
nats kv rm PREDICATE_CATALOG
nats kv rm NAME_INDEX
nats kv rm CONTEXT_INDEX
nats kv rm ALIAS_INDEX
nats kv rm SPATIAL_INDEX
nats kv rm TEMPORAL_INDEX
nats kv rm TEMPORAL_INDEX_REVERSE
nats kv rm EMBEDDING_INDEX
nats kv rm EMBEDDING_DEDUP
nats kv rm EMBEDDINGS_CACHE
nats kv rm COMMUNITY_INDEX
nats kv rm ANOMALY_INDEX
nats kv rm STRUCTURAL_INDEX
```

This list is the union of `graph.FrameworkOwnedBuckets()` and graph-ingest's `ENTITY_SUFFIX_INDEX` and
`GRAPH_INGEST_APPLIED_SEQ` guard buckets at the time of this release. Bucket names may be overridden by deployment
configuration. Do not copy this list into a shared account without comparing it to the rendered configuration, and
do not remove unrelated operational or product KV buckets.

There is no local beta-state export or preservation step in this procedure. Source data remains in its independent
authoritative system; regenerate canonical Graphables from that source after the wipe.

## Restart and Canonical Reseed

1. Start the matching breaking SemStreams binary against the empty account.
2. Confirm graph-ingest creates an empty `ENTITY_STATES` bucket and projection owners create their empty buckets.
3. Start only producers that have passed the new source/configuration gates.
4. Reseed from canonical source events or regenerate Graphables from the authoritative source system.
5. Poll graph index status until readiness is true and the indexed revision reaches the current entity-state target.
6. Run exact entity, exact predicate, namespace, relationship, prefix, and search queries against pinned expected IDs.
7. Restart SemStreams without another write and prove replay reaches the same readiness revision and query results.

If the deployment returns `graph_state_reset_required`, stop. Find and fix the incompatible producer or injected
state before wiping again; repeating the wipe cannot correct a source that still emits malformed identities.

## Required Local Proof

After source, configuration, fixture, schema, and documentation updates are complete, capture green output from:

```bash
task lint
go test -race ./...
go test -race -tags=integration ./...
task schema:generate
git diff --exit-code -- schemas specs
go test ./test/contract/...
task e2e:core
task e2e:structural
task e2e:agentic
task e2e:semantic
```

The final e2e runs must use current source and fresh volumes. Earlier green evidence from the first implementation
slice does not satisfy the final breaking gate.

## Explicit Non-Features

This pre-v1 cutover provides no permissive flag, legacy validator, alias ledger, compatibility constructor, dual
reader/writer, persisted-state rewriter, online migration, beta-state preservation contract, or rollback path. A
post-v1 migration design belongs to the operational-retention work; it must not leave pre-v1 compatibility cruft in
the framework.
