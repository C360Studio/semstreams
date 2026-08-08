# NATS KV Key Migration Ledger

This ledger is the production-boundary baseline for the
[shared NATS KV key contract](../concepts/31-nats-kv-key-contract.md). It assigns existing paths; it does not authorize
changing their bytes or rejection behavior.

## Boundary inventory

Each entry records current bytes, responsible component, missing decision, rebuild effect, owning change, shared-contract
status, and migration state in that order.

Shared-contract status has four allowed values:

- `conforming`: current bytes and construction have the required syntax, bounds, and filter proof;
- `unassessed`: an owner-specific bound, codec, layout, or filter proof is still missing;
- `nonconforming`: a known identity collision or key/filter grammar violation exists; and
- `out-of-scope`: the path does not use the NATS KV key capability.

Migration state has five allowed values:

- `not-required`: no physical-byte or acceptance change is currently required;
- `assigned`: the owner and owning change are recorded, but the missing decision has not established whether a
  migration is required;
- `pending`: owner adoption or a known correction requires a separately approved change;
- `complete`: an authorized migration and any required rebuild have landed; and
- `out-of-scope`: no KV-key migration applies.

- `natsclient.KVStore.Get/Put/Create/Update/Delete`: caller key unchanged; each bucket owner; domain bound and
  literal/opaque choice; owner-specific; each owning change; unassessed; assigned.
- `KVStore.UpdateWithRetry/UpdateJSON`: caller key unchanged and missing rows use raw `bucket.Create`; each bucket
  owner; same decision as direct operations; owner-specific; each owning change; unassessed; assigned.
- `KVStore.Keys`: no caller filter; each bucket owner; no key construction gap; none; natsclient; conforming;
  not-required.
- `KVStore.KeysByPrefix`: caller prefix plus `>` unchanged; each bucket owner; token-boundary and complete-filter
  proof; none unless bytes change; each owning change; unassessed; assigned.
- `KVStore.KeysByFilter`, `FilteredKeys`, `Watch`, and raw `Watch/WatchAll`: caller filter unchanged; each bucket or
  watcher owner; wildcard grammar and semantic arity; none unless bytes change; each owning change; unassessed;
  assigned.
- Raw bucket Get/Put/Create/Update/Delete/Purge/List: caller or builder bytes unchanged; each bucket owner; domain
  bound and physical codec; owner-specific; each owning change; unassessed; assigned.
- Flow, template, and persona stores: raw IDs; `flowstore`, `flowtemplate`, and `persona`; public ID bounds and literal
  syntax; possible clean bucket rebuild; future ID contract; unassessed; pending.
- Runtime config: fixed keys plus `components.<name>` and `services.<name>`; `config`; component/service name semantics;
  possible config migration; config key contract; unassessed; pending.
- `sanitizeNATSKey`: spaces replaced with `_`; `config`; lossy identity collision; explicit semantic migration;
  config key contract; nonconforming; pending.
- Lifecycle entity state and watches: entity IDs and workflow patterns; `pkg/lifecycle`; six-part entity bound and
  filter grammar; possible state rebuild; entity-ID contract; unassessed; pending.
- Entity state, suffix, and ingest guard: six-part IDs, suffixes, and stream-derived guards; graph ingest; entity and
  stream bounds; bucket-specific rebuild; graph ingest contract; unassessed; pending.
- PREDICATE, PREDICATE_CATALOG, NAME, and INCOMING: current graph-index composite and catalog keys; graph index; fixed
  arity and owner filters; mandatory derived rebuild when current-layout reconciliation activates; graph-index
  reconciliation; unassessed; pending.
- OUTGOING: current graph-index entity-owned key; graph index; entity bound; rebuild only if a later owner decision
  changes bytes or acceptance; graph-index reconciliation; unassessed; pending.
- ALIAS: raw alias exact key with entity ID in the value and variable token arity; graph-index ALIAS; alias identity
  bound and literal/opaque/owner-discovery choice; rebuild or migration only if the owning decision changes key bytes
  or ownership; graph-index ALIAS representation and ownership change; unassessed; assigned.
- Spatial and temporal indexes: geohash/time composite keys; spatial/temporal graph indexes; axis bounds and arity;
  derived rebuild; spatial/temporal index specs; unassessed; pending.
