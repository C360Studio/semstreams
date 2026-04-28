# Approval Flow (Human-in-the-Loop Tool Gating)

## Why this exists

Some agent tools should not run without human review — destructive
actions (`delete_rule`, `terminate_pod`), high-impact API calls,
anything where a confused LLM can cause real damage. The
framework's answer is `approval_required`: a config field on
`agentic-tools` that names the tools needing approval. Beta.19 is
the first release where this gate actually halts the agent loop.

Before beta.19, `approval_required` rejected the first call with a
permission error and let the LLM keep going — the model would see
the error and could retry or reroute. From beta.19 forward, the
loop pauses on the first rejection, persists the pending call, and
emits a NATS event a product-layer approval UI can subscribe to.
The loop only resumes when an explicit response arrives.

## The contract

Subjects:

| Direction | Subject | Payload | Frequency |
|---|---|---|---|
| Loop → UI | `agent.approval_pending.<loop_id>` | `agentic.ApprovalPendingEvent` | Once per gated call |
| UI → Loop | `agent.approval_response.<loop_id>` | `agentic.ApprovalResponse` | Once per resolution |

State machine:

```
exploring/planning/executing
        │
        │  tool call hits approval_required
        ▼
  awaiting_approval  ◀─── publishes ApprovalPendingEvent
        │
        │  ApprovalResponse arrives
        ▼
  approve/modify  ─────►  re-dispatch ToolCall (with approved_by)
                          ─►  agentic-tools filter bypasses
                          ─►  executor runs
                          ─►  ToolResult flows back normally

  reject  ──────────────►  synthesise ToolResult with
                          approval_rejected: prefix
                          ─►  LLM gets the rejection
                          ─►  loop continues normally
```

Key design points:

- **One pending call at a time per loop.** If a batch of tool calls
  contains two `approval_required` tools, the first to hit the
  filter triggers the pause. Sibling results (including normal
  tools that did execute) are absorbed without advancing.
- **Distinct prefixes.** The gating prefix is
  `approval_required:`; the synthesised rejection prefix is
  `approval_rejected:`. The loop's gate logic only matches the
  first; the second flows through as a normal tool error.
- **Approver identity is preserved end-to-end.** It rides on the
  `ApprovalResponse`, gets stamped onto the re-dispatched
  `ToolCall.ApprovedBy`, flows through `tool.execute`, lands in
  the trajectory step. Audit consumers can correlate every gated
  action to the human who said yes.
- **Restart-safe.** `LoopEntity.PendingApproval` lives in the
  AGENT_LOOPS KV bucket. A process restart mid-approval doesn't
  lose the pending state; the new process picks up the same
  `awaiting_approval` loop and waits on the same response subject.

## Wiring an approval UI

The framework ships the events and the state. The actual UI is
product layer — semspec, semdragon, your own portal. The minimal
shape:

```go
// Subscribe to all pending approvals (or scope by loop_id).
sub, err := js.Subscribe("agent.approval_pending.>", func(msg *nats.Msg) {
    base, err := decoder.Decode(msg.Data)
    if err != nil {
        log.Error("decode approval pending", "err", err)
        return
    }
    pending, ok := base.Payload().(*agentic.ApprovalPendingEvent)
    if !ok {
        return
    }
    // Surface pending to the human reviewer:
    //   - pending.LoopID    — what loop is paused
    //   - pending.ToolName  — what action is gated
    //   - pending.Arguments — the proposed call payload
    //   - pending.Reason    — original rejection text
    // Capture decision (approve/reject/modify) + approver identity.
})

// Publish the human's decision.
response := &agentic.ApprovalResponse{
    LoopID:     pending.LoopID,
    CallID:     pending.CallID,
    Decision:   agentic.ApprovalDecisionApprove,
    ApprovedBy: "alice@example.com",
    DecidedAt:  time.Now().UTC(),
}
data, _ := json.Marshal(message.NewBaseMessage(
    response.Schema(), response, "approval-ui",
))
js.Publish("agent.approval_response."+pending.LoopID, data)
```

For the modify path, set `response.ModifiedArguments` to the
narrowed payload — the loop substitutes them for the original
arguments before re-dispatching:

```go
response := &agentic.ApprovalResponse{
    LoopID:            pending.LoopID,
    CallID:            pending.CallID,
    Decision:          agentic.ApprovalDecisionModify,
    ModifiedArguments: map[string]any{"path": "/tmp/safe"},
    ApprovedBy:        "alice@example.com",
    DecidedAt:         time.Now().UTC(),
}
```

## When NOT to use this

- **Tools that should never run without human review.**
  `approval_required` makes the LLM able to ask for a sensitive
  action; a human can still say no, but the request reaches the
  human at all. If you don't trust the LLM to be in the same room
  as the tool, **don't register the tool with the agent.** Scope
  `default_tools` / `publish_agent.tools` to an allowlist that
  excludes it.
- **High-frequency confirmations.** Every gated call pauses the
  loop until a human responds. If you're tempted to approve 50
  things per second, the gate is the wrong primitive — build a
  policy engine instead.
- **Programmatic approvals.** Nothing stops an automated responder
  from publishing `agent.approval_response.*` (the `ApprovedBy`
  field can be a service account ID), but at that point you're
  re-implementing access control on top of the approval pathway.
  An allowlist + a coordinator agent is cleaner.

## Defence in depth

The approval flow is one layer. Treat it as part of a stack:

1. **Allowlist what the LLM can call.** A tool not in
   `default_tools` cannot be invoked.
2. **Schema-validate what gets through.** `ToolCall.Arguments`
   should match a strict JSON schema; don't let free-form
   strings into shell commands.
3. **Use approvals for genuinely human-dependent decisions.** Not
   as a substitute for #1 and #2.
4. **Audit everything.** The trajectory, the AGENT_LOOPS KV
   history, and the JetStream replay on
   `agent.approval_pending.>` give you three independent records
   of what happened.

## Related

- [migration-beta19.md](../operations/migration-beta19.md) — the
  upgrade guide.
- [13-agentic-systems.md](13-agentic-systems.md) — the broader
  agent loop architecture this hooks into.
- [14-orchestration-layers.md](14-orchestration-layers.md) — where
  approvals fit in the rules + coordinator + ops layering.
