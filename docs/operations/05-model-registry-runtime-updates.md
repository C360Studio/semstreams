# Model Registry Runtime Updates

SemStreams keeps the live model registry in NATS KV under the key
`model_registry` (bucket `semstreams_config`). Updating that key changes
endpoint definitions, capability routing, and rate limits without
restarting the process.

This guide covers what happens when the key changes, how it propagates
to the running components, and how external library consumers can keep
their own registry pointers in sync.

## The Three Audiences

There are three groups of code that care about the model registry, and
they each see updates a different way:

| Audience | Who | Update path |
|---|---|---|
| **Components in flow configs** | The `agentic-loop`, `agentic-model`, `agentic-dispatch`, `graph-query`, `graph-embedding` factories | Auto-restarted by ComponentManager when their factory declares `component.DepModelRegistry` |
| **External library consumers** | Code outside the semstreams runtime that imports `model.Registry` directly (e.g., a downstream service that wraps the dispatcher) | `model.Watch` + `cfgMgr.WatchModelRegistry()` |
| **Runtime-resolved callers** | Anything calling `RegistryReader.GetEndpoint(name)` per request | Just works — they read the latest state on the next call |

If you're writing a new component for a flow config, you don't need this
guide. Declare the dep on your registration and ComponentManager handles
the rest. See [agentic component patterns](../advanced/08-agentic-components.md).

## How an Update Propagates

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

## External Consumers

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

### Why `atomic.Pointer`?

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

## Updating the KV Key from Operations

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

## What Validates the Update

`config.Manager` runs `model.Registry.Validate()` on every KV change.
Bad registries are rejected before any subscriber sees them — the live
state stays on the prior valid registry. Validate covers:

- Endpoint name (the map key): non-empty, alphanumeric + `-_` only
- Endpoint config: `model` required, `max_tokens` non-negative,
  `provider` in {anthropic, ollama, openai, openrouter},
  `tool_format` in {anthropic, openai}, `reasoning_effort` in
  {none, low, medium, high}, prices non-negative
- Capabilities reference real endpoints; `requires_tools` capabilities
  must have at least one tool-capable endpoint in their chain
- `defaults.model` and `defaults.capability` reference real entries

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

- [Agentic Component Patterns](../advanced/08-agentic-components.md) —
  how registry-dependent components declare and consume the dep
- [ADR-024 Layered LLM Timeouts](../adr/024-layered-llm-timeouts.md) —
  per-endpoint and per-capability timeout config in the same key
