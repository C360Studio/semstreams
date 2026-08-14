# GitHub #963 max-ack-pending design

Baseline: `1002a0513a8aab3dd4d28faf4f0ee6c8d50a49ab`

Status: `owner accepted; implementation complete and pending SemStreams reviewer sign-off`

Accepted inventory dependency body SHA-256: `436cb06b7c587c01be4105ed5a48cded4c748b88e8df404466e47bda100be691`

Design body SHA-256: `4360372a0a1e4381d61373624f03ebea987dcfd853b2426628ab9090fb424cf2`

Hash method: `sed -n '/^## Design body$/,$p' <file> | tail -n +2 | shasum -a 256`

## Design body

## Accepted inventory body
<!-- accepted-inventory-begin -->

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
<!-- accepted-inventory-end -->

## Architect design handoff

### Owner ruling — output literal zero

On 2026-08-14, the owner selected Option A for JetStream output `max_ack_pending`. The generated output schema SHALL
contain an optional integer property constrained by `const: 0`. Omission and literal zero both represent semantic
absence. Positive values and `-1` remain invalid because an output declaration creates no consumer.

Runtime discovery and editor metadata SHALL remain input-only and SHALL NOT advertise output tuning. This ruling is a
correction to the generated representation of semantic absence, not a new output tuning capability. It supersedes the
earlier draft statements that generated output variants omit `max_ack_pending`.

The owner also explicitly approved the narrow exported `PortFieldInfo.ZeroIsOmitted() bool` predicate. The canonical
binding owns a private `zeroIsOmitted` marker; discovery JSON does not serialize that marker; the read-only predicate is
consumed only by checked-in schema generation and its independent contract projection. Runtime validation reads the
private binding-owned marker directly. Editors continue to learn only the input direction and minimum from discovery
metadata. All other accepted rulings remain binding, including the absence of output consumer-policy observation.

### Option A ruling conformance

| Binding clause | Implementation evidence |
|---|---|
| Omission and literal zero are semantic absence at runtime | `component/port_codec.go:53-58`; `component/port_resolver.go:68-80`; `component/port_resolver_test.go:145-176` |
| Generated output schema is optional `const: 0` | `cmd/openapi-generator/main.go:327-337`; `cmd/openapi-generator/openapi_generator_test.go:68-107`; `schemas/graph-ingest.v1.json:575-578` |
| Positive and `-1` output values are invalid in runtime and generated schema | `component/port_resolver_test.go:145-176`; `cmd/openapi-generator/openapi_generator_test.go:78-107`; `test/contract/schema_contract_test.go:159-199` |
| Discovery/editor metadata remains input-only | `component/port_codec.go:53-58`; `component/schema_tags.go:735-746`; `component/schema_tags_test.go:582-593` |
| The marker is private and absent from discovery JSON | `component/schema_tags.go:86-106`; `component/schema_tags_test.go:587-593` |
| The approved predicate is read-only and limited to schema projections | `component/schema_tags.go:102-106`; `cmd/openapi-generator/main.go:327-337`; `test/contract/schema_contract_test.go:625-635` |
| The correction is not output tuning and creates no policy observation | `openspec/changes/honor-jetstream-max-ack-pending/specs/component-runtime-config/spec.md:23-46`; `component/port_resolver_test.go:145-176` |

## Decision-skill outcome

No canonical decision skill triggers. The change adds no communication path, orchestration behavior, payload kind, or query access.

## Measurable design premises

1. `max_ack_pending` is already a public, validated JetStream port field and survives facts/extraction:
   `component/port_jetstream.go:42-47,75-153`; `component/port_codec.go:332-367`;
   `component/port_facts.go:133-134,211-226`.
2. The final existing runtime carrier already forwards positive values and `-1`:
   `natsclient/stream.go:19-49,472-477`.
3. The complete core census is `1 honor / 9 omit / 3 override`; four additional port-backed implementations bypass the
   extractor. The accepted inventory names every site.
4. The three agentic fixed policies have intentionality evidence:
   `agentic-loop` 1/10, `agentic-model` 1, `agentic-tools` 3 in code, with matching long-running recommendations at
   `docs/advanced/11-jetstream-tuning.md:170-239` and the agent-task substrate commitment at
   `docs/concepts/14-orchestration-layers.md:87-93`.
5. No repository-wide capacity measurement justifies one cross-component positive default. The only measured sizing
   evidence is graph-ingest-specific.
6. Zero is omission/server policy, not necessarily 1000: the pinned server may inherit or lower the default under
   stream/server/account policy. Positive values above a limit may be rejected. `-1` disables the positive outstanding-ack
   gate: pinned server `consumer.go:559-560,582-587,640-662,780-787,4451-4454`.
7. `CreateOrUpdateConsumer` is the current creation path and the pinned server updates `MaxAckPending` in place:
   `natsclient/stream.go:333-341`; pinned server `consumer.go:2343-2351`.
8. NATS already exposes the resulting configuration through `ConsumerInfo`; SemStreams can observe it after creation
   instead of asking the operator to predict it.
9. Runtime `PortFields` carries the field while generated JSON schemas discard the whole port variant:
   `component/schema_tags.go:696-752`; `cmd/openapi-generator/main.go:198-235,267-286`.
10. No shipped machine-readable config currently sets `max_ack_pending`; new rejection behavior nevertheless remains
    breaking for external configurations that previously supplied it on an output or component-owned input.

## Options considered

### Option 1: Do nothing

Keep the accepted-but-silent contract.

Cost:

- Nine core consumers and four bypasses continue discarding declarations.
- Component-owned policies remain indistinguishable from accidental omissions.
- Output ports retain a consumer-only phantom field.
- Operators must inspect source and reconcile contradictory documents.
- No effective-policy observation closes the declaration/runtime gap.

### Option 2: Make every input consumer honor the port declaration

Copy the existing carrier into all consumers, including agentic-loop/model/tools.

Cost:

- Smallest conceptual rule.
- Removes the fixed agentic serial/admission guarantees despite code, guide, and orchestration evidence that they are
  intentional.
- Makes agent-task fan-out tuning capable of violating the documented serial-per-consumer substrate commitment.
- Still needs output rejection, effective observation, schema correction, and documentation correction.

### Option 3: Preserve every current omission as component policy

Reject nonzero declarations on every currently omitting or bypassing consumer and document their server/default policy.

Cost:

- Makes accidental omissions permanent API policy without evidence.
- Leaves most of the public field unusable.
- Forces adopters to memorize a large component allowlist.
- Does not extend the existing carriers to their intended consumers.

### Option 4: Explicit two-policy contract using the existing carriers — recommended

- Ordinary port-backed JetStream inputs honor the existing declaration.
- The three evidenced agentic owners retain fixed policies and reject any nonzero declaration instead of silently
  replacing it.
- JetStream outputs reject nonzero `max_ack_pending`.
- Every create/update observes the actual NATS configuration before delivery starts.
- No generic positive default is introduced.
- Generated schemas truthfully expose the canonical port model.
- No parallel policy declaration is added. The exported consumption signatures intentionally break; the constraint,
  registry, direct-observation, and classification surfaces remain owner-gated.

Cost:

- Touches every inventoried consumer once.
- Previously accepted external configs can begin failing on component-owned inputs and outputs.
- The schema-generator repair regenerates every schema containing `ports`; it cannot be hidden as a graph-ingest-only
  schema edit.
- Effective-policy observation adds exactly three configuration gauges, while broader queue/drop metrics remain #309.

## Recommended contract

### Value semantics

| Declaration | Honor-policy input | Component-owned input | JetStream output |
|---|---|---|---|
| Omitted or `0` | Send zero; observe inherited/default/capped server result | Apply and observe the fixed component value | Valid because no consumer request is expressed |
| Positive | Forward exactly; server acceptance is authoritative | Reject before creating a consumer | Reject during canonical direction validation |
| `-1` | Forward exactly as unlimited outstanding acknowledgements | Reject before creating a consumer | Reject during canonical direction validation |
| `< -1` | Existing canonical rejection | Existing canonical rejection | Existing canonical rejection |

Zero remains indistinguishable from omission. No generic nonzero default is added.

### Complete consumer disposition

