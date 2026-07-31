# graph-ingest — Delta

## ADDED Requirements

### Requirement: Graph-ingest holds no steady-state self-watch on ENTITY_STATES

After the boot snapshot sweep completes, graph-ingest MUST hold no live watcher on ENTITY_STATES:
the sweep validates the full resident snapshot synchronously during Start, stops its watcher, and
then MUST continue consuming the watcher's update channel until it closes, discarding
post-marker entries — the deliberate stop and its channel closure MUST NOT be classified as
watch loss or transport failure. A genuine transport failure during the snapshot drain keeps the
existing recovery contract: ingest boots, entity queries stay not-ready, and no poison is
recorded from the failure itself.

#### Scenario: a steady-state write is not re-delivered to its writer

- **GIVEN** graph-ingest completed Start with a successful snapshot sweep
- **WHEN** an entity write commits to ENTITY_STATES
- **THEN** no graph-ingest-owned watcher receives that write
- **AND** the ENTITY_STATES stream carries no graph-ingest guard consumer at steady state

#### Scenario: deliberate stop drains to channel close without misclassification

- **GIVEN** the snapshot sweep reached the end-of-snapshot marker while concurrent writers keep
  publishing
- **WHEN** graph-ingest stops the sweep watcher
- **THEN** the update channel is consumed until it closes and pending entries are discarded
- **AND** entity queries are ready and Health is not degraded by the stopped watcher

#### Scenario: snapshot transport failure keeps the boot recovery contract

- **GIVEN** the snapshot drain fails with a transport error before completing
- **WHEN** Start continues
- **THEN** ingest writers boot and operate
- **AND** entity queries return the transient not-ready classification
- **AND** no poison is recorded from the transport failure

### Requirement: The boot snapshot sweep validates every resident entity, last revision wins

The boot snapshot sweep MUST validate every resident ENTITY_STATES value with the canonical
decoder before steady-state operation, recording poisoned entities in the poison inventory
(structured ERROR + metric) instead of failing startup, and MUST resolve multiple deliveries of
the same key during the drain to the last-delivered revision — a key whose poisoned revision is
superseded by a valid pre-marker revision ends with no inventory entry. Snapshot completeness
assumes ENTITY_STATES keeps history depth 1; raising history invalidates this contract.

#### Scenario: resident poison from before this boot is inventoried

- **GIVEN** an ENTITY_STATES value that fails the canonical decode is resident at boot
- **WHEN** the snapshot sweep processes it
- **THEN** the entity is recorded in the poison inventory with its bounded reason and revision
- **AND** a structured ERROR names the entity once
- **AND** ingest still boots

#### Scenario: a key repaired mid-drain is not inventoried

- **GIVEN** the drain delivers a poisoned revision of entity A and later a valid revision of A
  before the end-of-snapshot marker
- **WHEN** the sweep completes
- **THEN** A has no poison inventory entry

### Requirement: Poison refusal is scoped to the poisoned entity

A poisoned entity MUST refuse per-entity across every lane: reads of the poisoned entity return
the typed `graph_state_reset_required` classification with its bounded reason, mutations whose
resident read or RMW cycle encounters the poison fail with the same typed fatal classification
(never a retryable or caller-blaming class), and reads, ingest, and mutations of every other
entity proceed. On poison detection the entity's query-cache entry MUST be invalidated so cached
responses cannot outlive detection.

#### Scenario: one poisoned entity does not take down the query surface

- **GIVEN** entity A's resident state fails the canonical decode and entity B's is valid
- **WHEN** a caller queries A and then queries B
- **THEN** the read of A fails with `graph_state_reset_required`
- **AND** the read of B returns B's state

#### Scenario: ingest of healthy entities continues during a poison incident

- **GIVEN** entity A is poisoned
- **WHEN** a Graphable arrival for entity B is processed
- **THEN** B's merge commits normally

#### Scenario: mutation read seams return the typed classification

- **GIVEN** entity A's resident state is poisoned
- **WHEN** a caller issues `entity.update`, `update_with_triples`, or `create_with_triples`
  against A
- **THEN** the reply carries the fatal `graph_state_reset_required` classification
- **AND** no reply invites the caller to retry the same request

#### Scenario: detection invalidates the cached entry

- **GIVEN** entity A's state is cached by the query cache and A's stored bytes are poisoned
  out-of-band
