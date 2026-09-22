# nats-streaming Delta

## REMOVED Requirements

### Requirement: the legacy helper is a shrinking remainder, never a compatibility surface

**Reason**: The remainder reached zero in this change. `ConsumeWithHeartbeat` is deleted with its last production
caller (`agentic/agentrun`), without alias, shim, or deprecation window, and the ratchet that held the shrinking
caller set now asserts absence (`jetstream-consumer-policy`, "semantic heartbeat settlement has one permanent exported
surface"). Adopters get the removal from `docs/operations/migration-beta162-to-beta163.md`, as this requirement said
they would (owner ruling 2026-09-18). JetStream remains the delivery and redelivery authority; no supervisor,
checkpoint, outbox, receipt ledger, state-machine runtime, or new durable primitive is added by the removal.

### Requirement: Heartbeat consumption SHALL expose settlement failure

**Reason**: `ConsumeWithHeartbeat`, the only function this requirement binds, is deleted by this change with its last
production caller; the requirement's own text ("This requirement is deleted together with the helper by the PR that
migrates its last caller (#1249)") named this PR. Its scenarios ("transient work fails and delayed NAK fails",
"shutdown NAK fails") describe the deleted helper's NAK paths and go with it. Migrated bindings are governed by
`jetstream-consumer-policy` ("semantic heartbeat settlement has one permanent exported surface") and by
"replay follows the binding's durable authority" in this capability.