- Query exact/prefix/filter reads: entity IDs and graph-index filters; graph query; consume owning index contract;
  follows index rebuild; graph-index reconciliation; unassessed; pending.
- Embedding cache/index/dedup: SHA-256 and entity-derived keys; graph embedding; deliberate hash and entity bounds;
  derived rebuild if changed; embedding storage contract; unassessed; assigned.
- Community index: level prefixes plus entity/community IDs; graph clustering; integer and ID bounds; derived
  rebuild if changed; clustering contracts; unassessed; assigned.
- Anomaly store and indexes: anomaly IDs, enums, entity IDs, and truncated SHA-256; graph inference; deliberate hash
  collision model and ID bounds; derived rebuild if changed; anomaly storage contract; unassessed; assigned.
- Rule state, schedules, and config: fixed prefixes plus rule/action IDs; rule processor; rule/action ID bounds;
  owner-specific; rule persistence contract; unassessed; pending.
- Agent loop, tool, research-graph, and memory stores: fixed prefixes plus loop/run/entity IDs; agentic processors;
  loop/run/entity bounds; owner-specific; agentic persistence contracts; unassessed; pending.
- Completion watchers: configured bucket plus `WatchAll`; `pkg/dispatch`; declared all-keys filter and bucket
  authority; none unless key bytes change; dispatch persistence contract; unassessed; assigned.
- Message-logger KV diagnostics: operator-supplied key/filter; service diagnostics; authorization plus filter grammar;
  none; diagnostics API contract; unassessed; pending.
- ObjectStore names and keys: registered Store contract rather than KV key suffixes; storage/objectstore; outside this
  capability; none; ObjectStore contract; out-of-scope; out-of-scope.

## Explicit production call-site assignments

The following assignments make local and configurable boundaries explicit. They use the same seven fields and
allowed values as the boundary inventory.

- `component/config_validator.go:ValidateAndPersistComponentConfig`: caller-produced component config key through the
  persister interface; component configuration; component key bound; possible config migration; config key contract;
  unassessed; pending.
- `processor/agentic-governance/violation.go:ViolationHandler.storeViolation`: `violation:<violation.ID>` with a
  literal colon; agentic governance audit; replacement-free physical layout and ID bound; byte change and migration
  required because `:` is not accepted by NATS KV; governance persistence contract; nonconforming; pending.
- `flowstore/manager.go`: raw `flow.ID` Create/Put/Get/Delete, raw Keys, and caller Watch pattern; flow store; flow-ID
  bound plus filter grammar; possible flow bucket rebuild; flow identity contract; unassessed; pending.
- `flowtemplate/manager.go`: raw template ID Create/Put/Get/Delete and Keys; flow templates; template-ID bound;
  possible template bucket rebuild; template identity contract; unassessed; pending.
- `persona/manager.go`: raw persona ID Create/Put/Get/Delete and Keys; personas; persona-ID bound; possible persona
  bucket rebuild; persona identity contract; unassessed; pending.
- `config/manager.go`: fixed `version`, `platform`, `nats`, and `model_registry` keys plus lossy
  `components.<sanitizeNATSKey(name)>` and `services.<sanitizeNATSKey(name)>`; runtime config; fixed-key declaration and
  collision-free name layout; config migration required for lossy names; config key contract; nonconforming; pending.
- `pkg/lifecycle/manager.go` and `manager_query.go`: raw six-part entity IDs, ListKeys, configured entity filters, and
  WatchAll; lifecycle state; entity bound and workflow-filter grammar; follows ENTITY_STATES migration; entity-ID and
  lifecycle contracts; unassessed; pending.
- `processor/graph-ingest/component.go` and `keyed_ingest.go`: raw entity IDs, suffix/type-instance keys, and
  entity/stream ingest-guard composites; graph ingest; entity, suffix, and stream bounds; bucket-specific rebuild;
  graph ingest contract; unassessed; pending.
- `processor/graph-index/component.go` plus PREDICATE, NAME, and INCOMING codec paths: current PREDICATE,
  PREDICATE_CATALOG, NAME, and INCOMING composites, source-owner filters, and entity-state watches; graph index; exact
  arity and owner-filter decisions; mandatory derived rebuild when current-layout reconciliation activates;
  graph-index reconciliation; unassessed; pending.
- `processor/graph-index/component.go`: current OUTGOING entity-owned key; graph index; entity bound; rebuild only if
  a later owner decision changes bytes or acceptance; graph-index reconciliation; unassessed; pending.
