# GitHub #963 max-ack-pending inventory

Baseline: `1002a0513a8aab3dd4d28faf4f0ee6c8d50a49ab`

Phase: `inventory-only`

Scope: no mutations, tests, Docker operations, or sister-repository access.

Body SHA-256: `436cb06b7c587c01be4105ed5a48cded4c748b88e8df404466e47bda100be691`

Hash method: `sed -n '/^## Inventory body$/,$p' <file> | tail -n +2 | shasum -a 256`

## Inventory body

# GitHub #963 JetStream `max_ack_pending` Contract Inventory

Baseline: `1002a0513a8aab3dd4d28faf4f0ee6c8d50a49ab`
Worktree: clean
Phase: `inventory-only`
Repository mutations/tests/Docker/sister access: none

## Problem statement and measured issue hypothesis

GitHub #963 is OPEN, labeled `bug`, has no comments, and was updated `2026-08-13T23:57:25Z`. Its title is:

> component: JetStreamPort max_ack_pending is accepted but not honored by most consumers

The issue body’s baseline-`421f450e` claims were remeasured independently at `1002a051`: the core `GetConsumerConfig` census is still exactly 13 consumers—one forwards the declared value, nine omit it, and three replace it with component-owned values. The broader repository census also finds port-backed consumers that bypass `GetConsumerConfig` entirely.

No canonical decision skill triggers during inventory: this is not a new communication path, payload, orchestration flow, or query access surface.

## 1. Claimed gap

The public field already exists and is fully accepted through the declaration/extraction carrier:

- `component.JetStreamPort.MaxAckPending`, JSON `max_ack_pending`: `component/port_jetstream.go:42-47`.
- Canonical port validation accepts `-1` or any greater value and rejects values below `-1`: `component/port_codec.go:332-367`.
- Canonical facts retain it: `component/port_facts.go:211-226`.
- `StreamFacts.MaxAckPending()` exposes it: `component/port_facts.go:133-134`.
- `component.ConsumerConfig.MaxAckPending` retains it: `component/port_jetstream.go:75-86`.
- `GetConsumerConfig` copies any nonzero value, including `-1`: `component/port_jetstream.go:97-153`.
- `natsclient.StreamConsumerConfig.MaxAckPending` is the runtime carrier: `natsclient/stream.go:19-49`.
- `natsclient.buildConsumerConfig` copies any nonzero value into `jetstream.ConsumerConfig`: `natsclient/stream.go:422-484`.

The gap is after extraction. In 12 of the 13 core port-backed consumer implementations, the component either omits `consumerCfg.MaxAckPending` when constructing `natsclient.StreamConsumerConfig` or replaces it with its own fixed policy. Configuration remains valid and startup can succeed, so the mismatch is silent.

The issue’s “most consumers” claim is therefore accurate, but its complete surface also includes:

- `agentic-dispatch` and the optional OTEL adapter, which resolve JetStream ports but bypass `GetConsumerConfig`;
- two E2E-only example processors with the same bypass;
- output JetStream ports, on which `max_ack_pending` is accepted even though no consumer is born.

## 2. Every current spelling and complete consumer census

### Public declaration and builder chain

| Fact/owner | Current spelling and behavior |
|---|---|
| Public port declaration | `JetStreamPort.MaxAckPending` / `max_ack_pending`: `component/port_jetstream.go:42-47` |
| Port validation | `MaxAckPending >= -1`: `component/port_codec.go:332-367` |
| Immutable facts | `StreamFacts.maxAckPending` and accessor: `component/port_facts.go:211-226`, `:133-134` |
| Extracted component carrier | `component.ConsumerConfig.MaxAckPending`: `component/port_jetstream.go:75-86` |
| Extractor | Nonzero copied; zero remains unset: `component/port_jetstream.go:121-138` |
| NATS client carrier | `natsclient.StreamConsumerConfig.MaxAckPending`: `natsclient/stream.go:19-49` |
| Final builder | Nonzero, including `-1`, reaches nats.go: `natsclient/stream.go:472-477` |
| NATS wire field | `jetstream.ConsumerConfig.MaxAckPending`: pinned nats.go `jetstream/consumer_config.go:188-192` |

### Core `GetConsumerConfig` production census

The independent search was:

```text
rg -n --glob '!openspec/changes/archive/**' --glob '!docs/proposals/**' \
  --glob '!**/*_test.go' 'GetConsumerConfig\(' --type go .
```