| Surface | Binding disposition |
|---|---|
| `graph-ingest` | Honor; retain existing forwarding |
| `json-generic` | Honor |
| `json-filter` | Honor |
| `json-map` | Honor |
| `objectstore` | Honor |
| `agentic-governance` | Honor |
| `rule` | Honor |
| File output component’s JetStream input | Honor |
| HTTP POST output component’s JetStream input | Honor |
| WebSocket output component’s JetStream input | Honor |
| `agentic-tools` | Component-owned 3; reject every nonzero declaration |
| `agentic-model` | Component-owned 1; reject every nonzero declaration |
| `agentic-loop` `agent.task`, `agent.response`, `tool.result` | Component-owned 1; reject every nonzero declaration |
| `agentic-loop` fast/advisory input | Component-owned 10; reject every nonzero declaration |
| `agentic-dispatch` five inputs | Honor through `GetConsumerConfig` |
| Optional OTEL inputs | Honor through `GetConsumerConfig`, then set the direct nats.go config |
| E2E example `iot_sensor` inputs | Honor through `GetConsumerConfig` |
| E2E example `document` inputs | Honor through `GetConsumerConfig` |
| Any JetStream output declaration | Reject nonzero or `-1`; no consumer exists |
| Non-port JetStream consumers inventoried separately | Not governed; they make no `JetStreamPort` configurability claim |

A declaration equal to a fixed component value is still rejected. Accepting it would falsely imply operator ownership
and make future policy changes appear to violate a working knob.

### Component-owned rejection

Each of the three agentic components checks the extracted value before creating any consumer. A nonzero value returns a
typed invalid-config error naming component, port, `max_ack_pending`, and the fixed effective policy. No consumer is
created and no fallback silently proceeds.

This keeps the current 1/10/1/3 behavior binding while resolving the accepted-but-ignored field.

### Complete exported consumption API disposition

All exported natsclient entry points that create port-backed consumers require the same bounded
`PortConsumerContext`. None accepts an optional observation binding, and none carries duplicate stream, consumer, or
requested-policy identity.

```go
type PortConsumerContext struct {
    Component      string
    Port           string
    ComponentOwned bool
}

func (c *Client) ConsumeStreamWithConfig(
    ctx context.Context,
    owner PortConsumerContext,
    cfg StreamConsumerConfig,
    handler func(context.Context, jetstream.Msg),
) error

func (c *Client) ConsumeStreamWithConfigContexts(
    setupCtx context.Context,
    handlerCtx context.Context,
    owner PortConsumerContext,
    cfg StreamConsumerConfig,
    handler func(context.Context, jetstream.Msg),
) error

func (c *Client) ConsumeDurable(
    ctx context.Context,
    owner PortConsumerContext,
    cfg StreamConsumerConfig,
    heartbeat time.Duration,
    handler func(context.Context, []byte) error,
) error

func (c *Client) ConsumeInternalStreamWithConfig(
    ctx context.Context,
    cfg StreamConsumerConfig,
    handler func(context.Context, jetstream.Msg),
) error
```

Disposition:

- `ConsumeStreamWithConfig` is the ordinary port-backed entry point. It delegates to
  `ConsumeStreamWithConfigContexts` with the same setup and handler context.
- `ConsumeStreamWithConfigContexts` is the authoritative port-backed implementation for split setup/handler lifetimes.
  It validates owner context, creates/updates, observes policy, installs private tracking, and only then begins delivery.
- `ConsumeDurable` remains the framework-owned heartbeat/ack wrapper but now requires owner context and delegates to the
  required port-backed `ConsumeStreamWithConfig`. Its heartbeat validation remains unchanged.
- `ConsumeInternalStreamWithConfig` is the sole exported unobserved entry point and is explicitly restricted to the
  inventoried non-port consumers that make no `JetStreamPort` contract claim.
- The implementation core that separates setup and handler contexts is unexported. There is no exported
  `ConsumeInternalStreamWithConfigContexts`: no present non-port consumer needs split contexts.
- There is no `ConsumeInternalDurable`: no present in-repo production consumer exists. Adding one would require a new
  consumer-at-birth inventory and owner review.
- The old signatures of all three existing exported methods are removed without aliases. Compatibility overloads would
  preserve the silent bypass.

All authoritative consumer facts remain derived:

- requested value from final `StreamConsumerConfig.MaxAckPending`;
- canonical stream/consumer/effective value from the created consumer’s `ConsumerInfo`;
- cleanup key retained only inside private natsclient bookkeeping.

`PortConsumerContext` carries only bounded component/port ownership context.

### Legacy `Client.ConsumeStream` disposition

Remove:

```go
func (c *Client) ConsumeStream(
    ctx context.Context,
    streamName string,
    subject string,
    handler func(jetstream.Msg),
) error
```

The method has no production caller in this repository. It directly calls `CreateOrUpdateConsumer`, ignores initial
`Info` failure, starts delivery without requested/effective-policy classification, and advertises an ambiguous general
consumption path beside the new explicit port-backed and non-port operations.

It is not converted into a context-requiring port method because it accepts no final `StreamConsumerConfig` and
therefore has no authoritative requested `MaxAckPending` carrier. Adding policy context would still leave callers
reconstructing configuration beside the real consumer config.

It is not renamed to `ConsumeInternalStream`. There is no present production consumer for a second non-port convenience
surface. Non-port callers use `ConsumeInternalStreamWithConfig(ctx, cfg, handler)`; port-backed callers use the required
observed `ConsumeStreamWithConfig(ctx, owner, cfg, handler)`.

No compatibility alias, deprecated wrapper, variadic overload, or hidden direct-create implementation remains.

### Exported consumer API caller census

#### `ConsumeStreamWithConfigContexts`

Production callers:

- `natsclient/stream.go:270-286` — ordinary wrapper delegation;
- `processor/agentic-loop/component.go:934` — the sole direct production caller, required because setup and consumer
  callback lifetimes differ.

Tests:

- `natsclient/stream_integration_test.go:42`.

Disposition: wrapper and agentic-loop both pass required `PortConsumerContext`; the integration test proves observation
occurs before delivery with distinct setup/handler contexts.

#### `ConsumeDurable`

Production callers in this repository: zero.

Current tests:

- `natsclient/consume_durable_test.go:44`;
- `natsclient/consume_durable_integration_test.go:40,82`.

Historical cross-repo intent is recorded in ADR-070 and its archived OpenSpec artifacts, but sister repositories were not
accessed during this design. The exported wrapper remains because it is an accepted cross-repo contract; its signature
becomes source-breaking so any adopter must supply bounded context. No unobserved durable alternative is added.

#### Ordinary managed port-backed callers

The required `ConsumeStreamWithConfig` path is used by:

- graph-ingest;
- json-generic, json-filter, and json-map;
- objectstore;
- agentic-governance;
- rule;
- file, HTTP POST, and WebSocket output components’ JetStream inputs;
- agentic-model and agentic-tools;
- all five agentic-dispatch consumers;
- the `iot_sensor` and `document` examples.

Agentic-loop uses the required contexts variant.

#### Explicit non-port callers

The following migrate to `ConsumeInternalStreamWithConfig`:

- four service flow-runtime ack-none streamers: `service/flow_runtime_stream.go`;
- agent completion/failure subscribers: `agentic/agentrun/agentrun.go`;
- max-delivery advisory observer: `internal/maxdelivery/observer.go`;
- registry capability consumer: `component/registry.go`.

`service/flow_runtime_stream.go`’s local client interface changes to expose
`ConsumeInternalStreamWithConfig`, ensuring its test doubles compile against the explicit non-port contract.

### Legacy `ConsumeStream` caller census

Production callers:

```text
rg -n '\.ConsumeStream\(' --glob '*.go' --glob '!**/*_test.go' --glob '!natsclient/doc.go' .
# 0 matches: no executable production callers. The excluded documentation-only match is inventoried below.
```

Test callers:

- `natsclient/client_test.go:385`
- `natsclient/integration_test.go:217`
- `natsclient/integration_test.go:335`
- `natsclient/client_integration_test.go:170`

Documentation caller:

- `natsclient/doc.go:116-122`

The four tests migrate to `ConsumeInternalStreamWithConfig` because they exercise client state, general consumption, or
metrics without a `JetStreamPort` contract. The package example is rewritten to show the distinction between explicit
non-port consumption and the required port-backed operation.

### Direct OTEL observation

