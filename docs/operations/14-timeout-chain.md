# Timeout Chain

A semstreams deployment has six layers of timeout knobs. They are not
independent — every layer carries an implicit contract with its
neighbours, and tuning one without the others produces silent
redelivery, premature 504s, or paid-LLM duplicate billing. This page
documents the chain, the two operational postures it supports, and the
recipe for tuning each layer in lockstep.

## The chain

```
┌─ NATS JetStream consumer (per port — JetStreamPort) ─────────────────┐
│  ack_wait              // server reaps unacked work after this        │
│  heartbeat_interval    // client InProgress() interval — at most half │
│                        // the shortest BackOff, otherwise half AckWait │
│  max_deliver           // cap on redelivery attempts                  │
│  deliver_policy        // "new" / "all" / "last"                      │
│  ack_policy            // "explicit" / "none" / "all"                 │
│  max_ack_pending       // cap delivered-but-unacked work              │
└──────────────────────────────────────────────────────────────────────┘
            ↓ work goroutine receives workCtx (cancel on heartbeat fail)
┌─ Per-task LLM call wallclock budget ─────────────────────────────────┐
│  precedence (high → low):                                             │
│   1. req.Timeout                  // per-task TaskMessage override    │
│   2. endpoint.RequestTimeout      // per-endpoint config              │
│   3. capability.Timeout           // per-capability config            │
│   4. component.messageTimeout     // cached from agentic-model Config │
│   5. 110s hardcoded fallback      // STRICTLY LESS THAN ack_wait      │
│  Wraps the SDK call via context.WithTimeout(ctx, resolved).           │
└──────────────────────────────────────────────────────────────────────┘
            ↓
┌─ Model HTTP client (model.NewHTTPClient — beta.43 invariant) ────────┐
│  http.Client.Timeout=0          // context-driven, no whole-request   │
│                                 // ceiling beyond ctx.Done            │
│  IdleConnTimeout=30s            // shed pooled conns before upstream  │
│                                 // LB drops them                      │
│  MaxIdleConnsPerHost=1                                                │
│  HTTP/2 ReadIdleTimeout=15s     // PINGs before peer close            │
│  HTTP/2 PingTimeout=5s                                                │
│  EndpointConfig fields override: IdleConnTimeout,                     │
│                                  ResponseHeaderTimeout,               │
│                                  DisableKeepAlives                    │
└──────────────────────────────────────────────────────────────────────┘
            ↓
┌─ Capability-scoped inner bound (graph-query, graph-clustering, etc.) ┐
│  community_summary             default 60s                            │
│  anomaly_review                default 60s                            │
│  answer_synthesis              default 15s   (ADR-024)                │
│  query_classification          default  5s   (ADR-024)                │
│  intent_classification         default 15s                            │
│  embedding                     default 30s                            │
│  All resolved via model.ResolveCapabilityTimeout — endpoint           │
│  override > capability config > caller-supplied default.              │
└──────────────────────────────────────────────────────────────────────┘
            ↑
┌─ Service HTTP front door (service/service_manager.go) ───────────────┐
│  http_read_timeout=30s          // body read budget                   │
│  http_write_timeout=120s        // beta.49 fix; LLM-aware default     │
│  http_idle_timeout=60s                                                │
└──────────────────────────────────────────────────────────────────────┘
            ↓ proxies to graph-gateway
┌─ graph-gateway query path ───────────────────────────────────────────┐
│  query_timeout=60s              // wraps the GraphQL/MCP/HTTP handler │
│  StandaloneServer (when used):                                        │
│    read=30s, write=60s, idle=120s                                     │
└──────────────────────────────────────────────────────────────────────┘
```

## The contract — inequalities that must hold

Every adjacent pair has an inequality the operator must preserve when
tuning. Violating any of them produces a class of bugs the framework
has hit at least once.