It found exactly 13 core component consumers.

| Consumer | Extraction and construction | Effective policy |
|---|---|---|
| `graph-ingest` | `processor/graph-ingest/component.go:1374-1391` | **Forwards** `consumerCfg.MaxAckPending`; the only core forwarding consumer. |
| `json-generic` | `processor/json_generic/json_generic.go:282-295` | **Omits**; server/default policy applies. |
| `json-filter` | `processor/json_filter/json_filter.go:305-318` | **Omits**. |
| `json-map` | `processor/json_map/json_map.go:327-340` | **Omits**. |
| `objectstore` | `storage/objectstore/component.go:762-789` | **Omits**. |
| `agentic-governance` | `processor/agentic-governance/component.go:412-425` | **Omits**. |
| `rule` | `processor/rule/processor.go:1113-1126` | **Omits**. |
| File output | `output/file/file.go:343-356` | **Omits**. |
| HTTP POST output | `output/httppost/httppost.go:348-361` | **Omits**. |
| WebSocket output | `output/websocket/websocket.go:933-946` | **Omits**. |
| `agentic-tools` | extraction at `processor/agentic-tools/component.go:254-263`; construction at `:317-332` | **Overrides** every input with fixed `3`. |
| `agentic-model` | `processor/agentic-model/component.go:316-359` | **Overrides** with fixed `1`. |
| `agentic-loop` | `processor/agentic-loop/component.go:848-910` | **Overrides**: `1` for task/response/tool-result latency class and `10` for the remaining fast/advisory inputs. |

Measured core result:

```text
forwarded: 1
omitted:   9
overridden: 3
total:    13
```

The three override owners are not equivalent:

- `agentic-loop` derives latency classes from port names and chooses `1` or `10`: `processor/agentic-loop/component.go:853-903`.
- `agentic-model` fixes the delivery ceiling at `1` while separately honoring port ack wait, heartbeat, and MaxDeliver: `processor/agentic-model/component.go:321-359`.
- `agentic-tools` fixes it at `3` while separately honoring port ack wait, heartbeat, and MaxDeliver: `processor/agentic-tools/component.go:295-332`.

### Port-backed consumers that bypass the canonical extractor

| Consumer | Evidence | Effective policy |
|---|---|---|
| `agentic-dispatch` | Ports are resolved into subscription bindings, then five direct `StreamConsumerConfig` values are built: `processor/agentic-dispatch/component.go:375-518`; declarations at `processor/agentic-dispatch/config.go:65-81`. | No `MaxAckPending`; explicit-ack consumers receive server/default policy. |
| Optional OTEL adapter | Resolves JetStream input ports at `output/otel/component.go:80-124`, then directly creates nats.go consumers at `:217-258`. | No `MaxAckPending`; server/default policy. |
| Example `iot_sensor` | Resolves port facts then builds a direct client config: `examples/processors/iot_sensor/component.go:330-369`. | No `MaxAckPending`; E2E-only example. |
| Example `document` | Same shape: `examples/processors/document/component.go:330-369`. | No `MaxAckPending`; E2E-only example. |

The examples are not core production registrations. `componentregistry.Register` owns the core registry at `componentregistry/register.go:81-237`; `cmd/e2e-semstreams` separately registers the two examples at `cmd/e2e-semstreams/main.go:745-758`.

Both production roots consume the core registry:

- `cmd/semstreams/main.go:439`
- `cmd/e2e-semstreams/main.go:631`

The OTEL adapter is intentionally optional, imported through the separate adapter root at `cmd/semstreams/main.go:25` and `cmd/e2e-semstreams/main.go:29`.

### Non-port JetStream consumers in the same runtime

These consumers use `StreamConsumerConfig` or nats.go directly and do not claim `JetStreamPort` configurability:

- Registry capability announcements: `component/registry.go:1571-1607`.
- Flow-runtime HEALTH/FLOWS/LOGS/METRICS streamers, all ack-none: `service/flow_runtime_stream.go:313-525`.
- Agent milestone subscriber: `agentic/agentrun/agentrun.go:643-677`.
- Framework MaxDeliver observer: `internal/maxdelivery/observer.go:235-260`.
- Legacy `Client.ConsumeStream`: `natsclient/client.go:1261-1308`.
- Service-internal flow-runtime consumers: `service/flow_runtime_stream.go:326-333`, `:376-383`, `:424-431`, `:495-502`.