`ObserveDirectPortConsumerPolicy` exists only because OTEL owns direct nats.go creation. It accepts bounded
`PortConsumerContext`, the exact final `jetstream.ConsumerConfig` already sent to NATS, and the returned consumer handle,
then returns an opaque cleanup closure. Requested value comes from the final config; canonical identity and effective
value come from `ConsumerInfo`. OTEL invokes cleanup after its fetch goroutine exits.

The internal observation record is unexported:

```go
type consumerPolicyRecord struct {
    component    string
    port         string
    stream       string
    consumer     string
    policySource string
    requested    int
    handle       jetstream.Consumer
}
```

Its key is derived only after successful `Info` from the canonical tuple
`(component, port, info.Stream, info.Name, policySource)`. Managed cleanup stores that internal key in the existing
private `consumerBinding`; `StopConsumer`, replacement, `StopAllConsumers`, and client close remove it without caller
participation.

The required signatures, `PortConsumerContext`, the explicit internal-consumer method, and the direct OTEL observer
require owner review.

### Metric registry ownership and collision behavior

Existing `MetricsRegistry.RegisterGaugeVec` is idempotent by key but does not return the already-registered collector.
A second creator can therefore receive success while retaining an unregistered local vector. Natsclient and OTEL MUST
NOT each construct the same metric.

Add:

```go
func (r *MetricsRegistry) RegisterOrGetGaugeVec(
    serviceName string,
    metricName string,
    candidate *prometheus.GaugeVec,
) (*prometheus.GaugeVec, error)
```

Behavior:

- first registration stores and returns `candidate`;
- registration under an existing identical key returns the canonical registered `*prometheus.GaugeVec`;
- Prometheus `AlreadyRegisteredError` returns its existing collector only when it is a compatible GaugeVec;
- a same-key incompatible collector or descriptor is a fatal registration collision;
- callers always retain the returned canonical vector, never their candidate blindly.

This exported registry method also requires owner review. Its present consumer is the natsclient consumer-policy
recorder. OTEL records through `natsclient.Client` and never registers these collectors itself.

### Exact effective-policy metrics

The canonical recorder owns:

```text
semstreams_jetstream_consumer_max_ack_pending_requested{
  component,port,stream,consumer,policy_source
}

semstreams_jetstream_consumer_max_ack_pending_effective{
  component,port,stream,consumer,policy_source
}

semstreams_jetstream_consumer_max_ack_pending_observation_available{
  component,port,stream,consumer,policy_source
}
```

Cardinality is bounded by configured consumer records. `component`, `port`, `stream`, and `consumer` are configuration
identities already bounded by the assembled flow; `policy_source` has exactly `port`, `component`, or `server`. Subjects,
entity IDs, message IDs, errors, and arbitrary values are forbidden labels.

Lifecycle:

1. All three exported port-backed methods require nonempty component/port context. The ordinary and durable wrappers
   delegate to the authoritative contexts implementation without weakening that requirement.
2. After create/update, the contexts implementation reads `ConsumerInfo` before starting delivery.
3. Requested policy is read from the final `StreamConsumerConfig`; canonical identity and effective policy come from
   `ConsumerInfo`.
4. The resulting private record is attached to the private managed `consumerBinding`.
5. Refresh uses the retained canonical record and consumer handle.
6. Replacement removes the old private record and every old label tuple before installing the new record.
7. `StopConsumer`, `StopAllConsumers`, `StopAndDeleteConsumer`, and client close clean policy state through the private
   binding; callers provide no identity.
8. OTEL observation returns a cleanup closure. OTEL calls it after its fetch goroutine has stopped.
9. Refresh failure/external disappearance deletes effective truth and sets availability to zero as already specified.

On initial success, requested and effective are set to their policy values and availability is set to `1`. Refresh
success replaces the effective value and keeps availability `1`; refresh failure retains requested, deletes effective, sets availability `0`,
and emits one transition WARN. If an externally disappeared consumer returns, a successful refresh restores effective
truth and availability. Metrics-disabled deployments still perform mandatory startup observation, comparison,
classification, and structured logging; only collector storage and refresh are absent.

This is configuration truth for the existing knob. It adds no `NumAckPending`, queue depth, drop, saturation, high-water,
or flow-port metric and does not absorb #309.

### Startup validation and record

For a nonzero final request, observed `ConsumerInfo.Config.MaxAckPending` must equal the request. A deterministic private
validator owns this comparison. Mismatch is a typed invalid/effective-policy configuration error and delivery does not
start.

For a zero request, any successfully observed inherited/default/capped value is valid. The internally derived source is
`server`. Positive and `-1` honored declarations use `port`; fixed agentic values use `component`.

Every successful initial observation emits exactly one INFO record:

```text
message="JetStream consumer acknowledgement policy applied"
component=<component>
port=<port>
stream=<stream>
consumer=<consumer>
policy_source=<port|component|server>
requested_max_ack_pending=<int>
effective_max_ack_pending=<int>
```

An initial `Info` transport/unavailable failure is transient, creates no metric binding, and prevents delivery/fetch.

### Create/update and observation error classification

A single natsclient classifier owns consumer-policy API error interpretation.

When `CreateOrUpdateConsumer` returns `*jetstream.APIError`:

- API error `10121` is a typed invalid/effective-policy configuration error;
- API error `10082` is a typed invalid/effective-policy configuration error;
- the original API error and numeric code remain discoverable through wrapping;
- transport, timeout, unavailable, leader, and other non-policy failures remain transient.

The `ConsumeStreamWithConfig` seam replaces its current unconditional transient wrapping at
`natsclient/stream.go:337-341` with this classifier before any consume context is created.

The direct OTEL path calls the same exported natsclient classifier rather than duplicating numeric-code interpretation:

```go
func ClassifyConsumerPolicyError(err error, operation string) error
```

Its only present callers are natsclient consumer creation and OTEL direct consumer creation. It is answer-shaped to the
two NATS policy codes and preserves all other errors. As a new exported natsclient surface, it requires owner review.

An observed requested/effective mismatch is invalid configuration but is not represented as a fabricated NATS API error.
Initial `Info` transport/unavailable failure is transient. A deterministic private validator unit test covers mismatch;
real NATS is not expected to accept a mismatched explicit value.

### Canonical port-field constraints

The canonical owner is `component.PortFieldInfo`, attached to the existing closed `portBindingTable`. Runtime validation,
runtime discovery, and generated schemas consume the same metadata.

Extend `PortFieldInfo` with:

```go
Minimum *int `json:"minimum,omitempty"`
```

Reuse its existing field-level:

```go
Directions []Direction `json:"directions,omitempty"`
```

Each `portBinding` gains an unexported:

```go
fieldConstraints map[string]PortFieldInfo
```

The JetStream binding owns:

```go
"max_ack_pending": {
    Type:          "int",
    Editable:      true,
    Minimum:       intPointer(-1),
    Directions:    []Direction{DirectionInput},
    zeroIsOmitted: true,
}
```

`GeneratePortFieldSchema` continues reflecting field shape, then overlays the binding-owned constraint. It fails loudly
during tests if a constraint names an absent field or conflicts with its reflected type. No validator, generator, or
component repeats `-1` or input-only policy independently.

Canonical port resolution invokes one generic constraint validator after normalization and before returning the resolved
port:

- numeric values below `Minimum` fail with typed component/port/kind/field context;
- a field whose nonzero value is present in a prohibited direction fails with the same context;
- zero is treated as absent for direction prohibition, preserving omission/explicit-zero behavior;
- the existing hand-written `MaxAckPending < -1` check is removed after equivalent metadata-driven tests pass.

The generator consumes the same in-memory `PortFields` tree:

- JetStream input variants contain `max_ack_pending` with `minimum: -1`;
- JetStream output variants contain an optional integer `max_ack_pending` property constrained by `const: 0`;
- that output property represents semantic absence only: it is not required, and it does not expose output consumer
  tuning;
- runtime discovery retains the input-only direction and minimum metadata and does not serialize the binding-private
  semantic-zero marker;
- no generated schema is hand-patched.

`PortFieldInfo.Minimum` remains exported JSON discovery metadata and requires owner review. The semantic-zero marker is
the unexported `PortFieldInfo.zeroIsOmitted` binding field. Runtime validation consumes it inside `component`; the
checked-in schema generator consumes it through the owner-reviewed read-only `PortFieldInfo.ZeroIsOmitted() bool`
method. No `zeroIsOmitted` JSON discovery field is added. Present consumers of the method are the checked-in schema
generator and its contract projection only; it adds no operator knob.

