# Model Registry

The **model registry** is the single config block that names every LLM
and embedding endpoint a SemStreams deployment can reach, and binds each
LLM workload (called a **capability**) to a preferred endpoint. It lives
in NATS KV under the key `model_registry` (bucket `semstreams_config`).
Edits are durable desired configuration selected on the next successful
process boot; running components are not restarted or reconfigured in place.

The registry replaced per-component config fields like
`graph-clustering.llm_endpoint`, `graph-clustering.llm_model`, and
`graph-embedding.embedder_url` in alpha.29. See
[migration-alpha29.md](migration-alpha29.md) for the original migration.

## Minimal Complete Example

```json
{
  "model_registry": {
    "endpoints": {
      "seminstruct": {
        "provider": "openai",
        "url": "http://seminstruct:8083/v1",
        "model": "qwen2.5-0.5b-instruct-q4-k-m",
        "max_tokens": 4096
      },
      "semembed": {
        "provider": "openai",
        "url": "http://semembed:8081/v1",
        "model": "all-MiniLM-L6-v2",
        "max_tokens": 0
      }
    },
    "capabilities": {
      "community_summary": { "preferred": ["seminstruct"] },
      "embedding":         { "preferred": ["semembed"] }
    },
    "defaults": {
      "model": "seminstruct"
    }
  }
}
```

This is the actual `configs/semantic.json` shape. Two endpoints, two
capabilities bound to them, one default endpoint for unbound resolutions.

## Structure

The registry has three top-level blocks under `model_registry`:

| Block | Purpose |
|---|---|
| `endpoints` | Map of endpoint name → connection details. The endpoint name is the operator-chosen handle used in capability bindings. |
| `capabilities` | Map of capability name → routing config (`preferred` chain, `fallback` chain, `requires_tools` filter, per-capability `timeout`). |
| `defaults` | Fallback endpoint and capability used when a caller's request resolves to no specific binding. |

### EndpointConfig fields

Defined in `model/registry.go:54-122`. Every field is optional except
`model`.