These are adjacent runtime owners, not additional #963 port-contract consumers.

### Accepted output-only surface with no present consumer

`JetStreamPort` is legal for both input and output directions. Direction validation requires `stream_name` only for inputs and does not reject consumer-only fields on outputs: `component/port_codec.go:49-52`; `component/port_resolver.go:16-49`.

Therefore an output declaration can carry `max_ack_pending`, pass validation, survive facts projection, and never create a consumer. No production output-path reader of `StreamFacts.MaxAckPending()` exists. This is a present accepted surface with zero runtime consumer.

### Defaults and actual NATS behavior

The repository’s effective semantics are:

- Port `0` is indistinguishable from omission because the JSON field uses `omitempty`: `component/port_jetstream.go:47`.
- Extraction leaves `0` as `0`: `component/port_jetstream.go:134-138`.
- The natsclient builder leaves `0` unset: `natsclient/stream.go:472-477`.
- For explicit/all ack consumers, pinned NATS server v2.12.4:
  - first inherits a positive stream-level consumer limit, if present;
  - otherwise defaults to `1000`;
  - may lower that default under server/account limits:
    `server/consumer.go:559-560`, `:640-662`.
- `-1` passes through both SemStreams carriers and disables the positive-limit delivery gate. The server accepts `-1`; values below `-1` normalize to `-1` without pedantic mode, while SemStreams rejects them earlier: pinned server `consumer.go:582-587`.
- A positive value is the maximum outstanding unacknowledged-message count. Once reached, delivery is suspended until capacity frees: pinned nats.go `jetstream/consumer_config.go:188-192`; pinned server `consumer.go:4451-4454`.
- A positive MaxAckPending with ack-none is invalid for the applicable push-consumer path: pinned server `consumer.go:747-749`.
- Server, account, and stream consumer limits may reject a requested positive value above their allowed maximum: pinned server `consumer.go:780-787`.

Consequently, “zero means server default 1000” is only shorthand. Actual runtime may inherit or be capped by server/account/stream policy. `-1` means unlimited outstanding acknowledgements, not unlimited stream retention, local queue memory, processing concurrency, or message size.

No generic cross-component capacity measurement was found that justifies one universal nonzero default. The only checked-in measured sizing narrative is graph-ingest-specific: eight lanes, per-lane queue depth 256, and semboids-derived throughput evidence at `processor/graph-ingest/component.go:411-428` and ADR-072.

## 3. Same-class collision table and adjacent claims

### Same-class collision table: delivery admission, backpressure, and concurrency