## Updated adopter seam

### Operator

They must know only:

- positive means a requested finite outstanding-ack ceiling;
- `-1` means unlimited outstanding acknowledgements;
- omission delegates to the component/server policy shown by effective-policy observation.

They no longer need a consumer census. Unsupported ownership is a boot error, while accepted values are observed before
delivery. The requested, effective, and availability gauges plus the startup record show the actual server result,
including inherited/capped zero behavior.

### External component author

A component author with a `JetStreamPort` calls the required port-backed `ConsumeStreamWithConfig` and supplies only
component name, port name, and whether the policy is component-owned. They do not copy stream, consumer, requested
value, effective value, or cleanup identity into an observation structure.

Authors needing split setup/handler lifetimes or the durable heartbeat wrapper use
`ConsumeStreamWithConfigContexts` or `ConsumeDurable` with the same required context.

The compiler rejects the former unobserved call signature. Non-port runtime owners use the explicitly named
`ConsumeInternalStreamWithConfig`, making the absence of a public port-policy claim visible at the call site.

The author still copies the existing consumer tuning fields into the final `StreamConsumerConfig`; observation then
derives requested policy from that final carrier and verifies it against NATS. There is no parallel policy declaration
whose value can diverge.

### Legacy API adopter migration

An adopter must first classify the old call:

- If it consumes a declared SemStreams JetStream input port, migrate to
  `ConsumeStreamWithConfig(ctx, owner, cfg, handler)`.
- If it is framework-internal consumption with no port contract, migrate to
  `ConsumeInternalStreamWithConfig(ctx, cfg, handler)`.

They must not invent a second stream/subject convenience wrapper. The final `StreamConsumerConfig` becomes the single
requested-policy carrier. The migration is compile-visible; an old binary does not silently continue through an
unobserved path.

## Breaking and E2E classification

This change is breaking in two independent ways.

### Configuration break

Previously accepted nonzero `max_ack_pending` values on JetStream outputs and component-owned agentic inputs now fail
validation.

### Exported Go source break

The following exported signatures intentionally change:

```go
ConsumeStreamWithConfig(ctx, cfg, handler)
```

becomes:

```go
ConsumeStreamWithConfig(ctx, owner, cfg, handler)
```

```go
ConsumeStreamWithConfigContexts(setupCtx, handlerCtx, cfg, handler)
```

becomes:

```go
ConsumeStreamWithConfigContexts(setupCtx, handlerCtx, owner, cfg, handler)
```

```go
ConsumeDurable(ctx, cfg, heartbeat, handler)
```

becomes:

```go
ConsumeDurable(ctx, owner, cfg, heartbeat, handler)
```

Non-port in-repo callers migrate from `ConsumeStreamWithConfig` to the new explicit
`ConsumeInternalStreamWithConfig`.

This is an intentional clean pre-v1 source break. No deprecated aliases, overloads, variadic compatibility arguments, or
optional observation fields remain. External and sister-repository adopters must update call sites before compiling.
Sister repositories were not accessed, so their exact caller counts are not claimed.

Migration:

```go
owner := natsclient.PortConsumerContext{
    Component:      componentName,
    Port:           portName,
    ComponentOwned: false, // true only for an intentional fixed component policy
}
err := client.ConsumeStreamWithConfig(ctx, owner, cfg, handler)
```

Durable migration uses the same owner context:

```go
err := client.ConsumeDurable(ctx, owner, cfg, heartbeat, handler)
```

### Removed legacy API

The exported `client.ConsumeStream(ctx, streamName, subject, handler)` method is removed.

Non-port callers construct one authoritative `StreamConsumerConfig` and migrate to
`ConsumeInternalStreamWithConfig`. Port-backed callers migrate to `ConsumeStreamWithConfig` with
`PortConsumerContext` and their final consumer configuration. This is an intentional exported Go source break; no
compatibility wrapper is retained.

No stored identity, payload, subject, or data format changes; no wipe or reseed is required.

Because the commit is BREAKING, `task e2e:core` and `task e2e:agentic` must be green before landing. The agentic tier must
exercise the direct contexts entry point. Focused real-NATS integration must exercise the durable wrapper’s required
observation path even though no current in-repo production caller exists.

## TDD and conformance matrix

| Contract | First failing proof | Production seam |
|---|---|---|
| One constraint owner | `PortFieldInfo` discovery, runtime validation, and generator agreement tests | `portBinding.fieldConstraints` |
| Output positive/`-1` rejected; zero unchanged | Direction-resolution table test | Generic constraint validator |
| Compatible metric registration shares collector | Registry identity/collision tests | `RegisterOrGetGaugeVec` |
| Ordinary wrapper requires observation | Compile migration plus fake pre-delivery assertion | `ConsumeStreamWithConfig` |
| Contexts variant requires observation | Distinct setup/handler context integration test | `ConsumeStreamWithConfigContexts` |
| Agentic-loop uses observed contexts path | Component test with fixed policy and distinct contexts | Agentic-loop setup |
| Durable wrapper requires observation | Unit delegation test and real-NATS integration | `ConsumeDurable` |
| No unobserved durable compatibility | Exported API compile contract/API census | Removed old signature |
| Non-port entry point has exact census | Source call-site contract test | `ConsumeInternalStreamWithConfig` |
| Port-backed caller cannot use internal path | Production AST/call-site census against `GetConsumerConfig` users | All component packages |
| No internal contexts/durable phantom | Exported symbol census | natsclient public API |
| Source migration is complete | `go test`/build of all in-repo callers and interfaces | Complete caller census |
| Legacy ambiguous creator is absent | Exported-symbol/API census | Removal from `natsclient/client.go` |
| No renamed convenience alias appears | Exported method-name and signature census | natsclient public API |
| Four legacy tests use explicit non-port config | Updated unit/integration tests | `ConsumeInternalStreamWithConfig` |
| Package docs teach both migrations | Doc example/contract assertion | `natsclient/doc.go` |
| No production behavior is removed in-repo | Exact non-test caller search remains empty | Repository census |
| Port-backed migration remains observed | Managed-path integration test | Required-context method |
| No duplicated requested policy | Fake final config with sentinel value | Private managed observer reads final config |
| Created consumer owns identity | Fake config names differ from fake `ConsumerInfo`; asserted labels/key use Info | Private record construction |
| Managed cleanup needs no caller identity | Replacement/stop tests inspect private tracker and metric deletion | `consumerBinding` private key |
| OTEL copies no consumer identity/policy | Fake final nats.go config and ConsumerInfo with distinct sentinels | Direct observer |
| OTEL cleanup is opaque | Stop test invokes returned closure after fetch exit | Closure-captured private key |
| Wrong caller context fails before creation | Empty component/port table test | Required-context validation |
| Nonzero mismatch is invalid | Deterministic private validator table, not real NATS | Private requested/effective validator |
| Initial `Info` failure is transient | Deterministic fake consumer lookup/Info test | Observation error seam |
| API 10121/10082 are invalid | Wrapped `jetstream.APIError` unit table | Shared policy error classifier |
| Other create/update failure is transient | Timeout/unavailable fake table | Same classifier |
| Positive/zero/`-1` survive NATS | Real-NATS `ConsumerInfo.Config` table | Existing carriers and observer |
| In-place update preserves durable identity/state | Real-NATS update test | `CreateOrUpdateConsumer` |
| NATS policy rejection remains invalid | Real-NATS configured-limit rejection | Shared classifier |
| Effective refresh never goes stale | Fake success→Info failure→recovery sequence | Policy tracker and three gauges |
| Replacement/deletion/stop cleans series | Deterministic lifecycle tests | Private binding key and opaque OTEL cleanup closure |
| Startup record is identity-complete and exact-one | Captured slog test | Initial observation only |
| #309 remains separate | Collector census test | No queue/drop collector added |

Real NATS does not test an impossible accepted mismatch. It proves positive, zero, `-1`, in-place update, and real server
policy rejection. The private validator deterministically proves mismatch classification.

Tests must use explicit readiness/consumer-info polling, not sleeps.

## Exact production file deltas

### Canonical constraint ownership

