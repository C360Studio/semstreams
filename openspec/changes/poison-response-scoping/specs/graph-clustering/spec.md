# graph-clustering — Delta

## ADDED Requirements

### Requirement: Contract validation rides the polled input path, with no ENTITY_STATES watcher

Graph-clustering MUST validate ENTITY_STATES values at its consuming read seam (the polled
entity-state queries that drive detection and enhancement) and MUST hold no ENTITY_STATES
watcher at all — its input path is timer-driven polled reads, not a watch. A validating-decode
failure at the consuming seam MUST drive the sticky whole-view projection reset-required latch;
because each detection cycle's corpus read decodes the resident entity set, resident poison
latches within one detection interval of appearing.

#### Scenario: consumed poison latches the sticky projection reset

- **GIVEN** a poisoned ENTITY_STATES value resident in the bucket
- **WHEN** a detection or enhancement cycle reads and fails to decode it
- **THEN** clustering enters its sticky reset-required projection state
- **AND** the latch survives a later valid overwrite of the same key until process restart

#### Scenario: steady-state writes cost clustering nothing

- **GIVEN** graph-clustering is running at steady state
- **WHEN** an entity write commits to ENTITY_STATES
- **THEN** clustering receives zero deliveries of that write
- **AND** clustering holds zero ENTITY_STATES watchers