| Dimension | Current owners and evidence |
|---|---|
| Semantic class | NATS server delivery admission is `MaxAckPending`; local memory/concurrency admission is owned separately by component dispatch queues, keyed lanes, endpoint throttles, or fetch batches. `MaxAckPending` does not itself bound all local work. |
| Owners | Public declaration/extraction: `component/port_jetstream.go:42-47,75-153`. Final carrier: `natsclient/stream.go:19-49,422-484`. Server enforcement: pinned NATS server `consumer.go:653-662,4451-4454`. Component-owned overrides: agentic-loop/model/tools cited above. Graph-ingest local queues: `processor/graph-ingest/component.go:411-428`; `processor/graph-ingest/keyed_ingest.go:71-108`. Agentic-model endpoint semaphore/rate limit: `processor/agentic-model/throttle.go:9-100`. |
| Catalogs | Canonical port-kind catalog reflects all `JetStreamPort` JSON fields into runtime `PortFields`: `component/schema_tags.go:696-752`. Checked-in JSON schema generation does not carry `PortFields`; `ports` falls through to JSON string because `mapTypeToJSONSchema` recognizes neither `ports` nor its variants: `cmd/openapi-generator/main.go:198-235,267-286`. |
| Status | NATS `ConsumerInfo` exposes `NumPending` and `NumAckPending`; SemStreams’ `OutstandingWork` sums them: `natsclient/client.go:700-742`. JetStream metrics expose only `NumPending` as `consumer_pending_messages`: `natsclient/jetstream_metrics.go:65-71,196-210`. Graph-ingest exposes queue wait, queue depth, in-flight concurrency, and redelivery drops: `processor/graph-ingest/component.go:205-266`; `pkg/dispatch/keyed_pool.go:446-505`. No SemStreams status reports declared versus effective `MaxAckPending`. |
| Lifecycle | Port changes are complete named replacements: `component/ports.go:153-206`. Declaration-affecting runtime updates require component replacement: `openspec/specs/component-runtime-config/spec.md`, “Declarations are immutable within a generation.” At component start, `ConsumeStreamWithConfig` calls `CreateOrUpdateConsumer`: `natsclient/stream.go:333-341`. The pinned NATS server supports an in-place `MaxAckPending` update: it replaces `o.maxp`, signals new messages, and resets pending-delivery allocation when lowering the value: pinned server `consumer.go:2343-2351`. Therefore SemStreams replacement/restart is its declaration lifecycle; deletion and recreation are not a pinned-server requirement for this field. |
| Ownership | Operator configuration owns the public port declaration. Nine components silently defer to server policy, three components silently substitute local policy, one forwards, and bypass consumers never consult the field. NATS owns final enforcement and external limits. No single current surface reports this ownership split. |
| Readers | Thirteen core `GetConsumerConfig` readers; only graph-ingest reads the extracted MaxAckPending into its final config. `StreamFacts.MaxAckPending()` has no other production reader beyond the shared extractor. NATS/operator tooling can inspect final `ConsumerInfo`, but SemStreams does not project the effective value. |
| Writers | External config authors write `ports.inputs[].config.max_ack_pending`. Component builders write `StreamConsumerConfig`; agentic-loop/model/tools write their own fixed values. NATS applies inherited/default/capped runtime policy. Output declarations may write the field with no consumer writer at birth. |
| Recovery | Durable consumer state and pending acknowledgements remain server-owned across process restart; component restart recreates/updates the consumer config. Graph-ingest drains its bounded local lanes on graceful stop: `processor/graph-ingest/keyed_ingest.go:71-108`; `pkg/dispatch/keyed_pool.go:239-399`. No reconciliation record tells an operator whether restart applied, ignored, or replaced the declaration. |

### Current specs, ADRs, docs, and issue overlaps