- `component/port_jetstream.go` — correct zero/inherited wording.
- `component/schema_tags.go` — add `PortFieldInfo.Minimum`; overlay binding constraints.
- `component/schema_tags_test.go` — discovery metadata and conflict tests.
- `component/port_codec.go` — add binding-owned field constraints; remove handwritten minimum.
- `component/port_resolver.go` — invoke generic direction/minimum validation.
- `component/port_codec_test.go`
- `component/port_resolver_test.go`

### Existing honoring consumers

- `processor/json_generic/json_generic.go`
- `processor/json_filter/json_filter.go`
- `processor/json_map/json_map.go`
- `storage/objectstore/component.go`
- `processor/agentic-governance/component.go`
- `processor/rule/processor.go`
- `output/file/file.go`
- `output/httppost/httppost.go`
- `output/websocket/websocket.go`

`processor/graph-ingest/component.go` requires no behavioral forwarding change.

### Component-owned consumers

- `processor/agentic-loop/component.go`
- `processor/agentic-model/component.go`
- `processor/agentic-tools/component.go`
- Their existing package tests, adding pure setup/rejection coverage without exporting helpers.

### Bypasses

- `processor/agentic-dispatch/component.go`
- `processor/agentic-dispatch/port_overrides_test.go`
- `examples/processors/iot_sensor/component.go`
- `examples/processors/document/component.go`
- Package tests for both examples.

### Canonical metric registration

- `metric/registry.go` — `RegisterOrGetGaugeVec`.
- `metric/registry_test.go` — canonical identity, compatible duplicate, incompatible type/descriptor, concurrency.

### Exported consumption entry points

- `natsclient/stream.go`
  - change both exported managed signatures;
  - keep the contexts method as authoritative observed implementation;
  - add the explicit non-port wrapper over an unexported contexts core.
- `natsclient/consume_durable.go`
  - add required `PortConsumerContext`;
  - delegate through the observed managed path.
- `natsclient/consume_durable_test.go`
- `natsclient/consume_durable_integration_test.go`
- `natsclient/stream_integration_test.go`
- `natsclient/consumer_policy_callsite_test.go`
  - exact managed/internal/context/durable source census.

### Direct contexts caller

- `processor/agentic-loop/component.go`
- existing agentic-loop component/integration tests.

### Explicit non-port migration

- `service/flow_runtime_stream.go`
- its interface mocks and flow-runtime tests;
- `agentic/agentrun/agentrun.go`
- `internal/maxdelivery/observer.go`
- `component/registry.go`

### Policy observation implementation

- `natsclient/client.go` — retain the private policy-record key and clean it on replacement, stop, delete, and close.
- `natsclient/consumer_policy.go`
  - remove exported binding/forget operations;
  - add unexported `consumerPolicyRecord`;
  - add managed record construction and direct OTEL observation returning cleanup closure.
- `natsclient/consumer_policy_test.go`
  - required context;
  - final-config requested authority;
  - ConsumerInfo identity authority;
  - source derivation;
  - private cleanup.
- `natsclient/consumer_policy_integration_test.go`
  - positive, zero, `-1`, update, and policy rejection through the required managed path.
- `natsclient/jetstream_metrics.go` — canonical requested/effective/availability vectors and refresh.
- `natsclient/jetstream_metrics_test.go`

### Managed caller migration context

Every previously listed port-backed consumer file changes to pass only:

```go
natsclient.PortConsumerContext{
    Component:      <stable component metadata name>,
    Port:           port.Name,
    ComponentOwned: <true only for agentic-loop/model/tools>,
}
```

No caller supplies stream, consumer, requested, effective, or cleanup identity.

### Direct OTEL

- `output/otel/component.go`
  - retain the exact final `jetstream.ConsumerConfig` used for creation;
  - pass it and the created consumer to `ObserveDirectPortConsumerPolicy`;
  - store only the returned cleanup closure;
  - invoke cleanup after fetch goroutine termination.
- `output/otel/component_test.go`
  - distinct-sentinel proof that requested comes from final config and identity comes from `ConsumerInfo`;
  - observation-before-fetch;
  - cleanup-after-fetch.

### Documentation/API contract

- `natsclient/doc.go` and exported method comments.
- `docs/adr/070-gated-dag-durable-dispatch.md`
  - append a #963 source-migration clarification; do not rewrite historical text.
- Current operational documentation that shows any old method signature.
- Archived OpenSpec artifacts remain historical and are not edited.

### Legacy consumer retirement

- `natsclient/client.go`
  - remove `Client.ConsumeStream`;
  - remove its direct `CreateOrUpdateConsumer`, ignored-Info, tracking, and bookkeeping path.
- `natsclient/client_test.go` — migrate the legacy call to `ConsumeInternalStreamWithConfig`.
- `natsclient/integration_test.go` — migrate both legacy calls.
- `natsclient/client_integration_test.go` — migrate the legacy call.
- `natsclient/doc.go`
  - remove the advertised `ConsumeStream` example;
  - show explicit non-port and required port-backed alternatives.
- `natsclient/consumer_policy_callsite_test.go`
  - assert the legacy exported symbol/signature is absent;
  - assert no stream/subject-only replacement appears.

### Schema generation

- `cmd/openapi-generator/main.go`
- `cmd/openapi-generator/main_test.go`
- The generated `schemas/*.v1.json` files currently containing `"ports"`:
  `agentic-dispatch`, `agentic-governance`, `agentic-loop`, `agentic-model`, `agentic-tools`, `file`, `file_input`,
  `graph-clustering`, `graph-embedding`, `graph-gateway`, `graph-index-spatial`, `graph-index-temporal`, `graph-index`,
  `graph-ingest`, `graph-query`, `httppost`, `json_filter`, `json_generic`, `json_map`, `lifecycle-gateway`,
  `objectstore`, `otel-exporter`, the five `research-graph-*` schemas, `rule-processor`, `udp`, `websocket`, and
  `websocket_input`.

Only generator output is edited; no schema is hand-patched.

### Documentation

- `docs/advanced/11-jetstream-tuning.md` — replace stale unset state; describe fixed policies, actual zero semantics,
  and in-place update.
- `docs/operations/14-timeout-chain.md` — remove universal-honor claim; state honor versus component-owned rejection.
- `docs/adr/046-parallel-fan-out-and-gated-dag-dispatch.md` — append a #963 clarification that agent-task is fixed at 1;
  do not rewrite historical decision text.
- `configs/rules/example-fan-out/README.md` — remove ineffective operator instruction.
- `docs/concepts/14-orchestration-layers.md` — retain and clarify fixed agent-task commitment.
- `docs/concepts/03-streams-vs-kv-watches.md` — distinguish NATS capability from SemStreams component policy.
- Component field comments and generated schema descriptions must use inherited/default/capped zero wording.

## OpenSpec artifact drafts

### `openspec/changes/honor-jetstream-max-ack-pending/proposal.md`

```md
# Honor JetStream max_ack_pending contracts

## Why

`JetStreamPort.max_ack_pending` is accepted and extracted, but only graph-ingest currently forwards it. Nine core
consumers omit it, three agentic consumers silently replace it, four port-backed consumers bypass the extractor, and
outputs accept it without creating a consumer. Operators cannot tell declaration from effective NATS policy.

## What Changes

- Ordinary port-backed JetStream consumers honor the existing declaration.
- Agentic-loop, agentic-model, and agentic-tools retain fixed policies and reject nonzero declarations.
- JetStream outputs reject nonzero `max_ack_pending`.
- All three exported port-backed natsclient entry points require bounded component/port context and perform policy
  observation before delivery.
- Non-port consumers migrate to the explicitly named internal-consumption entry point.
- The ambiguous legacy `Client.ConsumeStream` convenience method is removed without a renamed alias.
- Requested and effective policy are derived from final creation artifacts; no parallel policy identity is introduced.
- Existing effective-policy metrics, schema/catalog correction, error classification, and documentation corrections
  remain as designed.

No generic nonzero default, payload, subject, stream, bucket, or duplicate policy declaration is introduced.

## Impact

**BREAKING configuration:** explicit nonzero values on outputs and component-owned agentic inputs now fail.

**BREAKING Go API:**

- removal of `Client.ConsumeStream`;
- required-context signature changes to `ConsumeStreamWithConfig`, `ConsumeStreamWithConfigContexts`, and
  `ConsumeDurable`;
- addition of the explicit non-port `ConsumeInternalStreamWithConfig`.

Existing source using the prior signatures does not compile. No compatibility alias is retained.

## Additional breaking API retirement

`Client.ConsumeStream(ctx, streamName, subject, handler)` is removed. It has no in-repository production caller and
directly creates a consumer outside the required observed port-backed path and the explicit non-port path.

Tests and non-port adopters migrate to `ConsumeInternalStreamWithConfig`. Port-backed adopters migrate to the required
`ConsumeStreamWithConfig` operation with `PortConsumerContext`.

This removal is an additional exported Go source break. No alias or renamed stream/subject convenience method is added.

No stored-data migration or reseed is required.
```

