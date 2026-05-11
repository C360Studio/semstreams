# wire/testdata

Captured wire-format JSON for `model/wire` round-trip tests.

## Status (2026-05-10)

The fixtures here are **hand-crafted from public provider documentation**, sufficient to exercise the JSON shape coverage required by chunks 2-5 of [ADR-037](../../../docs/adr/037-self-hosted-llm-wire-package.md).

The full live-capture pass (ADR-037 chunk 1) is deferred to a separate session with provider access. When that lands, fixtures will be regenerated against real responses from OpenAI, Anthropic-via-OpenRouter, Gemini 2.5, Ollama, and vLLM/sparky.

## Layout

| File | Source | Purpose |
|---|---|---|
| `request_simple.json` | OpenAI API docs | Minimal ChatCompletion request |
| `response_simple.json` | OpenAI API docs | Single-choice text response |
| `response_tool_calls.json` | OpenAI API docs | Response with function tool call |
| `response_gemini_thought_signature.json` | ai.google.dev/gemini-api/docs/thought-signatures | Gemini 3.x preview shape with `extra_content.google.thought_signature` on a tool_call |
| `error_object.json` | OpenAI API docs | Standard `{"error": {...}}` envelope |
| `error_array_wrapped.json` | Gemini 3.x preview report | Array-wrapped variant `[{"error": {...}}]` |

## Refresh

When chunk 1 lands, `task fixtures:refresh` will re-capture these against live providers and update the table above.