- `processor/graph-index/component.go:UpdateAliasIndex/DeleteFromAliasIndex`: raw alias exact key mapped to an entity-ID
  value with variable token arity; graph-index ALIAS; alias identity bound and literal/opaque/owner-discovery choice;
  rebuild or migration only if the owning decision changes key bytes or ownership; graph-index ALIAS representation
  and ownership change; unassessed; assigned.
- `graph/query/client.go` and `processor/graph-query`: raw entity/geohash Gets, graph-index exact/prefix/filter reads,
  community watch, and entity-state WatchAll; graph query; consume each owning bucket's key contract; follows owning
  index rebuild; graph-index reconciliation and spatial contracts; unassessed; pending.
- `graph/clustering/storage.go` and enhancement workers: level/community/entity composites plus community watches;
  graph clustering; level and ID bounds; derived rebuild if changed; clustering storage contract; unassessed; assigned.
- `graph/embedding/cache.go`, `storage.go`, and workers: SHA-256 text hashes, entity keys, dedup keys, and watches;
  graph embedding; deliberate hash declaration plus entity bounds; derived rebuild if changed; embedding storage
  contract; unassessed; assigned.
- `graph/inference/storage.go` and review workers: anomaly IDs, enum indexes, entity IDs, truncated SHA-256 pair
  indexes, exact Gets, prefix filters, and WatchAll; graph inference; hash collision model and ID bounds; derived
  rebuild if changed; anomaly storage contract; unassessed; assigned.
- `processor/graph-index-spatial` and `processor/graph-index-temporal`: geohash/time/entity composites, reverse keys,
  prefix filters, and entity watches; spatial/temporal graph indexes; axis bounds and exact arity; derived rebuild;
  spatial and temporal index contracts; unassessed; pending.
- `processor/rule/kv_config_integration.go`, `entity_watcher.go`, state and schedule trackers: `rules.<ruleID>`,
  execution/schedule keys, configured bucket names, and entity filters; rule processor; rule/action/entity bounds and
  filter grammar; owner-specific migration; rule persistence contract; unassessed; pending.
- `processor/rule/actions.go:ActionExecutor.executeUpdateKV` through `kv_writer.go:natsKVWriter`: arbitrary
  rule-configured and variable-substituted bucket/key bytes reach `KVStore.UpdateJSON` or `KVStore.Put`; rule/action
  orchestration; bucket-name grammar plus substituted key semantics and bounds; existing domain buckets may contain
  unconstrained keys and any byte change requires owner-specific data classification/rebuild; rule update-KV action
  contract; nonconforming; pending.
- `processor/agentic-loop`, `processor/agentic-dispatch/http.go`, and `processor/agentic-tools/store.go`: loop/run/tool
  fixed prefixes plus raw IDs, ListKeys, exact Gets, and WatchAll; agentic runtime; loop/run/tool ID bounds;
  owner-specific migration; agentic persistence contract; unassessed; pending.
- `agentic/agentrun/nats_reader.go`: raw entity-ID Get from ENTITY_STATES; agent-run reader; six-part entity bound;
  follows ENTITY_STATES migration; entity-ID contract; unassessed; pending.
- `processor/gated-dag/executor.go`: raw entity-ID Get from the configured entity-state bucket; gated DAG; six-part
  entity bound and bucket authority; follows entity-state migration; gated-DAG persistence contract; unassessed;
  pending.
- `pkg/dispatch/completion_watcher.go`: configured bucket plus WatchAll; dispatch completion; bucket authority and
  all-keys filter declaration; none unless bytes change; dispatch persistence contract; unassessed; assigned.
- `processor/agentic-tools/executors/register_graph_query.go` and
  `frameworkcapabilities/graphresearch/register_tool.go`: raw entity/loop IDs through KVStore Get/Put; graph query
  and graph-research tools; entity/loop bounds; follows owning bucket migration; agentic tool contracts; unassessed;
  pending.
- `processor/agentic-tools/loop_result.go:normalizeLoopID`: drops every prefix through the final dot before building
  `COMPLETE_<bareID>` for KV Get; agentic loop-result tool; decide whether full and prefixed loop IDs are semantically
  equivalent and bound accepted prefix shapes; no stored-byte change if equivalence is governed, otherwise caller
  migration or an AGENT_LOOPS layout/rebuild; agentic loop-ID contract; nonconforming; pending.
