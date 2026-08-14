## MODIFIED Requirements

### Requirement: The pre-v1 beta cutover is a clean owned-source break

The breaking stable release MUST announce the exact entity-ID contract change and update every in-repo source,
schema, tool, configuration, fixture, and exact-query expectation to zero violations. Every downstream adoption MUST
start on newly provisioned NATS storage and MUST rerun affected framework and product E2E while readiness remains
fail-closed through initial replay.

This change MUST NOT require or provide persisted-state export, preservation, old-state audit, destructive release
wipe or reseed, online or in-place migration, compatibility readers, alias or rename ledgers, permissive dual
contracts, or rollback to beta state. Downstream adoption and product proof occur after publication and MUST NOT block
local framework graph-index work after its named current-layout prerequisites pass. Discovery of retained deployed
state MUST stop only that adoption and require a separate owner-reviewed migration or recovery design.

Malformed current writes or entity data injected directly into NATS MUST still fail through the canonical typed
contract before state or derived output. This fail-closed behavior and scoped typed poison recovery MUST NOT be
presented as support for upgrading old persisted state.

#### Scenario: the owned reference fleet cuts over from clean state

- **GIVEN** every owned source, configuration, schema, tool, fixture, and expected query is canonical
- **AND** the downstream has newly provisioned NATS storage
- **WHEN** the stable release starts and replays canonical sources
- **THEN** every newly persisted identity satisfies the canonical contract
- **AND** readiness and affected product E2E pass without reading or translating beta state

#### Scenario: Retained state blocks only the affected adoption

- **GIVEN** a downstream intends to adopt the stable release
- **WHEN** retained deployed NATS state is discovered
- **THEN** that adoption stops
- **AND** a separate owner-reviewed migration or recovery design is required
- **AND** the framework does not activate a compatibility, preservation, wipe, reseed, or rollback path

#### Scenario: directly injected malformed current data fails closed

- **GIVEN** a malformed entity record is written directly to an authoritative NATS input after fresh adoption
- **WHEN** an authoritative decoder observes it
- **THEN** the decoder returns the canonical typed structural error before state or projection I/O
- **AND** no compatibility reader, sanitizer, or partial derived result exposes the malformed identity

### Requirement: The entity-ID bound gates graph-index fixed-arity activation

Graph-index MUST treat the canonical maximum as `E = 256` when proving complete current-layout keys and filters
against the shared 1,024-byte NATS KV contract. The maximum INCOMING layout MUST be proven as
`2E + 390 = 902` bytes and 13 tokens. Maximum keys and exact-position filters for every affected layout MUST pass the
shared validators and pinned real-NATS conformance before fixed-arity owner reconciliation activates.

This dependency MUST NOT authorize entity-ID encoding, predicate-layout selection, or graph-index activation before
its separate correctness, performance, readiness, and ADR gates pass.

Graph-index framework activation MUST depend on the completed local entity-ID contract/API, local zero-violation
source corpus, ObjectStore zero-I/O, newly provisioned NATS storage, cold-start/readiness proof, key/filter proof, and
breaking E2E evidence. It MUST NOT depend on this change being archived and MUST NOT add persistence migration or
legacy compatibility. Retained deployed state MUST stop adoption for separate owner review.

#### Scenario: the worst current key fits the shared storage contract

- **GIVEN** canonical source and target entity IDs of 256 bytes each
- **AND** the maximum current predicate token contribution used by INCOMING
- **WHEN** graph-index constructs and validates the complete INCOMING key
- **THEN** the key is 902 bytes and 13 tokens
- **AND** the shared NATS key validator accepts it below the 1,024-byte and 64-token limits

#### Scenario: arithmetic does not bypass real-NATS proof

- **GIVEN** the 902-byte calculation passes unit validation
- **WHEN** graph-index fixed-arity activation is evaluated
- **THEN** activation remains blocked until maximum key/filter match sets pass pinned real-NATS conformance
- **AND** the dependent graph-index correctness, performance, readiness, fresh-start, and ADR gates also pass
