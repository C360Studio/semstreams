# Model Registry

> Previously titled "Model Registry Runtime Updates". The runtime-updates
> content lives under [Runtime Updates](#runtime-updates) below. This page
> now also documents the registry's structure, every LLM workload it
> routes, and the known gaps where workloads bypass the registry.

The **model registry** is the single config block that names every LLM
and embedding endpoint a SemStreams deployment can reach, and binds each
LLM workload (called a **capability**) to a preferred endpoint. It lives
in NATS KV under the key `model_registry` (bucket `semstreams_config`)
and hot-reloads at runtime — components that depend on it auto-restart
when the key changes.

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

The registry defines five capability constants in `model/registry.go:11-25`.
Components resolve them via `model.ResolveEndpoint(registry, model.Capability*)`.

| Capability | Constant | Caller | Workload tier | Default route |
|---|---|---|---|---|
| `summarization` | `model.CapabilitySummarization` | agentic-loop context compaction (`processor/agentic-loop/component.go:184`) | User-facing, blocking — fires inline when a loop hits its token budget | Largest-context endpoint when no binding; ops/deep-research configs typically bind `"fast"` |
| `community_summary` | `model.CapabilityCommunitySummary` | graph-clustering enhancement worker (`processor/graph-clustering/component.go:1072`) | Background batch — async after community detection | `seminstruct` |
| `query_classification` | `model.CapabilityQueryClassification` | graph-query T3 classifier fallback (`processor/graph-query/component.go:372`) | User-facing, slow path — only fires when keyword/spatial/temporal classifiers fail | Unbound by default; degrades gracefully to keyword-only |
| `answer_synthesis` | `model.CapabilityAnswerSynthesis` | graph-query `globalSearch` answer composition (`processor/graph-query/component.go:406`) | User-facing, blocking | Unbound by default; falls back to template synthesis |
| `embedding` | `model.CapabilityEmbedding` | graph-embedding (`processor/graph-embedding/component.go:622`) | Background batch | `semembed` (HTTP embedder, **not** chat completions — different protocol) |

Per-capability **model selection guidance** (input/output envelope,
required model traits, latency budget, suggested probe prompts) lives
in [11-llm-routing-and-model-selection.md](11-llm-routing-and-model-selection.md).

### Known Gaps — workloads that bypass the registry

Three production LLM call sites are **not yet capability-bound**.
Operators reading the registry have no dedicated dial for them; they
inherit routing implicitly. A future cleanup pass will lift each into
its own capability constant. Document them here so the gap is visible.

| Site | File | Current routing | Why this matters |
|---|---|---|---|
| **Anomaly relationship review** | `graph/inference/review_worker.go:408` | Receives the `LLMClient` graph-clustering resolved via `community_summary`. No separate dial. | Anomaly review prompts are short and structured (APPROVE/REJECT). Community summarization prompts are open-ended narrative. They share an endpoint by accident, not design. |
| **Onboarding layer normalization** | `processor/agentic-dispatch/normalize_extractor.go:43` | Hardcoded `extractionModelName = "default"` — resolves to whatever endpoint `defaults.model` names. | Looks like `defaults.model` is the dial, but it actually serves multiple unrelated workloads. Editing `defaults.model` for one purpose silently affects this call. |
| **Intent classification** (every user message) | `processor/agentic-dispatch/intent_classifier.go:57` | `modelName` defaults to `"default"`; same fallback chain as above. | The most-frequent user-facing LLM call in the system. Fires on every user message with a 15s timeout. Operators must be able to bind it to a fast endpoint independently of the agentic chat itself. |

If you hit the limits of these implicit routes today (e.g. you want a
distinct fast endpoint for intent classification), the workaround is to
set `defaults.model` to the endpoint that should serve all three of
these calls collectively. There is no per-site override yet.

## Defaults

`defaults.model` is the endpoint used when a capability resolves nowhere
else (no preferred chain, no fallback). It is also the **silent backstop
for the three Known Gaps above** — operators should be aware that
changing `defaults.model` affects intent classification, layer
normalization, and the implicit anomaly-review path even though the
registry doesn't show those bindings explicitly.

`defaults.capability` is used when an `AgentRequest` arrives without a
specified `Model` or `Capability`. The named capability is resolved
through the standard chain.

## Runtime Updates

This section covers what happens when the `model_registry` KV key
changes, how it propagates to running components, and how external
library consumers can keep their own registry pointers in sync.

### The Three Audiences

There are three groups of code that care about the model registry, and
they each see updates a different way:

| Audience | Who | Update path |
|---|---|---|
| **Components in flow configs** | The `agentic-loop`, `agentic-model`, `agentic-dispatch`, `graph-query`, `graph-embedding` factories | Auto-restarted by ComponentManager when their factory declares `component.DepModelRegistry` |
| **External library consumers** | Code outside the semstreams runtime that imports `model.Registry` directly (e.g., a downstream service that wraps the dispatcher) | `model.Watch` + `cfgMgr.WatchModelRegistry()` |
| **Runtime-resolved callers** | Anything calling `RegistryReader.GetEndpoint(name)` per request | Just works — they read the latest state on the next call |

If you're writing a new component for a flow config, you don't need
this guide. Declare the dep on your registration and ComponentManager
handles the rest. See [agentic component patterns](../advanced/08-agentic-components.md).

### How an Update Propagates

```text
operator runs:                         config.Manager:                    ComponentManager:
nats kv put semstreams_config \        receives KV watcher event,         receives OnChange("model_registry"),
    model_registry @new.json   ──▶    parses + replaces internal     ──▶  iterates registered components,
                                       ModelRegistry, fires               restarts those declaring
                                       OnChange("model_registry")         component.DepModelRegistry
```

After the dust settles, registry-dependent components are running with
fresh `deps.ModelRegistry` references. Components that don't declare
the dep are untouched.

### External Consumers

If you hold your own `*model.Registry` outside the semstreams component
lifecycle (e.g., a sidecar process that runs its own dispatcher), wire
it like this:

```go
import (
    "context"
    "sync/atomic"

    "github.com/c360studio/semstreams/config"
    "github.com/c360studio/semstreams/model"
)

func wireRegistry(ctx context.Context, cfgMgr *config.Manager) *atomic.Pointer[model.Registry] {
    var holder atomic.Pointer[model.Registry]

    // Seed with the current registry so the first request doesn't race
    // the watcher.
    if initial := cfgMgr.GetConfig().Get().ModelRegistry; initial != nil {
        holder.Store(initial)
    }

    // Keep it fresh as KV changes.
    go model.Watch(ctx, cfgMgr, holder.Store)

    return &holder
}
```

Then any code that needs the latest registry just calls `holder.Load()`.

`config.Manager` satisfies the `model.Watcher` interface via its
`WatchModelRegistry` method, so you can pass it straight in. The watcher
channel coalesces — if your consumer is slow, you'll see the most recent
registry on your next read, not a backlog.

#### Why `atomic.Pointer`?

`model.Watch` is invoked from a goroutine. `holder.Store` from that
goroutine races with `holder.Load` from request handlers. Plain
assignment is a data race even on a single pointer field; the race
detector will flag it. `atomic.Pointer[Registry]` is one line and is
exactly what `model.Handle` was — the helper formalizes the pattern
without forcing it into the framework.

If you need the typed `RegistryReader` interface rather than a raw
`*Registry`, wrap a getter:

```go
func (h *atomic.Pointer[model.Registry]) Resolve(cap string) string {
    if r := h.Load(); r != nil {
        return r.Resolve(cap)
    }
    return ""
}
```

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

## Validation

`config.Manager` runs `model.Registry.Validate()` on every KV change.
Bad registries are rejected before any subscriber sees them — the live
state stays on the prior valid registry. Validate covers:

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

If you push a bad registry, `nats kv put` succeeds (NATS doesn't
validate JSON content) but the watcher logs a parse error and skips the
update. Inspect the semstreams logs for `Failed to update configuration`
to confirm.

## When to Use This vs. Component Restart

The component-restart path (auto, via `Registration.Dependencies`)
covers everything inside the framework. Reach for `model.Watch` only
when:

- Your code lives outside the semstreams component lifecycle
- You're embedding `model.Registry` in a library that runs alongside
  semstreams in the same process
- You're building tooling that needs to react to registry changes
  (audit log, metrics emitter, etc.) but isn't itself a component

If you find yourself reaching for `model.Watch` in code that could be
modeled as a flow component, prefer the component path — it's tested,
restart-safe, and free.

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
