# Rule-Event Identity Clean Cutover

**Historical cutover evidence — not an active release procedure.**

This document records the 2026 cutover plan. Stable release adoption starts on newly provisioned NATS storage; do not
execute this body as a release gate. Its no-shim conclusions remain evidence. Typed graph-poison recovery is governed
by [operations 17](17-predicate-cutover-clean-wipe.md) and [operations 33](33-graph-poison-response-runbook.md).

This procedure is the SemStreams-local pre-v1 cutover for graph-event constructors, framework-derived alert and
trigger identities, and rule-pack producer identity. It is a clean break with no compatibility constructor, default
pack identity, dual identity, alias ledger, or rollback path.

## BREAKING Release Entry

- Graph-event constructors now return `(*Event, error)`; callers must handle the error.
- Legacy `alert_...` identities are replaced by canonical framework alert digests.
- Legacy `rule.<id>.triggered` and `test.entity.<id>` identities are replaced by pack/rule trigger digests.
- Every rule processor requires an explicit, composition-unique `pack_id`, including when graph integration is
  disabled; dotted PackIDs are invalid because PackID is one KV token.
- Duplicate enabled PackIDs fail composition before activation.
- Graph-event batches are preflighted even when graph integration is disabled; malformed batches fail atomically.
- `enable_graph_integration` now defaults from `true` to `false`; every deployment that requires rule-event
  publication must set it explicitly.

## Breaking Contract

Graph-event constructors return `(*graph.Event, error)` and validate the complete candidate before returning.
Framework alert IDs use `semstreams.framework.graph.rules.alert.<full-lowercase-sha256>`. Rule triggers use
`semstreams.framework.graph.rules.trigger.<full-lowercase-sha256>` derived from the exact rule PackID and rule ID.

Every rule-processor configuration declares a stable non-empty `pack_id`, whether graph integration is enabled or
disabled. PackID is 1–246 ASCII bytes matching `[A-Za-z0-9_=-]+`, is one literal KV token, has no default or
normalization, and is immutable for the process lifetime. Two enabled rule processors in one composition cannot
declare the same PackID.

PackID is passed through `rule.NewConfig(packID)`, which returns `(Config, error)`; the old exported anonymous
`DefaultConfig()` constructor no longer exists. Direct expression and test-rule factories also require the exact
PackID so no rule can activate before its trigger identity is known.

Every graph-event constructor returns `(*graph.Event, error)`. Constructors validate before return, copy the
top-level properties map, reject envelope or constructor-owned property collisions, and leave nested reference
values caller-owned and immutable-by-contract. The rule publisher structurally validates every member, then encodes
the complete batch before notification or publication in both integration modes. An encoding failure discards all
prepared frames before NATS, retry, callback, success counter, or publication metric. One bounded
`graph_event_rejections_total{lane,reason}` sample records an invalid batch; label values never contain IDs,
predicates, or caller data.

Alert identities are occurrence-scoped because their timestamp is digest-bearing. Different instants therefore add
different alert entities to `ENTITY_STATES`; this is audit semantics and must be included in the ADR-073 operational
retention budget. A future digest-domain version creates a new entity family and requires an explicit cutover — it is
not an in-place migration or alias.

## Source and Configuration Update

Before starting the breaking binary:

1. handle every graph-event constructor error without an ignored return or compatibility wrapper;
2. migrate direct graph-event producers and alert/trigger ID expectations to the framework digest identities;
3. assign every rule processor, including disabled instances, a stable composition-unique `pack_id` and set
   `enable_graph_integration` explicitly (`true` wherever rule events must reach the graph);
4. update schemas, generated configuration, examples, fixtures, and owned-product documentation; and
5. compile every owned producer and run structural and agentic e2e proof.

The entity-ID wipe, restart, reseed, replay, and query-parity procedure is
[`29-entity-id-contract-clean-cutover.md`](29-entity-id-contract-clean-cutover.md). Apply this identity migration before
that clean restart so no legacy event or anonymous rule-pack producer can repopulate the empty graph. That same
window activates ADR-077 replacement semantics and ADR-078's raw `predicate3.entity6` layout with no
`PREDICATE_CATALOG`; it is not a later derived-state migration.

## Required Proof

Capture green output after all constructor, schema, configuration, and fixture migrations are complete:

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
to each package, and `-count=1` makes the release evidence uncached. Both tagged vet commands and all five
fresh-volume E2E tiers are mandatory for the combined breaking tag, even when a narrower rule-event slice passed
earlier.

Owned products must record the exact SemStreams revision, their migrated PackIDs, explicit graph-integration modes,
constructor call sites, and affected product E2E result before v1 release and archive. Current downstream adoption
proof is defined by the canonical [beta.159 to beta.160 migration guide](migration-beta159-to-beta160.md). A named
consumer must prove that the first trigger creates or upserts the stable trigger entity and later triggers
replace/update it. A
must-exist update consumer rejects the first trigger; append semantics violate the stable-entity contract. The
current bounded no-consumer audit remains an open release item until its normative amendment is approved or a
consumer supplies that proof.

The combined wipe and index activation are governed by
[ADR-077](../adr/077-bounded-owner-discovery-and-incoming-ownership.md),
[ADR-078](../adr/078-raw-canonical-predicate-membership-keys.md),
[`graph-index-replacement-semantics`](../../openspec/changes/graph-index-replacement-semantics/proposal.md), and
[`predicate-raw-key-representation`](../../openspec/changes/predicate-raw-key-representation/proposal.md).
