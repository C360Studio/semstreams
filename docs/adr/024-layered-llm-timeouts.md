# ADR-024: Layered LLM Request Timeouts

## Status

Proposed

## Context

The `agentic-model` component wraps every LLM call in a single
`context.WithTimeout` driven by one component-level config field
(`config.Timeout`, default `120s`). This is wrong in both directions for any
real deployment:

- **Fast calls wait too long to fail.** A classification call that should
  return in under a second still burns 120 seconds before cancellation when the
  endpoint stalls.
- **Heavy calls die mid-plan.** A multi-step planning call that legitimately
  needs 180 seconds is cancelled at 120.

Three gaps surfaced from upstream users (in particular semspec):

1. **No per-capability timeouts.** Capabilities (`fast`, `general`, `heavy`,
   `summarization`, etc.) are the natural axis along which response-time
   budgets differ, but today there is one global value across all of them.
2. **Per-endpoint `request_timeout` ignored.** semspec already configures a
   `request_timeout` field per endpoint in its model registry. semstreams
   didn't read it.
3. **No per-task override.** A plan-reviewer task (short-lived) has no way to
   ask for a tighter budget than a DAG-node implementation task routed to the
   same capability.

The registry already owns the axes along which operators reason about model
behavior (capabilities, endpoints). That's where timeout configuration belongs
— not sprawled across every `TaskMessage` producer.

## Decision

Introduce a four-layer precedence chain for the per-call timeout applied in
`agentic-model.executeRequest`. The first layer with a non-empty, parseable
duration wins:

| Precedence | Source | Storage |
|------------|--------|---------|
| 1 (highest) | `TaskMessage.Timeout` → `AgentRequest.Timeout` | Per-message field; cached by `LoopManager` for continuation iterations |
| 2 | `EndpointConfig.RequestTimeout` | `model_registry.endpoints.<name>.request_timeout` |
| 3 | `CapabilityConfig.Timeout` | `model_registry.capabilities.<name>.timeout` |
| 4 (lowest) | `agentic-model.config.Timeout` | Component config (unchanged default `120s`) |

A hardcoded `120s` remains as the final safety fallback when every other layer
is empty or zero.

### Resolution site

All resolution happens in one place: a new `(*Component).resolveTimeout`
helper called from `executeRequest`. `getClientForRequest` is extended to
return the resolved `*EndpointConfig` and (when `req.Model` was a capability
name) the capability name, so the resolver has everything it needs without a
second registry walk. This keeps the precedence chain testable in isolation
and observable via one structured log line per request carrying a
`timeout_source` field with values `task`, `endpoint`, `capability`,
`component`, or `default`.

### Validation posture

- **Config-level timeouts** (endpoint, capability, component) validate as
  parseable durations at the configured layer. Today they are read lazily in
  `resolveTimeout` — a malformed value logs a warning and falls through, so
  one bad registry entry never poisons every request.
- **Message-level timeouts** (task, request) also fall through on parse
  failure rather than returning an error. A misconfigured producer should not
  be able to block a task; the next layer down still applies.

### Loop continuity

`TaskMessage.Timeout` is cached in `LoopManager` keyed by `loopID`
(`CacheRequestTimeout` / `GetCachedRequestTimeout`) following the same pattern
as cached tools, tool choice, and metadata. Continuation iterations rebuild
`AgentRequest` from `LoopEntity`, so without the cache the task-level timeout
would only apply to the first LLM call of a loop. The cache is cleaned up
alongside the other per-loop caches on loop termination.

## Consequences

### Positive

- semspec's existing `request_timeout` per endpoint is honored without config
  migration on its side.
- Operators tune response-time budgets once per capability in the model
  registry, rather than threading a timeout through every TaskMessage
  producer.
- The `timeout_source` log field makes timeout behavior directly observable
  from logs, which helps debug "why did this call time out?" questions that
  today require grepping config files.
- The `messageTimeout` field parsed in `NewComponent` — dead code before this
  change — is now the canonical component-level default, so the config value
  is parsed once rather than on every request.

### Negative

- Four precedence layers are more surface area than one. The observability
  mitigation (`timeout_source` log field) matters.
- `getClientForRequest` now returns four values instead of two. Acceptable
  locally; the capability/endpoint are genuinely needed for resolution.
- New fields on `EndpointConfig`, `CapabilityConfig`, `AgentRequest`, and
  `TaskMessage` — but each is `omitempty` and existing configs/messages
  continue to work unchanged.

### Neutral

- The four timeout fields are all Go duration strings (`"45s"`, `"5m"`), matching
  the existing `agentic-model.timeout` pattern rather than introducing
  numeric-seconds or duration-object alternatives.
- Provider-side request timeout hints (`x-request-timeout` and similar
  headers) are out of scope. `context.WithTimeout` at the call site remains
  authoritative; provider-hint plumbing can be a follow-up if needed.

## Alternatives Considered

### A. Component config as a map `{"default": "120s", "fast": "30s"}`

Would put capability timeouts on the agentic-model component instead of the
registry. Rejected: splits timeout configuration across two places (component
for capabilities, registry endpoints for per-endpoint timeout), and makes it
unclear which layer is the source of truth. The registry already owns the
capability concept.

### B. Single new field, replacing the existing global timeout

Would force every deployment to re-tune on upgrade. Rejected in favor of
additive layers that preserve today's default behavior when no new fields are
set.

### C. Retry/hedging policy keyed off timeout class

Interesting but orthogonal to the structural gap semspec hit. Tracked as
post-beta follow-up work.