| # | Constraint | Why |
|---|---|---|
| 1 | `heartbeat ≤ effective acknowledgement wait ÷ 2` | Setup validates the exact consumer configuration before allocation. `BackOff` overrides `AckWait`, so its shortest positive interval is effective; otherwise `AckWait` is effective. The loop defaults to 15s against `[30s,2m]`; agentic-model defaults to 60s against 120s. |
| 2 | `ack_wait > req.Timeout` (or component fallback) | The LLM call must cancel via context.Done **before** NATS reaps the work. Heartbeats save you in the steady state; the wallclock budget guarantees clean cancel even when heartbeats stop firing. **The 10s gap between agentic-model's 120s ack_wait and the 110s component fallback is load-bearing.** |
| 3 | `service.http_write_timeout > worst-case LLM wallclock` | Mid-write EOFs killed every LLM-backed handler before beta.49. 120s default covers synthesis (15s) + classification (5s) + headroom; raise it if you raise capability budgets. |
| 4 | `gateway.query_timeout > inner LLM bounded budget` | If the gateway gives up before the synthesizer's bounded sub-call returns, the client gets a 504 even though the framework's degraded fallback worked. Default 60s; bumping any synthesis/classification budget past 50s combined requires bumping this in lockstep. |
| 5 | `model.IdleConnTimeout < upstream LB idle timeout` | Stale pooled connections EOF on next reuse. Beta.43 set a conservative 30s under most LB defaults (60–90s). |
| 6 | `capability.Timeout ≤ req.Timeout` (when both set) | The inner bound binds first; setting capability.Timeout above the per-task budget wastes a layer of the chain. Default capability values are well below the 110s task fallback, so this holds out of the box. |

## Two operational postures

The framework defaults serve **forgiving redelivery** — transient
hiccups don't permanently fail tasks. For paid-LLM-with-strict-cost
deployments, **fail-loud-once** is preferred. The choice has
config-shape implications across the chain, not just one knob.

### Posture A — forgiving (default, framework out-of-the-box)

```yaml
# agentic-loop consumer (agent.task / response / tool.result)
ack_wait: 90s
max_deliver: 2
max_ack_pending: 1
heartbeat_interval: 15s
# component-owned BackOff: [30s, 2m]

# agentic-model
timeout: 110s          # framework default

# agentic-model + agentic-tools per-port consumer config
# ports[].config.ack_wait, .heartbeat_interval, .max_deliver, .deliver_policy,
# .ack_policy. When unset on the port, components apply their declared
# defaults (agentic-model 120s/60s/3, agentic-tools 5m/5s/3).
```

A transient consumer hiccup that exceeds the effective acknowledgement interval results in redelivery. The
`HandleTask` short circuit (`processor/agentic-loop/handlers.go`) deduplicates an already-created loop, but task
deduplication alone is not proof that a separately delivered model request avoided a second provider invocation.
Provider replay follows the model settlement and ambiguity policy rather than inferring safety from loop memory.

### Posture B — fail-loud-once (cost-sensitive paid LLM)

```yaml
# agentic-loop consumer
ack_wait: 300s          # retained setting; loop BackOff still controls the effective lease intervals
max_deliver: 1          # no redelivery, ever
max_ack_pending: 1
heartbeat_interval: 15s # validated against component-owned [30s, 2m] BackOff

# agentic-model
timeout: 270s           # strictly less than ack_wait, leaves 30s for
                        # cancellation propagation + failure publish + KV write
# agent.request port — port-level consumer config (preferred surface)
ports:
  inputs:
    - name: agent.request
      config:
        kind: jetstream
        subjects: ["agent.request.>"]
        ack_wait: 300s
        heartbeat_interval: 60s
        max_deliver: 1

# Required downstream contract
# - Wire a DLQ or surface failures via a dedicated agent.failed.* counter;
#   max_deliver: 1 means a Nak'd message is gone forever from the consumer.
# - Prefer streaming endpoints (endpoint.stream: true) so cancel-mid-flight
#   saves provider tokens past the cancel point.
```