- `component-runtime-config` requires every JetStream field, including maximum pending acknowledgements, to survive canonical decode, merge, runtime view, and facts projection. It does not require a consuming component to apply the extracted value: `openspec/specs/component-runtime-config/spec.md`, “JetStream fields survive canonical round-trip.”
- `graph-ingest` is the only current capability spec that normatively binds `max_ack_pending` to runtime behavior: `openspec/specs/graph-ingest/spec.md:162-179,204-205`.
- `keyed-dispatch` separately owns bounded in-process per-lane queues and blocking submit behavior; it is not the server delivery ceiling: `openspec/specs/keyed-dispatch/spec.md:5-44`.
- ADR-072 records that `max_ack_pending` became operator-settable “for every JetStream input port”: `docs/adr/072-keyed-concurrent-entity-ingest.md:198-201`. Its earlier context still contains historical “no config path” wording at `:172-180`, despite the same ADR recording that the plumbing shipped. That history is internally stale but not current runtime authority.
- The timeout-chain runbook presents `max_ack_pending` as a per-port knob and says the per-port surface generalizes to any JetStream consumer: `docs/operations/14-timeout-chain.md:10-21,92-109,154-181`. Current production behavior does not satisfy that general statement.
- The agentic JetStream tuning guide’s “Current State” says all three agentic components omit `MaxAckPending` and therefore receive 1000: `docs/advanced/11-jetstream-tuning.md:22-48`. That is false at this baseline. Agentic-loop assigns 1 to `agent.task`, `agent.response`, and `tool.result`, and 10 to its fast/advisory input: `processor/agentic-loop/component.go:853-903`; agentic-model assigns 1: `processor/agentic-model/component.go:342-359`; agentic-tools assigns 3: `processor/agentic-tools/component.go:317-332`. The guide also describes 1000 outstanding acknowledgements as 1000 concurrent LLM requests; current ownership separates the server delivery ceiling from component-local execution concurrency.
- The same guide’s recommended component policies name `agentic-loop=1`, `agentic-model=1`, and `agentic-tools=3`: `docs/advanced/11-jetstream-tuning.md:170-239`. Those values coincide with the current fixed policies for the three long-running agentic paths and are evidence of historical policy intent, but the text is recommendation/runbook material rather than a current capability contract. It does not describe agentic-loop’s fast/advisory value of 10.
- The tuning guide says changing `MaxAckPending` requires deleting and recreating a durable consumer: `docs/advanced/11-jetstream-tuning.md:395-397`. That statement contradicts the pinned server update path, which applies the change in place: pinned server `consumer.go:2343-2351`. SemStreams itself invokes `CreateOrUpdateConsumer`: `natsclient/stream.go:333-341`.
- ADR-046 says fan-out concurrency is determined by downstream `MaxAckPending` and tells operators who need a cap to set it on the JetStream consumer: `docs/adr/046-parallel-fan-out-and-gated-dag-dispatch.md:127-130`. The worked example repeats that operators with rate-limited LLM endpoints should set `MaxAckPending` on `agent.task.*`: `configs/rules/example-fan-out/README.md:95-98`. The current agentic-loop consumes `agent.task` with fixed `MaxAckPending=1` after extracting but ignoring the port value: `processor/agentic-loop/component.go:846-870,895-903`. Thus the documented operator instruction has no effective public-port path for a different `agent.task` value.
- The orchestration concept document calls `MaxAckPending=1` on `agent.task` consumers the substrate’s serial-per-consumer commitment: `docs/concepts/14-orchestration-layers.md:87-93`. That claim matches current agentic-loop code and supplies further evidence that 1 is intentional for this path, while conflicting with ADR-046 and the fan-out example’s presentation of the same value as operator-controlled.
- The streams-versus-KV concept accurately lists `MaxAckPending` as a NATS JetStream backpressure primitive, but twice links readers to the stale tuning guide: `docs/concepts/03-streams-vs-kv-watches.md:127-131,268-275`. At the SemStreams surface, that generic tuning claim is not equivalent to saying every port-backed component honors `JetStreamPort.max_ack_pending`.
- Runtime `ConfigSchema` reflection includes the field because it reflects `JetStreamPort`: `component/schema_tags.go:696-752`. Checked-in generated schemas contain no `max_ack_pending`; for example `schemas/graph-ingest.v1.json:25-29` reduces `ports` to a string.
- GitHub #309 is a separate deferred issue, “component: expose backpressure metrics for flow ports,” classified as a new metrics surface requiring consumer and operational evidence: `docs/proposals/post-g-foundation-remap-issue-census.tsv:18`.
- GitHub #586 is closed. Its remaining historical ask concerned core-NATS watcher/subscription pending-limit configuration, not JetStream consumer `MaxAckPending`. Repository documents that still call #586 open are stale snapshots; the split is described at `docs/proposals/gh954-slow-consumer-e2e-inventory.md:140-152`.
- #950/#954 cover core-NATS slow-consumer attribution and assembled proof, not JetStream admission policy: `openspec/specs/nats-client-diagnostics/spec.md`.
- #480/#488 and ADR-072 are the origin of the graph-ingest-specific plumbing and measurement; #963 exposes the broader shared-component contract gap.
- The only active OpenSpec change is `semantic-tier-split`; searching active changes for `max_ack_pending`, `MaxAckPending`, `backpressure`, and `pending-limit` returned zero matches.

## 4. Consumer at birth

No new symbol is proposed in this inventory. Present consumers of the existing public field are:

- operator/config author setting a JetStream input port;
- external component author using `component.GetConsumerConfig`;
- graph-ingest, the sole current component that carries the extracted value to NATS;
- runtime discovery clients receiving reflected `PortFields`;
- NATS, which owns the final effective consumer policy.

Present no-consumer surfaces are:

- `max_ack_pending` on any JetStream output declaration;
- checked-in generated component schemas, which expose no kind-specific field;
- an effective-policy status/readiness/health report—none exists;
- representative non-graph-ingest consumer tests proving declared nonzero or `-1` behavior—none exist.

### Test census

Current tests prove the two carrier halves independently:

- Facts projection and round-trip: `component/port_facts_projection_test.go:7-56`; `component/port_resolver_test.go:129-163`; `component/port_codec_test.go:209-224`.
- Invalid `< -1` rejection: `component/port_resolver_test.go:76-99`.
- Port-to-`ConsumerConfig` extraction for positive, zero, and `-1`: `component/port_jetstream_maxackpending_test.go:5-32`.
- `StreamConsumerConfig` to nats.go final-hop forwarding for positive, zero, and `-1`: `natsclient/max_ack_pending_test.go:5-25`.

No test names a representative production component consumer and proves the complete declared-port-to-effective-consumer path.

## Adopter seam inventory

### Specific adopter 1: operator configuring an assembled SemStreams component

