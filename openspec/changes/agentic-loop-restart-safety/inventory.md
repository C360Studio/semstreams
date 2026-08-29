# Inventory: agentic-loop restart-safe settlement

## Checkpoint status

- Baseline: `origin/main@b060511f383d74aa6a8684e39e42020a3b073a9b`.
- Source: repository evidence already recorded on #1146 plus a fresh SemStreams architect census in progress.
- Review state: **PROVISIONAL — NOT YET `INVENTORY PASS`**.
- This checkpoint authorizes the draft claim only. It does not authorize target-state design or implementation.

## Claimed gap

The current implementation has durable inputs and some durable outputs, but it does not consistently connect callback
return—and therefore JetStream settlement—to the required durable business outcome.

- Process-only execution state: `processor/agentic-loop/state.go:17-49,68-110`.
- Process-only model context: `processor/agentic-loop/context_manager.go:38-55,74-99`.
- Startup binds consumers and starts the approval sweeper without a proved replacement contract:
  `processor/agentic-loop/component.go:515-583`.
- Model, tool, signal, approval, and rule-verdict handlers are adapted from void:
  `processor/agentic-loop/component.go:887-910`.
- Missing ToolResult correlation returns normally and is ACKed:
  `processor/agentic-loop/component.go:1759-1787`.
- Approval-response failures are logged without propagating settlement:
  `processor/agentic-loop/approval_response_handler.go:152-187`.
- Loop-state persistence failures are logged without reaching input settlement:
  `processor/agentic-loop/component.go:1988-2008`.

These citations are hypotheses to re-measure on the checkpoint baseline; they are not an accepted complete census.

## Adjacent owners and collision boundary

| Semantic job | Existing or active owner | Collision question still to inventory |
|---|---|---|
| Delivery heartbeat and terminal settlement | #759 / `natsclient` | Which #1146 callbacks can classify against the shared contract without adding state? |
| Completed tool-call replay | `TOOL_CALL_OUTCOMES`, #949 | Which post-effect windows are already closed by CallID replay? |
| Loop current/terminal state | agentic-loop `LoopEntity` / `AGENT_LOOPS` | Which transitions are authoritative versus merely observable? |
| Large continuation and result material | registered `storage.Store` | Which callbacks already persist a typed reference before publication? |
| Approval timeout/current facts | approval records and sweeper | Does the exact reviewed call survive replacement for approve/modify/reject/timeout? |
| Dispatch terminal routing | `agentic-terminal-events` capability | Which PubAck/replay guarantees already close dispatch crash windows? |
| Component process lifecycle | component/service owner | Exact consumer handles remain component-owned; no second lifecycle authority is admitted. |

The complete collision table must enumerate catalogs, status, lifecycle, ownership, readers, writers, and recovery
for each row before inventory review.

## Adopter seam — provisional

The affected adopter is a SemStreams component author implementing durable agentic work.

- **What they currently must know:** which durable writes/publications define completion, which failures are safe to
  retry, and which external effects may be commit-unknown after interruption.
- **If they do nothing:** a nil callback return can ACK a failed or uncorrelated transition; blind retry can instead
  repeat an ambiguous external effect.
- **Where they find out today:** distributed handler behavior, logs, and documentation—below the required typed
  runtime boundary.
- **What they should know:** only how to classify the domain outcome after completing its required durable effects.
  The framework should own heartbeat and settlement mechanics, while the component retains exact consumer lifecycle
  and operation-specific ambiguity policy.

## Open inventory questions

- Exact subject, consumer, codec, handler, durable precondition, effect, PubAck, and current settlement for every
  model/tool/signal/approval/rule-verdict delivery.
- Exact stable identity and replay authority available at each boundary.
- Whether provider idempotency or reconciliation exists anywhere today.
- Which committed results can be replayed beyond the NATS duplicate window.
- Whether approval records retain or can retrieve the exact call the approver reviewed.
- Every current context-retention or hidden lifecycle authority on the touched surface.
- Every active change and issue that edits the same files or claims the same recovery job.

