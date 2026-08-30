## ADDED Requirements

### Requirement: A framework family minted from another entity derives a collision-free instance
A framework builder that mints an entity from another entity MUST derive the instance segment from that origin's full canonical identifier, so that two origins under distinct authorities never produce one local identity.
Owner-ruled acceptance (#1168, 2026-08-29), transcribed. The derivation primitive, its consolidation target, and the
`RunID` consequence are design decisions that wait on the inventory and on the owner's ruling of open question 1;
capability placement is provisional until the inventory confirms it.

#### Scenario: two foreign origins sharing an instance token stay distinct
- **GIVEN** a deployment with authority `acme`/`dep1`
- **AND** two imported origins `peer1.x.gcs.agent.loop.abc` and `peer2.y.gcs.agent.loop.abc` that share the instance token `abc`
- **WHEN** the framework mints its local family entity for each origin
- **THEN** the two local identities differ
- **AND** neither mint returns the entity the other origin's mint produced

### Requirement: A deployment provisioned from a cloned template does not share its authority pair
The framework MUST NOT let two deployments provisioned from one configuration template silently mint under the same `org.platform` authority pair.
Owner-ruled shape (#1168, 2026-08-29), transcribed: a framework-minted entropy suffix on `platform.id` by default,
persisted at first boot; an operator who overrides it with a pure-readable value owns uniqueness knowingly. Whether
configuration load refuses an entropy-less value or only defaults an absent one is open question 2, unruled.

#### Scenario: two fresh boots from one template mint distinct authorities
- **GIVEN** two deployments whose configuration files are byte-identical copies of one template
- **WHEN** each boots for the first time
- **THEN** the `org.platform` pair each mints under differs from the other's
- **AND** each deployment's pair is stable across its own later restarts