1. **What must they know?**
   - `0`/omission delegates to NATS policy, usually 1000 for explicit/all ack but possibly inherited or capped.
   - `-1` means unlimited outstanding acknowledgements.
   - Whether the selected component forwards, omits, overrides, or bypasses the declaration.
   - Agentic-loop fixes long-running inputs at 1 and its fast/advisory input at 10; agentic-model fixes 1; agentic-tools fixes 3.
   - Component-local queues/concurrency and server `MaxAckPending` are separate limits.
   - A named port override completely replaces the default declaration.
   - Current operator documents disagree about whether `agent.task` admission is an operator setting or a fixed substrate commitment.
   - The pinned server can update `MaxAckPending` in place even though the tuning guide says deletion/recreation is required.

2. **What happens if they do nothing?**
   - Graph-ingest and the nine omission consumers reach the server with zero and receive server/default policy.
   - Agentic-loop/model/tools receive their fixed component policies.
   - Dispatch, OTEL, and example bypass consumers receive server/default policy.
   - Configuration succeeds; there is no warning that an explicit declaration was ignored or replaced.

3. **What happens if they follow the current operator guidance?**
   - Setting `max_ack_pending` on the `agent.task` port is accepted, but agentic-loop replaces it with 1. A requested value of 1 appears successful by coincidence; any other requested value is silently ineffective.
   - Setting the field on agentic-model or agentic-tools is likewise accepted and replaced with 1 or 3.
   - A direct NATS update can take effect, but a later component start calls `CreateOrUpdateConsumer` with the component-owned fixed value.
   - Following the tuning guide’s delete/recreate instruction can cause an unnecessary consumer lifecycle interruption because the pinned server supports an in-place update.

4. **Where do they find out?**
   - Declaration validity: boot-time typed validation.
   - Generic NATS semantics: Go comments and current runbooks.
   - Fixed agentic values: component source; partially and inconsistently in the tuning, orchestration, ADR, and example documents cited above.
   - Actual effective value: NATS consumer inspection or source reading.
   - Omit/override behavior: nowhere in generated schemas, runtime status, health, or readiness.

5. **What should they have to know?**
   - Ideally only the operational intent relevant to their component. They should not need a source-code census, reconcile contradictory documents, distinguish a server update capability from SemStreams lifecycle behavior, or predict which builder copied or replaced a shared field. The current gap is the distance between a universally accepted declaration and component-specific silent behavior.

### Specific adopter 2: external developer authoring a SemStreams component

1. **What must they know?**
   - Calling `GetConsumerConfig` does not bind the returned values to NATS.
   - Every desired field must be copied manually into `StreamConsumerConfig`.
   - Omitting one field compiles and silently delegates to server policy.
   - If the component intentionally owns admission, that ownership is not expressed by the port type.

2. **What happens if they do nothing?**
   - Their consumer starts and processes messages with server/default MaxAckPending.
   - A user-supplied nonzero or `-1` declaration may be accepted and then discarded.
   - No typed error distinguishes “unsupported” from “forgot to forward.”

3. **Where do they find out?**
   - The extractor and builder comments document each carrier.
   - Tests prove each carrier separately.
   - No compile-time, boot-time, or typed runtime guard proves the author connected them.
   - The linked JetStream tuning guide demonstrates direct `StreamConsumerConfig` construction and fixed component values, but does not state the public-port honor/override contract; it therefore does not close the author’s manual-copy debt.

4. **What should they have to know?**
   - They should not have to remember a parallel field-copy checklist or predict NATS’ final inherited limit. The framework currently validates declaration shape but does not observe or report whether the consuming component honored it.

The surface currently asks both adopters to predict a fact the framework/NATS can observe: the effective consumer configuration after `CreateOrUpdateConsumer`.

## Exact searches closing empty surfaces

