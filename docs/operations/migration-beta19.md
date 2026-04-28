# Migration Guide: beta.18 → beta.19

## Summary

Beta.19 closes the long-standing gap in `approval_required` — the
config flag that was supposed to gate tools requiring human approval
but, through beta.18, only rejected the first call without halting
the agent loop. The LLM's next round-trip could retry the same tool
with different arguments or reroute through a sibling, so anything
relying on `approval_required` for human-in-the-loop safety was
exposed.

The fix is loop-side wiring: when the agentic-tools approval filter
returns an `approval_required: ...` rejection, the agent loop now
pauses, snapshots the pending call, emits a NATS event for a
product-layer approval UI to consume, and resumes only when a
matching response arrives.

This is **additive on existing surfaces** — `approval_required`
config field unchanged, existing tool implementations unchanged,
existing payload types unchanged. The behaviour change is the loop
no longer letting a gated rejection through to the next LLM
round-trip; that was the bug being fixed.

## Why

[`feedback_approval_required_gap.md`](https://github.com/anthropics/claude-code/issues)
captured the audit finding from semteams during their beta.16 review.
The gap had been present since beta.15. Beta.18 documented it as a
known limitation; beta.19 closes it.

Without loop-side wiring, `approval_required` was effectively
advisory — the LLM saw a permission error and decided what to do
next. With loop-side wiring, the loop pauses deterministically until
a human (or an automated approval system) responds.

## What Changes

### New payload types (`agentic` package)

Two new payloads register through the standard `RegisterPayloads`
plumbing (no manual setup required):

- `agentic.ApprovalPendingEvent` — published by agentic-loop when a
  tool call hits an `approval_required` gate. Carries the loop ID,
  call ID, tool name, original arguments, rejection reason, and an
  optional timeout.
- `agentic.ApprovalResponse` — published by a product-layer approval
  UI. Carries the decision (`approve` | `reject` | `modify`),
  optional `modified_arguments` for the modify path, and the
  approver identity.

Wire-format constants:

- `agentic.CategoryApprovalPending = "approval_pending"`
- `agentic.CategoryApprovalResponse = "approval_response"`
- `agentic.ApprovalRequiredPrefix = "approval_required: "`
- `agentic.ApprovalRejectedPrefix = "approval_rejected: "`
- `agentic.ApprovalDecisionApprove`, `ApprovalDecisionReject`,
  `ApprovalDecisionModify`

### New NATS subjects (agentic-loop ports)

| Direction | Port name | Default subject | Required |
|---|---|---|---|
| Output | `agent.approval_pending` | `agent.approval_pending.*` | (always declared) |
| Input | `agent.approval_response` | `agent.approval_response.*` | optional |

Both go through `component.ResolveSubject` so port-config overrides
work the same way as the existing `agent.complete` /
`agent.created` etc. ports.

### New LoopEntity fields

- `LoopEntity.PendingApproval *PendingApprovalState` — set when the
  loop pauses on a gated rejection. Persisted in AGENT_LOOPS KV so
  a process restart mid-approval still remembers what's pending.
- `LoopEntity.StateBeforeApproval LoopState` — the state to restore
  when the approval resolves. Kept separate from the existing
  `StateBeforePause` field so signal-pause and approval-pause stay
  orthogonal.

### New ToolCall field

- `ToolCall.ApprovedBy string` — set by the agent loop on
  re-dispatch after a successful `ApprovalResponse`. The agentic-
  tools approval filter recognises a non-empty value as the explicit
  bypass token. Empty preserves the existing gating behaviour.

### New agentic-loop config

```yaml
processor.agentic-loop:
  approval_timeout: ""  # e.g. "5m" or "1h" — empty means wait forever
```

`ApprovalTimeoutStr` / `ApprovalTimeout()` on `Config`. Default empty
preserves the "wait indefinitely" behaviour. The auto-reject timer
itself has not yet been wired (it lives behind the same
`ApprovalResponse` path with `decision=reject`); the field is
declared so downstream config can reference it without a future
breaking change.

## Wiring an Approval UI

Subscribe to `agent.approval_pending.<loop_id>` (or `>` for all
loops):

```go
sub, _ := js.Subscribe("agent.approval_pending.>", func(msg *nats.Msg) {
    decoder := message.NewDecoder(payloadRegistry)
    base, err := decoder.Decode(msg.Data)
    if err != nil { /* ... */ }
    pending, _ := base.Payload().(*agentic.ApprovalPendingEvent)
    // Surface pending.LoopID, pending.ToolName, pending.Arguments,
    // pending.Reason to whoever does the approving.
})
```

Publish `agent.approval_response.<loop_id>` once the human (or your
automation) decides:

```go
response := &agentic.ApprovalResponse{
    LoopID:     pending.LoopID,
    CallID:     pending.CallID,
    Decision:   agentic.ApprovalDecisionApprove,
    ApprovedBy: "alice@example.com",
    DecidedAt:  time.Now().UTC(),
}
envelope := message.NewBaseMessage(response.Schema(), response, "approval-ui")
data, _ := json.Marshal(envelope)
js.Publish("agent.approval_response."+pending.LoopID, data)
```

Decision behaviours:

- `approve` — loop re-dispatches the original ToolCall with
  `ApprovedBy` stamped on it. The agentic-tools filter sees the
  bypass token and lets the call through to the executor. Result
  flows back to the LLM normally.
- `modify` — same as approve but with
  `response.ModifiedArguments` substituted for the original
  arguments. Use this when the human wants to narrow scope (e.g.,
  change `path: "/etc/passwd"` to `path: "/tmp/safe"`) before
  approving.
- `reject` — loop synthesises a `ToolResult` carrying the distinct
  `approval_rejected: ...` prefix and feeds it through the normal
  tool-result path. The LLM gets one round-trip with the rejection
  to decide what to do next (often: terminate gracefully).

`ApprovedBy` is required on `approve` and `modify`; optional on
`reject` (timeout-driven auto-rejects can be anonymous).

## Idempotency and Edge Cases

- **Stale responses:** if the loop is no longer in
  `awaiting_approval` (e.g., because a duplicate response already
  resolved it), the handler logs and drops the message. No error,
  no state change.
- **Call_id mismatch:** if the response's `call_id` doesn't match
  `LoopEntity.PendingApproval.CallID`, the handler logs and drops.
  This protects against UI double-clicks racing with a webhook
  retry.
- **Sibling tool results:** if the LLM emitted multiple tool calls
  in a batch and the first one hit the approval gate, sibling
  results that arrive after the pause are stored and trajectoried
  but do **not** advance the loop. The resume path drains them
  alongside the approved/rejected result.

## Audit Trail

The full approval flow is observable from the trajectory and KV
state:

1. **Initial rejection** — `tool_call` trajectory step with
   `ErrorKind: "permission"` and `ErrorMessage` starting with
   `approval_required: ...`.
2. **Pause** — `LoopEntity.State = "awaiting_approval"`,
   `PendingApproval` populated. Persisted to AGENT_LOOPS KV.
3. **Pending event** — `ApprovalPendingEvent` published on
   `agent.approval_pending.<loop_id>`. Stream-backed, replayable.
4. **Resolution** — `ApprovalResponse` consumed; entity transitions
   back to the prior state; `PendingApproval` cleared.
5. **Re-dispatch (approve/modify)** — new `ToolCall` envelope with
   `approved_by` field set, visible in `tool.execute.<name>`
   subject and downstream trajectory.
6. **Rejection (reject)** — synthesised `tool_call` step with
   `ErrorMessage` starting with `approval_rejected: rejected by
   <approver>: <reason>`.

## Verification

After upgrading, the following should hold:

- `go build ./...` succeeds.
- `go test -race ./...` passes including the new
  `agentic/approval_*_test.go` and
  `processor/agentic-loop/approval_*_test.go` suites.
- `task lint` reports 0 revive warnings.
- Existing flows that don't use `approval_required` continue to
  work unchanged.
- For flows that **do** use `approval_required`: the LLM no longer
  receives the rejection as a tool error — instead the loop
  pauses, and you must wire an approval UI (or an automated
  responder) to resolve it.

## Security Model — IMPORTANT

`ToolCall.ApprovedBy` is the bypass token the agentic-tools approval
filter checks (`processor/agentic-tools/approval_filter.go`). The
agent loop sets it only when re-dispatching a tool after a valid
`ApprovalResponse`. **The filter trusts the field unconditionally.**
This has a real consequence: any process with NATS publish rights
to `tool.execute.>` can publish a forged `ToolCall{ApprovedBy:
"x"}` and bypass every gated tool — the filter cannot tell a
loop-injected re-dispatch from an external publisher.

In a single-process deployment with no NATS auth scoping, the
threat surface is "anything with NATS write rights to your stream."
For most semstreams deployments today that's the deployment itself
(every pod, every test harness left wired up).

### What you should do

- **Scope `tool.execute.>` writes via NATS auth.** Use NATS
  accounts or stream-level publish permissions to allow only the
  agentic-loop process(es) to publish on `tool.execute.>`. This
  is the cleanest fix and the recommended posture for any
  deployment where `approval_required` is part of the safety
  boundary. NATS docs:
  [Multi-Tenancy via Accounts](https://docs.nats.io/running-a-nats-service/configuration/securing_nats/accounts).
- **Tool-side defence in depth still applies.** The same advice
  from the beta.18 known-limitation section is still correct:
  if a tool genuinely must not run without human review, the
  tool implementation itself should require an out-of-band
  approval token (HMAC-signed, KV-cached, etc.) that the LLM
  cannot mint. The framework's approval flow is a coordination
  mechanism, not an authorization mechanism.
- **Allowlist-scope the registered tools.** A tool that isn't in
  `default_tools` cannot be invoked by an LLM, forged or not. If
  the threat model includes a compromised LLM or an in-process
  attacker, prefer "can't reach the tool at all" over "tool
  rejects without approval token."

### What's planned upstream (not in beta.19)

A future tag will move the bypass decision off the wire payload.
Likely shape: when the loop dispatches an approved re-execution
it writes a one-shot KV token keyed by `call_id` into a framework-
owned bucket; the executor (or a server-side filter wrapper) reads
and consumes the token before running the tool. Forging
`ApprovedBy` over the wire then has no effect because the executor
ignores the field and trusts only the KV record. Out of scope for
beta.19 because the surface change touches every executor.

## Out of Scope

- The auto-reject timer. The `approval_timeout` config field is
  declared but the timer-driven response synthesis is not yet
  wired. If you need a hard deadline, publish a
  `decision=reject` response from your UI's own scheduler.
- Multi-step approvals (committee, two-person rule).
- An approval audit ledger as a graph entity. Possible follow-up;
  current trajectory + KV history are sufficient for v1.
