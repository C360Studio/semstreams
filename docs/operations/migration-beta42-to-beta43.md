# Migration Guide: beta.42 → beta.43

## Summary

Beta.43 is the **architectural fix** for the long tail of stale-pooled-
connection wedges shipped through beta.34–42 as one-off per-endpoint
opt-ins. Three changes converge the framework on a single canonical
LLM-class HTTP client builder with safer-against-local-LLMs defaults:

1. **Tightened framework defaults in `model.NewHTTPClient`** —
   `IdleConnTimeout: 30s` (was 90s), `MaxIdleConnsPerHost: 1` (was 2),
   plus HTTP/2 PING-based liveness checks for HTTP/2-supporting
   upstreams (cloud providers, Cloudflare-fronted gateways).
2. **`graph/embedding/http_embedder.go` routes through `model.NewHTTPClient`** —
   was previously building a raw `&http.Client{Timeout: ...}`. Closes
   the third LLM-class client onto the canonical builder.
3. **Architectural invariant documented** — every LLM-class HTTP
   client in the framework MUST route through `model.NewHTTPClient`.
   The builder now owns all defaults; per-endpoint config is a knob
   for unusual cases, not a workaround for systemic class-of-bugs.

| Surface | Status |
|---|---|
| `FrameworkDefaultIdleConnTimeout = 30 * time.Second` | **Behavioural — was 90s (Go default)** |
| `FrameworkDefaultMaxIdleConnsPerHost = 1` | **Behavioural — was 2 (Go default)** |
| HTTP/2 PINGs (`ReadIdleTimeout: 15s`, `PingTimeout: 5s`) | **Additive — only fires on h2 connections** |
| `graph/embedding/HTTPConfig` gains `IdleConnTimeout` / `ResponseHeaderTimeout` / `DisableKeepAlives` fields | **Additive** — empty/false selects framework default |
| `graph/embedding` HTTP client now builds via `model.NewHTTPClient` | **Behavioural — was raw `&http.Client{}`** |
| Operator-configured per-endpoint values | **Unchanged** — explicit overrides still win |

**The simplest beta.42 → beta.43 upgrade is to do nothing.** Existing
deployments inherit the tighter defaults automatically. Operators who
need the legacy 90s pool window opt out per-endpoint via
`endpoint.idle_conn_timeout: "90s"`.

## Why this tag exists

The long tail of beta.34 → beta.42 keepalive bugs all had the same
root cause: Go's `http.DefaultTransport` defaults are tuned for
industrial-grade cloud servers, not for the LLM-gateway class. Local
inference servers (Ollama, llama.cpp, vLLM, sparky) and some cloud
gateways idle-kill connections aggressively and often silently. The
90s `IdleConnTimeout` default and 2-conn idle pool meant the framework
could hand out a stale pooled connection that the upstream had already
dropped — Read on the dead socket would block until the request-level
ctx fired (typically minutes).

Beta.34 added per-endpoint `DisableKeepAlives` / `IdleConnTimeout` /
`ResponseHeaderTimeout` knobs, defaulted to "preserve Go's defaults."
Every operator had to opt in per endpoint, AND had to know they
needed to. Five tags later, semspec/semteams were still hitting the
wedge class.

Beta.43 inverts the default. The framework now owns sensible defaults
for the LLM-gateway class; operators who want the legacy permissive
behaviour explicitly opt out.

## What's new

### Tightened framework defaults

`model.NewHTTPClient` now applies framework-default Transport tuning
when no operator value is supplied:

| Field | Old (Go default) | New (framework default) | Why |
|---|---|---|---|
| `IdleConnTimeout` | 90s | **30s** | Bounds stale-pool window against keepalive-hostile gateways |
| `MaxIdleConnsPerHost` | 2 (implicit) | **1** | Single dead conn can't sit alongside a healthy one waiting for LRU |
| HTTP/2 `ReadIdleTimeout` | 0 (off) | **15s** | PING-based liveness for h2 connections |
| HTTP/2 `PingTimeout` | 0 (off) | **5s** | Per-PING reply deadline |
| `ResponseHeaderTimeout` | 0 (off) | **0 (off)** | Unchanged — slow non-streaming models legitimately take minutes |
| `DisableKeepAlives` | false | **false** | Unchanged — only set when explicitly opted in |

