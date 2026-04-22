# Ollama Setup for Long Contexts

SemStreams talks to Ollama through Ollama's OpenAI-compatible `/v1/chat/completions` endpoint. That endpoint has one structural limitation you need to work around: **you can't set `num_ctx` per-request**. If you ignore this, every prompt beyond ~4K tokens silently truncates on Ollama's side with no error.

This guide covers the workaround, why it exists, and how SemStreams helps you catch the trap.

## The Problem

Ollama's `/v1/` layer aims to match OpenAI's request schema exactly. OpenAI's schema has no concept of "context window" as a request parameter — the model's context size is fixed at deployment. So Ollama's compat layer doesn't accept `num_ctx` either. Ollama's own maintainers have [closed community PRs](https://github.com/ollama/ollama/pull/6137) that tried to add it, on the principle that the OpenAI-compat endpoint should stay OpenAI-compliant.

The result: every request goes out with Ollama's built-in default context size (typically 4096 tokens). Long system prompts, multi-turn histories, and tool-call chains silently get cut off from the front.

## The Fix: Modelfile + `ollama create`

Ollama's native `/api/chat` endpoint accepts `num_ctx` in the request body, but SemStreams uses the OpenAI-compat path for everything so the same client code works across providers. Rather than maintain two protocol paths, we ask you to pre-create a model with the context size you want.

```text
# Modelfile — saves as literal filename "Modelfile"
FROM qwen3:8b
PARAMETER num_ctx 32768
```

Build the derived model once:

```bash
ollama create qwen3-8b-32k -f Modelfile
```

Then point your endpoint config at the new model name:

```json
{
  "endpoints": {
    "local-qwen3": {
      "provider": "ollama",
      "url": "http://localhost:11434/v1",
      "model": "qwen3-8b-32k",
      "max_tokens": 32768
    }
  }
}
```

That's it. The model is now server-side configured for 32K context. All SemStreams requests will use that size automatically.

## How SemStreams Detects the Trap

On the first request to any Ollama endpoint, SemStreams calls Ollama's `/api/show` endpoint to read the model's actual `num_ctx`. If that value is below `endpoint.max_tokens`, you get one WARN log per endpoint:

```text
ollama model num_ctx is below endpoint.max_tokens — prompts will silently truncate on the server
  model=qwen3:8b
  model_num_ctx=4096
  num_ctx_explicit=false
  endpoint_max_tokens=32768
  fix=docs/operations/04-ollama-setup.md
```

The probe fires once per Client (per unique URL+model combination), not per-request. It never blocks a real request — if Ollama is down or `/api/show` fails, the probe logs at Debug and silently moves on.

Field meanings:

| Field | Meaning |
|---|---|
| `model_num_ctx` | The context size Ollama will actually use for this model |
| `num_ctx_explicit` | `true` if the Modelfile sets `PARAMETER num_ctx`, `false` if using the 4096 default |
| `endpoint_max_tokens` | What your SemStreams config claims the model supports |

## Picking `num_ctx`

Three constraints to balance:

1. **Model architecture cap.** Each model has a maximum the architecture supports (e.g., Qwen3 variants are 128K-capable). Reading `model_info.<arch>.context_length` from `/api/show` gives this ceiling.
2. **GPU memory.** Larger `num_ctx` means more KV-cache allocation. A 32K context on an 8B model with Q4 quantization comfortably fits in 12-16GB VRAM; 128K may OOM on the same hardware.
3. **Actual needs.** SemStreams' agentic loop accumulates tool results and context. 32K is a reasonable default for agentic work; 8K-16K is fine for simple chat flows.

Start at 32K, monitor GPU usage with `nvidia-smi` or `ollama ps`, and raise only when you hit the `ollama model num_ctx is below endpoint.max_tokens` warning with a higher `endpoint.max_tokens`.

## Common Pitfalls

**Keeping `endpoint.max_tokens` in sync.** The probe compares against `endpoint.max_tokens`. If you raise `num_ctx` in the Modelfile but forget to raise `endpoint.max_tokens`, you won't get warned — but summarization heuristics in agentic-loop also won't use the extra space. Keep them matched.

**Using `ollama pull` doesn't update an existing derived model.** `ollama pull qwen3:8b` refreshes the base image only. Your derived `qwen3-8b-32k` stays on the old base until you run `ollama create` again with the same Modelfile.

**The `/v1` URL suffix is correct.** SemStreams expects `http://localhost:11434/v1` in `endpoint.url` for the chat-completion path. The probe automatically strips `/v1` when it calls `/api/show`.

**Options like `seed` and `mirostat` don't work via `/v1/`.** Ollama's `/v1/chat/completions` ignores most of the `options` block. If you need those knobs, the Modelfile is again the path — set them as `PARAMETER` entries alongside `num_ctx`.

## References

- [Ollama OpenAI compatibility documentation](https://docs.ollama.com/api/openai-compatibility)
- [Ollama Modelfile reference](https://docs.ollama.com/modelfile)
- [Issue #5356 — num_ctx via OpenAI compat](https://github.com/ollama/ollama/issues/5356)
- [PR #6137 — closed attempt to add num_ctx to /v1/](https://github.com/ollama/ollama/pull/6137)
