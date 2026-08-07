# agentic-model

Model integration component for the agentic processing system.

## Overview

The `agentic-model` component routes agent requests to OpenAI-compatible LLM endpoints. It receives `AgentRequest` messages from the loop orchestrator, calls the appropriate model endpoint, and publishes `AgentResponse` messages back. Supports multiple named endpoints, tool calling, retry with backoff, and token tracking.

## Architecture

```
┌───────────────┐     ┌────────────────┐     ┌──────────────────┐
│ agentic-loop  │────►│ agentic-model  │────►│ LLM Endpoint     │
│               │     │                │     │ (OpenAI, Ollama) │
│               │◄────│                │◄────│                  │
└───────────────┘     └────────────────┘     └──────────────────┘
  agent.request.*       HTTP/HTTPS          OpenAI-compatible
  agent.response.*                          /v1/chat/completions
```

## Features

- **Multiple Endpoints**: Configure different models/providers by name
- **OpenAI Compatible**: Works with OpenAI, Ollama, LiteLLM, vLLM, etc.
- **Tool Support**: Full tool calling (function calling) support
- **Retry Logic**: Exponential backoff with jitter via `pkg/retry`
- **Rate Limiting**: Per-endpoint token bucket rate limiting and concurrency control
- **Endpoint Throttling**: Semaphore-based concurrency cap shared across all agents
- **Token Tracking**: Tracks prompt and completion tokens

## Configuration

```json
{
  "type": "processor",
  "name": "agentic-model",
  "enabled": true,
  "config": {
    "stream_name": "AGENT",
    "timeout": "120s",
    "retry": {
      "max_attempts": 3,
      "initial_delay": "1s",
      "max_delay": "60s",
      "rate_limit_delay": "5s"
    },
    "ports": {
      "inputs": [
        {"name":"requests","config":{"kind":"jetstream","subjects":["agent.request.>"],"stream_name":"AGENT"}}
      ],
      "outputs": [
        {"name":"responses","config":{"kind":"jetstream","subjects":["agent.response.*"],"stream_name":"AGENT"}}
      ]
    }
  }
}
```

Endpoint configurations including rate limits are defined in the top-level `model_registry` config block,
not inline in this component. See the [Model Registry](../../docs/advanced/08-agentic-components.md) section
for endpoint configuration.

### Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `timeout` | string | "120s" | Request timeout |
| `stream_name` | string | "AGENT" | JetStream stream name |
| `consumer_name_suffix` | string | "" | Suffix for consumer names (for testing) |
| `retry.max_attempts` | int | 3 | Maximum retry attempts |
| `retry.initial_delay` | string | "1s" | Initial delay before first retry |
| `retry.max_delay` | string | "60s" | Maximum delay between retries |
| `retry.rate_limit_delay` | string | "5s" | Extra wait added before backoff on HTTP 429 |
| `ports` | object | (defaults) | Port configuration |

### Endpoint Configuration

Endpoints are configured in the top-level `model_registry` block. In addition to the base fields, each
endpoint now supports rate limiting controls:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `url` | string | yes | Base URL for OpenAI-compatible API |
| `model` | string | yes | Model name for API requests |
| `api_key_env` | string | no | Environment variable for API key |
| `requests_per_minute` | int | no | Token bucket rate limit (0 = unlimited) |
| `max_concurrent` | int | no | Maximum simultaneous in-flight requests (0 = unlimited) |

### Provider-Specific Notes

**Ollama.** The `/v1/chat/completions` layer can't set context size per-request — long prompts silently truncate on the server unless the model is pre-built with a Modelfile. On first request to an Ollama endpoint, agentic-model probes `/api/show` and emits one WARN if the model's `num_ctx` is below `endpoint.max_tokens`. See [docs/operations/04-ollama-setup.md](../../docs/operations/04-ollama-setup.md) for the Modelfile recipe.

### Model Aliases

Model aliases provide semantic names for endpoints, allowing other components to reference models by purpose rather than specific endpoint:

```json
{
  "model_aliases": {
    "reasoning": "gpt-4",
    "coding": "gpt-4-turbo",
    "fast": "gpt-3.5-turbo",
    "summarization": "gpt-3.5-turbo"
  }
}
```

Alias rules:

- Target must exist in `endpoints`
- No alias chaining (alias cannot point to another alias)
- Empty target is not allowed

Requests can use either endpoint names or aliases:

```json
{
  "model": "fast"
}
```

Resolves to the `gpt-3.5-turbo` endpoint.

## Ports

### Inputs

| Name | Type | Subject | Description |
|------|------|---------|-------------|
| requests | jetstream | agent.request.> | Agent requests from agentic-loop |

### Outputs

| Name | Type | Subject | Description |
|------|------|---------|-------------|
| responses | jetstream | agent.response.* | Model responses to agentic-loop |

## Endpoint Resolution

Requests are routed to endpoints by model name:

