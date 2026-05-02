# OpenAI Client Connection Hygiene

A guide to the failure mode where an LLM call hangs for the full
request-level timeout against an upstream that has silently dropped its
side of a pooled TCP connection — and how to tune the agentic-model and
graph/llm clients to fail fast instead.

## The failure mode

Both `processor/agentic-model/client.go` and `graph/llm/openai_client.go`
build `*openai.Client` instances using the
[sashabaranov/go-openai](https://github.com/sashabaranov/go-openai) SDK.
The SDK's default `&http.Client{}` falls through to Go's
`http.DefaultTransport`, which has:

- `IdleConnTimeout: 90s` — pooled connections are reused for up to 90s
- `ResponseHeaderTimeout: 0` — no timeout waiting for response headers
- `KeepAlive: 30s` (TCP) — kernel-level keepalive probes only

This default works against well-behaved cloud providers (OpenAI,
Anthropic). It misbehaves against gateways that silently drop idle TCP
connections without the FIN/RST ever reaching the client:

- **Ollama** (local) — closes idle connections aggressively
- **OpenRouter** — observed wedging for some routes (semspec post-mortem,
  beta.33)
- **Some reverse-proxy fronted endpoints** — proxy-side idle-kill
  invisible to the upstream

When the client's pool hands out a stale connection, the request body
write succeeds (kernel buffer absorbs it) and the response Read blocks
forever. The only timeout that can interrupt is the request-level
`context.Context` deadline, typically set 60–900s by operators tuning
for slow legitimate generations.

The symptom is unmistakable:

- Process at ~0–25% CPU (parked on `netFD.Read`)
- Stack trace in `stream.Recv()` (`processor/agentic-model/client.go:447`)
  or `client.CreateChatCompletion` (`client.go:393`)
- Request remains "in flight" for minutes after preceding requests on
  the same connection succeeded normally

## The fields

`model.EndpointConfig` carries three operator-tunable fields:

| Field | Default | Purpose |
|---|---|---|
| `idle_conn_timeout` | empty (Go default 90s) | Cap how long pooled connections stay idle. Tighten on endpoints whose upstream drops idle conns sooner than 90s. |
| `response_header_timeout` | empty (Go default 0, no timeout) | Cap how long the client waits for response headers. **Streaming-safe** (servers send headers immediately for SSE). **Non-streaming risk**: server sends headers only after generation completes, so a value shorter than legitimate generation time will false-positive. |
| `disable_keepalives` | `false` | Force fresh TCP/TLS connection per request. Eliminates stale-pool risk entirely; costs one handshake per call (~50–300ms for HTTPS). |

Empty/false values preserve Go's defaults; the change in beta.33 → next
tag is purely additive.

## Recommended bindings

Tune per endpoint based on observed behaviour, not provider name. The
table below is a starting point — adjust based on your workload.

### Cloud providers (OpenAI, Anthropic)

```yaml
endpoints:
  openai-gpt5:
    provider: openai
    url: https://api.openai.com/v1
    model: gpt-5
    # No tuning needed. Default 90s idle, no header timeout, keepalive on.
```

### Ollama (local)

```yaml
endpoints:
  ollama-qwen:
    provider: ollama
    url: http://localhost:11434/v1
    model: qwen3-coder:30b
    idle_conn_timeout: "10s"   # Ollama drops idle conns within ~30s
```

### OpenRouter (or any gateway with observed wedge behaviour)

```yaml
endpoints:
  openrouter-qwen3-moe:
    provider: openrouter
    url: https://openrouter.ai/api/v1
    model: qwen/qwen3.6-27b
    disable_keepalives: true   # belt-and-suspenders for known-wedgy gateways
```

### Streaming endpoints (any provider)

```yaml
endpoints:
  agentic-fast:
    provider: openrouter
    url: https://openrouter.ai/api/v1
    model: anthropic/claude-haiku-4.5
    stream: true
    response_header_timeout: "30s"  # SSE servers send headers immediately
                                    # — this catches dead conns fast
```

## Why these and not other options

### Why not default `response_header_timeout` to 30s for everyone?

The non-streaming OpenAI-compat path returns response headers only after
generation is complete. Operators tune the request-level timeout (e.g.
900s) to allow slow legitimate generations. A 30s default
`response_header_timeout` would cancel any non-streaming request that
takes longer than 30s — silently broken for many real workloads.

### Why not provider-conditional defaults?

Baking provider knowledge into framework defaults rots fast. Today
OpenRouter wedges; tomorrow it's a different gateway; next month the
provider you trusted introduces a CDN that idle-kills. Operators should
tune per endpoint based on observation, not provider-name allowlists.

### Why not just `disable_keepalives: true` everywhere?

Costs a TLS handshake per LLM call. Agents make 5–10 LLM calls per
turn; that adds 0.5–3s of pure handshake overhead per agent step on
cloud HTTPS endpoints. Acceptable as a targeted escape hatch, not as a
universal default.

### Why no streaming idle watchdog yet?

`stream.Recv()` blocks per-chunk; if a streaming response starts and
then the model wedges mid-generation, only a per-chunk idle watchdog
would catch it. semspec's post-mortem confirmed `stream: false` was
their configuration, so streaming idle is not the symptom they hit.
Could ship as a follow-up if the streaming class of wedge surfaces.

## Cross-references

- semspec post-mortem against beta.33 (the empirical case study this
  document encodes)
- `model/httpclient.go` — implementation
- `processor/agentic-tools/executors/httprequest.go:198` — sibling
  mitigation for tool-side HTTP calls (already used `DisableKeepAlives:
  true` for per-request clients before this work)
