# Change: Normalize agent terminal event settlement

## Why

The framework emits three registered terminal payloads through one production `BaseMessage` envelope, but dispatch,
AgentRun, and OTel interpret that terminal set independently. That drift currently causes user-visible failures:

- successful loops carry outcome `success`, while dispatch tests for `complete` when selecting the response;
- cancellation rides `agent.complete.*`, but dispatch asserts only `LoopCompletedEvent` and ACKs cancellation as
  unexpected;
- AgentRun parses a flat envelope while production emits `{id,type,payload,meta}`;
- dispatch ACKs terminal deliveries even when decode or required response publication fails.

The accepted design is recorded in `docs/proposals/gh865-866-terminal-event-design.md`, complete-body SHA-256
`a4b5607eefee80fd3910a769c74b509f4faec8cc04eff84cdddeae26868804c5`.

## What changes

- Add one repo-internal terminal normalizer using a registry-bound `message.Decoder`.
- Fail closed on invalid source identity, message type, metadata, concrete payload, loop/task identity, applicable
  terminal timestamp, or category/outcome pair.
- Recognize exactly three terminal wire pairs:
  - `loop_completed + success`;
  - `loop_failed + failed`;
  - `loop_cancelled + cancelled`.
- Migrate dispatch, AgentRun, and OTel to the internal normalizer.
- Correct success, failure, and cancellation projections.
- Reconcile `ChannelType`, `ChannelID`, and optional `UserID` field-by-field across process-local, terminal-wire, and
  persisted `LoopEntity` state. Publication requires `ChannelType` plus `ChannelID`; `UserID` is optional metadata.
- Give terminal-derived `UserResponse` messages stable `ResponseID` and `Nats-Msg-Id` values derived from the source
  terminal `BaseMessage.ID`.
- Require synchronous response PubAck before terminal ACK; Term permanent rejection and delayed-NAK transient
  routing/publication failure.
- Set dispatch terminal consumers to `MaxDeliver=0` without an adopter retry-count knob.

## Bounded guarantee

`MaxDeliver=0` permits unlimited attempts only while the source terminal remains stored. The checked AGENT stream
configuration is 24h MaxAge, 256MiB MaxBytes, and DiscardOld. Age or capacity eviction can remove an unsettled
terminal, after which this change provides no response-publication guarantee.

The stable response message ID deduplicates only inside the USER stream duplicate window. The contract is
at-least-once within bounded broker retention and deduplication mechanisms, not exactly-once. No outbox, alternate
stream, KV response record, or second durable authority is added.

## Impact

- Modified capability: `agentic-terminal-events`.
- Runtime surfaces: `processor/agentic-dispatch`, `agentic/agentrun`, `output/otel`, and one private normalizer.
- Existing AgentRun constructors, callbacks, terminal subjects, and typed `UserResponse` wire contracts remain.
- Behavioral correction: intended AgentRun callbacks begin firing and terminal projection failures no longer settle
  as success.
- Required evidence: focused race tests, real-NATS production-envelope and retention proof, and `task e2e:agentic`.

## Non-goals

- No new payload, schema, subject, stream, outbox, or exported normalized terminal type.
- No exactly-once or post-AGENT-eviction response guarantee.
- No producer change for truncation: persisted `LoopEntity.Outcome="truncated"` may differ from emitted
  `LoopFailedEvent.Outcome="failed"`.
- No general dispatch restart reconstruction.
- No terminal source-publication-loss repair.
- No result-by-reference or large-result retrieval change; #857 remains separate.
- No heterogeneous `user.response.>` ownership migration; #952 owns that breaking semdev-lockstep change.