HTTP/2 PINGs are the most operator-invisible improvement:
HTTP/2-supporting upstreams (OpenAI, Anthropic, Cloudflare-fronted
OpenRouter routes) get dead-conn detection at the protocol level
without the operator having to disable keepalive entirely.
HTTP/1.1-only upstreams (Ollama, llama.cpp HTTP) are unaffected
by the PING work; they fall back to the tightened
`IdleConnTimeout` default (30s).

### `graph/embedding` converges

`graph/embedding/http_embedder.go` previously built its own raw
`&http.Client{Timeout: timeout}` — bypassing the framework defaults
entirely. The third LLM-class client (alongside `agentic-model` and
`graph/llm`) now routes through `model.NewHTTPClient`. The
`HTTPConfig` gained the same three operator-tunable fields its
sibling configs already had.

### Architectural invariant

Going forward: **every LLM-class HTTP client in the framework
MUST route through `model.NewHTTPClient`.** Direct
`&http.Client{...}` construction is acceptable only for non-LLM
HTTP traffic (agentic-tools' web_search, github_client; output's
httppost, otel exporter; the sandbox client). Code review should
flag new LLM-client construction sites that bypass the canonical
builder.

This invariant is what "one client builder" actually means — a
single place where defaults change, a single audit when behaviour
needs to evolve, no per-package divergence.

## Performance impact

For cloud providers (OpenAI, Anthropic, Cloudflare-fronted):
**negligible**. TLS session resumption + HTTP/2 multiplexing absorb
the slightly more frequent reconnects. HTTP/2 PINGs add ~10-50 byte
frames every 15s of idleness — wire cost is in the noise.

For local inference servers (Ollama, llama.cpp, vLLM, sparky):
**positive**. The 30s `IdleConnTimeout` default sidesteps the
silent-FIN wedge class for typical agent flows where requests
are seconds apart. The 1-conn idle pool prevents wedge accumulation
under sustained load.

For long-idle workloads (e.g., a graph-clustering job that does an
embedding call every 5 minutes): operators may see slightly more
TCP setup cost. If that's measurable, set
`endpoint.idle_conn_timeout: "300s"` to opt out.

## Migration steps

### Existing operators

No required action. Watch for log-line uptick on
"transient error, retrying" — if you see >5x the prior baseline,
your specific endpoint may benefit from
`endpoint.idle_conn_timeout: "60s"` or higher.

### `graph/embedding` consumers

If you construct `embedding.HTTPConfig` directly (rare — most
deployments configure via the model registry), no change required.
Three new optional fields (`IdleConnTimeout`, `ResponseHeaderTimeout`,
`DisableKeepAlives`) match the equivalent fields on `model.EndpointConfig`
and `graph/llm.OpenAIConfig`. Empty/false selects the framework
default.

## Backward compatibility

- `model.NewHTTPClient` signature: unchanged.
- `model.HTTPClientOptions` shape: unchanged. Operator-supplied values
  still override framework defaults exactly as before.
- `model.HTTPClientOptionsFromEndpoint`: unchanged.
- `graph/embedding.HTTPConfig`: additive (three new optional fields).
- Per-endpoint `EndpointConfig.IdleConnTimeout` etc.: unchanged.
- Operators who previously set `IdleConnTimeout: "30s"` to work around
  the 90s default can now leave the field unset — the framework
  default is now what they were configuring.

## Cross-references

- `model/httpclient.go` — `FrameworkDefault*` constants and the
  unified builder
- `model/httpclient_test.go` — pinned regression tests for the new
  defaults
- `graph/embedding/http_embedder.go` — converged onto
  `model.NewHTTPClient`
- `processor/agentic-model/client.go:NewClient` — already routes
  through `model.NewHTTPClient` (beta.34)
- `graph/llm/openai_client.go:NewOpenAIClient` — already routes
  through `model.NewHTTPClient` (beta.34)
- `docs/operations/12-openai-client-keepalive.md` — operator-facing
  guidance still applies; framework defaults now codify the
  recommendations there