### `openspec/changes/honor-jetstream-max-ack-pending/design.md`

Use the design sections above verbatim in this order:

1. accepted inventory;
2. premises;
3. options and costs;
4. recommended contract;
5. complete disposition table;
6. observation/update behavior;
7. schema/catalog truth;
8. adopter seam;
9. breaking/E2E classification;
10. conformance matrix;
11. file deltas and stop conditions.

### `specs/jetstream-consumer-policy/spec.md`

```md
## ADDED Requirements

### Requirement: Every port-backed JetStream input has an explicit acknowledgement-admission disposition

An ordinary port-backed JetStream input SHALL honor its canonical `max_ack_pending` declaration through the existing
consumer carriers. Omission or zero SHALL leave the value unset for server policy; positive values and `-1` SHALL be
forwarded unchanged. No framework-wide positive default SHALL be supplied.

A component that owns a fixed acknowledgement-admission policy SHALL reject every nonzero declaration before creating
a consumer and SHALL identify the component, port, field, and component-owned effective value. It SHALL NOT accept and
silently replace the declaration.

#### Scenario: Positive ordinary declaration reaches NATS

- **GIVEN** an ordinary JetStream input declares `max_ack_pending: 17`
- **WHEN** its component creates the consumer
- **THEN** NATS reports effective `MaxAckPending=17`

#### Scenario: Unlimited ordinary declaration reaches NATS

- **GIVEN** an ordinary JetStream input declares `max_ack_pending: -1`
- **WHEN** its component creates the consumer
- **THEN** NATS reports effective `MaxAckPending=-1`

#### Scenario: Component-owned declaration fails before consumer creation

- **GIVEN** an agentic component with fixed policy receives any nonzero declaration
- **WHEN** startup validates its consumer configuration
- **THEN** startup fails with typed component, port, field, and fixed-value context
- **AND** no consumer is created

### Requirement: Agentic acknowledgement-admission policies remain component-owned

Agentic-loop SHALL use 1 for `agent.task`, `agent.response`, and `tool.result`, and 10 for its fast/advisory input.
Agentic-model SHALL use 1. Agentic-tools SHALL use 3. Omission SHALL select these policies rather than server default.

#### Scenario: Agent task remains serial per consumer

- **GIVEN** agentic-loop has no declared `max_ack_pending`
- **WHEN** it creates its `agent.task` consumer
- **THEN** NATS reports effective `MaxAckPending=1`

### Requirement: Every exported port-backed consumption operation requires policy context

`ConsumeStreamWithConfig`, `ConsumeStreamWithConfigContexts`, and `ConsumeDurable` SHALL require nonempty component and
port context. Each SHALL derive requested policy from its final `StreamConsumerConfig`, derive canonical consumer
identity and effective policy from `ConsumerInfo`, and complete initial observation before delivery begins.

The former signatures without context SHALL NOT remain as aliases, overloads, or compatibility wrappers.

#### Scenario: Ordinary wrapper cannot bypass observation

- **GIVEN** a port-backed caller uses `ConsumeStreamWithConfig`
- **WHEN** the consumer is created
- **THEN** the wrapper passes its required owner context into the contexts implementation
- **AND** delivery starts only after successful observation

#### Scenario: Split-context caller cannot bypass observation

- **GIVEN** agentic-loop uses distinct setup and handler contexts
- **WHEN** it calls `ConsumeStreamWithConfigContexts`
- **THEN** it supplies component and port context
- **AND** setup observation completes under the setup context
- **AND** delivered handlers retain the declared handler-context lifetime

#### Scenario: Durable wrapper cannot bypass observation

- **GIVEN** a durable port-backed consumer uses `ConsumeDurable`
- **WHEN** heartbeat validation succeeds
- **THEN** the wrapper delegates through the required port-backed consumption path
- **AND** policy observation completes before its heartbeat-managed handler receives a message

### Requirement: Non-port consumption is explicit and bounded to present consumers

Consumers that make no `JetStreamPort` configurability claim MAY use
`ConsumeInternalStreamWithConfig`. A port-backed consumer SHALL NOT use it.

No exported split-context or durable internal variant SHALL be introduced without a present non-port consumer and a new
surface/adopter inventory.

#### Scenario: Flow-runtime consumer is explicitly non-port

- **GIVEN** a service-internal ack-none flow-runtime streamer
- **WHEN** it installs its consumer
- **THEN** it calls `ConsumeInternalStreamWithConfig`
- **AND** does not emit a port-policy observation

#### Scenario: Port-backed census contains no internal call

- **WHEN** production consumption call sites are enumerated
- **THEN** every `GetConsumerConfig`-backed consumer uses a required port operation
- **AND** `ConsumeInternalStreamWithConfig` callers equal the named non-port census

### Requirement: Durable source compatibility is intentionally broken

The old `ConsumeDurable(ctx, cfg, heartbeat, handler)` source signature SHALL be retired. Adopters SHALL migrate by
supplying `PortConsumerContext`. No compatibility overload SHALL preserve unobserved durable consumption.

#### Scenario: Durable adopter supplies bounded context

- **GIVEN** code using the prior durable signature
- **WHEN** it upgrades
- **THEN** compilation requires component, port, and component-owned context
- **AND** stream, consumer, and MaxAckPending are not copied into that context

### Requirement: The legacy unclassified stream consumer is retired

The exported `Client.ConsumeStream(ctx, streamName, subject, handler)` operation SHALL NOT exist. No alias or wrapper
SHALL directly create a consumer outside either required observed port-backed consumption or explicitly named non-port
consumption with a complete `StreamConsumerConfig`.

A non-port consumer SHALL use `ConsumeInternalStreamWithConfig`. A port-backed consumer SHALL use a required
`PortConsumerContext` operation.

#### Scenario: Exported API contains no ambiguous consumer creator

- **WHEN** the exported natsclient API is enumerated
- **THEN** `ConsumeStream` is absent
- **AND** no renamed convenience wrapper accepts only stream and subject
- **AND** every remaining consumer-creation operation is explicitly port-backed or non-port

#### Scenario: Legacy tests migrate to explicit non-port configuration

- **GIVEN** a client test previously called `ConsumeStream` with stream and subject
- **WHEN** it is migrated
- **THEN** it constructs one authoritative `StreamConsumerConfig`
- **AND** calls `ConsumeInternalStreamWithConfig`
- **AND** no policy observation is claimed

#### Scenario: A component-port migration uses the observed path

- **GIVEN** an external adopter used `ConsumeStream` for a SemStreams JetStream input port
- **WHEN** it migrates
- **THEN** it supplies `PortConsumerContext` and the final `StreamConsumerConfig`
- **AND** requested policy is observed before delivery

### Requirement: Direct OTEL observation derives policy from creation artifacts

OTEL SHALL pass its exact final nats.go consumer configuration and returned consumer handle to the common natsclient
observer before starting fetch. The observer SHALL derive requested policy from that final configuration and canonical
identity/effective policy from `ConsumerInfo`.

The observer SHALL return an opaque cleanup closure. OTEL SHALL invoke it after its fetch goroutine exits. OTEL SHALL
NOT construct a policy binding or copy stream, consumer, requested value, or cleanup identity into observation state.

#### Scenario: OTEL performs no second policy declaration

- **GIVEN** OTEL directly creates a consumer
- **WHEN** it registers policy observation
- **THEN** requested policy comes from the exact configuration sent to NATS
- **AND** canonical identity and effective policy come from the returned consumer
- **AND** the only caller context is component, port, and component-owned status

Initial observation transport or availability failure SHALL be transient and SHALL prevent delivery or fetch from
beginning. A nonzero final request SHALL equal the observed NATS value; mismatch SHALL be invalid effective-policy
configuration. A zero request SHALL accept the observed inherited/default/capped value.

#### Scenario: Initial Info failure is transient

- **GIVEN** consumer creation succeeds but initial `Info` is unavailable
- **WHEN** policy observation runs
- **THEN** startup fails transiently
- **AND** no metric record is installed
- **AND** delivery or OTEL fetch does not begin

#### Scenario: Deterministic mismatch is invalid configuration

- **GIVEN** the private policy validator receives unequal nonzero requested and effective values
- **WHEN** it classifies the observation
- **THEN** it returns typed invalid/effective-policy configuration
- **AND** no consume or fetch operation starts

### Requirement: Consumer policy metrics never retain stale effective truth

The framework SHALL expose exactly:

- `semstreams_jetstream_consumer_max_ack_pending_requested`;
- `semstreams_jetstream_consumer_max_ack_pending_effective`;
- `semstreams_jetstream_consumer_max_ack_pending_observation_available`.

Each SHALL use labels `component`, `port`, `stream`, `consumer`, and `policy_source`, where policy source is one of
`port`, `component`, or `server`.

On initial or refresh success, requested and effective values SHALL be current and availability SHALL be 1. On refresh
failure or external disappearance, the effective series SHALL be removed and availability SHALL be 0. Replacement,
stop, deletion, client close, and OTEL stop SHALL remove every series for the old private record.

These metrics SHALL report configuration truth only and SHALL NOT add queue, drop, pending, saturation, or flow-port
metrics.

#### Scenario: Refresh failure removes stale effective value

- **GIVEN** a tracked consumer previously reported an effective value
- **WHEN** periodic `Info` refresh fails
- **THEN** its effective series is deleted
- **AND** requested remains
- **AND** observation availability becomes 0

#### Scenario: Replacement removes old labels

- **GIVEN** a private record is replaced with a changed consumer identity or policy source
- **WHEN** the new record is installed
- **THEN** every old requested, effective, and availability series is deleted
- **AND** only the new label tuple remains

#### Scenario: Stop cleans policy observation

- **WHEN** a managed consumer or direct OTEL consumer stops
- **THEN** its tracked private record and all three metric series are removed

### Requirement: Successful initial policy observation emits one identity-complete record

Successful initial observation SHALL emit exactly one INFO record with message
`JetStream consumer acknowledgement policy applied` and fields `component`, `port`, `stream`, `consumer`,
`policy_source`, `requested_max_ack_pending`, and `effective_max_ack_pending`.

A refresh SHALL NOT repeat the success record. Availability transitions MAY emit one WARN per transition.

#### Scenario: Server-owned zero is recorded honestly

- **GIVEN** the final request is zero
- **WHEN** initial observation succeeds
- **THEN** the record carries `policy_source=server`
- **AND** requested value 0
- **AND** the actual observed effective value

### Requirement: NATS consumer-policy rejections are invalid configuration

NATS API errors 10121 and 10082 returned by consumer create/update SHALL be classified as invalid/effective-policy
configuration while preserving the original API error and code. Transport and unavailable errors SHALL remain
transient.

#### Scenario: Policy API errors are not retryable transport failures

- **WHEN** create/update returns API error 10121 or 10082
- **THEN** startup returns typed invalid configuration
- **AND** no consumer-policy metric record or consume/fetch loop starts

### Requirement: Metric registration returns one canonical collector

Consumer-policy metric registration SHALL return and retain the canonical registered GaugeVec. Repeated compatible
registration SHALL return that same collector. A same-key incompatible type or descriptor SHALL fail fatally rather
than report false idempotent success.

#### Scenario: Two clients share the registered collector

- **GIVEN** two natsclient instances use one MetricsRegistry
- **WHEN** both initialize policy metrics
- **THEN** both receive the same registered GaugeVec instances
- **AND** observations from either appear in the same scrape

### Requirement: Consumer policy updates preserve durable consumer state

A declaration change SHALL follow the existing component replacement lifecycle and SHALL use
`CreateOrUpdateConsumer`. It SHALL NOT delete and recreate a durable consumer merely to change `MaxAckPending`.

#### Scenario: Changed declaration updates the durable consumer

- **GIVEN** an existing durable consumer
- **WHEN** component replacement changes its honored `max_ack_pending`
- **THEN** the existing durable consumer is updated in place
- **AND** its pending durable state is not discarded by consumer deletion
```

