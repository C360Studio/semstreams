# Foundation B execution design

Inventory baseline: `61022ae1b4da0309e93ce49ec00c9c64679d09d8`; accepted artifact `docs/proposals/foundation-b-port-language-inventory.md`, 955 lines, 53,247 bytes, SHA256 `d957dfd00a2ca9bbf3ee3cf4aa2d0d9005008eb78198c7762403aa2c66ba9000`. `INVENTORY PASS` had no findings.

## Options and costs

| Option | Outcome | Cost |
|---|---|---|
| Do nothing | No migration | Preserves silent NATS fallback, aliases, dead declarations, asymmetric round trips, unknown capabilities, omitted management facts, and #859 drift |
| Execute roadmap §8 literally | Canonical grammar and clean wire break | Repeats disproven premises: two dead graph-query reads become false declarations; message-logger is not the sole raw-config owner; dead `KVWrite` lane survives; AGENT_LOOPS reader-side provisioning contradicts `kv-read` |
| Corrected Foundation B | Canonical grammar plus evidence-backed declaration and consumer migration | Breaking migration across 124 executable Go declarations, 522 JSON rows, 93 `Type` reads, 16 assertions, and 76 renderers |
| Combine Foundations B and C | Removes `Discoverable` and raw message-logger interpretation immediately | Excessive review/E2E blast radius; mixes grammar correctness with snapshot lifecycle and atomic registry replacement |

Recommendation: corrected Foundation B. Keep Foundation C’s `Discoverable`/snapshot cutover separate.

## Binding design

Canonical exported kinds are:

`timer`, `network`, `file`, `http-client`, `nats`, `nats-request`, `jetstream`, `kv-watch`, `kv-read`, `kv-write`, `store-read`, `store-provide`.

`kv-watch`, `kv-read`, and `kv-write` remain distinct `PortKind` values, but all three normalize both resource ID and
connection identity to the common `kv:<bucket>` spelling.

`Portable.Type() string` becomes `Kind() PortKind`. One closed binding table owns kind factory, allowed directions, strict decoding, validation, normalization, resource identity, exclusivity, interface, interaction pattern, connection identifiers, NATS subjects, and stream facts. No custom-kind registration exists.

`PortDefinition` retains `name`, `required`, `description`, and typed `Config Portable`. Definition and runtime `Port` use one wire:

```json
{
  "name": "graph_mutations",
  "required": true,
  "config": {
    "kind": "nats-request",
    "subject": "graph.mutation.>",
    "timeout": "1s",
    "interface": {"type": "semstreams.graph.mutation", "version": "v1"}
  }
}
```

Delete flat `type`, `subject`, `interface`, `timeout`, `stream_name`, `bucket`, `Config any`, and runtime `{"type","data"}`. Delete aliases `kv`, `kvwatch`, `kvwrite`, `http`, `grpc`, and `websocket-server`. Protocol is `network.protocol`. Unknown kind/field, wrong direction, duplicate name, malformed duration/port, and missing required data fail with component/port/kind/field context before initialization. Only network host `0.0.0.0` and request timeout `1s` default.

An immutable normalized facts projection is produced by the unexported resolver and consumed by Registry, flowgraph, ComponentManager, schema generation, and stream provisioning. No consumer reclassifies concrete types. The existing merge surface becomes the strict public façade over the unexported resolver for the current `InputPorts`/`OutputPorts` era; it performs complete replacement, rejects duplicate/unknown names and kind/direction changes, and returns errors. It is one behavior, not a compatibility path.

## Stop-risk rulings

1. Delete the two graph-query `ENTITY_STATES` `kv_read` rows. Graph-query declares no `KVReadPort`; its real KV dependencies remain community watches.
2. Real KVRead consumers at birth are exactly:
   - graph-clustering: `ENTITY_STATES`, `OUTGOING_INDEX`, `INCOMING_INDEX`;
   - agentic-tools: `ENTITY_STATES`, `AGENT_LOOPS`.