```text
rg -n --hidden --glob '!.git/**' \
  --glob '!docs/proposals/gh963-max-ack-pending-inventory.md' \
  '(MaxAckPending|max_ack_pending|maxAckPending|max-ack-pending|MAX_ACK_PENDING|max ack pending)' .
# Repository-wide spelling census. In addition to the declaration/carrier/builders,
# fixed component policies, tests, specs, ADR-072, and archived proposals already
# inventoried, this exposed the current tuning guide, ADR-046, fan-out example,
# orchestration concept, and streams-versus-KV links classified above.
# It found no additional production forwarding component.

rg -n --hidden --glob '!.git/**' --glob '!docs/**' --glob '!openspec/**' \
  '(max-ack-pending|MAX_ACK_PENDING)' .
# 0 matches: no CLI-flag or environment-variable spelling exists.

rg -n '(MaxAckPending|max_ack_pending|maxAckPending)' \
  configs docker test cmd service schemas specs \
  --glob '!**/*.md' --glob '!**/*_test.go'
# 0 matches: no shipped machine-readable configuration, CLI/service wiring,
# or checked-in generated schema sets or exposes the field. The excluded Markdown
# is material: configs/rules/example-fan-out/README.md contains the contradictory
# operator instruction inventoried above.

rg -n 'MaxAckPending\(\)' --glob '!**/*_test.go' \
  --glob '!openspec/**' --glob '!docs/**' .
# Only component.GetConsumerConfig and the accessor definition.

rg -n 'max_ack_pending' configs docker test cmd service schemas specs \
  --glob '!**/*_test.go'
# 0 matches: no shipped configuration or checked-in generated schema sets/exposes it.

rg -n '(effective|actual|resolved).*(max_ack_pending|MaxAckPending)|\
(max_ack_pending|MaxAckPending).*(effective|actual|resolved|status|health|readiness)' \
  --glob '!openspec/changes/archive/**' --glob '!docs/proposals/**' .
# 0 matches: no effective-policy report or status surface.

rg -n 'MaxAckPending|max_ack_pending' --glob '**/*_test.go' \
  --glob '!openspec/**' .
# Only facts/codec/extractor validation and natsclient final-builder tests;
# no representative component consumer path.

find openspec/changes -mindepth 1 -maxdepth 1 -type d -not -name archive
# openspec/changes/semantic-tier-split

rg -n --glob '!openspec/changes/archive/**' \
  '(max_ack_pending|MaxAckPending|backpressure|pending-limit)' openspec/changes
# 0 matches.
```

## Open evidence questions for later phases

- The tuning guide’s recommended values and the orchestration concept’s “substrate commitment” language are evidence that the fixed agentic policies were intentional. No current capability spec states whether those fixed values are binding component-owned admission contracts, defaults, or stale historical policy.
- ADR-046 and the fan-out example present downstream `agent.task` `MaxAckPending` as operator-controlled, while current code and the orchestration concept fix it at 1. Which text expresses current contract truth requires an owner ruling.
- The tuning guide presents delete/recreate as a NATS requirement, while the pinned server and SemStreams client use an update path. Whether any separate SemStreams lifecycle constraint justifies that operator instruction is not stated.
- The streams-versus-KV concept routes adopters to the stale tuning guide from both its processing-time discussion and related links, expanding the affected adopter surface beyond the tuning guide itself.
- No measured capacity evidence covers the nine omission consumers, dispatch, or OTEL; repository evidence supports only graph-ingest-specific sizing.
- The runtime discovery `PortFields` representation contains `max_ack_pending`, while checked-in generated schemas discard all port variants. Which adopter surface is authoritative is not stated.
- NATS exposes the effective value through consumer information, but SemStreams does not currently bind that observation to declared-port truth.
- Output-port acceptance of consumer-only fields has no present runtime consumer and no current spec disposition.

## Handoff

Technical writer: materialize this inventory verbatim as a complete line-addressable artifact, record baseline
`1002a0513a8aab3dd4d28faf4f0ee6c8d50a49ab` and the artifact content hash, and preserve the inventory-only status.

Independent SemStreams inventory review should verify:

- the `1 forward / 9 omit / 3 override` core census;
- the dispatch, OTEL, and two example bypass rows;
- output-port acceptance with no consumer;
- runtime reflected-schema versus checked-in generated-schema split;
- actual NATS zero, inherited/capped default, positive, and `-1` semantics;
- separation from #309 and closed #586;
- the tuning guide’s false unset/default claim, historical fixed-policy evidence, and false delete/recreate claim;
- the ADR-046/fan-out operator instruction versus agentic-loop’s fixed `agent.task=1`;
- the orchestration concept’s substrate-commitment claim and the stale links from the streams-versus-KV concept;
- adopter consequences for an accepted-but-overridden port value and for following the delete/recreate instruction;
- both adopter seams and the exact empty searches.

Stop for `INVENTORY PASS`. No target state, options, recommendation, artifact delta, or implementation tasks are included.
