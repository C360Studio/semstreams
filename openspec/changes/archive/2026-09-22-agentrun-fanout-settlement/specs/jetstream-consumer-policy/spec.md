# jetstream-consumer-policy Delta

## MODIFIED Requirements

### Requirement: semantic heartbeat settlement has one permanent exported surface

The framework SHALL expose `ConsumeDeliveryWithHeartbeat` with validated `HeartbeatDeliveryPolicy`,
`DeliveryDecision`, and `DeliveryResult`.

`NewDurableHandler` and `ConsumeWithHeartbeat` SHALL NOT exist or have an alias. Every original model, tools,
dispatch, loop, and AgentRun heartbeat binding SHALL use the permanent typed surface with its owner-specific durable
definition of done.

No capability SHALL describe a production legacy allowlist. The caller ratchet SHALL assert zero declarations and
zero references to the removed helper in every package; it is retirement conformance only.

#### Scenario: public surface at this layer

- **WHEN** this change is archived
- **THEN** the permanent typed API exists
- **AND** `NewDurableHandler` and every alias are absent with zero production callers
- **AND** `ConsumeWithHeartbeat` is absent: no declaration, alias, or production caller

#### Scenario: binding migration requires semantic authority

- **WHEN** a durable binding migrates
- **THEN** its decision matrix names the exact durable positive and negative consequences
- **AND** nil/error callback behavior alone does not authorize ACK or Retry

#### Scenario: fast lane lacks an admitted settlement route

- **WHEN** an inventoried fast no-heartbeat lane cannot use an existing owner path
- **THEN** migration stops for a separately reviewed capability delta
- **AND** no raw message settlement or exported no-heartbeat interpreter is introduced