- `processor/research-graph-{assess,classify,execute,route,synthesize}` adapters: fixed phase prefixes plus raw loop IDs
  through KVStore Get/Put; research graph; loop-ID bound and prefix arity; owner-specific migration; research-graph
  persistence contract; unassessed; pending.
- `service/message_logger_http.go` and `message_logger_kv_watch.go`: operator-selected bucket, raw Keys/Get, and
  operator-supplied Watch filter; diagnostics; authorization and wildcard grammar; none; diagnostics API contract;
  unassessed; pending.
- `service/graph_triples_http.go`: ENTITY_STATES Keys and raw key Get; graph diagnostics; entity-ID bound; follows
  ENTITY_STATES migration; entity-ID contract; unassessed; pending.
## Production source coverage

The baseline audit searched non-test Go sources for `jetstream.KeyValue`, `*natsclient.KVStore`,
`GetKeyValueBucket`, `CreateKeyValueBucket`, direct wrapper operations, raw bucket operations, list/watch entry points,
key builders, reversible codecs, hashes, and lossy sanitation. Local variables returned from bucket lookup/create
were traced to their operations, which caught boundaries such as governance violation storage that do not declare a
KV field. The covered production source families are:

- `natsclient`, `component`, `config`, `flowstore`, `flowtemplate`, and `persona`;
- `pkg/lifecycle` and `pkg/dispatch`;
- `graph/query`, `graph/embedding`, `graph/clustering`, `graph/structural`, and `graph/inference`;
- graph ingest, index, query, clustering, embedding, spatial, and temporal processors;
- rule, agentic-loop, agentic-tools, agentic-dispatch, and research-graph processors;
- service message-logger paths.

## Transferred SemTeams obligations

ADR-075 removes OASF projection and AGNTCY directory registration from the SemStreams framework composition and
transfers that product bundle to SemTeams. Consequently, the deleted `processor/oasf-generator` and
`output/directory-bridge` paths are not current SemStreams production call sites and do not remain as framework rows
in this ledger.

The underlying obligations transfer with ownership rather than disappearing. Before SemTeams releases its owned
bundle, its owner notice and release gate must inventory and prove:

- the raw entity-ID Get from `ENTITY_STATES` and its six-part entity bound;
- the OASF record output key layout, collision model, and any required clean rebuild of the owned record bucket;
- every entity watch or configurable selection filter, including wildcard grammar and selected-bucket authority; and
- the directory registration watch over the owned OASF bucket, including an explicit all-keys filter declaration.

SemTeams must apply the same migration rule below: classify existing data, validate complete keys and filters before
I/O, authorize rebuilds when bytes or acceptance change, and prove invalid input has no observable I/O side effect.
Downstream owner validation is a pre-v1 release gate recorded in the
[framework package boundary clean-break inventory](27-framework-package-boundary-clean-break.md), not a reason to
retain deleted product packages in SemStreams.

`graph.EncodePredicateToken` is a reversible untagged hexadecimal graph codec. Its bytes remain unchanged. NAME and
PREDICATE hash helpers are deliberate layout-specific hashes, not reversible opaque codecs. Located
production identity-changing transforms include config's space-to-underscore replacement and loop-result's
prefix-dropping normalization; both are explicitly `nonconforming` with a `pending` migration above.

## Migration rule

An owning change may move a shared-contract status to `conforming` only after it:

1. proves the semantic input bound and literal/opaque/hash choice;
2. classifies existing callers and stored data;
3. validates the complete key or wildcard filter before I/O;
4. authorizes a clean rebuild when physical bytes or acceptance change; and
5. proves invalid input creates no write, retry, callback, watcher, lister, raw-input log, or operation metric.

After that proof, the owning change sets migration state to `not-required` when bytes and acceptance remain unchanged,
or to `complete` after an authorized migration and rebuild land. `complete` is a terminal value reserved for future
owning changes; no baseline row is complete. A row cannot become `conforming` while its migration remains `assigned`
or `pending`.

Existing wrappers, `UpdateWithRetry`'s direct Create branch, and `KeysByPrefix` filter construction remain unchanged
until those owning migrations land. There are no compatibility readers, dual writes, sanitizing fallbacks, or
deprecated shims in this baseline.
