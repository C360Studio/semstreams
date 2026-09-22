# nats-streaming Delta

## REMOVED Requirements

### Requirement: the legacy helper is a shrinking remainder, never a compatibility surface

**Reason**: The remainder reached zero in this change. `ConsumeWithHeartbeat` is deleted with its last production
caller (`agentic/agentrun`), without alias, shim, or deprecation window, and the ratchet that held the shrinking
caller set now asserts absence (`jetstream-consumer-policy`, "semantic heartbeat settlement has one permanent exported
surface"). Adopters get the removal from `docs/operations/migration-beta162-to-beta163.md`, as this requirement said
they would (owner ruling 2026-09-18). JetStream remains the delivery and redelivery authority; no supervisor,
checkpoint, outbox, receipt ledger, state-machine runtime, or new durable primitive is added by the removal.
