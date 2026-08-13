# Change: Make tool-call completion durable before request acknowledgement

## Why

`agentic-tools` currently executes an at-least-once `tool.execute` delivery, publishes `tool.result`, and then ACKs.
A crash or publication failure between the external effect and the ACK can execute the same logical call again. Worse,
the current consumer callback discards the handler outcome, so failed result publication is ACKed as success.

## What changes

- Add one framework-owned `TOOL_CALL_OUTCOMES` KV bucket, created by `agentic-tools` before its consumers.
- Persist an immutable COMPLETED outcome using Create-CAS before synchronously publishing the authoritative result.
- Replay a matching completed result without invoking an executor and terminate collision/corrupt records.
- Propagate the handler outcome through heartbeat-owned ACK, delayed NAK, and Term settlement.
- Treat `approval_required` as nonterminal coordination with no ledger write and a phase-distinct message ID.
- Attempt full payloads first. A storage-side typed oversize creates a compact authority; a publication-only typed
  oversize preserves the full authority and gets one compact transport-surrogate attempt for that delivery.
- Recover executor panic into a compact internal result.

## Impact

- Modified capabilities: `agentic-tools`, `nats-streaming`, `framework-bucket-catalog`.
- Runtime: `processor/agentic-tools`, `graph`, and the shared heartbeat settlement helper.
- Behavioral breaking change: poison tool requests terminate rather than being silently ACKed, and completed results
  become immutable by `ToolCall.ID` plus the complete V1 request fingerprint.
- Fresh pre-v1 state: no deployed outcome ledger requires migration or compatibility handling.
- Required breaking gate: `task e2e:agentic` with result-publication fault injection and exact invocation count one.

## Non-goals

- Universal exactly-once external effects.
- Claimed/in-progress leases, result references, chunking, predictive size checks, or adopter payload-limit knobs.
- Changes owned by #865 or #866.