### `specs/component-runtime-config/spec.md`

```md
## ADDED Requirements

### Requirement: Canonical port-field constraints govern runtime and generated schemas

The canonical port binding catalog SHALL own field-level numeric minima and allowed directions through `PortFieldInfo`.
Runtime port resolution, runtime discovery, and checked-in schema generation SHALL consume that same metadata. They
SHALL NOT repeat a field’s minimum or direction policy in component-local validators or generator special cases.

A zero numeric value SHALL remain omission for a field whose JSON contract uses `omitempty`. Direction prohibition
SHALL apply only to a nonzero declared value.

#### Scenario: Runtime discovery reports the canonical constraint

- **GIVEN** runtime discovery reports the JetStream `max_ack_pending` field
- **WHEN** its `PortFieldInfo` is inspected
- **THEN** its minimum is `-1`
- **AND** its allowed directions contain only input

#### Scenario: Runtime validation consumes the same minimum

- **GIVEN** a JetStream input declares `max_ack_pending: -2`
- **WHEN** canonical port resolution runs
- **THEN** it fails before component initialization
- **AND** the failure identifies port, kind, and field

#### Scenario: Generated input schema consumes the canonical constraint

- **GIVEN** a generated component schema containing JetStream input ports
- **WHEN** the input variant is inspected
- **THEN** `max_ack_pending` is present with `minimum: -1`

#### Scenario: Generated output schema preserves semantic omission

- **GIVEN** a generated component schema containing JetStream output ports
- **WHEN** the output variant is inspected
- **THEN** `max_ack_pending` is present as an optional integer property constrained by `const: 0`
- **AND** it is not required
- **AND** positive values and `-1` are rejected by the schema
- **AND** the property does not represent output consumer tuning

### Requirement: JetStream outputs reject consumer acknowledgement admission

A JetStream output SHALL reject any nonzero `max_ack_pending` because an output declaration creates no consumer. Zero
or omission SHALL remain valid and SHALL create no consumer-policy observation.

#### Scenario: Positive output declaration is rejected

- **GIVEN** a JetStream output declares a positive `max_ack_pending`
- **WHEN** canonical direction validation runs
- **THEN** configuration fails before component initialization
- **AND** the failure identifies the output port, JetStream kind, and field

#### Scenario: Unlimited output declaration is rejected

- **GIVEN** a JetStream output declares `max_ack_pending: -1`
- **WHEN** canonical direction validation runs
- **THEN** configuration fails before component initialization
- **AND** no consumer is created

#### Scenario: Zero output declaration preserves omission behavior

- **GIVEN** a JetStream output omits `max_ack_pending` or supplies zero
- **WHEN** canonical direction validation runs
- **THEN** the output remains valid
- **AND** no consumer-policy surface is born
```

These are separate ADDED requirements. The existing “Component ports have one strict canonical grammar” requirement and
all nine of its current scenarios remain byte-for-byte present, preventing archive-time scenario loss.

### `openspec/changes/honor-jetstream-max-ack-pending/tasks.md`