1. Exact match: Request `model: "gpt-4"` routes to `endpoints.gpt-4`
2. Alias match: Request `model: "fast"` routes to `model_aliases.fast` target
3. Default fallback: If no match, routes to `endpoints.default` (if configured)
4. Error: If no match and no default, returns error response

## Compatible Providers

| Provider | URL Format | Notes |
|----------|------------|-------|
| OpenAI | `https://api.openai.com/v1/chat/completions` | Requires API key |
| Azure OpenAI | `https://{resource}.openai.azure.com/...` | Requires API key |
| Ollama | `http://localhost:11434/v1/chat/completions` | No auth required |
| LiteLLM | `http://localhost:8000/v1/chat/completions` | Proxy for multiple providers |
| vLLM | `http://localhost:8000/v1/chat/completions` | Self-hosted models |
| LocalAI | `http://localhost:8080/v1/chat/completions` | Local models |

## Response Status Mapping

| OpenAI Finish Reason | Agentic Status | Description |
|---------------------|----------------|-------------|
| `stop` | `complete` | Normal completion |
| `length` | `complete` | Max tokens reached |
| `tool_calls` | `tool_call` | Model wants to use tools |
| (error) | `error` | API or network error |

## Environment Variables

API keys are read from environment variables:

```bash
export OPENAI_API_KEY="sk-..."
export ANTHROPIC_API_KEY="..."
```

Reference in config:

```json
{
  "api_key_env": "OPENAI_API_KEY"
}
```

## Retry Behavior

Retries are implemented using `pkg/retry` with exponential backoff and jitter.

- **Max Attempts**: Default 3, configurable via `retry.max_attempts`
- **Initial Delay**: Default 1s (`retry.initial_delay`); tests use 100ms for speed
- **Max Delay**: Default 60s (`retry.max_delay`); each interval doubles with jitter
- **HTTP 429 Handling**: Detected via SDK error types (`openai.APIError.HTTPStatusCode`,
  `openai.RequestError.HTTPStatusCode`). An extra `rate_limit_delay` (default 5s) is added before
  the normal backoff begins, giving the provider time to recover
- **Retryable**: HTTP 429, 500, 502, 503, 504, and network errors
- **Non-retryable**: HTTP 400, 401, 403, 404, and context cancellation

## Rate Limiting

When running agent teams or quests, multiple agents concurrently target the same endpoint. Without
coordination, they can saturate the provider's rate limit within seconds, causing cascading 429 errors
that waste time in retry loops. Per-endpoint throttling solves this by enforcing limits before requests
leave the process.

Each endpoint in the model registry can be configured with two complementary controls:

- **`requests_per_minute`**: A token bucket that caps the request rate. Agents block until a token is
  available rather than racing to the endpoint.
- **`max_concurrent`**: A semaphore that caps simultaneous in-flight requests. Useful for providers that
  enforce concurrency limits independently of rate limits.

Both controls are shared across all agents targeting the same endpoint — the throttle is instantiated
once per cached client, not per agent.

### Configuration Example

```json
{
  "model_registry": {
    "endpoints": {
      "gpt-4": {
        "url": "https://api.openai.com/v1/chat/completions",
        "model": "gpt-4-turbo-preview",
        "api_key_env": "OPENAI_API_KEY",
        "requests_per_minute": 60,
        "max_concurrent": 5
      },
      "ollama": {
        "url": "http://localhost:11434/v1/chat/completions",
        "model": "qwen2.5-coder:14b"
      }
    }
  }
}
```

The `ollama` endpoint has no limits configured — local models typically do not need them.

### When to Use

| Scenario | Recommended Setting |
|----------|---------------------|
| Single agent, low traffic | No limits needed |
| Agent team (quest), shared OpenAI key | `requests_per_minute` matching your tier |
| Shared API key across multiple services | Both `requests_per_minute` and `max_concurrent` |
| Local model (Ollama, vLLM) | No limits needed |

### Observability

Rate limit events are tracked via:

```
semstreams_agentic_model_rate_limit_hits_total{model="gpt-4"}
```

This counter increments each time a request encounters an HTTP 429. A sustained high rate suggests
the configured `requests_per_minute` is too close to the actual provider limit and should be reduced.

## Troubleshooting

### Endpoint not found

- Verify model name in request matches an endpoint key
- Add a "default" endpoint as fallback
- Check for typos in model names

### Authentication errors

- Verify `api_key_env` points to correct environment variable
- Check that environment variable is set
- Ensure API key is valid and has quota

### Timeout errors

- Increase `timeout` for complex requests
- Check network connectivity to endpoint
- Verify endpoint URL is correct

### Tool calls not working

- Ensure tools are provided in AgentRequest
- Check tool parameter schemas are valid JSON Schema
- Verify model supports tool calling

## Related Components

- [agentic-loop](../agentic-loop/) - Loop orchestration
- [agentic-tools](../agentic-tools/) - Tool execution
- [agentic types](../../agentic/) - Shared type definitions