- **WHEN** any lane detects A's poison and records it in the inventory
- **THEN** A's query-cache entry is invalidated in the same detection

#### Scenario: suffix resolution does not serve entity state

- **GIVEN** entity A is poisoned
- **WHEN** a suffix query resolves A's ID
- **THEN** the resolution may return the ID without decoding A's bytes
- **AND** any subsequent read of A's state fails with the typed classification

### Requirement: An aggregate read encountering poison fails naming every poisoned entity

A multi-entity read that encounters poisoned entities MUST fail as a whole with the typed
`graph_state_reset_required` error identifying every poisoned entity encountered in that attempt
as a bounded list, MUST record all of them in the poison inventory in that same attempt, and
MUST NOT silently omit any entity from a successful response.

#### Scenario: batch fetch fails loudly and names all poisoned entities

- **GIVEN** a batch read spanning entities A and C (both poisoned) and B (valid)
- **WHEN** the aggregate read executes
- **THEN** the whole read fails with `graph_state_reset_required` identifying both A and C
- **AND** A and C are both recorded in the poison inventory by that single attempt
- **AND** no response is returned that contains B but silently omits A or C

### Requirement: The poison inventory is observability-only, revision-stamped, and self-healing

The per-entity poison inventory MUST NOT gate any read or write decision — refusal derives
solely from decoding the bytes actually stored — and each entry MUST carry the KV revision
whose decode failed. An entry MUST clear when the entity is deleted, when a write successfully
commits a newer revision to its key, or when any read of the key successfully validates its
current bytes, so Health and metrics recover without a process restart in both in-band and
out-of-band repair directions. Steady-state cost with an empty inventory MUST be a single
atomic check on the commit path.

#### Scenario: inventory drives Health, gauge, and enumeration while poison is present

- **GIVEN** the poison inventory is non-empty
- **WHEN** Health is reported
- **THEN** the component is unhealthy with status `degraded`, the poisoned-entity count, and a
  bounded sample of IDs
- **AND** the poisoned-entities gauge equals the inventory size
- **AND** the full inventory is enumerable through the component debug surface

#### Scenario: operator repair recovers Health without restart

- **GIVEN** entity A is the only inventoried poisoned entity
- **WHEN** an operator deletes A through the canonical `graph.mutation.entity.delete` verb
- **THEN** A's inventory entry clears and the gauge reads zero
- **AND** Health recovers
- **AND** a subsequent canonical create of A serves normally

#### Scenario: an out-of-band repair clears on the next successful read

- **GIVEN** entity A is inventoried and A's stored bytes are subsequently replaced with valid
  bytes outside the mutation API
- **WHEN** any read of A validates its current bytes
- **THEN** A's inventory entry clears without a restart

#### Scenario: a concurrent repair commit is not erased by a stale record

- **GIVEN** a mutation lane classifies A's resident poison at revision R while another lane
  commits valid bytes to A at revision R+1
- **WHEN** both the record and the clear complete in either order
- **THEN** the inventory ends with no entry for A

#### Scenario: re-poisoned entity is re-inventoried and re-logged

- **GIVEN** entity A was inventoried, repaired, and cleared
- **WHEN** A's stored bytes fail the canonical decode again on any detection path
- **THEN** A is re-recorded in the inventory
- **AND** a structured ERROR names A again

#### Scenario: a stale inventory entry cannot refuse a repaired entity

- **GIVEN** entity A's stored bytes were repaired
- **WHEN** a caller reads A before any inventory bookkeeping runs
- **THEN** the read validates A's current bytes and serves

### Requirement: Resident-poison arrivals are redelivered, not destroyed

An ingest arrival that fails because the target entity's RESIDENT state is poisoned MUST be
negatively acknowledged for redelivery (bounded by the consumer's delivery cap) so valid data
survives the repair window, while an arrival whose own candidate is structurally invalid remains
terminally rejected.

#### Scenario: valid arrival survives a poison window

- **GIVEN** entity A's resident state is poisoned and a valid Graphable arrival for A is
  delivered
- **WHEN** the ingest lane classifies the resident poison
- **THEN** the message is negatively acknowledged, not terminated
- **AND** after an operator repairs A, a redelivery of the same message applies successfully

#### Scenario: structurally invalid candidate is still terminal

- **GIVEN** an arrival whose own projection fails the structural contract
- **WHEN** the ingest lane rejects it
- **THEN** the message is terminated and never redelivered
