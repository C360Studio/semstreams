# Migration Guide: beta.38 → beta.39

## Summary

Beta.39 closes a wedge semteams hit against beta.37/38: every
sandbox-routed bash call landed on `taskID="default"` because the
`agentic-loop` dispatch layer never stamped the loop ID onto the
outgoing `agentic.ToolCall`. Concurrent agent chains collided on a
single workspace, the `verify_clean` precondition reported cross-chain
pollution, and `read_only_paths` enforcement (sandbox-side, when
deployed) bled across chains. The fix is two-line: dispatch now stamps
both `tc.LoopID` and `tc.Metadata["loop_id"]` before publishing.

| Surface | Status |
|---|---|
| `agentic-loop` dispatch stamps `tc.LoopID` and `tc.Metadata["loop_id"]` | **Behavioural — transparent to existing callers** |
| Bash tool (sandbox path) routes per-loop instead of per-`"default"` | **Behavioural — unblocks concurrent chains** |
| `verify_clean` precondition is now per-loop accurate | **Behavioural — was globally unsound** |
| `agentic.ToolCall.LoopID` and `Metadata["loop_id"]` semantics | **Unchanged** — only the producer changed |

**The simplest beta.38 → beta.39 upgrade is to do nothing.** Existing
deployments inherit correct per-loop routing automatically. No config
changes are required.

## The bug

`processor/agentic-loop/handlers.go:dispatchToolCall(result, loopID, tc)`
took the loopID as a parameter but never wrote it onto `tc` before
serialising. Downstream
(`processor/agentic-tools/executors/bash.go:103-110`) reads:

```go
taskID := "default"
if m := call.Metadata; m != nil {
    if tid, ok := m["task_id"].(string); ok && tid != "" {
        taskID = tid
    } else if lid, ok := m["loop_id"].(string); ok && lid != "" {
        taskID = lid
    }
}
```

With nothing populating either Metadata key, every sandbox-routed bash
call fell through to `"default"`. semteams's reproduction
(`cmd/semteams/sandbox/integration_test.go`) had to manually populate
`Metadata: map[string]any{"task_id": ...}` on the ToolCall to get past
sandbox 404s — a workaround that is no longer needed after this fix.

## The fix

`dispatchToolCall` now stamps loopID onto two fields, both
don't-clobber so explicit upstream values survive:

- `tc.LoopID` — the typed top-level field on `agentic.ToolCall`. The
  canonical contract.
- `tc.Metadata["loop_id"]` — the legacy soft-fallback the bash sandbox
  path reads. Preserved as a wire-level fallback so downstream
  consumers don't need to migrate to read the typed field today.

Both writes happen at the dispatch site so every dispatch path (main
path, approval re-dispatch, queue dequeue) gets the stamp. The
upstream typed-field stamp at `handlers.go:879-880` (where domain
metadata is propagated) intentionally does NOT also stamp
`Metadata["loop_id"]` — keeping the canonical stamp in one place
(dispatchToolCall) avoids drift and ensures no path can bypass it.

## Backward compatibility

- Existing callers of `dispatchToolCall`: unchanged behaviour. The
  function signature is the same; the new writes are internal.
- LLMs / governance filters / experimental tool layers that already
  populated `tc.LoopID` or `tc.Metadata["loop_id"]` explicitly: their
  values are preserved (don't-clobber).
- Bash tool consumers reading `Metadata["loop_id"]`: unchanged. The
  field is now reliably populated end-to-end.
- Bash tool consumers reading `Metadata["task_id"]`: unchanged. That
  field still wins the priority chain on bash.go.

## Cross-references

- `processor/agentic-loop/handlers.go:dispatchToolCall` —
  the stamping site
- `processor/agentic-loop/dispatch_test.go` — three regression tests:
  - `TestDispatchToolCall_StampsLoopID` (main dispatch path)
  - `TestDispatchToolCall_PreservesExplicitMetadata` (don't-clobber)
  - `TestDispatchToolCall_ApprovalRedispatchStampsLoopID`
    (approval_response_handler.go path)
  - `TestDispatchToolCall_DequeuedCallStampsLoopID`
    (handlers.go:dispatchedFromQueue path)
- `processor/agentic-tools/executors/bash.go:103-110` —
  the downstream reader (unchanged)
- `cmd/semteams/sandbox/integration_test.go` (semteams repo) — the
  contract test that reproduced the wedge; the manual `Metadata: {task_id: ...}`
  workaround can be removed at beta.39 or later
- semteams ask: dispatchToolCall doesn't propagate loop_id (the
  empirical case study)

## Follow-up (not in this tag)

`processor/agentic-tools/executors/bash.go` could be simplified in a
later tag to read `tc.LoopID` directly (the typed field) instead of
walking through the Metadata fallback chain. That would let us drop
the `Metadata["loop_id"]` write here and converge on a single source
of truth. Deferred because:

1. The dual-write makes the bash-side migration optional rather than
   required, so beta.39 ships immediately.
2. semteams and other downstream consumers may have wire-level
   tooling (logging, metrics, tracing) that reads `Metadata["loop_id"]`;
   removing it without a coordinated bump risks silent breakage.

A future tag can collapse this once consumers are surveyed.