```md
# Tasks

## Contract tests first

- [ ] Add positive and `-1` representative ordinary-consumer tests.
- [ ] Add agentic-loop/model/tools omission-effective and nonzero-rejection tests.
- [ ] Add dispatch, OTEL, and example bypass tests.

## Canonical constraint metadata

- [ ] Add `PortFieldInfo.Minimum` and record the exported-surface owner-review gate.
- [ ] Add binding-owned `fieldConstraints` for JetStream `max_ack_pending`.
- [ ] Make runtime resolution and generator consume the same metadata.
- [ ] Remove the handwritten `< -1` validator only after equivalent tests pass.
- [ ] Prove zero/output backward behavior, positive and `-1` output rejection, and input minimum.
- [ ] Prove runtime discovery and generated schemas expose identical direction/minimum facts.
- [ ] Correct zero semantics in public comments without introducing a default.

## Honor-policy consumers

- [ ] Forward the existing extracted field in all nine omission consumers.
- [ ] Carry extracted consumer config through all five agentic-dispatch bindings.
- [ ] Honor it in OTEL’s direct nats.go consumer config.
- [ ] Honor it in both E2E-only example processors.
- [ ] Re-run the complete production census and account for every result.

## Component-owned policies

- [ ] Retain agentic-loop 1/10, agentic-model 1, and agentic-tools 3.
- [ ] Reject every nonzero declaration before consumer creation with typed context.
- [ ] Emit component-owned policy context at startup.

## Canonical metric ownership

- [ ] Add `RegisterOrGetGaugeVec`.
- [ ] Prove compatible repeated registration returns the identical registered collector.
- [ ] Fail fatally on incompatible collector type or descriptor.
- [ ] Ensure natsclient alone registers policy collectors; OTEL records through natsclient.

## Complete exported API migration

- [ ] Change `ConsumeStreamWithConfig` to require `PortConsumerContext`.
- [ ] Change `ConsumeStreamWithConfigContexts` to require the same context.
- [ ] Change `ConsumeDurable` to require the same context.
- [ ] Remove all old-signature aliases and overloads.
- [ ] Add `ConsumeInternalStreamWithConfig` for the exact non-port census.
- [ ] Keep the internal split-context core unexported.
- [ ] Do not add `ConsumeInternalStreamWithConfigContexts` or `ConsumeInternalDurable`.
- [ ] Migrate agentic-loop’s direct contexts call.
- [ ] Migrate every ordinary managed port caller.
- [ ] Migrate the four named non-port owner groups and flow-runtime interface mocks.
- [ ] Update durable unit/integration tests to prove mandatory observation.
- [ ] Add a production call-site census covering all four exported operations.
- [ ] Prove no `GetConsumerConfig` consumer calls the internal operation.
- [ ] Prove no production caller or test retains the old signatures.
- [ ] Update exported docs and append the ADR-070 migration clarification.
- [ ] Record the configuration break and Go source break independently in proposal/release material.

## Retire legacy direct consumer creation

- [ ] Remove exported `Client.ConsumeStream`.
- [ ] Do not add `ConsumeInternalStream` or another stream/subject-only convenience wrapper.
- [ ] Migrate the four test call sites to `ConsumeInternalStreamWithConfig`.
- [ ] Replace the package documentation example with explicit port-backed and non-port examples.
- [ ] Add the removal to proposal, breaking notes, and adopter migration text.
- [ ] Extend the exported API census to prove the symbol and any equivalent alias are absent.
- [ ] Re-run the non-test caller census and record zero production callers.
- [ ] Obtain owner approval for the exported method removal before implementation.

## Direct OTEL observation

- [ ] Add the owner-reviewed direct observer taking bounded context, exact final nats.go config, and created consumer.
- [ ] Derive requested from the exact final config and identity/effective from `ConsumerInfo`.
- [ ] Return an opaque cleanup closure rather than an identity-bearing forget API.
- [ ] Start no fetch goroutine before observation succeeds.
- [ ] Invoke cleanup only after fetch termination.
- [ ] Prove OTEL performs no second policy-value or consumer-identity copy.

## Policy classification and metric lifecycle

- [ ] Classify API 10121 and 10082 invalid while preserving the original error/code.
- [ ] Keep transport/unavailable and initial `Info` failure transient.
- [ ] Use a private deterministic validator for nonzero mismatch.
- [ ] Emit the exact identity-complete startup INFO once.
- [ ] Implement requested/effective/availability initial set and periodic refresh.
- [ ] Delete stale effective data on refresh failure or external disappearance.
- [ ] Delete all old series on replacement, deletion, stop, client close, and OTEL stop.
- [ ] Prove recovery restores effective truth without duplicating the startup record.

## Schema generation

- [ ] Regenerate every schema containing ports and inspect the complete diff.
- [ ] Do not hand-edit generated schema files.

## OpenSpec safety

- [ ] Add separate component-runtime-config requirements; do not modify or truncate the existing strict-grammar
      requirement or its nine scenarios.
- [ ] Promote transient observation failure, mismatch class, API-code class, exact metrics/labels/lifecycle, exact
      startup record, and direct OTEL behavior into normative scenarios.
- [ ] Run OpenSpec validation and inspect the archive preview for scenario loss.

## Real-NATS proof

- [ ] Real NATS: positive effective value.
- [ ] Real NATS: zero server-observed value.
- [ ] Real NATS: `-1`.
- [ ] Real NATS: in-place update preserving durable consumer state.
- [ ] Real NATS: configured-limit policy rejection.
- [ ] Do not manufacture a real-NATS accepted mismatch; cover mismatch through the deterministic private validator.

## Documentation

- [ ] Correct the tuning guide’s current state, concurrency wording, and update behavior.
- [ ] Correct timeout-chain generalization, ADR-046 clarification, fan-out example, and concept links.
- [ ] Document zero/inherited/capped, positive, `-1`, component-owned, and update behavior consistently.

## Verification

- [ ] Run focused unit and real-NATS integration tests with `-race`.
- [ ] Run `task lint`.
- [ ] Run `go test -race ./...`.
- [ ] Run `task schema:generate` and prove no uncommitted generation drift.
- [ ] Run `go test ./test/contract/...`.
- [ ] Run and record `task e2e:core`.
- [ ] Run and record `task e2e:agentic`.
- [ ] Record optional OTEL/example E2E coverage or the explicit coverage gap.
- [ ] Obtain SemStreams reviewer approval before integration.
```

## Stop conditions and owner-review gates

Stop before implementation if:

- the accepted census changes;
- any component cannot be classified as honor or evidenced component-owned policy;
- `Client.ConsumeStream` remains exported;
- a deprecated alias or renamed stream/subject-only wrapper preserves its direct-create behavior;
- the old method is changed to accept `PortConsumerContext` without first gaining a single authoritative final
  `StreamConsumerConfig`;
- any migrated test uses the managed path while claiming no JetStream port;
- package documentation continues advertising the removed method;
- the proposal or breaking section omits this fourth exported API break;
- production callers are discovered, invalidating the zero-consumer premise;
- owner review does not approve removal of the exported method;
- either existing exported `ConsumeStreamWithConfigContexts` or `ConsumeDurable` remains on its old signature;
- any compatibility alias, overload, variadic argument, or optional context preserves unobserved port consumption;
- agentic-loop can reach the internal/unobserved contexts core;
- a port-backed caller reaches `ConsumeInternalStreamWithConfig`;
- an exported internal contexts or durable variant is introduced without a present consumer;
- the call-site census omits service flow-runtime’s local interface or its mocks;
- the breaking section reports only configuration breakage and omits exported Go source breakage;
- sister-repository caller counts are claimed without access;
- owner review does not approve all three exported signature changes, `PortConsumerContext`, and the new explicit
  non-port method;
- a managed caller supplies stream, consumer, requested, effective, or cleanup identity separately from its final
  `StreamConsumerConfig` and created consumer;
- canonical metric labels or cleanup keys use caller-predicted stream/consumer instead of `ConsumerInfo`;
- OTEL must repeat `MaxAckPending`, stream, or consumer identity outside its exact final nats.go config and created
  consumer;
- OTEL cleanup requires reconstructing identity instead of invoking the returned closure;
- owner review does not approve the additive exported `PortFieldInfo.Minimum`;
- owner review does not approve `RegisterOrGetGaugeVec`;
- runtime validation or generation requires another minimum/direction owner beside binding-owned `PortFieldInfo`;
- metric registration can return an unregistered candidate after reporting success;
- any refresh path can leave a stale effective series while reporting observation available;
- OpenSpec archive preview removes or replaces any existing strict-grammar scenario;
- NATS API 10121 or 10082 remains wrapped as transient;
- a real-NATS mismatch test is proposed instead of deterministic validator coverage;
- labels expand beyond bounded component/port/stream/consumer/source identity;
- #309 queue/drop metrics, a generic nonzero default, or a second policy declaration enters scope;
- the breaking E2E paths cannot be made observable.

Owner approval is required for the recommended honor/reject split, both breaking classifications, the three metric
names, additive `PortFieldInfo.Minimum`, `RegisterOrGetGaugeVec`, all three exported signature changes, explicit
internal-consumer method, removal of `Client.ConsumeStream`, `PortConsumerContext`, OTEL direct observation seam, and
shared policy-error classifier. No design is approved by this handoff.

Technical writer: materialize the accepted inventory bytes first, then this design/proposal/spec/tasks text; record the
new artifact body hashes. Send the materialized design to the owner for binding review before developer handoff.
