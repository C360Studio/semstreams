# Migration Guide: beta.69 → beta.70

## Summary

beta.70 ships **ADR-039 Phase 3** — retirement of the in-process
`ToolCallFilter` wiring that the beta.67+68 release shipped. Subject-mode
tool-call governance (added in beta.69) is now the sole governance path.

This tag is marked **BREAKING** because the following public symbols
are removed:

| Removed symbol | Replacement |
|---|---|
| `agentic.ToolCallFilter` interface | Subject-mode rules subscribed to `agent.toolcall.proposed.>` |
| `agentic.ToolCallFilterResult` | Rule actions emit verdicts directly to `agent.toolcall.approved.*` / `.rejected.*` |
| `agentic.ToolCallRejection` | Same as above |
| `agenticloop.Component.SetToolCallFilter(filter)` | `tool_call_governance.mode = "audit"` or `"enforce"` config |
| `agenticloop.MessageHandler.SetToolCallFilter(filter)` | Same as above |
| `agenticgovernance.Component.ToolCallFilter()` accessor | Filter still operates inside the FilterChain via `ProcessMessage`; no cross-component handoff |
| `agenticgovernance.ToolCallFilter.FilterToolCalls(...)` method | Same — chain processing is the surviving surface |

## For semspec consumers

If you have already migrated to subject-mode (`tool_call_governance.mode = "enforce"`)
on beta.69 and confirmed your rules produce verdicts as expected, this
tag is a **no-op upgrade** for your code path — the symbols being
removed are the ones you stopped calling when you flipped to
subject-mode.

If you're still wiring the in-process filter at beta.69 (i.e. you
haven't completed the Stage 2 cutover from the
[beta.68 → beta.69 migration guide][prev]), upgrading to beta.70
will not compile. You must finish the beta.69 migration first.

[prev]: migration-beta68-to-beta69.md

### Wiring code that compiled at beta.69 but won't at beta.70

```go
// BEFORE — beta.69 escape hatch (no longer compiles at beta.70)
gov := governanceComponent.(*agenticgovernance.Component)
loop := loopComponent.(*agenticloop.Component)
if f := gov.ToolCallFilter(); f != nil {
    loop.SetToolCallFilter(f)
}
```

After beta.70 this becomes simply:

```go
// AFTER — beta.70 (subject-mode is wired automatically via config)
// No cross-component glue code needed. The agentic-loop and
// agentic-governance components are constructed independently; each
// reads its own config section.
```

The agentic-loop's `tool_call_governance.mode` config field activates
the subject-mode flow; the agentic-governance component's filter chain
processes inbound subjects per its own port configuration.

## Internal API change: ApprovalFilter

The human-in-the-loop `agentictools.ApprovalFilter` no longer
implements `agentic.ToolCallFilter` (because the interface no longer
exists). Its `FilterToolCalls` method returns an internal
`ApprovalFilterResult` directly, not the old generic shape.

External callers of `ApprovalFilter` are not expected — it has always
been internal to agentic-tools. If you reach for it from outside the
package, change the call site from:

```go
result, err := filter.FilterToolCalls(loopID, calls) // beta.69
```

to:

```go
result := filter.FilterToolCalls(loopID, calls) // beta.70
```

The rejected entries are now of type `ApprovalRejection` rather than
`agentic.ToolCallRejection`. Field shape (`Call`, `Reason`) is
unchanged.

## What stays

- `tool_call_governance.{mode,timeout}` config (unchanged)
- All three modes: `disabled` (default, no governance gate), `audit`,
  `enforce`
- `agent.toolcall.proposed.*`, `agent.toolcall.approved.>`,
  `agent.toolcall.rejected.>` subject topology
- All three Prometheus metrics:
  `tool_call_governance_verdict_duration_seconds`,
  `tool_call_governance_verdict_total`,
  `tool_call_governance_subscribe_before_publish_failures_total`
- `agentic-governance.ToolCallFilter` struct as a chain-internal Filter
  (operates on inbound subjects via the FilterChain's `Process`
  surface; the cross-component accessor that exposed it is gone)
- The rule engine's `$message.*` substitution and `approve` action

## Verification

- All in-process `*ToolCallFilter` references in this repo are removed.
- Cross-component wiring (the `SetToolCallFilter` accessor on agentic-loop
  and the `ToolCallFilter()` accessor on agentic-governance) is removed.
- Unit + race + integration tests green.
- `task e2e:agentic` green with the existing audit-mode + approve-all
  rule scenario from beta.69.