| Field | Type | Purpose |
|---|---|---|
| `provider` | string | One of `anthropic`, `ollama`, `openai`, `openrouter`. Determines which provider adapter wraps requests. See [provider-adapter-normalization](10-provider-adapter-normalization.md). |
| `url` | string | API endpoint URL. Required for `ollama`/`openai`/`openrouter`. Optional for `anthropic` (defaults to public API). |
| `model` | string | **Required.** Model identifier sent to the provider. |
| `max_tokens` | int | **Context window size.** For ollama, this is informational (Ollama's `num_ctx` is set in the Modelfile, not per-request). For other providers it informs summarization routing and context budgeting in agentic-loop. |
| `max_output_tokens` | int | **Per-request output cap** (added beta.30). Forwarded as the OpenAI `max_tokens` field when `AgentRequest.MaxTokens` is unset. Closes the deterministic-16384-truncation gap on gateway proxies that apply low implicit caps. See [migration-beta29-to-beta30.md](migration-beta29-to-beta30.md). Distinct from `max_tokens` above (which is the context window). |
| `supports_tools` | bool | Whether this endpoint supports OpenAI-style function/tool calling. Capabilities with `requires_tools: true` only resolve to endpoints where this is set. |
| `tool_format` | string | `anthropic` or `openai`. Empty auto-detects from provider. |
| `api_key_env` | string | Name of the env var holding the API key. Required for anthropic/openai/openrouter. Ignored for ollama. |
| `options` | map | Provider-specific template parameters passed as `chat_template_kwargs`. For vLLM/SGLang thinking models: `enable_thinking`, `thinking_budget`. Ollama's OpenAI-compatible endpoint ignores this. |
| `stream` | bool | Enables SSE streaming. Reduces time-to-first-token; the inter-component protocol still emits complete `AgentResponse` messages. |
| `reasoning_effort` | string | `none`/`low`/`medium`/`high` (provider-default if empty). Forwarded as `reasoning_effort` on OpenAI-compatible requests for reasoning models. |
| `request_timeout` | duration string | Per-endpoint cap on a single LLM request (e.g. `"45s"`). See [ADR-024](../adr/024-layered-llm-timeouts.md). |
| `requests_per_minute` | int | Rate limit. 0 means no limit. Applied per-endpoint across all consumers. |
| `max_concurrent` | int | Concurrency limit. 0 means no limit. |
| `input_price_per_1m_tokens` | float | Cost per 1M input tokens, USD. Joined with usage data for cost calculation. |
| `output_price_per_1m_tokens` | float | Cost per 1M output tokens, USD. |

### CapabilityConfig fields

Defined in `model/registry.go:124-140`.

| Field | Type | Purpose |
|---|---|---|
| `description` | string | Free-form documentation of what this capability is for. |
| `preferred` | []string | **Required.** Endpoint names in preference order. The first reachable one wins. |
| `fallback` | []string | Backup endpoint names if all `preferred` are unavailable (circuit-broken or unconfigured). |
| `requires_tools` | bool | If true, only endpoints with `supports_tools: true` are eligible. |
| `timeout` | duration string | Per-capability cap on a single LLM request, applied between endpoint and component-level timeouts in the precedence chain. See [ADR-024](../adr/024-layered-llm-timeouts.md). |

## LLM Capability Inventory

The registry defines seven capability constants in `model/registry.go:11-37`.
Components resolve them via `model.ResolveEndpoint(registry, model.Capability*)`
or, where the call site has its own resolution chain, by passing the
capability constant as the resolution key.

| Capability | Constant | Caller | Workload tier | Default route when unbound |
|---|---|---|---|---|
| `summarization` | `model.CapabilitySummarization` | agentic-loop context compaction (`processor/agentic-loop/component.go:184`) | User-facing, blocking — fires inline when a loop hits its token budget | Largest-context endpoint when no binding; ops/deep-research configs typically bind `"fast"` |
| `community_summary` | `model.CapabilityCommunitySummary` | graph-clustering enhancement worker (`processor/graph-clustering/component.go:1072`) | Background batch — async after community detection | `seminstruct` |
| `query_classification` | `model.CapabilityQueryClassification` | graph-query T3 classifier fallback (`processor/graph-query/component.go:372`) | User-facing, slow path — only fires when keyword/spatial/temporal classifiers fail | Unbound by default; degrades gracefully to keyword-only |
| `answer_synthesis` | `model.CapabilityAnswerSynthesis` | graph-query `globalSearch` answer composition (`processor/graph-query/component.go:406`) | User-facing, blocking | Unbound by default; falls back to template synthesis |
| `embedding` | `model.CapabilityEmbedding` | graph-embedding (`processor/graph-embedding/component.go:622`) | Background batch | `semembed` (HTTP embedder, **not** chat completions — different protocol) |
| `intent_classification` | `model.CapabilityIntentClassification` | agentic-dispatch (`processor/agentic-dispatch/intent_classifier.go`) — fires on every incoming user message | User-facing, blocking — most-frequent user-facing LLM call in the system | Falls through to `defaults.model` |
| `anomaly_review` | `model.CapabilityAnomalyReview` | graph-inference ReviewWorker (`graph/inference/review_worker.go`) — classifies suggested missing relationships | Background batch | Falls through to the `community_summary` endpoint (legacy piggyback preserved) |

Per-capability **model selection guidance** (input/output envelope,
required model traits, latency budget, suggested probe prompts) lives
in [11-llm-routing-and-model-selection.md](11-llm-routing-and-model-selection.md).

### Resolution semantics for the last two capabilities

`intent_classification` and `anomaly_review` were lifted into
capabilities in Phase 2 from previously hardcoded call sites. To keep
upgrades zero-ceremony, each has slightly different fallback semantics:

- **`intent_classification`** — when the capability is not bound, the
  call-site's resolution chain falls through to `defaults.model`. This
  is the same endpoint these calls used pre-Phase-2 via the hardcoded
  `"default"` string. Operators who previously tuned `defaults.model`
  for this workload see no change; binding the capability gives them a
  per-site override.
- **`anomaly_review`** — when the capability is not bound, the review
  worker reuses graph-clustering's `community_summary` LLM client, the
  same endpoint it piggybacked on pre-Phase-2. Operators who want a
  separate model for review (e.g. a smaller, faster classifier) bind
  `anomaly_review` explicitly.

In both cases, **doing nothing on upgrade preserves prior behavior
bit-for-bit.** Binding the new capability is opt-in.

## Defaults

`defaults.model` is the endpoint used when a capability resolves nowhere
else (no preferred chain, no fallback). For the `intent_classification`
capability, `defaults.model` is the fallback target when the capability
is not bound — preserving the pre-Phase-2 hardcoded behavior. Operators
who want per-site routing for that workload should bind the capability
explicitly rather than reshaping `defaults.model`.

`defaults.capability` is used when an `AgentRequest` arrives without a
specified `Model` or `Capability`. The named capability is resolved
through the standard chain.

## Activation boundary

The composition root selects one model registry after file/KV version
arbitration and passes that value to every component created for the boot.
Post-boot writes update desired state only. They do not replace registry
pointers or restart dependent components. External consumers should receive
the selected registry through their own boot composition rather than watch
SemStreams' operational KV.

### Updating the KV Key from Operations

```bash
# Read current registry
nats kv get semstreams_config model_registry

# Update from a JSON file
nats kv put semstreams_config model_registry "$(cat new-registry.json)"

# Roll back to a prior revision
nats kv get --history semstreams_config model_registry
nats kv put semstreams_config model_registry "$(nats kv get --raw semstreams_config model_registry --revision N)"
```

The KV bucket keeps the last 5 revisions (`semstreams_config` History=5).
Restart SemStreams after committing the desired revision.

## Validation

Boot configuration validation runs `model.Registry.Validate()` before the
registry is selected for component construction. Validate covers:

- **Endpoint name** (the map key): non-empty, alphanumeric + `-_` only
- **Endpoint config**: `model` required; `max_tokens` non-negative;
  `max_output_tokens` non-negative (beta.30); `provider` in
  `{anthropic, ollama, openai, openrouter}`; `tool_format` in
  `{anthropic, openai}`; `reasoning_effort` in
  `{none, low, medium, high}`; prices non-negative
- **Capabilities**: reference real endpoints; `requires_tools`
  capabilities must have at least one tool-capable endpoint in their
  chain
- **Defaults**: `defaults.model` and `defaults.capability` reference
  real entries

If you push a bad registry, `nats kv put` succeeds because NATS does not
validate JSON content. The next boot fails validation rather than applying a
partial or invalid registry.

## Operational rule

Treat `model_registry` like every other component dependency: author it as
desired state, validate it, and restart the process to activate it. Dedicated
rule-definition hot reload is the sole live configuration exception.

## Related

- [11 — LLM Routing and Model Selection](11-llm-routing-and-model-selection.md)
  — per-capability spec sheets and small-model selection guidance for
  operators picking endpoint bindings
- [Agentic Component Patterns](../advanced/08-agentic-components.md) —
  how registry-dependent components declare and consume the dep
- [ADR-024 Layered LLM Timeouts](../adr/024-layered-llm-timeouts.md) —
  per-endpoint and per-capability timeout config in the same key
- [ADR-023 Provider Adapters and Tool Choice](../adr/023-provider-adapters-and-tool-choice.md)
  — how `provider` selection drives request normalization
- [migration-alpha29.md](migration-alpha29.md) — the migration that
  introduced the centralized model registry, replacing per-component
  `llm_endpoint`/`llm_model`/`embedder_url` fields
- [migration-beta29-to-beta30.md](migration-beta29-to-beta30.md) —
  added `max_output_tokens` to close the 16384-truncation gap on
  gateway proxies
- [06 — Endpoint Health and Circuit Breaker](06-endpoint-health-circuit-breaker.md)
  — how the registry's resolution chain interacts with endpoint health
- [08 — LLM Truncation Handling](08-llm-truncation-handling.md) —
  context utilization branching when `finish_reason=length` fires
