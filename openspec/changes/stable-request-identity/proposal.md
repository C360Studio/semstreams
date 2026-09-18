# Change: stable request, call and loop identity

## Why

Restart safety needs names, not content. When a process is replaced mid-flight, the replacement has to decide
whether a redelivered task, model response or tool result was already applied — and every answer it can give from
message *content* is a reconstruction. The four reviewed checkpoints this change lands (`fd0277c2`, `3d6cab9f`,
`78d54986`, `af829616` on `codex/gh1146-agentic-loop-restart`) give each unit of logical work a stable identity, so
the decision becomes a lookup.

Three identities were missing or unstable. Dispatch minted a fresh LoopID on every redelivery of the same
`UserMessage`, so one task could name two loops. A provider that repeats a `CallID` across turns collided in
`TOOL_CALL_OUTCOMES`, because the outcome was keyed by conversation data rather than by framework identity. And the
RequestID itself was a UUID: two publishes of the *same* logical request carried different names, so neither the
server nor agentic-model could recognise the second as the first.

## What changes

- **Dispatch recovers task identity.** TaskID derives from validated `UserMessage` identity; the minted LoopID is
  retained in the committed `TaskMessage` and read back on redelivery. One TaskID naming two LoopIDs quarantines.
- **Tool execution gets framework identity.** Every `ToolCall` carries RequestID, a positive `CallOrdinal`, and an
  `ExecutionID` derived from the three. Provider `CallID` stays conversation data. Tool, approval, governance and
  completed-outcome correlation move onto the framework identity.
- **The RequestID becomes deterministic.** `<loopID>:req:<iteration>:<retry>` replaces `<loopID>:req:<uuid>`, so the
  same logical request minted twice has the same name (owner ruling Q4 on #1330; #1328 scope amendment).
- **Every `agent.request` publish stamps that name as `Nats-Msg-Id`** through the existing
  `natsclient.PublishToStreamWithMsgID` (ruling Q5), so a stream with a `Duplicates` window rejects the second
  publish server-side.
- **Provider work settles from retained responses.** Before every provider call, agentic-model exact-reads the
  retained `AgentResponse` for the RequestID: a matching one acknowledges without invoking the provider, a
  conflicting one quarantines, typed absence permits the call. `handleRequest` returns a classified
  `DeliveryDecision` instead of logging and acking.
- **Cancel requires PubAck before its source settles**, and the ordinary at-least-once publications of loop,
  tools, dispatch and governance state that they may repeat.

## Impact

- Affected capabilities: `agentic-loop`, `agentic-model`, `agentic-tools`, `agentic-dispatch`, `agentic-governance`.
- Affected code: `processor/agentic-dispatch/task_recovery.go`, `processor/agentic-loop/execution_identity.go`,
  `processor/agentic-loop/state.go`, `processor/agentic-loop/handlers.go`, `processor/agentic-model/component.go`,
  `processor/agentic-model/provider_settlement.go`, `processor/agentic-tools/outcomes.go`, `processor/rule/actions.go`,
  `agentic/user_types.go`, `schemas/agentic-loop.v1.json`.
- Consumer-visible: a RequestID's suffix is no longer a UUID. The `<loopID>:` prefix is unchanged, so a consumer
  that splits on the first colon is unaffected; one that parses the suffix as a UUID is not. Recorded in
  `docs/operations/migration-beta162-to-beta163.md`.

## Non-goals

- **No durable recovery state.** `LoopEntity.PublishedRequestID`, the durable applied-execution set and the
  replacement of the reconstruction layer are L4 (#1330) and are deliberately absent.
- **No exactly-once.** Provider invocation stays durably at-least-once with one recovery rule (owner ruling
  2026-09-05 on #1146). The duplicate window is bounded suppression, never proof.
- **No new bucket, subject, metric family or communication path.**
