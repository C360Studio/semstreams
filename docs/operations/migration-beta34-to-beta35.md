# Migration Guide: beta.34 → beta.35

## Summary

Beta.35 closes a structural bug in the agentic-tools executor registry
that semspec hit in production against Anthropic's API: multi-tool
executors could not be correctly registered. Two patterns coexisted in
the codebase, both broken in different ways. The fix introduces a
single correct registration shape and migrates every internal caller.

| Surface | Status |
|---|---|
| New method `(*ExecutorRegistry).RegisterExecutor(exec)` | **Additive** |
| `(*ExecutorRegistry).ListTools()` now dedups by tool name | **Behavioural change — eliminates duplicate emit** |
| `(*ExecutorRegistry).RegisterTool(name, exec)` | **Unchanged** — kept for single-tool callers; doc warns about multi-tool footgun |
| `(*Component).RegisterToolExecutor(exec)` | **Behavioural change — delegates to RegisterExecutor for atomic registration** |
| 5 internal `register_*.go` sites migrated | **Internal** — no external impact |

**The simplest beta.34 → beta.35 upgrade is to do nothing.** Internal
behaviour for tool advertise + dispatch becomes correct; no config
changes required.

## The bug

Two registration patterns coexisted in beta.34:

**Pattern A** (rules, flows, personas, flow_templates): registered the
same executor under each of its tool names via `RegisterTool` in a
loop. Five entries in the registry pointing at one executor.
`ListTools()` walked all five entries and called `executor.ListTools()`
on each, yielding 5×5 = 25 entries with duplicates. Anthropic's API
rejects duplicate tool names with HTTP 400.

**Pattern B** (graph_query): registered once under one canonical name.
`ListTools()` correctly emitted the executor's five tool defs (one
entry × five-tool executor). But `Execute()` looked up by `call.Name`
in the registry map, which only had the canonical name — the other
four tools 400-not-found at dispatch despite being advertised.

Mock-llm tests masked both bugs. semspec discovered them in production
with their qwen3 / OpenRouter route once Anthropic-strict validation
was on the wire.

## The fix

`(*ExecutorRegistry).RegisterExecutor(executor)` registers the
executor under every name returned by `executor.ListTools()`. Atomic:
validates all names before committing any, and rolls back on
collision. This is the **only** correct registration shape for
multi-tool executors.

`(*ExecutorRegistry).ListTools()` now dedups by tool name as
defense-in-depth — any caller still using the loop pattern stops
emitting duplicates.

`(*Component).RegisterToolExecutor(executor)` (the public runtime API
for adding executors at runtime) now delegates to `RegisterExecutor`.
Atomic registration replaces the prior loop, which left dispatch in a
half-wired state on partial collisions.

## What changed under the hood

Five internal call sites migrated from `RegisterTool`-in-loop or
single-name registration to `RegisterExecutor`:

- `processor/agentic-tools/executors/register_rules.go` (5 tools)
- `processor/agentic-tools/executors/register_flows.go` (5 tools)
- `processor/agentic-tools/executors/register_personas.go` (5 tools)
- `processor/agentic-tools/executors/register_flow_templates.go` (6 tools)
- `processor/agentic-tools/executors/register_graph_query.go` (5 tools)

Nine single-tool register sites (bash, decide, github_read, etc.) are
unchanged — `RegisterTool(name, executor)` is correct for single-tool
executors.

The dead per-tool-name string constants in the four Pattern A files
were removed (they only existed to feed the broken loop).

## Recommended migration for external code

If your code uses `(*Component).RegisterToolExecutor`: nothing to do.
The method now does the right thing.

If your code calls `(*ExecutorRegistry).RegisterTool` directly:

- For single-tool executors (whose `ListTools()` returns one
  definition): no change needed.
- For multi-tool executors (whose `ListTools()` returns multiple
  definitions): replace the call with
  `(*ExecutorRegistry).RegisterExecutor(executor)`. The new method
  handles every advertised name atomically.

```go
// Before (broken — duplicate advertise):
for _, name := range []string{"foo", "bar"} {
    reg.RegisterTool(name, executor)
}

// Before (broken — dispatch gap):
reg.RegisterTool("foo", executor) // executor advertises foo + bar

// After (correct):
reg.RegisterExecutor(executor)
```

## Backward compatibility

`RegisterTool(name, executor)` is preserved verbatim. Existing
single-tool callers continue to work without modification. The doc
comment now warns about the multi-tool footgun and points to
`RegisterExecutor`.

`ListTools()`'s new dedup is observable: callers that previously
received duplicate tool defs (and were presumably working around the
bug somehow, or hitting the same Anthropic 400 in their own
deployments) will now receive each name exactly once.

## Cross-references

- semspec post-mortem against beta.34 (the empirical case study)
- `processor/agentic-tools/executor.go` — `RegisterExecutor` /
  `ListTools` / `RegisterTool` implementation
- `processor/agentic-tools/component.go:RegisterToolExecutor` — public
  runtime API, now delegates
- `processor/agentic-tools/executor_test.go` —
  `TestExecutorRegistry_ListTools_DedupesAcrossEntries`,
  `TestExecutorRegistry_RegisterExecutor_AllNamesDispatch`,
  `TestExecutorRegistry_RegisterExecutor_ValidationFailures`

## Follow-up (not in this tag)

Considered and deferred:

- **Dedup observability**: `ListTools()` could emit a one-shot
  `slog.Warn` when dedup fires from distinct executor instances
  declaring the same tool name (vs. the legacy loop-pattern
  false-positive). Useful for catching real misconfigurations across
  multiple registered executor providers. Not done in this tag because
  the migrated internal callers no longer trigger the loop pattern,
  and the warn requires distinguishing pointer identity. Track as a
  follow-up if the multi-provider misconfig case arises.