The tradeoff: any consumer-side network blip or restart that exceeds
`ack_wait` becomes a permanent task failure. For paid LLMs with
$0.X/1K-token charges, that's the right tradeoff. For local llama.cpp
or hobbyist deployments, posture A is cheaper.

## What happens when a heartbeat fails

The cancellation path is end-to-end and verified in code:

1. The model binding enters `ConsumeDeliveryWithHeartbeat`, which derives a cancellable work context.
2. Ticker fires `msg.InProgress()` every `heartbeat` interval.
3. On InProgress() failure (NATS connection lost, server flap),
   the work context is cancelled and the exact consumer owner is drained.
4. `workCtx` cancels.
5. The work goroutine's `agenticmodel.Client.ChatCompletion(ctx, ...)`
   sees ctx.Done.
6. Calls `c.client.CreateChatCompletion(ctx, ...)` — go-openai SDK.
7. SDK builds via `http.NewRequestWithContext(ctx, ...)` — verified at
   `internal/request_builder.go:44`.
8. Go's `http.Transport` aborts the in-flight request, closes the TCP
   connection (RST if data was in flight, FIN otherwise).

The delivery is quarantined without terminal settlement because ownership safety is no longer known. New delivery
admission remains closed for that exact owner until the component is restarted.

The provider sees a closed connection. **Whether the provider stops
billing depends on the provider's policy** — not something semstreams
controls:

| Provider | Mid-stream cancel | Mid-non-streaming cancel |
|---|---|---|
| OpenAI direct | Bills only tokens streamed | Bills inferred response if generation completed |
| Anthropic | Bills only tokens streamed | Same |
| OpenRouter | Forwards cancel; depends on underlying provider | Same |
| vLLM / sparky / llama.cpp | Inference typically halts on TCP close | Same |

**Streaming is the cost-sensitive lever** orthogonal to redelivery —
even with perfect cancellation, a non-streaming cancel may bill for
the full response the upstream finished generating.

## Consumer tuning ownership

Ordinary port-backed inputs honor per-port consumer configuration. The agentic-loop, agentic-model, and agentic-tools
components own fixed acknowledgement-admission values (1/10, 1, and 3 respectively) and reject nonzero
`max_ack_pending` declarations. Zero leaves those component policies intact. This makes ownership explicit instead of
accepting a knob that cannot take effect.

### Per-port `JetStreamPort`

`AckWait`, `HeartbeatInterval`, `MaxDeliver`, `DeliverPolicy`,
`AckPolicy` live on the port struct. Operators tune honored fields per-port
in component config:

```yaml
ports:
  inputs:
    - name: agent.request
      config:
        kind: jetstream
        subjects: ["agent.request.>"]
        ack_wait: 300s             # parsed via time.ParseDuration
        heartbeat_interval: 60s
        max_deliver: 1
        deliver_policy: new
        ack_policy: explicit
```

`component.GetConsumerConfig(port)` extracts the parsed values from canonical
facts. Empty strings leave consumer-local defaults unset; malformed or
non-positive durations fail port resolution and block startup rather than
silently changing redelivery behavior.

## Default values reference

For the actual current values across the codebase, the audit table in
the [beta.53 release notes][beta53-tag] is the canonical snapshot.
Out-of-tree config in `configs/*.json` overrides framework defaults
where present.

[beta53-tag]: https://github.com/C360Studio/semstreams/releases/tag/v1.0.0-beta.53

## Related ADRs and concepts

- ADR-024 (`docs/adr/024-layered-llm-timeouts.md`) — graph-query
  bounded sub-timeouts (synthesis 15s, classification 5s).
- ADR-033 (`docs/adr/033-operating-curve-based-observability.md`) —
  end-to-end latency observability where cap-budgets meet the
  operating-curve framework.
- `docs/concepts/03-streams-vs-kv-watches.md` — the facts-vs-requests
  decision that determines which JetStream knobs apply.
- `docs/operations/12-openai-client-keepalive.md` — the keepalive saga
  beta.34→43 that produced the unified-HTTP-client invariant.