3. Agentic-tools registers AGENT_LOOPS tools unconditionally but binds must-exist lazily per execution. Remove reader-side `CreateKeyValueBucket`; agentic-loop remains provisioning/writing owner.
4. Migrate seven agentic-tools `ENTITY_STATES` rows into ordinary inputs.
5. Delete `PortConfig.KVWrite` in B, not C: it has zero runtime consumers. Move all 16 shipped rows and the agentic-loop default into ordinary outputs.
6. Delete dead `NATSStreamPortConfig` and `NATSRequestPortConfig`.
7. Network, file, store-read, and store-provide receive strict decode/resolve/runtime round trips. Store-provide remains nonexclusive; StoreRegistry retains duplicate-owner authority.
8. Preserve every JetStream field, including subjects, storage, retention, size, replicas, consumer settings, `MaxAckPending`, and interface.
9. Delete all 93 flat `PortDefinition.Type` interpretations. Replace the 16 projection assertions with normalized facts. All 76 hand-rolled renderers delegate grammar work to strict merge/resolve; component-local optional-port selection may remain but cannot classify kinds.
10. Two raw-config owner families remain visible:
    - stream provisioning consumes the canonical decoder/facts for explicit JetStream outputs;
    - message-logger temporarily projects canonical NATS/JetStream subjects from raw config until Foundation C.
    Neither owns another grammar.
11. Suspended `RequestReply bool` is rejected; `nats-request` exclusively owns request/reply classification.
12. No custom-kind escape hatch.

## Implementation checkpoints

All checkpoints live on one breaking branch; none is independently mergeable or releasable.

1. **Grammar and codec:** failing table tests, then kinds, typed configs, strict common codec, resolver/facts, strict merge, field-complete JetStream and store round trips.
2. **Owned migration:** migrate all 124 executable constructions and account for all 522 JSON rows; migrate/delete KV lanes and aliases; add five truthful KVRead declarations; change AGENT_LOOPS acquisition.
3. **Shared consumers:** move flowgraph, Registry capabilities/conflicts, ComponentManager reporting, schema generation, and stream provisioning to normalized facts; retain both named raw-config owners only.
4. **Renderer/runtime sweep:** migrate all 76 methods; prove zero flat `Type` reads, zero old aliases/fields/dead types, zero projection type switches, zero top-level `kv_read`/`kv_write`.
5. **Release gate:** schema regeneration, contract tests, race/integration tests, and E2E.

Focused tests cover every kind/direction, JSON round trip, unknown fields/kinds, duplicates, required data, resource identity, flow classification, capability/manager parity, store federation, mutation interface preservation, stream derivation, graph-query row absence, five KVRead declarations, AGENT_LOOPS late-owner recovery, and proof that reads never create.

Required gates:

- `task lint`
- `go test -race ./...`
- `task test:integration`
- `task schema:generate` with clean schema/spec diff
- `go test ./test/contract/...`
- `task e2e:agentic`
- `task e2e:semantic`
- `task e2e:all`
- `task e2e:research-graph`

## Spec/task deltas

`component-runtime-config`: replace flat/builder precedence with strict common envelope, complete field preservation, typed boot failures, and canonical merge.

Component discovery: require canonical kinds, one resolver/facts projection, direction validation, and identical Registry/flowgraph/manager views.

`stream-provisioning`: derive declarations from canonical normalized JetStream facts, never flat fields.

`framework-composition` and `graph-ingest`: preserve canonical `nats-request` mutation interface/family and exactly-one-provider validation.

No graph-query semantic spec change; only false configuration is removed. No ADR is needed: this is contract mechanics under ADR-063, ADR-075, ADR-090, and ADR-091.

Task truth must enumerate the five checkpoints and their exact searches/tests; no shim, deprecated API, dual decoder, alias, or migration window may appear.

## Adopter seam and skills

An external component author must know only direction, canonical kind, and semantic resource data. Old Go code fails compilation; old JSON fails typed boot validation—never silent fallback. The binding matrix and generated schema are authoritative. No caller predicts subjects, buckets, readiness, or provisioning policy.

No decision skill triggers: no new KV/Stream communication path, orchestration, payload type, or query access surface is introduced.

Owner acceptance is required for the corrected roadmap rulings: no graph-query KVRead, lazy must-exist AGENT_LOOPS, early `KVWrite` lane deletion, two raw-config owners, and strict merge/resolve while `Discoverable` remains until Foundation C.

Hard stop after Foundation B: re-inventory the merged tree and stop if any alias, flat discriminator, top-level side lane, dead type, independent shared projection, false KV declaration, or undeclared runtime-policy dependency remains. Foundation C begins only after a new accepted inventory and owner remap.
