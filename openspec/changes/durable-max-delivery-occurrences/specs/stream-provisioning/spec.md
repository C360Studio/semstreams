# stream-provisioning Specification Delta

## ADDED Requirements

### Requirement: The framework MUST provision its MaxDeliver advisory ledger without adopter configuration

The framework MUST own the name, subject, bounds, storage, retention, discard, and replica declaration of the
MaxDeliver occurrence ledger. Operator `config.streams` MUST NOT override that declaration, because it is failure-path
infrastructure whose identity must remain identical across replicas. The ledger MUST be provisioned after ordinary
stream resolution and before component consumers start.

#### Scenario: An adopter provides no advisory configuration

- **GIVEN** a valid SemStreams configuration that says nothing about MaxDeliver advisories
- **WHEN** framework streams are provisioned
- **THEN** the fixed bounded ledger is provisioned
- **AND** no application component must predict its subject, name, durable identity, or retention values

#### Scenario: Configuration collides with the fixed ledger

- **GIVEN** `config.streams` or a component-derived port declares the name `MAX_DELIVERY_EVENTS`
- **WHEN** stream declarations are resolved before NATS I/O
- **THEN** boot rejects the collision even when its values equal the fixed declaration
- **AND** the operator is told to remove the declaration

#### Scenario: A restrictive deployment lacks framework permissions

- **GIVEN** the runtime principal lacks required JetStream stream API, reply-inbox, or fixed-consumer permission
- **WHEN** the framework provisions streams or binds the observer
- **THEN** boot fails loudly
- **AND** the framework does not claim MaxDeliver visibility
