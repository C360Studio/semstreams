# Entity-ID Contract Clean Cutover

This procedure is the SemStreams-local pre-v1 cutover for canonical entity IDs. The source audit used here is a
bounded fixture-hygiene lint over statically identifiable candidates; it is not implementation-surface coverage or
enforcement evidence. This is a clean break that does not preserve, rewrite, inspect, or roll back incompatible beta
graph state.

## Combined BREAKING Release Entry

This procedure is the one destructive window for the coordinated entity, predicate, and graph-index changes:

- entity IDs are canonical bounded six-part identities;
- `PREDICATE_INDEX` uses the raw fixed-nine-token `predicate3.entity6` layout selected by
  [ADR-078](../adr/078-raw-canonical-predicate-membership-keys.md);
- `PREDICATE_CATALOG` is retired and must not be created, repaired, joined, or read after cutover;
- NAME, PREDICATE, and source-owned INCOMING projections use complete replacement semantics under
  [ADR-077](../adr/077-bounded-owner-discovery-and-incoming-ownership.md); and
- the rule-event constructor, digest identity, PackID, and graph-integration default changes in
  [30 — rule-event identity clean cutover](30-rule-event-identity-clean-cutover.md) land before this restart.

The single breaking tag contains all of these changes. They do not activate through a later routine bump or a
second wipe.

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
2. update predicate producers, exact queries, and reseed sources to the canonical three-part contract; and
3. run both bounded local source gates:

```bash
task entity-id:audit
task predicate:audit
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

For a persistent local NATS account, first create a reviewed command sheet from the exact breaking binary,
composition, and rendered deployment configuration. Record literal bucket names, including configured overrides;
do not execute a script whose context or bucket variables are unset. Select the intended NATS CLI context and
capture it with the command sheet:

```bash
nats context select <exact-cutover-context>
nats context info
```

Replace `<exact-cutover-context>` with the literal reviewed context before execution. Resolve the deletion set from
`graph.FrameworkOwnedBuckets()`, graph-ingest's guard buckets, and the enabled component port bindings. The following
commands are the literal command sheet only for a deployment that uses every current default bucket name:

`CONTEXT_INDEX` is retired by ADR-090 and appears below only so an older beta bucket is removed. A fresh deployment
does not recreate it, and an absent-bucket response is expected.

`STRUCTURAL_INDEX` is likewise retired by ADR-090. Its command removes stale beta state only; it is no longer
framework-owned or recreated.

```bash
nats kv rm ENTITY_STATES
nats kv rm ENTITY_SUFFIX_INDEX
nats kv rm GRAPH_INGEST_APPLIED_SEQ
nats kv rm OUTGOING_INDEX
nats kv rm INCOMING_INDEX
nats kv rm PREDICATE_INDEX
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

This list combines the current `graph.FrameworkOwnedBuckets()`, graph-ingest's `ENTITY_SUFFIX_INDEX` and
`GRAPH_INGEST_APPLIED_SEQ` guard buckets, and explicitly labeled retired beta state. Delete only buckets enabled by
the rendered deployment. If a binding overrides a name, replace the corresponding command with that literal
resolved name. Do not copy this list into a shared account, use wildcard deletion, or remove unrelated operational,
product, workflow, stream, ObjectStore, or upstream source-system state.

`PREDICATE_CATALOG` is intentionally absent from the current-bucket commands because ADR-078 retired it from the
framework inventory. If the pre-cutover deployment has a legacy catalog under an old or overridden name, record
that exact legacy name and its removal separately in the same reviewed maintenance-window command sheet. For an
upgrading deployment that used the old default name, include this explicit legacy-only command:

```bash
# Legacy-only: run only when the reviewed pre-cutover deployment created this old default bucket.
nats kv rm PREDICATE_CATALOG
```

This is not a current framework bucket. Omit the command for fresh deployments; use the literal resolved legacy
name instead when the old deployment overrode it. A fresh breaking deployment must not recreate the catalog.

The wipe is required for more than entity grammar. Old PREDICATE rows use an incompatible hashed layout; old
catalog state has no reader; additive NAME, PREDICATE, and INCOMING memberships may contain stale A/B rows; and old
INCOMING cleanup used the target axis instead of the source owner. A fresh rebuild from canonical `ENTITY_STATES`
establishes raw predicate keys and complete `[A] -> [B] -> []` replacement behind fail-closed readiness.

There is no local beta-state export or preservation step in this procedure. Source data remains in its independent
authoritative system; regenerate canonical Graphables from that source after the wipe.

## Restart and Canonical Reseed

1. Execute the recorded literal start command for the matching breaking tag against the empty account.
2. Confirm graph-ingest creates an empty `ENTITY_STATES` bucket and projection owners create their empty buckets.
3. Confirm `PREDICATE_INDEX` is empty and that no `PREDICATE_CATALOG` bucket was created.
4. Execute only the recorded producer-start and canonical reseed commands that passed the source/configuration gates.
5. Record the authoritative source revision, input count, and resulting `ENTITY_STATES` revision.
6. Poll graph-index status until readiness is true and the indexed revision reaches that entity-state target.
7. Prove raw exact/category/domain predicate queries, owner replacement, relationship, prefix, and search parity.
8. Restart with the same recorded command and no intervening write. Prove the same readiness revision and results.

The concrete service-manager, container, and reseed commands are product-owned because SemStreams cannot infer an
upstream authoritative source. They must appear as literal commands in the product evidence envelope defined by
[31 — sister-repo cutover checklist](31-sister-repo-cutover-checklist.md). “Restart and reseed” without those
commands, revisions, and counts is not release evidence.

If the deployment returns `graph_state_reset_required`, stop. Find and fix the incompatible producer or injected
state before wiping again; repeating the wipe cannot correct a source that still emits malformed identities.

## Required Local Proof

After source, configuration, fixture, schema, and documentation updates are complete, capture green output from:

```bash
task lint
go vet -tags=integration ./...
go vet -tags=live_llm ./...
go test -race ./...
go test -race -tags=integration -p 2 -timeout=20m -count=1 ./...
task schema:generate
git diff --exit-code -- schemas specs
go test ./test/contract/...
go test ./test/release/...
task e2e:core
task e2e:structural
task e2e:statistical
task e2e:semantic
task e2e:agentic
```

The integration race uses `-p 2` to bound concurrent testcontainer packages. Its `-timeout=20m` applies separately
to each package, while `-count=1` disables cached test results so the release evidence is from this execution. Both
tagged vet commands are required; skipping tests at runtime does not replace compile-time vet coverage. The final
five E2E tiers must use the exact breaking revision and fresh volumes. Earlier green evidence from an implementation
slice does not satisfy the final combined gate.

## Explicit Non-Features

This pre-v1 cutover provides no permissive flag, legacy validator, alias ledger, compatibility constructor, dual
reader/writer, persisted-state rewriter, online migration, beta-state preservation contract, or rollback path. A
post-v1 migration design belongs to the operational-retention work; it must not leave pre-v1 compatibility cruft in
the framework.

## Governing Decisions and Changes

- [ADR-077 — bounded owner discovery and source-owned INCOMING
  evidence](../adr/077-bounded-owner-discovery-and-incoming-ownership.md)
- [ADR-078 — raw canonical predicate membership keys](../adr/078-raw-canonical-predicate-membership-keys.md)
- [`graph-index-replacement-semantics`](../../openspec/changes/graph-index-replacement-semantics/proposal.md)
- [`predicate-raw-key-representation`](../../openspec/changes/predicate-raw-key-representation/proposal.md)
