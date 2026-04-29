# Migration Guide: beta.24 → beta.25

## Summary

Beta.25 closes the orphan tool-call recovery gap semdragon
flagged in their post-beta.21 audit. Pre-beta.25, several
failure modes (dispatch error, iteration timeout, cancel
signal, approval timeout, approval handler crash) could leave
assistant `tool_calls` in context with no matching `tool`
results. The model API rejects such contexts with 400
("tool_use without tool_result"), turning recoverable
tool-execution flakes into hard loop failures.

Beta.25 fixes this in two architectural seams:

1. **Synth-result emission on every failure path** — every
   terminating dispatch, timeout, cancel, or approval-rejection
   path now stamps a `tool_result` for the affected `call_id`
   so the assistant message's `tool_calls` always have matching
   results.
2. **Pre-request integrity audit** — the existing
   `repairToolPairsLocked` (previously compaction-only) is
   hoisted to a public `RepairToolPairs()` and called before
   every `agent.request` build site as a belt-and-suspenders
   safety net.

Plus the deferred beta.19 item: an **approval-timeout sweeper**
that auto-rejects gated tool calls after
`Config.ApprovalTimeoutStr` elapses, so a stuck human-approval
flow no longer leaves the gated call orphaned indefinitely.

Additive surface; no API breakage. No new payload types. No
configuration changes required for existing deployments.

## What changes

### Synth-result on every terminal-transition path

The framework previously synthesized failure results in three
places — empty-name tool calls, filter rejections, and explicit
approval rejections (beta.19). Beta.25 extends the same pattern
to five additional paths:

| Mode | Trigger | Synth-result diagnostic |
|---|---|---|
| Dispatch failure | `dispatchToolCall` returns error (publish error, marshal failure) | `tool dispatch failed: <reason>` |
| `failLoop` terminal transition | Any path that terminates the loop with pending tools | `loop failed: <reason>` |
| Max iterations reached | `IncrementIteration` returns `ErrMaxIterations` with pending tools | `max iterations reached before tool results returned` |
| Cancel signal | `handleCancelSignal` invoked while tools in flight | `loop cancelled by <user>` |
| Approval timeout | `Config.ApprovalTimeoutStr` elapses without a response | `approval timed out after <duration>` |

The `tool_call_id` is always preserved on the synth-result, so
the model sees a matching pair on the next iteration and can
decide how to recover (retry with different args, switch
tools, or return text).

### Approval-timeout sweeper

The framework now runs a 5-second-tick goroutine that scans
loops in `LoopStateAwaitingApproval` and auto-rejects any whose
`PendingApproval.Timeout` has elapsed. The auto-rejection feeds
through the existing `HandleApprovalResponse` path — same code
path human rejections take — so trajectory steps,
`agent.complete.*` events, and downstream observers see a
consistent shape.

`Config.ApprovalTimeoutStr` was an unused field pre-beta.25
(beta.19 defined it but deferred wiring the timer). Existing
deployments that set this string are now active; deployments
with the field empty get the prior wait-indefinitely behavior.

The `ApprovalResponse.ApprovedBy` field carries the distinct
sentinel `system:approval-timeout` for sweeper-driven
auto-rejects so observers can distinguish framework auto-rejects
from human "anonymous" rejects:

```go
// Before (human anonymous reject):
ApprovedBy: ""  // → handleRejectedApproval substitutes "anonymous"
// After (sweeper auto-reject):
ApprovedBy: "system:approval-timeout"
```

If your audit dashboards key on `ApprovedBy`, add the new
sentinel to your distinct-values list.

### Pre-request integrity audit

`(*ContextManager).RepairToolPairs()` is the new public method.
The framework calls it automatically before every `agent.request`
build site (`handleToolsComplete` and `emitRetryRequest`).
Products consuming the framework via the public Component API
don't need to call it directly; it's wired transparently.

Hot path: well-formed contexts iterate the recent-history slice
once and return zero. No allocation, no warning logged.

When the audit removes orphans (typically: a loop restored from
KV with corrupt context written by an older binary version),
the framework logs a `Warn` with the count.

### Approval handler panic recovery

`HandleApprovalResponse` now wraps its body in `defer recover()`.
A panic in the resolve race, dispatch path, or rejection-synth
no longer leaves the gated `tool_call` orphaned forever — the
recovery returns a benign empty result and the timeout sweeper
picks up the still-awaiting loop on its next tick.

## What is NOT changing

- **Existing `dispatchToolCall` callers** — signature unchanged.
  The orphan recovery wraps the call site, not the function.
- **`agentic.ToolResult` shape** — unchanged. Synth-results use
  the same `{CallID, Name, Error, LoopID}` shape that
  empty-name and filter-rejection paths have used since beta.18.
- **`agentic.ApprovalResponse` shape** — unchanged. Only the
  `ApprovedBy` sentinel value is new.
- **The compaction path** — still calls
  `repairToolPairsLocked` from `SliceForBudget`. The hoist is
  additive; the compaction call site uses the public method now,
  but the logic is unchanged.
- **Mode b — tool result publish failures on the executor side**
  — out of scope for the loop. The loop sees this as "result
  never arrives" — i.e., it manifests as the iteration-timeout
  path and gets a synth-result there.

## Verification

```bash
# Unit tests + state-machine tests
go test -race ./processor/agentic-loop/...

# Integration tests (requires Docker)
go test -race -tags=integration ./processor/agentic-loop/...

# Confirm `task lint` clean
task lint

# Schema regen unchanged (no new payload types)
task schema:generate
git diff schemas/ specs/openapi.v3.yaml
```

## Operational notes

### Sweeper interval

Hardcoded at 5 seconds (`approvalSweepInterval` in
`processor/agentic-loop/approval_sweeper.go`). Not configurable
in beta.25 — the latency tradeoff is small (sweep is cheap,
human-approval timeouts are typically minutes). Make this
configurable only if a product surfaces a real reason.

### Restart safety

The sweeper relies on `PendingApproval.RequestedAt` and
`PendingApproval.Timeout` (KV-persisted on the loop entity). A
loop's deadline is computed correctly on the first sweep tick
after process restart. An expired loop in KV at restart will
auto-reject within `approvalSweepInterval` of the new process
booting.

### Drain logging

Terminal-transition paths emit an `Info` log when they drain
pending tools to synth-results:

```
draining pending tool calls with synthetic failures
  loop_id=...
  count=3
  reason="loop cancelled by user-1"
```

Per-instance counts above one or two suggest a flaky executor
(real failure rate higher than the model expects). Counts of
zero are the hot path and produce no log.

## Related

- Memory: `project_orphan_tool_call_recovery.md` — captures the
  gap, six failure modes, the two-seam architectural choice, and
  scope decisions (mode b out, approval-timeout timer in).
- Plan: `~/.claude/plans/orphan-tool-call-recovery.md`.
- Beta.19 approval flow: `feedback_approval_required_gap.md` +
  `docs/operations/migration-beta18-to-beta19.md`.
- Compaction-time tool-pair audit (the prior, narrower path):
  `processor/agentic-loop/context_manager.go:repairToolPairsLocked`.
