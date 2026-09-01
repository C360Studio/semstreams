# LLM Routing and Model Selection

This doc is the per-capability spec sheet for every LLM workload
SemStreams runs. It tells operators (and AI agents auditing model fit)
exactly what each call site asks of its model, so a small-model
deployment can verify that a 1B–8B locally-baked model can handle the
ask before binding it.

If you're new to the registry concept, read
[05 — Model Registry](05-model-registry.md) first — this doc
assumes you understand what a capability is and how routing works.

## How to use this doc

**As an operator picking model bindings:** scan the [Workload Tier
Matrix](#workload-tier-matrix) below to decide which capabilities can
share an endpoint vs which need dedicated capacity. Then use the
[capability spec sheets](#capability-spec-sheets) to confirm your
candidate model meets each capability's traits and envelopes. Finally,
use the [configuration examples](#configuration-examples) to wire
bindings.

**As the seminstruct agent (or any model-fit auditor):** the spec
sheets are structured so you can parse them programmatically. Each
sheet has the same fields in the same order. The
[Probe Prompts](#probe-prompts-for-fit-validation) section gives you a
short test set you can run against a candidate model to confirm it
meets each capability's bar before letting an operator bind it.

**As someone hitting truncation, garbage JSON, or a slow path:** look
at the spec sheet for the affected capability and check whether the
bound model meets the listed traits. The most common failure mode is
binding a 1B model to a capability that needs 7B-class instruction
following.

## Workload Tier Matrix

Workloads sort into three tiers by latency sensitivity. Concurrent
backends (multiple llama-server instances behind a proxy) help most
in the **background batch** and **slow path** tiers. The
**latency-critical** tier benefits more from co-location with a fast,
dedicated endpoint than from concurrency.

| Tier | Concurrency wins | Capabilities |
|---|---|---|
| **Background batch** — fires async after some other event; user not waiting | Yes — pile-ups are common after community detection or KV queue drains | `community_summary`, `anomaly_review`, `embedding` |
| **Slow path / tolerant** — user-facing but tolerant of fallback to deterministic alternative | Yes — only fires when faster paths fail | `query_classification` (T3 fallback), `summarization` (compaction) |
| **Latency-critical** — fires inline on every user request or every loop step | Concurrency helps less than co-location with a fast model | `answer_synthesis`, `intent_classification` (every user message), agentic chat itself |

The **agentic chat itself** is not in the table because it isn't a
capability binding — it routes per-`AgentRequest` based on
`Model`/`Capability` fields and is the workload semstreams' agentic
loop is built around. Keep it on whatever fast endpoint your loops use.

## Capability Spec Sheets

Each sheet is a structured table the seminstruct agent can parse. All
seven LLM call sites are bound via `model.Capability*` constants in
`model/registry.go:11-37`. The last two (`intent_classification`,
`anomaly_review`) were lifted in Phase 2 — see their fallback notes
for the legacy behavior preserved when the capability isn't bound.

### `summarization` — Agentic-Loop Context Compaction

| Field | Value |
|---|---|
| **Status** | Capability-bound (`model.CapabilitySummarization`) |
| **Caller** | `processor/agentic-loop/component.go:184` (resolves) → `processor/agentic-loop/summarizer.go:40` (calls) |
| **Job** | Compress a conversation history (system + user + assistant + tool results) into a concise summary so the loop can continue past its token budget. |
| **Input envelope** | Free-form chat history. Token count varies — typically 4K–32K tokens (roughly the model's context window minus the response budget). |
| **Output envelope** | Plain text, 200–800 tokens typical. No structured format required; the summary is fed back as a system message in subsequent iterations. Hard cap from caller via `maxTokens` parameter. |
| **Required model traits** | Decent abstractive summarization. Faithful preservation of entity IDs, file paths, error messages, specific values. **Long-context handling** — the input may be near the model's context limit. |
| **Latency budget** | Inherited from `summarization` capability `timeout` (ADR-024) → endpoint `request_timeout` → component default `120s`. The loop is paused while compaction runs. |
| **Fallback** | If no `summarization` endpoint resolves, agentic-loop uses a stub compactor (truncates messages mechanically rather than summarizing). Loop continues but quality degrades. |
| **Prompt source** | `processor/agentic-loop/summarizer.go:34-37` (system prompt) + `processor/agentic-loop/summarizer.go:59-70` (user prompt builder) |

### `community_summary` — Graph Community Summarization

| Field | Value |
|---|---|
| **Status** | Capability-bound (`model.CapabilityCommunitySummary`) |
| **Caller** | `processor/graph-clustering/component.go:1072` (resolves + creates client) → `graph/clustering/summarizer.go` (`LLMSummarizer.Summarize`) |
| **Job** | Generate a 1–2 sentence narrative description of an entity community (cluster of related entities) for graph KB enrichment. |
| **Input envelope** | Structured prompt: entity-ID parts, dominant domain, domain breakdown, key keywords, sample entity titles/abstracts. Typically 500–2000 tokens. |
| **Output envelope** | Plain text, 1–2 sentences (50–150 tokens typical). No structured format. |
| **Required model traits** | Basic instruction following + abstractive summarization. The prompt teaches the model the 6-part federated entity ID notation, so the model needs to follow a small in-prompt taxonomy. **Tool calling: not required.** |
| **Latency budget** | Background — runs in the enhancement worker pool. Per-request timeout from the standard chain (typically 60–120s for local models). |
| **Fallback** | If no endpoint resolves or the call fails, the community is marked `summary_status=llm-failed` and the statistical (TF-IDF) summary is retained. Graph queries still work. |
| **Prompt source** | `graph/llm/prompts.go:14-52` (`CommunityPrompt`) — system prompt + Go text/template user prompt |

### `query_classification` — Graph-Query T3 Classifier

| Field | Value |
|---|---|
| **Status** | Capability-bound (`model.CapabilityQueryClassification`) |
| **Caller** | `processor/graph-query/component.go:372` (resolves + creates client) → `graph/query/classifier_llm_adapter.go:28` (`ClassifyQuery`) |
| **Job** | Classify a natural-language query into structured `SearchOptions` (predicates, types, strategy) when keyword/spatial/temporal classifiers (T0/T1/T2) all failed to produce a confident classification. |
| **Input envelope** | A classification prompt with the user's query + the schema of `SearchOptions`. Typically 500–1500 tokens. |
| **Output envelope** | **Strict JSON.** Reasoning models may emit `<think>` blocks which the adapter strips (see `graph/query/classifier_llm_adapter.go:42`). MaxTokens=2048 to give reasoning models headroom. |
| **Required model traits** | **JSON-mode reliability is critical.** Instruction following: "Return ONLY valid JSON. No markdown, no explanation. Do not use `<think>` tags." Hallucinated JSON keys here cause silent classification failures. **Tool calling: not required.** |
| **Latency budget** | User-facing but only fires on the slow path. From the standard chain — typically 30–60s budget on local models. Reasoning models burn time on thinking tokens; budget accordingly. |
| **Fallback** | If no endpoint resolves, the chain ends after T2 — the query degrades to a keyword-only classification. No hard failure. |
| **Prompt source** | `graph/query/classifier_llm_adapter.go:30-31` (system prompt) + the prompt-builder elsewhere in the `graph/query/` package |

### `answer_synthesis` — Graph-Query GlobalSearch Answer Composition

| Field | Value |
|---|---|
| **Status** | Capability-bound (`model.CapabilityAnswerSynthesis`) |
| **Caller** | `processor/graph-query/component.go:406` (resolves + creates client) → `processor/graph-query/answer.go:62` (`Synthesize`) |
| **Job** | Compose a query-focused natural-language answer from up to N community summaries returned by a `globalSearch` query. Answer should reference specific entities by name. |
| **Input envelope** | Structured prompt: user query + cluster summaries + representative entities + keywords. Typically 1K–4K tokens. |
| **Output envelope** | Plain text, factual prose. MaxTokens=500 (`processor/graph-query/answer.go:55`). Should reference named entities from the input rather than speculate. |
| **Required model traits** | Mid-tier instruction following + abstractive synthesis with **citation discipline** (use the entities/keywords provided; don't invent). 7B+ models tend to handle this better than 1–3B. |
| **Latency budget** | User-facing, blocking — the global-search HTTP response waits for this. Standard chain; typical 30–60s on local models. Temperature is held low (0.3) for factual output. |
| **Fallback** | If no endpoint resolves, the component swaps in `TemplateAnswerSynthesizer` (`processor/graph-query/answer.go:85`), which produces a deterministic template-based answer with no LLM call. The HTTP response stays well-formed. |
| **Prompt source** | `processor/graph-query/answer.go:24-28` (system prompt) + `processor/graph-query/answer.go:99-142` (user prompt builder) |

### `embedding` — Graph Embeddings

| Field | Value |
|---|---|
| **Status** | Capability-bound (`model.CapabilityEmbedding`) |
| **Caller** | `processor/graph-embedding/component.go:622` (resolves) → `graph/embedding/http_embedder.go:88` (creates client) |
| **Job** | Produce vector embeddings for entity text (label + abstract) for similarity search and clustering. |
| **Input envelope** | Short text, typically 50–500 tokens per entity. Batched in groups for throughput. |
| **Output envelope** | Vector of floats (the embedding). Dimension depends on model (384 for `all-MiniLM-L6-v2`, 768 for `bert-base`, etc.). |
| **Required model traits** | This is **not a chat-completion model.** It uses the OpenAI-compatible `/v1/embeddings` endpoint. The endpoint must serve a sentence-transformer-style embedding model, not an instruction-tuned chat model. |
| **Latency budget** | Background batch. |
| **Fallback** | If no endpoint resolves, graph-embedding starts in degraded mode (statistical-only Tier 1). No semantic Tier 2 features. |
| **Bound endpoint** | Conventionally `semembed` (the SemStreams embedding service running a sentence-transformer model). **Do not bind a chat model here** — different protocol. |

### `intent_classification` — Agentic-Dispatch Intent Router

| Field | Value |
|---|---|
| **Status** | Capability-bound (`model.CapabilityIntentClassification`) |
| **Caller** | `processor/agentic-dispatch/intent_classifier.go:67` (`Classify`). **Fires on every incoming user message.** |
| **Job** | Classify a user message into one of five intents (`new_task`, `continue`, `signal`, `question`, `meta`) so dispatch can route it to the right handler. |
| **Input envelope** | System prompt enumerating the five intents + active-loops context + the user message. Typically 200–800 tokens. |
| **Output envelope** | **Strict JSON object** of shape `{"type": "...", "loop_id": "...", "signal_type": "...", "confidence": 0.0–1.0}`. MaxTokens=128 (`processor/agentic-dispatch/intent_classifier.go:118`). |
| **Required model traits** | **JSON-mode reliability + low-latency response.** Temperature held at 0.1 for determinism. The five-intent taxonomy is small enough that 1B models can do it well if their JSON-mode is reliable. **Tool calling: not required.** |
| **Latency budget** | Hardcoded 15s timeout (`processor/agentic-dispatch/intent_classifier.go:107`). **This is the most-frequent user-facing LLM call in the system** — every user message hits it before anything else can happen. |
| **Fallback** | On endpoint-not-found, client-creation error, LLM error, or unparseable response → returns `IntentNewTask` with `confidence=0.5`. The user's message still gets handled but as a brand-new task even if it was a follow-up. When the capability is not bound, the resolution chain falls through to `defaults.model` (preserving the pre-Phase-2 piggyback). |
| **Prompt source** | `processor/agentic-dispatch/intent_classifier.go:78-90` (system prompt) |

### `anomaly_review` — Graph Inference Relationship Reviewer

| Field | Value |
|---|---|
| **Status** | Capability-bound (`model.CapabilityAnomalyReview`) |
| **Caller** | `graph/inference/review_worker.go:408` (`llmReview`); wired in `processor/graph-clustering/component.go` via `resolveReviewLLMClient`. |
| **Job** | Decide whether a structurally-suggested missing relationship between two graph entities should be added. APPROVE or REJECT plus reasoning. Only fires when confidence falls between auto-approve/auto-reject thresholds. |
| **Input envelope** | Structured: anomaly type, confidence, entity A/B IDs and contexts, evidence (similarity, structural distance, core level). Typically 500–2000 tokens. |
| **Output envelope** | First word `APPROVE` or `REJECT`, followed by free-form reasoning. Parsed by `parseLLMDecision` in `review_worker.go`. |
| **Required model traits** | Basic instruction following — the system prompt is concise and the output format is simple. Temperature held at 0.1 for deterministic decisions. **Tool calling: not required.** |
| **Latency budget** | Configurable via `ReviewTimeout` config field, default `30s` (`graph/inference/review_worker.go:393`). Background batch — drains `ANOMALY_INDEX` KV queue. |
| **Fallback** | On LLM error or no LLM client, falls through to human review (if `FallbackToHuman` configured) or auto-rejects with a "no LLM or human review" reason. **When the capability is not bound, the review worker reuses the `community_summary` LLM client** (legacy piggyback preserved). To run anomaly review on a different endpoint than community summarization, bind `anomaly_review` explicitly. |
| **Prompt source** | `graph/inference/review_worker.go:635-647` (`reviewSystemPrompt`) + `buildReviewPrompt` |

## Small-Model Suitability Guidance

Sizing rules of thumb when picking a small model (1B–8B) to bind to a
capability. These are starting points; always validate with the
[probe prompts](#probe-prompts-for-fit-validation) below.

| Capability | Smallest reasonable | Sweet spot | What goes wrong below the floor |
|---|---|---|---|
| `embedding` | n/a — sentence-transformer model, separate sizing concerns | `all-MiniLM-L6-v2` (~22M params) is fine for many use cases | Wrong protocol entirely if you bind a chat model |
| `intent_classification` | 1B if JSON-mode is reliable | 3B | Hallucinated intent values or malformed JSON → silent fallback to `new_task` for everything |
| `query_classification` | 3B for tool-free chat models | 7B for reasoning models | `<think>` block leakage into JSON, hallucinated SearchOptions fields |
| `community_summary` | 3B | 7B | Boring template-y output that doesn't leverage the entity-ID structure the prompt teaches |
| `anomaly_review` | 3B | 7B | Inverted decisions (APPROVE for things that should reject) or unparseable first-word verdict |
| `summarization` | 3B–7B (depends on context window) | 7B+ with long-context support | Faithfulness drops — entity IDs and error messages get paraphrased away |
| `answer_synthesis` | 7B | 7B+ tuned for instruction following | Speculation beyond input clusters; missing citations |

### Known weaknesses to test for

- **Hallucinated tool calls.** Some 1–3B models emit phantom tool
  calls even when no tools are provided in the request. Doesn't apply
  to most capabilities here (only `summarization` could in theory be
  tool-using, and it isn't), but watch for it on agentic chat.
- **JSON-mode unreliability.** Stripping markdown fences (` ```json ` …
  ` ``` `), accepting prose-wrapped JSON, and tolerating trailing
  commas. The `extractJSON` helper in
  `processor/agentic-dispatch/intent_classifier.go:176` is a
  workaround, but a model that needs that workaround often is a model
  that won't survive prompt-injection.
- **`<think>` tag leakage.** Reasoning models (qwen3, deepseek-r1)
  emit `<think>...</think>` blocks. The query classifier strips them
  (`graph/query/classifier_llm_adapter.go:42`). Other call sites
  don't. Avoid binding reasoning models to capabilities other than
  `query_classification` until handling is added.
- **Multi-turn coherence in compaction.** Some small models drift on
  turn 5+ of a long conversation, summarizing earlier turns
  incorrectly because they're "blended in" with later context. Test
  with deliberately long inputs (target 80% of context window).
- **Instruction-following on structured-output prompts.** A model that
  passes a single-shot probe may still fail on the 50th call when
  context conditions shift. Run probes with varied input lengths.

### Probe Prompts for Fit Validation

Use these against a candidate model bound to a temporary endpoint.
Each probe targets one capability; the model passes if the response
matches the listed criteria. Run the probe panel at least 5 times to
catch flakiness.

#### Probe — `summarization`

```text
System: <use the actual prompt from processor/agentic-loop/summarizer.go:34-37>
User: Conversation to summarize:

[system]: You are a helpful agent.
[user]: Look up the entity acme.ops.fleet.gcs.drone.001 and report its battery level.
[assistant]: I'll check that.
  -> tool_call: get_entity({"id": "acme.ops.fleet.gcs.drone.001"})
[tool]: {"battery_pct": 23, "last_seen": "2026-04-30T10:15:00Z"}
[assistant]: Drone 001 is at 23% battery as of 2026-04-30T10:15:00Z. Below low-battery threshold; recommend recall.
```

**Pass criteria:** Output preserves `acme.ops.fleet.gcs.drone.001`,
`23%`, and the timestamp verbatim. Output is 50–150 tokens. No
fabricated facts.

#### Probe — `community_summary`

Use the rendered output of `graph/llm/prompts.go:14-52` against a
small synthetic community (3–5 entities under one org/platform).

**Pass criteria:** Output is 1–2 sentences. References the dominant
domain. Doesn't fabricate entity IDs. Stays under 150 tokens.

#### Probe — `query_classification`

```text
System: You are a query classifier. Return ONLY valid JSON. No markdown, no explanation. Do not use <think> tags.
User: Classify this query into a SearchOptions JSON object: "what drones are at low battery in the warehouse?"
```

**Pass criteria:** Output is parseable JSON. No markdown fences. No
`<think>` blocks. Field names match the SearchOptions schema rather
than inventing new ones.

#### Probe — `answer_synthesis`

Render `processor/graph-query/answer.go:99-142` (`buildAnswerPrompt`)
against a synthetic globalSearch result with 3 clusters and 2 named
entities per cluster.

**Pass criteria:** Output is plain prose, 200–500 tokens. References
at least 2 named entities from the input verbatim. Doesn't speculate
about entities not in the input.

#### Probe — Intent classification

```text
System: <use the actual prompt from processor/agentic-dispatch/intent_classifier.go:78-90>
User: can you cancel 7c9e6679-7425-40de-944b-e07fc1f90ae7?
```

**Pass criteria:** Returns `{"type": "signal", "signal_type": "cancel",
"loop_id": "7c9e6679-7425-40de-944b-e07fc1f90ae7", "confidence": >0.8}`. Output is parseable
JSON. No prose preamble.

#### Probe — Anomaly review

Render `graph/inference/review_worker.go:635-647` system prompt with a
synthetic anomaly (similarity=0.85, structural_distance=2 hops).

**Pass criteria:** First word is `APPROVE` or `REJECT` (uppercase).
Reasoning is concrete, references the evidence values.

## Configuration Examples

### Offload routing — concurrent seminstruct backend

After the seminstruct concurrent llama-server proxy is in place, route
the offload-friendly capabilities to it. Existing agentic chat stays on
the small-model backend (or whatever the deployment uses).

```jsonc
{
  "model_registry": {
    "endpoints": {
      "seminstruct-concurrent": {
        "provider": "openai",
        "url": "http://seminstruct:8080/v1",
        "model": "qwen3-8b-instruct",
        "max_tokens": 32768,
        "max_output_tokens": 8192,
        "request_timeout": "60s"
      },
      "agentic-fast": {
        "provider": "openai",
        "url": "http://agentic-backend:8083/v1",
        "model": "qwen2.5-3b-instruct",
        "max_tokens": 16384,
        "max_output_tokens": 4096,
        "supports_tools": true,
        "request_timeout": "30s"
      },
      "semembed": {
        "provider": "openai",
        "url": "http://semembed:8081/v1",
        "model": "all-MiniLM-L6-v2",
        "max_tokens": 0
      }
    },
    "capabilities": {
      "summarization":          { "preferred": ["seminstruct-concurrent"], "fallback": ["agentic-fast"], "timeout": "60s" },
      "community_summary":      { "preferred": ["seminstruct-concurrent"], "timeout": "120s" },
      "query_classification":   { "preferred": ["seminstruct-concurrent"], "timeout": "30s" },
      "answer_synthesis":       { "preferred": ["seminstruct-concurrent"], "timeout": "30s" },
      "anomaly_review":         { "preferred": ["seminstruct-concurrent"], "timeout": "30s" },
      "intent_classification":  { "preferred": ["agentic-fast"], "timeout": "15s" },
      "embedding":              { "preferred": ["semembed"] }
    },
    "defaults": {
      "model": "agentic-fast"
    }
  }
}
```

`intent_classification` is bound to `agentic-fast` (not the concurrent
backend) because it fires on every user message and benefits more from
co-location than concurrency — every user-perceptible response waits
for it. The other lifted capability (`anomaly_review`) moves to the
concurrent backend with the rest of the offload set. If you omit any
binding, the resolution falls back as documented in each capability's
spec sheet above.

### Single-backend deployment (semantic.json shape)

The simpler default — one seminstruct endpoint serves everything:

```jsonc
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

`summarization`, `query_classification`, and `answer_synthesis` are
unbound and will degrade gracefully (stub compactor / keyword-only
classification / template synthesis respectively). Only bind them
explicitly once you've validated the model can handle the workload.

## Validation Checklist

Once you've bound a candidate model to a capability:

- [ ] All probe prompts for that capability pass on at least 5 of 5
      runs.
- [ ] The model's `EndpointConfig` has `max_output_tokens` set to a
      value at or below the model's actual output capacity (avoid the
      16384 silent-truncation gap — see
      [migration-beta29-to-beta30.md](migration-beta29-to-beta30.md)).
- [ ] Prometheus shows traffic flowing to the new endpoint:
      - `endpoint_health_state` gauge is 1 (closed/healthy) for the
        endpoint
      - `semstreams_router_loop_approvals_submitted_total` and
        capability-specific counters increment as expected
      - `graph_processor_enhancement_*` (for `community_summary`)
- [ ] The fallback path still works — temporarily make the new
      endpoint unreachable (kill the container or block its port) and
      verify the capability degrades gracefully:
      - `summarization` → stub compactor
      - `community_summary` → `summary_status=llm-failed`, statistical
        summary retained
      - `query_classification` → keyword-only
      - `answer_synthesis` → `TemplateAnswerSynthesizer`
      - `embedding` → graph-embedding starts in degraded mode
- [ ] Sampled outputs reviewed manually — pull 10 real
      production-shape inputs and confirm the model's outputs are
      acceptable on first read.
- [ ] Check `endpoint_health_state` again after letting traffic flow
      for ~5 minutes — circuit-breaker hasn't tripped.

## Related

- [05 — Model Registry](05-model-registry.md) — registry structure,
  field reference, runtime updates, validation rules
- [06 — Endpoint Health and Circuit Breaker](06-endpoint-health-circuit-breaker.md)
  — how health-gating filters the capability resolution chain
- [08 — LLM Truncation Handling](08-llm-truncation-handling.md) —
  what happens when `finish_reason=length` fires
- [10 — Provider Adapter Normalization](10-provider-adapter-normalization.md)
  — provider-specific request/response shaping
- [ADR-024 Layered LLM Timeouts](../adr/024-layered-llm-timeouts.md) —
  the four-layer timeout precedence chain referenced throughout
- [migration-beta29-to-beta30.md](migration-beta29-to-beta30.md) —
  `max_output_tokens` field
